package agent

import (
	"fmt"
	"strings"
)

// StripGoalMarkers removes goal status markers like [goal:complete],
// [goal:continue], and [goal:blocked:...] from display text so users see
// natural language instead of protocol markers. Exported for use by frontends
// (desktop wire, CLI TUI, HTTP/SSE serve).
//
// The markers are still kept in the session history — the controller's
// parseGoalStatusMarker relies on them to drive the goal loop, and the HTTP
// history endpoint returns them verbatim so a replayed conversation still
// advances. This function is only for the live, user-facing display path.
//
// Ported from DeepSeek-Reasonix. Behavior:
//   - [goal:complete] and [goal:continue] lines are dropped entirely.
//   - [goal:blocked:<reason>] is rewritten as "⚠️ Blocked: <reason>" so the
//     user still sees that the turn hit a blocker, just without the raw tag.
//   - A line with other content keeps that content; only bare marker lines
//     are removed.
func StripGoalMarkers(text string) string {
	text = strings.TrimSpace(text)
	if !strings.Contains(text, "[goal:") {
		// Fast path: no marker can be present, return without allocating.
		return text
	}
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[goal:complete]" || trimmed == "[goal:continue]" {
			continue
		}
		if strings.HasPrefix(trimmed, "[goal:blocked:") && strings.HasSuffix(trimmed, "]") {
			reason := strings.TrimPrefix(trimmed, "[goal:blocked:")
			reason = strings.TrimSuffix(reason, "]")
			if reason = strings.TrimSpace(reason); reason != "" {
				cleaned = append(cleaned, fmt.Sprintf("\u26a0\ufe0f Blocked: %s", reason))
			}
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}
