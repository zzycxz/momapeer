package builtin

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zzycxz/momapeer/internal/tool"
)

// Document tools (Phase 3 of coWork). These cover the text-based document
// formats an office agent produces and consumes most: CSV (encoding/csv), JSON,
// Markdown, and plain text — all stdlib, zero new deps. Binary Office formats
// (.docx/.xlsx/.pptx) are intentionally NOT here: PPT goes through the wps-ppt
// MCP server, and Word/Excel binary handling needs unioffice (license) or
// excelize — deferred until a license-clean path is chosen. For now the agent
// can produce .csv/.md/.txt/.json reports and read those formats; .docx docs can
// be imported into RAG once converted to text.
//
// All tools are read-only/write-aware flagged correctly so the agent's batch
// optimizer can parallelize reads.

// DocumentTools returns the document tools for cowork registration.
func DocumentTools() []tool.Tool {
	return []tool.Tool{docRead{}, docWrite{}, csvRead{}, csvWrite{}, xlsxRead{}, xlsxWrite{}, docConvert{}, mindmapCreate{}}
}

// --- doc_read (csv/json/md/txt) --------------------------------------------

type docRead struct{}

func (docRead) Name() string { return "doc_read" }

func (docRead) Description() string {
	return "Read a document and return its content. Supports: .csv (formatted table), .json (pretty-printed), .md/.txt/.html/.code (text), AND binary Office formats .xlsx (spreadsheet cells as a table), .docx (document text), .pptx (slide text). Binary formats are parsed via the OOXML zip+XML structure (no external deps). Cap on size: files over 200k chars are truncated."
}

func (docRead) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"Absolute path to the document"}
},
"required":["path"]
}`)
}

func (docRead) ReadOnly() bool { return true }

func (docRead) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	abs, err := filepath.Abs(strings.TrimSpace(p.Path))
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(abs), "."))
	const max = 200_000

	// Binary Office formats: parse via OOXML, don't treat as text.
	switch ext {
	case "xlsx":
		rows, err := readXLSX(abs)
		if err != nil {
			return "", err
		}
		content := formatRows(rows)
		if len(content) > max {
			content = content[:max] + fmt.Sprintf("\n\n[...truncated, %d more chars]", len(content)-max)
		}
		return content, nil
	case "docx":
		content, err := readDOCX(abs)
		if err != nil {
			return "", err
		}
		if len(content) > max {
			content = content[:max] + fmt.Sprintf("\n\n[...truncated, %d more chars]", len(content)-max)
		}
		return content, nil
	case "pptx":
		content, err := readPPTX(abs)
		if err != nil {
			return "", err
		}
		if len(content) > max {
			content = content[:max] + fmt.Sprintf("\n\n[...truncated, %d more chars]", len(content)-max)
		}
		return content, nil
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	content := string(data)
	switch ext {
	case "csv":
		content, err = formatCSV(data)
		if err != nil {
			return "", err
		}
	case "json":
		content, err = formatJSON(data)
		if err != nil {
			// Not valid JSON — return raw.
			content = string(data)
		}
	}
	if len(content) > max {
		content = content[:max] + fmt.Sprintf("\n\n[...truncated, %d more chars]", len(content)-max)
	}
	return content, nil
}

func formatCSV(data []byte) (string, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	rows, err := r.ReadAll()
	if err != nil {
		return "", fmt.Errorf("parse csv: %w", err)
	}
	if len(rows) == 0 {
		return "(empty csv)", nil
	}
	// Column-width table for readability.
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
			pad := widths[i] - len(cell)
			if pad < 0 {
				pad = 0
			}
			b.WriteString(cell)
			if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", pad+2))
			}
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func formatJSON(data []byte) (string, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// --- doc_write --------------------------------------------------------------

type docWrite struct{ roots []string }

func (docWrite) Name() string { return "doc_write" }

func (docWrite) Description() string {
	return "Write a document to a path, creating parent dirs. Format by extension: .md/.txt/.html/code (string content), .json (pretty-printed object), .csv (rows as array of arrays of strings), .xlsx (rows as an array of arrays → real spreadsheet, or a structured object for multi-sheet/styled output), .docx (structured sections → real Word document with headings/paragraphs/lists/tables + styling). Overwrites existing files."
}

func (docWrite) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"Absolute path to write (extension determines format)"},
  "content":{"description":"For .md/.txt/.html/code: a string. For .json: any JSON value. For .csv/.xlsx: an array of arrays of strings (simple) OR an object (structured — see xlsx notes)."},
  "sections":{"type":"array","description":"For .docx ONLY: document blocks. Each {type:'heading'|'paragraph'|'list'|'table', text, level(1-3), items(list), ordered(list bool), headers/rows(table), style:{bold,italic,color:'#RRGGBB',size(half-pts),font,align:'left|center|right',bg,header_bg}}."},
  "title":{"type":"string","description":"For .docx: optional document title (rendered as H1)."},
  "append":{"type":"boolean","description":"Append to the file instead of overwriting (text formats only, default false)"}
},
"required":["path"]
}`)
}

