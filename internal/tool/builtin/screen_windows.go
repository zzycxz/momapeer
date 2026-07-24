//go:build windows

package builtin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/zzycxz/momapeer/internal/tool"

	"golang.org/x/sys/windows"
)

// Desktop automation tools (Phase 2 of coWork) — Windows-native implementation.
// These drive the user's actual desktop: screen capture (Win32 BitBlt), mouse
// and keyboard input (Win32 SendInput), for the screenshot→VLM→action loop that
// underpins desktop automation. Unlike browser automation (which has the DOM and
// accessibility tree), the desktop only exposes pixels + a UIA tree, so the VLM
// is the primary perception channel here, with get_ui_tree (separate file) as
// the precise-location assist.
//
// Why Windows-native instead of robotgo: the app is Windows-only in practice,
// and robotgo's CGO toolchain is fragile on Windows. These tools use only
// syscall into user32/gdi32 — no CGO, so the build never breaks on a missing C
// compiler. go-ole (already an indirect dep) handles the UIA COM calls.
//
// The Win32 procs are loaded once via NewLazySystemDLL (the codebase's existing
// pattern, see internal/sysproxy). Call signatures follow Microsoft's docs.

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	// GDI32 / USER32 — screen capture via BitBlt.
	procGetDC              = user32.NewProc("GetDC")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBM = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procGetDIBits          = gdi32.NewProc("GetDIBits")

	// USER32 — input synthesis via SendInput, plus screen geometry + cursor.
	procSendInput    = user32.NewProc("SendInput")
	procGetSystemSM  = user32.NewProc("GetSystemMetrics")
	procSetCursorPos = user32.NewProc("SetCursorPos")
)

// setDPIAware makes the process DPI-aware so screen coordinates and BitBlt
// captures operate in physical pixels (not virtualized logical pixels). Without
// this, GetSystemMetrics returns scaled dimensions and screenshots are blurry on
// high-DPI displays, and click coordinates drift. Mirrors Rooster's
// interaction.py:38-55 (shcore.SetProcessDpiAwareness → SetProcessDPIAware fallback).
//
// Called once at package init; safe to call multiple times (subsequent calls are
// no-ops). This affects the WHOLE process, so the Wails frontend's own DPI
// handling coexists (the frontend is a separate rendering concern).
func init() {
	// Try shcore.SetProcessDpiAwareness (Win 8.1+). Value 1 =
	// PROCESS_SYSTEM_DPI_AWARE — coordinates are in physical pixels.
	if shcore := windows.NewLazySystemDLL("shcore.dll"); shcore.Load() == nil {
		if proc := shcore.NewProc("SetProcessDpiAwareness"); proc.Find() == nil {
			if _, _, err := proc.Call(1); err == nil || err == windows.Errno(0) {
				return
			}
		}
	}
	// Fallback: user32.SetProcessDPIAware (Vista+).
	if proc := user32.NewProc("SetProcessDPIAware"); proc.Find() == nil {
		proc.Call()
	}
}

const (
	smCXScreen = 0 // GetSystemMetrics: screen width
	smCYScreen = 1 // GetSystemMetrics: screen height
	srccopy    = 0x00CC0020

	inputMouse    uint32 = 0
	inputKeyboard uint32 = 1

	mouseeventfLeftDown   uint32 = 0x0002
	mouseeventfLeftUp     uint32 = 0x0004
	mouseeventfRightDown  uint32 = 0x0008
	mouseeventfRightUp    uint32 = 0x0010
	mouseeventfMiddleDown uint32 = 0x0020
	mouseeventfMiddleUp   uint32 = 0x0040
	mouseeventfWheel      uint32 = 0x0800

	keyeventfKeyUp   uint32 = 0x0002
	keyeventfUnicode uint32 = 0x0004
)

