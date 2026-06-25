package builtinmcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zzycxz/momapeer/internal/config"
)

// TestWPSPPTEntryEmptyPath errors when no server path is given — guards the
// "user must configure wps_ppt_server_path" contract.
func TestWPSPPTEntryEmptyPath(t *testing.T) {
	_, err := WPSPPTEntry("", "")
	if err == nil {
		t.Fatal("WPSPPTEntry with empty path should error")
	}
}

// TestWPSPPTEntryMissingFile errors when the path doesn't exist, so a typo
// fails at config time rather than as a confusing launch error later.
func TestWPSPPTEntryMissingFile(t *testing.T) {
	_, err := WPSPPTEntry(filepath.Join(t.TempDir(), "nope.py"), "")
	if err == nil {
		t.Fatal("WPSPPTEntry with a nonexistent file should error")
	}
}

// TestWPSPPTEntryValid builds a real entry from a temp file and checks the
// fields. We don't assert the python command (PATH-dependent); just that the
// entry is well-formed and uses the background tier.
func TestWPSPPTEntryValid(t *testing.T) {
	dir := t.TempDir()
	serverPy := filepath.Join(dir, "server.py")
	if err := os.WriteFile(serverPy, []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry, err := WPSPPTEntry(serverPy, "")
	if err != nil {
		t.Fatalf("WPSPPTEntry valid: %v", err)
	}
	if entry.Name != WPSPPTName {
		t.Errorf("Name = %q, want %q", entry.Name, WPSPPTName)
	}
	if entry.Type != "stdio" {
		t.Errorf("Type = %q, want stdio", entry.Type)
	}
	if entry.Tier != "background" {
		t.Errorf("Tier = %q, want background", entry.Tier)
	}
	if len(entry.Args) != 1 || entry.Args[0] != serverPy {
		t.Errorf("Args = %v, want [%s]", entry.Args, serverPy)
	}
	if entry.Command == "" {
		t.Error("Command should be non-empty (python interpreter)")
	}
}

// TestWPSPPTEntryExplicitPython confirms a caller-supplied python exe is used
// verbatim (no PATH lookup), so a venv-specific interpreter is respected.
func TestWPSPPTEntryExplicitPython(t *testing.T) {
	dir := t.TempDir()
	serverPy := filepath.Join(dir, "server.py")
	os.WriteFile(serverPy, []byte("# test"), 0o644)
	entry, err := WPSPPTEntry(serverPy, "/custom/venv/python")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Command != "/custom/venv/python" {
		t.Errorf("Command = %q, want /custom/venv/python", entry.Command)
	}
}

// TestWPSPPTEntryDedup is a logic contract check: the caller (boot.go) is
// responsible for dedup against [[plugins]]; this just documents the entry shape
// a dedup would compare against. Kept as a regression guard for the Name field.
func TestWPSPPTEntryNameStable(t *testing.T) {
	if WPSPPTName != "wps-ppt" {
		t.Errorf("WPSPPTName = %q, want wps-ppt (tool prefix mcp__wps-ppt__*)", WPSPPTName)
	}
}

// silence unused import warning if config becomes unused in a future trim.
var _ = config.PluginEntry{}
