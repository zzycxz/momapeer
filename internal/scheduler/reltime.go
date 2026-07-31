package scheduler

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ResolveRelativeTime converts a Chinese natural-language time phrase into an
// absolute time. It supports a compact but practical vocabulary aimed at office
// reminders ("后天下午3点", "下周一上午9点半", "月底 23:59"). The returned time is
// in the local timezone.
//
// Recognized vocabulary:
//
//   - Date words (mutually exclusive — pick the first that matches):
//     今天 / 今日 / 明天 / 明日 / 后天 / 大后天 / 大前天
//     下周X / 下周星期X / 本周X        (X = 一/二/.../日 or 1..7)
//     周X / 星期X / 礼拜X               (this week's day X)
//     N号 / N日 / N月N日 / N月N号       (absolute month/day in the current year)
//     月底                              (last day of current month)
//     YYYY年MM月DD日                    (fully absolute)
//
//   - Time words:
//     上午N点 / 早上N点 / N点           (hour N in the morning; 0<=N<=11)
//     中午12点 / 中午N点                (noon hour)
//     下午N点 / 傍晚N点 / 晚上N点 / 夜里N点 (hour N+12 for 1<=N<=11, or N for 12)
//     N点半 / N点30分                   (N:30)
//     N点M分                            (N:M)
//     HH:MM                             (24-hour absolute)
//
// Date and time may appear in either order; missing time defaults to 00:00.
// Missing date defaults to today (but the result must be in the future; if the
// same-day resolution yields a past instant, the date advances to tomorrow for
// pure-time inputs like "下午3点" — matching how people read such phrases).
//
// Anything that doesn't match returns an error; callers (the UI preview and the
// Create path) fall back to the literal expression.
func ResolveRelativeTime(text string, now time.Time) (time.Time, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, errors.New("empty time text")
	}
	if now.IsZero() {
		now = time.Now()
	}

	day, dayOk, dayIsRelativeOffset := resolveDate(text, now)
	hour, minute, timeOk := resolveTime(text)

	// No date and no time parsed → not a phrase we understand.
	if !dayOk && !timeOk {
		return time.Time{}, fmt.Errorf("无法解析时间词 %q（支持：明天/后天/下周一/月底/下午3点/15:00 等）", text)
	}

	base := now
	if dayOk {
		base = day
	}
	if !timeOk {
		hour, minute = 0, 0
	}
	result := time.Date(base.Year(), base.Month(), base.Day(), hour, minute, 0, 0, now.Location())

	// Future-guarding:
	//   - If the resolved instant is in the past AND falls on today's calendar
	//     day, roll forward by a day (handles "今天早上8点" said at 10am, and
	//     bare "早上8点" said at 10am).
	//   - A phrase that resolves to a DIFFERENT calendar day (明天/后天/下周一/
	//     3号/…) is honored as-is even if the time-on-that-day has technically
	//     passed — the user asked for that calendar day explicitly.
	if !result.After(now) && sameCalendarDay(result, now) {
		result = result.Add(24 * time.Hour)
	}
	_ = dayIsRelativeOffset
	return result, nil
}