// bitmapInfoHeader for GetDIBits pixel extraction (BGRA, top-down).
type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// ScreenTools returns the desktop-automation tools, for registration under the
// cowork profile. Windows-only; the non-Windows stub returns nil (see
// screen_other.go) so the cowork tool list simply omits them on macOS/Linux.
// Includes get_ui_tree (defined in uiauto_windows.go) which gives the VLM exact
// window + child-control coordinates to cross-reference with a screenshot.
func ScreenTools() []tool.Tool {
	return []tool.Tool{
		screenCapture{},
		screenClick{},
		screenType{},
		screenScroll{},
		screenKey{},
		getUITreeEnhanced{},
		screenPerceive{},
	}
}

// --- screenshot -------------------------------------------------------------

type screenCapture struct{}

func (screenCapture) Name() string { return "screenshot" }

func (screenCapture) Description() string {
	return "Capture the current screen (or a region) as a PNG and return its file path plus a base64 thumbnail. The image is ready to pass to image_understand for visual analysis, or use the path as an attachment. This is the primary perception channel for desktop automation — take a screenshot, have image_understand describe what's on screen, decide the next action. Optional region {x,y,w,h} captures a sub-rectangle; default is the full primary screen."
}

func (screenCapture) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "region":{"type":"object","description":"Optional sub-rectangle to capture. Omit for the full screen.","properties":{"x":{"type":"integer"},"y":{"type":"integer"},"w":{"type":"integer"},"h":{"type":"integer"}}}
},
"required":[]
}`)
}

func (screenCapture) ReadOnly() bool { return true }

func (screenCapture) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Region *struct {
			X int `json:"x"`
			Y int `json:"y"`
			W int `json:"w"`
			H int `json:"h"`
		} `json:"region"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
	}
	var rx, ry, rw, rh int
	hasRegion := p.Region != nil
	if hasRegion {
		rx, ry, rw, rh = p.Region.X, p.Region.Y, p.Region.W, p.Region.H
	}
	img, err := captureScreen(hasRegion, rx, ry, rw, rh)
	if err != nil {
		return "", err
	}
	dir := screenAttachmentsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create attachments dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("screen-%d.png", time.Now().Unix()))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("encode png: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("write screenshot: %w", err)
	}
	thumb := base64.StdEncoding.EncodeToString(buf.Bytes())
	if len(thumb) > 4096 {
		thumb = thumb[:4096] + "…"
	}
	return fmt.Sprintf("screenshot saved: %s (%dx%d)\nbase64 (first 4k): %s", path, img.Bounds().Dx(), img.Bounds().Dy(), thumb), nil
}

// --- screen_click -----------------------------------------------------------

type screenClick struct{}

func (screenClick) Name() string { return "screen_click" }

func (screenClick) Description() string {
	return "Click at screen coordinates (x, y). button is left (default)/right/middle; double sends a double-click. Coordinates are in physical screen pixels — get them from a screenshot analysis (image_understand) or get_ui_tree bounding boxes. Move + press + release is synthesized via SendInput, so it works on any window the cursor can reach."
}

func (screenClick) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "x":{"type":"integer","description":"Screen X coordinate (pixels)"},
  "y":{"type":"integer","description":"Screen Y coordinate (pixels)"},
  "button":{"type":"string","enum":["left","right","middle"],"description":"Mouse button (default left)"},
  "double":{"type":"boolean","description":"Double-click (default false)"}
},
"required":["x","y"]
}`)
}

func (screenClick) ReadOnly() bool { return false }

func (screenClick) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		X      int    `json:"x"`
		Y      int    `json:"y"`
		Button string `json:"button"`
		Double bool   `json:"double"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if err := moveMouse(p.X, p.Y); err != nil {
		return "", err
	}
	times := 1
	if p.Double {
		times = 2
	}
	for i := 0; i < times; i++ {
		if err := clickMouse(p.Button); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("clicked (%d, %d)%s%s", p.X, p.Y, buttonLabel(p.Button), doubleLabel(p.Double)), nil
}

// --- screen_type ------------------------------------------------------------

type screenType struct{}

func (screenType) Name() string { return "screen_type" }

