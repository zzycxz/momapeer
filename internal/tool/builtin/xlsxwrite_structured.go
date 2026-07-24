package builtin

// xlsxwrite_structured.go upgrades xlsx generation beyond XLSXWriteRows (which
// only dumps a 2D string array into Sheet1). XLSXWriteStructured accepts a
// full workbook description: multiple sheets, per-cell value/formula, cell
// styles (font/color/bold/background/alignment/number format), merged ranges,
// and column widths — all via excelize (already a dependency).
//
// The input is a structured JSON object (see XLSXWorkbook) so the agent can
// express rich spreadsheets the way doc_write/docx expresses rich documents.
// XLSXWriteRows stays for the simple rows-array case (back-compat).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// XLSXCell is one cell: a value XOR a formula, plus an optional style and
// number format. Ref is the A1 reference (e.g. "B3"); required.
type XLSXCell struct {
	Ref     string    `json:"ref"`     // A1 reference, e.g. "B3" (required)
	Value   *string   `json:"value"`   // literal value (string form; numbers/bools coerced)
	Formula *string   `json:"formula"` // e.g. "=SUM(B2:B5)" (overrides value when set)
	Format  string    `json:"format"`  // number format code, e.g. "#,##0", "0.00%", "yyyy-mm-dd"
	Style   XLSXStyle `json:"style"`
}

// XLSXStyle mirrors the run/cell style vocabulary shared with docx, plus a few
// xlsx-specifics (vertical align, border). Colors are "#RRGGBB" (we strip #).
type XLSXStyle struct {
	Bold   bool   `json:"bold"`
	Italic bool   `json:"italic"`
	Color  string `json:"color"`  // font color "#RRGGBB"
	Bg     string `json:"bg"`     // cell fill "#RRGGBB"
	Size   int    `json:"size"`   // font size in points (not half-points; xlsx uses real pts)
	Font   string `json:"font"`   // font family
	Align  string `json:"align"`  // "left"|"center"|"right"
	Wrap   bool   `json:"wrap"`   // wrap text in cell
	Border bool   `json:"border"` // thin border all sides
}

// XLSXMerge is a merged range, A1 notation (e.g. "A1:C1").
type XLSXMerge struct {
	Range string `json:"range"`
}

// XLSXColWidth sets a column's width by letter (e.g. {"A": 20}).
type XLSXColWidth struct {
	Col   string  `json:"col"`
	Width float64 `json:"width"`
}

// XLSXSheet is one worksheet.
type XLSXSheet struct {
	Name      string         `json:"name"`  // sheet tab name (default "Sheet1")
	Cells     []XLSXCell     `json:"cells"` // sparse cells by ref
	Merges    []XLSXMerge    `json:"merges"`
	ColWidths []XLSXColWidth `json:"col_widths"`
}

// XLSXWorkbook is the structured-write payload.
type XLSXWorkbook struct {
	Path   string      `json:"path"`
	Sheets []XLSXSheet `json:"sheets"`
}

// XLSXWriteStructured writes a multi-sheet styled workbook via excelize.
// Produces a fully-valid .xlsx openable in Excel/WPS/LibreOffice. Returns the
// sheet count for the success message.
func XLSXWriteStructured(wb XLSXWorkbook) (int, error) {
	if err := os.MkdirAll(filepath.Dir(wb.Path), 0o755); err != nil {
		return 0, err
	}
	f := excelize.NewFile()
	defer f.Close()
	// Rename the default "Sheet1" to the first requested sheet, or add new
	// sheets for subsequent ones. Excelize creates a default Sheet1 we reuse.
	for i, sh := range wb.Sheets {
		name := strings.TrimSpace(sh.Name)
		if name == "" {
			name = fmt.Sprintf("Sheet%d", i+1)
		}
		var sheet string
		var err error
		if i == 0 {
			// Reuse the default sheet by renaming it.
			if name != "Sheet1" {
				if err = f.SetSheetName("Sheet1", name); err != nil {
					return 0, err
				}
			}
			sheet = name
		} else {
			idx, addErr := f.NewSheet(name)
			if addErr != nil {
				return 0, addErr
			}
			sheet = name
			f.SetActiveSheet(idx)
		}
		if err := writeSheet(f, sheet, sh); err != nil {
			return 0, err
		}
	}
	if err := f.SaveAs(wb.Path); err != nil {
		return 0, err
	}
	return len(wb.Sheets), nil
}

