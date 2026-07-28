package builtin

// docxwrite.go generates .docx files from a structured JSON description
// (sections → headings/paragraphs/tables/lists with styles), compiling each to
// OOXML and packaging into the standard .docx zip. Pure stdlib (archive/zip +
// encoding/xml via text templates) — no external docx library, mirroring how
// readDOCX parses with the same stdlib tools.
//
// The document model is intentionally small but covers the office-report 80%:
// headings (H1-H3), paragraphs, bulleted/ordered lists, and tables (with an
// optional styled header row). Run-level styles (bold/italic/color/size/font)
// are honored via <w:rPr>. The agent describes WHAT the doc contains; this
// builder compiles the HOW (OOXML), the same split as ppt_create/xlsx_write.

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DocSection is one block of the document. Type selects the renderer; the
// shared Style applies to text runs within the section where relevant.
type DocSection struct {
	Type    string     `json:"type"`    // "heading"|"paragraph"|"list"|"table"
	Level   int        `json:"level"`   // heading level (1-6, default 1)
	Text    string     `json:"text"`    // heading/paragraph text; list single item (when Items empty)
	Items   []string   `json:"items"`   // list items (type=list)
	Ordered bool       `json:"ordered"` // list ordered? (type=list)
	Headers []string   `json:"headers"` // table header cells (type=table)
	Rows    [][]string `json:"rows"`    // table body rows (type=table)
	Style   DocStyle   `json:"style"`   // run styling (bold/italic/color/size/font/align)
}

// DocStyle is the shared run/paragraph style vocabulary. Color is "#RRGGBB".
type DocStyle struct {
	Bold        bool    `json:"bold"`
	Italic      bool    `json:"italic"`
	Color       string  `json:"color"`       // "#RRGGBB"
	Size        int     `json:"size"`        // half-points (24 = 12pt); 0 = default
	Font        string  `json:"font"`        // font family; "" = default
	Align       string  `json:"align"`       // "left"|"center"|"right" (paragraph-level)
	Bg          string  `json:"bg"`          // table cell shading "#RRGGBB"
	LineSpacing float64 `json:"lineSpacing"` // line spacing multiplier (1.5 = 1.5×); 0 = default
	Indent      int     `json:"indent"`      // first-line indent in characters; 0 = none
	HeaderBg    string  `json:"header_bg"`   // table header row shading "#RRGGBB"
}

// DocInput is the top-level payload for writeDOCX.
type DocInput struct {
	Path     string       `json:"path"`
	Title    string       `json:"title"` // optional document title (rendered as H1 if non-empty)
	Sections []DocSection `json:"sections"`
	Append   bool         `json:"append,omitempty"` // when true, insert sections into existing docx
}

// writeDOCX compiles a DocInput into a valid .docx at the given path. The zip
// contains the minimum OOXML parts Word/WPS/LibreOffice require:
// [Content_Types].xml, _rels/.rels, word/document.xml, word/_rels/document.xml.rels,
// word/styles.xml. Produces a file openable in any conformant reader.
//
// When in.Append is true and the file already exists, new sections are inserted
// before </w:body> in the existing document.xml, preserving all prior content.
func writeDOCX(in DocInput) error {
	if err := os.MkdirAll(filepath.Dir(in.Path), 0o755); err != nil {
		return err
	}

	var xmlBody string
	var styles string

	if in.Append {
		// Read existing document.xml and insert new sections before </w:body>.
		existing, err := readDocxPart(in.Path, "word/document.xml")
		if err != nil {
			// Append to a non-existent file degrades to a full write — the
			// common "first chapter" case where the agent starts with
			// append:true and no file exists yet. Any other read error (corrupt
			// zip, permission) still surfaces as an error.
			if !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("append: read existing docx: %w", err)
			}
			xmlBody = buildDocumentXML(in)
			styles = defaultStylesXML()
		} else {
			newFragments := buildSectionsXML(in)
			xmlBody = strings.Replace(existing, "</w:body>", newFragments+"</w:body>", 1)
			styles, _ = readDocxPart(in.Path, "word/styles.xml")
			if styles == "" {
				styles = defaultStylesXML()
			}
		}
	} else {
		xmlBody = buildDocumentXML(in)
		styles = defaultStylesXML()
	}

	f, err := os.Create(in.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypesXML},
		{"_rels/.rels", rootRelsXML},
		{"word/_rels/document.xml.rels", documentRelsXML},
		{"word/styles.xml", styles},
		{"word/numbering.xml", numberingXML},
		{"word/document.xml", xmlBody},
	}
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			return err
		}
	}
	return zw.Close()
}

