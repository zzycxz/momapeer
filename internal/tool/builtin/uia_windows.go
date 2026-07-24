//go:build windows

package builtin

import (
	"fmt"
	"sort"
	"strings"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// UIA element tree dump via IUIAutomation COM. This is the "precise structure"
// half of the UIA+VLM fusion: it returns real ControlType/Name/IsEnabled for
// every on-screen element, which the SoM layer uses to classify (A/N/K/U) and
// label elements for VLM selection. Mirrors Rooster's EliteEngine
// (src/utils/vision/engine.py) but in Go via go-ole's IDispatch.
//
// Why COM over EnumChildWindows: EnumChildWindows only gives class name + text
// (no semantic ControlType), and modern apps (WPF/Electron/Qt) use opaque class
// names. IUIAutomation gives the real accessibility role (Button/Edit/ComboBox)
// which is what VLM needs for meaningful labels. go-ole (already a dep) handles
// the COM dispatch; no CGO.

// UIAElement is one accessibility element with semantic info for SoM labeling.
type UIAElement struct {
	Name      string  `json:"name"`   // accessible name ("登录", "用户名")
	Type      string  `json:"type"`   // ControlType ("Button", "Edit", "ComboBox")
	Box       [4]int  `json:"box"`    // [x1,y1,x2,y2] physical pixels
	Center    [2]int  `json:"center"` // [cx,cy] physical pixels
	IsEnabled bool    `json:"is_enabled"`
	Hwnd      uintptr `json:"-"` // window handle (internal, not JSON-exposed)
}

// controlTypeIDs maps UIA ControlType int IDs to readable names. Values from
// the UIAutomationClient.h enum (UiaElementTypeIds).
var controlTypeIDs = map[int32]string{
	50000: "Button", 50001: "Calendar", 50002: "CheckBox", 50003: "ComboBox",
	50004: "Edit", 50005: "Hyperlink", 50006: "Image", 50007: "ListItem",
	50008: "List", 50009: "Menu", 50010: "MenuBar", 50011: "MenuItem",
	50012: "ProgressBar", 50013: "RadioButton", 50014: "ScrollBar",
	50015: "Slider", 50016: "Spinner", 50017: "StatusBar", 50018: "Tab",
	50019: "TabItem", 50020: "Text", 50021: "ToolBar", 50022: "ToolTip",
	50023: "Tree", 50024: "TreeItem", 50025: "Custom", 50026: "Group",
	50027: "Thumb", 50028: "DataGrid", 50029: "DataItem", 50030: "Document",
	50031: "SplitButton", 50032: "Window", 50033: "Pane", 50034: "Header",
	50035: "HeaderItem", 50036: "Table", 50037: "TitleBar", 50038: "Separator",
	50039: "SemanticZoom", 50040: "AppBar",
}

// containerTypes are elements that contain other elements (for suppression).
var containerTypes = map[string]bool{
	"Pane": true, "Group": true, "List": true, "Tab": true,
	"ToolBar": true, "Window": true, "Custom": true, "Document": true,
	"Table": true, "Tree": true, "MenuBar": true,
}

// dumpUIA returns all visible, enabled, on-screen elements from the foreground
// window's UIA tree. This is the Go equivalent of Rooster's EliteEngine.dump().
// It walks the IUIAutomation tree via go-ole COM dispatch, collecting elements
// with their ControlType/Name/rect, then deduplicates by 15px grid (same as
// Rooster) and filters out invisible/disabled/offscreen elements.
//
// Returns the elements + the count of "interactive" (non-container) elements in
// the foreground window, used by the quality assessment (RICH vs SPARSE).
func dumpUIA() (elements []UIAElement, fgInteractiveCount int, err error) {
	// Initialize COM for this goroutine. CoInitializeEx is idempotent; if already
	// initialized it returns S_FALSE (not an error). Must be paired with
	// CoUninitialize, but we skip CoUninitialize because the process lifetime
	// owns the COM apartment (calling it prematurely breaks later COM calls).
	ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	// Note: deliberately NOT calling CoUninitialize — the process keeps COM alive.

	// Create the IUIAutomation instance via CLSID.
	// CLSID_CUIAutomation = {FF48DBA4-60EF-4201-AA87-54103EEF594E}
	// IID_IUIAutomation   = {30CBE57D-D9D0-452A-AB13-7AC5AC4825EE}
	clsid, _ := ole.CLSIDFromString("{FF48DBA4-60EF-4201-AA87-54103EEF594E}")
	iid, _ := ole.IIDFromString("{30CBE57D-D9D0-452A-AB13-7AC5AC4825EE}")

	unknown, err := ole.CreateInstance(clsid, iid)
	if err != nil || unknown == nil {
		return nil, 0, fmt.Errorf("create IUIAutomation: %w", err)
	}
	defer unknown.Release()

	// QueryInterface for IDispatch (IUIAutomation supports IDispatch for late binding).
	auto, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return nil, 0, fmt.Errorf("query IDispatch: %w", err)
	}
	defer auto.Release()

	// Get the root (desktop) element.
	rootVar, err := oleutil.CallMethod(auto, "GetRootElement")
	if err != nil {
		return nil, 0, fmt.Errorf("GetRootElement: %w", err)
	}
	root := rootVar.ToIDispatch()
	defer root.Release()

	// Get the TreeWalker for control view (filters raw elements, keeps semantic).
	// ControlView walker gives us the element tree users "see" (buttons, edits),
	// not every invisible structuring node.
	walkerVar, err := oleutil.GetProperty(auto, "ControlViewWalker")
	if err != nil {
		// Fallback: use RawViewWalker (less filtering but always available).
		walkerVar, err = oleutil.GetProperty(auto, "RawViewWalker")
		if err != nil {
			return nil, 0, fmt.Errorf("get TreeWalker: %w", err)
		}
	}
	walker := walkerVar.ToIDispatch()
	defer walker.Release()

	// Find the foreground window element. We walk the root's children to find
	// the first Window, but for a full scan we collect from ALL top-level windows
	// that are visible. Rooster scans foreground + same-PID + taskbar in "low"
	// mode; we scan all visible top-level windows (simpler, covers multi-window).
	fgHwnd := getForegroundWindow()
	seen := map[string]bool{} // 15px grid dedup
	var allEls []UIAElement

	// Walk root's immediate children (top-level windows).
	childVar, err := oleutil.CallMethod(walker, "GetFirstChildElement", root)
	for childVar != nil && err == nil {
		child := childVar.ToIDispatch()

		// Check if this window is visible + on-screen.
		if isElementVisible(child) {
			els := walkElementTree(walker, child, fgHwnd, seen, 0)
			allEls = append(allEls, els...)
		}
		child.Release()

		// Get next sibling.
		nextVar, nextErr := oleutil.CallMethod(walker, "GetNextSiblingElement", child)
		childVar = nextVar
		err = nextErr
	}

	// Count interactive elements in the foreground window for quality assessment.
	for _, el := range allEls {
		if el.Hwnd == fgHwnd && !containerTypes[el.Type] && el.IsEnabled {
			fgInteractiveCount++
		}
	}

	return allEls, fgInteractiveCount, nil
}

