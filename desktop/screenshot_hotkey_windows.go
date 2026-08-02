package main

// screenshot_hotkey_windows.go implements the global-hotkey screenshot solve
// feature: when the user presses the configured hotkey (default Ctrl+Shift+Alt+W)
// ANYWHERE on their desktop — even with MoMAPeer minimized — we:
//   1. Capture the full screen via the existing Win32 BitBlt screen capture.
//   2. Run a tool-calling mini-agent (web_search + web_fetch) on the image to
//      SOLVE whatever problem is on screen — it may search the web for fresh
//      info, then reports the answer + reasoning. When no search backend is
//      configured (or the agent errors) it degrades to a one-shot VLM solve.
//   3. Reply with the result via the IM bot gateway (feishu/QQ/WeChat) AND
//      surface an in-app toast so the user sees it without switching apps.
//
// Hotkey detection uses GetAsyncKeyState polling instead of RegisterHotKey +
// WM_HOTKEY messages. RegisterHotKey relies on the Windows message queue, which
// can be unreliable on some machines (e.g. bluetooth keyboards, ASUS ATK driver,
// or other software consuming WM_HOTKEY). GetAsyncKeyState reads the keyboard
// hardware state directly and works regardless of message queue issues.

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"log/slog"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zzycxz/momapeer/internal/agent"
	"github.com/zzycxz/momapeer/internal/boot"
	"github.com/zzycxz/momapeer/internal/config"
	"github.com/zzycxz/momapeer/internal/event"
	"github.com/zzycxz/momapeer/internal/netclient"
	"github.com/zzycxz/momapeer/internal/provider"
	"github.com/zzycxz/momapeer/internal/tool/builtin"
)

// Win32 modifier key VK codes for hotkey matching.
const (
	vkControl = 0x11
	vkShift   = 0x10
	vkAlt     = 0x12
	vkLWin    = 0x5B
	vkRWin    = 0x5C
)

// Win32 API for keyboard state polling.
var (
	user32DLL            = syscall.NewLazyDLL("user32.dll")
	procGetAsyncKeyState = user32DLL.NewProc("GetAsyncKeyState")
)

// hotkeyManager polls keyboard state via GetAsyncKeyState to detect the
// configured hotkey combination. No message queue, no hidden window.
type hotkeyManager struct {
	app      *App
	stopCh   chan struct{}
	stopOnce sync.Once
	// Parsed hotkey: the main key VK code + required modifier VK codes.
	mainVK uint16
	ctrl   bool
	shift  bool
	alt    bool
	win    bool
}

// StartScreenshotHotkey begins polling for the configured hotkey. Called from
// app startup when screenshot_enabled=true.
func (a *App) StartScreenshotHotkey() {
	cfg, err := config.Load()
	if err != nil || !cfg.Cowork.ScreenshotEnabled {
		return
	}
	hk, err := newHotkeyManager(a, cfg.Cowork.ScreenshotHotkey)
	if err != nil {
		slog.Warn("screenshot: invalid hotkey config", "hotkey", cfg.Cowork.ScreenshotHotkey, "err", err)
		return
	}
	a.mu.Lock()
	a.hotkeyMgr = hk
	a.mu.Unlock()

	slog.Info("screenshot: hotkey polling started", "hotkey", cfg.Cowork.ScreenshotHotkey)
	go hk.loop()
}

// StopScreenshotHotkey stops the polling loop.
func (a *App) StopScreenshotHotkey() {
	a.mu.Lock()
	hk := a.hotkeyMgr
	a.hotkeyMgr = nil
	a.mu.Unlock()
	if hk != nil {
		hk.Stop()
	}
}

// newHotkeyManager parses a hotkey string like "Ctrl+Shift+Alt+W" into a polling
// configuration.
func newHotkeyManager(app *App, hotkeyStr string) (*hotkeyManager, error) {
	mod, vk, err := parseHotkey(hotkeyStr)
	if err != nil {
		return nil, err
	}
	return &hotkeyManager{
		app:    app,
		stopCh: make(chan struct{}),
		mainVK: uint16(vk),
		ctrl:   (mod & 0x0002) != 0,
		shift:  (mod & 0x0004) != 0,
		alt:    (mod & 0x0001) != 0,
		win:    (mod & 0x0008) != 0,
	}, nil
}

