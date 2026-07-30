package rag

// officedoc.go implements text extraction from binary Office formats (.docx,
// .xlsx, .pptx, .pdf) using stdlib for Office and ledongthuc/pdf for PDF.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	pdflib "github.com/ledongthuc/pdf"

	"github.com/zzycxz/momapeer/internal/docconv"
	"github.com/zzycxz/momapeer/internal/proc"
)

// cjkSpaceRe matches a space between two CJK characters.
var cjkSpaceRe = regexp.MustCompile(`(\p{Han})\s+(\p{Han})`)

// fixCJKSpaces removes spaces between CJK characters. PDF renderers often
// insert spurious spaces because the internal character positioning uses
// discrete glyph boxes — a tiny gap becomes a space in extracted text.
func fixCJKSpaces(s string) string {
	// Run multiple passes because the regex only catches one gap at a time
	// and adjacent matches don't overlap ("A B C" → "AB C" → "ABC").
	for {
		fixed := cjkSpaceRe.ReplaceAllString(s, "$1$2")
		if fixed == s {
			return fixed
		}
		s = fixed
	}
}

// readPDF extracts text from a .pdf file. Prefers the Python pipeline
// (pdfplumber for tables + PaddleOCR for scanned pages). Falls back to
// ledongthuc/pdf (pure Go) when the Python script is unavailable.
func readPDF(path string) (string, error) {
	// Try Python pipeline first (pdfplumber + PaddleOCR).
	if findOCRScript() != "" {
		text, err := readPDFWithOCR(path)
		if err == nil && utf8.RuneCountInString(text) > 0 {
			return fixCJKSpaces(text), nil
		}
	}

	// Try markitdown (Python doc converter) for PDF — handles modern PDF
	// stream encodings that the Go library can't parse.
	if findDocConverterScript() != "" {
		text, err := convertWithMarkitdown(path)
		if err == nil && utf8.RuneCountInString(text) > 0 {
			return fixCJKSpaces(text), nil
		}
	}

	// Fallback: ledongthuc/pdf (pure Go). Limited — can't parse many
	// modern PDFs (xref streams, object streams). Returns error on failure
	// so the caller can report which files were skipped.
	f, r, err := pdflib.Open(path)
	if err != nil {
		return "", fmt.Errorf("open pdf (Go fallback): %w", err)
	}
	defer f.Close()
	var b strings.Builder
	pageErrors := 0
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			pageErrors++
			continue
		}
		text = strings.TrimSpace(text)
		if text != "" {
			b.WriteString(text)
			b.WriteString("\n\n")
		}
	}
	result := strings.TrimSpace(b.String())
	if result == "" {
		if pageErrors > 0 {
			return "", fmt.Errorf("pdf: %d/%d pages failed to parse (unsupported stream encoding)", pageErrors, r.NumPage())
		}
		return "", fmt.Errorf("pdf: no extractable text found")
	}
	return fixCJKSpaces(result), nil
}

// ocrScriptCandidates lists possible locations of ocr_pdf.py.
func ocrScriptCandidates() []string { //nolint:unused
	return docconv.ScriptCandidates("ocr_pdf.py")
}

// findOCRScript locates the ocr_pdf.py script.
func findOCRScript() string {
	return docconv.FindScript("ocr_pdf.py")
}

// docConverterScriptCandidates lists possible locations of doc_converter.py.
func docConverterScriptCandidates() []string { //nolint:unused
	return docconv.ScriptCandidates("doc_converter.py")
}

// findDocConverterScript locates the doc_converter.py script.
func findDocConverterScript() string {
	return docconv.FindScript("doc_converter.py")
}

// convertWithMarkitdown calls doc_converter.py to convert a file to Markdown.
func convertWithMarkitdown(path string) (string, error) {
	return docconv.ConvertText(path)
}

// readPDFWithOCR calls the ocr_pdf.py Python script to extract text from a
// scanned PDF using PaddleOCR. Returns the OCR'd text or an error.
func readPDFWithOCR(path string) (string, error) {
	script := findOCRScript()
	if script == "" {
		return "", fmt.Errorf("ocr_pdf.py not found")
	}
	python := "python"
	if runtime.GOOS != "windows" {
		python = "python3"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, python, script, path)
	proc.HideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ocr script: %w: %s", err, stderr.String())
	}

	var result struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return "", fmt.Errorf("ocr parse: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("ocr: %s", result.Error)
	}
	return result.Text, nil
}

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

// readXLSXAsText extracts cell values from a .xlsx via stdlib zip+xml,
// returning all sheets as tab-separated text. No excelize dependency.
func readXLSXAsText(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open xlsx (is it a valid .xlsx?): %w", err)
	}
	defer zr.Close()

	// Read shared strings first.
	var sharedStrings []string
	for _, f := range zr.File {
		if f.Name == "xl/sharedStrings.xml" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			data, _ := io.ReadAll(rc)
			rc.Close()
			sharedStrings = parseSharedStrings(data)
			break
		}
	}

	// Read each sheet.
	var sheetFiles []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheetFiles = append(sheetFiles, f.Name)
		}
	}
	sort.Slice(sheetFiles, func(i, j int) bool {
		ni, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(sheetFiles[i]), "sheet"), ".xml"))
		nj, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(sheetFiles[j]), "sheet"), ".xml"))
		return ni < nj
	})

	var b strings.Builder
	for si, name := range sheetFiles {
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
			if si > 0 {
				fmt.Fprintf(&b, "\n--- sheet %d ---\n", si+1)
			}
			rows := parseSheetRows(data, sharedStrings)
			for _, row := range rows {
				b.WriteString(strings.Join(row, "\t"))
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(b.String()), nil
}

// parseSharedStrings extracts <t> text from xl/sharedStrings.xml.
func parseSharedStrings(data []byte) []string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var out []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "t" {
			var txt string
			if err := dec.DecodeElement(&txt, &se); err == nil {
				out = append(out, txt)
			}
		}
	}
	return out
}

// parseSheetRows extracts rows/cells from a worksheet XML.
func parseSheetRows(data []byte, sharedStrings []string) [][]string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var rows [][]string
	var currentRow []string
	inRow := false
	inValue := false
	cellType := ""
	var valueBuf strings.Builder

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				inRow = true
				currentRow = nil
			case "c":
				cellType = ""
				for _, attr := range t.Attr {
					if attr.Name.Local == "t" {
						cellType = attr.Value
					}
				}
			case "v":
				inValue = true
				valueBuf.Reset()
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inValue = false
				val := valueBuf.String()
				if cellType == "s" {
					// Shared string reference.
					idx, err := strconv.Atoi(val)
					if err == nil && idx < len(sharedStrings) {
						val = sharedStrings[idx]
					}
				}
				currentRow = append(currentRow, val)
			case "row":
				inRow = false
				if len(currentRow) > 0 {
					rows = append(rows, currentRow)
				}
			}
		case xml.CharData:
			if inValue && inRow {
				valueBuf.Write(t)
			}
		}
	}
	return rows
}

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