func (screenType) Description() string {
	return "Type text at the current cursor focus via synthesized keyboard input. The target element must already have focus (click it first with screen_click). Uses SendInput with per-character unicode key synthesis (KEYEVENTF_UNICODE), so it works in any focused field — native apps, browsers, Electron apps — regardless of keyboard layout, including non-ASCII characters. Optional press_enter sends Enter after typing."
}

func (screenType) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "text":{"type":"string","description":"Text to type"},
  "press_enter":{"type":"boolean","description":"Press Enter after typing (default false)"}
},
"required":["text"]
}`)
}

func (screenType) ReadOnly() bool { return false }

func (screenType) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Text       string `json:"text"`
		PressEnter bool   `json:"press_enter"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if err := typeText(p.Text); err != nil {
		return "", err
	}
	if p.PressEnter {
		if err := pressKey(0x0D /* VK_RETURN */); err != nil {
			return "", err
		}
	}
	suffix := ""
	if p.PressEnter {
		suffix = " + Enter"
	}
	return fmt.Sprintf("typed %d chars%s", len(p.Text), suffix), nil
}

// --- screen_scroll ----------------------------------------------------------

type screenScroll struct{}

func (screenScroll) Name() string { return "screen_scroll" }

func (screenScroll) Description() string {
	return "Scroll the mouse wheel at (x, y). amount is in notches (one notch ≈ 120 units); positive scrolls down/forward, negative up/back. Move + wheel synthesized via SendInput. Use to reach content below the fold before re-screenshotting."
}

func (screenScroll) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "x":{"type":"integer"},
  "y":{"type":"integer"},
  "amount":{"type":"integer","description":"Wheel notches: positive = down/forward, negative = up/back (default 3)"}
},
"required":["x","y"]
}`)
}

func (screenScroll) ReadOnly() bool { return false }

func (screenScroll) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		X      int `json:"x"`
		Y      int `json:"y"`
		Amount int `json:"amount"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	amount := p.Amount
	if amount == 0 {
		amount = 3
	}
	if err := moveMouse(p.X, p.Y); err != nil {
		return "", err
	}
	if err := scrollWheel(amount); err != nil {
		return "", err
	}
	dir := "down"
	if amount < 0 {
		dir = "up"
	}
	return fmt.Sprintf("scrolled %s %d notches at (%d, %d)", dir, absInt(amount), p.X, p.Y), nil
}

// --- Win32 capture ----------------------------------------------------------

// CaptureFullScreen grabs the full primary screen as an image.RGBA. Exported
// wrapper around captureScreen for use by the desktop layer's screenshot-hotkey
// feature (which needs the raw pixels to PNG-encode for the VLM).
func CaptureFullScreen() (*image.RGBA, error) {
	return captureScreen(false, 0, 0, 0, 0)
}

// captureScreen grabs the full primary screen (or a region) via BitBlt into a
// Go RGBA image. Flow: GetDC(NULL) → compatible DC + bitmap → BitBlt → GetDIBits
// (BGRA pixels) → assemble image.RGBA. All GDI handles released.
func captureScreen(hasRegion bool, rx, ry, rw, rh int) (*image.RGBA, error) {
	screenW, err := systemMetrics(smCXScreen)
	if err != nil {
		return nil, err
	}
	screenH, err := systemMetrics(smCYScreen)
	if err != nil {
		return nil, err
	}
	x, y, w, h := 0, 0, screenW, screenH
	if hasRegion && rw > 0 && rh > 0 {
		x, y, w, h = rx, ry, rw, rh
	}

	hdc, _, callErr := procGetDC.Call(0)
	if hdc == 0 {
		return nil, fmt.Errorf("GetDC failed: %w", callErr)
	}
	defer procReleaseDC.Call(0, hdc)

	memDC, _, err := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed: %w", err)
	}
	defer procDeleteDC.Call(memDC)

	hBmp, _, err := procCreateCompatibleBM.Call(hdc, uintptr(w), uintptr(h))
	if hBmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed: %w", err)
	}
	defer procDeleteObject.Call(hBmp)

	oldObj, _, _ := procSelectObject.Call(memDC, hBmp)
	defer procSelectObject.Call(memDC, oldObj)

	ok, _, err := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), hdc, uintptr(x), uintptr(y), uintptr(srccopy))
	if ok == 0 {
		return nil, fmt.Errorf("BitBlt failed: %w", err)
	}

	// GetDIBits: top-down BGRA. Negative height → row 0 at top.
	bi := bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(w),
		Height:      int32(-h),
		Planes:      1,
		BitCount:    32,
		Compression: 0, // BI_RGB
	}
	buf := make([]byte, w*h*4)
	procGetDIBits.Call(memDC, hBmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0)

	// BGRA → RGBA.
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			off := (row*w + col) * 4
			b, g, r := buf[off], buf[off+1], buf[off+2]
			// image.RGBA expects [R,G,B,A].
			img.Pix[off] = r
			img.Pix[off+1] = g
			img.Pix[off+2] = b
			img.Pix[off+3] = 255
		}
	}
	return img, nil
}

