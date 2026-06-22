package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zzycxz/momapeer/internal/tool"
)

func init() { tool.RegisterBuiltin(editFile{}) }

// editFile replaces an exact string in a file. roots confines the target to the
// workspace when non-empty (see writeFile); workDir, when non-empty, is the
// directory a relative path resolves against (see resolveIn).
type editFile struct {
	roots   []string
	workDir string
}

func (editFile) Name() string { return "edit_file" }

func (editFile) Description() string {
	return "Replace text in a file. Uses fuzzy matching: exact match first, then tries line-trim, indent-normalize, and block-anchor matching. old_string must occur exactly once; add surrounding context to disambiguate. Use for targeted edits instead of rewriting the whole file."
}

func (editFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"},"old_string":{"type":"string","description":"Exact text to replace (must be unique in the file)"},"new_string":{"type":"string","description":"Replacement text (may be empty to delete)"}},"required":["path","old_string","new_string"]}`)
}

func (editFile) ReadOnly() bool { return false }

func (e editFile) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if p.OldString == "" {
		return "", fmt.Errorf("old_string is required")
	}
	p.Path = resolveIn(e.workDir, p.Path)
	if err := confine(e.roots, p.Path); err != nil {
		return "", err
	}

	content, enc, err := readFileEncoded(p.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.Path, err)
	}

	old, newStr := matchLineEndings(content, p.OldString, p.NewString)
	region, found, unique := fuzzyMatch(content, old)
	if !found {
		return "", fmt.Errorf("old_string not found in %s", p.Path)
	}
	if !unique {
		return "", fmt.Errorf("old_string is not unique in %s; add more surrounding context", p.Path)
	}

	// Guard against fuzzy-match fallback returning a region that is not an
	// exact substring of the file content (e.g. a fully-stripped
	// indent-normalized version).  strings.Replace would silently replace
	// zero occurrences, reporting success while making no change.
	if !strings.Contains(content, region) {
		return "", fmt.Errorf(
			"fuzzy match found %q but it does not appear verbatim in %s; "+
				"please supply the exact text from the file (including indentation)", old, p.Path)
	}

	updated := strings.Replace(content, region, newStr, 1)
	if err := writeFileEncoded(p.Path, updated, enc); err != nil {
		return "", fmt.Errorf("write %s: %w", p.Path, err)
	}
	return fmt.Sprintf("edited %s", p.Path), nil
}
