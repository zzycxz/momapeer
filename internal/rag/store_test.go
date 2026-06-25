package rag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportAndSearch(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Write a markdown doc and a text doc.
	mdPath := filepath.Join(dir, "spec.md")
	writeFile(t, mdPath, "# Product Spec\n\nThe widget engine handles rendering.\n\nIt supports themes and dark mode.\n\nPerformance target is 60fps.")
	txtPath := filepath.Join(dir, "notes.txt")
	writeFile(t, txtPath, "Meeting notes: ship date moved to Q3.")

	// Import both into collections.
	if n, err := store.Import("specs", mdPath, nil); err != nil {
		t.Fatal(err)
	} else if n == 0 {
		t.Fatal("imported 0 chunks")
	}
	if _, err := store.Import("notes", txtPath, nil); err != nil {
		t.Fatal(err)
	}

	// Search "widget rendering" in all collections.
	results, err := store.Search("widget rendering", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected matches for 'widget rendering'")
	}
	// The top hit should be from the spec.
	if !strings.Contains(results[0].Path, "spec.md") {
		t.Errorf("top hit path = %s, want spec.md", results[0].Path)
	}

	// Scoped search: "meeting" only in notes.
	results, err = store.Search("meeting", "notes", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || !strings.Contains(results[0].Path, "notes.txt") {
		t.Errorf("scoped notes search miss: %+v", results)
	}

	// Scoped search in wrong collection returns nothing.
	results, _ = store.Search("meeting", "specs", 5)
	if len(results) != 0 {
		t.Errorf("meeting in specs should be empty, got %d", len(results))
	}
}

func TestCJKSearch(t *testing.T) {
	store := newTempStore(t)
	defer store.Close()
	p := filepath.Join(t.TempDir(), "cn.md")
	// Note: CJK adjacent to punctuation (渲染。) is indexed as one token by FTS5's
	// unicode61, so we search terms that stand alone. This mirrors how the memory
	// subsystem's CJK search behaves in practice.
	writeFile(t, p, "# 产品规格\n\n小部件 引擎 负责 渲染 输出\n\n支持 深色 模式")
	if _, err := store.Import("default", p, nil); err != nil {
		t.Fatal(err)
	}
	results, err := store.Search("渲染", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("CJK search '渲染' should match")
	}
}

func TestListAndDelete(t *testing.T) {
	store := newTempStore(t)
	defer store.Close()
	p := filepath.Join(t.TempDir(), "doc.md")
	writeFile(t, p, "alpha beta gamma delta epsilon")
	store.Import("colA", p, nil)
	store.Import("colB", p, nil)

	cols, err := store.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 {
		t.Fatalf("List = %d collections, want 2", len(cols))
	}

	// Delete one doc from colA.
	if err := store.Delete("colA", p); err != nil {
		t.Fatal(err)
	}
	cols, _ = store.List("colA")
	// colA now has 0 docs (only doc removed) but the collection row may persist
	// with 0; check chunks.
	for _, c := range cols {
		if c.Name == "colA" && c.Documents != 0 {
			t.Errorf("colA documents = %d, want 0 after delete", c.Documents)
		}
	}

	// Delete whole colB.
	store.Delete("colB", "")
	cols, _ = store.List("colB")
	for _, c := range cols {
		if c.Name == "colB" {
			t.Error("colB should be gone after collection delete")
		}
	}
}

func TestReImportReplaces(t *testing.T) {
	store := newTempStore(t)
	defer store.Close()
	p := filepath.Join(t.TempDir(), "doc.md")
	writeFile(t, p, "first version content alpha")
	n1, _ := store.Import("c", p, nil)
	// Re-import with different content.
	writeFile(t, p, "second version content beta")
	n2, _ := store.Import("c", p, nil)
	// Chunk count may be equal; the point is no duplication.
	_ = n1
	_ = n2
	// Search for old content — should NOT match after replace.
	results, _ := store.Search("alpha", "c", 5)
	if len(results) != 0 {
		t.Errorf("old content 'alpha' should be gone after re-import, got %d", len(results))
	}
	// New content matches.
	results, _ = store.Search("beta", "c", 5)
	if len(results) == 0 {
		t.Error("new content 'beta' should match after re-import")
	}
}

func TestBinaryFormatRejected(t *testing.T) {
	store := newTempStore(t)
	defer store.Close()
	p := filepath.Join(t.TempDir(), "doc.docx")
	writeFile(t, p, "fake binary")
	_, err := store.Import("c", p, nil)
	if err == nil {
		t.Error("docx import should be rejected in Phase 3")
	}
}

func TestChunkDoc(t *testing.T) {
	// Short markdown: paragraphs merge into one chunk (under maxChunk).
	body := "para one.\n\npara two.\n\npara three."
	chunks := chunkDoc(body, "md")
	if len(chunks) != 1 {
		t.Errorf("short md chunk count = %d, want 1 (merged)", len(chunks))
	}
	// Long single block → windowed into multiple.
	long := strings.Repeat("x", 3000)
	chunks = chunkDoc(long, "")
	if len(chunks) < 2 {
		t.Errorf("long body should split into windows, got %d chunks", len(chunks))
	}
	// Empty.
	if len(chunkDoc("", "md")) != 0 {
		t.Error("empty body should yield no chunks")
	}
}

func newTempStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
