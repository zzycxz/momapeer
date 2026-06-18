package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDreamStateRecordAndRead verifies the run history round-trips through
// dream_state.json — the file that replaced the broken session-meta scan. This
// is the load-bearing mechanism for both cadence gating and the "last run"
// status display, so it must be reliable on its own.
func TestDreamStateRecordAndRead(t *testing.T) {
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, ".momapeer", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// No runs yet: LastDreamRun reports nothing.
	if _, ok := LastDreamRun(sessionsDir, KindDream); ok {
		t.Fatalf("expected no prior dream run in fresh dir")
	}

	// Record a dream run, then a distill run.
	appendDreamRun(sessionsDir, DreamRun{Kind: KindDream, Trigger: TriggerAuto, StartedAt: time.Now(), Status: "ok"})
	appendDreamRun(sessionsDir, DreamRun{Kind: KindDistill, Trigger: TriggerManual, StartedAt: time.Now().Add(-time.Hour), Status: "ok"})

	last, ok := LastDreamRun(sessionsDir, KindDream)
	if !ok || last.Kind != KindDream || last.Status != "ok" {
		t.Fatalf("LastDreamRun = %+v ok=%v, want a recorded dream run", last, ok)
	}

	dreamHist := DreamHistory(sessionsDir, KindDream)
	if len(dreamHist) != 1 {
		t.Fatalf("dream history len = %d, want 1", len(dreamHist))
	}
	distillHist := DreamHistory(sessionsDir, KindDistill)
	if len(distillHist) != 1 || distillHist[0].Trigger != TriggerManual {
		t.Fatalf("distill history = %+v, want one manual run", distillHist)
	}
}

// TestTrimDreamRunsCapsPerKind ensures the state file stays bounded: each kind
// keeps only the most recent dreamStateHistory entries.
func TestTrimDreamRunsCapsPerKind(t *testing.T) {
	var runs []DreamRun
	for i := 0; i < dreamStateHistory+5; i++ {
		runs = append(runs, DreamRun{Kind: KindDream, StartedAt: time.Unix(int64(i), 0)})
		runs = append(runs, DreamRun{Kind: KindDistill, StartedAt: time.Unix(int64(i), 0)})
	}
	got := trimDreamRuns(runs)
	dream, distill := 0, 0
	for _, r := range got {
		switch r.Kind {
		case KindDream:
			dream++
		case KindDistill:
			distill++
		}
	}
	if dream != dreamStateHistory || distill != dreamStateHistory {
		t.Fatalf("after trim: dream=%d distill=%d, want both %d", dream, distill, dreamStateHistory)
	}
}

// TestWorkspaceOldEnough gates the cold-start auto trigger: a brand-new sessions
// dir should not fire consolidation immediately.
func TestWorkspaceOldEnough(t *testing.T) {
	dir := t.TempDir()
	// Empty dir: not old enough.
	if workspaceOldEnough(dir, time.Hour) {
		t.Fatalf("empty dir should not be old enough")
	}
	// A session file older than the interval qualifies.
	old := filepath.Join(dir, "old.jsonl")
	if err := os.WriteFile(old, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Backdate mtime to > interval.
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if !workspaceOldEnough(dir, time.Hour) {
		t.Fatalf("dir with a 2h-old session should be old enough for a 1h interval")
	}
	if workspaceOldEnough(dir, 3*time.Hour) {
		t.Fatalf("dir with a 2h-old session should NOT be old enough for a 3h interval")
	}
}
