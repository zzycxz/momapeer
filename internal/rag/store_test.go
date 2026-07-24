package rag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
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
	long := strings.Repeat("x", 5000)
	chunks = chunkDoc(long, "")
	if len(chunks) < 2 {
		t.Errorf("long body should split into windows, got %d chunks", len(chunks))
	}
	// Empty.
	if len(chunkDoc("", "md")) != 0 {
		t.Error("empty body should yield no chunks")
	}
}

// TestChunkTabularHeaderRetention verifies the core fix: when a CSV is split
// into multiple chunks, EVERY chunk must carry the header row + separator so
// the vertical (column) semantics survive. This is what makes a split table
// still queryable.
func TestChunkTabularHeaderRetention(t *testing.T) {
	// Header + many data rows, large enough to exceed maxChunk (1200 chars).
	var b strings.Builder
	b.WriteString("姓名,年龄,职务,城市\n")
	for i := 0; i < 80; i++ {
		b.WriteString("张三丰,35,开发工程师,北京朝阳区科技园\n")
	}
	chunks := chunkDoc(b.String(), "csv")
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for large CSV, got %d", len(chunks))
	}
	headerLine := "| 姓名 | 年龄 | 职务 | 城市 |"
	sepLine := "| --- | --- | --- | --- |"
	for i, c := range chunks {
		lines := strings.Split(c, "\n")
		// First non-empty line must be the header, second the separator.
		var first, second string
		n := 0
		for _, ln := range lines {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			switch n {
			case 0:
				first = ln
			case 1:
				second = ln
			}
			n++
			if n >= 2 {
				break
			}
		}
		if first != headerLine {
			t.Errorf("chunk %d first line = %q, want header %q", i, first, headerLine)
		}
		if second != sepLine {
			t.Errorf("chunk %d second line = %q, want separator %q", i, second, sepLine)
		}
	}
}

// TestChunkTabularQuotedFields verifies encoding/csv handles quoted fields
// correctly: a quoted field containing a comma must survive as one cell, not
// be split on the comma.
func TestChunkTabularQuotedFields(t *testing.T) {
	body := "姓名,备注\n\"张,三\",研发\n\"李;四\",测试\n"
	chunks := chunkDoc(body, "csv")
	if len(chunks) == 0 {
		t.Fatal("quoted CSV yielded no chunks")
	}
	c := chunks[0]
	// The quoted comma must NOT create an extra column: still 2 columns.
	if !strings.Contains(c, "| 张,三 |") {
		t.Errorf("quoted field should be preserved as '张,三', got:\n%s", c)
	}
	// And there must be no 3-column row (which would mean the comma split it).
	for _, ln := range strings.Split(c, "\n") {
		if strings.HasPrefix(ln, "|") && strings.Count(ln, "|") > 4 {
			t.Errorf("row has too many columns (comma split a quoted field): %q", ln)
		}
	}
}

// TestChunkCSVRoute confirms chunkDoc routes .csv through the tabular chunker
// (output is a Markdown pipe table), not the generic window fallback.
func TestChunkCSVRoute(t *testing.T) {
	body := "col_a,col_b\n1,2\n3,4\n"
	chunks := chunkDoc(body, "csv")
	if len(chunks) != 1 {
		t.Fatalf("small CSV should be 1 chunk, got %d", len(chunks))
	}
	// Must be a Markdown table, not raw CSV.
	if !strings.HasPrefix(chunks[0], "| col_a | col_b |") {
		t.Errorf("csv chunk should start with a md table header, got:\n%s", chunks[0])
	}
	if !strings.Contains(chunks[0], "| --- | --- |") {
		t.Errorf("csv chunk should contain a md table separator, got:\n%s", chunks[0])
	}
}

// TestChunkTSVRoute confirms tab-separated input routes through the same
// chunker with the tab as delimiter.
func TestChunkTSVRoute(t *testing.T) {
	body := "col_a\tcol_b\n1\t2\n3\t4\n"
	chunks := chunkDoc(body, "tsv")
	if len(chunks) != 1 {
		t.Fatalf("small TSV should be 1 chunk, got %d", len(chunks))
	}
	if !strings.HasPrefix(chunks[0], "| col_a | col_b |") {
		t.Errorf("tsv chunk should start with a md table header, got:\n%s", chunks[0])
	}
}

// TestChunkTabularOversizedRow: a single row whose rendered form already
// exceeds maxChunk must still be emitted as its own chunk (with the header),
// never dropped.
func TestChunkTabularOversizedRow(t *testing.T) {
	huge := strings.Repeat("x", 2000) // single cell > 1200 chars
	body := "col_a,col_b\n" + huge + ",2\n"
	chunks := chunkDoc(body, "csv")
	if len(chunks) == 0 {
		t.Fatal("oversized row CSV yielded no chunks — data must not be dropped")
	}
	// The chunk must still carry the header.
	if !strings.Contains(chunks[0], "| col_a | col_b |") {
		t.Errorf("oversized row chunk should still carry header, got:\n%s", chunks[0])
	}
}

