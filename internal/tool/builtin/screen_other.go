//go:build !windows

package builtin

import "github.com/zzycxz/momapeer/internal/tool"

// ScreenTools returns the desktop-automation tools. On non-Windows platforms
// these are unavailable (the Win32 capture/input APIs don't exist), so we return
// nil — boot.go registers them only when non-empty, and the cowork tool list
// simply omits them on macOS/Linux. The cowork profile still works (browser +
// VLM + web), just without desktop control.
func ScreenTools() []tool.Tool { return nil }
