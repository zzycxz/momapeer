package agent

// repeat_text.go detects when the model's streamed output falls into a text
// repetition loop (emitting the same passage over and over), distinct from the
// tool-level loop guards (stormBreak / repeatSuccessBlock) which key on tool
// calls. A stuck model can narrate the same paragraph repeatedly without ever
// repeating a tool call, so the tool guards never trip; this catches that case
// by sliding an n-gram window over the assistant text.
//
// This is advisory only: on detection it signals the caller (which surfaces a
// Notice), it does NOT abort the turn — a false positive on legitimate repeated
// phrasing (common in Chinese summaries) is worse than a missed loop, since the
// hard maxSteps guard remains the ultimate backstop. Ported from MiMo-Code's
// text-ngram-detection, adapted to Go and made conservative (notice, not abort).

import (
	"strings"
)

const (
	// repeatN is the n-gram size. 8 words is long enough that a chance match in
	// normal prose is rare, short enough to catch a repeated sentence.
	repeatN = 8
	// repeatThreshold is how many times an n-gram must appear before it counts
	// as a loop. 3 = the passage appears three times, reducing false positives
	// on legitimate repeated phrasing (common in Chinese summaries/lists) while
	// still catching genuine output loops.
	repeatThreshold = 3
	// repeatWindowTokens caps how many tokens of recent output we scan, so cost
	// stays bounded and old, unrelated repetition doesn't fire.
	repeatWindowTokens = 400
)

// repeatTextMonitor slides an n-gram window over appended text and reports when
// the same n-gram recurs within the window. It is reset between turns by the
// caller, so repetition is only flagged within a single streamed answer.
type repeatTextMonitor struct {
	tokens []string
}

// append feeds a chunk of streamed text and returns true if a repetition loop
// is detected within the current window. Whitespace is collapsed; the token
// stream is lowercased so casing differences don't mask a repeat.
func (m *repeatTextMonitor) append(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	words := tokenize(text)
	m.tokens = append(m.tokens, words...)
	// Trim the window to the last repeatWindowTokens tokens.
	if len(m.tokens) > repeatWindowTokens {
		m.tokens = m.tokens[len(m.tokens)-repeatWindowTokens:]
	}
	return detectRepeatedNgram(m.tokens, repeatN, repeatThreshold)
}

// reset clears the monitor between turns.
func (m *repeatTextMonitor) reset() {
	m.tokens = nil
}

// tokenize lowercases, collapses whitespace, and splits on spaces. CJK text has
// no word boundaries, so it tokenizes into runs of non-space characters, which
// is coarse but sufficient for detecting verbatim repetition of a passage.
func tokenize(text string) []string {
	// Collapse all whitespace runs to single spaces, then split.
	lowered := strings.ToLower(text)
	collapsed := strings.Builder{}
	prevSpace := false
	for _, r := range lowered {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !prevSpace {
				collapsed.WriteRune(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		collapsed.WriteRune(r)
	}
	return strings.Fields(collapsed.String())
}

// detectRepeatedNgram reports whether any n-gram appears at least threshold
// times in tokens. Returns false when there isn't enough text to form one n-gram.
func detectRepeatedNgram(tokens []string, n, threshold int) bool {
	if n <= 0 || threshold < 2 || len(tokens) < n {
		return false
	}
	counts := make(map[string]int, len(tokens))
	for i := 0; i <= len(tokens)-n; i++ {
		gram := strings.Join(tokens[i:i+n], "\x00")
		counts[gram]++
		if counts[gram] >= threshold {
			return true
		}
	}
	return false
}
