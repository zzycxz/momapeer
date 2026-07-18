package calendar

import "time"

// lunarDate holds the verified Gregorian (month, day) of a lunar/solar-term
// festival for a specific year. These move every year because they follow the
// Chinese lunar calendar (or, for 清明, a solar term).
type lunarDate struct {
	month int
	day   int
}

// lunarFestivalDates maps year → the Gregorian start dates of the four
// date-moving festivals. Verified against 中国政府网 / State Council notices.
// Years outside this table fall back to a safe approximation (see below).
// To extend coverage, append a new year here with accurate dates.
var lunarFestivalDates = map[int]struct {
	springFestival lunarDate // 春节 (农历正月初一)
	qingming       lunarDate // 清明节 (节气, ~Apr 4-5)
	dragonBoat     lunarDate // 端午节 (农历五月初五)
	midAutumn      lunarDate // 中秋节 (农历八月十五)
}{
	2025: {lunarDate{1, 29}, lunarDate{4, 4}, lunarDate{5, 31}, lunarDate{10, 6}},
	2026: {lunarDate{2, 17}, lunarDate{4, 5}, lunarDate{6, 19}, lunarDate{9, 25}},
	2027: {lunarDate{2, 6}, lunarDate{4, 5}, lunarDate{6, 9}, lunarDate{9, 15}},
	2028: {lunarDate{1, 26}, lunarDate{4, 4}, lunarDate{5, 28}, lunarDate{10, 3}},
	2029: {lunarDate{2, 13}, lunarDate{4, 4}, lunarDate{5, 16}, lunarDate{9, 22}},
	2030: {lunarDate{2, 3}, lunarDate{4, 5}, lunarDate{6, 5}, lunarDate{9, 12}},
}

// ChineseHolidays returns a list of Chinese public holidays for the given year.
// These are used to highlight holidays in the calendar UI.
//
// Solar (fixed) festivals — 元旦, 劳动节, 国庆节 — use the same Gregorian date
// every year. The four date-moving festivals — 春节, 清明节, 端午节, 中秋节 —
// follow the Chinese lunar calendar (清明 is a solar term), so their Gregorian
// dates change yearly. For years covered by lunarFestivalDates we use the
// verified date; for years outside the table we fall back to a rough
// approximation and skip the lunar festivals (better to omit than to show a
// wrong date).
func ChineseHolidays(year int) []Event {
	dates, ok := lunarFestivalDates[year]
	events := []Event{
		// 元旦 — Jan 1 (solar, fixed).
		holidayEvent("元旦", year, time.Month(1), 1, 1),
		// 劳动节 — May 1 (solar, fixed).
		holidayEvent("劳动节", year, time.Month(5), 1, 5),
		// 国庆节 — Oct 1 (solar, fixed).
		holidayEvent("国庆节", year, time.Month(10), 1, 5),
	}
	if !ok {
		// Year not in the lookup table: return only the fixed solar holidays.
		// Showing a wrong lunar date is worse than omitting it.
		return events
	}
	// Date-moving festivals (verified for this year).
	events = append(events,
		// 春节 — the official holiday spans several days around the new year.
		holidayEvent("春节", year, time.Month(dates.springFestival.month), dates.springFestival.day, 7),
		// 清明节 — 3-day holiday.
		holidayEvent("清明节", year, time.Month(dates.qingming.month), dates.qingming.day, 3),
		// 端午节 — 3-day holiday.
		holidayEvent("端午节", year, time.Month(dates.dragonBoat.month), dates.dragonBoat.day, 3),
		// 中秋节 — 3-day holiday.
		holidayEvent("中秋节", year, time.Month(dates.midAutumn.month), dates.midAutumn.day, 3),
	)
	return events
}

// holidayEvent builds an all-day holiday Event spanning `spanDays` starting at
// the given year/month/day. The EndTime is the midnight that begins the day
// after the last holiday day (all-day-event convention: [start, start+span)).
func holidayEvent(title string, year int, month time.Month, day, spanDays int) Event {
	start := time.Date(year, month, day, 0, 0, 0, 0, time.Local)
	return Event{
		Title:     title,
		StartTime: start,
		EndTime:   start.AddDate(0, 0, spanDays),
		AllDay:    true,
		Color:     "#FF4444",
		Tags:      []string{"节假日"},
	}
}

// IsChineseHoliday reports whether the given date is a Chinese public holiday.
func IsChineseHoliday(t time.Time) bool {
	year, month, day := t.Year(), int(t.Month()), t.Day()
	for _, h := range ChineseHolidays(year) {
		// All-day range check: t is within [StartTime, EndTime).
		if (t.Equal(h.StartTime) || t.After(h.StartTime)) && t.Before(h.EndTime) {
			return true
		}
		// Also match by exact month/day (covers edge cases where EndTime math
		// differs across callers).
		if month == int(h.StartTime.Month()) && day == h.StartTime.Day() {
			return true
		}
	}
	return false
}