// TestWindowChunkRuneSafe is the regression test for the CJK corruption bug.
// Before the fix, windowChunk sliced by byte and a leading ASCII byte would
// misalign all subsequent 1200-byte boundaries onto the middle of 3-byte CJK
// runes, producing invalid UTF-8.
func TestWindowChunkRuneSafe(t *testing.T) {
	// Mixed ASCII + CJK, sized to exceed the 1200-rune window so it splits.
	// Under the OLD byte-based slice, a leading ASCII byte misaligned the
	// 1200-byte boundary onto the middle of a 3-byte CJK rune → invalid UTF-8.
	// The rune-based slice can never split a rune, so every chunk stays valid.
	body := "a" + strings.Repeat("中", 1500) // 1 + 1500 = 1501 runes
	chunks := windowChunk(body, 1200)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple windows for 1501 runes, got %d", len(chunks))
	}
	for i, c := range chunks {
		if !utf8.ValidString(c) {
			t.Errorf("chunk %d is not valid UTF-8 (rune-safety regression)", i)
		}
	}
	// Pure CJK long text must also remain valid (was already safe, but guard it).
	pure := strings.Repeat("中", 1500)
	for _, c := range windowChunk(pure, 1200) {
		if !utf8.ValidString(c) {
			t.Error("pure CJK window became invalid UTF-8")
		}
	}
}

// TestCSVSearchRoundTrip is an end-to-end check: import a CSV and confirm a
// search by a column value still hits (proving the tabular chunks are indexed
// and queryable, not silently corrupted).
func TestCSVSearchRoundTrip(t *testing.T) {
	store := newTempStore(t)
	defer store.Close()
	p := filepath.Join(t.TempDir(), "people.csv")
	writeFile(t, p, "name,role\nalice,engineer\nbob,designer\n")
	if n, err := store.Import("c", p, nil); err != nil {
		t.Fatal(err)
	} else if n == 0 {
		t.Fatal("CSV import produced 0 chunks")
	}
	results, err := store.Search("engineer", "c", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("search for a CSV column value 'engineer' returned no hits")
	}
}

// TestChunkTabularRaggedRows: a CSV where data rows have varying field counts
// (common in hand-edited or messy Excel exports) must NOT abort tabular
// chunking. Good rows survive and are padded/truncated to the header width so
// the emitted table stays well-formed. Before the fix, csv.ReadAll() rejected
// the whole file on the first mismatch → silent fallback to byte-windowing.
func TestChunkTabularRaggedRows(t *testing.T) {
	body := "name,role,city\n" +
		// short row (2 fields), long row (4 fields), well-formed (3 fields)
		"alice,engineer\n" +
		"bob,designer,nyc,extra\n" +
		"carol,pm,sf\n"
	chunks := chunkDoc(body, "csv")
	if len(chunks) == 0 {
		t.Fatal("ragged CSV yielded no chunks — good rows should survive, not abort")
	}
	// Every emitted row must be a well-formed 3-column table row.
	for _, c := range chunks {
		for _, ln := range strings.Split(c, "\n") {
			if !strings.HasPrefix(ln, "|") {
				continue
			}
			// A 3-col row has exactly 4 pipes: | a | b | c |
			if pc := strings.Count(ln, "|"); pc != 4 {
				t.Errorf("ragged row not normalized to 3 cols (pipes=%d): %q", pc, ln)
			}
		}
	}
	// The good data must still be searchable.
	if !strings.Contains(chunks[0], "alice") {
		t.Error("good row 'alice' lost from ragged CSV")
	}
}

// TestChunkTabularBOMStripped: UTF-8 BOM (common from Excel/Notepad saves) on
// the first column name must be stripped, or searches for the column name would
// silently miss because the BOM makes the token not match.
func TestChunkTabularBOMStripped(t *testing.T) {
	body := "\uFEFFname,role\nalice,engineer\n"
	chunks := chunkDoc(body, "csv")
	if len(chunks) == 0 {
		t.Fatal("BOM CSV yielded no chunks")
	}
	if strings.Contains(chunks[0], "\uFEFF") {
		t.Errorf("BOM not stripped from chunk header: %q", chunks[0])
	}
	if !strings.HasPrefix(chunks[0], "| name | role |") {
		t.Errorf("header should be '| name | role |' after BOM strip, got: %s", chunks[0])
	}
}