// walkElementTree recursively walks the UIA tree under a root element, collecting
// elements. Depth-limited to 2500 per window (same as Rooster) to bound runtime
// on complex UIs. Each element is deduped by a 15px grid key.
func walkElementTree(walker *ole.IDispatch, el *ole.IDispatch, fgHwnd uintptr, seen map[string]bool, count int) []UIAElement {
	if count > 2500 {
		return nil
	}
	var results []UIAElement

	// Read this element's properties.
	if uiaEl, ok := readUIAElement(el); ok {
		// Only keep elements with a real rect (non-zero area) that are on-screen.
		if uiaEl.Box[2] > uiaEl.Box[0] && uiaEl.Box[3] > uiaEl.Box[1] {
			gridKey := fmt.Sprintf("%d_%d", uiaEl.Center[0]/15, uiaEl.Center[1]/15)
			if !seen[gridKey] {
				seen[gridKey] = true
				uiaEl.Hwnd = fgHwnd // tag with foreground for quality counting
				results = append(results, uiaEl)
			}
		}
	}

	// Recurse into children.
	childVar, err := oleutil.CallMethod(walker, "GetFirstChildElement", el)
	for childVar != nil && err == nil && count < 2500 {
		child := childVar.ToIDispatch()
		results = append(results, walkElementTree(walker, child, fgHwnd, seen, count+1)...)
		child.Release()
		nextVar, nextErr := oleutil.CallMethod(walker, "GetNextSiblingElement", child)
		childVar = nextVar
		err = nextErr
		count++
	}

	return results
}

