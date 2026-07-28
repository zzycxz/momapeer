//go:build windows

package builtin

import "testing"

// TestScreenToolsRoster guards the desktop-automation tool set. On Windows the
// roster includes all 7 tools; on other platforms ScreenTools returns nil (the
// tools don't exist) — the test asserts whichever applies so a dropped tool is
// caught regardless of build platform.
func TestScreenToolsRoster(t *testing.T) {
	tools := ScreenTools()
	// On Windows we expect the full set; the build tag selects screen_windows.go.
	// This file compiles on all platforms (no _windows suffix), so handle both.
	want := map[string]bool{
		"screenshot": true, "screen_click": true, "screen_type": true,
		"screen_scroll": true, "get_ui_tree": true, "screen_perceive": true,
		"screen_key": true,
	}
	seen := make(map[string]bool, len(tools))
	for _, tl := range tools {
		name := tl.Name()
		seen[name] = true
		if !want[name] {
			t.Errorf("unexpected screen tool %q", name)
		}
	}
	// On Windows every expected tool must be present. On non-Windows, ScreenTools
	// returns nil (no tools) — that's the documented stub, not a failure.
	if len(tools) > 0 {
		for name := range want {
			if !seen[name] {
				t.Errorf("missing screen tool %q", name)
			}
		}
	}
}

// TestScreenToolsReadOnlyClassification locks the read-only flags so a flip
// doesn't silently change batching behavior. screenshot + get_ui_tree are
// read-only (safe to parallelize); click/type/scroll mutate the desktop.
func TestScreenToolsReadOnlyClassification(t *testing.T) {
	readOnly := map[string]bool{"screenshot": true, "get_ui_tree": true, "screen_perceive": true}
	for _, tl := range ScreenTools() {
		got := tl.ReadOnly()
		if want := readOnly[tl.Name()]; got != want {
			t.Errorf("%s ReadOnly() = %v, want %v", tl.Name(), got, want)
		}
	}
}

// TestAbsInt covers the scroll-direction helper used by screen_scroll's message.
func TestAbsInt(t *testing.T) {
	cases := map[int]int{0: 0, 3: 3, -3: 3, 100: 100, -100: 100}
	for in, want := range cases {
		if got := absInt(in); got != want {
			t.Errorf("absInt(%d) = %d, want %d", in, got, want)
		}
	}
}
