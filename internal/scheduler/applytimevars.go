package scheduler

import (
	"fmt"
	"strings"
	"time"
)

// applyTimeVars replaces time-related placeholders in the given string:
//   - {today}           → current date (YYYY-MM-DD)
//   - {week_start}      → Monday of the current week (Mon..Sun)
//   - {week_end}        → Sunday of the current week
//   - {month_start}     → first day of the current month
//   - {last_month_start}→ first day of the previous month
//   - {last_month_end}  → last day of the previous month
//
// Unknown placeholders are left as-is.
func applyTimeVars(s string, now time.Time) string {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())

	// Week: Mon..Sun. weekday() returns 1=Mon..7=Sun.
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7 // Sunday = 7
	}
	weekStart := today.AddDate(0, 0, -(wd - 1))
	weekEnd := weekStart.AddDate(0, 0, 6)

	monthStart := time.Date(y, m, 1, 0, 0, 0, 0, now.Location())

	lastMonthEnd := monthStart.AddDate(0, 0, -1)
	lastMonthStart := time.Date(lastMonthEnd.Year(), lastMonthEnd.Month(), 1, 0, 0, 0, 0, now.Location())

	replacements := map[string]string{
		"{today}":            fmt.Sprintf("%04d-%02d-%02d", y, m, d),
		"{week_start}":       weekStart.Format("2006-01-02"),
		"{week_end}":         weekEnd.Format("2006-01-02"),
		"{month_start}":      monthStart.Format("2006-01-02"),
		"{last_month_start}": lastMonthStart.Format("2006-01-02"),
		"{last_month_end}":   lastMonthEnd.Format("2006-01-02"),
	}

	for token, val := range replacements {
		s = strings.ReplaceAll(s, token, val)
	}
	return s
}
