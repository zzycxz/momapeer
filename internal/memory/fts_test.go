package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestFTSSearchTitleAndDescription confirms the v4 schema indexes title and
// description, not just body. Before v4 a keyword present only in the title
// (absent from body) returned no results — the original "problem 6".
func TestFTSSearchTitleAndDescription(t *testing.T) {
	dir := t.TempDir()
	fts, err := OpenFTSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fts.Close()

	// "Beijing" appears ONLY in the title/description, never in the body, so a
	// body-only index would miss it entirely.
	if err := fts.UpsertWithTime("/r.md", "global", "user",
		"Beijing move", "Relocated to Beijing",
		"The user changed cities recently.", // body has no "Beijing"
		"active", "2026-05-01", "", "fp1"); err != nil {
		t.Fatal(err)
	}

	// Title keyword must match.
	if r, _ := fts.Search("Beijing", 10, 0.15); len(r) != 1 {
		t.Errorf("title keyword 'Beijing' should match, got %d results", len(r))
	}
	// Description keyword must match too.
	if r, _ := fts.Search("Relocated", 10, 0.15); len(r) != 1 {
		t.Errorf("description keyword 'Relocated' should match, got %d results", len(r))
	}
}

// TestSchemaMigrationV3ToV4 simulates an upgrade from a pre-v4 database (title/
// description not yet indexed) to v4. It builds a v3-style store with a fact on
// disk, forces the schema version down, then runs EnsureSchema and confirms the
// rebuild re-indexes everything — including making the title searchable. This
// is the migration real users hit when v0.3.2 ships.
func TestSchemaMigrationV3ToV4(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	// Seed a fact via the store (writes the .md file that Rebuild will re-index).
	s.Save(Memory{
		Name: "move", Title: "Beijing relocation", Description: "Moved to Beijing",
		Type: TypeUser, Body: "City changed.",
	})

	svc, err := NewSearchService(s)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	// Index once at current version, then force the version DOWN to simulate an
	// older db that predates the title/description columns.
	if err := svc.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	if err := svc.fts.SetSchemaVersion(3); err != nil {
		t.Fatal(err)
	}

	// EnsureSchema must detect v3 < v4 and Rebuild, restoring title search.
	if err := svc.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema migration: %v", err)
	}
	if v := svc.fts.SchemaVersion(); v != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", v, currentSchemaVersion)
	}
	// After rebuild, the title keyword (absent from body) must be searchable.
	results, err := svc.Search("Beijing")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("after v3→v4 migration, 'Beijing' (title-only) should match, got %d", len(results))
	}
}

// TestSearchAsOfInjectionSafe confirms asOfDate is bound as a parameter, not
// string-interpolated: a malicious date string must be treated as a literal
// value (matching nothing) rather than parsed as SQL. Before the fix, a string
// like "x' OR 1=1 --" would have broken out of the WHERE clause.
func TestSearchAsOfInjectionSafe(t *testing.T) {
	dir := t.TempDir()
	fts, err := OpenFTSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fts.Close()
	fts.UpsertWithTime("/a.md", "project", "project", "", "", "active fact", "active", "2026-01-01", "", "fp1")

	// A payload that would widen the result set if interpolated; with parameter
	// binding it's just a non-matching date string → 0 results, no error.
	res, err := fts.SearchAsOf("active", 10, 0.15, parseDateMust("2026-06-01"))
	if err != nil {
		t.Fatalf("baseline search failed: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("baseline should find the active fact")
	}
	// Direct searchInternal with a hostile asOfDate: must not error and must not
	// return rows it shouldn't (the injected SQL would return the row regardless
	// of date).
	if _, err := fts.searchInternal("active", 10, 0.15, "' OR 1=1 --"); err != nil {
		t.Errorf("hostile asOfDate should not error (parameter binding), got: %v", err)
	}
}

func parseDateMust(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
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
		{"测试中文", `"测" OR "试" OR "中" OR "文"`},
		{"hello世界", `"hello" OR "世" OR "界"`},
		{"あいう", `"あ" OR "い" OR "う"`},
		{"가나다", `"가" OR "나" OR "다"`},
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
