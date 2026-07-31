package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestPlainTaskSkipsRunner confirms the core contract of the Plain flag:
//   - Plain=true  → the runner is NOT called; the prompt text is surfaced verbatim
//     as the result (so notify/IM/email deliver the user's exact text, not AI output).
//   - Plain=false (default) → the runner IS called and its output is the result.
//
// This guards the failure mode the user flagged: an AI directive must never be
// silently swallowed into a plain echo, and a plain reminder must not spin up the
// agent. The flag is explicit per-task — no guessing from prompt text.
func TestPlainTaskSkipsRunner(t *testing.T) {
	cases := []struct {
		name      string
		plain     bool
		prompt    string
		wantCall  bool   // should the runner be invoked?
		wantInRes string // substring expected in the fired result
	}{
		{"plain on echoes prompt", true, "通知我喝水", false, "通知我喝水"},
		{"plain off runs agent", false, "通知我喝水", true, "agent-output"},
		// Even a verb-heavy prompt with Plain=true is echoed as-is — the flag is
		// authoritative, the prompt text is never re-interpreted.
		{"plain on overrides verb", true, "总结今天的邮件", false, "总结今天的邮件"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := New(t.TempDir() + "/sched.json")
			_, err := s.Create(ScheduledTask{
				Name:       c.name,
				Expression: "every 1h",
				Prompt:     c.prompt,
				Plain:      c.plain,
			})
			if err != nil {
				t.Fatal(err)
			}
			// Force due.
			s.mu.Lock()
			s.tasks[0].NextRun = time.Now().Add(-time.Minute)
			s.mu.Unlock()

			called := false
			s.SetRunner(fakeRunner{fn: func(ctx context.Context, profile, prompt string) (string, error) {
				called = true
				return "agent-output", nil
			}})
			s.fireDue(time.Now())

			got := s.List(false)[0]
			if called != c.wantCall {
				t.Errorf("runner called = %v, want %v", called, c.wantCall)
			}
			// For the echo path the result equals the trimmed prompt; for the agent
			// path it equals the runner output. Check the expected substring.
			if c.wantInRes != "" && !strings.Contains(got.LastResult, c.wantInRes) {
				t.Errorf("LastResult = %q, want to contain %q", got.LastResult, c.wantInRes)
			}
		})
	}
}