func (docWrite) ReadOnly() bool { return false }

func (w docWrite) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path     string          `json:"path"`
		Content  json.RawMessage `json:"content"`
		Sections json.RawMessage `json:"sections"`
		Title    string          `json:"title"`
		Append   bool            `json:"append"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if err := confine(w.roots, p.Path); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(strings.TrimSpace(p.Path))
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(abs), "."))
	// Binary docx: structured sections → real Word document.
	if ext == "docx" {
		var sections []DocSection
		if len(p.Sections) > 0 {
			if err := json.Unmarshal(p.Sections, &sections); err != nil {
				return "", fmt.Errorf("docx sections must be an array: %w", err)
			}
		}
		if err := writeDOCX(DocInput{Path: abs, Title: p.Title, Sections: sections}); err != nil {
			return "", err
		}
		return fmt.Sprintf("wrote %s (%d sections)", abs, len(sections)), nil
	}
	// Binary xlsx: structured {sheets:[...]} object OR a simple rows array.
	if ext == "xlsx" {
		trimmed := strings.TrimSpace(string(p.Content))
		// Structured form: content is an object with "sheets" (and a "path"
		// that may be omitted since the tool's path wins).
		if len(trimmed) > 0 && trimmed[0] == '{' {
			var wb XLSXWorkbook
			if err := json.Unmarshal(p.Content, &wb); err != nil {
				return "", fmt.Errorf("xlsx structured content invalid: %w", err)
			}
			wb.Path = abs
			n, err := XLSXWriteStructured(wb)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("wrote %s (%d sheets)", abs, n), nil
		}
		// Simple form: array of arrays (rows → Sheet1).
		var rows [][]string
		if err := json.Unmarshal(p.Content, &rows); err != nil {
			return "", fmt.Errorf("xlsx content must be an array of arrays (rows) or an object with sheets: %w", err)
		}
		if err := XLSXWriteRows(abs, rows); err != nil {
			return "", err
		}
		return fmt.Sprintf("wrote %s (%d rows)", abs, len(rows)), nil
	}
	var data []byte
	switch ext {
	case "json":
		// Pretty-print JSON content.
		var v any
		if err := json.Unmarshal(p.Content, &v); err != nil {
			return "", fmt.Errorf("json content invalid: %w", err)
		}
		data, err = json.MarshalIndent(v, "", "  ")
		if err != nil {
			return "", err
		}
	case "csv":
		var rows [][]string
		if err := json.Unmarshal(p.Content, &rows); err != nil {
			return "", fmt.Errorf("csv content must be an array of arrays: %w", err)
		}
		var b strings.Builder
		w := csv.NewWriter(&b)
		_ = w.WriteAll(rows)
		if err := w.Error(); err != nil {
			return "", err
		}
		data = []byte(b.String())
	default:
		// Text: content is a string.
		var s string
		if err := json.Unmarshal(p.Content, &s); err != nil {
			return "", errors.New("content must be a string for this format")
		}
		data = []byte(s)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	flag := os.O_CREATE | os.O_WRONLY
	if p.Append && (ext == "md" || ext == "txt" || ext == "html" || ext == "") {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(abs, flag, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	mode := "wrote"
	if p.Append {
		mode = "appended"
	}
	return fmt.Sprintf("%s %s (%d bytes)", mode, abs, len(data)), nil
}

// --- csv_read / csv_write aliases for discoverability -----------------------
// (The agent may reach for csv_read specifically; route to doc_read/write logic.)

type csvRead struct{}

func (csvRead) Name() string { return "csv_read" }
func (csvRead) Description() string {
	return "Read a .csv file and return it as a formatted table. Alias for doc_read on .csv, surfaced separately so it's discoverable for spreadsheet tasks. Returns up to 200k chars."
}
func (csvRead) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (csvRead) ReadOnly() bool { return true }
func (csvRead) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return docRead{}.Execute(ctx, args)
}

type csvWrite struct{ roots []string }

func (csvWrite) Name() string { return "csv_write" }
func (csvWrite) Description() string {
	return "Write rows to a .csv file. content is an array of arrays of strings (each inner array = one row). Overwrites by default; set append=true to append rows. Creates parent dirs."
}
func (csvWrite) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"description":"array of arrays of strings (rows)"},"append":{"type":"boolean"}},"required":["path","content"]}`)
}
func (csvWrite) ReadOnly() bool { return false }
func (w csvWrite) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return docWrite(w).Execute(ctx, args)
}

