package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocWriteReadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.md")
	content := "# Report\n\nBody text here."

	// Write markdown.
	out, err := (docWrite{}).Execute(context.Background(), toArgs(t, map[string]any{
		"path":    path,
		"content": content,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "wrote") {
		t.Errorf("write output: %s", out)
	}

	// Read it back.
	got, err := (docRead{}).Execute(context.Background(), toArgs(t, map[string]any{"path": path}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Report") || !strings.Contains(got, "Body text") {
		t.Errorf("read-back mismatch: %s", got)
	}
}

func TestDocWriteCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")
	rows := [][]string{{"name", "age"}, {"Alice", "30"}, {"Bob", "25"}}
	_, err := (docWrite{}).Execute(context.Background(), toArgs(t, map[string]any{
		"path":    path,
		"content": rows,
	}))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Alice") || !strings.Contains(string(data), "name,age") {
		t.Errorf("csv content: %s", string(data))
	}

	// Read formats as a table.
	got, err := (csvRead{}).Execute(context.Background(), toArgs(t, map[string]any{"path": path}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Alice") {
		t.Errorf("csv read: %s", got)
	}
}

func TestDocWriteJSONPretty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conf.json")
	_, err := (docWrite{}).Execute(context.Background(), toArgs(t, map[string]any{
		"path":    path,
		"content": map[string]any{"key": "value", "n": 42},
	}))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	// Pretty-printed JSON has newlines.
	if !strings.Contains(string(data), "\n  \"key\"") {
		t.Errorf("json not pretty-printed: %s", string(data))
	}
	// Read pretty-prints.
	got, _ := (docRead{}).Execute(context.Background(), toArgs(t, map[string]any{"path": path}))
	if !strings.Contains(got, "value") {
		t.Errorf("json read: %s", got)
	}
}

func TestDocConvertMDToHTML(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.md")
	dst := filepath.Join(dir, "out.html")
	os.WriteFile(src, []byte("# Title\n\n**bold** text"), 0o644)
	out, err := (docConvert{}).Execute(context.Background(), toArgs(t, map[string]any{
		"path": src, "out_path": dst,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "converted") {
		t.Errorf("convert output: %s", out)
	}
	data, _ := os.ReadFile(dst)
	html := string(data)
	for _, want := range []string{"<h1>Title</h1>", "<strong>bold</strong>"} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q: %s", want, html)
		}
	}
}

func TestDocConvertUnsupported(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "b.docx")
	os.WriteFile(src, []byte("hi"), 0o644)
	_, err := (docConvert{}).Execute(context.Background(), toArgs(t, map[string]any{
		"path": src, "out_path": dst,
	}))
	if err == nil {
		t.Error("unsupported conversion should error")
	}
}

func TestDocWriteAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	mustExec(t, docWrite{}, map[string]any{"path": path, "content": "line1\n"})
	mustExec(t, docWrite{}, map[string]any{"path": path, "content": "line2\n", "append": true})
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "line1") || !strings.Contains(string(data), "line2") {
		t.Errorf("append failed: %s", string(data))
	}
}

func mustExec(t *testing.T, tool interface {
	Execute(context.Context, json.RawMessage) (string, error)
}, args any) string {
	t.Helper()
	out, err := tool.Execute(context.Background(), toArgs(t, args))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// toArgs marshals a value to JSON RawMessage for tool args. Named to avoid
// clashing with apply_patch_test.go's mustJSON(string).
func toArgs(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
