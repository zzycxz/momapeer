package calendar

import (
	"strings"
	"time"
)

// ExpandRecurring takes a list of events (some recurring) and a time range,
// and returns all instances (including non-recurring events) that fall within
// [since, before). Recurring events are expanded according to their RRULE.
func ExpandRecurring(events []Event, since, before time.Time) []EventInstance {
	var out []EventInstance
	for _, e := range events {
		if e.Recurrence == "" {
			// Non-recurring: include if overlaps range
			if e.EndTime.After(since) && e.StartTime.Before(before) {
				out = append(out, EventInstance{
					EventID:       e.ID,
					Title:         e.Title,
					StartTime:     e.StartTime,
					EndTime:       e.EndTime,
					AllDay:        e.AllDay,
					Color:         e.Color,
					Location:      e.Location,
					TaskID:        e.TaskID,
					Reminders:     e.Reminders,
					OutputMode:    e.OutputMode,
					OutputDest:    e.OutputDest,
					OutputAccount: e.OutputAccount,
				})
			}
			continue
		}
		// Recurring: expand
		instances := expandRRULE(e, since, before)
		out = append(out, instances...)
	}
	return out
}

// EventInstance is one occurrence of an event (for recurring events, each
// occurrence is a separate instance). Reminders carries the event's reminder
// offsets (minutes before start) so the reminder engine can compute a per-
// instance remind time without re-reading the original Event.
type EventInstance struct {
	EventID   string
	Title     string
	StartTime time.Time
	EndTime   time.Time
	AllDay    bool
	Color     string
	Location  string
	TaskID    string
	Reminders []int
	// Output fields are copied from the parent Event so the reminder engine can
	// route each instance's push without re-reading the Event.
	OutputMode    string
	OutputDest    string
	OutputAccount string
}

// expandRRULE generates instances of a recurring event within [since, before).
// Supports: FREQ=DAILY/WEEKLY/MONTHLY/YEARLY, INTERVAL, BYDAY, COUNT, UNTIL.
func expandRRULE(e Event, since, before time.Time) []EventInstance {
	rule := parseRRULE(e.Recurrence)
	duration := e.EndTime.Sub(e.StartTime)

	// Determine end condition
	maxEnd := before
	if !e.RecurrenceEnd.IsZero() && e.RecurrenceEnd.Before(before) {
		maxEnd = e.RecurrenceEnd
	}
	if rule.until != nil && rule.until.Before(maxEnd) {
		maxEnd = *rule.until
	}

	var out []EventInstance
	current := e.StartTime
	count := 0
	const maxIterations = 10000 // safety limit to prevent infinite loops

	for current.Before(maxEnd) && (rule.count == 0 || count < rule.count) && count < maxIterations {
		// Skip instances before `since` (but keep iterating for count/interval)
		instanceEnd := current.Add(duration)
		if instanceEnd.After(since) && current.Before(before) {
			out = append(out, EventInstance{
				EventID:       e.ID,
				Title:         e.Title,
				StartTime:     current,
				EndTime:       instanceEnd,
				AllDay:        e.AllDay,
				Color:         e.Color,
				Location:      e.Location,
				TaskID:        e.TaskID,
				Reminders:     e.Reminders,
				OutputMode:    e.OutputMode,
				OutputDest:    e.OutputDest,
				OutputAccount: e.OutputAccount,
			})
		}

		count++
		current = nextOccurrence(current, rule)
	}
	return out
}

type parsedRRULE struct {
	freq     string // DAILY, WEEKLY, MONTHLY, YEARLY
	interval int
	byday    []string // MO, TU, WE, TH, FR, SA, SU
	count    int
	until    *time.Time
}

func parseRRULE(s string) parsedRRULE {
	r := parsedRRULE{interval: 1}
	for _, part := range strings.Split(s, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key, val := strings.ToUpper(kv[0]), kv[1]
		switch key {
		case "FREQ":
			r.freq = val
		case "INTERVAL":
			parseInt(val, &r.interval)
			if r.interval < 1 {
				r.interval = 1
			}
		case "BYDAY":
			r.byday = strings.Split(val, ",")
		case "COUNT":
			parseInt(val, &r.count)
		case "UNTIL":
			t, err := time.Parse("20060102T150405", val)
			if err != nil {
				t, _ = time.Parse("20060102", val)
			}
			if !t.IsZero() {
				r.until = &t
			}
		}
	}
	return r
}

func nextOccurrence(current time.Time, rule parsedRRULE) time.Time {
	switch rule.freq {
	case "DAILY":
		return current.AddDate(0, 0, rule.interval)
	case "WEEKLY":
		if len(rule.byday) > 0 {
			// Find next matching weekday
			for i := 1; i <= 7; i++ {
				next := current.AddDate(0, 0, i)
				if matchesWeekday(next, rule.byday) {
					// Check if we crossed a week boundary for interval > 1
					weekDiff := weekNumber(next) - weekNumber(current)
					if rule.interval == 1 || weekDiff%rule.interval == 0 {
						return next
					}
				}
			}
		}
		return current.AddDate(0, 0, 7*rule.interval)
	case "MONTHLY":
		return current.AddDate(0, rule.interval, 0)
	case "YEARLY":
		return current.AddDate(rule.interval, 0, 0)
	default:
		return current.AddDate(0, 0, 1)
	}
}

func matchesWeekday(t time.Time, byday []string) bool {
	day := strings.ToUpper(t.Format("Mon"))
	// Convert Go format to RRULE format
	dayMap := map[string]string{
		"MON": "MO", "TUE": "TU", "WED": "WE",
		"THU": "TH", "FRI": "FR", "SAT": "SA", "SUN": "SU",
	}
	rruleDay := dayMap[day]
	for _, d := range byday {
		if strings.ToUpper(strings.TrimSpace(d)) == rruleDay {
			return true
		}
	}
	return false
}

func weekNumber(t time.Time) int {
	_, wk := t.ISOWeek()
	return wk
}

func parseInt(s string, out *int) {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	*out = n
}
