package builtin

import (
	"archive/zip"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteDOCXAppendPreservesAndAdds verifies the core append contract: an
// initial write creates a doc, a second call with Append:true inserts new
// sections BEFORE </w:body> while preserving every existing section in order.
// This is the foundation of long-document generation (chapter-by-chapter into
// one file), so we assert both content preservation and ordering.
func TestWriteDOCXAppendPreservesAndAdds(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "book.docx")

	// Chapter 1 + 2 (full write).
	if err := writeDOCX(DocInput{Path: out, Sections: []DocSection{
		{Type: "heading", Level: 1, Text: "第一章"},
		{Type: "paragraph", Text: "第一章正文内容"},
		{Type: "heading", Level: 1, Text: "第二章"},
		{Type: "paragraph", Text: "第二章正文内容"},
	}}); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Append chapter 3.
	if err := writeDOCX(DocInput{Path: out, Append: true, Sections: []DocSection{
		{Type: "heading", Level: 1, Text: "第三章"},
		{Type: "paragraph", Text: "第三章正文内容"},
	}}); err != nil {
		t.Fatalf("append write: %v", err)
	}

	xml := readDocXMLShared(t, out)
	// All three chapters present, in order.
	for _, want := range []string{"第一章", "第一章正文内容", "第二章", "第二章正文内容", "第三章", "第三章正文内容"} {
		if !strings.Contains(xml, want) {
			t.Errorf("append lost content: %q not in document.xml", want)
		}
	}
	// Ordering: chapter 1 before chapter 2 before chapter 3.
	i1 := strings.Index(xml, "第一章")
	i2 := strings.Index(xml, "第二章")
	i3 := strings.Index(xml, "第三章")
	if !(i1 >= 0 && i1 < i2 && i2 < i3) {
		t.Errorf("chapter ordering wrong after append: i1=%d i2=%d i3=%d", i1, i2, i3)
	}
	// The sectPr (page setup) must survive exactly once — append must not
	// duplicate or drop the section properties.
	if c := strings.Count(xml, "<w:sectPr"); c != 1 {
		t.Errorf("expected exactly one <w:sectPr> after append, got %d", c)
	}
}

// TestWriteDOCXAppendDegradestoFullWrite: appending to a non-existent path
// falls back to a full write (not an error). This is the "first chapter" case —
// the agent starts with append:true and no file exists yet.
func TestWriteDOCXAppendDegradestoFullWrite(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "new.docx") // does not exist yet

	if err := writeDOCX(DocInput{Path: out, Append: true, Sections: []DocSection{
		{Type: "paragraph", Text: "首章内容"},
	}}); err != nil {
		t.Fatalf("append to non-existent path should degrade to full write, got: %v", err)
	}
	xml := readDocXMLShared(t, out)
	if !strings.Contains(xml, "首章内容") {
		t.Errorf("degraded full write lost content")
	}
}

// TestWriteDOCXAppendKeepsStyles: appending must preserve the original
// styles.xml byte-for-byte (all chapters share one style set — no per-chapter
// font drift). We write with a known title (which sets an H1), append, then
// confirm styles.xml still has the Heading1 definition.
func TestWriteDOCXAppendKeepsStyles(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "styled.docx")

	if err := writeDOCX(DocInput{Path: out, Title: "Doc", Sections: []DocSection{
		{Type: "paragraph", Text: "a"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := writeDOCX(DocInput{Path: out, Append: true, Sections: []DocSection{
		{Type: "heading", Level: 1, Text: "后加章"},
	}}); err != nil {
		t.Fatal(err)
	}
	styles := readPartShared(t, out, "word/styles.xml")
	if !strings.Contains(styles, `w:styleId="Heading1"`) {
		t.Errorf("append dropped the Heading1 style from styles.xml")
	}
	// The appended heading should render using Heading1 (proving styles survive
	// AND the new section references them).
	xml := readDocXMLShared(t, out)
	if !strings.Contains(xml, `<w:pStyle w:val="Heading1"/>`) {
		t.Errorf("appended heading should reference Heading1 style")
	}
}

// TestWriteDOCXAppendMultipleChapters: a stress check that N sequential appends
// accumulate all chapters, mirroring how a long doc is built across many turns.
func TestWriteDOCXAppendMultipleChapters(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "multi.docx")
	// Seed with chapter 1.
	if err := writeDOCX(DocInput{Path: out, Sections: []DocSection{
		{Type: "heading", Level: 1, Text: "章一"},
	}}); err != nil {
		t.Fatal(err)
	}
	// Append chapters 2-5.
	chapters := []string{"章二", "章三", "章四", "章五"}
	for _, title := range chapters {
		if err := writeDOCX(DocInput{Path: out, Append: true, Sections: []DocSection{
			{Type: "heading", Level: 1, Text: title},
		}}); err != nil {
			t.Fatalf("append %s: %v", title, err)
		}
	}
	xml := readDocXMLShared(t, out)
	// All five chapter markers present.
	for _, want := range append([]string{"章一"}, chapters...) {
		if !strings.Contains(xml, want) {
			t.Errorf("after 5 chapters, %q missing", want)
		}
	}
}

// readDocXMLShared and readPartShared are shared helpers for the append tests
// (kept here to avoid depending on the gongwen test file's helpers). They open
// the .docx zip and return one part's bytes as a string.
func readDocXMLShared(t *testing.T, path string) string {
	t.Helper()
	return readPartShared(t, path, "word/document.xml")
}

func readPartShared(t *testing.T, path, name string) string {
	t.Helper()
	f := openZipShared(t, path)
	for _, p := range f.File {
		if p.Name != name {
			continue
		}
		rc, err := p.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(b)
	}
	t.Fatalf("%s not found", name)
	return ""
}

func openZipShared(t *testing.T, path string) *zip.ReadCloser {
	t.Helper()
	f, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open docx: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
