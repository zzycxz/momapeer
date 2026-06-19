//go:build windows

package proc

import (
	"os"
	"os/exec"
	"path/filepath"
)

// ResolvePowerShell returns the full path to a usable PowerShell executable.
// It tries, in order:
//  1. powershell.exe found via PATH (exec.LookPath)
//  2. %SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe (standard
//     Windows location, works even when PATH is incomplete)
//  3. pwsh (PowerShell 7+ Core) found via PATH
//
// Returns "powershell" as a last resort so exec.Command still attempts the
// bare name — which at least produces a clear "not found" error.
func ResolvePowerShell() string {
	if p, err := exec.LookPath("powershell"); err == nil {
		return p
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		full := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, err := os.Stat(full); err == nil {
			return full
		}
	}
	// Also try windir, which some minimal environments set instead of SystemRoot.
	if root := os.Getenv("windir"); root != "" {
		full := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, err := os.Stat(full); err == nil {
			return full
		}
	}
	if p, err := exec.LookPath("pwsh"); err == nil {
		return p
	}
	return "powershell"
}
