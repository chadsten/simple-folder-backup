//go:build !windows

// Package main - messagebox_other.go implements cross-platform message box fallback.
//
// This module provides a simple console-based fallback for displaying error
// messages on non-Windows platforms where native message box APIs aren't
// available or differ significantly across platforms.
//
// Design rationale:
//
// 1. Cross-platform compatibility: While the Windows implementation uses native
//    API calls, this fallback ensures the application works on Linux, macOS,
//    and other platforms without requiring platform-specific GUI libraries.
//
// 2. Console output approach: Since most non-Windows deployments of backup
//    tools are likely to be headless or command-line environments, console output
//    is often more appropriate than attempting to display GUI dialogs.
//
// 3. Minimal dependencies: Avoids pulling in heavy GUI toolkits just for
//    occasional error message display, keeping the binary size small.
//
// 4. Development consistency: Provides the same showMessageBox interface
//    across all platforms, enabling consistent error handling code.
//
// In practice, most users will run this tool on Windows (primary desktop platform),
// but this fallback ensures it works correctly in mixed environments or for
// users who prefer alternative platforms.
package main

import "fmt"

// showMessageBox displays an error message via console output on non-Windows platforms.
//
// This is the fallback implementation for platforms that don't have the native
// Windows message box functionality. Simply prints the title and message to
// stdout in a consistent format.
//
// While less user-friendly than a modal dialog, this approach works in both
// GUI and headless environments, making it suitable for automated deployments
// or testing scenarios.
//
// The output format matches what users might expect from command-line tools,
// making it appropriate for non-Windows environments where console interaction
// is more common.
func showMessageBox(title, message string) {
	fmt.Printf("%s: %s\n", title, message)
}