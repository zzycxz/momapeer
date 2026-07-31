package scheduler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseExpression validates an expression and returns it normalized. It supports:
//   - "every 30m" / "every 2h" / "every 45s"
//   - "hourly"                       (== every 1h)
//   - "daily 09:00"                  (daily at 09:00 local)
//   - "daily 09:00 Mon-Fri"          (weekdays at 09:00)
//   - "daily 09:00 Mon,Wed,Fri"      (specific weekdays)
//   - "at 2026-06-24 15:00"          (one-shot absolute local time; auto-disables after firing)
//   - "in 2h30m" / "in 3d"           (one-shot relative offset, normalized to "at <now+offset>")
//   - 5-field cron "0 9 * * 1-5"     (power-user fallback)
//
// Relative natural-language words ("后天下午3点") are NOT parsed by this function
// — NormalizeExpression handles them by first trying parseExpression, then falling
// back to ResolveRelativeTime to convert a Chinese phrase into "at YYYY-MM-DD HH:MM".
func parseExpression(expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", errors.New("empty expression")
	}
	low := strings.ToLower(expr)
	switch {
	case low == "hourly" || strings.HasPrefix(low, "every "):
		if _, err := parseEvery(low); err != nil {
			return "", err
		}
		return expr, nil
	case strings.HasPrefix(low, "at "):
		if _, err := parseAt(expr); err != nil {
			return "", err
		}
		return expr, nil
	case strings.HasPrefix(low, "in "):
		// "in X" is a relative one-shot. Without a reference time we can't fix
		// the absolute instant, so callers must normalize it (Create does this
		// via NormalizeExpression) before storing. Reject raw "in" here.
		return "", errors.New("\"in\" expressions must be converted to an absolute \"at YYYY-MM-DD HH:MM\" first (use NormalizeExpression)")
	case strings.HasPrefix(low, "daily"):
		if _, _, err := parseDaily(expr); err != nil {
			return "", err
		}
		return expr, nil
	default:
		if _, err := parseCron(expr); err != nil {
			return "", fmt.Errorf("not a recognized expression (try \"every 30m\", \"daily 09:00\", \"at 2026-06-24 15:00\", or 5-field cron): %w", err)
		}
		return expr, nil
	}
}

// NormalizeExpression converts relative forms to their stored canonical form.
//   - "in 2h" / "in 3d" are resolved against `now` into an absolute "at ..." so
//     the persisted task is restart-stable (a relative offset would otherwise
//     drift forward every load).
//   - Chinese natural-language phrases ("后天下午3点", "9点50", "下周一 10:00")
//     are resolved via ResolveRelativeTime into "at YYYY-MM-DD HH:MM". This is the
//     fix for the "saved a natural-language task but it errored on save" bug —
//     the preview path (PreviewSchedule) always resolved these, but Create/Update
//     went straight to parseExpression (which only knows every/daily/at/in/cron),
//     so a phrase the UI showed as valid became a parse error on save.
//   - "at ..." and all other forms pass through.
func NormalizeExpression(expr string, now time.Time) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", errors.New("empty expression")
	}
	low := strings.ToLower(expr)
	if strings.HasPrefix(low, "in ") {
		t, err := parseIn(expr, now)
		if err != nil {
			return "", err
		}
		return "at " + t.Format("2006-01-02 15:04"), nil
	}
	// Try the known expression forms first (cheap, exact). If that fails, attempt
	// ResolveRelativeTime — this catches Chinese NL phrases. Only if BOTH fail do
	// we return the original parse error (so the user still gets a clear message
	// for genuinely malformed input).
	if canonical, err := parseExpression(expr); err == nil {
		return canonical, nil
	}
	if t, err := ResolveRelativeTime(expr, now); err == nil {
		return "at " + t.Format("2006-01-02 15:04"), nil
	}
	// Neither path worked — re-run parseExpression to produce the canonical error
	// message (which is more actionable than ResolveRelativeTime's).
	_, err := parseExpression(expr)
	return "", err
}

// IsOneShot reports whether an expression fires once and then should auto-disable.
// "at ..." (and its "in ..." source, once normalized) are one-shot; everything
// else repeats.
func IsOneShot(expr string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(expr)), "at ")
}

