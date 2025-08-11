// Package main - deduplication.go implements hash-based content change detection.
//
// This module is the core of the backup optimization system. It provides intelligent
// content-aware backup skipping that dramatically reduces I/O and storage overhead
// when source directories haven't changed.
//
// Key architectural decisions:
//
// 1. Directory-level hashing using golang.org/x/mod/sumdb/dirhash:
//    - Cryptographically secure hash of entire directory tree
//    - Includes file contents, names, permissions, and directory structure
//    - Detects any change within the source directory tree
//    - Consistent across platforms and Go versions
//
// 2. Persistent state management:
//    - Stores hash history in JSON for persistence across application restarts
//    - Tracks both successful backup and skip actions for intelligent scheduling
//    - Thread-safe operations for concurrent backup configurations
//
// 3. Action-type tracking ("backup" vs "skipped"):
//    - Enables intelligent scheduling that considers when content was last checked
//    - Prevents backup scheduling drift when content remains unchanged
//    - Provides audit trail of backup decisions
//
// The hash-based approach is essential for folder backups because content
// often remains unchanged for extended periods, making full directory copying wasteful.
// Hash comparison is orders of magnitude faster than file I/O operations.
package main

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"golang.org/x/mod/sumdb/dirhash"
)

// HashStatus represents the stored state for a backup configuration's content tracking.
//
// This structure captures everything needed for intelligent backup decisions:
// - LastHash: Cryptographic hash of directory content for change detection
// - LastActionType: "backup" or "skipped" to distinguish action types
// - LastActionTime: When the action occurred for scheduling calculations
//
// The action type distinction is crucial because it enables the scheduler to
// make intelligent decisions about timing based on when content was last checked
// rather than just when backups were last performed.
type HashStatus struct {
	LastHash       string    `json:"lastHash"`       // Directory content hash
	LastActionType string    `json:"lastActionType"` // "backup" or "skipped"
	LastActionTime time.Time `json:"lastActionTime"` // When action occurred
}

// HashManager provides thread-safe management of hash-based backup state.
//
// The singleton pattern with global instance ensures consistent state management
// across all backup configurations. Thread safety is critical because multiple
// backup schedulers may be checking/updating hash state concurrently.
//
// Design decisions:
// - RWMutex allows concurrent reads while protecting writes
// - JSON persistence survives application restarts
// - Map keyed by config name supports multiple backup configurations
type HashManager struct {
	mu        sync.RWMutex          // Protects concurrent access to hash state
	hashes    map[string]HashStatus // Per-config hash tracking
	filePath  string                // Persistent storage location
}

// Global singleton instance ensures consistent hash state across all backup operations
var hashManager = &HashManager{
	hashes:   make(map[string]HashStatus),
	filePath: "hashes.json",
}

// loadFromFile initializes the hash manager state from persistent storage.
//
// This method is called once during application startup to restore hash state
// from the previous session. The graceful handling of missing files ensures
// the application works correctly on first run.
//
// Thread safety: Uses write lock since this modifies the internal hash map.
// Only called during initialization when no concurrent access is possible.
func (hm *HashManager) loadFromFile() error {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	// Missing hash file is normal on first run - start with empty state
	if _, err := os.Stat(hm.filePath); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(hm.filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &hm.hashes)
}

