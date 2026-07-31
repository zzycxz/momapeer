package scheduler

import (
	"testing"
	"time"
)

// TestLoadCapturesMissedOneShot confirms that a one-shot task whose fire instant
// passed while the app was down is captured as a MissedReminder (so the desktop
// layer can surface a catch-up notification) instead of being silently disabled.
// A task that ALREADY fired (RunCount>0) must NOT produce a catch-up (no dupes).
func TestLoadCapturesMissedOneShot(t *testing.T) {
	path := t.TempDir() + "/sched.json"
	s1 := New(path)
	// Write past-due one-shot tasks directly to the store (Create refuses past
	// one-shots by design — these represent tasks that were valid when created
	// but aged into the past while the app was down).
	s1.mu.Lock()
	s1.tasks = []ScheduledTask{
		{ID: "t1", Name: "missed-meal", Expression: "at 2020-01-01 09:00", Prompt: "该吃饭了", Enabled: true, OneShot: true, RunCount: 0},
		{ID: "t2", Name: "already-fired", Expression: "at 2020-01-01 09:00", Prompt: "done", Enabled: true, OneShot: true, RunCount: 1},
	}
	s1.mu.Unlock()
	if err := s1.store.save(s1.tasks); err != nil {
		t.Fatal(err)
	}
	if err := s1.store.save(s1.tasks); err != nil {
		t.Fatal(err)
	}

	// Reload in a fresh scheduler — Load detects both are past one-shots.
	s2 := New(path)
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	missed := s2.DrainMissedReminders()
	if len(missed) != 1 {
		t.Fatalf("DrainMissedReminders = %d items, want 1 (only the never-fired task)", len(missed))
	}
	if missed[0].Name != "missed-meal" {
		t.Errorf("missed reminder name = %q, want missed-meal", missed[0].Name)
	}
	if missed[0].Body != "该吃饭了" {
		t.Errorf("missed reminder body = %q, want 该吃饭了", missed[0].Body)
	}
	// Drain is destructive — a second call returns nothing.
	if again := s2.DrainMissedReminders(); len(again) != 0 {
		t.Errorf("second DrainMissedReminders = %d, want 0", len(again))
	}
	// Both tasks must now be disabled (not re-fire later).
	for _, tk := range s2.List(false) {
		if tk.Enabled {
			t.Errorf("task %q still enabled after Load (past one-shot should be disabled)", tk.Name)
		}
	}
}

// TestLoadNoMissedForFutureOneShot confirms a future one-shot is NOT flagged as
// missed — it stays enabled with its NextRun intact.
func TestLoadNoMissedForFutureOneShot(t *testing.T) {
	path := t.TempDir() + "/sched.json"
	s1 := New(path)
	future := time.Now().Add(1 * time.Hour).Format("2006-01-02 15:04")
	_, err := s1.Create(ScheduledTask{Name: "future", Expression: "at " + future, Prompt: "later"})
	if err != nil {
		t.Fatal(err)
	}

	s2 := New(path)
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if missed := s2.DrainMissedReminders(); len(missed) != 0 {
		t.Errorf("future one-shot flagged as missed: %v", missed)
	}
	tk := s2.List(false)[0]
	if !tk.Enabled {
		t.Error("future one-shot should remain enabled")
	}
}
