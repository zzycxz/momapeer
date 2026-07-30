package main

// screenshot_hotkey_windows.go implements the global-hotkey screenshot-to-VLM
// feature: when the user presses the configured hotkey (default Ctrl+Shift+S)
// ANYWHERE on their desktop — even with MoMAPeer minimized — we:
//   1. Capture the full screen via the existing Win32 BitBlt screen capture.
//   2. Send the image to the configured VLM model (default qwen/qwen3.5-397b-a17b)
//      for recognition, via a one-shot provider call (rate-limited, background).
//   3. Reply with the result via the IM bot gateway (feishu/QQ/WeChat) AND
//      surface an in-app toast so the user sees it without switching apps.
//
// The global hotkey uses Win32 RegisterHotKey (user32.dll syscall), the same
// zero-CGO approach as our existing screen capture / mouse input. It registers
// a hotkey id on a hidden message-only window, then a goroutine pumps the
// Windows message loop to detect WM_HOTKEY and dispatch the capture.

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"log/slog"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zzycxz/momapeer/internal/boot"
	"github.com/zzycxz/momapeer/internal/config"
	"github.com/zzycxz/momapeer/internal/netclient"
	"github.com/zzycxz/momapeer/internal/provider"
	"github.com/zzycxz/momapeer/internal/tool/builtin"
)

const (
	hotkeyID   = 0x7A21 // arbitrary unique id for RegisterHotKey
	wmHotkey   = 0x0312
	modAlt     = 0x0001
	modControl = 0x0002
	modShift   = 0x0004
	modWin     = 0x0008
	vkS        = 0x53   // 'S'
	pmNoRemove = 0x0000 // PeekMessage: leave message in queue
	pmRemove   = 0x0001 // PeekMessage: remove after peek
)

var (
	user32DLL            = syscall.NewLazyDLL("user32.dll")
	procRegisterHotKey   = user32DLL.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32DLL.NewProc("UnregisterHotKey")
	procCreateWindowEx   = user32DLL.NewProc("CreateWindowExW")
	procDefWindowProc    = user32DLL.NewProc("DefWindowProcW")
	procDestroyWindow    = user32DLL.NewProc("DestroyWindow")
	procGetMessage       = user32DLL.NewProc("GetMessageW")
	procPeekMessage      = user32DLL.NewProc("PeekMessageW")
	procTranslateMessage = user32DLL.NewProc("TranslateMessage")
	procDispatchMessage  = user32DLL.NewProc("DispatchMessageW")
	procRegisterClassEx  = user32DLL.NewProc("RegisterClassExW")
)

// hotkeyManager owns the global hotkey registration + message loop.
type hotkeyManager struct {
	app      *App
	mu       sync.Mutex
	stopCh   chan struct{}
	stopOnce sync.Once
}

// StartScreenshotHotkey registers the global hotkey and begins pumping the
// message loop. Called from app startup when screenshot_enabled=true. If the
// hotkey is already registered by another app, RegisterHotKey fails and we log
// a warning (the user must pick a different combination).
func (a *App) StartScreenshotHotkey() {
	cfg, err := config.Load()
	if err != nil || !cfg.Cowork.ScreenshotEnabled {
		return
	}
	hk := &hotkeyManager{app: a, stopCh: make(chan struct{})}
	if err := hk.register(cfg.Cowork.ScreenshotHotkey); err != nil {
		slog.Warn("screenshot: hotkey registration failed (combination may be in use by another app, or unsupported); screenshot hotkey unavailable",
			"hotkey", cfg.Cowork.ScreenshotHotkey, "err", err)
		return
	}
	// Keep a handle so StopScreenshotHotkey can stop the loop on shutdown.
	// Without this the goroutine (and its GetMessage block) leaked, since the
	// manager was a local variable nothing could reach.
	a.mu.Lock()
	a.hotkeyMgr = hk
	a.mu.Unlock()
	go hk.loop()
}

// StopScreenshotHotkey tears down the hotkey + message loop at shutdown.
func (a *App) StopScreenshotHotkey() {
	a.mu.Lock()
	hk := a.hotkeyMgr
	a.hotkeyMgr = nil
	a.mu.Unlock()
	if hk != nil {
		hk.Stop()
	}
}

