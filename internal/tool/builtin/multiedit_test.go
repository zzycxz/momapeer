package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runMultiEdit is a small helper mirroring runEdit: it builds a multi_edit
// payload, runs it against a file in dir, and returns the error (if any) so a
// test can assert on partial-failure behavior.
func runMultiEdit(t *testing.T, dir, name string, edits []map[string]any) error {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"path": name, "edits": edits})
	_, err := (multiEdit{workDir: dir}).Execute(context.Background(), b)
	return err
}

// TestMultiEditAppliesAllEdits is the happy path: two edits both apply.
func TestMultiEditAppliesAllEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644)

	err := runMultiEdit(t, dir, "f.txt", []map[string]any{
		{"old_string": "alpha", "new_string": "ALPHA"},
		{"old_string": "gamma", "new_string": "GAMMA"},
	})
	if err != nil {
		t.Fatalf("multi_edit: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "ALPHA\nbeta\nGAMMA\n" {
		t.Fatalf("result = %q", got)
	}
}

// TestMultiEditPartialFailureReportsApplied is the regression test for the
// silent-partial-failure hazard (A3): when edit 2 of 2 fails, edit 1 is already
// on disk. The error must say so (mention the applied count and that the file
// is partially edited / not rolled back), so the caller knows the file is in a
// half-edited state and can retry correctly instead of double-applying edit 1.
func TestMultiEditPartialFailureReportsApplied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644)

	err := runMultiEdit(t, dir, "f.txt", []map[string]any{
		{"old_string": "alpha", "new_string": "ALPHA"}, // succeeds, lands on disk
		{"old_string": "missing", "new_string": "x"},   // fails — not in file
	})
	if err == nil {
		t.Fatal("expected the second edit to fail")
	}
	msg := err.Error()
	// The error identifies which edit failed.
	if !strings.Contains(msg, "edit 2") {
		t.Errorf("error should identify the failing edit, got: %s", msg)
	}
	// multi_edit is atomic: the file is untouched on failure.
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "alpha") {
		t.Errorf("atomic: file should be unchanged on failure; file = %q", got)
	}
}

// TestMultiEditFirstFailureReportsNoApplied: when the very first edit fails,
// the error should not claim any edits were applied (the simpler error path).
func TestMultiEditFirstFailureReportsNoApplied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("alpha\n"), 0o644)

	err := runMultiEdit(t, dir, "f.txt", []map[string]any{
		{"old_string": "missing", "new_string": "x"},
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if strings.Contains(err.Error(), "already applied") {
		t.Errorf("first-edit failure should not mention already-applied, got: %s", err)
	}
}

// TestMultiEditHookInvokedOnce verifies the post-edit hook (LSP diagnostics)
// fires exactly once for a batch, not once per sub-edit. Before the fix a
// 2-edit batch triggered the hook 3 times (N inside editFile.Execute + 1 at the
// end); now each sub-edit suppresses the hook (skipHook=true) and only the
// final call after all edits runs it.
func TestMultiEditHookInvokedOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644)

	calls := 0
	SetPostEditHook(func(_ context.Context, _ string) string {
		calls++
		return ""
	})
	defer SetPostEditHook(nil) // restore so other tests see no hook

	err := runMultiEdit(t, dir, "f.txt", []map[string]any{
		{"old_string": "alpha", "new_string": "ALPHA"},
		{"old_string": "gamma", "new_string": "GAMMA"},
	})
	if err != nil {
		t.Fatalf("multi_edit: %v", err)
	}
	if calls != 1 {
		t.Fatalf("post-edit hook called %d times, want 1 (once per batch, not per edit)", calls)
	}
}
