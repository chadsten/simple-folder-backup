// Package main - status.go implements status tracking for system tray display.
//
// This module provides the real-time status information displayed in the system tray menu,
// giving users visibility into backup operations across multiple configurations.
//
// Key design decisions:
//
// 1. Thread-safe status aggregation: Multiple backup schedulers update status concurrently,
//    requiring careful synchronization to prevent data races in status display.
//
// 2. Hash-aware status display: Integrates with hash manager to show skip indicators,
//    helping users understand when backups were optimized away vs actually performed.
//
// 3. Cross-configuration status summary: Aggregates status across all backup configurations
//    to show "most recent" and "next due" information in limited tray menu space.
//
// 4. Intelligent scheduling integration: Mirrors the scheduler's hash-aware timing logic
//    to ensure status display matches actual scheduler behavior for consistency.
//
// The status system is critical for user confidence - without visibility into backup
// operations, users can't verify the tool is working correctly or troubleshoot issues.
package main

import (
	"fmt"
	"math"
	"os"
	"sync"
	"time"
)

// BackupStatus manages thread-safe status tracking for all backup configurations.
//
// This structure maintains the state needed for system tray status display:
// - lastBackupTimes: When each config was last checked/backed up
// - nextBackupTimes: When each config is scheduled for next action
// - scheduleMinutes: Interval configuration for each backup
// - configNames: Mapping for config name lookups (enables iteration)
//
// The RWMutex enables concurrent reads for frequent status display updates while
// protecting occasional writes when backup operations complete.
//
// Design choice: Separate maps for each piece of information rather than a single
// map of structs because status display reads are much more frequent than updates,
// and this structure optimizes for read access patterns.
type BackupStatus struct {
	mu                sync.RWMutex          // Protects all status state
	lastBackupTimes   map[string]time.Time  // When config was last processed
	nextBackupTimes   map[string]time.Time  // When config is due for next action
	scheduleMinutes   map[string]int        // Backup interval for each config
	configNames       map[string]string     // Enables iteration over active configs
}

// Global singleton instance provides centralized status tracking across all schedulers
var backupStatus = &BackupStatus{
	lastBackupTimes: make(map[string]time.Time),
	nextBackupTimes: make(map[string]time.Time),
	scheduleMinutes: make(map[string]int),
	configNames:     make(map[string]string),
}

// updateBackupCompleted updates status tracking after a backup operation completes.
//
// Called by both actual backups and skipped backups to maintain consistent status
// display. The "completed" terminology refers to the backup decision being completed,
// not necessarily that files were copied.
//
// Updates all status fields atomically under write lock to ensure consistent
// state for status display. The next backup time calculation uses the current
// time rather than the effective last time to prevent scheduling drift.
//
// Thread safety: Uses write lock since this modifies multiple status fields.
func (bs *BackupStatus) updateBackupCompleted(configName string, scheduleMinutes int) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	
	now := time.Now()
	bs.lastBackupTimes[configName] = now
	bs.nextBackupTimes[configName] = now.Add(time.Duration(scheduleMinutes) * time.Minute)
	bs.scheduleMinutes[configName] = scheduleMinutes
	bs.configNames[configName] = configName
}

// initializeSchedule sets up initial status tracking for a backup configuration.
//
// Called during scheduler startup to establish initial status display values.
// This method mirrors the scheduler's hash-aware timing logic to ensure the
// status display matches actual scheduler behavior.
//
// The complex logic handles the same startup scenarios as the scheduler:
// - Existing backups with no hash data
// - Previous skipped backups with unchanged content
// - Previous skipped backups with changed content
// - First run with no previous state
//
// Thread safety: Uses write lock since this initializes multiple status fields.
func (bs *BackupStatus) initializeSchedule(config BackupConfig) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	
	now := time.Now()
	
	// Mirror scheduler logic: determine effective last action time
	lastBackupTime := bs.findLastBackupTime(config)
	var effectiveLastTime time.Time
	
	if config.IsHashCheckEnabled() {
		// Hash-aware status initialization
		lastActionType := hashManager.getLastActionType(config.Name)
		lastActionTime := hashManager.getLastActionTime(config.Name)
		
		if lastActionType == "skipped" && !lastActionTime.IsZero() {
			// Check if content changed since last skip
			shouldSkip, err := hashManager.shouldSkipBackup(config.Name, config.Source)
			if err != nil || !shouldSkip {
				// Hash check failed or content changed - use backup folder time
				effectiveLastTime = lastBackupTime
			} else {
				// Content unchanged since skip - use skip time
				effectiveLastTime = lastActionTime
			}
		} else {
			// Last action was backup or no hash data - use folder time
			effectiveLastTime = lastBackupTime
		}
	} else {
		// Hash checking disabled - simple folder-based timing
		effectiveLastTime = lastBackupTime
	}
	
	// Set initial status values based on effective last time
	if !effectiveLastTime.IsZero() {
		bs.lastBackupTimes[config.Name] = effectiveLastTime
		// Calculate next backup based on effective time + schedule interval
		bs.nextBackupTimes[config.Name] = effectiveLastTime.Add(time.Duration(config.ScheduleMinutes) * time.Minute)
	} else {
		// No previous state or content changed - next backup uses current time base
		bs.nextBackupTimes[config.Name] = now.Add(time.Duration(config.ScheduleMinutes) * time.Minute)
	}
	
	bs.scheduleMinutes[config.Name] = config.ScheduleMinutes
	bs.configNames[config.Name] = config.Name
}

