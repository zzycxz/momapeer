package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	fileenc "github.com/zzycxz/momapeer/internal/fileutil/encoding"
	"github.com/zzycxz/momapeer/internal/tool"
)

func init() { tool.RegisterBuiltin(applyPatch{}) }

type applyPatch struct {
	roots   []string
	workDir string
}

func (applyPatch) Name() string { return "apply_patch" }

func (applyPatch) Description() string {
	return `Apply a multi-file patch. Supports add, update, delete, and move operations in one call.
Patch format:
*** Begin Patch
*** Add File: path/to/new.go
+package new
+// content
*** Update File: path/to/existing.go
@@ optional context
 unchanged line
-old line
+new line
*** Delete File: path/to/old.go
*** End Patch
Use for multi-file refactoring. Two-phase commit: all hunks are validated before any file is modified.`
}

func (applyPatch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"patchText":{"type":"string","description":"The full patch text describing all changes"}},"required":["patchText"]}`)
}

func (applyPatch) ReadOnly() bool { return false }

// --- Patch types ---

type patchHunkType int

const (
	hunkAdd patchHunkType = iota
	hunkUpdate
	hunkDelete
)

type patchHunk struct {
	typ      patchHunkType
	path     string
	movePath string // for update with move
	contents string // for add: the new file content
	chunks   []updateChunk
}

type updateChunk struct {
	oldLines []string
	newLines []string
}

// --- Parser ---

func parsePatch(patchText string) ([]patchHunk, error) {
	normalized := strings.ReplaceAll(patchText, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")

	beginIdx := -1
	endIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "*** Begin Patch" {
			beginIdx = i
		}
		if strings.TrimSpace(line) == "*** End Patch" {
			endIdx = i
		}
	}
	if beginIdx == -1 || endIdx == -1 || beginIdx >= endIdx {
		return nil, fmt.Errorf("invalid patch format: missing *** Begin Patch / *** End Patch markers")
	}

	var hunks []patchHunk
	i := beginIdx + 1
	for i < endIdx {
		line := lines[i]

		if strings.HasPrefix(line, "*** Add File:") {
			path := strings.TrimSpace(line[len("*** Add File:"):])
			if path == "" {
				return nil, fmt.Errorf("line %d: empty file path for Add File", i+1)
			}
			content, nextIdx := parseAddContent(lines, i+1, endIdx)
			hunks = append(hunks, patchHunk{typ: hunkAdd, path: path, contents: content})
			i = nextIdx
			continue
		}

		if strings.HasPrefix(line, "*** Delete File:") {
			path := strings.TrimSpace(line[len("*** Delete File:"):])
			if path == "" {
				return nil, fmt.Errorf("line %d: empty file path for Delete File", i+1)
			}
			hunks = append(hunks, patchHunk{typ: hunkDelete, path: path})
			i++
			continue
		}

		if strings.HasPrefix(line, "*** Update File:") {
			path := strings.TrimSpace(line[len("*** Update File:"):])
			if path == "" {
				return nil, fmt.Errorf("line %d: empty file path for Update File", i+1)
			}
			hunk := patchHunk{typ: hunkUpdate, path: path}
			i++

			// Check for move directive.
			if i < endIdx && strings.HasPrefix(lines[i], "*** Move to:") {
				hunk.movePath = strings.TrimSpace(lines[i][len("*** Move to:"):])
				i++
			}

			chunks, nextIdx := parseUpdateChunks(lines, i, endIdx)
			hunk.chunks = chunks
			hunks = append(hunks, hunk)
			i = nextIdx
			continue
		}

		i++
	}

	if len(hunks) == 0 {
		return nil, fmt.Errorf("no hunks found in patch")
	}
	return hunks, nil
}

func parseAddContent(lines []string, start, end int) (string, int) {
	var content strings.Builder
	i := start
	for i < end && !strings.HasPrefix(lines[i], "***") {
		line := lines[i]
		if strings.HasPrefix(line, "+") {
			content.WriteString(line[1:])
			content.WriteByte('\n')
		}
		i++
	}
	return strings.TrimSuffix(content.String(), "\n"), i
}

func parseUpdateChunks(lines []string, start, end int) ([]updateChunk, int) {
	var chunks []updateChunk
	i := start

	for i < end && !strings.HasPrefix(lines[i], "***") {
		if !strings.HasPrefix(lines[i], "@@") {
			i++
			continue
		}
		i++ // skip @@ line

		var oldLines, newLines []string
		for i < end && !strings.HasPrefix(lines[i], "@@") && !strings.HasPrefix(lines[i], "***") {
			line := lines[i]
			if strings.HasPrefix(line, " ") {
				content := line[1:]
				oldLines = append(oldLines, content)
				newLines = append(newLines, content)
			} else if strings.HasPrefix(line, "-") {
				oldLines = append(oldLines, line[1:])
			} else if strings.HasPrefix(line, "+") {
				newLines = append(newLines, line[1:])
			}
			i++
		}

		chunks = append(chunks, updateChunk{oldLines: oldLines, newLines: newLines})
	}

	return chunks, i
}

// --- Apply update chunks ---

func deriveNewContent(original string, chunks []updateChunk) (string, error) {
	origLines := strings.Split(original, "\n")
	// Drop trailing empty element.
	if len(origLines) > 0 && origLines[len(origLines)-1] == "" {
		origLines = origLines[:len(origLines)-1]
	}

	// Build replacements: [startIdx, oldLen, newLines]
	type replacement struct {
		start    int
		oldLen   int
		newLines []string
	}
	var replacements []replacement
	lineIdx := 0

	for _, chunk := range chunks {
		if len(chunk.oldLines) == 0 {
			// Pure addition at end.
			insertIdx := len(origLines)
			replacements = append(replacements, replacement{start: insertIdx, oldLen: 0, newLines: chunk.newLines})
			continue
		}

		found := seekSequence(origLines, chunk.oldLines, lineIdx)
		if found == -1 {
			// Retry without trailing empty line.
			pattern := chunk.oldLines
			newSlice := chunk.newLines
			if len(pattern) > 0 && pattern[len(pattern)-1] == "" {
				pattern = pattern[:len(pattern)-1]
				if len(newSlice) > 0 && newSlice[len(newSlice)-1] == "" {
					newSlice = newSlice[:len(newSlice)-1]
				}
				found = seekSequence(origLines, pattern, lineIdx)
			}
			if found == -1 {
				return "", fmt.Errorf("failed to find expected lines:\n%s", strings.Join(chunk.oldLines, "\n"))
			}
			replacements = append(replacements, replacement{start: found, oldLen: len(pattern), newLines: newSlice})
			lineIdx = found + len(pattern)
		} else {
			replacements = append(replacements, replacement{start: found, oldLen: len(chunk.oldLines), newLines: chunk.newLines})
			lineIdx = found + len(chunk.oldLines)
		}
	}

	// Apply in reverse order.
	result := make([]string, len(origLines))
	copy(result, origLines)
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		before := result[:r.start]
		after := result[r.start+r.oldLen:]
		result = append(append(before, r.newLines...), after...)
	}

	// Ensure trailing newline.
	if len(result) == 0 || result[len(result)-1] != "" {
		result = append(result, "")
	}
	return strings.Join(result, "\n"), nil
}

// seekSequence finds pattern in lines starting from startIdx.
// Tries exact match first, then trimmed match.
func seekSequence(lines, pattern []string, startIdx int) int {
	if len(pattern) == 0 {
		return -1
	}
	// Pass 1: exact match.
	for i := startIdx; i <= len(lines)-len(pattern); i++ {
		match := true
		for j := 0; j < len(pattern); j++ {
			if lines[i+j] != pattern[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	// Pass 2: trimmed match.
	for i := startIdx; i <= len(lines)-len(pattern); i++ {
		match := true
		for j := 0; j < len(pattern); j++ {
			if strings.TrimSpace(lines[i+j]) != strings.TrimSpace(pattern[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// --- Tool execution ---

func (a applyPatch) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		PatchText string `json:"patchText"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.PatchText == "" {
		return "", fmt.Errorf("patchText is required")
	}

	hunks, err := parsePatch(p.PatchText)
	if err != nil {
		return "", fmt.Errorf("apply_patch parse failed: %w", err)
	}

	// Phase 1: Validate all hunks.
	type fileChange struct {
		path       string
		movePath   string
		oldContent string
		newContent string
		changeType string       // "add", "update", "delete", "move"
		enc        fileenc.Kind // detected encoding to preserve on write (update/move)
	}
	var changes []fileChange

	for _, hunk := range hunks {
		filePath := resolveIn(a.workDir, hunk.path)
		if err := confine(a.roots, filePath); err != nil {
			return "", err
		}

		switch hunk.typ {
		case hunkAdd:
			changes = append(changes, fileChange{
				path:       filePath,
				newContent: hunk.contents,
				changeType: "add",
			})

		case hunkDelete:
			if _, err := os.Stat(filePath); err != nil {
				return "", fmt.Errorf("apply_patch verify: cannot delete %s: %w", filePath, err)
			}
			oldContent, _, err := readFileEncoded(filePath)
			if err != nil {
				return "", fmt.Errorf("apply_patch verify: read %s: %w", filePath, err)
			}
			changes = append(changes, fileChange{
				path:       filePath,
				oldContent: oldContent,
				changeType: "delete",
			})

		case hunkUpdate:
			if _, err := os.Stat(filePath); err != nil {
				return "", fmt.Errorf("apply_patch verify: cannot update %s: %w", filePath, err)
			}
			oldContent, enc, err := readFileEncoded(filePath)
			if err != nil {
				return "", fmt.Errorf("apply_patch verify: read %s: %w", filePath, err)
			}
			newContent, err := deriveNewContent(oldContent, hunk.chunks)
			if err != nil {
				return "", fmt.Errorf("apply_patch verify: %w", err)
			}

			movePath := ""
			if hunk.movePath != "" {
				movePath = resolveIn(a.workDir, hunk.movePath)
				if err := confine(a.roots, movePath); err != nil {
					return "", err
				}
			}

			changes = append(changes, fileChange{
				path:       filePath,
				movePath:   movePath,
				oldContent: oldContent,
				newContent: newContent,
				changeType: map[string]string{"": "update", "move": "move"}[func() string {
					if movePath != "" {
						return "move"
					}
					return ""
				}()],
				enc: enc,
			})
		}
	}

	// Phase 2: Apply changes.
	var summary []string
	for _, c := range changes {
		switch c.changeType {
		case "add":
			dir := filepath.Dir(c.path)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("mkdir %s: %w", dir, err)
			}
			if err := writeFileEncoded(c.path, c.newContent, 0); err != nil {
				return "", fmt.Errorf("write %s: %w", c.path, err)
			}
			summary = append(summary, fmt.Sprintf("A %s", c.path))

		case "update":
			// Preserve the original file's encoding (GBK/UTF-16/etc.) on write —
			// re-encoding to UTF-8 would silently corrupt non-UTF-8 sources.
			if err := writeFileEncoded(c.path, c.newContent, c.enc); err != nil {
				return "", fmt.Errorf("write %s: %w", c.path, err)
			}
			summary = append(summary, fmt.Sprintf("M %s", c.path))

		case "move":
			dir := filepath.Dir(c.movePath)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("mkdir %s: %w", dir, err)
			}
			// Carry the source file's encoding to the destination.
			if err := writeFileEncoded(c.movePath, c.newContent, c.enc); err != nil {
				return "", fmt.Errorf("write %s: %w", c.movePath, err)
			}
			os.Remove(c.path)
			summary = append(summary, fmt.Sprintf("M %s -> %s", c.path, c.movePath))

		case "delete":
			if err := os.Remove(c.path); err != nil {
				return "", fmt.Errorf("delete %s: %w", c.path, err)
			}
			summary = append(summary, fmt.Sprintf("D %s", c.path))
		}
	}

	return fmt.Sprintf("apply_patch: %d files changed\n%s", len(summary), strings.Join(summary, "\n")), nil
}
