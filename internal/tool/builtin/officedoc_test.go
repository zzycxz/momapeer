package builtin

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestXLSXWriteReadRoundtrip writes a real .xlsx via our OOXML builder, then
// reads it back via the parser — end-to-end without Excel/excelize. Guards the
// hand-built zip+XML against the hand-built parser (both must agree).
func TestXLSXWriteReadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.xlsx")
	rows := [][]string{
		{"name", "age", "city"},
		{"Alice", "30", "Beijing"},
		{"Bob", "25", "Shanghai"},
		{"Carol", "41", "Shenzhen"},
	}
	if err := XLSXWriteRows(path, rows); err != nil {
		t.Fatal(err)
	}

	got, err := readXLSX(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(rows) {
		t.Fatalf("row count = %d, want %d", len(got), len(rows))
	}
	// Check a few cells (shared-strings path).
	if got[0][0] != "name" || got[1][1] != "30" || got[2][2] != "Shanghai" {
		t.Errorf("cell mismatch:\n%v", got)
	}
}

// TestXLSXReadViaDocRead confirms doc_read dispatches .xlsx to the parser and
// returns a formatted table.
func TestXLSXReadViaDocRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.xlsx")
	XLSXWriteRows(path, [][]string{{"h1", "h2"}, {"v1", "v2"}})
	out, err := (docRead{}).Execute(context.Background(), toArgs(t, map[string]any{"path": path}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "v1") || !strings.Contains(out, "h1") {
		t.Errorf("doc_read xlsx missing data: %s", out)
	}
}

// TestXLSXWriteViaXlsxWriteTool confirms the xlsx_write tool path produces a
// readable file.
func TestXLSXWriteViaXlsxWriteTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.xlsx")
	_, err := (xlsxWrite{}).Execute(context.Background(), toArgs(t, map[string]any{
		"path":    path,
		"content": [][]string{{"a", "b"}, {"1", "2"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := readXLSX(path)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "a" {
		t.Errorf("xlsx_write produced wrong data: %v", rows)
	}
}

// TestColToIndex / TestIndexToCol removed: the hand-rolled column-letter
// conversion helpers were deleted when xlsx moved to excelize (which provides
// its own CoordinatesToCellName). The xlsx roundtrip tests below exercise the
// real read+write path end-to-end through excelize.

func TestNormalizeNumber(t *testing.T) {
	if got := normalizeNumber("42.0"); got != "42" {
		t.Errorf("normalizeNumber(42.0) = %q, want 42", got)
	}
	if got := normalizeNumber("3.14"); got != "3.14" {
		t.Errorf("normalizeNumber(3.14) = %q, want 3.14", got)
	}
}
