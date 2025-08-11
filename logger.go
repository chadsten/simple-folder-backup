// Package main - logger.go implements per-backup logging with retention management.
//
// This module provides the logging infrastructure that gives users visibility into
// backup operations and enables troubleshooting when issues occur. The logging
// system is designed around several key principles:
//
// 1. Separate logs per backup configuration: Each backup config gets its own
//    log directory and files, preventing log entries from different backups
//    from interfering with troubleshooting.
//
// 2. Date-based log rotation: Daily log files with automatic cleanup based on
//    configurable retention periods, preventing unbounded log growth.
//
// 3. System vs operational logging separation: System-level events (startup,
//    configuration errors) go to system.log, while backup operations go to
//    per-config logs for easier debugging.
//
// 4. Retention management: Automatic cleanup of old log files prevents disk
//    space issues while preserving recent logs for troubleshooting.
//
// The logging design is critical for a long-running backup service where users
// need to verify operations and diagnose issues without manual inspection of
// every backup directory.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// LogDateFormat is defined in utils.go for consistency across modules

// LoggerConfig defines the configuration for creating a logger instance.
//
// This structure supports different logging behaviors:
// - Name: Descriptive name for error messages and debugging
// - Path: File system path where logs should be written
// - ClearOnStartup: Whether to truncate existing log file (for system.log)
// - RetentionDays: How many days of logs to keep (nil = no retention)
//
// The flexible design supports both system logging (cleared on startup,
// no retention) and per-backup logging (appended, with retention).
type LoggerConfig struct {
	Name           string // Descriptive name for error reporting
	Path           string // File path for log output
	ClearOnStartup bool   // Whether to clear existing log on startup
	RetentionDays  *int   // Days to retain logs (nil = no cleanup)
}

// createLogger creates a configured logger instance with directory setup and retention management.
//
// This is the core logger factory that handles all the complexity of setting up
// logging for different use cases (system vs per-backup). The function:
// 1. Creates necessary directory structure
// 2. Performs log retention cleanup if configured
// 3. Opens log file with appropriate flags (truncate vs append)
// 4. Returns configured logger with standard formatting
//
// Error handling strategy: Log retention cleanup failures are logged as warnings
// but don't prevent logger creation, ensuring backup operations can continue
// even if log maintenance fails.
//
// The logger format includes date, time, and source file for debugging.
func createLogger(config LoggerConfig) (*log.Logger, error) {
	// Create directory structure if needed
	if dir := filepath.Dir(config.Path); dir != "." {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return nil, err
		}
	}
	
	// Perform log retention cleanup before creating new logs
	if config.RetentionDays != nil {
		err := cleanupOldLogs(filepath.Dir(config.Path), *config.RetentionDays)
		if err != nil {
			// Non-fatal error - log cleanup failure shouldn't prevent backups
			fmt.Printf("Warning: Failed to cleanup old logs for %s: %v\n", config.Name, err)
		}
	}
	
	// Configure file opening behavior based on logger type
	openFlags := os.O_CREATE | os.O_WRONLY
	if config.ClearOnStartup {
		openFlags |= os.O_TRUNC // Clear previous session (system.log)
	} else {
		openFlags |= os.O_APPEND // Append to existing (per-backup logs)
	}
	
	logFile, err := os.OpenFile(config.Path, openFlags, 0666)
	if err != nil {
		return nil, err
	}
	
	// Create logger with consistent formatting: date, time, source file
	return log.New(logFile, "", log.Ldate|log.Ltime|log.Lshortfile), nil
}

// getTodayLogPath generates a log file path based on current date.
//
// Creates daily log files using the format "prefix_DD-MM-YYYY.log" within
// the specified base directory. This daily rotation makes it easy to find
// logs for specific dates and enables date-based retention cleanup.
//
// The date format matches the LogDateFormat constant defined in utils.go
// for consistency across the application.
func getTodayLogPath(baseDir, prefix string) string {
	fileName := fmt.Sprintf("%s_%s.log", prefix, time.Now().Format(LogDateFormat))
	return filepath.Join(baseDir, fileName)
}

