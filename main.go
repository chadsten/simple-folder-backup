// Package main implements a folder backup tool that runs as a desktop system tray application.
//
// The tool is designed to continuously backup folders with intelligent
// hash-based change detection to avoid unnecessary backups when content hasn't changed.
// Key architectural decisions:
//
// 1. System tray application vs service: Chosen for user visibility and easier management
// 2. Single instance enforcement: Prevents conflicts and resource contention
// 3. Hash-based change detection: Dramatically reduces I/O and storage overhead
// 4. Per-backup logging: Enables debugging specific backup configurations
// 5. Concurrent schedulers: Each backup config runs independently for reliability
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getlantern/systray"
)

// main initializes the backup tool with single instance enforcement and system tray integration.
//
// Single instance enforcement is critical for this application because:
// 1. Multiple instances would compete for file locks during backups
// 2. Hash state could become corrupted with concurrent modifications
// 3. System resources would be wasted on duplicate backup operations
// 4. Log files could become corrupted with concurrent writes
func main() {
	// Enforce single instance before any other initialization to prevent race conditions
	lockFile, err := acquireInstanceLock()
	if err != nil {
		showMessageBox("Backup Tool", "Another instance is already running.\n\nPlease close the existing instance before starting a new one.")
		os.Exit(1)
	}
	defer releaseInstanceLock(lockFile)

	// Initialize system logger first (clears previous session log for fresh start)
	// System logger captures application-level events vs per-backup operational logs
	systemLogger, err := initSystemLogger()
	if err != nil {
		fmt.Printf("Failed to initialize system logger: %v\n", err)
		os.Exit(1)
	}
	
	// Redirect Go's default logger to our system logger for consistent logging
	log.SetOutput(systemLogger.Writer())
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	
	log.Printf("Application starting...")
	
	// systray.Run blocks until application exit - all initialization happens in onReady
	systray.Run(onReady, onExit)
}

// onReady initializes the system tray UI and starts all backup schedulers.
//
// This function is called by the systray library after the system tray is ready.
// Design decisions:
// 1. Status menu items are disabled (read-only) to prevent user confusion
// 2. Each backup config gets its own goroutine for fault isolation
// 3. 30-second status update interval balances UI responsiveness with performance
// 4. Graceful shutdown handling ensures proper cleanup of resources
func onReady() {
	// Set up system tray appearance
	systray.SetIcon(iconData)
	systray.SetTitle("SimpleFolderBackup")
	systray.SetTooltip("SimpleFolderBackup")
	
	// Create status display menu items (disabled = read-only)
	mLastBackup := systray.AddMenuItem("Last backup: Never", "Last backup time")
	mLastBackup.Disable()
	
	mNextBackup := systray.AddMenuItem("Next backup: Unknown", "Next backup time")
	mNextBackup.Disable()
	
	systray.AddSeparator()
	
	mQuit := systray.AddMenuItem("Exit", "Exit the application")
	
	// Load and validate configuration before starting any backup operations
	config, err := loadConfig()
	if err != nil {
		log.Printf("Error loading config: %v", err)
		return
	}
	
	err = validatePaths(config)
	if err != nil {
		log.Printf("Error validating paths: %v", err)
		return
	}
	
	// Initialize hash manager for content-based backup skipping
	// This must be done before any backup schedulers start to avoid race conditions
	initHashManager()
	
	// Create cancellable context for coordinated shutdown of all schedulers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Start a scheduler goroutine for each enabled backup configuration
	// Each runs independently to prevent one backup failure from affecting others
	for _, backup := range config.Backups {
		if backup.IsEnabled() {
			// Create dedicated logger for this backup to isolate log entries
			backupLogger, err := initBackupLogger(backup)
			if err != nil {
				log.Printf("Failed to create logger for %s: %v", backup.Name, err)
				continue
			}
			go startBackupScheduler(ctx, backup, backupLogger)
		} else {
			log.Printf("Skipping disabled backup config: %s", backup.Name)
		}
	}
	
	// Brief delay to allow schedulers to initialize before displaying status
	time.Sleep(100 * time.Millisecond)
	mLastBackup.SetTitle(backupStatus.getLastBackupStatus())
	mNextBackup.SetTitle(backupStatus.getNextBackupStatus())
	
	// Start status update goroutine with 30-second refresh interval
	// Frequent enough for user awareness, infrequent enough to avoid performance impact
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mLastBackup.SetTitle(backupStatus.getLastBackupStatus())
				mNextBackup.SetTitle(backupStatus.getNextBackupStatus())
			}
		}
	}()
	
	// Handle OS signals for graceful shutdown (Ctrl+C, service stop, etc.)
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		cancel() // Signal all backup schedulers to stop
		systray.Quit()
	}()
	
	// Main event loop - blocks until quit is selected or application is terminated
	for {
		select {
		case <-mQuit.ClickedCh:
			cancel() // Signal all backup schedulers to stop cleanly
			systray.Quit()
			return
		}
	}
}

// onExit is called when the system tray application is shutting down.
// The systray library handles most cleanup automatically, but this provides
// a hook for any final cleanup operations if needed in the future.
func onExit() {
	fmt.Println("Application exiting...")
}

// acquireInstanceLock creates an exclusive lock file to enforce single instance operation.
//
// Uses OS-level file locking with O_EXCL flag to atomically check-and-create the lock file.
// This approach works across all platforms and prevents race conditions between multiple
// startup attempts. The PID is written to the lock file for debugging purposes to identify
// which process holds the lock if manual cleanup is ever needed.
//
// Returns the lock file handle that must be passed to releaseInstanceLock for cleanup.
func acquireInstanceLock() (*os.File, error) {
	lockFilePath := "backup-tool.lock"
	
	// Use O_EXCL with O_CREATE for atomic test-and-set behavior
	// If file exists, this fails immediately without race conditions
	lockFile, err := os.OpenFile(lockFilePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("lock file exists - another instance may be running")
		}
		return nil, fmt.Errorf("failed to create lock file: %v", err)
	}
	
	// Write current process ID for debugging/manual cleanup if needed
	_, err = fmt.Fprintf(lockFile, "%d\n", os.Getpid())
	if err != nil {
		// If we can't write PID, clean up lock file to prevent permanent lock
		lockFile.Close()
		os.Remove(lockFilePath)
		return nil, fmt.Errorf("failed to write to lock file: %v", err)
	}
	
	return lockFile, nil
}

// releaseInstanceLock cleans up the instance lock file created by acquireInstanceLock.
//
// This is critical for proper application shutdown - without cleanup, the lock file
// would persist and prevent future application starts. The function is defensive
// and safe to call with a nil lock file handle.
func releaseInstanceLock(lockFile *os.File) {
	if lockFile != nil {
		lockFile.Close()
		os.Remove(lockFile.Name())
	}
}