package calendar

import (
	"testing"
	"time"
)

// findHoliday returns the holiday event with the given title, or nil.
func findHoliday(events []Event, title string) *Event {
	for i := range events {
		if events[i].Title == title {
			return &events[i]
		}
	}
	return nil
}

// TestSpringFestival2026Correct is the core regression: the old code hardcoded
// 春节 as Jan 29 for every year, but 2026's Spring Festival is Feb 17.
func TestSpringFestival2026Correct(t *testing.T) {
	events := ChineseHolidays(2026)
	sf := findHoliday(events, "春节")
	if sf == nil {
		t.Fatal("春节 missing from 2026 holidays")
	}
	want := time.Date(2026, 2, 17, 0, 0, 0, 0, time.Local)
	if !sf.StartTime.Equal(want) {
		t.Errorf("2026 春节 start = %v, want %v (was hardcoded Jan 29 before fix)", sf.StartTime, want)
	}
}

// TestSpringFestivalVariesByYear confirms the lunar dates move across years
// (not a fixed Jan 29 every year).
func TestSpringFestivalVariesByYear(t *testing.T) {
	want := map[int]time.Time{
		2025: time.Date(2025, 1, 29, 0, 0, 0, 0, time.Local),
		2026: time.Date(2026, 2, 17, 0, 0, 0, 0, time.Local),
		2027: time.Date(2027, 2, 6, 0, 0, 0, 0, time.Local),
	}
	for year, w := range want {
		sf := findHoliday(ChineseHolidays(year), "春节")
		if sf == nil {
			t.Errorf("%d: 春节 missing", year)
			continue
		}
		if !sf.StartTime.Equal(w) {
			t.Errorf("%d 春节 = %v, want %v", year, sf.StartTime, w)
		}
	}
}

// TestDragonBoat2026 confirms 端午 (lunar 5th month 5th day) uses the correct
// Gregorian date — 2026: Jun 19 (the old code had May 31).
func TestDragonBoat2026Correct(t *testing.T) {
	events := ChineseHolidays(2026)
	db := findHoliday(events, "端午节")
	if db == nil {
		t.Fatal("端午节 missing")
	}
	want := time.Date(2026, 6, 19, 0, 0, 0, 0, time.Local)
	if !db.StartTime.Equal(want) {
		t.Errorf("2026 端午节 = %v, want %v", db.StartTime, want)
	}
}

// TestSolarHolidaysFixed confirms fixed-date holidays are correct.
func TestSolarHolidaysFixed(t *testing.T) {
	for _, year := range []int{2025, 2026, 2027} {
		events := ChineseHolidays(year)
		// 元旦 Jan 1
		nyd := findHoliday(events, "元旦")
		if nyd == nil || !nyd.StartTime.Equal(time.Date(year, 1, 1, 0, 0, 0, 0, time.Local)) {
			t.Errorf("%d 元旦 wrong: %v", year, nyd)
		}
		// 劳动节 May 1
		ld := findHoliday(events, "劳动节")
		if ld == nil || !ld.StartTime.Equal(time.Date(year, 5, 1, 0, 0, 0, 0, time.Local)) {
			t.Errorf("%d 劳动节 wrong: %v", year, ld)
		}
		// 国庆节 Oct 1
		nat := findHoliday(events, "国庆节")
		if nat == nil || !nat.StartTime.Equal(time.Date(year, 10, 1, 0, 0, 0, 0, time.Local)) {
			t.Errorf("%d 国庆节 wrong: %v", year, nat)
		}
	}
}

// TestIsChineseHolidayDetection confirms the predicate works for known dates.
func TestIsChineseHolidayDetection(t *testing.T) {
	// 2026 国庆节 Oct 1.
	if !IsChineseHoliday(time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)) {
		t.Error("2026-10-01 should be a holiday (国庆节)")
	}
	// 2026 春节 Feb 17.
	if !IsChineseHoliday(time.Date(2026, 2, 17, 9, 0, 0, 0, time.Local)) {
		t.Error("2026-02-17 should be a holiday (春节)")
	}
	// A random non-holiday day.
	if IsChineseHoliday(time.Date(2026, 7, 15, 12, 0, 0, 0, time.Local)) {
		t.Error("2026-07-15 should NOT be a holiday")
	}
}
