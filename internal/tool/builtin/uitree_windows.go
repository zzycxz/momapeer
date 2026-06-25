//go:build windows

package builtin

import (
	"fmt"
	"syscall"
	"unsafe"
)

// get_ui_tree — the "precise location assist" for desktop automation. It
// enumerates on-screen windows (and, where reachable, their immediate children)
// with titles + bounding rectangles, giving the VLM exact coordinates to target
// instead of guessing from pixels. This complements screenshot: the VLM says
// "click the Notepad window", and get_ui_tree tells it Notepad's rect is at
// (120,80,800,600) so screen_click hits center reliably.
//
// Scope decision: full IUIAutomation COM (element-level tree: every textbox,
// button, list item) is powerful but fragile to hand-wire via go-ole's reflected
// dispatch (the interface has 50+ vtable methods, easy to get wrong). For Phase 2
// we ship the WINDOW-level tree via EnumWindows + GetWindowRect + GetWindowText,
// which covers the common need (which window is where) with zero COM risk.
// Element-level UIA can layer on later behind the same tool name when needed.

var (
	procGetWindowTextW   = user32.NewProc("GetWindowTextW")
	procGetWindowTextLen = user32.NewProc("GetWindowTextLengthW")
	procGetWindowRect    = user32.NewProc("GetWindowRect")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
	procGetClassNameW    = user32.NewProc("GetClassNameW")
)

type rect struct {
	Left, Top, Right, Bottom int32
}

// getUITreeEnhanced (defined in uiauto_windows.go) is the registered UI-tree
// tool — it extends the window tree here with child-control enumeration. This
// file now holds only the shared Win32 window-enumeration helpers; the tool
// type itself moved so the element-level logic lives next to its impl.

func prefixFilterNote(prefix string) string {
	if prefix == "" {
		return ""
	}
	return fmt.Sprintf(" matching %q", prefix)
}

type winInfo struct {
	Hwnd  uintptr
	Title string
	Class string
	R     rect
}

// enumVisibleWindows calls EnumWindows, keeping visible windows with a non-empty
// rect. The callback collects into a slice passed via the LPARAM (unsafe).
func enumVisibleWindows() ([]winInfo, error) {
	var collected []winInfo
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		// Skip invisible windows — they'd just be noise.
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1 // continue
		}
		var r rect
		ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		if ok == 0 {
			return 1
		}
		// Skip zero-size / off-screen rects.
		if r.Right <= r.Left || r.Bottom <= r.Top {
			return 1
		}
		collected = append(collected, winInfo{
			Hwnd:  hwnd,
			Title: windowText(hwnd),
			Class: className(hwnd),
			R:     r,
		})
		return 1
	})
	// EnumWindows is in x/sys/windows but takes uintptr callback; call via the
	// loaded user32 proc to keep one proc source.
	procEnumWindows := user32.NewProc("EnumWindows")
	r, _, err := procEnumWindows.Call(cb, 0)
	if r == 0 {
		return nil, fmt.Errorf("EnumWindows failed: %w", err)
	}
	return collected, nil
}

// windowText reads a window's title via GetWindowTextW.
func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLen.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

// className reads a window's class name via GetClassNameW.
func className(hwnd uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}
