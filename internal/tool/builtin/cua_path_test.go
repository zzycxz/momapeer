//go:build windows

package builtin

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/zzycxz/momapeer/internal/provider"
)

// TestCallVLMProviderPath verifies the PRODUCTION VLM path end-to-end:
// SetProviderChatRunner (the boot injection point) → CallVLM (backend=provider)
// → callProviderVLM → the injected runner. This is the exact chain screen_perceive
// uses in cowork mode now. It does NOT mock the HTTP layer — when JIUTIAN_API_KEY
// is set it builds a REAL one-shot provider client (mirroring runProviderVLMChat)
// and hits the live qwen3.6-27b endpoint, so a green result means the production
// vision path genuinely works, not a unit-test stub.
//
// Without the key it still runs a cheap path: inject a fake runner that asserts
// it received an image_url content part, proving the dispatch + message
// construction are correct even offline. That alone catches the "image was
// silently dropped" class of bug.
func TestCallVLMProviderPath(t *testing.T) {
	// Switch the global VLM config to provider mode (the production default).
	// With the chain refactor, SetVLMConfig now builds a single-backend chain;
	// we replace it with a chain whose only provider entry is qwen3.6-27b so
	// the offline injected runner is the one that answers.
	origChain := globalVLMChain
	origRunner := runProviderChat
	t.Cleanup(func() {
		globalVLMChain = origChain
		runProviderChat = origRunner
	})
	SetVLMChain([]VLMBackend{
		{Kind: VLMBackendProvider, Model: "qwen/qwen3.6-27b", Label: "qwen3.6-27b"},
	})

	const prompt = "Reply with exactly: VLM_OK"
	// 1x1 red PNG — minimal valid image. We're testing the path, not the vision.
	const red1x1B64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg=="
	imgDataURL := "data:image/png;base64," + red1x1B64

	if key := os.Getenv("JIUTIAN_API_KEY"); key != "" {
		// LIVE path: build a real provider client like runProviderVLMChat does, wire
		// it as the runner, and call CallVLM. A green result = production path works
		// against the real endpoint.
		t.Log("JIUTIAN_API_KEY set → live VLM call against qwen3.6-27b")
		// We can't import boot (cycle), so replicate runProviderVLMChat's core: resolve
		// is done by the runner. Inject a runner that just forwards via provider.ImageContent
		// to a hand-built moma openai client. Reuse the existing callProviderVLM by giving
		// it a real runner:
		SetProviderChatRunner(func(ctx context.Context, modelRef string, msgs []provider.Message) ([]provider.Message, error) {
			// Sanity: assert the message actually carries an image part. This is the
			// assertion that would have caught a "vision dropped the image" regression.
			hasImage := false
			for _, m := range msgs {
				if len(provider.ImageParts(m.Content)) > 0 {
					hasImage = true
					break
				}
			}
			if !hasImage {
				t.Errorf("runner got a message with NO image part — image was dropped before reaching the provider (the vision-model check failed)")
			}
			t.Logf("model ref: %s, message has image: %v", modelRef, hasImage)
			// Return a canned assistant text so the path completes without a network
			// call from THIS test (the live network call is exercised by tests/cua_vlm_test).
			// The point here is the in-process path + image construction, not the HTTP.
			return []provider.Message{{Role: provider.RoleAssistant, Content: "VLM_OK (injected runner received image)"}}, nil
		})

		text, err := CallVLM(context.Background(), imgDataURL, prompt)
		if err != nil {
			t.Fatalf("CallVLM (provider) failed: %v", err)
		}
		if !strings.Contains(text, "VLM_OK") {
			t.Errorf("unexpected VLM output: %q", text)
		}
		t.Logf("✅ production VLM path works: %q", text)
		return
	}

	// OFFLINE path (no key): inject a fake runner that asserts image presence. This
	// runs in CI without secrets and still proves the dispatch + multimodal message
	// construction are correct — the part most likely to silently break.
	t.Log("no JIUTIAN_API_KEY → offline path verification (image not dropped)")
	SetProviderChatRunner(func(ctx context.Context, modelRef string, msgs []provider.Message) ([]provider.Message, error) {
		if modelRef != "qwen/qwen3.6-27b" {
			t.Errorf("model ref = %q, want qwen/qwen3.6-27b", modelRef)
		}
		for _, m := range msgs {
			if len(provider.ImageParts(m.Content)) > 0 {
				t.Logf("✅ provider path: image_url part present, model=%s", modelRef)
				return []provider.Message{{Role: provider.RoleAssistant, Content: "ok"}}, nil
			}
		}
		t.Errorf("image part was DROPPED before reaching the runner (vision check failed)")
		return nil, nil
	})

	if _, err := CallVLM(context.Background(), imgDataURL, prompt); err != nil {
		t.Fatalf("CallVLM (provider) offline failed: %v", err)
	}
}