// readDocxPart extracts a single part from an existing .docx zip archive.
func readDocxPart(docxPath, partName string) (string, error) {
	r, err := zip.OpenReader(docxPath)
	if err != nil {
		return "", err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == partName {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
	}
	return "", fmt.Errorf("part %q not found in %s", partName, docxPath)
}

// buildSectionsXML renders only the section fragments (no XML header or
// <w:body> wrapper) for use in append mode.
func buildSectionsXML(in DocInput) string {
	var b strings.Builder
	for _, sec := range in.Sections {
		switch sec.Type {
		case "heading":
			b.WriteString(renderHeading(sec.Text, sec.Level, sec.Style))
		case "paragraph":
			b.WriteString(renderParagraph(sec.Text, sec.Style))
		case "table":
			b.WriteString(renderTable(sec.Headers, sec.Rows, sec.Style))
		case "list":
			// Render list items as bullet paragraphs.
			for _, item := range sec.Items {
				b.WriteString(renderParagraph("• "+item, sec.Style))
			}
		}
	}
	return b.String()
}

// buildDocumentXML renders the <w:document><w:body>…</w:body></w:document>
// from sections. Each section maps to one or more <w:p>/<w:tbl> blocks.
func buildDocumentXML(in DocInput) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	b.WriteString(`<w:body>`)
	if strings.TrimSpace(in.Title) != "" {
		b.WriteString(renderHeading(in.Title, 1, DocStyle{Bold: true}))
	}
	for _, s := range in.Sections {
		b.WriteString(renderSection(s))
	}
	// Section properties: A4 page size + default margins so the doc renders
	// predictably across readers (Word defaults to US Letter otherwise).
	b.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>`)
	b.WriteString(`<w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/>`)
	b.WriteString(`</w:sectPr>`)
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

// renderSection dispatches by Type. Unknown types render as a plain paragraph
// so the agent never produces a broken doc over a typo.
func renderSection(s DocSection) string {
	switch strings.ToLower(s.Type) {
	case "heading":
		lvl := s.Level
		if lvl < 1 {
			lvl = 1
		}
		if lvl > 6 {
			lvl = 6 // Word defines Heading1-6; 公文 needs up to H4 ("（1）每周例会")
		}
		// Headings are bold by default (the Heading1-6 styles carry <w:b/>), but
		// we do NOT force Bold=true on the run: 公文 headings use SimHei/KaiTi
		// fonts (already visually heavy) and explicitly pass Bold:false, which
		// must be honored at the run level. The style-level <w:b/> still gives
		// plain users bold headings.
		return renderHeading(s.Text, lvl, s.Style)
	case "paragraph", "para", "text", "":
		return renderParagraph(s.Text, s.Style)
	case "list", "ul", "ol":
		items := s.Items
		if len(items) == 0 && s.Text != "" {
			items = []string{s.Text}
		}
		return renderList(items, s.Ordered)
	case "table":
		return renderTable(s.Headers, s.Rows, s.Style)
	default:
		return renderParagraph(s.Text, s.Style)
	}
}

// renderHeading maps level → a built-in heading style (Heading1-6) defined in
// styles.xml. The style carries the size/bold; per-run style overrides color/font.
func renderHeading(text string, level int, st DocStyle) string {
	pStyle := fmt.Sprintf("Heading%d", level)
	return fmt.Sprintf(`<w:p>%s%s</w:p>`, pPrXML(pStyle, st), runXML(text, st))
}

// renderParagraph emits a body paragraph with run styling + alignment.
func renderParagraph(text string, st DocStyle) string {
	return fmt.Sprintf(`<w:p>%s%s</w:p>`, pPrXML("", st), runXML(text, st))
}

// pPrXML builds the full <w:pPr>…</w:pPr> for a paragraph: an optional heading
// pStyle, then line spacing / first-line indent, then alignment. All properties
// sit INSIDE one pPr block — a bare <w:jc> outside pPr is silently ignored by
// Word (the bug behind "title not centered"), so we never emit one.
func pPrXML(pStyle string, st DocStyle) string {
	var parts []string
	if pStyle != "" {
		parts = append(parts, fmt.Sprintf(`<w:pStyle w:val="%s"/>`, pStyle))
	}
	if st.LineSpacing > 0 {
		// OOXML line spacing: 240 = single, 360 = 1.5×, 480 = double.
		val := int(st.LineSpacing * 240)
		parts = append(parts, fmt.Sprintf(`<w:spacing w:line="%d" w:lineRule="auto"/>`, val))
	}
	if st.Indent > 0 {
		// First-line indent in CHARACTER units (公文 standard): each char =
		// 100 hundredths-of-a-char (firstLineChars). Indent:2 → "200" = 2 chars,
		// which Word renders at the font's actual char width regardless of font
		// size — the property Chinese official documents require. (The earlier
		// firstLine twips value drifted with font size, breaking 公文 layout.)
		val := st.Indent * 100
		parts = append(parts, fmt.Sprintf(`<w:ind w:firstLineChars="%d"/>`, val))
	}
	if jc := pAlignXML(st.Align); jc != "" {
		parts = append(parts, jc)
	}
	if len(parts) == 0 {
		return ""
	}
	return `<w:pPr>` + strings.Join(parts, "") + `</w:pPr>`
}

// renderList emits a sequence of paragraphs each carrying a numbering property.
// We use the numPr abstractNumId 0 (unordered, bullet) or 1 (ordered, decimal)
// defined in styles.xml — so lists show proper bullets/numbers, not dashes.
func renderList(items []string, ordered bool) string {
	numID := 0
	if ordered {
		numID = 1
	}
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="%d"/></w:numPr></w:pPr>%s</w:p>`,
			numID, runXML(it, DocStyle{}))
	}
	return b.String()
}

