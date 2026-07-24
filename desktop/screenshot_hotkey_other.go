//go:build !windows

package main

// hotkeyManager is a no-op stub on non-Windows platforms. The real
// implementation lives in screenshot_hotkey_windows.go.
type hotkeyManager struct{}

// StartScreenshotHotkey is a no-op on non-Windows platforms (the global-hotkey
// feature uses Win32 RegisterHotKey). This stub keeps app.go compilable on
// macOS/Linux — the feature simply does nothing there.
func (a *App) StartScreenshotHotkey() {}

// StopScreenshotHotkey mirrors the no-op for symmetry with
// StartScreenshotHotkey so app.go's shutdown code is unconditional (it calls
// a.StopScreenshotHotkey() on every platform). Without this stub, non-Windows
// builds fail with "cannot find method StopScreenshotHotkey" — the estop
// sibling (estop_hotkey_other.go) already has both Start+Stop stubs; this was
// an oversight. See build-blocker audit finding B1.
func (a *App) StopScreenshotHotkey() {}