// sameCalendarDay reports whether a and b fall on the same YYYY-MM-DD in the
// reference location.
func sameCalendarDay(a, b time.Time) bool {
	a = a.In(b.Location())
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// resolveDate extracts a date from the text. Returns (day, ok, isExplicitPast).
// isExplicitPast distinguishes "前天/大前天" (user explicitly named a past day —
// honored as-is, no future roll) from everything else.
func resolveDate(text string, now time.Time) (time.Time, bool, bool) {
	// Fully absolute: "2026年6月24日"
	if m := reFullDate.FindStringSubmatch(text); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, now.Location()), true, false
	}
	// "N月N日" / "N月N号" (current year)
	if m := reMonthDay.FindStringSubmatch(text); m != nil {
		mo, _ := strconv.Atoi(m[1])
		d, _ := strconv.Atoi(m[2])
		y := now.Year()
		// If the referenced month/day has already passed this year, roll to next
		// year (people don't schedule reminders in the past).
		t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, now.Location())
		if t.Before(now) {
			t = t.AddDate(1, 0, 0)
		}
		return t, true, false
	}
	// "N号" / "N日" (this month). We avoid RE2's lack of lookahead by checking
	// the character that follows the match at call time — "15号" is a day,
	// "15日10点" is also a day, but we must not misfire on "日" inside other words.
	if m := reDayOfMonth.FindStringSubmatch(text); m != nil {
		// Reject if immediately followed by 点/: (those indicate we matched a
		// time-of-day fragment, not a day-of-month — e.g. the regex shouldn't,
		// but be defensive).
		idx := strings.Index(text, m[0])
		after := ""
		if idx+len(m[0]) < len(text) {
			after = text[idx+len(m[0]):]
		}
		if !strings.HasPrefix(strings.TrimSpace(after), "点") && !strings.HasPrefix(strings.TrimSpace(after), ":") {
			d, _ := strconv.Atoi(m[1])
			t := time.Date(now.Year(), now.Month(), d, 0, 0, 0, 0, now.Location())
			if t.Before(now) {
				t = t.AddDate(0, 1, 0)
			}
			return t, true, false
		}
	}
	// "月底"
	if strings.Contains(text, "月底") {
		firstNext := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
		last := firstNext.AddDate(0, 0, -1)
		return last, true, false
	}
	// Relative day offsets: 大前天/大后天/前天/后天/今天/明天/今日/明日
	switch {
	case strings.Contains(text, "大前天"):
		return now.AddDate(0, 0, -3), true, true
	case strings.Contains(text, "大后天"):
		return now.AddDate(0, 0, 3), true, true
	case strings.Contains(text, "前天"):
		return now.AddDate(0, 0, -2), true, true
	case strings.Contains(text, "后天"):
		return now.AddDate(0, 0, 2), true, true
	case strings.Contains(text, "明天") || strings.Contains(text, "明日"):
		return now.AddDate(0, 0, 1), true, true
	case strings.Contains(text, "今天") || strings.Contains(text, "今日"):
		return now, true, true
	}
	// Weekday forms: 下周X / 本周X / 周X / 星期X / 礼拜X. We try the explicit
	// prefix form first (下/本), then fall back to bare — otherwise the bare
	// alternative in a combined regex wins via leftmost-match and swallows the
	// 下/本 prefix.
	if m := reWeekdayNext.FindStringSubmatch(text); m != nil {
		prefix := m[1] // "下" or "本"
		dayNum := parseWeekdayCN(m[3])
		if dayNum >= 0 {
			return weekdayTarget(now, prefix, dayNum), true, false
		}
	}
	if m := reWeekdayBare.FindStringSubmatch(text); m != nil {
		dayNum := parseWeekdayCN(m[2])
		if dayNum >= 0 {
			return weekdayTarget(now, "", dayNum), true, false
		}
	}
	return time.Time{}, false, false
}

// weekdayTarget computes the absolute date for a (prefix, weekday) phrase.
// prefix is "下" (next week), "本" (this week), or "" (bare = next occurrence).
func weekdayTarget(now time.Time, prefix string, dayNum int) time.Time {
	// Normalize 1..7 (7=Sunday) → Go's time.Weekday (0=Sunday).
	targetWd := time.Sunday
	if dayNum < 7 {
		targetWd = time.Weekday(dayNum)
	}
	todayWd := int(now.Weekday())
	delta := (int(targetWd) - todayWd + 7) % 7
	switch prefix {
	case "下":
		// "下周X" = X of next week. delta here is days-until-this-X (0..6).
		// Next week's X is always +7 from this X, so total = delta + 7.
		// (delta==0 when today is X → next week's X is exactly +7.)
		return now.AddDate(0, 0, delta+7)
	case "本":
		// "本周X" = X of THIS week (already started). If X already passed this
		// week (delta==0 means "today is X" — keep today), honor today.
		return now.AddDate(0, 0, delta)
	default: // bare "周X" = next occurrence (today if same weekday)
		if delta == 0 {
			delta = 7
		}
		return now.AddDate(0, 0, delta)
	}
}

// extraWeekShift is reserved for future "下周末" / "下周日" tuning.
func extraWeekShift(now time.Time, dayNum int) int { return 0 } //nolint:unused

// parseWeekdayCN converts 一/二/.../日 or 1..7 to 1..7 (7=Sunday). -1 = unknown.
func parseWeekdayCN(s string) int {
	s = strings.TrimSpace(s)
	switch s {
	case "一", "1", "壹":
		return 1
	case "二", "2", "贰":
		return 2
	case "三", "3", "叁":
		return 3
	case "四", "4", "肆":
		return 4
	case "五", "5", "伍":
		return 5
	case "六", "6", "陆":
		return 6
	case "日", "天", "七", "7", "末":
		return 7
	}
	return -1
}

