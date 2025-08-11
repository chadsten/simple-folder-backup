// Package main - scheduler.go implements intelligent backup scheduling with hash awareness.
//
// This module provides the timing intelligence that drives backup operations. Unlike simple
// interval-based scheduling, this scheduler integrates with the hash-based change detection
// system to make smart decisions about when backups should occur.
//
// Key architectural decisions:
//
// 1. Hash-aware scheduling: Considers both actual backups and content verification (skips)
//    when determining next backup time. This prevents scheduling drift when content
//    remains unchanged for extended periods.
//
// 2. Startup timing intelligence: Analyzes existing backups and hash history to determine
//    optimal first backup timing, avoiding unnecessary immediate backups after restarts.
//
// 3. Content change detection: When restarting after skipped backups, detects if content
//    has changed since last check and adjusts timing accordingly.
//
// 4. Graceful degradation: Falls back to folder modification times if hash system fails,
//    ensuring backups continue even without optimization benefits.
//
// This intelligent scheduling is essential for folder backups where content may
// remain unchanged for hours, making traditional fixed-interval scheduling wasteful.
package main

import (
	"context"
	"log"
	"time"
)

// startBackupScheduler runs the intelligent backup scheduling loop for a single backup configuration.
//
// This is the main scheduling intelligence that determines when backups should occur.
// The scheduler considers multiple factors:
// 1. Existing backup folders and their timestamps
// 2. Hash-based action history (skips vs actual backups)  
// 3. Content changes detected since last action
// 4. Configured scheduling intervals
//
// The complex startup logic is necessary because the scheduler must handle various scenarios:
// - First run with no backups
// - Restart after actual backups  
// - Restart after skipped backups with unchanged content
// - Restart after skipped backups with changed content
//
// Each backup configuration gets its own scheduler goroutine for fault isolation.
func startBackupScheduler(ctx context.Context, config BackupConfig, logger *log.Logger) {
	// Initialize status tracking for UI display
	backupStatus.initializeSchedule(config)
	logger.Printf("Started backup scheduler for %s (every %d minutes)", config.Name, config.ScheduleMinutes)
	
	// Define backup execution wrapper for consistent error handling and logging
	performBackupTask := func() {
		err := executeBackup(config, logger)
		if err != nil {
			logger.Printf("Backup failed for %s: %v", config.Name, err)
		} else {
			logger.Printf("Backup completed successfully for %s", config.Name)
		}
	}
	
	// Analyze existing state to determine optimal first backup timing
	lastBackupTime := backupStatus.findLastBackupTime(config)
	scheduleInterval := time.Duration(config.ScheduleMinutes) * time.Minute
	
	// Determine the effective "last action" time based on hash awareness
	var effectiveLastTime time.Time
	var timeDescription string
	
	if config.IsHashCheckEnabled() {
		// Hash checking enabled - use intelligent action-aware scheduling
		lastActionType := hashManager.getLastActionType(config.Name)
		lastActionTime := hashManager.getLastActionTime(config.Name)
		
		if lastActionType == "skipped" && !lastActionTime.IsZero() {
			// Last action was a skip - check if content has changed since then
			shouldSkip, err := hashManager.shouldSkipBackup(config.Name, config.Source)
			if err != nil {
				// Hash check failed - fall back to backup folder timing
				logger.Printf("Hash check failed for %s, using backup folder time: %v", config.Name, err)
				effectiveLastTime = lastBackupTime
				timeDescription = "backup folder"
			} else if shouldSkip {
				// Content still unchanged since last skip - use skip time for scheduling
				effectiveLastTime = lastActionTime
				timeDescription = "last skip"
			} else {
				// Content changed since last skip - run immediately
				effectiveLastTime = time.Time{}
				timeDescription = "content changed since skip"
			}
		} else {
			// Last action was backup or no hash data - use folder modification time
			effectiveLastTime = lastBackupTime
			timeDescription = "backup folder"
		}
	} else {
		// Hash checking disabled - use simple folder-based timing
		effectiveLastTime = lastBackupTime
		timeDescription = "backup folder"
	}
	
	// Calculate first backup delay based on effective last action time
	var firstBackupDelay time.Duration
	if effectiveLastTime.IsZero() {
		// No previous actions or content changed - run immediately
		firstBackupDelay = 0
		logger.Printf("No previous backups found for %s or %s, running immediately", config.Name, timeDescription)
	} else {
		timeSinceLastAction := time.Since(effectiveLastTime)
		if timeSinceLastAction >= scheduleInterval {
			// Overdue - run immediately
			firstBackupDelay = 0
			logger.Printf("Last action for %s (%s) was %v ago (overdue), running immediately", config.Name, timeDescription, timeSinceLastAction)
		} else {
			// Calculate remaining time until next scheduled backup
			firstBackupDelay = scheduleInterval - timeSinceLastAction
			logger.Printf("Last action for %s (%s) was %v ago, next backup in %v", config.Name, timeDescription, timeSinceLastAction, firstBackupDelay)
		}
	}
	
	// Execute first backup after calculated delay
	firstTimer := time.NewTimer(firstBackupDelay)
	defer firstTimer.Stop()
	
	select {
	case <-ctx.Done():
		logger.Printf("Backup scheduler stopped for %s before first backup", config.Name)
		return
	case <-firstTimer.C:
		performBackupTask()
	}
	
	// Start regular interval timer for subsequent backups
	ticker := time.NewTicker(scheduleInterval)
	defer ticker.Stop()
	
	// Main scheduling loop - continues until context cancellation
	for {
		select {
		case <-ctx.Done():
			logger.Printf("Backup scheduler stopped for %s", config.Name)
			return
		case <-ticker.C:
			performBackupTask()
		}
	}
}