// register creates a hidden message-only window, registers the hotkey against
// it, and stores the manager for dispatch.
func (h *hotkeyManager) register(hotkeyStr string) error {
	hwnd := createMessageWindow()
	if hwnd == 0 {
		return fmt.Errorf("create message window failed")
	}
	mod, vk, err := parseHotkey(hotkeyStr)
	if err != nil {
		return err
	}
	r1, _, _ := procRegisterHotKey.Call(
		uintptr(hwnd),
		uintptr(hotkeyID),
		uintptr(mod),
		uintptr(vk),
	)
	if r1 == 0 {
		return fmt.Errorf("RegisterHotKey failed (combination may be in use)")
	}
	h.app.screenshotHwnd = hwnd
	return nil
}

// loop pumps the Windows message loop, dispatching WM_HOTKEY to onHotkey.
//
// It uses PeekMessage (non-blocking) instead of GetMessage: GetMessage blocks
// until a message arrives, which means the stopCh check above it was only
// reachable when no message was pending — once blocked, Stop() could never
// interrupt it and the goroutine leaked until process kill. PeekMessage returns
// immediately whether or not a message is present, so we drain any pending
// messages then sleep on a ticker + stopCh, making Stop() responsive.
func (h *hotkeyManager) loop() {
	msg := make([]byte, 48) // MSG struct
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		// Drain all currently-queued messages (non-blocking).
		for {
			ret, _, _ := procPeekMessage.Call(
				uintptr(unsafe.Pointer(&msg[0])),
				0, // all windows owned by this thread
				0, 0,
				uintptr(pmRemove),
			)
			if ret == 0 {
				break // no message available
			}
			msgID := *(*uint32)(unsafe.Pointer(&msg[4]))
			if msgID == wmHotkey {
				h.onHotkey()
			}
			_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
			_, _, _ = procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
		}
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
		}
	}
}

// onHotkey fires when the global hotkey is pressed: capture → VLM → IM + toast.
func (h *hotkeyManager) onHotkey() {
	// Surface a "recognizing..." toast immediately so the user knows it fired.
	h.app.emitScreenshotNotice("正在识别截图内容…", "")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// 1. Capture screen → PNG bytes.
		img, err := builtin.CaptureFullScreen()
		if err != nil || img == nil {
			h.app.emitScreenshotNotice("截图失败", "")
			return
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			h.app.emitScreenshotNotice("截图编码失败", "")
			return
		}
		b64 := base64.StdEncoding.EncodeToString(buf.Bytes())

		// 2. Send to VLM for recognition.
		cfg, err := config.Load()
		if err != nil {
			h.app.emitScreenshotNotice("配置读取失败", "")
			return
		}
		model := cfg.Cowork.ScreenshotVLMModel
		if model == "" {
			model = "qwen/qwen3.5-397b-a17b"
		}
		result, err := recognizeScreenshot(ctx, model, b64)
		if err != nil {
			h.app.emitScreenshotNotice("识别失败: "+err.Error(), "")
			return
		}

		// 3. Reply via IM bot (if configured) + toast.
		h.app.emitScreenshotNotice(result, "")
		// IM push is best-effort — the bot gateway may not be running yet. Pick
		// a real destination (a connected feishu/weixin conversation) instead of
		// a hard-coded "feishu:default", which never delivers.
		if gw := h.app.botGW.Load(); gw != nil {
			if dest := h.app.screenshotPushDest(); dest != "" {
				pushCtx, pushCancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := gw.Push(pushCtx, dest, "📸 截图识别结果：\n\n"+result); err != nil {
					slog.Warn("screenshot: IM push failed", "dest", dest, "err", err)
				}
				pushCancel()
			} else {
				slog.Debug("screenshot: no connected feishu/weixin conversation to push to; skipping IM push")
			}
		}
	}()
}

