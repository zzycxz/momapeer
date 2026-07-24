//go:build windows

package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

// Element-level UI tree via EnumChildWindows. This extends get_ui_tree from
// window-level (EnumWindows) to control-level: each window's buttons, edit
// fields, static labels, and other child windows, each with its class name +
// bounding rect. It gives the VLM exact coordinates to target a specific control
// inside a window (e.g. the "Save" button at {x,y,w,h}) without guessing from a
// screenshot.
//
// Why EnumChildWindows (not full IUIAutomation COM): the COM interface is large
// (50+ vtable methods) and fragile to wire via go-ole's reflected dispatch.
// EnumChildWindows uses the same user32 syscalls the window tree already uses —
// zero new deps, zero COM init, and covers the dominant need (control rects).
// Accessibility properties (role/state) can layer on via UIA later behind the
// same tool; the rect+class+text surface is what desktop automation needs most.

var (
	procEnumChildWindows = user32.NewProc("EnumChildWindows")
	procGetWindowThread  = user32.NewProc("GetWindowThreadProcessId")
)

// ControlInfo is one child control of a window: class name, text, and rect.
type ControlInfo struct {
	Class string         `json:"class"`
	Text  string         `json:"text,omitempty"`
	Rect  map[string]int `json:"rect"`
}

// getUITreeEnhanced extends the window tree with optional child-control detail.
// When a window_title filter targets a single window, we also enumerate that
// window's children so the VLM gets control-level coordinates. Without a filter
// (listing all windows) we skip children to keep the output manageable.
type getUITreeEnhanced struct{}

func (getUITreeEnhanced) Name() string { return "get_ui_tree" }

func (getUITreeEnhanced) Description() string {
	return "Enumerate visible on-screen windows AND (when you pass title_prefix to target one) that window's child controls — buttons, edit fields, labels — each with its exact bounding rect. Returns JSON the VLM cross-references with a screenshot to find precise click coordinates. Use WITHOUT title_prefix to list all windows (no children); WITH title_prefix to get a specific window's controls (control-level rects so you can click a button by its center, not by eyeballing pixels). This is the precision complement to screenshot+image_understand."
}

func (getUITreeEnhanced) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "title_prefix":{"type":"string","description":"Filter to windows whose title starts with this (case-insensitive). When set, ALSO returns that window's child controls with their rects — use this to get control-level precision for a specific app."},
  "max_children":{"type":"integer","description":"Max child controls to list per window (default 80, to bound output on dense UIs)"}
},
"required":[]
}`)
}

func (getUITreeEnhanced) ReadOnly() bool { return true }

func (getUITreeEnhanced) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		TitlePrefix string `json:"title_prefix"`
		MaxChildren int    `json:"max_children"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
	}
	if p.MaxChildren <= 0 {
		p.MaxChildren = 80
	}

	wins, err := enumVisibleWindows()
	if err != nil {
		return "", err
	}
	prefix := strings.ToLower(strings.TrimSpace(p.TitlePrefix))

	type winOut struct {
		Title    string         `json:"title"`
		Class    string         `json:"class"`
		Rect     map[string]int `json:"rect"`
		Children []ControlInfo  `json:"children,omitempty"`
	}

	out := make([]winOut, 0, len(wins))
	for _, w := range wins {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(w.Title), prefix) {
			continue
		}
		entry := winOut{
			Title: w.Title,
			Class: w.Class,
			Rect: map[string]int{
				"x": int(w.R.Left), "y": int(w.R.Top),
				"w": int(w.R.Right - w.R.Left), "h": int(w.R.Bottom - w.R.Top),
			},
		}
		// Only enumerate children when targeting a window (prefix set) — listing
		// children for every window would produce a huge, mostly-useless dump.
		if prefix != "" {
			entry.Children = enumChildren(w.Hwnd, p.MaxChildren)
		}
		out = append(out, entry)
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	if len(out) == 0 {
		return fmt.Sprintf("no visible windows%s", prefixFilterNote(prefix)), nil
	}
	note := ""
	if prefix != "" {
		total := 0
		for _, w := range out {
			total += len(w.Children)
		}
		note = fmt.Sprintf(" (with %d child control(s))", total)
	}
	return fmt.Sprintf("%d visible window(s)%s%s:\n%s", len(out), prefixFilterNote(prefix), note, string(b)), nil
}

// enumChildren returns up to max child controls of hwnd via EnumChildWindows.
// Each child's class/text/rect is collected. Invisible or zero-size children
// are skipped (they'd just be noise).
func enumChildren(hwnd uintptr, max int) []ControlInfo {
	// Pre-allocate; EnumChildWindows callback appends. We bound at max via the
	// counter to avoid runaway on a dense UI.
	var children []ControlInfo
	count := 0
	cb := syscall.NewCallback(func(child uintptr, lparam uintptr) uintptr {
		if count >= max {
			return 0 // stop enumeration
		}
		// Skip invisible children.
		vis, _, _ := procIsWindowVisible.Call(child)
		if vis == 0 {
			return 1 // continue
		}
		var r rect
		ok, _, _ := procGetWindowRect.Call(child, uintptr(unsafe.Pointer(&r)))
		if ok == 0 || r.Right <= r.Left || r.Bottom <= r.Top {
			return 1 // zero/invalid size — skip
		}
		children = append(children, ControlInfo{
			Class: className(child),
			Text:  windowText(child),
			Rect: map[string]int{
				"x": int(r.Left), "y": int(r.Top),
				"w": int(r.Right - r.Left), "h": int(r.Bottom - r.Top),
			},
		})
		count++
		return 1
	})
	procEnumChildWindows.Call(hwnd, cb, 0)
	return children
}

// init supresses unused-symbol warnings if a proc is only referenced in paths
// the analyzer doesn't trace; harmless in normal builds.
var _ = procGetWindowThread
