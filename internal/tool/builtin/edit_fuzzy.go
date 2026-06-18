package builtin

import (
	"strings"
	"unicode"
)

// fuzzyMatch tries to find old in content using progressively looser strategies.
// Returns (matchedRegion, found, unique). If found but not unique, the caller
// should report "not unique" rather than "not found".
//
// Level 1: Exact substring (current behavior).
// Level 2: Line-trim match — trim each line of old, compare against trimmed
//
//	content lines, then map back to the original untrimmed region.
//
// Level 3: Indent-normalize match — strip common leading whitespace from both
//
//	old and content, then compare.
//
// Level 4: Block-anchor match — match old's first and last lines exactly, then
//
//	verify the middle lines exist in order (allows reordering/extra lines).
func fuzzyMatch(content, old string) (string, bool, bool) {
	if old == "" {
		return "", false, false
	}

	// Level 1: Exact match.
	count := strings.Count(content, old)
	if count == 1 {
		return old, true, true
	}
	if count > 1 {
		return old, true, false // found but not unique
	}

	// Level 2: Line-trim match.
	if region, ok, unique := lineTrimMatch(content, old); ok {
		return region, ok, unique
	}

	// Level 3: Indent-normalize match.
	if region, ok, unique := indentNormMatch(content, old); ok {
		return region, ok, unique
	}

	// Level 4: Block-anchor match.
	if region, ok, unique := blockAnchorMatch(content, old); ok {
		return region, ok, unique
	}

	return "", false, false
}

// lineTrimMatch trims each line of old and content, finds the trimmed old in
// trimmed content, then maps back to the original untrimmed region.
func lineTrimMatch(content, old string) (string, bool, bool) {
	oldLines := strings.Split(old, "\n")
	if len(oldLines) <= 1 {
		// Single-line: try matching after trimming both sides.
		trimmedOld := strings.TrimSpace(old)
		if trimmedOld == "" || trimmedOld == old {
			return "", false, false
		}
		// Search each trimmed content line for the trimmed old.
		contentLines := strings.Split(content, "\n")
		matchCount := 0
		var matchRegion string
		for _, line := range contentLines {
			if strings.TrimSpace(line) == trimmedOld {
				matchCount++
				matchRegion = line // return the original untrimmed line
			}
		}
		if matchCount == 1 {
			return matchRegion, true, true
		}
		if matchCount > 1 {
			return matchRegion, true, false
		}
		return "", false, false
	}

	// Multi-line: build a trimmed version and match against trimmed content.
	trimmedOld := trimLines(oldLines)
	if trimmedOld == "" {
		return "", false, false
	}

	// Check if trimmed old appears in trimmed content.
	contentLines := strings.Split(content, "\n")
	trimmedContentLines := make([]string, len(contentLines))
	for i, line := range contentLines {
		trimmedContentLines[i] = strings.TrimSpace(line)
	}
	trimmedContent := strings.Join(trimmedContentLines, "\n")

	count := strings.Count(trimmedContent, trimmedOld)
	if count == 1 {
		// Find the position in trimmed content and map back to original.
		idx := strings.Index(trimmedContent, trimmedOld)
		if region, ok := mapTrimmedToOriginal(content, trimmedContent, idx, len(trimmedOld)); ok {
			return region, true, true
		}
		// Fallback: return trimmed version (will work for Replace).
		return trimmedOld, true, true
	}
	if count > 1 {
		return trimmedOld, true, false
	}
	return "", false, false
}

// indentNormMatch strips all leading whitespace from each line of both old and
// content, then tries exact match on the fully-stripped versions.
func indentNormMatch(content, old string) (string, bool, bool) {
	oldNorm := stripAllIndent(old)
	if oldNorm == old {
		return "", false, false // no change, already tried exact
	}

	contentNorm := stripAllIndent(content)
	count := strings.Count(contentNorm, oldNorm)
	if count == 1 {
		// Find the position in normalized content and map back.
		idx := strings.Index(contentNorm, oldNorm)
		if region, ok := mapNormalizedToOriginal(content, contentNorm, idx, len(oldNorm)); ok {
			return region, true, true
		}
		return oldNorm, true, true
	}
	if count > 1 {
		return oldNorm, true, false
	}
	return "", false, false
}

