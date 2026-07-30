package main

// estop_hotkey_windows.go implements the global EMERGENCY-STOP hotkey for coWork
// desktop automation. When the user presses the configured combo (default
// Ctrl+Shift+Pause/Break) ANYWHERE on their desktop — even with momapeer
// minimized and another app focused — we cancel the in-flight turn on the
// active tab. This is the safety baseline for screen_* tools, which perform
// IRREVERSIBLE physical actions (clicks, typing): once a wrong button is
// clicked or a destructive confirm pressed, you can't take it back, so the user
// needs an always-available "kill switch" that doesn't depend on momapeer
// having focus.
//
// Why a SECOND Win32 hotkey (not a frontend useGlobalShortcut): the value of an
// emergency stop is precisely that it works while the user is watching ANOTHER
// window (the app the agent is driving). Frontend keydown listeners die when
// momapeer loses focus, so a JS handler would be useless at the exact moment it
// matters. RegisterHotKey is system-global and fires regardless of focus. This
// mirrors the screenshot hotkey's design (screenshot_hotkey_windows.go); both
// are shown in the shortcuts cheatsheet as displayOnly entries.
//
// The hotkey reuses the screenshot hotkey's message-only window + message loop
// (a single hidden window can receive multiple hotkey ids), so we add a new
// hotkey id rather than a second window/pump. If the screenshot hotkey feature
// is off (no window exists yet), we create our own window + loop.

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/zzycxz/momapeer/internal/config"
)

const (
	// estopHotkeyID is a unique id distinct from the screenshot hotkey
	// (0x7A21) so RegisterHotKey dispatches to the right handler.
	estopHotkeyID = 0x7A22
	// vkPause is the virtual-key code for the Pause/Break key — a natural
	// "stop" key that almost no other app binds globally.
	vkPause = 0x13
)

// estopManager owns the emergency-stop hotkey registration + message loop. It
// is separate from hotkeyManager (screenshot) so the two features can be
// started/stopped independently, but it reuses the same Win32 procs and
// window-creation helpers.
type estopManager struct {
	app      *App
	hwnd     uintptr
	mu       sync.Mutex
	stopCh   chan struct{}
	stopOnce sync.Once
}

// estopMgr is the singleton held on App so Stop can reach it at shutdown.
// (estopManager) is the in-flight manager; nil when the feature is off/stopped.

// StartEStopHotkey registers the global emergency-stop hotkey and begins
// pumping its message loop. Called from app startup. If the combo is already
// taken by another app, RegisterHotKey fails and we log a warning (the user
// must pick a different combination). Failure is non-fatal: coWork still works,
// just without the kill switch.
func (a *App) StartEStopHotkey() {
	em := &estopManager{app: a, stopCh: make(chan struct{})}
	hotkeyStr := a.estopHotkeyString()
	if hotkeyStr == "" {
		return // feature disabled by empty config
	}
	if err := em.register(hotkeyStr); err != nil {
		// Non-fatal: log and move on. The user can reconfigure.
		slog.Warn("estop: hotkey registration failed (combination may be in use by another app); emergency stop unavailable",
			"hotkey", hotkeyStr, "err", err)
		return
	}
	a.mu.Lock()
	a.estopMgr = em
	a.mu.Unlock()
	go em.loop()
	slog.Info("estop: global emergency-stop hotkey registered", "hotkey", hotkeyStr)
}

// StopEStopHotkey tears down the hotkey + message loop at shutdown.
func (a *App) StopEStopHotkey() {
	a.mu.Lock()
	em := a.estopMgr
	a.estopMgr = nil
	a.mu.Unlock()
	if em != nil {
		em.Stop()
	}
}

// estopHotkeyString returns the configured combo, defaulting to Ctrl+Shift+Pause.
// Empty means disabled.
func (a *App) estopHotkeyString() string {
	cfg, err := config.Load()
	if err != nil {
		return defaultEStopHotkey
	}
	if cfg.Cowork.EStopHotkey == "off" || cfg.Cowork.EStopHotkey == "disabled" {
		return ""
	}
	if strings.TrimSpace(cfg.Cowork.EStopHotkey) == "" {
		return defaultEStopHotkey
	}
	return cfg.Cowork.EStopHotkey
}

const defaultEStopHotkey = "Ctrl+Shift+Pause"

// register creates a hidden message-only window (its own, so the feature works
// even if the screenshot hotkey is off and created no window) and registers the
// combo against it.
func (e *estopManager) register(hotkeyStr string) error {
	hwnd := createMessageWindow()
	if hwnd == 0 {
		return fmt.Errorf("create message window failed")
	}
	mod, vk, err := parseEStopHotkey(hotkeyStr)
	if err != nil {
		return err
	}
	r1, _, _ := procRegisterHotKey.Call(
		uintptr(hwnd),
		uintptr(estopHotkeyID),
		uintptr(mod),
		uintptr(vk),
	)
	if r1 == 0 {
		return fmt.Errorf("RegisterHotKey failed (combination may be in use)")
	}
	e.hwnd = hwnd
	e.app.estopHwnd = hwnd
	return nil
}

