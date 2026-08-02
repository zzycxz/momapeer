//go:build !windows && !darwin && !linux

package main

// screenshot_hotkey_other.go is the fallback stub for platforms that aren't
// Windows, macOS, or Linux. Hotkey detection is not supported; use the tray
// menu to trigger screenshot solving.

type hotkeyManager struct{}

func (a *App) StartScreenshotHotkey() {}
func (a *App) StopScreenshotHotkey()  {}