// Stop unregisters the hotkey and stops the message loop.
func (h *hotkeyManager) Stop() {
	h.stopOnce.Do(func() {
		close(h.stopCh)
		if h.app.screenshotHwnd != 0 {
			procUnregisterHotKey.Call(uintptr(h.app.screenshotHwnd), uintptr(hotkeyID))
			// Destroy the message-only window so repeated stop/start cycles don't
			// leak HWNDs (the OS reclaims them at exit, but a long-lived process
			// toggling the screenshot feature could accumulate many).
			procDestroyWindow.Call(uintptr(h.app.screenshotHwnd))
			h.app.screenshotHwnd = 0
		}
	})
}

// parseHotkey converts "Ctrl+Shift+S" → (MOD_CONTROL|MOD_SHIFT, VK_S).
func parseHotkey(s string) (mod, vk int, err error) {
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
			// Last part is the key itself.
			if vk != 0 {
				return 0, 0, fmt.Errorf("multiple keys in hotkey %q", s)
			}
			vk = keyToVK(p)
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

// keyToVK maps a single key name to its Windows virtual-key code.
func keyToVK(key string) int {
	if len(key) == 1 {
		c := key[0]
		if c >= 'A' && c <= 'Z' {
			return int(c) // VK_A..VK_Z == ASCII
		}
		if c >= 'a' && c <= 'z' {
			return int(c - 32)
		}
		if c >= '0' && c <= '9' {
			return int(c) // VK_0..VK_9 == ASCII
		}
	}
	switch strings.ToUpper(key) {
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
	case "SPACE":
		return 0x20
	case "ENTER":
		return 0x0D
	case "TAB":
		return 0x09
	}
	return 0
}

// createMessageWindow creates a hidden message-only window for receiving
// WM_HOTKEY. Uses CreateWindowEx with the "Message" window class.
func createMessageWindow() uintptr {
	className, _ := syscall.UTF16PtrFromString("MoMAPeerHotkey")
	var wc struct {
		Size       uint32
		Style      uint32
		WndProc    uintptr
		ClsExtra   int32
		WndExtra   int32
		Instance   uintptr
		Icon       uintptr
		Cursor     uintptr
		Background uintptr
		MenuName   *uint16
		ClassName  *uint16
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	wc.WndProc = syscall.NewCallback(defWindowProc)
	wc.ClassName = className
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	windowName, _ := syscall.UTF16PtrFromString("")
	hwnd, _, _ := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0, 0, 0, 0,
	)
	return hwnd
}

// defWindowProc is the default window procedure — just calls DefWindowProc.
func defWindowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	ret, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return ret
}

// recognizeScreenshot calls the VLM with a base64 image + "describe this" prompt.
func recognizeScreenshot(ctx context.Context, modelRef, imageB64 string) (string, error) {
	entry, err := resolveModelEntry(modelRef)
	if err != nil {
		return "", err
	}
	prov, err := boot.NewProviderWithProxy(entry, netclient.ProxySpec{Mode: netclient.ModeAuto}, false, false)
	if err != nil {
		return "", fmt.Errorf("build VLM provider: %w", err)
	}
	req := provider.Request{
		Messages: []provider.Message{
			{
				Role: provider.RoleUser,
				Content: []provider.ContentPart{
					{Type: "image_url", ImageURL: &provider.ImageURL{URL: "data:image/png;base64," + imageB64}},
					{Type: "text", Text: "请识别并描述这张截图的内容。如果是文字，请提取并整理；如果是图表，请描述关键信息；如果是界面，请说明是什么应用和操作。简洁回答。"},
				},
			},
		},
	}
	ch, err := prov.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for chunk := range ch {
		if chunk.Type == provider.ChunkText {
			b.WriteString(chunk.Text)
		}
		if chunk.Err != nil {
			return b.String(), chunk.Err
		}
	}
	return strings.TrimSpace(b.String()), nil
}

// resolveModelEntry finds the provider entry for a model ref like "qwen/qwen3.5-397b-a17b".
func resolveModelEntry(modelRef string) (*config.ProviderEntry, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	entry, ok := cfg.ResolveModel(modelRef)
	if !ok {
		return nil, fmt.Errorf("VLM model %q not found in config — add it under [[providers]]", modelRef)
	}
	return entry, nil
}

// emitScreenshotNotice pushes a toast to the frontend via event.
func (a *App) emitScreenshotNotice(message, detail string) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "screenshot:notice", map[string]string{
		"message": message,
		"detail":  detail,
	})
}
