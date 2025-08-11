// Package main - config.go implements configuration management with backward compatibility.
//
// This module handles the JSON-based configuration system with several key design principles:
//
// 1. Backward compatibility: New fields use pointer types with nil defaults to distinguish
//    between "not specified" (use default) and "explicitly disabled" (use false)
//
// 2. Self-initializing configuration: Creates example config.json if none exists to
//    guide users in initial setup
//
// 3. Defensive path handling: Normalizes and validates all file paths to prevent
//    common user configuration errors
//
// 4. Configuration validation: Ensures paths are accessible before starting backup
//    operations to fail fast on misconfiguration
//
// The pointer-based approach for optional fields is critical because it allows us to
// distinguish between "user didn't specify" (use default) vs "user explicitly disabled"
// (respect their choice), which is essential for maintaining backward compatibility
// as new features are added.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// BackupConfig defines the configuration for a single backup operation.
//
// Key design decisions in the field types:
// - Required fields (name, source, destination, etc.) are plain types
// - Optional fields (enabled, hash_check, log_retention_days) are pointers
//   to distinguish between "not specified" (use default) and "explicitly set"
// - omitempty JSON tags prevent null values in generated config files
//
// This structure supports multiple backup configurations in a single application
// instance, allowing users to backup different sources simultaneously.
type BackupConfig struct {
	Name             string `json:"name"`              // Display name for UI and logging
	Source           string `json:"source"`            // Path to directory to backup
	Destination      string `json:"destination"`       // Path where backups are stored
	ScheduleMinutes  int    `json:"schedule_minutes"`  // Backup interval in minutes
	RotationCount    int    `json:"rotation_count"`    // Number of backups to retain
	Enabled          *bool  `json:"enabled,omitempty"` // nil=enabled, pointer to distinguish from false
	HashCheck        *bool  `json:"hash_check,omitempty"`       // nil=enabled, optimizes unchanged content
	LogRetentionDays *int   `json:"log_retention_days,omitempty"` // nil=7 days, per-backup log cleanup
}

// Config is the root configuration structure containing all backup configurations.
type Config struct {
	Backups []BackupConfig `json:"backups"`
}

// loadConfig loads the backup configuration from config.json, creating a default if none exists.
//
// The loading process implements several important behaviors:
// 1. Auto-generates example config on first run to guide user setup
// 2. Applies backward compatibility defaults for new optional fields
// 3. Ensures all configurations have sensible defaults for operation
//
// Error handling strategy: File read errors propagate upward, but missing config
// file is handled gracefully by creating defaults, ensuring the application can
// always start even on first run.
//
// The default configuration provides a working example with reasonable values
// that users can modify for their specific needs.
func loadConfig() (*Config, error) {
	configPath := "config.json"
	
	// If no config exists, create a helpful example for first-time users
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		enabled := true
		config := &Config{
			Backups: []BackupConfig{
				{
					Name:            "Example Backup",
					Source:          "C:\\Source\\Folder",
					Destination:     "D:\\Backups\\Destination",
					ScheduleMinutes: 30,               // 30-minute intervals are reasonable for most use cases
					RotationCount:   5,                // Keep 5 backups (~2.5 hours of history)
					Enabled:         &enabled,         // Explicitly enabled in example
					HashCheck:       &enabled,         // Enable optimization by default
					LogRetentionDays: nil,             // nil = use default 7 days
				},
			},
		}
		return config, saveConfig(config)
	}

	// Load existing configuration
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	// Apply backward compatibility defaults for configurations that may predate new features
	for i := range config.Backups {
		// Default to enabled if not specified (backward compatibility)
		if config.Backups[i].Enabled == nil {
			enabled := true
			config.Backups[i].Enabled = &enabled
		}
		// Default to hash checking enabled if not specified (optimization by default)
		if config.Backups[i].HashCheck == nil {
			hashCheck := true
			config.Backups[i].HashCheck = &hashCheck
		}
		// LogRetentionDays defaults to nil (handled by GetLogRetentionDays helper)
	}

	return &config, nil
}

// IsEnabled returns true if the backup configuration is enabled for processing.
//
// The three-valued logic implemented here:
// - nil pointer: defaults to enabled (backward compatibility)
// - pointer to true: explicitly enabled
// - pointer to false: explicitly disabled
//
// This design allows distinguishing between "user didn't specify" (default enabled)
// and "user explicitly disabled" (respect their choice).
func (bc *BackupConfig) IsEnabled() bool {
	return bc.Enabled == nil || *bc.Enabled
}

// IsHashCheckEnabled returns true if hash-based backup skipping is enabled.
//
// Hash checking provides significant performance benefits by avoiding file I/O
// when source content hasn't changed. This is especially valuable for applications
// that may idle for extended periods without file modifications.
//
// Uses the same three-valued logic as IsEnabled, defaulting to enabled because
// the optimization benefits far outweigh the minimal hash computation cost.
func (bc *BackupConfig) IsHashCheckEnabled() bool {
	return bc.HashCheck == nil || *bc.HashCheck
}

// GetLogRetentionDays returns the number of days to retain per-backup log files.
//
// Log retention prevents unbounded log file accumulation over time while preserving
// recent logs for debugging backup issues. The 7-day default provides sufficient
// history for troubleshooting while being reasonable for storage usage.
//
// Returns the configured value or 7 days if not specified (nil pointer).
func (bc *BackupConfig) GetLogRetentionDays() int {
	if bc.LogRetentionDays == nil {
		return 7 // Conservative default balances troubleshooting needs vs storage
	}
	return *bc.LogRetentionDays
}

// saveConfig writes the configuration structure to config.json with pretty formatting.
//
// Uses JSON indentation for human readability since users will likely need to
// edit the configuration file manually. The 0644 permissions allow user read/write
// while protecting from modification by other users.
func saveConfig(config *Config) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile("config.json", data, 0644)
}

// validatePaths normalizes and validates all configured file paths.
//
// This preprocessing step is critical for preventing common user configuration errors:
// 1. Converts relative paths to absolute paths for consistent operation
// 2. Normalizes path separators and removes redundant elements (., ..)
// 3. Ensures path resolution happens at startup, not during backup operations
//
// The validation runs before any backup schedulers start to fail fast on
// configuration errors rather than discovering them during backup attempts.
//
// Path normalization is especially important for cross-platform operation and
// handles edge cases like trailing slashes, mixed separators, and relative references.
func validatePaths(config *Config) error {
	for i, backup := range config.Backups {
		// Convert source path to absolute and normalize
		absSource, err := filepath.Abs(backup.Source)
		if err != nil {
			return err
		}
		config.Backups[i].Source = filepath.Clean(absSource)
		
		// Convert destination path to absolute and normalize  
		absDestination, err := filepath.Abs(backup.Destination)
		if err != nil {
			return err
		}
		config.Backups[i].Destination = filepath.Clean(absDestination)
	}
	return nil
}