// renderTable emits a <w:tbl> with an optional header row. Header cells get
// bold + shading (HeaderBg or a default); body cells honor per-section Bg.
// Column widths are auto (Word distributes evenly); borders are on by default.
func renderTable(headers []string, rows [][]string, st DocStyle) string {
	var b strings.Builder
	// Table properties: 100% width, single-line borders.
	b.WriteString(`<w:tbl>`)
	b.WriteString(`<w:tblPr><w:tblW w:w="5000" w:type="pct"/>`)
	b.WriteString(`<w:tblBorders>`)
	for _, edge := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		fmt.Fprintf(&b, `<w:%s w:val="single" w:sz="4" w:space="0" w:color="auto"/>`, edge)
	}
	b.WriteString(`</w:tblBorders></w:tblPr>`)
	// Header row.
	if len(headers) > 0 {
		b.WriteString(`<w:tr>`)
		hSt := DocStyle{Bold: true}
		bg := st.HeaderBg
		if bg == "" {
			bg = "#D9D9D9" // light gray default header
		}
		for _, h := range headers {
			b.WriteString(renderTableCell(h, hSt, bg))
		}
		b.WriteString(`</w:tr>`)
	}
	// Body rows.
	for _, row := range rows {
		b.WriteString(`<w:tr>`)
		for _, cell := range row {
			b.WriteString(renderTableCell(cell, DocStyle{}, st.Bg))
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	// Empty paragraph after table (OOXML requires a paragraph after a table).
	b.WriteString(`<w:p/>`)
	return b.String()
}

// renderTableCell emits one <w:tc> with optional shading.
func renderTableCell(text string, st DocStyle, bg string) string {
	shd := ""
	if bg != "" {
		shd = fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, hexNoHash(bg))
	}
	return fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="0" w:type="auto"/>%s</w:tcPr>%s</w:tc>`,
		shd, runXML(text, st))
}

// runXML emits a <w:r> with optional <w:rPr> styling + a <w:t> text run. XML
// escaping is via xml.Escape (handles & < > and quotes).
func runXML(text string, st DocStyle) string {
	rPr := runPropsXML(st)
	var esc strings.Builder
	xml.Escape(&esc, []byte(text))
	// preserveSpaces keeps leading/trailing spaces Word would otherwise trim.
	return fmt.Sprintf(`<w:r>%s<w:t xml:space="preserve">%s</w:t></w:r>`, rPr, esc.String())
}

// runPropsXML builds the <w:rPr> for a run from a DocStyle. Empty when no
// styling is set (keeps the XML lean).
func runPropsXML(st DocStyle) string {
	var parts []string
	if st.Bold {
		parts = append(parts, `<w:b/>`)
	}
	if st.Italic {
		parts = append(parts, `<w:i/>`)
	}
	if st.Size > 0 {
		parts = append(parts, fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, st.Size, st.Size))
	}
	if c := hexNoHash(st.Color); c != "" {
		parts = append(parts, fmt.Sprintf(`<w:color w:val="%s"/>`, c))
	}
	if st.Font != "" {
		parts = append(parts, fmt.Sprintf(`<w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s"/>`, st.Font, st.Font, st.Font))
	}
	if len(parts) == 0 {
		return ""
	}
	return "<w:rPr>" + strings.Join(parts, "") + "</w:rPr>"
}

