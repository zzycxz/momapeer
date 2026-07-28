package builtin

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// fuzzyMatch tries to find old in content using progressively looser strategies.
// Returns (matchedRegion, found, unique). If found but not unique, the caller
// should report "not unique" rather than "not found".
//
// Level 0: Unicode-normalize match — when old and content differ only by
// Unicode presentation (smart quotes, em-dash, NBSP, NFC vs NFD, full-width
// vs half-width), match on NFKC-normalized lines and return the original
// (un-normalized) line region.
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

	// Level 0: Unicode-normalize match.
	if region, ok, unique := unicodeNormMatch(content, old); ok {
		return region, ok, unique
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

// normalizeForFuzzyMatch mirrors pi's edit-diff.ts normalizeForFuzzyMatch: NFKC
// fold (handles NFC/NFD and full-width→half-width), then ASCII substitution of
// the LLM's most common Unicode drift (smart quotes, dashes, special spaces),
// then per-line right-trim. It is used only inside the matching space — the
// returned region always comes from the original content, never this output.
func normalizeForFuzzyMatch(s string) string {
	// NFKC first: composes compatibility equivalents (full-width Ａ→A) and
	// canonical decomposition+composition (NFD café → NFC café).
	n := norm.NFKC.String(s)

	// Smart single quotes → '
	n = strings.NewReplacer(
		"\u2018", "'", // ‘
		"\u2019", "'", // ’
		"\u201A", "'", // ‚
		"\u201B", "'", // ‛
	).Replace(n)

	// Smart double quotes → "
	n = strings.NewReplacer(
		"\u201C", "\"", // “
		"\u201D", "\"", // ”
		"\u201E", "\"", // „
		"\u201F", "\"", // ‟
	).Replace(n)

	// Dashes/hyphens → -
	n = strings.NewReplacer(
		"\u2010", "-", // ‐ hyphen
		"\u2011", "-", // ‑ non-breaking hyphen
		"\u2012", "-", // ‒ figure dash
		"\u2013", "-", // – en dash
		"\u2014", "-", // — em dash
		"\u2015", "-", // ― horizontal bar
		"\u2212", "-", // − minus sign
	).Replace(n)

	// Special spaces → regular space.
	n = strings.Map(func(r rune) rune {
		switch r {
		case '\u00A0', // NBSP
			'\u2002', '\u2003', '\u2004', '\u2005', '\u2006', // en/em/... spaces
			'\u2007', '\u2008', '\u2009', '\u200A', // figure/punctuation/thin/hair spaces
			'\u202F', // narrow no-break space
			'\u205F', // medium mathematical space
			'\u3000': // ideographic space
			return ' '
		}
		return r
	}, n)

	// Per-line right-trim, matching pi's normalizeForFuzzyMatch.
	lines := strings.Split(n, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	return strings.Join(lines, "\n")
}

// unicodeNormMatch implements fuzzyMatch's Level 0: it runs only when old and
// content differ in Unicode presentation (otherwise it short-circuits to "no
// match" so Level 1 can take over). Matching is line-granular: both sides are
// split on "\n", each line is run through normalizeForFuzzyMatch AND TrimSpace
// (so this level also closes the "smart quotes + lost indentation" gap that
// neither Level 1 nor Level 2 alone covers), and the normalized+trimmed old is
// searched for as a contiguous run of normalized+trimmed content lines. The
// returned region is the corresponding ORIGINAL (un-normalized, un-trimmed)
// lines joined with "\n", which is always a verbatim substring of content (so
// editfile.go's strings.Contains guard and strings.Replace both work).
//
// We deliberately do NOT attempt within-line substring mapping: NFKC/NFC change
// byte/rune counts, so a normalized-space offset cannot be projected back to
// the original bytes without a per-rune offset table. Whole-line matching keeps
// the region a clean original substring at the cost of replacing the whole line
// (same trade-off Level 2 line-trim already makes).
func unicodeNormMatch(content, old string) (region string, found, unique bool) {
	// Fast path: if neither side has a Unicode-presentation difference, defer
	// to the exact/structural levels. This keeps Level 0 a pure superset for
	// genuinely-Unicode-divergent inputs and avoids shadowing Level 1.
	normContent := normalizeForFuzzyMatch(content)
	normOld := normalizeForFuzzyMatch(old)
	if normContent == content && normOld == old {
		return "", false, false
	}
	// If exact match already works on the original strings, Level 1 owns it.
	if strings.Count(content, old) > 0 {
		return "", false, false
	}

	origLines := strings.Split(content, "\n")
	normContentLines := strings.Split(normContent, "\n")
	normOldLines := strings.Split(normOld, "\n")

	// Empty old (or all-blank after normalization) is the caller's job; skip.
	if len(normOldLines) == 0 || strings.TrimSpace(normOld) == "" {
		return "", false, false
	}
	// Defensive: line counts must line up (normalizeForFuzzyMatch preserves "\n"
	// structure, only mutating within-line runes).
	if len(normContentLines) != len(origLines) {
		return "", false, false
	}

	// Compare on the trimmed+normalized form of each line (closes the
	// "Unicode drift + indentation drift" gap).
	trimNormContent := make([]string, len(normContentLines))
	for i, l := range normContentLines {
		trimNormContent[i] = strings.TrimSpace(l)
	}

	// Single-line old: search each trimmed+normalized content line.
	if len(normOldLines) == 1 {
		needle := strings.TrimSpace(normOldLines[0])
		if needle == "" {
			return "", false, false
		}
		matchCount := 0
		var matchRegion string
		for i, nl := range trimNormContent {
			if nl == needle {
				matchCount++
				matchRegion = origLines[i] // original un-trimmed line
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

	// Multi-line old: find contiguous runs of trimmed+normalized content lines
	// equal to the trimmed+normalized old lines. The region is the
	// corresponding original-line slice — always a verbatim substring.
	needle := make([]string, len(normOldLines))
	for i, l := range normOldLines {
		needle[i] = strings.TrimSpace(l)
	}
	matchCount := 0
	var matchRegion string
	for i := 0; i+len(needle) <= len(trimNormContent); i++ {
		equal := true
		for j := range needle {
			if trimNormContent[i+j] != needle[j] {
				equal = false
				break
			}
		}
		if equal {
			matchCount++
			matchRegion = strings.Join(origLines[i:i+len(needle)], "\n")
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
//
//nolint:unused // retained for future fuzzy-match improvements
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

//nolint:unused // used by normalizeIndent
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
//
//nolint:unused // retained for future CJK-aware fuzzy matching
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// nearestLine returns the 1-based line number and trimmed text of the content
// line most similar to oldString's first non-empty line, plus a similarity
// fraction (0..1). It is used to give a closest-match hint when an edit fails
// to find old_string, so the model can see what's actually near its target
// instead of a bare "not found". Returns ok=false when no usable line exists.
func nearestLine(content, oldString string) (line int, text string, similarity float64, ok bool) {
	// Take oldString's first non-empty, trimmed line as the probe.
	probe := ""
	for _, l := range strings.Split(oldString, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			probe = t
			break
		}
	}
	if probe == "" {
		return 0, "", 0, false
	}
	// Normalize whitespace for a fair comparison (tabs→spaces, collapse runs).
	normProbe := normSpace(probe)
	bestLine, bestText, bestSim := 0, "", 0.0
	for i, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		s := jaccardNorm(normSpace(t), normProbe)
		if s > bestSim {
			bestLine, bestText, bestSim = i+1, t, s
		}
	}
	if bestLine == 0 || bestSim < 0.2 {
		return 0, "", 0, false
	}
	return bestLine, bestText, bestSim, true
}

// normSpace collapses runs of whitespace (incl. tabs) into single spaces.
func normSpace(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// jaccardNorm returns a 0..1 word-level similarity between two
// whitespace-normalized strings via the Jaccard index of their word sets. It's
// cheap and order-insensitive, which suits "which line looks most like the
// target" ranking better than exact substring matching.
func jaccardNorm(a, b string) float64 {
	wa := strings.Fields(a)
	wb := strings.Fields(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(wa)+len(wb))
	intersect := 0
	for _, w := range wa {
		set[w] = struct{}{}
	}
	for _, w := range wb {
		if _, ok := set[w]; ok {
			intersect++
			continue
		}
		set[w] = struct{}{}
	}
	union := len(set)
	if union == 0 {
		return 0
	}
	return float64(intersect) / float64(union)
}
