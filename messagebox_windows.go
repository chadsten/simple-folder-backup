//go:build windows

// Package main - messagebox_windows.go implements Windows-specific message box display.
//
// This module provides native Windows message box functionality for displaying
// critical error messages to users, particularly during application startup
// when the system tray UI isn't yet available.
//
// Key design decisions:
//
// 1. Native Windows API integration: Uses user32.dll MessageBoxW for proper
//    Windows integration rather than console output that users might miss.
//
// 2. Unicode support: Uses MessageBoxW (wide character version) with UTF-16
//    conversion to properly display international characters in messages.
//
// 3. Warning icon: Uses MB_ICONWARNING to visually indicate important messages
//    that require user attention (like instance conflicts).
//
// 4. Modal dialog: Message box blocks until user acknowledges, ensuring they
//    see critical startup errors before the application exits.
//
// This Windows-specific implementation is essential for desktop applications
// where users need immediate feedback about startup issues without having to
// check console output or log files.
package main

import (
	"syscall"
	"unsafe"
)

// Windows API constants for MessageBoxW function
const (
	MB_OK          = 0x00000000 // OK button only
	MB_ICONWARNING = 0x00000030 // Warning icon display
)

// Lazy-loaded Windows API functions for runtime efficiency
var (
	user32          = syscall.NewLazyDLL("user32.dll")   // User interface API library
	procMessageBoxW = user32.NewProc("MessageBoxW")      // Unicode message box function
)

// showMessageBox displays a native Windows message box with warning icon.
//
// This is the Windows-specific implementation of the cross-platform message box
// interface. Uses the native Windows MessageBoxW API to display modal dialogs
// that integrate properly with the Windows desktop environment.
//
// The function converts Go strings to UTF-16 for proper Unicode support and
// uses unsafe pointers to interface with the Windows C API. The message box
// is modal and blocks until the user clicks OK.
//
// Primary use case: Displaying critical error messages during application
// startup, particularly the "another instance is running" warning.
func showMessageBox(title, message string) {
	// Convert Go strings to UTF-16 for Windows API
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	
	// Call Windows MessageBoxW API with warning icon
	procMessageBoxW.Call(
		0, // hWnd (no parent window - standalone dialog)
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(MB_OK|MB_ICONWARNING), // OK button with warning icon
	)
}