// pAlignXML maps an align string to a <w:jc> paragraph property.
func pAlignXML(align string) string {
	switch strings.ToLower(align) {
	case "center":
		return `<w:jc w:val="center"/>`
	case "right":
		return `<w:jc w:val="right"/>`
	case "left":
		return `<w:jc w:val="left"/>`
	}
	return ""
}

// hexNoHash strips a leading # from a hex color, uppercased. Returns "" for
// invalid/empty input so callers can skip the attribute.
func hexNoHash(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return ""
	}
	return strings.ToUpper(s)
}

// --- static OOXML parts -----------------------------------------------------

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>
</Types>`

const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

const documentRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>
</Relationships>`

// numberingXML defines two numbering definitions referenced by renderList:
// numId=0 → bullet (•), numId=1 → decimal (1. 2. 3.). Lives in its own
// word/numbering.xml part (Word rejects numbering declared inside styles.xml).
const numberingXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum>
<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="1"/></w:num>
<w:num w:numId="0"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`

// defaultStylesXML defines Normal + Heading1-6 styles. Heading sizes:
// H1=32 half-pts (16pt), H2=28 (14pt), H3=24 (12pt), H4=24 (12pt), H5=22 (11pt),
// H6=22 (11pt). H4+ exist for 公文 ("（1）每周例会") and deep document outlines.
// Numbering lives in numbering.xml (not here).
func defaultStylesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="SimSun"/><w:sz w:val="22"/><w:szCs w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:styleId="Normal"><w:name w:val="Normal"/><w:pPr><w:spacing w:after="120" w:line="276" w:lineRule="auto"/></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="32"/><w:szCs w:val="32"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="200" w:after="100"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="28"/><w:szCs w:val="28"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="160" w:after="80"/><w:outlineLvl w:val="2"/></w:pPr><w:rPr><w:b/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading4"><w:name w:val="heading 4"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="160" w:after="80"/><w:outlineLvl w:val="3"/></w:pPr><w:rPr><w:b/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading5"><w:name w:val="heading 5"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="140" w:after="80"/><w:outlineLvl w:val="4"/></w:pPr><w:rPr><w:b/><w:sz w:val="22"/><w:szCs w:val="22"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading6"><w:name w:val="heading 6"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="140" w:after="80"/><w:outlineLvl w:val="5"/></w:pPr><w:rPr><w:b/><w:sz w:val="22"/><w:szCs w:val="22"/></w:rPr></w:style>
</w:styles>`
}
