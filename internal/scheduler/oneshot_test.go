package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestOneShotAutoDisablesAfterFire confirms a task with an "at ..." expression
// fires once, then is auto-disabled (Enabled=false, NextRun zeroed) — and stays
// in the list (not deleted) so history is preserved.
func TestOneShotAutoDisablesAfterFire(t *testing.T) {
	s := New(t.TempDir() + "/sched.json")
	// Future instant: 2 seconds from now keeps the test fast but deterministic.
	future := time.Now().Add(2 * time.Second)
	task, err := s.Create(ScheduledTask{
		Name:       "one-shot",
		Expression: "at " + future.Format("2006-01-02 15:04:05"),
		Prompt:     "ping",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !task.OneShot {
		t.Error("Create should mark at-tasks as OneShot=true")
	}
	if !task.Enabled {
		t.Error("new one-shot should start enabled")
	}

	s.SetRunner(fakeRunner{fn: func(ctx context.Context, profile, prompt string) (string, error) {
		return "fired", nil
	}})

	// Fire at a time just past the scheduled instant.
	s.fireDue(future.Add(time.Second))

	got := s.List(false)
	if len(got) != 1 {
		t.Fatalf("one-shot should still be in list after firing; got %d tasks", len(got))
	}
	if got[0].Enabled {
		t.Error("one-shot should be auto-disabled after firing")
	}
	if !got[0].NextRun.IsZero() {
		t.Errorf("one-shot NextRun should be zero after firing; got %v", got[0].NextRun)
	}
	if got[0].RunCount != 1 {
		t.Errorf("RunCount = %d, want 1", got[0].RunCount)
	}
}

// TestOneShotRejectsPastTime confirms Create refuses a one-shot whose instant
// already passed (otherwise we'd store a never-firing task).
func TestOneShotRejectsPastTime(t *testing.T) {
	s := New(t.TempDir() + "/sched.json")
	past := time.Now().Add(-time.Hour).Format("2006-01-02 15:04:05")
	_, err := s.Create(ScheduledTask{
		Name:       "past",
		Expression: "at " + past,
		Prompt:     "x",
	})
	if err == nil {
		t.Error("Create should reject a past one-shot time")
	}
}

// TestNormalizeInExpressionAtCreate confirms "in X" is converted to absolute
// "at ..." at Create time, so the stored task is restart-stable.
func TestNormalizeInExpressionAtCreate(t *testing.T) {
	s := New(t.TempDir() + "/sched.json")
	task, err := s.Create(ScheduledTask{
		Name:       "relative",
		Expression: "in 1h",
		Prompt:     "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(task.Expression, "at ") {
		t.Errorf("stored expression = %q, want normalized \"at ...\"", task.Expression)
	}
	if !task.OneShot {
		t.Error("normalized in-task should be OneShot=true")
	}
}

// TestHistoryRecordedAfterFire confirms a run appends a history record visible
// via History().
func TestHistoryRecordedAfterFire(t *testing.T) {
	s := New(t.TempDir() + "/sched.json")
	task, err := s.Create(ScheduledTask{Name: "hist", Expression: "every 1h", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	// Force due.
	s.mu.Lock()
	s.tasks[0].NextRun = time.Now().Add(-time.Minute)
	s.mu.Unlock()

	s.SetRunner(fakeRunner{fn: func(ctx context.Context, profile, prompt string) (string, error) {
		return "ran ok", nil
	}})
	s.fireDue(time.Now())

	recs := s.History("")
	if len(recs) != 1 {
		t.Fatalf("History = %d records, want 1", len(recs))
	}
	if recs[0].TaskID != task.ID {
		t.Errorf("history TaskID = %q, want %q", recs[0].TaskID, task.ID)
	}
	if recs[0].Status != "ok" {
		t.Errorf("history Status = %q, want ok", recs[0].Status)
	}
	if !strings.Contains(recs[0].Result, "ran ok") {
		t.Errorf("history Result = %q, want to contain 'ran ok'", recs[0].Result)
	}

	// Filter by a different task returns nothing.
	if recs := s.History("other"); len(recs) != 0 {
		t.Errorf("History(other) = %d, want 0", len(recs))
	}
}

// TestRunNowDoesNotAdvanceSchedule confirms RunNow fires immediately and records
// history, but does NOT change the task's NextRun (the scheduled fire still
// happens at its normal time).
func TestRunNowDoesNotAdvanceSchedule(t *testing.T) {
	s := New(t.TempDir() + "/sched.json")
	task, err := s.Create(ScheduledTask{Name: "ondemand", Expression: "daily 09:00", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	originalNext := s.List(false)[0].NextRun

	s.SetRunner(fakeRunner{fn: func(ctx context.Context, profile, prompt string) (string, error) {
		return "manual run", nil
	}})
	if _, err := s.RunNow(task.ID); err != nil {
		t.Fatal(err)
	}
	got := s.List(false)[0]
	if got.RunCount != 1 {
		t.Errorf("RunCount = %d, want 1", got.RunCount)
	}
	// NextRun should be unchanged (still the daily 09:00 computation).
	if !got.NextRun.Equal(originalNext) {
		t.Errorf("NextRun changed after RunNow: was %v, now %v", originalNext, got.NextRun)
	}
}

// TestDescribeHumanizer spot-checks the Describe renderer for the card UI.
func TestDescribeHumanizer(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"every 30m", "每 30 分钟"},
		{"every 2h", "每 2 小时"},
		{"hourly", "每小时"},
		{"daily 09:00", "每天 09:00"},
		{"daily 09:00 Mon-Fri", "工作日 09:00"},
		{"daily 09:00 Mon,Wed,Fri", "周一/周三/周五 09:00"},
		{"daily 10:00 weekend", "周末 10:00"},
	}
	for _, c := range cases {
		got := Describe(c.expr)
		if got != c.want {
			t.Errorf("Describe(%q) = %q, want %q", c.expr, got, c.want)
		}
	}
	// One-shot "at ..." shows the timestamp + marker.
	got := Describe("at 2026-06-24 15:00")
	if !strings.Contains(got, "2026-06-24 15:00") || !strings.Contains(got, "一次性") {
		t.Errorf("Describe(at ...) = %q, want timestamp + 一次性 marker", got)
	}
}
