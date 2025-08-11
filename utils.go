// Package main - utils.go provides shared utilities for backup directory operations.
//
// This module contains common utility functions used throughout the backup system
// for consistent handling of backup directory naming, timestamp parsing, and
// path operations.
//
// Key design decisions:
//
// 1. Consistent timestamp formats: Standardized formats for both backup directories
//    and log files ensure predictable parsing and sorting across the application.
//
// 2. Backup directory naming convention: Uses timestamp_sourcename pattern for
//    easy identification and chronological sorting while maintaining source context.
//
// 3. Timezone-aware parsing: All timestamp operations use local timezone to match
//    user expectations and filesystem timestamps.
//
// 4. Defensive parsing: Functions handle malformed directory names gracefully,
//    returning zero values that signal to callers that parsing failed.
//
// The utilities are essential for maintaining consistency in how backup directories
// are named, identified, and processed across different modules.
package main

import (
	"path/filepath"
	"time"
)

// Date format constants used throughout the application for consistency.
//
// BackupTimestampFormat: Used for backup directory names, includes time for uniqueness
// LogDateFormat: Used for daily log file names, date-only for daily rotation
//
// Both formats use Go's reference time (Mon Jan 2 15:04:05 MST 2006) which
// corresponds to Unix timestamp 1136239445.
const (
	BackupTimestampFormat = "02-01-2006_15-04-05" // DD-MM-YYYY_HH-MM-SS format
	LogDateFormat         = "02-01-2006"          // DD-MM-YYYY format for daily logs
)

// getSourceFolderName extracts the final directory name from a source path.
//
// Used to generate consistent backup directory names based on the source
// directory being backed up. For example, "/path/to/data" becomes "data".
//
// This function is critical for backup directory naming and identification
// since the source folder name is used as the suffix in backup directory names
// (e.g., "02-01-2006_15-04-05_data").
func getSourceFolderName(sourcePath string) string {
	return filepath.Base(sourcePath)
}

// isBackupDirectory checks if a directory name matches the backup naming pattern.
//
// Validates that a directory follows the expected backup naming convention:
// "DD-MM-YYYY_HH-MM-SS_sourcename" where sourcename matches the provided
// source folder name.
//
// The length check (20 characters) accounts for the minimum timestamp portion
// plus underscore separator. This prevents false positives from directory names
// that happen to end with the source folder name but aren't backup directories.
//
// Used during backup cleanup and status checking to identify relevant backup
// directories while ignoring other directories in the destination folder.
func isBackupDirectory(dirName, sourceFolderName string) bool {
	// Minimum length check: timestamp (19) + separator (1) + source name length
	minLength := len(sourceFolderName) + 20
	if len(dirName) < minLength {
		return false
	}
	
	// Check if directory name ends with the source folder name
	return dirName[len(dirName)-len(sourceFolderName):] == sourceFolderName
}

// parseBackupTimestamp extracts and parses the timestamp from a backup directory name.
//
// Given a backup directory name like "02-01-2006_15-04-05_data", extracts
// the timestamp portion "02-01-2006_15-04-05" and parses it into a time.Time.
//
// Uses ParseInLocation with time.Local to ensure timestamps are interpreted
// in the system's local timezone, which matches user expectations and is
// consistent with filesystem timestamps.
//
// Returns zero time and nil error for directories that don't match the backup
// pattern, allowing callers to distinguish between parsing errors and
// non-backup directories.
func parseBackupTimestamp(dirName, sourceFolderName string) (time.Time, error) {
	if !isBackupDirectory(dirName, sourceFolderName) {
		return time.Time{}, nil // Not a backup directory - return zero time
	}
	
	// Extract timestamp portion by removing source name suffix and separator
	timestampPart := dirName[:len(dirName)-len(sourceFolderName)-1]
	return time.ParseInLocation(BackupTimestampFormat, timestampPart, time.Local)
}

// generateBackupDirName creates a backup directory name using current timestamp.
//
// Combines the formatted timestamp with the source folder name to create
// backup directory names like "02-01-2006_15-04-05_data".
//
// The timestamp-first naming convention ensures backup directories sort
// chronologically when listed alphabetically, making it easy to identify
// the most recent backups.
//
// Used by the backup system when creating new backup directories to ensure
// consistent naming across all backup operations.
func generateBackupDirName(sourcePath string, timestamp time.Time) string {
	sourceFolderName := getSourceFolderName(sourcePath)
	return timestamp.Format(BackupTimestampFormat) + "_" + sourceFolderName
}