// loop polls GetAsyncKeyState every 50ms to detect the hotkey combination.
// When all required keys are pressed simultaneously, it triggers the screenshot
// solve. A 500ms debounce prevents repeated triggers from key-repeat.
func (h *hotkeyManager) loop() {
	slog.Warn("screenshot: polling loop STARTED", "mainVK", fmt.Sprintf("0x%X", h.mainVK), "ctrl", h.ctrl, "shift", h.shift, "alt", h.alt)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	lastTrigger := time.Time{}
	wasPressed := false
	debugTick := time.NewTicker(5 * time.Second)
	defer debugTick.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			pressed := h.checkKeys()
			if pressed && !wasPressed && time.Since(lastTrigger) > 500*time.Millisecond {
				lastTrigger = time.Now()
				slog.Warn("screenshot: HOTKEY DETECTED via polling!")
				h.app.triggerScreenshotSolve()
			}
			wasPressed = pressed
		case <-debugTick.C:
			// Periodic heartbeat to confirm loop is alive.
			mainDown := isKeyDown(h.mainVK)
			ctrlDown := isKeyDown(vkControl)
			slog.Debug("screenshot: polling heartbeat", "main_key_down", mainDown, "ctrl_down", ctrlDown)
		}
	}
}

// checkKeys returns true if the configured hotkey combination is currently held.
func (h *hotkeyManager) checkKeys() bool {
	// Check main key (must be pressed).
	if !isKeyDown(h.mainVK) {
		return false
	}
	// Check required modifiers.
	if h.ctrl && !isKeyDown(vkControl) {
		return false
	}
	if h.shift && !isKeyDown(vkShift) {
		return false
	}
	if h.alt && !isKeyDown(vkAlt) {
		return false
	}
	if h.win && !isKeyDown(vkLWin) && !isKeyDown(vkRWin) {
		return false
	}
	return true
}

// isKeyDown checks if a virtual key is currently pressed via GetAsyncKeyState.
// Bit 15 (0x8000) indicates the key is physically pressed.
func isKeyDown(vk uint16) bool {
	ret, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return (ret & 0x8000) != 0
}

// Stop stops the polling loop. Idempotent.
func (h *hotkeyManager) Stop() {
	h.stopOnce.Do(func() {
		close(h.stopCh)
	})
}

// triggerScreenshotSolve is the unified entry point for screenshot solving.
// Called by both the global hotkey and the system tray menu item.
// It captures the screen, runs the VLM agent (with optional web search),
// and pushes the result via IM + toast.
func (a *App) triggerScreenshotSolve() {
	// Surface a "solving..." toast immediately so the user knows it fired.
	a.emitScreenshotNotice("正在解题中（可能联网搜索，请稍候）…", "")

	go func() {
		// Solving may run several search → read → reason rounds, so it needs a
		// longer budget than the old recognition call (60s).
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()

		// 1. Capture screen → PNG bytes.
		img, err := builtin.CaptureFullScreen()
		if err != nil || img == nil {
			a.emitScreenshotNotice("截图失败", "")
			return
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			a.emitScreenshotNotice("截图编码失败", "")
			return
		}
		dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

		// 2. Resolve the model + provider (shared by the agent path and the
		// one-shot fallback).
		cfg, err := config.Load()
		if err != nil {
			a.emitScreenshotNotice("配置读取失败", "")
			return
		}
		model := cfg.Cowork.ScreenshotVLMModel
		if model == "" {
			model = "qwen/qwen3.5-397b-a17b"
		}
		entry, err := resolveModelEntry(model)
		if err != nil {
			a.emitScreenshotNotice("模型未找到: "+err.Error(), "")
			return
		}
		prov, err := boot.NewProviderWithProxy(entry, netclient.ProxySpec{Mode: netclient.ModeAuto}, false, false)
		if err != nil {
			a.emitScreenshotNotice("模型初始化失败: "+err.Error(), "")
			return
		}

		// 3. Solve whatever problem is on screen. Prefers a tool-calling
		// mini-agent (web_search + web_fetch) when a search backend is keyed;
		// degrades to a one-shot VLM solve otherwise or on agent failure.
		prompt := cfg.Cowork.ScreenshotPrompt
		if strings.TrimSpace(prompt) == "" {
			prompt = defaultSolvePrompt
		}
		result, err := solveScreenshot(ctx, prov, entry, dataURL, prompt)
		if err != nil {
			a.emitScreenshotNotice("解题失败: "+err.Error(), "")
			return
		}

		// 4. Reply via IM bot (if configured) + toast.
		a.emitScreenshotNotice(result, "")
		// IM push is best-effort — the bot gateway may not be running yet. Pick
		// a real destination (a connected feishu/weixin conversation) instead of
		// a hard-coded "feishu:default", which never delivers.
		if gw := a.botGW.Load(); gw != nil {
			if dest := a.screenshotPushDest(); dest != "" {
				pushCtx, pushCancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := gw.Push(pushCtx, dest, "🧮 截图解题结果：\n\n"+result); err != nil {
					slog.Warn("screenshot: IM push failed", "dest", dest, "err", err)
				}
				pushCancel()
			} else {
				slog.Debug("screenshot: no connected feishu/weixin conversation to push to; skipping IM push")
			}
		}
	}()
}