// blockAnchorMatch finds old's first and last non-empty lines in content, then
// verifies the middle lines appear in order between them.
func blockAnchorMatch(content, old string) (string, bool, bool) {
	oldLines := strings.Split(old, "\n")
	firstLine := strings.TrimSpace(oldLines[0])
	lastLine := strings.TrimSpace(oldLines[len(oldLines)-1])

	if firstLine == "" || lastLine == "" || len(oldLines) < 3 {
		return "", false, false // need at least 3 lines for anchor matching
	}

	// Find all occurrences of firstLine in content.
	contentLines := strings.Split(content, "\n")
	var candidates []int
	for i, line := range contentLines {
		if strings.TrimSpace(line) == firstLine {
			candidates = append(candidates, i)
		}
	}

	if len(candidates) == 0 {
		return "", false, false
	}

	// For each candidate first line, check if lastLine follows with matching middle.
	matches := 0
	var lastRegion string
	for _, startIdx := range candidates {
		// Look for lastLine after startIdx.
		for endIdx := startIdx + 2; endIdx < len(contentLines); endIdx++ {
			if strings.TrimSpace(contentLines[endIdx]) != lastLine {
				continue
			}
			// Check middle lines exist in order.
			middleOld := make([]string, 0)
			for _, line := range oldLines[1 : len(oldLines)-1] {
				if t := strings.TrimSpace(line); t != "" {
					middleOld = append(middleOld, t)
				}
			}
			middleContent := make([]string, 0)
			for _, line := range contentLines[startIdx+1 : endIdx] {
				if t := strings.TrimSpace(line); t != "" {
					middleContent = append(middleContent, t)
				}
			}

			if len(middleOld) == 0 || linesContainInOrder(middleContent, middleOld) {
				// Build the matched region from original content.
				region := strings.Join(contentLines[startIdx:endIdx+1], "\n")
				matches++
				lastRegion = region
			}
		}
	}

	if matches == 1 {
		return lastRegion, true, true
	}
	if matches > 1 {
		return lastRegion, true, false
	}
	return "", false, false
}

// --- helpers ---

// trimLines trims each line and joins with \n, dropping leading/trailing empty lines.
func trimLines(lines []string) string {
	var trimmed []string
	for _, line := range lines {
		trimmed = append(trimmed, strings.TrimSpace(line))
	}
	return strings.Join(trimmed, "\n")
}

// stripAllIndent removes all leading whitespace from each line.
func stripAllIndent(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimLeftFunc(line, unicode.IsSpace)
	}
	return strings.Join(lines, "\n")
}

// normalizeIndent strips the common leading whitespace from all lines.
func normalizeIndent(s string) (string, int) {
	lines := strings.Split(s, "\n")
	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := countLeadingSpaces(line)
		if minIndent < 0 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent <= 0 {
		return s, 0
	}
	for i, line := range lines {
		if len(line) >= minIndent {
			lines[i] = line[minIndent:]
		}
	}
	return strings.Join(lines, "\n"), minIndent
}

func countLeadingSpaces(s string) int {
	count := 0
	for _, r := range s {
		if r == ' ' || r == '\t' {
			count++
		} else {
			break
		}
	}
	return count
}

// mapTrimmedToOriginal maps a position in trimmed content back to the original
// content region. Returns the original untrimmed region.
func mapTrimmedToOriginal(original, trimmed string, trimIdx, trimLen int) (string, bool) {
	origLines := strings.Split(original, "\n")
	trimLines := strings.Split(trimmed, "\n")

	// Count lines in the trimmed match region.
	matchStartLine := 0
	matchEndLine := 0
	charCount := 0
	for i, line := range trimLines {
		lineLen := len(line) + 1 // +1 for \n
		if charCount <= trimIdx && trimIdx < charCount+lineLen {
			matchStartLine = i
		}
		if charCount <= trimIdx+trimLen && trimIdx+trimLen <= charCount+lineLen {
			matchEndLine = i
			break
		}
		charCount += lineLen
	}

	if matchStartLine >= len(origLines) || matchEndLine >= len(origLines) {
		return "", false
	}

	return strings.Join(origLines[matchStartLine:matchEndLine+1], "\n"), true
}

// mapNormalizedToOriginal maps a position in indent-normalized content back.
func mapNormalizedToOriginal(original, normalized string, normIdx, normLen int) (string, bool) {
	// Simple approach: find the normalized region's first and last lines,
	// then return the corresponding original lines.
	origLines := strings.Split(original, "\n")
	normLines := strings.Split(normalized, "\n")

	matchStartLine := 0
	matchEndLine := 0
	charCount := 0
	for i, line := range normLines {
		lineLen := len(line) + 1
		if charCount <= normIdx && normIdx < charCount+lineLen {
			matchStartLine = i
		}
		if charCount <= normIdx+normLen && normIdx+normLen <= charCount+lineLen {
			matchEndLine = i
			break
		}
		charCount += lineLen
	}

	if matchStartLine >= len(origLines) || matchEndLine >= len(origLines) {
		return "", false
	}

	return strings.Join(origLines[matchStartLine:matchEndLine+1], "\n"), true
}

// linesContainInOrder checks if target lines appear in order within content
// (allowing gaps between them).
func linesContainInOrder(content, target []string) bool {
	if len(target) == 0 {
		return true
	}
	t := 0
	for _, line := range content {
		if t < len(target) && line == target[t] {
			t++
		}
	}
	return t == len(target)
}

// isCJK reports whether r is a CJK ideograph, kana, or hangul.
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
