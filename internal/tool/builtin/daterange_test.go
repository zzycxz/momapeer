package builtin

import (
	"testing"
	"time"
)

func TestParseDateRangeEmpty(t *testing.T) {
	since, before, err := parseDateRange("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !since.IsZero() || !before.IsZero() {
		t.Fatal("empty inputs should yield zero times")
	}
}

func TestParseDateRangeAbsolute(t *testing.T) {
	since, _, err := parseDateRange("2026-06-01", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local)
	if !since.Equal(want) {
		t.Fatalf("since = %v, want %v", since, want)
	}
}

func TestParseDateRangeRelativeDays(t *testing.T) {
	since, _, err := parseDateRange("7d", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// ~7 days ago, allow a few seconds slack.
	got := time.Since(since)
	if got < 7*24*time.Hour-time.Minute || got > 7*24*time.Hour+time.Minute {
		t.Fatalf("7d should be ~7 days ago, got %v", got)
	}
}

func TestParseDateRangeRelativeWeeksAndMonths(t *testing.T) {
	cases := map[string]int{"1w": 7, "2w": 14, "1m": 30, "3m": 90}
	for in, days := range cases {
		since, _, err := parseDateRange(in, "")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", in, err)
		}
		got := time.Since(since)
		want := time.Duration(days) * 24 * time.Hour
		if got < want-time.Minute || got > want+time.Minute {
			t.Fatalf("%s: want ~%v, got %v", in, want, got)
		}
	}
}

func TestParseDateRangeInvalid(t *testing.T) {
	bad := []string{"notadate", "2026/06/01", "abc", "7x"}
	for _, in := range bad {
		if _, _, err := parseDateRange(in, ""); err == nil {
			t.Fatalf("expected error for %q", in)
		}
	}
}

func TestParseDateRangeBothBounds(t *testing.T) {
	since, before, err := parseDateRange("2026-06-01", "2026-06-30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if since.IsZero() || before.IsZero() {
		t.Fatal("both bounds should be set")
	}
}