// systemMetrics wraps GetSystemMetrics.
func systemMetrics(index int) (int, error) {
	v, _, err := procGetSystemSM.Call(uintptr(index))
	if v == 0 {
		return 0, fmt.Errorf("GetSystemMetrics(%d): %w", index, err)
	}
	return int(int32(v)), nil
}

// --- Win32 input ------------------------------------------------------------

// mouseInput matches Win32 MOUSEINPUT.
type mouseInput struct {
	Type      uint32
	DX        int32
	DY        int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	Extra     uintptr
}

// keyboardInput matches Win32 KEYBDINPUT.
type keyboardInput struct {
	Type  uint32
	Vk    uint16
	Scan  uint16
	Flags uint32
	Time  uint32
	Extra uintptr
}

// moveMouse moves the cursor to (x,y) via SetCursorPos (physical pixels), with
// ±1px random jitter to mimic human imprecision. Anti-bot behavioral detection
// flags pixel-perfect clicks as synthetic; the jitter is cheap insurance. Mirrors
// Rooster interaction.py:129-138.
func moveMouse(x, y int) error {
	x += randInt(-1, 1)
	y += randInt(-1, 1)
	r, _, err := procSetCursorPos.Call(uintptr(x), uintptr(y))
	if r == 0 {
		return fmt.Errorf("SetCursorPos(%d, %d): %w", x, y, err)
	}
	return nil
}

// clickMouse presses + releases a button via SendInput, with a randomized hold
// duration (40-90ms) to mimic human click timing. Fixed timing is a bot
// fingerprint; humans vary. Mirrors Rooster interaction.py:131-138.
func clickMouse(button string) error {
	var down, up uint32
	switch button {
	case "", "left":
		down, up = mouseeventfLeftDown, mouseeventfLeftUp
	case "right":
		down, up = mouseeventfRightDown, mouseeventfRightUp
	case "middle":
		down, up = mouseeventfMiddleDown, mouseeventfMiddleUp
	default:
		return fmt.Errorf("unknown button %q", button)
	}
	if err := sendMouseEvent(down); err != nil {
		return err
	}
	time.Sleep(time.Duration(40+randInt(0, 50)) * time.Millisecond)
	return sendMouseEvent(up)
}

// scrollWheel sends mouse-wheel delta. WHEEL_DELTA=120; amount notches → units.
func scrollWheel(amount int) error {
	const wheelDelta = 120
	mi := mouseInput{
		Type:      inputMouse,
		MouseData: uint32(int32(amount) * wheelDelta),
		Flags:     mouseeventfWheel,
	}
	return sendInput(inputMouse, unsafe.Pointer(&mi), int(unsafe.Sizeof(mi)))
}

func sendMouseEvent(flags uint32) error {
	mi := mouseInput{Type: inputMouse, Flags: flags}
	return sendInput(inputMouse, unsafe.Pointer(&mi), int(unsafe.Sizeof(mi)))
}

