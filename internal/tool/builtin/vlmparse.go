package builtin

import (
	"regexp"
	"strconv"
	"strings"
)

// VLM output parsing: extract coordinates (normalized 0-1000) or element labels
// (IDs like "A", "B3") from the freeform text VLMs return. This is the Go port
// of Rooster's VisionStrategy._parse_coordinates (10-pattern chain) + the label
// selection parser. Different VLMs (九天/minimax/kimi/GLM/Doubao) format their
// outputs differently; the chain tries each format in priority order and uses
// the first match.

// coordPatterns is the ordered chain of regex patterns for coordinate extraction.
// Each pattern has 2 capture groups: x and y (both 0-1000 normalized). Patterns
// are tried in order; the first match wins. Order matters — more specific
// patterns first, generic fallbacks last.
var coordPatterns = []*regexp.Regexp{ //nolint:unused
	// 1. [TARGET_ACTION]...payload...x:..,y:.. (Rooster protocol, most specific)
	regexp.MustCompile(`(?is)\[TARGET_ACTION\].*?['"]?x['"]?\s*[:=]\s*(\d+\.?\d*)\s*[,;]\s*['"]?y['"]?\s*[:=]\s*(\d+\.?\d*)`),
	// 2. JSON: {"x": 123, "y": 456}
	regexp.MustCompile(`["']x["']\s*:\s*(\d+\.?\d*)\s*,\s*["']y["']\s*:\s*(\d+\.?\d*)`),
	// 3. Chinese punctuation: x：123，y：456
	regexp.MustCompile(`x[：:]\s*(\d+\.?\d*)\s*[,，;；]\s*y[：:]\s*(\d+\.?\d*)`),
	// 4. Standard: x=123, y=456 or x: 123, y: 456
	regexp.MustCompile(`\bx\s*[=:]\s*(\d+\.?\d*)\s*[,，]\s*y\s*[=:]\s*(\d+\.?\d*)`),
	// 5. No separator: x=123 y=456
	regexp.MustCompile(`\bx\s*=\s*(\d+\.?\d*)\s+y\s*=\s*(\d+\.?\d*)`),
	// 6. Parenthetical: (123, 456) or 坐标(123, 456)
	regexp.MustCompile(`(?:坐标|位置|point|center|at|位于|在)\s*[（(]?\s*(\d+\.?\d*)\s*[,，]\s*(\d+\.?\d*)\s*[）)]?`),
	// 7. GLM-4.6V double bracket: [[x1,y1,x2,y2]] (take first two)
	regexp.MustCompile(`\[\[\s*(\d+\.?\d*)\s*[,，]\s*(\d+\.?\d*)`),
	// 8. Doubao bbox: <bbox>x1 y1 x2 y2</bbox> (take first two)
	regexp.MustCompile(`<bbox>\s*(\d+\.?\d*)\s+(\d+\.?\d*)`),
	// 9. Point tag: <point>x y</point>
	regexp.MustCompile(`<point>\s*(\d+\.?\d*)\s+(\d+\.?\d*)\s*</point>`),
	// 10. Generic: two consecutive numbers in brackets/parens
	regexp.MustCompile(`[（(]\s*(\d+\.?\d*)\s*[,，]\s*(\d+\.?\d*)\s*[）)]`),
}

// parseVLMCoords extracts normalized (0-1000) coordinates from VLM output text.
// Returns (x, y, true) if found; (0, 0, false) otherwise. Values are clamped to
// [0, 1000] — values outside this range indicate a parse error.
func parseVLMCoords(text string) (x, y float64, ok bool) { //nolint:unused
	for _, p := range coordPatterns {
		m := p.FindStringSubmatch(text)
		if m != nil {
			x, errX := strconv.ParseFloat(m[1], 64)
			y, errY := strconv.ParseFloat(m[2], 64)
			if errX == nil && errY == nil {
				// Clamp to valid range; reject if clearly out of bounds.
				if x < 0 || x > 1000 || y < 0 || y > 1000 {
					continue
				}
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// labelPatterns extracts an element ID (like "A", "B3", "AA") that the VLM
// selected. Matches formats like: [A], (A), 选A, label: A, target: A, 编号A.
var labelPatterns = []*regexp.Regexp{ //nolint:unused
	// [TARGET] A or [TARGET_ACTION] A (Rooster protocol)
	regexp.MustCompile(`(?i)\[TARGET(?:_ACTION)?\][^A-Z0-9]*([A-Z]{1,2}\d*)`),
	// label: A, target: A, 选择A, 选中A, 编号A
	regexp.MustCompile(`(?i)(?:label|target|选择|选中|编号|元素)\s*[:：]?\s*([A-Z]{1,2}\d*)`),
	// [A] or (A) — bracketed single/double letter
	regexp.MustCompile(`[（(\[]\s*([A-Z]{1,2}\d*)\s*[）)\]]`),
}

// parseVLMLabel extracts the element ID the VLM chose. Returns (id, true) if
// found; ("", false) otherwise.
func parseVLMLabel(text string) (id string, ok bool) { //nolint:unused
	for _, p := range labelPatterns {
		m := p.FindStringSubmatch(text)
		if m != nil {
			id = strings.TrimSpace(m[1])
			if id != "" {
				return id, true
			}
		}
	}
	return "", false
}

// parseConfidence extracts a [CONFIDENCE: 0-100] value from VLM output.
var confidencePattern = regexp.MustCompile(`(?i)\[CONFIDENCE[:\s]+(\d{1,3})\]`) //nolint:unused

func parseConfidence(text string) int { //nolint:unused
	m := confidencePattern.FindStringSubmatch(text)
	if m == nil {
		return 50 // default mid-confidence when not specified
	}
	v, err := strconv.Atoi(m[1])
	if err != nil {
		return 50
	}
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// denormalize converts a 0-1000 normalized coordinate to physical screen pixels.
func denormalize(norm float64, screenDimension int) int { //nolint:unused
	return int(norm / 1000.0 * float64(screenDimension))
}