// readUIAElement extracts Name/ControlType/rect/IsEnabled from an IUIAutomationElement.
func readUIAElement(el *ole.IDispatch) (UIAElement, bool) {
	var u UIAElement

	// Name.
	if v, err := oleutil.GetProperty(el, "CurrentName"); err == nil {
		u.Name = strings.TrimSpace(v.ToString())
	}

	// ControlType (int → string).
	if v, err := oleutil.GetProperty(el, "CurrentControlType"); err == nil {
		typeID := int32(v.Val)
		u.Type = controlTypeIDs[typeID]
		if u.Type == "" {
			u.Type = "Unknown"
		}
	}

	// BoundingRectangle (tagRECT: left, top, width, height).
	if v, err := oleutil.GetProperty(el, "CurrentBoundingRectangle"); err == nil {
		// The property returns a tagRECT as a packed int64 or a struct.
		// go-ole returns it as a VARIANT; we extract via .Val (int64) and unpack.
		rect := unpackRect(v.Val)
		u.Box = [4]int{rect[0], rect[1], rect[0] + rect[2], rect[1] + rect[3]}
		u.Center = [2]int{rect[0] + rect[2]/2, rect[1] + rect[3]/2}
	}

	// IsEnabled.
	if v, err := oleutil.GetProperty(el, "CurrentIsEnabled"); err == nil {
		u.IsEnabled = v.Val != 0
	}

	// Skip elements with no type and no name (pure structural noise).
	if u.Type == "" && u.Name == "" {
		return u, false
	}
	return u, true
}

// unpackRect unpacks a UIA tagRECT from a VARIANT. The tagRECT is returned as
// a pointer to a struct {left, top, width, height int32}. go-ole wraps it as
// an int64 Val or as a SAFEARRAY; we handle the common int64-ptr case.
func unpackRect(val int64) [4]int {
	// IUIAutomationElement.CurrentBoundingRectangle returns a tagRECT which
	// go-ole may marshal as a pointer. We try to interpret it as 4 int32s.
	// If val is a pointer, dereference it.
	if val == 0 {
		return [4]int{}
	}
	ptr := unsafe.Pointer(uintptr(val))
	return [4]int{
		int(*(*int32)(unsafe.Pointer(ptr))),
		int(*(*int32)(unsafe.Pointer(uintptr(val) + 4))),
		int(*(*int32)(unsafe.Pointer(uintptr(val) + 8))),
		int(*(*int32)(unsafe.Pointer(uintptr(val) + 12))),
	}
}

// isElementVisible checks if a UIA element's window is on-screen and not offscreen.
func isElementVisible(el *ole.IDispatch) bool {
	// CurrentIsOffscreen — true means the element is scrolled out / hidden.
	if v, err := oleutil.GetProperty(el, "CurrentIsOffscreen"); err == nil {
		if v.Val != 0 {
			return false
		}
	}
	return true
}

// getForegroundWindow returns the HWND of the current foreground window.
var procGetForegroundWindow = user32.NewProc("GetForegroundWindow")

func getForegroundWindow() uintptr {
	hwnd, _, _ := procGetForegroundWindow.Call()
	return hwnd
}

// sortUIAElementsByPosition sorts elements top-to-bottom, left-to-right (same
// ordering as Rooster's ID assignment: Y//20 buckets, then X).
func sortUIAElementsByPosition(els []UIAElement) {
	sort.SliceStable(els, func(i, j int) bool {
		bucketI := els[i].Center[1] / 20
		bucketJ := els[j].Center[1] / 20
		if bucketI != bucketJ {
			return bucketI < bucketJ
		}
		return els[i].Center[0] < els[j].Center[0]
	})
}