// parseHotkey converts "Ctrl+Shift+Alt+W" → (MOD_CONTROL|MOD_SHIFT|MOD_ALT, VK_W).
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
			mod |= 0x0002
		case "shift":
			mod |= 0x0004
		case "alt":
			mod |= 0x0001
		case "win", "super", "meta":
			mod |= 0x0008
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

// defaultSolvePrompt is the default instruction sent with the screenshot image.
// Users can override this via [cowork].screenshot_prompt in config.toml.
const defaultSolvePrompt = "请从屏幕截图中找到用户当前遇到的问题或题目，然后逐步推理并给出答案。完成后自行验证答案是否正确，如不确定请联网搜索核实。最终给出：1）识别到的题目 2）答案 3）解题过程 4）验证结果。"

// defaultSolveSystemPrompt is the system prompt for the search mini-agent.
const defaultSolveSystemPrompt = "你是一个能看图的解题助手。首先从截图中准确识别出用户正在处理的题目或问题，然后逐步推理。遇到不确定的信息，主动使用联网搜索核实。给出答案后，用逆向推理或代入法验证答案的正确性。如果发现错误，自行纠正后再给出最终答案。"

// solveScreenshot solves whatever problem is on the captured screen. It runs a
// tool-calling mini-agent (web_search + web_fetch) when a search backend is
// configured, so the model can look up fresh information; otherwise — or if
// the agent loop errors out — it degrades to a single one-shot VLM call.
func solveScreenshot(ctx context.Context, prov provider.Provider, entry *config.ProviderEntry, imageDataURL string, prompt string) (string, error) {
	content := provider.ImageContent(prompt, imageDataURL)

	if !webSearchKeyConfigured() {
		return solveOneShot(ctx, prov, content)
	}
	reg := (&desktopExpertRunner{}).webSearchRegistry()
	if reg == nil {
		return solveOneShot(ctx, prov, content)
	}

	sess := agent.NewSession(defaultSolveSystemPrompt)
	opts := agent.Options{
		MaxSteps:      8,
		ContextWindow: entry.ContextWindow,
	}
	sink := event.FuncSink(func(e event.Event) { _ = e })
	sub := agent.New(prov, reg, sess, opts, sink)
	runErr := sub.Run(ctx, content)
	answer := lastAssistantText(sess)
	if runErr != nil {
		if isMaxStepsPaused(runErr) && answer != "" {
			return answer, nil
		}
		slog.Warn("screenshot: search mini-agent failed, falling back to one-shot", "err", runErr)
		return solveOneShot(ctx, prov, content)
	}
	if answer == "" {
		return solveOneShot(ctx, prov, content)
	}
	return answer, nil
}

// solveOneShot streams a single completion with the image+prompt content and
// no tools — the degrade path when search is unavailable or the agent failed.
func solveOneShot(ctx context.Context, prov provider.Provider, content any) (string, error) {
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: content},
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

// webSearchKeyConfigured reports whether at least one web_search backend has
// an API key set.
func webSearchKeyConfigured() bool {
	return os.Getenv("BRAVE_API_KEY") != "" ||
		os.Getenv("BRAVE_SEARCH_API_KEY") != "" ||
		os.Getenv("EXA_API_KEY") != "" ||
		os.Getenv("LINKUP_API_KEY") != ""
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
	wailsruntime.EventsEmit(a.ctx, "screenshot:notice", map[string]string{
		"message": message,
		"detail":  detail,
	})
}