// cleanupOldLogs removes log files older than the specified retention period.
//
// Scans the log directory for .log files and removes any whose embedded date
// is older than the retention cutoff. This prevents unbounded log growth over
// time while preserving recent logs for troubleshooting.
//
// The cleanup process:
// 1. Scans directory for .log files
// 2. Extracts dates from filenames using extractDateFromLogName
// 3. Removes files older than retention period
// 4. Logs warnings for deletion failures but continues processing
//
// Error handling: Individual file deletion failures are logged as warnings
// but don't abort the cleanup process, ensuring partial cleanup can succeed.
func cleanupOldLogs(logDir string, retentionDays int) error {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return err
	}
	
	// Calculate cutoff date for retention
	cutoffDate := time.Now().AddDate(0, 0, -retentionDays)
	
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".log") {
			continue // Skip non-log files
		}
		
		// Extract date from filename pattern (backup_DD-MM-YYYY.log)
		if dateStr := extractDateFromLogName(entry.Name()); dateStr != "" {
			if logDate, err := time.Parse(LogDateFormat, dateStr); err == nil {
				if logDate.Before(cutoffDate) {
					// Log file is older than retention period - delete it
					logPath := filepath.Join(logDir, entry.Name())
					err := os.Remove(logPath)
					if err != nil {
						// Log failure but continue with other files
						fmt.Printf("Warning: Failed to delete old log file %s: %v\n", logPath, err)
					}
				}
			}
		}
	}
	return nil
}

// extractDateFromLogName extracts the date portion from a log filename.
//
// Parses filenames following the pattern "prefix_DD-MM-YYYY.log" to extract
// the date string for retention processing. The regex matches the specific
// date format used by this application's log naming convention.
//
// Returns empty string if the filename doesn't match the expected pattern,
// which causes the file to be skipped during retention cleanup.
func extractDateFromLogName(filename string) string {
	// Extract "10-08-2025" from "backup_10-08-2025.log"
	re := regexp.MustCompile(`(\d{2}-\d{2}-\d{4})\.log$`)
	if matches := re.FindStringSubmatch(filename); len(matches) > 1 {
		return matches[1]
	}
	return "" // No matching date pattern found
}

// sanitizeConfigName converts a backup configuration name into a safe directory name.
//
// Transforms user-provided configuration names into filesystem-safe directory names
// by:
// 1. Converting to lowercase for consistency
// 2. Replacing spaces with hyphens for readability
// 3. Removing any characters that aren't alphanumeric or hyphens
//
// This ensures log directories can be created on any filesystem without issues
// while maintaining some readability of the original configuration name.
//
// Example: "Application Server" becomes "application-server"
func sanitizeConfigName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	// Remove any characters that aren't safe for directory names
	return regexp.MustCompile(`[^a-z0-9\-]`).ReplaceAllString(name, "")
}

// initSystemLogger creates the system-level logger for application events.
//
// The system logger captures application-level events like startup, configuration
// errors, and scheduler lifecycle events. It's distinguished from per-backup
// operational logs to make troubleshooting easier.
//
// Key characteristics:
// - Clears on each application startup for fresh session logs
// - No retention management (cleared each start, so no accumulation)
// - Single shared log for all system-level events
//
// This logger is used for Go's default log output, capturing events that
// aren't specific to individual backup operations.
func initSystemLogger() (*log.Logger, error) {
	config := LoggerConfig{
		Name:           "system",
		Path:           "logs/system.log",
		ClearOnStartup: true, // Fresh log each session
		RetentionDays:  nil,  // No retention needed (clears on startup)
	}
	return createLogger(config)
}

// initBackupLogger creates a dedicated logger for a specific backup configuration.
//
// Each backup configuration gets its own logger with:
// - Dedicated directory based on sanitized config name
// - Daily log files for easy date-based lookup
// - Append mode to preserve logs across application restarts
// - Configurable retention to prevent unbounded growth
//
// The per-backup logger isolation makes it much easier to troubleshoot issues
// with specific backup configurations without sifting through logs from other
// backups or system events.
//
// Log structure: logs/{sanitized-config-name}/backup_DD-MM-YYYY.log
func initBackupLogger(backupConfig BackupConfig) (*log.Logger, error) {
	// Create config-specific directory
	configDir := filepath.Join("logs", sanitizeConfigName(backupConfig.Name))
	logPath := getTodayLogPath(configDir, "backup")
	retentionDays := backupConfig.GetLogRetentionDays()
	
	config := LoggerConfig{
		Name:           backupConfig.Name,
		Path:           logPath,
		ClearOnStartup: false,        // Append to preserve history
		RetentionDays:  &retentionDays, // User-configurable retention
	}
	return createLogger(config)
}

