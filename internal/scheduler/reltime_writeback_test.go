package scheduler

import (
	"testing"
	"time"
)

// Verify that the write-back form "at YYYY-MM-DD HH:MM" re-parses cleanly so the
// post-smart-parse useEffect (which re-runs PreviewSchedule) never flips the
// resolved one-shot back into an "unknown" preview.
func TestSmartParseWritebackReResolves(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 30, 0, 0, time.Local)
	cases := []string{
		"at 2026-08-14 15:00",
		"at 2026-08-14 15:04",
	}
	for _, in := range cases {
		expr, err := NormalizeExpression(in, now)
		if err != nil {
			t.Fatalf("NormalizeExpression(%q): %v", in, err)
		}
		if !IsOneShot(expr) {
			t.Errorf("write-back %q should be one-shot, got expr=%q", in, expr)
		}
		nr := NextRunPublic(expr, now)
		if nr.IsZero() {
			t.Errorf("write-back %q resolved to zero (past?) — preview would show unknown", in)
		}
	}
}
