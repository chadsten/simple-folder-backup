//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32      = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex = kernel32.NewProc("CreateMutexW")
	procCloseHandle = kernel32.NewProc("CloseHandle")
)

type Mutex struct {
	handle syscall.Handle
}

func acquireMutex() (*Mutex, error) {
	mutexName := "Global\\SimpleFolderBackup_SingleInstance"
	mutexNamePtr, err := syscall.UTF16PtrFromString(mutexName)
	if err != nil {
		return nil, fmt.Errorf("failed to convert mutex name: %v", err)
	}

	handle, _, err := procCreateMutex.Call(
		0, // lpMutexAttributes (default security)
		0, // bInitialOwner (false - don't initially own)
		uintptr(unsafe.Pointer(mutexNamePtr)), // lpName
	)

	if handle == 0 {
		return nil, fmt.Errorf("failed to create mutex: %v", err)
	}

	// Check if mutex already existed (ERROR_ALREADY_EXISTS = 183)
	if errno, ok := err.(syscall.Errno); ok && errno == 183 {
		// Close the handle and return error - another instance is running
		procCloseHandle.Call(handle)
		return nil, fmt.Errorf("another instance is already running")
	}

	return &Mutex{handle: syscall.Handle(handle)}, nil
}

func (m *Mutex) release() {
	if m.handle != 0 {
		procCloseHandle.Call(uintptr(m.handle))
		m.handle = 0
	}
}