// typeText types text via SendInput. For >5 characters it uses the clipboard +
// Ctrl+V (faster, handles CJK/emoji reliably, avoids per-key timing issues);
// for short text it uses per-character Unicode key synthesis. Mirrors
// Rooster/UI-TARS-desktop's clipboard fallback for long input.
func typeText(text string) error {
	if utf8RuneCount(text) > 5 {
		return typeViaClipboard(text)
	}
	return typeViaUnicode(text)
}

func typeViaUnicode(text string) error {
	for _, r := range text {
		if err := typeRune(r); err != nil {
			return err
		}
		time.Sleep(8 * time.Millisecond)
	}
	return nil
}

func typeRune(r rune) error {
	ki := keyboardInput{Type: inputKeyboard, Scan: uint16(r), Flags: keyeventfUnicode}
	if err := sendInput(inputKeyboard, unsafe.Pointer(&ki), int(unsafe.Sizeof(ki))); err != nil {
		return err
	}
	ki.Flags = keyeventfUnicode | keyeventfKeyUp
	return sendInput(inputKeyboard, unsafe.Pointer(&ki), int(unsafe.Sizeof(ki)))
}

// typeViaClipboard writes text to the clipboard then sends Ctrl+V. Faster for
// long text and handles CJK/emoji that Unicode scan codes can't (BMP >0xFFFF).
// Mirrors Rooster interaction.py:234 + UI-TARS-desktop operator.ts:88-104.
func typeViaClipboard(text string) error {
	if err := setClipboardText(text); err != nil {
		// Clipboard failed — fall back to per-character (may lose emoji).
		return typeViaUnicode(text)
	}
	// Ctrl down → V down → V up → Ctrl up.
	if err := pressKeyCombo(0x11 /*VK_CONTROL*/, 0x56 /*VK_V*/); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	return nil
}

// pressKeyCombo presses two keys simultaneously (modifier + key), then releases.
func pressKeyCombo(modifier, key uint16) error {
	// Modifier down
	mi := keyboardInput{Type: inputKeyboard, Vk: modifier}
	if err := sendInput(inputKeyboard, unsafe.Pointer(&mi), int(unsafe.Sizeof(mi))); err != nil {
		return err
	}
	// Key down
	ki := keyboardInput{Type: inputKeyboard, Vk: key}
	if err := sendInput(inputKeyboard, unsafe.Pointer(&ki), int(unsafe.Sizeof(ki))); err != nil {
		return err
	}
	time.Sleep(30 * time.Millisecond)
	// Key up
	ki.Flags = keyeventfKeyUp
	if err := sendInput(inputKeyboard, unsafe.Pointer(&ki), int(unsafe.Sizeof(ki))); err != nil {
		return err
	}
	// Modifier up
	mi.Flags = keyeventfKeyUp
	return sendInput(inputKeyboard, unsafe.Pointer(&mi), int(unsafe.Sizeof(mi)))
}

// setClipboardText puts text on the Windows clipboard via user32 (OpenClipboard
// → EmptyClipboard → SetClipboardData(CF_UNICODETEXT) → CloseClipboard).
var (
	procOpenClipboard  = user32.NewProc("OpenClipboard")
	procEmptyClipboard = user32.NewProc("EmptyClipboard")
	procSetClipData    = user32.NewProc("SetClipboardData")
	procCloseClipboard = user32.NewProc("CloseClipboard")
	procGlobalAlloc    = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalAlloc")
	procGlobalLock     = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalLock")
	procGlobalUnlock   = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalUnlock")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

func setClipboardText(text string) error {
	// Convert to UTF-16 (Windows native).
	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	byteLen := len(utf16) * 2

	// Allocate global memory for the clipboard data.
	hMem, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(byteLen))
	if hMem == 0 {
		return fmt.Errorf("GlobalAlloc: %w", err)
	}
	ptr, _, err := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return fmt.Errorf("GlobalLock: %w", err)
	}
	// Copy UTF-16 bytes into the global memory.
	for i, v := range utf16 {
		*(*uint16)(unsafe.Pointer(ptr + uintptr(i*2))) = v
	}
	procGlobalUnlock.Call(hMem)

	// Set clipboard data.
	if r, _, err := procOpenClipboard.Call(0); r == 0 {
		return fmt.Errorf("OpenClipboard: %w", err)
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	r, _, _ := procSetClipData.Call(cfUnicodeText, hMem)
	if r == 0 {
		return fmt.Errorf("SetClipboardData failed")
	}
	return nil
}