// Describe renders an expression as a friendly Chinese phrase for UI display:
//   - "every 30m"     → "每 30 分钟"
//   - "every 2h"      → "每 2 小时"
//   - "hourly"        → "每小时"
//   - "daily 09:00"   → "每天 09:00"
//   - "daily 09:00 Mon-Fri" → "工作日 09:00"
//   - "daily 09:00 Sat,Sun" → "周末 09:00"
//   - "daily 09:00 Mon,Wed,Fri" → "周一/三/五 09:00"
//   - "at ..."        → the stored timestamp (one-shot)
//   - cron / unknown  → the raw expression
func Describe(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ""
	}
	low := strings.ToLower(expr)
	switch {
	case low == "hourly":
		return "每小时"
	case strings.HasPrefix(low, "every "):
		d, err := parseEvery(low)
		if err != nil {
			return expr
		}
		if d%time.Hour == 0 {
			return fmt.Sprintf("每 %d 小时", int(d/time.Hour))
		}
		if d%time.Minute == 0 {
			return fmt.Sprintf("每 %d 分钟", int(d/time.Minute))
		}
		return "每 " + d.String()
	case strings.HasPrefix(low, "at "):
		// "at 2026-06-24 15:00" → "2026-06-24 15:00（一次性）"
		rest := strings.TrimSpace(expr[2:])
		return rest + "（一次性）"
	case strings.HasPrefix(low, "daily"):
		fields := strings.Fields(expr)
		if len(fields) < 2 {
			return expr
		}
		tod := fields[1]
		if len(fields) >= 3 {
			dayField := strings.ToLower(strings.Join(fields[2:], " "))
			switch dayField {
			case "mon-fri", "weekday", "weekdays":
				return "工作日 " + tod
			case "weekend", "weekends":
				return "周末 " + tod
			}
			// Translate weekday tokens (Mon,Wed,Fri → 周一/三/五).
			parts := strings.Split(dayField, ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if cn := weekdayShortCN(p); cn != "" {
					out = append(out, cn)
				} else if strings.Contains(p, "-") {
					ends := strings.SplitN(p, "-", 2)
					a, b := weekdayShortCN(strings.TrimSpace(ends[0])), weekdayShortCN(strings.TrimSpace(ends[1]))
					if a != "" && b != "" {
						out = append(out, a+"-"+b)
					} else {
						out = append(out, p)
					}
				} else {
					out = append(out, p)
				}
			}
			return strings.Join(out, "/") + " " + tod
		}
		return "每天 " + tod
	default:
		return expr
	}
}

// weekdayShortCN converts an English weekday abbreviation ("mon".."sun") to its
// Chinese short form ("周一".."周日"). Empty string for unknown input.
func weekdayShortCN(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mon", "monday":
		return "周一"
	case "tue", "tuesday":
		return "周二"
	case "wed", "wednesday":
		return "周三"
	case "thu", "thursday":
		return "周四"
	case "fri", "friday":
		return "周五"
	case "sat", "saturday":
		return "周六"
	case "sun", "sunday":
		return "周日"
	}
	return ""
}

// parseAt parses "at 2026-06-24 15:00" (local time). Accepts a few common layouts.
func parseAt(expr string) (time.Time, error) {
	// Strip the leading "at" from the ORIGINAL to preserve case of the timestamp.
	rest := strings.TrimSpace(expr[2:])
	layouts := []string{
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006/01/02 15:04",
		time.RFC3339,
	}
	var lastErr error
	for _, lay := range layouts {
		t, err := time.ParseInLocation(lay, rest, time.Local)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return time.Time{}, fmt.Errorf("\"at\" needs a timestamp like \"at 2026-06-24 15:00\": %w", lastErr)
	}
	return time.Time{}, errors.New("invalid \"at\" timestamp")
}

// parseIn parses "in 2h30m" / "in 3d" against `now` and returns the absolute time.
// Supports Go duration syntax (h/m/s) plus "Nd" / "Nw" day/week units.
func parseIn(expr string, now time.Time) (time.Time, error) {
	rest := strings.TrimSpace(expr[2:])
	t, err := parseRelativeOffset(rest, now)
	if err != nil {
		return time.Time{}, fmt.Errorf("\"in\" needs a duration like \"in 2h30m\" or \"in 3d\": %w", err)
	}
	return t, nil
}