// --- xlsx_read / xlsx_write aliases for discoverability ---------------------
// (Binary spreadsheet access via the OOXML parser; surfaced separately so the
// agent reaches for the right tool on spreadsheet tasks.)

type xlsxRead struct{}

func (xlsxRead) Name() string { return "xlsx_read" }
func (xlsxRead) Description() string {
	return "Read a .xlsx spreadsheet and return its cells as a formatted table. Parses shared strings, inline strings, and numeric values. Binary format (no external deps — reads the OOXML zip directly). Returns up to 200k chars."
}
func (xlsxRead) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (xlsxRead) ReadOnly() bool { return true }
func (xlsxRead) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return docRead{}.Execute(ctx, args)
}

type xlsxWrite struct{ roots []string }

func (xlsxWrite) Name() string { return "xlsx_write" }
func (xlsxWrite) Description() string {
	return "Write rows to a real .xlsx spreadsheet file. content is an array of arrays of strings (each inner array = one row). Produces a valid .xlsx (one sheet, Sheet1) openable in Excel/WPS/LibreOffice. Creates parent dirs."
}
func (xlsxWrite) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"description":"array of arrays of strings (rows)"}},"required":["path","content"]}`)
}
func (xlsxWrite) ReadOnly() bool { return false }
func (w xlsxWrite) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return docWrite(w).Execute(ctx, args)
}

// --- doc_convert (md↔html, json pretty) -------------------------------------

type docConvert struct{ roots []string }

func (docConvert) Name() string { return "doc_convert" }

func (docConvert) Description() string {
	return "Convert a text document between formats: markdown→html, html→markdown (text), or pretty-print json. Reads from path, writes to out_path. Supported: md→html, html→md, json→json (pretty). Binary Office format conversion (docx↔pdf) is not supported — use the ppt tools for slides or export from the source app."
}

func (docConvert) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"Source file path"},
  "out_path":{"type":"string","description":"Output file path (extension determines target format)"},
  "format":{"type":"string","description":"Explicit target: \"html\", \"markdown\", \"text\", \"json\". Inferred from out_path extension when omitted."}
},
"required":["path","out_path"]
}`)
}

func (docConvert) ReadOnly() bool { return false }

