//go:build !windows

package main

// StartEStopHotkey is a no-op on non-Windows platforms (the global emergency-
// stop hotkey uses Win32 RegisterHotKey, which doesn't exist on macOS/Linux).
// This stub keeps app.go compilable cross-platform — the feature simply does
// nothing on non-Windows. coWork still works there (browser + VLM + web), just
// without desktop control and therefore without a kill switch for it.
func (a *App) StartEStopHotkey() {}

// StopEStopHotkey mirrors the no-op for symmetry with StartEStopHotkey so
// shutdown code is unconditional.
func (a *App) StopEStopHotkey() {}
