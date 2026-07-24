package builtin

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Binary Office document parsing.
//   - .xlsx read + write: excelize (full style/formula/multi-sheet support).
//   - .docx / .pptx text extraction: stdlib (archive/zip + encoding/xml),
//     since there's no lightweight license-clean docx lib comparable to
//     excelize. Writing .docx is unsupported (use the source app or ppt tools).
//
// excelize gives xlsx robustness the earlier hand-rolled OOXML lacked (formulas,
// styles, multi-sheet, dates). docx/pptx text extraction is a contained need
// (pull <w:t>/<a:t> runs) that stdlib covers well.

// --- xlsx (excelize) --------------------------------------------------------

// readXLSX extracts cell values from a .xlsx via excelize, returning rows across
// ALL sheets (each sheet's rows concatenated, with a "--- sheet: Name ---"
// separator between sheets). Handles shared strings, formulas (cached values),
// booleans, dates, and multi-sheet workbooks. Integer-valued numbers drop the
// trailing .0 for readability.
func readXLSX(path string) ([][]string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open xlsx (is it a valid .xlsx?): %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, nil
	}
	var allRows [][]string
	for si, sheet := range sheets {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return nil, fmt.Errorf("read sheet %q: %w", sheet, err)
		}
		if si > 0 {
			allRows = append(allRows, []string{fmt.Sprintf("--- sheet: %s ---", sheet)})
		}
		for _, row := range rows {
			for ci, cell := range row {
				row[ci] = normalizeNumber(strings.TrimSpace(cell))
			}
			allRows = append(allRows, row)
		}
	}
	return allRows, nil
}

// XLSXWriteRows writes rows to a .xlsx file at path via excelize (one sheet,
// "Sheet1"). Produces a fully-valid workbook openable in Excel/WPS/LibreOffice.
func XLSXWriteRows(path string, rows [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Sheet1"
	for ri, row := range rows {
		for ci, val := range row {
			cell, err := excelize.CoordinatesToCellName(ci+1, ri+1)
			if err != nil {
				return err
			}
			if err := f.SetCellValue(sheet, cell, val); err != nil {
				return err
			}
		}
	}
	return f.SaveAs(path)
}

// normalizeNumber drops a trailing .0 from integer-valued floats for readability.
func normalizeNumber(s string) string {
	if s == "" {
		return ""
	}
	if strings.HasSuffix(s, ".0") {
		if _, err := strconv.ParseFloat(s, 64); err == nil {
			return s[:len(s)-2]
		}
	}
	return s
}

// --- docx (stdlib zip+xml text extraction) ----------------------------------

// readDOCX extracts text from word/document.xml, joining paragraphs with newlines.
func readDOCX(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open docx (is it a valid .docx?): %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return "", err
		}
		return parseDOCXText(data), nil
	}
	return "", fmt.Errorf("docx has no word/document.xml")
}

// parseDOCXText walks the document XML collecting <w:t> runs, breaking at
// paragraph boundaries (<w:p>). Tabs and breaks are approximated.
func parseDOCXText(data []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var b strings.Builder
	var inPara bool
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				if inPara {
					b.WriteByte('\n')
				}
				inPara = true
			case "t":
				var txt string
				if err := dec.DecodeElement(&txt, &t); err == nil {
					b.WriteString(txt)
				}
			case "tab":
				b.WriteByte('\t')
			case "br":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				b.WriteByte('\n')
				inPara = false
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// --- pptx (stdlib zip+xml text extraction, best-effort) ---------------------

// readPPTX extracts text from each slide (slideN.xml <a:t> runs), one block
// per slide labeled [slide N].
func readPPTX(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open pptx (is it a valid .pptx?): %w", err)
	}
	defer zr.Close()
	var slideFiles []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slideFiles = append(slideFiles, f.Name)
		}
	}
	sort.Slice(slideFiles, func(i, j int) bool {
		ni, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(slideFiles[i]), "slide"), ".xml"))
		nj, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(slideFiles[j]), "slide"), ".xml"))
		return ni < nj
	})
	var b strings.Builder
	for _, name := range slideFiles {
		for _, ff := range zr.File {
			if ff.Name != name {
				continue
			}
			rc, err := ff.Open()
			if err != nil {
				continue
			}
			data, _ := io.ReadAll(rc)
			rc.Close()
			fmt.Fprintf(&b, "[slide %s]\n", filepath.Base(name))
			b.WriteString(parseSlideText(data))
			b.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(b.String()), nil
}

// parseSlideText collects all <a:t> run text from a slide XML, space-joined.
func parseSlideText(data []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "t" {
			var txt string
			if err := dec.DecodeElement(&txt, &se); err == nil {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(txt)
			}
		}
	}
	return b.String()
}

// formatRows renders a [][]string grid as an aligned table (same style as CSV read).
func formatRows(rows [][]string) string {
	if len(rows) == 0 {
		return "(empty)"
	}
	widths := make([]int, 0)
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				widths = append(widths, len(cell))
			} else if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	var b strings.Builder
	for _, row := range rows {
		for i, cell := range row {
			pad := 0
			if i < len(widths) {
				pad = widths[i] - len(cell)
			}
			b.WriteString(cell)
			if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", pad+2))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