// parseRelativeOffset parses "2h30m", "3d", "1w", "2d12h" relative to now.
func parseRelativeOffset(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty offset")
	}
	// Tokenize day/week units, then let ParseDuration handle the rest.
	total := time.Duration(0)
	rest := s
	// Match Nd / Nw greedily.
	for {
		idx := strings.IndexAny(rest, "dw")
		if idx <= 0 {
			break
		}
		numStr := rest[:idx]
		n, err := strconv.Atoi(numStr)
		if err != nil {
			return time.Time{}, fmt.Errorf("bad number before %q in %q", rest[idx], s)
		}
		switch rest[idx] {
		case 'd':
			total += time.Duration(n) * 24 * time.Hour
		case 'w':
			total += time.Duration(n) * 7 * 24 * time.Hour
		}
		rest = strings.TrimSpace(rest[idx+1:])
	}
	if rest != "" {
		d, err := time.ParseDuration(rest)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid duration %q: %w", rest, err)
		}
		total += d
	}
	if total <= 0 {
		return time.Time{}, errors.New("offset must be positive")
	}
	return now.Add(total), nil
}

// nextRun computes the next fire time at or after `from`. For one-shot "at ..."
// expressions, returns the stored instant if still in the future; a zero time if
// it has already passed (callers treat zero as "no future fire — auto-disable").
func nextRun(expr string, from time.Time) time.Time {
	expr = strings.TrimSpace(expr)
	now := from
	if now.IsZero() {
		now = time.Now()
	}
	low := strings.ToLower(expr)
	switch {
	case strings.HasPrefix(low, "at "):
		t, err := parseAt(expr)
		if err != nil {
			return time.Time{}
		}
		// One-shot: no future fire once the instant has passed.
		if !t.After(now) {
			return time.Time{}
		}
		return t
	case low == "hourly" || strings.HasPrefix(low, "every "):
		d, _ := parseEvery(low)
		return now.Add(d)
	case strings.HasPrefix(low, "daily"):
		_, days, err := parseDaily(expr)
		if err != nil {
			return now.Add(24 * time.Hour)
		}
		return nextDaily(expr, now, days)
	default:
		return nextCron(expr, now)
	}
}

func parseEvery(low string) (time.Duration, error) {
	if low == "hourly" {
		return time.Hour, nil
	}
	rest := strings.TrimPrefix(low, "every ")
	fields := strings.Fields(rest)
	if len(fields) != 1 {
		return 0, errors.New("\"every\" needs one duration, e.g. \"every 30m\"")
	}
	d, err := time.ParseDuration(fields[0])
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", fields[0], err)
	}
	if d < time.Minute {
		return 0, errors.New("minimum interval is 1m (avoid hot-looping)")
	}
	return d, nil
}

// parseDaily returns the time-of-day duration and allowed weekdays (1=Mon..7=Sun;
// empty map = every day).
func parseDaily(expr string) (time.Duration, map[int]bool, error) {
	fields := strings.Fields(expr)
	if len(fields) < 2 {
		return 0, nil, errors.New("\"daily\" needs a time, e.g. \"daily 09:00\"")
	}
	tod, err := parseTimeOfDay(fields[1])
	if err != nil {
		return 0, nil, err
	}
	days := map[int]bool{}
	if len(fields) >= 3 {
		dayField := strings.Join(fields[2:], " ")
		days, err = parseWeekdays(dayField)
		if err != nil {
			return 0, nil, err
		}
	}
	return tod, days, nil
}

func parseTimeOfDay(s string) (time.Duration, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("time %q must be HH:MM", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("invalid hour in %q", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("invalid minute in %q", s)
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, nil
}

var weekdayNames = map[string]int{
	"sun": 7, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
	"sunday": 7, "monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4, "friday": 5, "saturday": 6,
}

func parseWeekdays(s string) (map[int]bool, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "weekday", "weekdays", "mon-fri":
		return map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true}, nil
	case "weekend", "weekends":
		return map[int]bool{6: true, 7: true}, nil
	}
	out := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			ends := strings.SplitN(part, "-", 2)
			from, ok1 := weekdayNames[strings.TrimSpace(ends[0])]
			to, ok2 := weekdayNames[strings.TrimSpace(ends[1])]
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("unknown weekday in %q", part)
			}
			for d := from; d <= to; d++ {
				out[d] = true
			}
		} else {
			d, ok := weekdayNames[part]
			if !ok {
				return nil, fmt.Errorf("unknown weekday %q", part)
			}
			out[d] = true
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no weekdays parsed from %q", s)
	}
	return out, nil
}

