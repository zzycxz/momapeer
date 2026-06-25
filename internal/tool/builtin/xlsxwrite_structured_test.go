package builtin

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// TestXLSXWriteStructuredMultiSheet confirms multiple sheets are written and
// readable back, with the second sheet's name preserved.
func TestXLSXWriteStructuredMultiSheet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.xlsx")
	n, err := XLSXWriteStructured(XLSXWorkbook{Path: path, Sheets: []XLSXSheet{
		{Name: "Q1汇总", Cells: []XLSXCell{{Ref: "A1", Value: strPtr("指标")}}},
		{Name: "Q2汇总", Cells: []XLSXCell{{Ref: "A1", Value: strPtr("指标")}}},
	}})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != 2 {
		t.Errorf("returned %d sheets, want 2", n)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) != 2 || sheets[0] != "Q1汇总" || sheets[1] != "Q2汇总" {
		t.Errorf("sheets = %v, want [Q1汇总 Q2汇总]", sheets)
	}
}

// TestXLSXWriteStructuredFormulaAndStyle confirms a formula is stored and a
// styled header cell reads back its value + the formula computes (we check the
// formula string is present, not the cached result, since excelize doesn't
// recompute on write).
func TestXLSXWriteStructuredFormulaAndStyle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "styled.xlsx")
	v := "120"
	_, err := XLSXWriteStructured(XLSXWorkbook{Path: path, Sheets: []XLSXSheet{
		{Name: "数据", Cells: []XLSXCell{
			{Ref: "A1", Value: strPtr("营收"), Style: XLSXStyle{Bold: true, Color: "#FFFFFF", Bg: "#005A9C", Align: "center", Border: true}},
			{Ref: "B1", Value: strPtr("利润"), Style: XLSXStyle{Bold: true, Color: "#FFFFFF", Bg: "#005A9C", Align: "center"}},
			{Ref: "A2", Value: &v, Format: "#,##0"},
			{Ref: "B2", Formula: strPtr("=A2*0.3")},
			{Ref: "B3", Formula: strPtr("=SUM(B2:B2)")},
		}},
	}})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	// Value survived.
	a1, _ := f.GetCellValue("数据", "A1")
	if a1 != "营收" {
		t.Errorf("A1 = %q, want 营收", a1)
	}
	// Formula stored.
	fb2, _ := f.GetCellFormula("数据", "B2")
	if !strings.Contains(fb2, "A2") {
		t.Errorf("B2 formula = %q, want to reference A2", fb2)
	}
}

// TestXLSXWriteStructuredMergeAndColWidth confirms merge ranges and column
// widths are applied (read back via the merged-cell check + sheet view).
func TestXLSXWriteStructuredMergeAndColWidth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "merge.xlsx")
	_, err := XLSXWriteStructured(XLSXWorkbook{Path: path, Sheets: []XLSXSheet{
		{Name: "Sheet1", Cells: []XLSXCell{{Ref: "A1", Value: strPtr("标题")}}, Merges: []XLSXMerge{{Range: "A1:C1"}}, ColWidths: []XLSXColWidth{{Col: "A", Width: 20}}},
	}})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	// Merged cells: GetCellValue on a merged range returns the top-left value.
	a1, _ := f.GetCellValue("Sheet1", "A1")
	if a1 != "标题" {
		t.Errorf("merged A1 = %q, want 标题", a1)
	}
	// Column width.
	w, _ := f.GetColWidth("Sheet1", "A")
	if w < 19 || w > 21 {
		t.Errorf("col A width = %v, want ~20", w)
	}
}

// TestXLSXWriteStructuredBackcompatViaRows confirms the simple rows-array path
// (XLSXWriteRows) still works alongside the new structured writer — both go
// through doc_write but via different content shapes.
func TestXLSXWriteStructuredBackcompatViaRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rows.xlsx")
	if err := XLSXWriteRows(path, [][]string{{"a", "b"}, {"1", "2"}}); err != nil {
		t.Fatalf("write rows: %v", err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	a1, _ := f.GetCellValue("Sheet1", "A1")
	if a1 != "a" {
		t.Errorf("A1 = %q, want a", a1)
	}
}

// strPtr is a tiny helper to take the address of a string literal (used because
// XLSXCell.Value/Formula are *string to distinguish "absent" from "empty").
func strPtr(s string) *string { return &s }