// pressKey presses + releases a virtual-key code (Enter, Esc, etc.).
func pressKey(vk uint16) error {
	ki := keyboardInput{Type: inputKeyboard, Vk: vk}
	if err := sendInput(inputKeyboard, unsafe.Pointer(&ki), int(unsafe.Sizeof(ki))); err != nil {
		return err
	}
	time.Sleep(12 * time.Millisecond)
	ki.Flags = keyeventfKeyUp
	return sendInput(inputKeyboard, unsafe.Pointer(&ki), int(unsafe.Sizeof(ki)))
}

// sendInput wraps the Win32 SendInput call for a single INPUT record. The INPUT
// struct is { DWORD type; union{MOUSEINPUT;KEYBDINPUT;HARDWAREINPUT} }, 40 bytes
// on x64. We build it from the typed struct by copying its bytes into a 40-byte
// buffer and setting the type prefix.
func sendInput(inputType uint32, data unsafe.Pointer, size int) error {
	const inputSize = 40
	in := make([]byte, inputSize)
	copy(in, (*[40]byte)(data)[:size])
	*(*uint32)(unsafe.Pointer(&in[0])) = inputType
	sent, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in[0])), inputSize)
	if sent == 0 {
		return fmt.Errorf("SendInput failed: %w", err)
	}
	return nil
}

// --- message helpers --------------------------------------------------------

func buttonLabel(b string) string {
	if b == "" || b == "left" {
		return ""
	}
	return " " + b
}
func doubleLabel(d bool) string {
	if d {
		return " (double)"
	}
	return ""
}
func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// randInt returns a random int in [min, max]. Used for human-like jitter/timing.
func randInt(min, max int) int {
	if max <= min {
		return min
	}
	return min + rand.Intn(max-min+1)
}

// utf8RuneCount counts runes in a string (for the clipboard-vs-unicode decision).
func utf8RuneCount(s string) int {
	return len([]rune(s))
}
func screenAttachmentsDir() string {
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, ".momapeer", "attachments")
	}
	return filepath.Join(os.TempDir(), "momapeer-screen")
}

// screenKey implements the `screen_key` tool: send a keyboard shortcut (e.g.
// "ctrl+s", "alt+tab", "shift+enter") or a single key (e.g. "enter", "esc",
// "f5") to whatever window currently has keyboard focus. This is the save-PPT
// path (Ctrl+S), the close-dialog path (Esc), the confirm path (Enter) — the
// shortcuts screen_type cannot express (it types text, not modifiers).
type screenKey struct{}

func (screenKey) Name() string { return "screen_key" }

func (screenKey) Description() string {
	return "Send a keyboard shortcut or single key to the focused window. Use for actions screen_type can't do: Ctrl+S (save), Ctrl+A (select all), Ctrl+C/V (copy/paste), Enter (confirm), Esc (cancel/close dialog), Tab, arrow keys, F-keys. The keys string uses '+' to combine a modifier (ctrl/shift/alt) with a key: 'ctrl+s', 'alt+tab', 'shift+arrowleft'. Single keys: 'enter', 'esc', 'tab', 'f5', 'delete', 'backspace'. Keys go to whatever window has focus — call window_focus first to be sure."
}

