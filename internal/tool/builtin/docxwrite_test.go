package builtin

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteDOCXRoundtrip confirms a generated .docx is (a) a valid zip with
// the required OOXML parts, (b) readable back by our own readDOCX, and (c)
// contains the expected text. This is the strongest end-to-end check: if the
// OOXML is malformed, readDOCX (a strict-ish XML walker) will miss text.
func TestWriteDOCXRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.docx")
	in := DocInput{
		Path:  path,
		Title: "2026 年度报告",
		Sections: []DocSection{
			{Type: "heading", Level: 2, Text: "一、关键指标"},
			{Type: "paragraph", Text: "本季度营收同比增长 30%。", Style: DocStyle{Bold: true, Color: "#005A9C"}},
			{Type: "heading", Level: 2, Text: "二、要点"},
			{Type: "list", Items: []string{"产品迭代", "市场扩张"}, Ordered: false},
			{Type: "list", Items: []string{"第一步", "第二步"}, Ordered: true},
			{Type: "heading", Level: 2, Text: "三、数据表"},
			{Type: "table", Headers: []string{"指标", "Q1", "Q2"}, Rows: [][]string{{"营收", "1.2亿", "1.8亿"}, {"利润", "0.3亿", "0.5亿"}}, Style: DocStyle{HeaderBg: "#005A9C"}},
			{Type: "paragraph", Text: "结论：保持增长。", Style: DocStyle{Italic: true, Color: "#666666"}},
		},
	}
	if err := writeDOCX(in); err != nil {
		t.Fatalf("writeDOCX: %v", err)
	}

	// (a) Valid zip with required parts.
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	want := map[string]bool{
		"[Content_Types].xml":          false,
		"word/document.xml":            false,
		"word/styles.xml":              false,
		"word/numbering.xml":           false,
		"word/_rels/document.xml.rels": false,
	}
	for _, f := range zr.File {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing OOXML part %q", name)
		}
	}
	zr.Close()

	// (b)+(c) Read back and check the expected text survived the roundtrip.
	got, err := readDOCX(path)
	if err != nil {
		t.Fatalf("readDOCX: %v", err)
	}
	for _, want := range []string{"2026 年度报告", "一、关键指标", "本季度营收同比增长", "产品迭代", "第二步", "指标", "1.8亿", "结论：保持增长"} {
		if !strings.Contains(got, want) {
			t.Errorf("readback missing %q\ngot:\n%s", want, got)
		}
	}
}

// TestWriteDOCXEmptyAndMinimal guards against crashes on edge inputs: empty
// sections list (title-only doc) and a doc with just one paragraph.
func TestWriteDOCXEmptyAndMinimal(t *testing.T) {
	dir := t.TempDir()

	// Title-only.
	p1 := filepath.Join(dir, "t1.docx")
	if err := writeDOCX(DocInput{Path: p1, Title: "Solo"}); err != nil {
		t.Fatalf("title-only: %v", err)
	}
	if _, err := readDOCX(p1); err != nil {
		t.Errorf("read title-only: %v", err)
	}

	// Single paragraph, no title.
	p2 := filepath.Join(dir, "t2.docx")
	if err := writeDOCX(DocInput{Path: p2, Sections: []DocSection{{Type: "paragraph", Text: "Hello"}}}); err != nil {
		t.Fatalf("single para: %v", err)
	}
	got, err := readDOCX(p2)
	if err != nil {
		t.Fatalf("read single para: %v", err)
	}
	if !strings.Contains(got, "Hello") {
		t.Errorf("single para readback = %q, want to contain Hello", got)
	}

	// Table with no header row (body only).
	p3 := filepath.Join(dir, "t3.docx")
	if err := writeDOCX(DocInput{Path: p3, Sections: []DocSection{
		{Type: "table", Rows: [][]string{{"a", "b"}}},
	}}); err != nil {
		t.Fatalf("headerless table: %v", err)
	}
	if _, err := readDOCX(p3); err != nil {
		t.Errorf("read headerless table: %v", err)
	}
}

// TestWriteDOCXHTMLEscaping confirms & < > in text are escaped so the XML
// stays well-formed (unescaped would break readDOCX / Word open).
func TestWriteDOCXHTMLEscaping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "esc.docx")
	weird := "A & B < C > D \"quotes\" 'apos'"
	if err := writeDOCX(DocInput{Path: path, Sections: []DocSection{{Type: "paragraph", Text: weird}}}); err != nil {
		t.Fatalf("writeDOCX: %v", err)
	}
	got, err := readDOCX(path)
	if err != nil {
		t.Fatalf("readDOCX (XML likely malformed): %v", err)
	}
	if !strings.Contains(got, weird) {
		t.Errorf("esc readback = %q, want to contain %q", got, weird)
	}
}

// TestWriteDOCXCreatesParentDirs confirms the writer creates missing parent
// directories (agents often pass freshly-invented paths).
func TestWriteDOCXCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c", "deep.docx")
	if err := writeDOCX(DocInput{Path: nested, Sections: []DocSection{{Type: "paragraph", Text: "x"}}}); err != nil {
		t.Fatalf("writeDOCX nested: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("nested file not created: %v", err)
	}
}
