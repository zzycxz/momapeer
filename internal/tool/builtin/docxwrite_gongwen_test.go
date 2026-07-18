package builtin

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGongwenDOCX verifies that 公文 (official document) styling works when
// applied PER-SECTION via DocStyle fields — NOT via baked-in defaults. This is
// the key architectural point: the tool ships NEUTRAL defaults (so any user's
// format works), and a 公文 document gets its FangSong/SimHei/1.5×-line styling
// through explicit per-section overrides, driven by the office-doc skill.
//
// We assert both that the 公文 styling IS present in the rendered doc AND that
// the global defaults are NOT opinionated (no FangSong/360 in styles.xml).
func TestGongwenDOCX(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "gongwen.docx")

	// A minimal 公文: every styling is passed explicitly per-section.
	err := writeDOCX(DocInput{
		Path: out,
		Sections: []DocSection{
			{Type: "paragraph", Text: "关于推进XXX建设的工作方案",
				Style: DocStyle{Font: "SimSun", Size: 44, Bold: true, Align: "center", LineSpacing: 1.5}},
			{Type: "paragraph", Text: "为贯彻要求，制定本方案。",
				Style: DocStyle{Font: "FangSong", Size: 32, Indent: 2, LineSpacing: 1.5}},
			{Type: "heading", Level: 1, Text: "一、总体要求",
				Style: DocStyle{Font: "SimHei", Size: 32, Bold: false}},
			{Type: "heading", Level: 2, Text: "（一）指导思想",
				Style: DocStyle{Font: "KaiTi", Size: 32, Bold: false}},
			{Type: "heading", Level: 3, Text: "1.建立机制",
				Style: DocStyle{Font: "FangSong", Size: 32, Bold: false}},
			{Type: "heading", Level: 4, Text: "（1）每周例会",
				Style: DocStyle{Font: "FangSong", Size: 32, Bold: false}},
		},
	})
	if err != nil {
		t.Fatalf("writeDOCX failed: %v", err)
	}

	xml := readDocXML(t, out)
	stylesXML := readStylesXML(t, out)
	checks := []struct{ name, want string }{
		// 公文 styling lives in document.xml (per-section), NOT in styles.xml.
		{"1.5x line spacing (per-section)", `w:line="360"`},
		{"first-line indent", `w:firstLineChars="200"`},
		{"level-4 heading style", `<w:pStyle w:val="Heading4"/>`},
		{"SimHei font (level 1)", `w:eastAsia="SimHei"`},
		{"KaiTi font (level 2)", `w:eastAsia="KaiTi"`},
		{"FangSong body font", `w:eastAsia="FangSong"`},
		// The title paragraph is centered — <w:jc> must appear INSIDE a
		// <w:pPr>…</w:pPr> block (a bare <w:jc> in <w:p> is ignored by Word).
		// We check the substring rather than exact order, since a section with
		// both LineSpacing and Align emits <w:spacing/> then <w:jc/> in pPr.
		{"title centered (jc present)", `<w:jc w:val="center"/>`},
		{"title pPr wraps props", `<w:pPr>`},
	}
	for _, c := range checks {
		if !strings.Contains(xml, c.want) {
			t.Errorf("%s: document.xml missing %q", c.name, c.want)
		}
	}
	// CRITICAL: the global defaults must NOT bake in 公文 opinions. A casual
	// user (no per-section style) must not inherit FangSong or 1.5× line.
	for _, bad := range []string{`eastAsia="FangSong"`, `w:line="360"`} {
		if strings.Contains(stylesXML, bad) {
			t.Errorf("neutral-defaults violation: styles.xml bakes in %q — 公文 styling leaked into global defaults", bad)
		}
	}
	// The non-bold heading must NOT emit <w:b/> in its run.
	if i := strings.Index(xml, "一、总体要求"); i >= 0 {
		start := i - 200
		if start < 0 {
			start = 0
		}
		if strings.Contains(xml[start:i], "<w:b/>") {
			t.Errorf("non-bold heading \"一、总体要求\" incorrectly contains <w:b/>")
		}
	}
	// CRITICAL regression guard: every <w:jc> (alignment) MUST sit inside a
	// <w:pPr> block. A bare <w:jc> directly under <w:p> is silently ignored by
	// Word (the bug behind "title not centered"). Walk every <w:jc> and assert
	// it is preceded by an unclosed <w:pPr> with no </w:pPr> in between.
	for pos := 0; ; {
		idx := strings.Index(xml[pos:], `<w:jc w:val="center"/>`)
		if idx < 0 {
			break
		}
		abs := pos + idx
		// scan back for the nearest <w:pPr> or </w:pPr>
		head := xml[:abs]
		lastOpen := strings.LastIndex(head, "<w:pPr>")
		lastClose := strings.LastIndex(head, "</w:pPr>")
		if lastOpen < lastClose {
			t.Errorf("regression: <w:jc> at byte %d is OUTSIDE a <w:pPr> block (bare jc ignored by Word)", abs)
		}
		pos = abs + 1
	}
}

// readDocXML extracts word/document.xml from a .docx zip for assertion.
func readDocXML(t *testing.T, path string) string {
	return readZipPart(t, path, "word/document.xml")
}

// readStylesXML extracts word/styles.xml — used to assert global defaults stay
// neutral (no 公文 opinions baked in).
func readStylesXML(t *testing.T, path string) string {
	return readZipPart(t, path, "word/styles.xml")
}

// readZipPart opens the .docx zip and returns one part's bytes as a string.
func readZipPart(t *testing.T, path, name string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open docx: %v", err)
	}
	defer f.Close()
	st, _ := f.Stat()
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	for _, p := range zr.File {
		if p.Name == name {
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
	}
	t.Fatalf("%s not found in docx", name)
	return ""
}