// findLastBackupTime scans the destination directory for existing backup folders
// and returns the timestamp of the most recent backup for this configuration.
//
// This method provides fallback timing information when hash-based scheduling
// isn't available or fails. It's used by both the scheduler and status system
// to understand existing backup history.
//
// The scan only considers directories matching the backup naming pattern for
// this specific source folder, ensuring multiple backup configurations don't
// interfere with each other's timing calculations.
//
// Returns zero time if no backups exist or directory scan fails, which signals
// to callers that this is a first-run scenario.
func (bs *BackupStatus) findLastBackupTime(config BackupConfig) time.Time {
	entries, err := os.ReadDir(config.Destination)
	if err != nil {
		return time.Time{} // Directory doesn't exist or can't be read
	}
	
	// Filter to only backup directories for this source
	sourceFolderName := getSourceFolderName(config.Source)
	var mostRecentTime time.Time
	
	for _, entry := range entries {
		if entry.IsDir() && isBackupDirectory(entry.Name(), sourceFolderName) {
			// Parse timestamp from directory name
			if backupTime, err := parseBackupTimestamp(entry.Name(), sourceFolderName); err == nil && !backupTime.IsZero() {
				if backupTime.After(mostRecentTime) {
					mostRecentTime = backupTime
				}
			}
		}
	}
	
	return mostRecentTime
}

// getLastBackupStatus generates the "Last backup" status string for system tray display.
//
// Aggregates status across all backup configurations to show the most recently
// processed backup. The display includes:
// - Time since last action ("Just now", "N minutes ago")
// - Configuration name that was processed
// - Skip indicator [S] if last action was optimized away
//
// The skip indicator helps users understand when backups were intelligently
// skipped due to unchanged content, providing confidence that the system is
// working correctly even when no actual file copying occurred.
//
// Thread safety: Uses read lock for concurrent access during frequent UI updates.
func (bs *BackupStatus) getLastBackupStatus() string {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	
	if len(bs.lastBackupTimes) == 0 {
		return "Last: Never"
	}
	
	// Find most recent backup action across all configurations
	var mostRecent time.Time
	var mostRecentConfigName string
	
	for configName, lastTime := range bs.lastBackupTimes {
		if lastTime.After(mostRecent) {
			mostRecent = lastTime
			mostRecentConfigName = configName
		}
	}
	
	// Format time display with proper pluralization
	minutesAgo := int(math.Round(time.Since(mostRecent).Minutes()))
	if minutesAgo == 0 {
		// Recent action - check if it was skipped for optimization
		skipIndicator := ""
		if hashManager.getLastActionType(mostRecentConfigName) == "skipped" {
			skipIndicator = " [S]" // [S] indicates optimized skip
		}
		return fmt.Sprintf("Last: Just now (%s)%s", mostRecentConfigName, skipIndicator)
	}
	
	// Format with proper singular/plural minutes
	minuteWord := "minutes"
	if minutesAgo == 1 {
		minuteWord = "minute"
	}
	
	// Add skip indicator if applicable
	skipIndicator := ""
	if hashManager.getLastActionType(mostRecentConfigName) == "skipped" {
		skipIndicator = " [S]"
	}
	
	return fmt.Sprintf("Last: %d %s ago (%s)%s", minutesAgo, minuteWord, mostRecentConfigName, skipIndicator)
}

// getNextBackupStatus generates the "Next backup" status string for system tray display.
//
// Aggregates next backup times across all configurations to show which backup
// is due soonest. This gives users visibility into upcoming backup activity
// and helps them understand the backup schedule.
//
// The display format matches getLastBackupStatus for consistency:
// - Time until next backup ("N minutes", "Due now")  
// - Configuration name that will be processed next
// - Proper pluralization for professional appearance
//
// Returns "Next: Unknown" if no backup configurations are active, which should
// only occur during startup before schedulers initialize.
//
// Thread safety: Uses read lock for concurrent access during frequent UI updates.
func (bs *BackupStatus) getNextBackupStatus() string {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	
	if len(bs.nextBackupTimes) == 0 {
		return "Next: Unknown"
	}
	
	// Find earliest next backup time across all configurations
	var earliest time.Time
	var earliestConfigName string
	first := true
	
	for configName, nextTime := range bs.nextBackupTimes {
		if first || nextTime.Before(earliest) {
			earliest = nextTime
			earliestConfigName = configName
			first = false
		}
	}
	
	// Format countdown with proper pluralization
	minutesUntil := int(math.Round(time.Until(earliest).Minutes()))
	if minutesUntil <= 0 {
		return fmt.Sprintf("Next: Due now (%s)", earliestConfigName)
	}
	
	minuteWord := "minutes"
	if minutesUntil == 1 {
		minuteWord = "minute"
	}
	
	return fmt.Sprintf("Next: %d %s (%s)", minutesUntil, minuteWord, earliestConfigName)
}