func (w docConvert) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path    string `json:"path"`
		OutPath string `json:"out_path"`
		Format  string `json:"format"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if err := confine(w.roots, p.Path); err != nil {
		return "", err
	}
	if err := confine(w.roots, p.OutPath); err != nil {
		return "", err
	}
	src, err := filepath.Abs(strings.TrimSpace(p.Path))
	if err != nil {
		return "", err
	}
	dst, err := filepath.Abs(strings.TrimSpace(p.OutPath))
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	srcExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(src), "."))
	target := strings.ToLower(strings.TrimSpace(p.Format))
	if target == "" {
		target = strings.ToLower(strings.TrimPrefix(filepath.Ext(dst), "."))
	}

	var out []byte
	switch {
	case (srcExt == "md" || srcExt == "markdown") && (target == "html" || target == "htm"):
		out = []byte(markdownToHTML(string(data)))
	case (srcExt == "html" || srcExt == "htm") && (target == "md" || target == "markdown" || target == "text"):
		out = []byte(stripHTMLText(string(data)))
	case srcExt == "json" && target == "json":
		s, err := formatJSON(data)
		if err != nil {
			return "", err
		}
		out = []byte(s)
	default:
		return "", fmt.Errorf("unsupported conversion %s→%s (try md→html, html→md/text, json→json)", srcExt, target)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("converted %s → %s (%d bytes)", src, dst, len(out)), nil
}

// markdownToHTML is a minimal converter (headings, bold, italic, code, links,
// lists, paragraphs). Not a full CommonMark parser — sufficient for rendering
// agent-produced reports into a viewable HTML file. For richer rendering use the
// frontend's Markdown component.
func markdownToHTML(md string) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\"><style>body{font-family:sans-serif;max-width:760px;margin:2em auto;padding:0 1em;line-height:1.6}code{background:#f4f4f4;padding:2px 4px;border-radius:3px}pre{background:#f4f4f4;padding:1em;border-radius:6px;overflow:auto}</style></head><body>\n")
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "###### "):
			b.WriteString("<h6>" + inline(strings.TrimPrefix(t, "###### ")) + "</h6>\n")
		case strings.HasPrefix(t, "##### "):
			b.WriteString("<h5>" + inline(strings.TrimPrefix(t, "##### ")) + "</h5>\n")
		case strings.HasPrefix(t, "#### "):
			b.WriteString("<h4>" + inline(strings.TrimPrefix(t, "#### ")) + "</h4>\n")
		case strings.HasPrefix(t, "### "):
			b.WriteString("<h3>" + inline(strings.TrimPrefix(t, "### ")) + "</h3>\n")
		case strings.HasPrefix(t, "## "):
			b.WriteString("<h2>" + inline(strings.TrimPrefix(t, "## ")) + "</h2>\n")
		case strings.HasPrefix(t, "# "):
			b.WriteString("<h1>" + inline(strings.TrimPrefix(t, "# ")) + "</h1>\n")
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* "):
			b.WriteString("<li>" + inline(strings.TrimPrefix(strings.TrimPrefix(t, "- "), "* ")) + "</li>\n")
		case t == "":
			b.WriteString("<br>\n")
		default:
			b.WriteString("<p>" + inline(t) + "</p>\n")
		}
	}
	b.WriteString("</body></html>")
	return b.String()
}

// inline handles **bold**, *italic*, `code`.
func inline(s string) string {
	out := s
	// Bold: **x** → <strong>x</strong>
	for {
		i := strings.Index(out, "**")
		if i < 0 {
			break
		}
		j := strings.Index(out[i+2:], "**")
		if j < 0 {
			break
		}
		inner := out[i+2 : i+2+j]
		out = out[:i] + "<strong>" + inner + "</strong>" + out[i+2+j+2:]
	}
	out = wrapPairs(out, "`", "<code>", "</code>")
	out = wrapPairs(out, "*", "<em>", "</em>")
	return out
}

// wrapPairs replaces pairs of delim with open/close tags.
func wrapPairs(s, delim, openTag, closeTag string) string {
	var b strings.Builder
	isOpen := true
	for {
		i := strings.Index(s, delim)
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		if isOpen {
			b.WriteString(openTag)
		} else {
			b.WriteString(closeTag)
		}
		s = s[i+len(delim):]
		isOpen = !isOpen
	}
	return b.String()
}

// stripHTMLText is a local minimal tag stripper (the rag package has its own).
func stripHTMLText(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
