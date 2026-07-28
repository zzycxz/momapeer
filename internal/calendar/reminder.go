package calendar

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// ReminderEngine checks for due reminders every 60 seconds and fires
// notifications via the Notifier interface. It expands recurring events
// and checks each instance's reminder offset against the current time.
type ReminderEngine struct {
	store    *Store
	notifier ReminderNotifier
	imPusher ReminderIMPusher
	emailer  ReminderEmailSender
	logger   func(string, ...any)
	stop     chan struct{}
	done     chan struct{}
	// now returns the current time. Defaults to time.Now; overridable in tests
	// for deterministic reminder scheduling.
	now func() time.Time
}

// ReminderNotifier delivers a reminder to the user (desktop toast, etc.).
// This is the default/fallback channel — every reminder fires here regardless
// of OutputMode, so a reminder is never silently lost.
type ReminderNotifier interface {
	NotifyReminder(title, body string)
}

// ReminderIMPusher pushes a reminder body to an IM chat. Optional — when nil,
// events with OutputMode="im" fall back to the desktop toast only. dest is the
// event's OutputDest ("platform:chatID" or "platform:chatType:chatID").
type ReminderIMPusher interface {
	Push(ctx context.Context, dest, text string) error
}

// ReminderEmailSender emails a reminder body. Optional — when nil, events with
// OutputMode="email" fall back to the desktop toast. account selects the named
// sender ("" = default); to is the recipient.
type ReminderEmailSender interface {
	Send(ctx context.Context, account, to, subject, body string) error
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

// SetIMPusher binds the IM delivery bridge for events with OutputMode="im".
// Nil (the default) means IM output degrades to the desktop toast only.
func (re *ReminderEngine) SetIMPusher(p ReminderIMPusher) { re.imPusher = p }

// SetEmailSender binds the email delivery bridge for events with
// OutputMode="email". Nil means email output degrades to the desktop toast.
func (re *ReminderEngine) SetEmailSender(e ReminderEmailSender) { re.emailer = e }

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

	// Always fire the desktop toast first — it's the never-fail fallback, so a
	// reminder is never silently dropped even when IM/email aren't configured or
	// fail. Then, if the event opted into IM/email push, attempt that channel;
	// a push failure is logged but doesn't block the toast or the dedup mark.
	if re.notifier != nil {
		re.notifier.NotifyReminder("日程提醒", body)
	}

	switch strings.ToLower(strings.TrimSpace(inst.OutputMode)) {
	case "im":
		if re.imPusher != nil && strings.TrimSpace(inst.OutputDest) != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := re.imPusher.Push(ctx, inst.OutputDest, body); err != nil {
				if re.logger != nil {
					re.logger("reminder: IM push to %s failed: %v", inst.OutputDest, err)
				}
			}
			cancel()
		}
	case "email":
		if re.emailer != nil && strings.TrimSpace(inst.OutputDest) != "" {
			to := strings.TrimSpace(inst.OutputDest)
			subject := "日程提醒：" + inst.Title
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := re.emailer.Send(ctx, inst.OutputAccount, to, subject, body); err != nil {
				if re.logger != nil {
					re.logger("reminder: email to %s failed: %v", to, err)
				}
			}
			cancel()
		}
	}

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
