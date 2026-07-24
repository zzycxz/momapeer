package calendar

import (
	"testing"
	"time"
)

func TestExpandRecurring_Daily(t *testing.T) {
	events := []Event{
		{
			ID:         "evt1",
			Title:      "Daily Standup",
			StartTime:  time.Date(2026, 7, 7, 9, 0, 0, 0, time.Local),
			EndTime:    time.Date(2026, 7, 7, 9, 30, 0, 0, time.Local),
			Recurrence: "FREQ=DAILY",
		},
	}

	since := time.Date(2026, 7, 7, 0, 0, 0, 0, time.Local)
	before := time.Date(2026, 7, 11, 0, 0, 0, 0, time.Local)

	instances := ExpandRecurring(events, since, before)
	if len(instances) != 4 {
		t.Fatalf("expected 4 instances (Jul 7-10), got %d", len(instances))
	}
	if instances[0].Title != "Daily Standup" {
		t.Errorf("title = %q", instances[0].Title)
	}
}

func TestExpandRecurring_Weekly(t *testing.T) {
	events := []Event{
		{
			ID:         "evt2",
			Title:      "Weekly Meeting",
			StartTime:  time.Date(2026, 7, 6, 10, 0, 0, 0, time.Local), // Monday
			EndTime:    time.Date(2026, 7, 6, 11, 0, 0, 0, time.Local),
			Recurrence: "FREQ=WEEKLY;BYDAY=MO",
		},
	}

	since := time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local)
	before := time.Date(2026, 7, 31, 0, 0, 0, 0, time.Local)

	instances := ExpandRecurring(events, since, before)
	if len(instances) < 3 {
		t.Fatalf("expected at least 3 weekly instances, got %d", len(instances))
	}
}

func TestExpandRecurring_NonRecurring(t *testing.T) {
	events := []Event{
		{
			ID:        "evt3",
			Title:     "One-off",
			StartTime: time.Date(2026, 7, 10, 14, 0, 0, 0, time.Local),
			EndTime:   time.Date(2026, 7, 10, 15, 0, 0, 0, time.Local),
		},
	}

	since := time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local)
	before := time.Date(2026, 7, 31, 0, 0, 0, 0, time.Local)

	instances := ExpandRecurring(events, since, before)
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
}

func TestExpandRecurring_OutOfRange(t *testing.T) {
	events := []Event{
		{
			ID:        "evt4",
			Title:     "Past",
			StartTime: time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local),
			EndTime:   time.Date(2026, 6, 1, 11, 0, 0, 0, time.Local),
		},
	}

	since := time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local)
	before := time.Date(2026, 7, 31, 0, 0, 0, 0, time.Local)

	instances := ExpandRecurring(events, since, before)
	if len(instances) != 0 {
		t.Fatalf("expected 0 instances, got %d", len(instances))
	}
}

func TestParseRRULE(t *testing.T) {
	tests := []struct {
		input    string
		freq     string
		interval int
		byday    int
		count    int
	}{
		{"FREQ=DAILY", "DAILY", 1, 0, 0},
		{"FREQ=WEEKLY;BYDAY=MO", "WEEKLY", 1, 1, 0},
		{"FREQ=WEEKLY;BYDAY=MO,WE,FR", "WEEKLY", 1, 3, 0},
		{"FREQ=MONTHLY;INTERVAL=2", "MONTHLY", 2, 0, 0},
		{"FREQ=DAILY;COUNT=10", "DAILY", 1, 0, 10},
	}

	for _, tt := range tests {
		r := parseRRULE(tt.input)
		if r.freq != tt.freq {
			t.Errorf("parseRRULE(%q).freq = %q, want %q", tt.input, r.freq, tt.freq)
		}
		if r.interval != tt.interval {
			t.Errorf("parseRRULE(%q).interval = %d, want %d", tt.input, r.interval, tt.interval)
		}
		if len(r.byday) != tt.byday {
			t.Errorf("parseRRULE(%q).byday len = %d, want %d", tt.input, len(r.byday), tt.byday)
		}
		if r.count != tt.count {
			t.Errorf("parseRRULE(%q).count = %d, want %d", tt.input, r.count, tt.count)
		}
	}
}
