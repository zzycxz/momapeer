// Package docconv centralizes the external Python-script document-conversion
// helpers (markitdown via doc_converter.py, and PDF OCR via ocr_pdf.py) that
// were previously duplicated across three packages (rag, tool/builtin, control).
//
// The conversion logic, script discovery, and Python-process invocation are
// byte-for-byte identical in all three call sites; this package collapses them
// into one implementation. Each caller keeps its own fallback policy (e.g.
// rag falls back to a Go docx parser when markitdown is absent) — this package
// only owns the shared "find script → run python → parse JSON" plumbing.
package docconv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/zzycxz/momapeer/internal/proc"
)

// Result is the JSON shape emitted by doc_converter.py (and ocr_pdf.py).
// Text holds the converted markdown/text; Title is the detected document
// title (may be empty); Error carries a Python-side error message.
type Result struct {
	Text  string `json:"text"`
	Title string `json:"title"`
	Error string `json:"error"`
}

// DefaultTimeout is the max time allowed for a single conversion subprocess.
// markitdown uses 3 min; PDF OCR (PaddleOCR over many pages) needs longer and
// overrides this via ConvertFile.
const DefaultTimeout = 3 * time.Minute

// FindScript searches the conventional locations for a bundled Python script
// (e.g. "doc_converter.py", "ocr_pdf.py"): the current working directory first,
// then next to the running executable and up to two parent directories. Returns
// "" when not found. This is the single source of truth that the three former
// duplicated finders resolved to.
func FindScript(name string) string {
	for _, c := range ScriptCandidates(name) {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// ScriptCandidates returns the ordered list of paths to probe for a bundled
// script of the given name. Exposed so tests can inspect/override the list.
func ScriptCandidates(name string) []string {
	candidates := []string{name}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, name),
			filepath.Join(dir, "..", name),
			filepath.Join(dir, "..", "..", name),
		)
	}
	return candidates
}

// pythonExe returns the Python executable name for the current platform:
// "python" on Windows, "python3" elsewhere. Matches the prior per-package logic.
func pythonExe() string {
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

// ConvertFile runs the given Python script against `path`, capturing its JSON
// stdout into a Result. A zero timeout uses DefaultTimeout. The error wraps
// both the process exit error (with stderr) and any JSON-parse failure, and
// surfaces a Python-emitted error field, so callers can distinguish causes.
//
// This is the unified replacement for rag.convertWithMarkitdown,
// builtin.convertDocWithMarkitdown, and control.convertFileWithMarkitdown.
func ConvertFile(scriptPath, path string, timeout time.Duration) (Result, error) {
	var res Result
	if scriptPath == "" {
		return res, fmt.Errorf("doc_converter.py not found")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, pythonExe(), scriptPath, path)
	proc.HideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return res, fmt.Errorf("doc_converter: %w: %s", err, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		return res, fmt.Errorf("doc_converter parse: %w", err)
	}
	if res.Error != "" {
		return res, fmt.Errorf("doc_converter: %s", res.Error)
	}
	return res, nil
}

// ConvertText is a convenience wrapper that runs doc_converter.py (located via
// FindScript) and returns just the converted text, preserving the exact
// signature the three call sites used (string text + error). Returns ("", err)
// when the script is missing or fails — callers apply their own fallback.
func ConvertText(path string) (string, error) {
	res, err := ConvertFile(FindScript("doc_converter.py"), path, DefaultTimeout)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}
