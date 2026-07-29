package agent

import (
	"strings"
	"testing"
)

// TestRepeatTextNoRepeat verifies normal, varied text does not trigger the
// monitor — the core anti-false-positive property.
func TestRepeatTextNoRepeat(t *testing.T) {
	var m repeatTextMonitor
	text := "The quick brown fox jumps over the lazy dog. Pack my box with five dozen liquor jugs."
	for _, w := range strings.Fields(text) {
		if m.append(w + " ") {
			t.Fatalf("varied text should not trigger repeat detection")
		}
	}
}

// TestRepeatTextDetectsVerbatimLoop verifies the monitor fires when the same
// passage repeats back-to-back (the loop shape it exists to catch).
func TestRepeatTextDetectsVerbatimLoop(t *testing.T) {
	var m repeatTextMonitor
	passage := "the model is stuck repeating the same passage over and over again here "
	// First and second occurrences: should not fire (threshold is 3, so two
	// appearances of the same n-gram is still within normal phrasing).
	if m.append(passage) {
		t.Fatalf("first occurrence should not fire")
	}
	if m.append(passage) {
		t.Fatalf("second occurrence should not fire (threshold=3)")
	}
	// Third occurrence (same words): n-grams now meet the threshold.
	if !m.append(passage) {
		t.Fatalf("verbatim repeat of a passage should be detected on the third occurrence")
	}
}

// TestRepeatTextReset verifies reset clears state so a fresh turn isn't tainted
// by the previous turn's text.
func TestRepeatTextReset(t *testing.T) {
	var m repeatTextMonitor
	m.append("some text here that fills the buffer a little bit more ")
	m.reset()
	if len(m.tokens) != 0 {
		t.Fatalf("reset should clear tokens, got %d", len(m.tokens))
	}
}

// TestRepeatTextShortTextNoFire verifies very short text (below the n-gram size)
// never fires, even if duplicated.
func TestRepeatTextShortTextNoFire(t *testing.T) {
	var m repeatTextMonitor
	// Fewer tokens than repeatN (8) — can't form an n-gram, so no detection.
	m.append("a b c")
	if m.append("a b c") {
		t.Fatalf("text shorter than n-gram size should not fire")
	}
}

// TestRepeatTextCJKPassage verifies CJK (no word boundaries) is still detected
// when a passage repeats — tokenization splits on whitespace, so a repeated
// CJK sentence appears as a repeated token run.
func TestRepeatTextCJKPassage(t *testing.T) {
	var m repeatTextMonitor
	// A CJK sentence repeated; each sentence is one whitespace-delimited token
	// run, so with enough repeated tokens the n-gram window still catches it.
	passage := "模型卡住了 正在重复 同一段话 模型卡住了 正在重复 同一段话 模型卡住了 正在重复 同一段话 "
	// First append may or may not fire depending on internal repetition;
	// the second append of the same passage must fire.
	_ = m.append(passage)
	if !m.append(passage) {
		t.Fatalf("a repeated CJK passage should be detected on the second append")
	}
}

// TestDetectRepeatedNgramDirect verifies the detector's boundary conditions.
func TestDetectRepeatedNgramDirect(t *testing.T) {
	// Not enough tokens to form one n-gram.
	if detectRepeatedNgram([]string{"a", "b"}, 8, 2) {
		t.Fatal("should not fire when fewer tokens than n")
	}
	// threshold < 2 is disabled.
	if detectRepeatedNgram(strings.Fields("a b c d e f g h"), 8, 1) {
		t.Fatal("threshold<2 should disable detection")
	}
	// A genuine repeat: 8-gram appears twice.
	tokens := []string{}
	block := []string{"w1", "w2", "w3", "w4", "w5", "w6", "w7", "w8"}
	tokens = append(tokens, block...)
	tokens = append(tokens, block...)
	if !detectRepeatedNgram(tokens, 8, 2) {
		t.Fatal("a repeated 8-gram block should be detected")
	}
}