// loop pumps the Windows message loop, dispatching WM_HOTKEY to onHotkey. Uses
// non-blocking PeekMessage (same rationale as the screenshot hotkey: so Stop()
// can interrupt an idle pump rather than blocking forever in GetMessage).
func (e *estopManager) loop() {
	msg := make([]byte, 48) // MSG struct
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		// Drain all currently-queued messages (non-blocking).
		for {
			ret, _, _ := procPeekMessage.Call(
				uintptr(unsafe.Pointer(&msg[0])),
				0, 0, 0,
				uintptr(pmRemove),
			)
			if ret == 0 {
				break
			}
			msgID := *(*uint32)(unsafe.Pointer(&msg[4]))
			if msgID == wmHotkey {
				e.onHotkey()
			}
			_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
			_, _, _ = procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
		}
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
		}
	}
}

// onHotkey fires when the global emergency-stop hotkey is pressed: cancel the
// in-flight turn on the active tab and surface a red toast so the user gets
// immediate confirmation the stop landed. Cancel is a no-op if no turn is
// running, so an accidental press during idle is harmless.
func (e *estopManager) onHotkey() {
	// Cancel the active tab's in-flight turn. CancelTab is a no-op when nothing
	// is running, so stray presses don't error.
	e.app.CancelTab("")
	// Surface immediate confirmation via a frontend event → red toast. Emit
	// regardless of whether a turn was running: the user pressed STOP and
	// deserves a visible ack that the input registered.
	e.app.emitEStopNotice()
}

// Stop unregisters the hotkey and stops the message loop. Idempotent.
func (e *estopManager) Stop() {
	e.stopOnce.Do(func() {
		close(e.stopCh)
		if e.hwnd != 0 {
			procUnregisterHotKey.Call(uintptr(e.hwnd), uintptr(estopHotkeyID))
			// Destroy the message-only window to avoid leaking HWNDs across
			// repeated stop/start cycles of the estop feature.
			procDestroyWindow.Call(uintptr(e.hwnd))
			e.hwnd = 0
		}
	})
}

// parseEStopHotkey converts "Ctrl+Shift+Pause" → (MOD_CONTROL|MOD_SHIFT, VK_PAUSE).
// Reuses the modifier parsing from parseHotkey (screenshot) but adds the Pause
// key, which the screenshot parser doesn't know about.
func parseEStopHotkey(s string) (mod, vk int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, fmt.Errorf("empty hotkey")
	}
	parts := strings.Split(s, "+")
	vk = 0
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch strings.ToLower(p) {
		case "ctrl", "control":
			mod |= modControl
		case "shift":
			mod |= modShift
		case "alt":
			mod |= modAlt
		case "win", "super", "meta":
			mod |= modWin
		default:
			if vk != 0 {
				return 0, 0, fmt.Errorf("multiple keys in hotkey %q", s)
			}
			vk = estopKeyToVK(p)
			if vk == 0 {
				return 0, 0, fmt.Errorf("unknown key %q in hotkey", p)
			}
		}
	}
	if vk == 0 {
		return 0, 0, fmt.Errorf("no key in hotkey %q", s)
	}
	return mod, vk, nil
}

// estopKeyToVK maps a single key name to its Windows virtual-key code. Supports
// the full set the emergency-stop combo is likely to use (letters, digits,
// function keys, Pause/Break, Esc, Space, Enter).
func estopKeyToVK(key string) int {
	if len(key) == 1 {
		c := key[0]
		if c >= 'A' && c <= 'Z' {
			return int(c)
		}
		if c >= 'a' && c <= 'z' {
			return int(c - 32)
		}
		if c >= '0' && c <= '9' {
			return int(c)
		}
	}
	switch strings.ToUpper(key) {
	case "PAUSE", "BREAK":
		return vkPause
	case "ESC", "ESCAPE":
		return 0x1B
	case "SPACE":
		return 0x20
	case "ENTER":
		return 0x0D
	case "TAB":
		return 0x09
	case "F1":
		return 0x70
	case "F2":
		return 0x71
	case "F3":
		return 0x72
	case "F4":
		return 0x73
	case "F5":
		return 0x74
	case "F6":
		return 0x75
	case "F7":
		return 0x76
	case "F8":
		return 0x77
	case "F9":
		return 0x78
	case "F10":
		return 0x79
	case "F11":
		return 0x7A
	case "F12":
		return 0x7B
	}
	return 0
}

// emitEStopNotice pushes a stop-confirmation toast to the frontend. The frontend
// renders this as a prominent red banner so the user sees the kill landed.
func (a *App) emitEStopNotice() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "estop:fired", map[string]string{
		"message": "已紧急停止 AI 操作",
		"detail":  "全局热键触发的紧急停止已生效，进行中的任务被中断。",
	})
}

// ensure unused-import guard doesn't trip on syscall when only used transitively.
var _ = syscall.NewLazyDLL
