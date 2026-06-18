package builtin

import (
	"testing"
)

func TestFuzzyMatch_Exact(t *testing.T) {
	content := "line1\nline2\nline3"
	old := "line2"
	region, found, unique := fuzzyMatch(content, old)
	if !found || !unique {
		t.Fatalf("expected found+unique, got found=%v unique=%v", found, unique)
	}
	if region != old {
		t.Fatalf("region=%q, want %q", region, old)
	}
}

func TestFuzzyMatch_LineTrim(t *testing.T) {
	// old has extra spaces that content doesn't have.
	content := "func main() {\n\tfmt.Println(\"hello\")\n}"
	old := "  func main() {  \n    fmt.Println(\"hello\")  \n  }"
	region, found, unique := fuzzyMatch(content, old)
	if !found {
		t.Fatal("expected found via line-trim match")
	}
	if !unique {
		t.Fatal("expected unique")
	}
	_ = region // region is the original untrimmed content lines
}

func TestFuzzyMatch_IndentNorm(t *testing.T) {
	// old has 4-space indent, content has 2-space indent.
	content := "if true {\n  x := 1\n  y := 2\n}"
	old := "if true {\n    x := 1\n    y := 2\n}"
	region, found, unique := fuzzyMatch(content, old)
	if !found {
		t.Fatal("expected found via indent-normalize match")
	}
	if !unique {
		t.Fatal("expected unique")
	}
	_ = region
}

func TestFuzzyMatch_BlockAnchor(t *testing.T) {
	content := `func test() {
	// some comment
	x := 1
	y := 2
	return x + y
}`
	old := `func test() {
	x := 1
	y := 2
	return x + y
}`
	region, found, unique := fuzzyMatch(content, old)
	if !found {
		t.Fatal("expected found via block-anchor match")
	}
	if !unique {
		t.Fatal("expected unique")
	}
	_ = region
}

func TestFuzzyMatch_NotFound(t *testing.T) {
	content := "line1\nline2\nline3"
	old := "nonexistent"
	_, found, _ := fuzzyMatch(content, old)
	if found {
		t.Fatal("expected not found")
	}
}

func TestFuzzyMatch_NotUnique(t *testing.T) {
	content := "abc\nabc\ndef"
	old := "abc"
	_, found, unique := fuzzyMatch(content, old)
	if !found {
		t.Fatal("expected found")
	}
	if unique {
		t.Fatal("expected not unique")
	}
}

func TestFuzzyMatch_EmptyOld(t *testing.T) {
	content := "anything"
	_, found, _ := fuzzyMatch(content, "")
	if found {
		t.Fatal("empty old should not match")
	}
}

func TestFuzzyMatch_SingleLineTrim(t *testing.T) {
	// old has no spaces, content line has spaces around it.
	// fuzzyMatch should find it via line-trim (Level 2).
	content := "  hello world  \nfoo\nbar"
	old := "helloworld" // not a substring of content, exact fails
	_ = content
	_ = old
	// Actually test with a realistic case: old has extra spaces
	content2 := "func main() {\n}"
	old2 := "  func main() {  \n  }"
	region, found, unique := fuzzyMatch(content2, old2)
	if !found {
		t.Fatal("expected found via line-trim")
	}
	if !unique {
		t.Fatal("expected unique")
	}
	_ = region
}

func TestLineTrimMatch_MultiLine(t *testing.T) {
	content := "line1\nline2\nline3"
	old := "  line1  \n  line2  \n  line3  "
	region, found, unique := lineTrimMatch(content, old)
	if !found {
		t.Fatal("expected found")
	}
	if !unique {
		t.Fatal("expected unique")
	}
	_ = region
}

func TestIndentNormMatch(t *testing.T) {
	content := "	if true {\n		x = 1\n	}"
	old := "if true {\n\tx = 1\n}"
	region, found, unique := indentNormMatch(content, old)
	if !found {
		t.Fatal("expected found")
	}
	if !unique {
		t.Fatal("expected unique")
	}
	_ = region
}

func TestBlockAnchorMatch(t *testing.T) {
	content := "func A() {\n\t// comment\n\tx := 1\n}"
	old := "func A() {\n\tx := 1\n}"
	region, found, unique := blockAnchorMatch(content, old)
	if !found {
		t.Fatal("expected found")
	}
	if !unique {
		t.Fatal("expected unique")
	}
	_ = region
}

func TestLinesContainInOrder(t *testing.T) {
	tests := []struct {
		content []string
		target  []string
		want    bool
	}{
		{[]string{"a", "b", "c"}, []string{"a", "c"}, true},
		{[]string{"a", "b", "c"}, []string{"c", "a"}, false},
		{[]string{"a", "b"}, []string{"a", "b", "c"}, false},
		{[]string{"a", "b", "c"}, []string{}, true},
	}
	for _, tt := range tests {
		got := linesContainInOrder(tt.content, tt.target)
		if got != tt.want {
			t.Errorf("linesContainInOrder(%v, %v) = %v, want %v", tt.content, tt.target, got, tt.want)
		}
	}
}
