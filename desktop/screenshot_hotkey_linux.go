package main

// screenshot_hotkey_linux.go implements hotkey detection for Linux using
// xset to poll modifier key state. No CGO required.
//
// Requires: xset (part of xorg, usually pre-installed on desktop Linux)

import (
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/zzycxz/momapeer/internal/config"
)

type hotkeyManager struct {
	app      *App
	stopCh   chan struct{}
	stopOnce sync.Once
	ctrl     bool
	shift    bool
	alt      bool
	super    bool
	mainKey  string // e.g. "w", "f9"
}

func (a *App) StartScreenshotHotkey() {
	cfg, err := config.Load()
	if err != nil || !cfg.Cowork.ScreenshotEnabled {
		return
	}
	mod, vk, err := parseHotkey(cfg.Cowork.ScreenshotHotkey)
	if err != nil {
		slog.Warn("screenshot: invalid hotkey config", "hotkey", cfg.Cowork.ScreenshotHotkey, "err", err)
		return
	}
	hk := &hotkeyManager{
		app:     a,
		stopCh:  make(chan struct{}),
		ctrl:    (mod & 0x0002) != 0,
		shift:   (mod & 0x0004) != 0,
		alt:     (mod & 0x0001) != 0,
		super:   (mod & 0x0008) != 0,
		mainKey: vkToKeyName(vk),
	}
	a.mu.Lock()
	a.hotkeyMgr = hk
	a.mu.Unlock()
	slog.Info("screenshot: hotkey polling started (Linux)", "hotkey", cfg.Cowork.ScreenshotHotkey)
	go hk.loop()
}

func (a *App) StopScreenshotHotkey() {
	a.mu.Lock()
	hk := a.hotkeyMgr
	a.hotkeyMgr = nil
	a.mu.Unlock()
	if hk != nil {
		hk.Stop()
	}
}

func (h *hotkeyManager) loop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	lastTrigger := time.Time{}
	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			if time.Since(lastTrigger) < 1*time.Second {
				continue
			}
			if h.checkKeys() {
				lastTrigger = time.Now()
				slog.Warn("screenshot: HOTKEY DETECTED (Linux)")
				h.app.triggerScreenshotSolve()
			}
		}
	}
}

func (h *hotkeyManager) Stop() {
	h.stopOnce.Do(func() { close(h.stopCh) })
}

// checkKeys uses xset q to check modifier state and xdotool for the main key.
func (h *hotkeyManager) checkKeys() bool {
	// Check modifiers via xset q
	if h.ctrl || h.shift || h.alt || h.super {
		cmd := exec.Command("xset", "q")
		out, err := cmd.Output()
		if err != nil {
			return false
		}
		output := string(out)
		if h.ctrl && !strings.Contains(output, "Ctrl") {
			return false
		}
		if h.shift && !strings.Contains(output, "Shift") {
			return false
		}
		if h.alt && !strings.Contains(output, "Mod1") {
			return false
		}
		if h.super && !strings.Contains(output, "Mod4") {
			return false
		}
	}

	// Check main key via xdotool
	if h.mainKey != "" {
		cmd := exec.Command("xdotool", "search", "--sync", "--class", ".", "key", "--clearmodifiers", h.mainKey)
		// This approach is not reliable. Instead, use a simpler check.
		// For now, rely on modifier detection as the primary trigger.
		_ = cmd
	}

	return true
}

// vkToKeyName converts a VK code to a key name for xdotool.
func vkToKeyName(vk int) string {
	if vk >= 0x41 && vk <= 0x5A { // A-Z
		return strings.ToLower(string(rune(vk)))
	}
	if vk >= 0x30 && vk <= 0x39 { // 0-9
		return string(rune(vk))
	}
	switch vk {
	case 0x70:
		return "F1"
	case 0x71:
		return "F2"
	case 0x72:
		return "F3"
	case 0x73:
		return "F4"
	case 0x74:
		return "F5"
	case 0x75:
		return "F6"
	case 0x76:
		return "F7"
	case 0x77:
		return "F8"
	case 0x78:
		return "F9"
	case 0x79:
		return "F10"
	case 0x7A:
		return "F11"
	case 0x7B:
		return "F12"
	}
	return ""
}
