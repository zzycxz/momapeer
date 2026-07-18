package builtin

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseDateRange parses since/before strings into times for IMAP SEARCH
// SINCE/BEFORE (server-side filtering by internal date = receive time). Each
// input accepts:
//   - absolute "2006-01-02" — that day at local midnight
//   - relative "7d" / "2w" / "1m" — now minus N days/weeks/months (in time.Local)
//   - "" — zero value, meaning "no filter on this side"
//
// "1m" = 30 days: calendar-month math is ambiguous across month lengths, and a
// 30-day window matches how people read "月总结" well enough for summarization.
// Returns zero times for empty inputs so callers can omit either bound.
func parseDateRange(sinceStr, beforeStr string) (since, before time.Time, err error) {
	if sinceStr = strings.TrimSpace(sinceStr); sinceStr != "" {
		since, err = parseDateOrRelative(sinceStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("since: %w", err)
		}
	}
	if beforeStr = strings.TrimSpace(beforeStr); beforeStr != "" {
		before, err = parseDateOrRelative(beforeStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("before: %w", err)
		}
	}
	return since, before, nil
}

// parseDateOrRelative parses one bound: "<n><d|w|m>" relative or "2006-01-02"
// absolute. Errors on anything else.
func parseDateOrRelative(s string) (time.Time, error) {
	// Relative: trailing unit d/w/m (case-insensitive).
	if n := len(s); n >= 2 {
		if days, ok := relativeDays(s); ok {
			return time.Now().In(time.Local).AddDate(0, 0, -days), nil
		}
	}
	// Absolute date.
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q (use 2006-01-02 or Nd/Nw/Nm, e.g. 7d)", s)
	}
	return t, nil
}

// relativeDays returns the day count for a "<n><unit>" string (d/w/m) and ok=true,
// or ok=false if it isn't one.
func relativeDays(s string) (int, bool) {
	last := s[len(s)-1]
	unit := strings.ToLower(string(last))
	if unit != "d" && unit != "w" && unit != "m" {
		return 0, false
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n < 0 {
		return 0, false
	}
	switch unit {
	case "w":
		return n * 7, true
	case "m":
		return n * 30, true
	default:
		return n, true
	}
}
