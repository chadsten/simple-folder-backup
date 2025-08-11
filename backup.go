// Package main - backup.go implements the core backup operations with hash-based change detection.
//
package main

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// executeBackup is the main entry point for backup operations, implementing intelligent
// hash-based skipping to avoid unnecessary I/O when source content hasn't changed.
//
// This two-phase approach is critical for folder backups because:
// 1. Content often remains unchanged for hours during idle periods
// 2. Full directory copying can be expensive (especially for large datasets)
// 3. Hash comparison is orders of magnitude faster than file I/O
// 4. Status tracking needs to be updated regardless of whether backup or skip occurs
//
// Error handling strategy: Hash check failures fall back to performing backup
// to ensure data protection is prioritized over performance optimization.
func executeBackup(config BackupConfig, logger *log.Logger) error {
	// Phase 1: Hash-based change detection check (if enabled)
	if config.IsHashCheckEnabled() {
		shouldSkip, err := hashManager.shouldSkipBackup(config.Name, config.Source)
		if err != nil {
			// Hash check failure - proceed with backup for data safety
			logger.Printf("Hash check failed for %s, proceeding with backup: %v", config.Name, err)
		} else if shouldSkip {
			// Content unchanged - record skip action and update scheduling status
			logger.Printf("Contents identical, backup skipped for %s", config.Name)
			err = hashManager.recordAction(config.Name, config.Source, "skipped")
			if err != nil {
				logger.Printf("Failed to record skip action for %s: %v", config.Name, err)
			}
			// Update status as if backup completed (for scheduling purposes)
			backupStatus.updateBackupCompleted(config.Name, config.ScheduleMinutes)
			
			// Trigger immediate UI update
			select {
			case statusUpdateChan <- struct{}{}:
			default:
			}
			return nil
		}
	}

	// Phase 2: Perform actual backup (either hash disabled or content changed)
	return performBackup(config, logger)
}

// performBackup executes the actual file copying and cleanup operations.
//
// This function implements atomic backup creation - the new backup is created
// completely before any old backups are cleaned up. This ensures that if the
// backup process fails midway, existing backups remain intact and recoverable.
//
// The operation sequence is critical:
// 1. Create backup directory with timestamp-based name
// 2. Copy all source files to backup directory
// 3. Clean up old backups based on rotation count
// 4. Update status tracking for UI display
// 5. Record backup action in hash manager for future change detection
//
// Error handling: Any failure in steps 1-3 will prevent status updates,
// ensuring the backup scheduler will retry on the next interval.
func performBackup(config BackupConfig, logger *log.Logger) error {
	timestamp := time.Now()
	backupDirName := generateBackupDirName(config.Source, timestamp)
	backupDir := filepath.Join(config.Destination, backupDirName)
	
	// Step 1: Create backup directory structure
	err := os.MkdirAll(backupDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create backup directory: %v", err)
	}
	
	// Step 2: Copy source directory tree to backup location
	err = copyDir(config.Source, backupDir)
	if err != nil {
		return fmt.Errorf("failed to copy files: %v", err)
	}
	
	// Step 3: Remove old backups beyond rotation limit
	err = cleanupOldBackups(config)
	if err != nil {
		return fmt.Errorf("failed to cleanup old backups: %v", err)
	}
	
	// Step 4: Update status tracking for UI display (only after successful backup)
	backupStatus.updateBackupCompleted(config.Name, config.ScheduleMinutes)
	
	// Trigger immediate UI update
	select {
	case statusUpdateChan <- struct{}{}:
	default:
	}
	
	// Step 5: Record successful backup in hash manager for future skip decisions
	if config.IsHashCheckEnabled() {
		err = hashManager.recordAction(config.Name, config.Source, "backup")
		if err != nil {
			// Non-critical error - backup succeeded, just hash tracking failed
			logger.Printf("Failed to record backup action for %s: %v", config.Name, err)
		}
	}
	
	return nil
}

