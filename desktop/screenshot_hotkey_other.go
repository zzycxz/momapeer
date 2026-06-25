//go:build !windows

package main

// StartScreenshotHotkey is a no-op on non-Windows platforms (the global-hotkey
// feature uses Win32 RegisterHotKey). This stub keeps app.go compilable on
// macOS/Linux — the feature simply does nothing there.
func (a *App) StartScreenshotHotkey() {}