// saveToFile persists the current hash state to disk for recovery after restarts.
//
// Called after each backup decision (both backup and skip actions) to ensure
// state consistency. Uses pretty-printed JSON for debugging and manual inspection.
//
// Thread safety: Uses read lock since this only reads the hash map state.
// The JSON marshaling creates a copy, so concurrent modifications won't corrupt output.
func (hm *HashManager) saveToFile() error {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	data, err := json.MarshalIndent(hm.hashes, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(hm.filePath, data, 0644)
}

// calculateDirectoryHash computes a cryptographic hash of the entire directory tree.
//
// Uses golang.org/x/mod/sumdb/dirhash.HashDir with Hash1 algorithm, which provides:
// - SHA-256 based cryptographic security
// - Includes file contents, names, permissions, and directory structure  
// - Consistent results across platforms and Go versions
// - Efficient streaming computation without loading entire directory into memory
//
// The hash captures any change within the directory tree, making it perfect for
// detecting when files have been modified.
func (hm *HashManager) calculateDirectoryHash(dirPath string) (string, error) {
	return dirhash.HashDir(dirPath, "", dirhash.Hash1)
}

// shouldSkipBackup determines if a backup should be skipped based on content hash comparison.
//
// This is the core intelligence of the backup optimization system. The decision process:
// 1. Calculate current directory hash
// 2. Compare with stored hash for this configuration  
// 3. Return true if hashes match (content unchanged), false otherwise
//
// Missing hash state (first run or new config) always returns false to ensure
// initial backup occurs. Hash calculation failures also return false to prioritize
// data protection over performance optimization.
//
// Thread safety: Uses read lock for hash lookup since we only need to read state.
func (hm *HashManager) shouldSkipBackup(configName, sourcePath string) (bool, error) {
	currentHash, err := hm.calculateDirectoryHash(sourcePath)
	if err != nil {
		return false, err
	}

	hm.mu.RLock()
	lastStatus, exists := hm.hashes[configName]
	hm.mu.RUnlock()

	if !exists {
		return false, nil // No previous hash - must backup
	}

	return currentHash == lastStatus.LastHash, nil
}

// recordAction updates the hash state after a backup decision (backup or skip).
//
// This method is called after every backup decision to maintain accurate state:
// - After actual backup completion: records "backup" action with current hash
// - After skip decision: records "skipped" action with current hash
//
// The dual purpose serves both optimization (future skip decisions) and scheduling
// (intelligent timing based on when content was last checked vs backed up).
//
// Thread safety: Uses write lock since this modifies hash state, then persists
// to disk for recovery across application restarts.
func (hm *HashManager) recordAction(configName, sourcePath, actionType string) error {
	currentHash, err := hm.calculateDirectoryHash(sourcePath)
	if err != nil {
		return err
	}

	hm.mu.Lock()
	hm.hashes[configName] = HashStatus{
		LastHash:       currentHash,
		LastActionType: actionType,
		LastActionTime: time.Now(),
	}
	hm.mu.Unlock()

	return hm.saveToFile()
}

// getLastActionType returns the type of the last action taken for a backup configuration.
//
// Used by the scheduler and status display to make intelligent decisions about timing
// and to show users whether the last action was an actual backup or optimization skip.
//
// Returns empty string if no previous action exists (first run).
// Thread safety: Uses read lock for concurrent access.
func (hm *HashManager) getLastActionType(configName string) string {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	if status, exists := hm.hashes[configName]; exists {
		return status.LastActionType
	}
	return ""
}

// getLastActionTime returns when the last action occurred for a backup configuration.
//
// Critical for intelligent scheduling - the scheduler uses this to determine when
// content was last checked (either backed up or verified unchanged) rather than
// just when backups last occurred.
//
// Returns zero time if no previous action exists.
// Thread safety: Uses read lock for concurrent access.
func (hm *HashManager) getLastActionTime(configName string) time.Time {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	if status, exists := hm.hashes[configName]; exists {
		return status.LastActionTime
	}
	return time.Time{}
}

// initHashManager initializes the global hash manager from persistent storage.
//
// Called once during application startup to restore hash state from previous sessions.
// Loading failures are logged as warnings but don't prevent startup - the application
// can function without hash optimization, just less efficiently.
//
// This design prioritizes reliability over optimization: if hash loading fails,
// backups still work normally, they just won't benefit from change detection.
func initHashManager() {
	if err := hashManager.loadFromFile(); err != nil {
		log.Printf("Warning: Could not load hash file: %v", err)
	}
}