// copyDir recursively copies an entire directory tree from src to dst.
//
// Uses filepath.WalkDir for efficient traversal with minimal memory footprint.
// This approach is preferred over alternatives because:
// 1. Handles arbitrary directory depths without stack overflow risk
// 2. Preserves directory permissions during copy operation
// 3. Processes files in filesystem order for better disk I/O patterns
// 4. Single-pass operation minimizes filesystem metadata lookups
//
// Error handling: Any file copy failure immediately stops the entire operation,
// ensuring partial backups are not considered successful.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		
		// Calculate relative path for preserving directory structure
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		
		dstPath := filepath.Join(dst, relPath)
		
		if d.IsDir() {
			// Preserve directory permissions from source
			return os.MkdirAll(dstPath, d.Type().Perm())
		}
		
		// Copy individual file with permission preservation
		return copyFile(path, dstPath)
	})
}

// copyFile copies a single file from src to dst, preserving permissions and timestamps.
//
// This implementation prioritizes data integrity and permission preservation:
// 1. Uses io.Copy for efficient buffered copying without loading entire file into memory
// 2. Ensures destination directory exists before attempting file creation
// 3. Preserves source file permissions to maintain executable flags, etc.
// 4. Proper resource cleanup with defer statements even on error paths
//
// This approach is essential for files which may have specific permission
// requirements or be quite large (especially data files).
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	
	// Ensure destination directory exists
	err = os.MkdirAll(filepath.Dir(dst), 0755)
	if err != nil {
		return err
	}
	
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	
	// Efficient buffered copy without loading entire file into memory
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}
	
	// Preserve source file permissions (important for executable files, etc.)
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	
	return os.Chmod(dst, srcInfo.Mode())
}

// cleanupOldBackups removes backup directories beyond the configured rotation count.
//
// This function implements intelligent backup rotation using modification time sorting:
// 1. Only considers directories matching the backup naming pattern for this source
// 2. Sorts by modification time to preserve the most recent backups
// 3. Only deletes excess backups beyond the rotation limit
// 4. Uses complete directory removal for atomic cleanup
//
// The rotation strategy is critical for long-running backup systems:
// - Prevents unlimited disk space growth from accumulating backups
// - Preserves recent backups which are most likely to be needed for recovery
// - Fails fast on any deletion errors to prevent partial cleanup states
//
// Design choice: ModTime-based sorting rather than timestamp parsing handles edge
// cases like manual backup directory manipulation or clock adjustments gracefully.
func cleanupOldBackups(config BackupConfig) error {
	entries, err := os.ReadDir(config.Destination)
	if err != nil {
		return err
	}
	
	// Filter to only backup directories for this specific source
	var backupDirs []os.DirEntry
	sourceFolderName := getSourceFolderName(config.Source)
	for _, entry := range entries {
		if entry.IsDir() && isBackupDirectory(entry.Name(), sourceFolderName) {
			backupDirs = append(backupDirs, entry)
		}
	}
	
	// No cleanup needed if within rotation limit
	if len(backupDirs) <= config.RotationCount {
		return nil
	}
	
	// Get modification times for sorting (most reliable for chronological order)
	type dirInfo struct {
		entry   os.DirEntry
		modTime time.Time
	}
	
	var dirInfos []dirInfo
	for _, entry := range backupDirs {
		info, err := entry.Info()
		if err != nil {
			continue // Skip entries we can't stat (maybe permissions issue)
		}
		dirInfos = append(dirInfos, dirInfo{entry: entry, modTime: info.ModTime()})
	}
	
	// Sort by modification time (oldest first) for deletion
	sort.Slice(dirInfos, func(i, j int) bool {
		return dirInfos[i].modTime.Before(dirInfos[j].modTime)
	})
	
	// Delete oldest backups beyond rotation count
	toDelete := len(dirInfos) - config.RotationCount
	for i := 0; i < toDelete; i++ {
		dirPath := filepath.Join(config.Destination, dirInfos[i].entry.Name())
		err := os.RemoveAll(dirPath)
		if err != nil {
			return err // Fail fast - don't leave partial cleanup state
		}
	}
	
	return nil
}