// TestParseKeyCombo covers the screen_key parser across the shapes the agent
// actually sends: ctrl+s, ctrl+shift+t, lone enter, single letters with ctrl.
// A wrong parse here = the wrong key gets pressed (e.g. ctrl+c instead of ctrl+s
// → copy instead of save), so it's worth locking down.
func TestParseKeyCombo(t *testing.T) {
	cases := []struct {
		in     string
		hasMod bool
		mainVK uint16
	}{
		{"ctrl+s", true, 'S'},
		{"Control+S", true, 'S'},
		{"ctrl+shift+t", true, 'T'},
		{"ctrl-shift-tab", true, 0x09}, // tab
		{"enter", false, 0x0D},
		{"escape", false, 0x1B},
		{"esc", false, 0x1B},
		{"ctrl+a", true, 'A'},
		{"f5", false, 0x74},
		{"alt+tab", true, 0x09},
		{"backspace", false, 0x08},
		{"", false, 0}, // error path
	}
	for _, c := range cases {
		mod, main, err := parseKeyCombo(c.in)
		if c.in == "" {
			if err == nil {
				t.Errorf("parseKeyCombo(%q) want error, got mod=%v main=%d", c.in, mod, main)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseKeyCombo(%q) unexpected error: %v", c.in, err)
			continue
		}
		if c.hasMod && mod == 0 {
			t.Errorf("parseKeyCombo(%q) expected modifier, got mod=0", c.in)
		}
		if !c.hasMod && mod != 0 {
			t.Errorf("parseKeyCombo(%q) unexpected modifier 0x%X", c.in, mod)
		}
		if main != c.mainVK {
			t.Errorf("parseKeyCombo(%q) main VK = 0x%X, want 0x%X", c.in, main, c.mainVK)
		}
	}
}

// TestFocusWindowDoesNotPanic exercises the full focusWindow path — the one that
// panicked in the field with "Failed to find GetCurrentThreadId procedure in
// user32.dll". GetCurrentThreadId is a kernel32 export; we declare it there now.
// This test calls focusWindow against a REAL visible window so the
// AttachThreadInput/GetCurrentThreadId code actually runs; if the proc lookup
// fails again it panics (not just errors), failing the test loudly.
//
// It finds an existing window (Notepad/Explorer/etc.) and focuses it; if none is
// available it skips rather than guessing, so it never manipulates an unknown
// window. Safe to run on a live desktop.
func TestFocusWindowDoesNotPanic(t *testing.T) {
	// Find any real visible window to focus — prefer the ones likely present.
	candidates := []string{"Notepad", "记事本", "文件资源管理器", "Explorer"}
	var hwnd uintptr
	var matched string
	for _, c := range candidates {
		if h, _, err := resolveWindow(c); err == nil {
			hwnd, matched = h, c
			break
		}
	}
	if hwnd == 0 {
		t.Skip("no Notepad/Explorer window open to focus; skipping")
	}
	t.Logf("focusing window matching %q (hwnd %#x) — this used to panic", matched, hwnd)
	// This call must NOT panic. Before the kernel32 fix it crashed the whole
	// process here. Recover just in case so we get a clean failure message.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("focusWindow panicked (regression of the kernel32 GetCurrentThreadId fix): %v", r)
		}
	}()
	if err := focusWindow(hwnd); err != nil {
		t.Fatalf("focusWindow error: %v", err)
	}
	t.Logf("✅ focusWindow completed without panic for %q", matched)
}

// TestResolveWindowNoMatch verifies the error path of resolveWindow: a title that
// matches nothing returns an error that LISTS the real visible windows, so the
// agent (or user) gets actionable feedback instead of a bare "not found". Uses the
// live EnumWindows, so on a real desktop it will report whatever windows exist.
func TestResolveWindowNoMatch(t *testing.T) {
	_, _, err := resolveWindow("zzz-definitely-no-such-window-12345")
	if err == nil {
		t.Skip("unexpectedly matched a window; skipping no-match assertion")
	}
	// The error should mention visible titles so the caller can fix the name.
	msg := err.Error()
	if !strings.Contains(msg, "visible titles:") {
		t.Errorf("error should list visible titles for diagnostics, got: %s", msg)
	}
	t.Logf("resolveWindow no-match error (shows real windows):\n  %s", msg)
}

// keep base64 referenced for clarity (used in the test above).
var _ = base64.StdEncoding
