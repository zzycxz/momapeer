package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDoubleStarMatchMultipleStars is the core regression for C5: a pattern
// with two ** segments must honor the fixed segment between them. Before the
// fix doubleStarMatch only checked the prefix and suffix, silently dropping the
// middle segment, so "a/**/b/**/c.go" wrongly matched "a/x/c.go" (no "b").
func TestDoubleStarMatchMultipleStars(t *testing.T) {
	cases := []struct {
		pattern string
		name    string
		want    bool
	}{
		// Middle segment present → match.
		{"a/**/b/**/c.go", "a/x/b/y/c.go", true},
		// Middle segment absent → must NOT match (this was the bug).
		{"a/**/b/**/c.go", "a/x/y/c.go", false},
		// b at the top level (a/b/c.go) — ** matches zero segments.
		{"a/**/b/**/c.go", "a/b/c.go", true},
		// Single ** still works (prefix + suffix).
		{"src/**/*.ts", "src/a/b/c.ts", true},
		{"src/**/*.ts", "out/a/b/c.ts", false},
		// ** at both ends.
		{"**/foo/**", "x/foo/y/z", true},
		{"**/foo/**", "x/bar/y/z", false},
		// Leading ** with a fixed tail.
		{"**/test/*.go", "a/b/test/x.go", true},
		{"**/test/*.go", "a/b/c/x.go", false},
	}
	for _, c := range cases {
		got := doubleStarMatch(c.pattern, c.name)
		if got != c.want {
			t.Errorf("doubleStarMatch(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

// TestGlobExecuteSortByMtime verifies the result is sorted by modification
// time (most recent first), matching the tool's documented behavior.
func TestGlobExecuteSortByMtime(t *testing.T) {
	dir := t.TempDir()
	// Create three files with distinct mtimes.
	old := filepath.Join(dir, "old.txt")
	mid := filepath.Join(dir, "mid.txt")
	recent := filepath.Join(dir, "recent.txt")
	for _, p := range []string{old, mid, recent} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Set mtimes in reverse: old is oldest, recent is newest.
	setMtime(t, old, time.Now().Add(-2*time.Hour))
	setMtime(t, mid, time.Now().Add(-1*time.Hour))
	setMtime(t, recent, time.Now())

	args, _ := json.Marshal(map[string]any{"pattern": "*.txt"})
	out, err := (globTool{workDir: dir}).Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	// Expect order: recent, mid, old (newest first). Compare on the basename
	// since Execute returns absolute paths rooted at workDir.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	want := []string{"recent.txt", "mid.txt", "old.txt"}
	if len(lines) != 3 {
		t.Fatalf("expected 3 results, got %d: %v", len(lines), lines)
	}
	for i, w := range want {
		if got := filepath.Base(lines[i]); got != w {
			t.Errorf("result[%d] = %q (base %q), want %q (full: %v)", i, lines[i], got, w, lines)
		}
	}
}

func setMtime(t *testing.T, path string, mt time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatal(err)
	}
}

// TestGlobTruncationCountCorrect is the regression for the truncation-count bug
// in glob: after slicing relMatches to maxResults, `len(relMatches)-maxResults`
// was always 0, so the "... (N more)" message always said "(0 more)". The fix
// captures the total before truncation.
func TestGlobTruncationCountCorrect(t *testing.T) {
	dir := t.TempDir()
	// Create more files than maxResults (200) so truncation triggers.
	const n = 210
	for i := 0; i < n; i++ {
		// Unique names so they all match *.txt.
		name := filepath.Join(dir, fmt.Sprintf("f%03d.txt", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	args, _ := json.Marshal(map[string]any{"pattern": "*.txt"})
	out, err := (globTool{workDir: dir}).Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	// The message must report the real overflow count (10), not 0.
	wantMsg := "... (10 more"
	if !strings.Contains(out, wantMsg) {
		t.Errorf("truncation message should say %q, got:\n%s", wantMsg, out)
	}
}
