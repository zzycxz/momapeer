package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFTSStoreUpsertAndSearch(t *testing.T) {
	dir := t.TempDir()
	fts, err := OpenFTSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fts.Close()

	// Upsert some test data.
	if err := fts.Upsert("/test/file1.md", "project", "project", "Go programming language basics", "100-123"); err != nil {
		t.Fatal(err)
	}
	if err := fts.Upsert("/test/file2.md", "project", "project", "Python data analysis with pandas", "200-456"); err != nil {
		t.Fatal(err)
	}
	if err := fts.Upsert("/test/file3.md", "global", "user", "User prefers Go over Python", "300-789"); err != nil {
		t.Fatal(err)
	}

	// Search for Go.
	results, err := fts.Search("Go", 10, 0.15)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for 'Go'")
	}
	found := false
	for _, r := range results {
		if r.Path == "/test/file1.md" || r.Path == "/test/file3.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected file1.md or file3.md in results, got %v", results)
	}

	// Search for Python.
	results, err = fts.Search("Python", 10, 0.15)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for 'Python'")
	}
}

func TestFTSStoreDelete(t *testing.T) {
	dir := t.TempDir()
	fts, err := OpenFTSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fts.Close()

	if err := fts.Upsert("/test/file1.md", "project", "project", "test content", "100-123"); err != nil {
		t.Fatal(err)
	}
	if err := fts.Delete("/test/file1.md"); err != nil {
		t.Fatal(err)
	}

	results, err := fts.Search("test", 10, 0.15)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(results))
	}
}

func TestFTSStoreUpsertOverwrite(t *testing.T) {
	dir := t.TempDir()
	fts, err := OpenFTSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fts.Close()

	// Insert, then update.
	if err := fts.Upsert("/test/file1.md", "project", "project", "old content", "100-123"); err != nil {
		t.Fatal(err)
	}
	if err := fts.Upsert("/test/file1.md", "project", "project", "new content updated", "200-456"); err != nil {
		t.Fatal(err)
	}

	// Should not have duplicates.
	count := fts.Count()
	if count != 1 {
		t.Errorf("expected 1 entry after overwrite, got %d", count)
	}

	// Search for updated content.
	results, err := fts.Search("updated", 10, 0.15)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'updated', got %d", len(results))
	}
}

func TestBuildFtsQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Go programming", `"go" OR "programming"`},
		{"hello", `"hello"`},
		{"", ""},
		{"   ", ""},
		{"测试中文", `"测试中文"`},
	}
	for _, tt := range tests {
		got := buildFtsQuery(tt.input)
		if got != tt.want {
			t.Errorf("buildFtsQuery(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestReconcile(t *testing.T) {
	dir := t.TempDir()
	store := Store{Dir: filepath.Join(dir, "memory")}
	os.MkdirAll(store.Dir, 0o755)

	// Write a test memory file.
	testFile := filepath.Join(store.Dir, "test-memory.md")
	os.WriteFile(testFile, []byte("---\nname: test-memory\ndescription: test\ntitle: Test\nmetadata:\n  type: project\n---\n\nTest body content"), 0o644)

	// Create FTS store and reconcile.
	ftsDir := filepath.Join(dir, ".fts")
	fts, err := OpenFTSStore(ftsDir)
	if err != nil {
		t.Fatal(err)
	}
	defer fts.Close()

	if err := fts.Reconcile(store); err != nil {
		t.Fatal(err)
	}

	// Should have indexed the file.
	count := fts.Count()
	if count != 1 {
		t.Errorf("expected 1 indexed entry after reconcile, got %d", count)
	}

	// Search should find it.
	results, err := fts.Search("Test body", 10, 0.15)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 search result, got %d", len(results))
	}
}