func nextDaily(expr string, from time.Time, allowedDays map[int]bool) time.Time {
	fields := strings.Fields(expr)
	tod, _ := parseTimeOfDay(fields[1])
	candidate := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location()).Add(tod)
	if !candidate.After(from) {
		candidate = candidate.Add(24 * time.Hour)
	}
	for i := 0; i < 8; i++ {
		wd := int(candidate.Weekday())
		if wd == 0 {
			wd = 7
		}
		if len(allowedDays) == 0 || allowedDays[wd] {
			return candidate
		}
		candidate = candidate.Add(24 * time.Hour)
	}
	return from.Add(24 * time.Hour)
}

// --- 5-field cron -----------------------------------------------------------

func parseCron(expr string) ([5]string, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return [5]string{}, fmt.Errorf("cron needs 5 fields, got %d", len(fields))
	}
	return [5]string{fields[0], fields[1], fields[2], fields[3], fields[4]}, nil
}

// nextCron brute-forces minute-by-minute from `now` up to a year ahead. Simple
// and obviously correct; cron expressions are tiny so the cost is negligible.
func nextCron(expr string, now time.Time) time.Time {
	fields, err := parseCron(expr)
	if err != nil {
		return now.Add(time.Hour)
	}
	mins := intSet(cronExpand(fields[0], 0, 59))
	hours := intSet(cronExpand(fields[1], 0, 23))
	doms := intSet(cronExpand(fields[2], 1, 31))
	mons := intSet(cronExpand(fields[3], 1, 12))
	dowsRaw := cronExpand(fields[4], 0, 7)
	// Normalize 7 → 0 (both Sunday).
	dows := map[int]bool{}
	for _, d := range dowsRaw {
		if d == 7 {
			d = 0
		}
		dows[d] = true
	}
	domStar := fields[2] == "*"
	dowStar := fields[4] == "*"

	cand := now.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 366*24*60; i++ {
		if !mins[cand.Minute()] || !hours[cand.Hour()] || !mons[int(cand.Month())] {
			cand = cand.Add(time.Minute)
			continue
		}
		domMatch := doms[cand.Day()]
		dowMatch := dows[int(cand.Weekday())]
		// Standard cron: if both restricted, OR; if either is *, the other applies.
		if !domStar && !dowStar {
			if domMatch || dowMatch {
				return cand
			}
		} else if !domStar {
			if domMatch {
				return cand
			}
		} else if !dowStar {
			if dowMatch {
				return cand
			}
		} else {
			return cand
		}
		cand = cand.Add(time.Minute)
	}
	return now.Add(24 * time.Hour)
}

func intSet(vs []int) map[int]bool {
	m := make(map[int]bool, len(vs))
	for _, v := range vs {
		m[v] = true
	}
	return m
}

// cronExpand parses one cron field into its matched ints. Supports *, comma-lists,
// ranges (a-b), and steps (*/n or a-b/n).
func cronExpand(field string, min, max int) []int {
	if field == "*" {
		out := make([]int, 0, max-min+1)
		for v := min; v <= max; v++ {
			out = append(out, v)
		}
		return out
	}
	var out []int
	for _, part := range strings.Split(field, ",") {
		out = append(out, cronPart(strings.TrimSpace(part), min, max)...)
	}
	return out
}

func cronPart(part string, min, max int) []int {
	step := 1
	rangePart := part
	if idx := strings.Index(part, "/"); idx >= 0 {
		rangePart = part[:idx]
		if s, err := strconv.Atoi(part[idx+1:]); err == nil && s > 0 {
			step = s
		}
	}
	var from, to int
	if rangePart == "*" {
		from, to = min, max
	} else if idx := strings.Index(rangePart, "-"); idx >= 0 {
		from, _ = strconv.Atoi(rangePart[:idx])
		to, _ = strconv.Atoi(rangePart[idx+1:])
	} else {
		from, _ = strconv.Atoi(rangePart)
		to = from
	}
	if from < min {
		from = min
	}
	if to > max {
		to = max
	}
	var out []int
	for v := from; v <= to; v += step {
		out = append(out, v)
	}
	return out
}