func (screenKey) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"keys": {"type": "string", "description": "Key combination, e.g. \"ctrl+s\", \"alt+tab\", \"enter\", \"esc\". Modifiers: ctrl, shift, alt. Keys: a-z, 0-9, enter, esc, tab, space, delete, backspace, home, end, pageup, pagedown, arrowup/down/left/right, f1-f12."}
		},
		"required": ["keys"]
	}`)
}

func (screenKey) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Keys string `json:"keys"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	keys := strings.TrimSpace(in.Keys)
	if keys == "" {
		return "", fmt.Errorf("keys is required")
	}
	mod, key, err := parseKeyCombo(keys)
	if err != nil {
		return "", err
	}
	if mod != 0 {
		if err := pressKeyCombo(mod, key); err != nil {
			return "", fmt.Errorf("key combo %q failed: %w", keys, err)
		}
	} else {
		if err := pressKey(key); err != nil {
			return "", fmt.Errorf("key %q failed: %w", keys, err)
		}
	}
	time.Sleep(50 * time.Millisecond) // let the app react
	return fmt.Sprintf("Sent key %q.", keys), nil
}

func (screenKey) ReadOnly() bool { return false }

// parseKeyCombo parses a key combination string like "ctrl+shift+s" or "enter"
// into a Windows VK modifier code (0 if none) and a VK key code. Supported
// modifiers: ctrl (0x11), shift (0x10), alt (0x12). Supported keys: a-z,
// 0-9, enter/return, esc/escape, tab, space, delete/del, backspace, home, end,
// pageup, pagedown, arrowup/down/left/right, f1-f12.
func parseKeyCombo(s string) (mod, key uint16, err error) {
	// Normalize: replace dashes with plus signs so both ctrl+s and ctrl-s work.
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "-", "+")
	parts := strings.Split(normalized, "+")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return 0, 0, fmt.Errorf("empty key combo")
	}
	// All parts except the last are modifiers; the last is the key.
	for _, p := range parts[:len(parts)-1] {
		switch strings.TrimSpace(p) {
		case "ctrl", "control":
			mod |= 0x11
		case "shift":
			mod |= 0x10
		case "alt":
			mod |= 0x12
		default:
			return 0, 0, fmt.Errorf("unknown modifier %q (use ctrl, shift, or alt)", p)
		}
	}
	key, err = parseVK(strings.TrimSpace(parts[len(parts)-1]))
	if err != nil {
		return 0, 0, err
	}
	return mod, key, nil
}

// parseVK maps a key name to its Windows virtual-key code.
func parseVK(name string) (uint16, error) {
	if len(name) == 1 {
		c := name[0]
		if c >= 'a' && c <= 'z' {
			return uint16(c - 'a' + 0x41), nil // 'a'=0x41 ... 'z'=0x5A
		}
		if c >= '0' && c <= '9' {
			return uint16(c - '0' + 0x30), nil // '0'=0x30 ... '9'=0x39
		}
	}
	switch name {
	case "enter", "return":
		return 0x0D, nil
	case "esc", "escape":
		return 0x1B, nil
	case "tab":
		return 0x09, nil
	case "space":
		return 0x20, nil
	case "delete", "del":
		return 0x2E, nil
	case "backspace":
		return 0x08, nil
	case "home":
		return 0x24, nil
	case "end":
		return 0x23, nil
	case "pageup":
		return 0x21, nil
	case "pagedown":
		return 0x22, nil
	case "arrowup", "up":
		return 0x26, nil
	case "arrowdown", "down":
		return 0x28, nil
	case "arrowleft", "left":
		return 0x25, nil
	case "arrowright", "right":
		return 0x27, nil
	case "f1":
		return 0x70, nil
	case "f2":
		return 0x71, nil
	case "f3":
		return 0x72, nil
	case "f4":
		return 0x73, nil
	case "f5":
		return 0x74, nil
	case "f6":
		return 0x75, nil
	case "f7":
		return 0x76, nil
	case "f8":
		return 0x77, nil
	case "f9":
		return 0x78, nil
	case "f10":
		return 0x79, nil
	case "f11":
		return 0x7A, nil
	case "f12":
		return 0x7B, nil
	}
	return 0, fmt.Errorf("unknown key %q", name)
}