// writeSheet applies cells/merges/col-widths to one sheet.
func writeSheet(f *excelize.File, sheet string, sh XLSXSheet) error {
	// Column widths first (so styled cells land in correctly-sized columns).
	for _, cw := range sh.ColWidths {
		col := strings.TrimSpace(cw.Col)
		if col == "" || cw.Width <= 0 {
			continue
		}
		if err := f.SetColWidth(sheet, col, col, cw.Width); err != nil {
			return fmt.Errorf("col width %s: %w", col, err)
		}
	}
	// Cells.
	for _, c := range sh.Cells {
		ref := strings.TrimSpace(c.Ref)
		if ref == "" {
			continue
		}
		if c.Formula != nil && *c.Formula != "" {
			if err := f.SetCellFormula(sheet, ref, *c.Formula); err != nil {
				return fmt.Errorf("formula %s: %w", ref, err)
			}
		} else if c.Value != nil {
			if err := f.SetCellValue(sheet, ref, *c.Value); err != nil {
				return fmt.Errorf("value %s: %w", ref, err)
			}
		}
		// Number format.
		if fmtCode := strings.TrimSpace(c.Format); fmtCode != "" {
			styleID, err := f.NewStyle(&excelize.Style{CustomNumFmt: &fmtCode})
			if err == nil {
				_ = f.SetCellStyle(sheet, ref, ref, styleID)
			}
		}
		// Cell style (font/fill/align/border). Applied after value so the
		// style isn't overwritten by SetCellValue.
		if !isStyleEmpty(c.Style) {
			styleID, err := f.NewStyle(xlsxStyleFrom(c.Style))
			if err != nil {
				return fmt.Errorf("style %s: %w", ref, err)
			}
			if err := f.SetCellStyle(sheet, ref, ref, styleID); err != nil {
				return fmt.Errorf("apply style %s: %w", ref, err)
			}
		}
	}
	// Merges.
	for _, m := range sh.Merges {
		r := strings.TrimSpace(m.Range)
		if r == "" || !strings.Contains(r, ":") {
			continue
		}
		parts := strings.SplitN(r, ":", 2)
		if err := f.MergeCell(sheet, strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])); err != nil {
			return fmt.Errorf("merge %s: %w", r, err)
		}
	}
	return nil
}

// xlsxStyleFrom compiles our XLSXStyle into an excelize.Style. Colors drop the
// leading # (excelize wants RRGGBB). Fill uses a solid pattern.
func xlsxStyleFrom(s XLSXStyle) *excelize.Style {
	st := &excelize.Style{}
	font := &excelize.Font{Bold: s.Bold, Italic: s.Italic}
	if s.Color != "" {
		font.Color = hexNoHash(s.Color)
	}
	if s.Font != "" {
		font.Family = s.Font
	}
	if s.Size > 0 {
		font.Size = float64(s.Size)
	}
	st.Font = font
	if s.Bg != "" {
		st.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{hexNoHash(s.Bg)}}
	}
	if s.Align != "" || s.Wrap {
		al := &excelize.Alignment{WrapText: s.Wrap}
		switch strings.ToLower(s.Align) {
		case "left":
			al.Horizontal = "left"
		case "center":
			al.Horizontal = "center"
		case "right":
			al.Horizontal = "right"
		}
		st.Alignment = al
	}
	if s.Border {
		st.Border = []excelize.Border{
			{Type: "left", Color: "999999", Style: 1},
			{Type: "right", Color: "999999", Style: 1},
			{Type: "top", Color: "999999", Style: 1},
			{Type: "bottom", Color: "999999", Style: 1},
		}
	}
	return st
}

func isStyleEmpty(s XLSXStyle) bool {
	return !s.Bold && !s.Italic && !s.Wrap && !s.Border &&
		s.Color == "" && s.Bg == "" && s.Size == 0 && s.Font == "" && s.Align == ""
}
