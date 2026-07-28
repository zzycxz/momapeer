package builtin

import (
	"strings"
	"testing"
)

// TestNormalizeForFuzzyMatch exercises the Level-0 normalizer directly so a
// regression in the rune tables is caught without going through the matcher.
func TestNormalizeForFuzzyMatch(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"identity", "hello world", "hello world"},
		{"smart single quote", "we\u2019ll", "we'll"},
		{"smart double quotes", "\u201chello\u201d", "\"hello\""},
		{"em dash", "a\u2014b", "a-b"},
		{"en dash", "1\u20132", "1-2"},
		{"NBSP", "a\u00a0b", "a b"},
		{"ideographic space", "a\u3000b", "a b"},
		{"full-width A (NFKC fold)", "\uFF21", "A"},
		{"trailing whitespace trimmed", "line  \nnext", "line\nnext"},
		{"tab in trailing trim", "line\t\nnext", "line\nnext"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeForFuzzyMatch(tt.in); got != tt.want {
				t.Errorf("normalizeForFuzzyMatch(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestUnicodeNormMatch_SmartQuotes: LLM emits smart quotes, file has ASCII
// quotes — Level 0 must recover the original line verbatim (including the
// leading tab, since the region is the un-trimmed original line).
func TestUnicodeNormMatch_SmartQuotes(t *testing.T) {
	content := "func main() {\n\tfmt.Println(\"hello\")\n}"
	old := "fmt.Println(\u201chello\u201d)" // “hello”
	region, found, unique := fuzzyMatch(content, old)
	if !found {
		t.Fatal("expected Level 0 to match smart quotes")
	}
	if !unique {
		t.Fatal("expected unique")
	}
	// Region is the ORIGINAL line (with leading tab preserved). The caller's
	// strings.Replace will swap the whole tab-prefixed line for new_string,
	// which is correct — new_string should carry the same indentation.
	want := "\tfmt.Println(\"hello\")"
	if region != want {
		t.Errorf("region = %q, want %q (original un-trimmed line)", region, want)
	}
	if !strings.Contains(content, region) {
		t.Errorf("region %q is not a substring of content (guard violation)", region)
	}
}

// TestUnicodeNormMatch_EmDash: em-dash in old, hyphen in file.
func TestUnicodeNormMatch_EmDash(t *testing.T) {
	content := "status: ok — done"
	old := "status: ok \u2014 done" // em dash
	region, found, unique := fuzzyMatch(content, old)
	if !found || !unique {
		t.Fatalf("expected found+unique, got found=%v unique=%v", found, unique)
	}
	if region != "status: ok — done" {
		t.Errorf("region = %q, want original file line with ASCII dash variant", region)
	}
	if !strings.Contains(content, region) {
		t.Errorf("region %q not a substring of content", region)
	}
}

// TestUnicodeNormMatch_NBSP: non-breaking space in old, regular space in file.
// NBSP folds to ASCII space; the rest of the line is identical so it must match.
func TestUnicodeNormMatch_NBSP(t *testing.T) {
	content := "status: ok done"
	old := "status:\u00a0ok done" // NBSP between "status:" and "ok"
	region, found, unique := fuzzyMatch(content, old)
	if !found || !unique {
		t.Fatalf("expected found+unique, got found=%v unique=%v", found, unique)
	}
	if !strings.Contains(content, region) {
		t.Errorf("region %q not a substring of content", region)
	}
}

// TestUnicodeNormMatch_NFCvsNFD: the same accented character with different
// code-point sequences must match. "café" (NFC, 1 rune for é) vs NFD (e + ́ ).
func TestUnicodeNormMatch_NFCvsNFD(t *testing.T) {
	nfc := "cafel \u00e9"  // café with NFC é (U+00E9)
	nfd := "cafel e\u0301" // café with NFD (e + combining acute)
	// File holds NFC, LLM emits NFD.
	region, found, unique := fuzzyMatch(nfc, nfd)
	if !found || !unique {
		t.Fatalf("expected found+unique, got found=%v unique=%v", found, unique)
	}
	if !strings.Contains(nfc, region) {
		t.Errorf("region %q not a substring of NFC content", region)
	}
}

// TestUnicodeNormMatch_FullWidth: full-width Ａ folds to A via NFKC.
func TestUnicodeNormMatch_FullWidth(t *testing.T) {
	content := "var ABC = 1"
	old := "var \uFF21\uFF22\uFF23 = 1" // ＡＢＣ
	region, found, unique := fuzzyMatch(content, old)
	if !found || !unique {
		t.Fatalf("expected found+unique, got found=%v unique=%v", found, unique)
	}
	if !strings.Contains(content, region) {
		t.Errorf("region %q not a substring of content", region)
	}
}

// TestUnicodeNormMatch_MultiLine: a multi-line block where every line differs
// only by Unicode presentation must match as a contiguous original-line run.
func TestUnicodeNormMatch_MultiLine(t *testing.T) {
	content := "func main() {\n\tfmt.Println(\"hi\")\n\treturn\n}"
	// old uses smart quotes on line 2; line 3 also needs to normalize to match.
	// Both old lines have the same trimmed form as the content lines.
	old := "func main() {\nfmt.Println(\u201chi\u201d)\nreturn\n}"
	region, found, unique := fuzzyMatch(content, old)
	if !found || !unique {
		t.Fatalf("expected found+unique, got found=%v unique=%v", found, unique)
	}
	if !strings.Contains(content, region) {
		t.Errorf("region %q not a substring of content", region)
	}
	// Region must be the whole 4-line original block (with tabs preserved).
	if region != content {
		t.Errorf("region = %q, want entire content %q", region, content)
	}
}

// TestUnicodeNormMatch_NotUnique: when multiple lines normalize to the same
// form, Level 0 reports found but not unique (mirrors Level 1+ semantics).
// Both content lines use ASCII quotes; old is also ASCII — after normalize
// both content lines are identical, so the single-line old is ambiguous.
func TestUnicodeNormMatch_NotUnique(t *testing.T) {
	content := "fmt.Println(\"x\")\nfmt.Println(\"x\")"
	old := "fmt.Println(\"x\")"
	region, found, unique := fuzzyMatch(content, old)
	if !found {
		t.Fatal("expected found")
	}
	if unique {
		t.Fatal("expected not unique (two identical lines)")
	}
	// Region should still be a valid substring (the first line).
	if region != "fmt.Println(\"x\")" {
		t.Errorf("region = %q, want the first line", region)
	}
}

// TestUnicodeNormMatch_PureASCII_DefersToExact: when there is no Unicode
// difference at all, Level 0 must short-circuit (return false) so Level 1 owns
// the exact match. Otherwise Level 0 would shadow and break Level 1 semantics.
func TestUnicodeNormMatch_PureASCII_DefersToExact(t *testing.T) {
	content := "line1\nline2\nline3"
	old := "line2"
	// Should still match (via Level 1, since Level 0 defers), and region is old.
	region, found, unique := fuzzyMatch(content, old)
	if !found || !unique {
		t.Fatalf("expected found+unique, got found=%v unique=%v", found, unique)
	}
	if region != old {
		t.Errorf("region = %q, want %q", region, old)
	}
}

// TestUnicodeNormMatch_NoMatch: genuine non-matches (no Unicode or structural
// relationship) still return not-found.
func TestUnicodeNormMatch_NoMatch(t *testing.T) {
	content := "func a() {}\nfunc b() {}"
	old := "totally different content"
	_, found, _ := fuzzyMatch(content, old)
	if found {
		t.Fatal("expected not found for unrelated text")
	}
}

// TestFuzzyMatch_LevelsStillWork_Regression: a sanity check that adding Level 0
// did not regress the existing 4 levels. Uses the same shapes as the original
// suite but through fuzzyMatch's unified entry point.
func TestFuzzyMatch_LevelsStillWork_Regression(t *testing.T) {
	// Exact
	if region, found, unique := fuzzyMatch("abc", "abc"); !found || !unique || region != "abc" {
		t.Errorf("exact broken: region=%q found=%v unique=%v", region, found, unique)
	}
	// Line-trim
	if _, found, unique := fuzzyMatch("  hi  \nfoo", "hi"); !found || !unique {
		t.Errorf("line-trim broken: found=%v unique=%v", found, unique)
	}
	// Not found stays not found
	if _, found, _ := fuzzyMatch("abc", "xyz"); found {
		t.Error("not-found broken")
	}
}