// resolveTime extracts (hour, minute) from the text. ok=false means no time phrase
// matched (caller defaults to 00:00).
func resolveTime(text string) (int, int, bool) {
	// "HH:MM" 24-hour absolute.
	if m := reClock.FindStringSubmatch(text); m != nil {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		if h < 0 || h > 23 || mi < 0 || mi > 59 {
			return 0, 0, false
		}
		return h, mi, true
	}
	// Period + N点[M分][半]. Periods: 凌晨/早上/上午/中午/下午/傍晚/晚上/夜里/夜间
	if m := rePeriodHour.FindStringSubmatch(text); m != nil {
		period := m[1]
		h, _ := strconv.Atoi(m[2])
		minStr := ""
		if m[3] != "" { // "M分"
			minStr = strings.TrimSuffix(m[3], "分")
		} else if m[4] != "" { // "半"
			minStr = "30"
		}
		mi, _ := strconv.Atoi(strings.TrimSpace(minStr))
		finalH := applyPeriod(period, h)
		if finalH < 0 || finalH > 23 || mi < 0 || mi > 59 {
			return 0, 0, false
		}
		return finalH, mi, true
	}
	// Bare "N点[M分][半]" with no period — treated as morning (0-11).
	if m := reBareHour.FindStringSubmatch(text); m != nil {
		h, _ := strconv.Atoi(m[1])
		minStr := ""
		if m[2] != "" {
			minStr = strings.TrimSuffix(m[2], "分")
		} else if m[3] != "" {
			minStr = "30"
		}
		mi, _ := strconv.Atoi(strings.TrimSpace(minStr))
		if h < 0 || h > 23 || mi < 0 || mi > 59 {
			return 0, 0, false
		}
		return h, mi, true
	}
	return 0, 0, false
}

// applyPeriod maps "下午3点" → 15. Morning periods leave hour unchanged (except
// 12 stays 12). Afternoon/evening periods add 12 to hours 1..11; 12 stays 12.
func applyPeriod(period string, hour int) int {
	switch period {
	case "凌晨", "早上", "清晨", "上午":
		return hour
	case "中午":
		if hour == 0 {
			return 12
		}
		return hour
	case "下午", "傍晚", "晚上", "夜里", "夜间", "午夜":
		if hour >= 1 && hour <= 11 {
			return hour + 12
		}
		return hour // 12 → 12 (noon/midnight unchanged)
	}
	return hour
}

var (
	reFullDate   = regexp.MustCompile(`(\d{4})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*日`)
	reMonthDay   = regexp.MustCompile(`(\d{1,2})\s*月\s*(\d{1,2})\s*(?:日|号)`)
	reDayOfMonth = regexp.MustCompile(`(?:^|\D)(\d{1,2})\s*(?:号|日)`)
	// Chinese weekday words: 下周X / 本周X / 周X / 星期X / 礼拜X. The prefix is
	// just 下/本 (one char); the core 周/星期/礼拜 follows. We capture the prefix
	// char separately so a bare "周三" (no 下/本) is distinguishable.
	reWeekdayNext = regexp.MustCompile(`(下|本)(周|星期|礼拜)([一二三四五六七天末]|1|2|3|4|5|6|7)`)
	reWeekdayBare = regexp.MustCompile(`(周|星期|礼拜)([一二三四五六七天末]|1|2|3|4|5|6|7)`)
	reClock       = regexp.MustCompile(`(\d{1,2})\s*[:：]\s*(\d{1,2})`)
	// Period + 点[M(分)][半]. m[3]=minutes (1-2 digits, optional 分), m[4]=半.
	// 分 is OPTIONAL: people write "8点50" as often as "8点50分", and without
	// this the minute group silently fails to match, dropping 8:50 → 8:00.
	rePeriodHour = regexp.MustCompile(`(凌晨|清晨|早上|上午|中午|下午|傍晚|晚上|夜里|夜间|午夜)\s*(\d{1,2})\s*点(?:(\d{1,2})\s*分?|(\s*半))?`)
	reBareHour   = regexp.MustCompile(`(\d{1,2})\s*点(?:(\d{1,2})\s*分?|(\s*半))?`)
)