// TestChunkTabularBrokenQuote: an unterminated quoted field must not abort the
// whole file. With LazyQuotes, the stray quote degrades to a literal and the
// remaining good rows are still indexed.
func TestChunkTabularBrokenQuote(t *testing.T) {
	body := "name,note\n" +
		"alice,\"an unterminated quote\n" + // broken quote
		"bob,ok\n" + // good row after the broken one
		"carol,fine\n"
	chunks := chunkDoc(body, "csv")
	// Must not return nil (whole-file fallback). Good rows should survive.
	if len(chunks) == 0 {
		t.Fatal("broken-quote CSV yielded no chunks — good rows should survive")
	}
	// At least carol (a row well after the broken line) must be present.
	all := strings.Join(chunks, "")
	if !strings.Contains(all, "carol") {
		t.Errorf("good row 'carol' after a broken quote was lost; chunks:\n%s", all)
	}
}

// TestDeleteCleansEntitiesAndRelations proves Delete(collection, path) no longer
// orphans the structured layer. Before the fix it only DELETEd from rag_fts,
// leaving entities/relations whose Sources pointed at the deleted file.
func TestDeleteCleansEntitiesAndRelations(t *testing.T) {
	store := newTempStore(t)
	defer store.Close()
	// Seed: two files in one collection. fileA's entities/relations must be
	// removed when fileA is deleted; fileB's must survive.
	srcA := Source{Path: "/docs/a.md", Chunk: 0}
	srcB := Source{Path: "/docs/b.md", Chunk: 0}
	mustUpsert := func(e Entity, r Relation, src Source) {
		t.Helper()
		if err := store.UpsertEntity("c", e, src); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertRelation("c", r, src); err != nil {
			t.Fatal(err)
		}
	}
	mustUpsert(Entity{NameRaw: "Alice", Type: "person", Description: "from A"}, Relation{Source: "Alice", Target: "ProjectA", Type: "负责"}, srcA)
	mustUpsert(Entity{NameRaw: "Bob", Type: "person", Description: "from B"}, Relation{Source: "Bob", Target: "ProjectB", Type: "负责"}, srcB)
	// Entity shared by BOTH files: deleting A must keep it, just drop A's source.
	mustUpsert(Entity{NameRaw: "Shared", Type: "concept", Description: "in both"}, Relation{Source: "Shared", Target: "Alice", Type: "related_to"}, srcA)
	mustUpsert(Entity{NameRaw: "Shared"}, Relation{Source: "Shared", Target: "Bob", Type: "related_to"}, srcB)

	if err := store.Delete("c", "/docs/a.md"); err != nil {
		t.Fatal(err)
	}
	ents, err := store.SearchEntities("", "c", 50)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range ents {
		names[e.Name] = true
		// "Shared" must survive but have only the b.md source left.
		if e.Name == "shared" {
			for _, s := range e.Sources {
				if s.Path == "/docs/a.md" {
					t.Errorf("Shared still carries deleted a.md source: %+v", e.Sources)
				}
			}
			if len(e.Sources) == 0 {
				t.Error("Shared lost all sources — b.md source should remain")
			}
		}
	}
	if names["alice"] {
		t.Error("Alice (only in a.md) should be deleted, still present")
	}
	if !names["bob"] {
		t.Error("Bob (only in b.md) should survive deletion of a.md")
	}
	if !names["shared"] {
		t.Error("Shared (in both files) should survive with b.md source only")
	}
	// Relations touching only a.md must go; b.md relations survive.
	rels, _ := store.RelationsOf("c", "alice", false)
	if len(rels) != 0 {
		t.Errorf("Alice's relations should be gone, got %d", len(rels))
	}
	rels, _ = store.RelationsOf("c", "bob", false)
	if len(rels) == 0 {
		t.Error("Bob's relations should survive")
	}
}

// TestDeleteCollectionClearsAll proves Delete(collection, "") wipes the whole
// collection across every table (the high-frequency reset path, pure SQL).
func TestDeleteCollectionClearsAll(t *testing.T) {
	store := newTempStore(t)
	defer store.Close()
	store.UpsertEntity("c", Entity{NameRaw: "X"}, Source{Path: "p", Chunk: 0})
	store.UpsertRelation("c", Relation{Source: "x", Target: "y", Type: "r"}, Source{Path: "p", Chunk: 0})

	if err := store.Delete("c", ""); err != nil {
		t.Fatal(err)
	}
	ents, _ := store.SearchEntities("", "c", 10)
	if len(ents) != 0 {
		t.Errorf("collection delete left %d entities", len(ents))
	}
	rels, _ := store.RelationsOf("c", "x", true)
	if len(rels) != 0 {
		t.Errorf("collection delete left %d relations", len(rels))
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
