package calendar

import (
	"log/slog"
	"time"
)

// ReminderEngine checks for due reminders every 60 seconds and fires
// notifications via the Notifier interface. It expands recurring events
// and checks each instance's reminder offset against the current time.
type ReminderEngine struct {
	store    *Store
	notifier ReminderNotifier
	logger   func(string, ...any)
	stop     chan struct{}
	done     chan struct{}
	// now returns the current time. Defaults to time.Now; overridable in tests
	// for deterministic reminder scheduling.
	now func() time.Time
}

// ReminderNotifier delivers a reminder to the user (desktop toast, etc.).
type ReminderNotifier interface {
	NotifyReminder(title, body string)
}

// NewReminderEngine creates a reminder engine that checks every 60 seconds.
func NewReminderEngine(store *Store, notifier ReminderNotifier) *ReminderEngine {
	return &ReminderEngine{
		store:    store,
		notifier: notifier,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		now:      time.Now,
	}
}

// SetLogger sets a debug logger.
func (re *ReminderEngine) SetLogger(fn func(string, ...any)) {
	re.logger = fn
}

// Start begins the reminder check loop. Call Stop() to shut down.
func (re *ReminderEngine) Start() {
	go re.loop()
}

// Stop signals the engine to shut down and waits for it to finish.
func (re *ReminderEngine) Stop() {
	close(re.stop)
	<-re.done
}

func (re *ReminderEngine) loop() {
	defer close(re.done)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// Check immediately on start
	re.check()

	for {
		select {
		case <-re.stop:
			return
		case <-ticker.C:
			re.check()
		}
	}
}

func (re *ReminderEngine) check() {
	now := time.Now()
	if re.now != nil {
		now = re.now()
	}
	events, err := re.store.DueReminders(now)
	if err != nil {
		if re.logger != nil {
			re.logger("reminder: query failed: %v", err)
		}
		return
	}

	// Expand each event into occurrences within [now-fireWindow, now+24h] so
	// recurring events (whose original start_time is months ago) still produce
	// instances whose reminder offsets we can evaluate. For non-recurring events
	// the single instance is returned unchanged.
	//
	// fireWindow is deliberately wider than the 60s tick: if the engine is
	// delayed (GC pause, process suspended, a missed tick on startup), a narrow
	// 2-minute window would let the remindAt instant slip past and the reminder
	// would NEVER fire (later ticks see now.Sub(remindAt) >= window and skip).
	// 10 minutes tolerates multiple missed ticks while still avoiding stale
	// re-fires (dedup via RemindedAt keeps each tier/instance from repeating).
	const lookahead = 24 * time.Hour
	const fireWindow = 10 * time.Minute
	instances := ExpandRecurring(events, now.Add(-fireWindow), now.Add(lookahead))

	for _, inst := range instances {
		if len(inst.Reminders) == 0 {
			continue
		}
		for _, minutes := range inst.Reminders {
			remindAt := inst.StartTime.Add(-time.Duration(minutes) * time.Minute)
			// Fire if remindAt has arrived but is no older than fireWindow (to
			// catch a remindAt that fell between two ticks).
			if now.Sub(remindAt) < 0 || now.Sub(remindAt) >= fireWindow {
				continue
			}
			// Per-reminder dedup: the stored reminded_at holds the remindAt of the
			// most recently fired reminder. If it's already at-or-after this
			// remindAt, this tier already fired (e.g. the 60-min tier blocked the
			// 15-min one) and we skip. Find the event to read its RemindedAt.
			alreadyFired := false
			for _, e := range events {
				if e.ID == inst.EventID && !e.RemindedAt.IsZero() && !e.RemindedAt.Before(remindAt) {
					alreadyFired = true
					break
				}
			}
			if alreadyFired {
				continue
			}
			re.fire(inst, minutes, remindAt)
		}
	}
}

func (re *ReminderEngine) fire(inst EventInstance, minutesBefore int, remindAt time.Time) {
	if re.notifier == nil {
		return
	}
	var body string
	if minutesBefore == 0 {
		body = inst.Title + " 现在开始"
	} else if minutesBefore < 60 {
		body = inst.Title + " 将在 " + itoa(minutesBefore) + " 分钟后开始"
	} else {
		body = inst.Title + " 将在 " + itoa(minutesBefore/60) + " 小时后开始"
	}
	if inst.Location != "" {
		body += " (" + inst.Location + ")"
	}

	if re.logger != nil {
		re.logger("reminder: firing for %q (%d min before)", inst.Title, minutesBefore)
	}
	re.notifier.NotifyReminder("日程提醒", body)

	// Record the fired remindAt (not time.Now) so a later tier (whose remindAt is
	// later) still fires: reminded_at=start-60m does not block remindAt=start-15m.
	if err := re.store.MarkReminded(inst.EventID, remindAt); err != nil && re.logger != nil {
		re.logger("reminder: mark reminded failed: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// Silence unused import warning.
var _ = slog.Info
