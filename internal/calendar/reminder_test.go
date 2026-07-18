package calendar

import (
	"path/filepath"
	"testing"
	"time"
)

// fakeNotifier captures reminder notifications for assertion.
type fakeNotifier struct {
	calls []string // bodies of fired reminders, in order
}

func (f *fakeNotifier) NotifyReminder(title, body string) {
	f.calls = append(f.calls, body)
}

// newTestEngine builds a ReminderEngine backed by a fresh temp DB with an
// injectable clock, so reminder timing is deterministic.
func newTestEngine(t *testing.T, now time.Time) (*ReminderEngine, *Store, *fakeNotifier) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "cal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	fn := &fakeNotifier{}
	re := NewReminderEngine(store, fn)
	re.now = func() time.Time { return now }
	return re, store, fn
}

// TestReminderOneShotFires verifies a plain (non-recurring) event fires its
// reminder at the right moment.
func TestReminderOneShotFires(t *testing.T) {
	// Event starts at 10:00; reminder 15 min before → remindAt = 09:45.
	// Set "now" to 09:45 (just due) → should fire.
	start := time.Date(2026, 7, 15, 10, 0, 0, 0, time.Local)
	now := start.Add(-15 * time.Minute)
	re, store, fn := newTestEngine(t, now)

	if err := store.Create(&Event{
		Title:     "周会",
		StartTime: start,
		EndTime:   start.Add(time.Hour),
		Reminders: []int{15},
	}); err != nil {
		t.Fatal(err)
	}

	re.check()
	if len(fn.calls) != 1 {
		t.Fatalf("expected 1 reminder, got %d: %v", len(fn.calls), fn.calls)
	}
}

// TestReminderRecurringFires is the core regression for B2: a WEEKLY recurring
// event whose original start_time is months in the past must still fire a
// reminder for its next upcoming occurrence. Before the fix DueReminders
// filtered on start_time > now, so recurring events were never returned.
func TestReminderRecurringFires(t *testing.T) {
	// First occurrence 3 months ago; recurs weekly on the same weekday/time.
	origStart := time.Date(2026, 4, 15, 9, 0, 0, 0, time.Local) // a Wednesday
	// Next occurrence: advance to the upcoming Wednesday after a fixed "now".
	now := time.Date(2026, 7, 15, 8, 45, 0, 0, time.Local) // Wednesday 08:45
	// The event recurs every week; this Wednesday's 09:00 instance has a
	// 15-min reminder due at 08:45 = now.
	re, store, fn := newTestEngine(t, now)

	if err := store.Create(&Event{
		Title:      "每周例会",
		StartTime:  origStart,
		EndTime:    origStart.Add(time.Hour),
		Reminders:  []int{15},
		Recurrence: "FREQ=WEEKLY;BYDAY=WE",
	}); err != nil {
		t.Fatal(err)
	}

	re.check()
	if len(fn.calls) != 1 {
		t.Fatalf("recurring event should fire its reminder; got %d: %v", len(fn.calls), fn.calls)
	}
}

// TestReminderMultiTierAllFire is the core regression for B3: an event with
// reminders [60, 15] should fire BOTH tiers. Before the fix MarkReminded set
// reminded_at=now (per-event), so the 60-min firing blocked the 15-min one.
func TestReminderMultiTierAllFire(t *testing.T) {
	start := time.Date(2026, 7, 15, 10, 0, 0, 0, time.Local)
	re, store, fn := newTestEngine(t, start.Add(-60*time.Minute))

	if err := store.Create(&Event{
		Title:     "评审会",
		StartTime: start,
		EndTime:   start.Add(time.Hour),
		Reminders: []int{60, 15},
	}); err != nil {
		t.Fatal(err)
	}

	// now = 09:00 → the 60-min tier (remindAt 09:00) is due.
	re.check()
	if len(fn.calls) != 1 {
		t.Fatalf("60-min tier should fire once, got %d", len(fn.calls))
	}

	// Advance the clock to 09:45 → the 15-min tier (remindAt 09:45) is due.
	// The 60-min tier must NOT block it.
	re.now = func() time.Time { return start.Add(-15 * time.Minute) }
	fn.calls = nil
	re.check()
	if len(fn.calls) != 1 {
		t.Fatalf("15-min tier should fire after the 60-min one; got %d: %v", len(fn.calls), fn.calls)
	}
}

// TestReminderDoesNotRefireSameTier ensures the same tier doesn't fire twice
// across consecutive ticks (the dedup still works after the rewrite).
func TestReminderDoesNotRefireSameTier(t *testing.T) {
	start := time.Date(2026, 7, 15, 10, 0, 0, 0, time.Local)
	now := start.Add(-15 * time.Minute)
	re, store, fn := newTestEngine(t, now)

	if err := store.Create(&Event{
		Title:     "单次提醒",
		StartTime: start,
		EndTime:   start.Add(time.Hour),
		Reminders: []int{15},
	}); err != nil {
		t.Fatal(err)
	}

	re.check()
	if len(fn.calls) != 1 {
		t.Fatalf("first tick: expected 1, got %d", len(fn.calls))
	}
	// Second tick at the same time → must not refire.
	fn.calls = nil
	re.check()
	if len(fn.calls) != 0 {
		t.Fatalf("second tick: should not refire, got %d: %v", len(fn.calls), fn.calls)
	}
}

// TestReminderRecurringFiresEveryWeek is the cross-week regression: a weekly
// recurring event must fire its reminder for the SECOND week's occurrence too,
// not just the first. Before the fix the per-event RemindedAt from week 1 could
// (if the dedup logic were wrong) block week 2. This guards that the timestamp-
// based dedup correctly distinguishes the two occurrences.
func TestReminderRecurringFiresEveryWeek(t *testing.T) {
	// Event starts two Wednesdays ago; recurs weekly on Wednesday at 09:00.
	week1Start := time.Date(2026, 7, 1, 9, 0, 0, 0, time.Local) // Wednesday
	re, store, fn := newTestEngine(t, week1Start.Add(-15*time.Minute))

	if err := store.Create(&Event{
		Title:      "每周站会",
		StartTime:  week1Start,
		EndTime:    week1Start.Add(30 * time.Minute),
		Reminders:  []int{15},
		Recurrence: "FREQ=WEEKLY;BYDAY=WE",
	}); err != nil {
		t.Fatal(err)
	}

	// Week 1 (Jul 1, 08:45 = 15 min before 09:00): should fire.
	re.check()
	if len(fn.calls) != 1 {
		t.Fatalf("week 1: expected 1 reminder, got %d", len(fn.calls))
	}

	// Week 2 (Jul 8, 08:45): the same event, next occurrence. RemindedAt is set
	// to week1's remindAt (Jul 1 08:45). Week 2's remindAt (Jul 8 08:45) is
	// later, so the dedup must NOT block it.
	re.now = func() time.Time { return week1Start.Add(7 * 24 * time.Hour).Add(-15 * time.Minute) }
	fn.calls = nil
	re.check()
	if len(fn.calls) != 1 {
		t.Fatalf("week 2: recurring event should fire again (not blocked by week 1 dedup), got %d: %v", len(fn.calls), fn.calls)
	}

	// Week 3 (Jul 15): fires again.
	re.now = func() time.Time { return week1Start.Add(14 * 24 * time.Hour).Add(-15 * time.Minute) }
	fn.calls = nil
	re.check()
	if len(fn.calls) != 1 {
		t.Fatalf("week 3: should fire again, got %d: %v", len(fn.calls), fn.calls)
	}
}
