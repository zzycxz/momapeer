package agent

import "strings"

// extractJSON finds the first { ... } block in a raw LLM response string.
// LLMs often wrap JSON in markdown code fences or add prose before/after;
// this extracts just the JSON object for unmarshalling. Returns raw unchanged
// if no braces are found.
func extractJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i >= 0 {
		jsonStr := raw[i:]
		if j := strings.LastIndex(jsonStr, "}"); j >= 0 {
			return jsonStr[:j+1]
		}
	}
	return raw
}
