//go:build !windows

package main

import (
	"fmt"
	"os"
)

type Mutex struct {
	lockFile *os.File
}

// Fallback to file-based locking on non-Windows platforms
func acquireMutex() (*Mutex, error) {
	lockFilePath := "SimpleFolderBackup.lock"
	
	lockFile, err := os.OpenFile(lockFilePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another instance is already running")
		}
		return nil, fmt.Errorf("failed to create lock file: %v", err)
	}
	
	// Write PID to lock file
	_, err = fmt.Fprintf(lockFile, "%d\n", os.Getpid())
	if err != nil {
		lockFile.Close()
		os.Remove(lockFilePath)
		return nil, fmt.Errorf("failed to write to lock file: %v", err)
	}
	
	return &Mutex{lockFile: lockFile}, nil
}

func (m *Mutex) release() {
	if m.lockFile != nil {
		m.lockFile.Close()
		os.Remove(m.lockFile.Name())
		m.lockFile = nil
	}
}