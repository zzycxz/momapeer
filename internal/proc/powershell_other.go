//go:build !windows

package proc

// ResolvePowerShell is a no-op on non-Windows platforms. It exists so that
// shared code can call proc.ResolvePowerShell() without build-tag guards; the
// callers are themselves Windows-only at runtime.
func ResolvePowerShell() string { return "powershell" }
