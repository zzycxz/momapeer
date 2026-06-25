// Package scheduler is a persistent, session-independent task scheduler for the
// coWork profile. It runs agent prompts on a recurring schedule (cron-like
// expressions or simple intervals) and survives app restarts via a JSON store.
//
// Why a separate scheduler (not part of the agent loop): a scheduled task like
// "every weekday at 9am, compile the overnight news digest and post to IM" must
// fire even when no chat tab is open, and must persist across restarts. The
// agent loop is per-tab and transient; this scheduler is app-level and owns its
// own goroutine, binding to whichever controller is currently active when a task
// fires.
//
// Expression format (intentionally simpler than full cron — covers office needs):
//   - "every 30m"            → every 30 minutes
//   - "every 2h"             → every 2 hours
//   - "daily 09:00"          → every day at 09:00 local
//   - "daily 09:00 Mon-Fri"  → weekdays only at 09:00 (day names: Mon-Sun)
//   - "hourly"               → every hour at :00
//   - "at 2026-06-24 15:00"  → one-shot at an absolute local time (auto-disables after firing)
//   - "in 2h30m" / "in 3d"   → one-shot relative offset (normalized to "at ..." before storage)
//   - A 5-field cron expression is also accepted ("0 9 * * 1-5") for power users.
//
// Relative Chinese phrases like "后天下午3点" are converted to absolute "at ..."
// form by ResolveRelativeTime before storage, so the UI always shows concrete
// instants and restarts don't drift the schedule.
//
// The scheduler is best-effort: a missed fire (app was closed) is skipped, not
// backfilled — recurring office tasks don't benefit from a burst of catch-up.
package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zzycxz/momapeer/internal/config"
)

// ScheduledTask is one recurring prompt. Prompt is the agent input fired on each
// run; Profile selects which product profile's controller runs it (default
// cowork). OutputMode/OutputDest route the result. Enabled=false pauses without
// deleting. OneShot tasks (Expression "at ...") auto-disable after their single
// fire and remain in the list for history.
type ScheduledTask struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Expression string    `json:"expression"` // "every 30m" | "daily 09:00" | "at ..." | cron
	Prompt     string    `json:"prompt"`
	Profile    string    `json:"profile,omitempty"` // empty = cowork
	Enabled    bool      `json:"enabled"`
	OneShot    bool      `json:"one_shot,omitempty"` // at/in; auto-disables after firing
	LastRun    time.Time `json:"last_run,omitempty"`
	NextRun    time.Time `json:"next_run,omitempty"`
	RunCount   int       `json:"run_count"`
	LastResult string    `json:"last_result,omitempty"` // truncated run output / error
	OutputMode string    `json:"output_mode,omitempty"` // "" | "im" | "file" | "email" | "notify"
	OutputDest string    `json:"output_dest,omitempty"` // IM channel / file path / email "to" / (notify: unused)
}

// Runner is the bridge to a controller: the scheduler calls Run with the task's
// prompt and gets back a result string. The desktop app supplies an
// implementation that targets the active cowork controller.
type Runner interface {
	Run(ctx context.Context, profile, prompt string) (string, error)
}

// IMPusher delivers a scheduled-task result to an IM channel. The desktop app
// supplies one backed by the bot gateway (gw.Push). When nil, IM output mode is
// a no-op (the result is still stored on the task for schedule_list).
type IMPusher interface {
	Push(ctx context.Context, dest, text string) error
}

// EmailSender delivers a scheduled-task result via SMTP. OutputDest is the
// recipient address (or "to;subject" to override the subject). The desktop app
// supplies one backed by the same SMTP config as the email_send tool; nil means
// email output mode degrades to store-only.
type EmailSender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Notifier surfaces a run result to the user in-app (desktop toast / event).
// The desktop app supplies one backed by Wails runtime.EventsEmit; nil means
// notify output mode degrades to store-only.
type Notifier interface {
	Notify(name, result string)
}

// RunRecord is one entry in the per-scheduler run history ring buffer. Kept
// in-memory + persisted to a sidecar JSON so the UI can show recent runs even
// after a restart.
type RunRecord struct {
	TaskID     string    `json:"task_id"`
	Name       string    `json:"name"`
	At         time.Time `json:"at"`
	Status     string    `json:"status"`       // "ok" | "error" | "skipped"
	Result     string    `json:"result"`       // truncated
	OutputMode string    `json:"output_mode"`  // echoed from the task at fire time
}

// Store persists tasks to a JSON file so they survive restarts. The file is
// rewritten atomically on every mutation.
type Store struct {
	path string
	mu   sync.Mutex
}

func newStore(path string) *Store { return &Store{path: path} }

func (s *Store) load() ([]ScheduledTask, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var tasks []ScheduledTask
	if err := json.Unmarshal(b, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *Store) save(tasks []ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Scheduler owns the task store and the firing goroutine. Create once per app
// (desktop), call Start to begin firing, SetRunner to bind a controller bridge.
type Scheduler struct {
	store       *Store
	historyPath string
	mu          sync.Mutex
	tasks       []ScheduledTask
	history     []RunRecord // newest last; capped at historyMax
	runner      Runner
	imPusher    IMPusher
	emailer     EmailSender
	notifier    Notifier
	stopCh      chan struct{}
	logf        func(format string, args ...any)
}

const historyMax = 100

// New creates a scheduler backed by storePath. The history sidecar is written
// next to storePath (storePath with ".history" suffix). Start must be called to
// fire.
func New(storePath string) *Scheduler {
	return &Scheduler{
		store:       newStore(storePath),
		historyPath: storePath + ".history",
		stopCh:      make(chan struct{}),
		logf:        func(string, ...any) {},
	}
}

// SetLogger installs a diagnostic logger (e.g. slog); default is silent.
func (s *Scheduler) SetLogger(logf func(format string, args ...any)) {
	if logf != nil {
		s.logf = logf
	}
}

// SetRunner binds the controller bridge. Required before tasks can fire; if nil
// at fire time the run is skipped with a "no runner" result.
func (s *Scheduler) SetRunner(r Runner) {
	s.mu.Lock()
	s.runner = r
	s.mu.Unlock()
}

// SetIMPusher binds the IM delivery bridge for tasks with OutputMode="im". Nil =
// IM output is a no-op (result still stored on the task).
func (s *Scheduler) SetIMPusher(p IMPusher) {
	s.mu.Lock()
	s.imPusher = p
	s.mu.Unlock()
}

// SetEmailSender binds the SMTP bridge for tasks with OutputMode="email". Nil =
// email output is a no-op (result still stored on the task).
func (s *Scheduler) SetEmailSender(e EmailSender) {
	s.mu.Lock()
	s.emailer = e
	s.mu.Unlock()
}

// SetNotifier binds the in-app notification bridge for tasks with
// OutputMode="notify". Nil = notifications are a no-op.
func (s *Scheduler) SetNotifier(n Notifier) {
	s.mu.Lock()
	s.notifier = n
	s.mu.Unlock()
}

// Load reads persisted tasks. Called by New-equivalent flows; also re-read after
// external edits. Safe to call before Start.
func (s *Scheduler) Load() error {
	tasks, err := s.store.load()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.tasks = tasks
	// Recompute NextRun for loaded tasks so the loop fires them correctly.
	now := time.Now()
	for i := range s.tasks {
		// One-shot tasks whose instant already passed are auto-disabled on load
		// (they fired in a prior session, or were missed while the app was down
		// — either way they shouldn't fire now).
		if s.tasks[i].Enabled {
			nr := nextRun(s.tasks[i].Expression, now)
			if s.tasks[i].OneShot && nr.IsZero() {
				s.tasks[i].Enabled = false
				s.tasks[i].NextRun = time.Time{}
			} else {
				s.tasks[i].NextRun = nr
			}
		}
	}
	// Best-effort load of run history sidecar (ignore errors — it's advisory).
	if hist, err := loadHistory(s.historyPath); err == nil {
		s.history = hist
	}
	s.mu.Unlock()
	// Persist any auto-disables we just applied.
	_ = s.store.save(s.tasks)
	return nil
}

// Start launches the firing goroutine. Idempotent (a second call is a no-op).
// The loop ticks every 30s and fires any task whose NextRun is due, then
// recomputes its next fire. Missed runs while stopped are skipped (no backfill).
func (s *Scheduler) Start() {
	s.mu.Lock()
	select {
	case <-s.stopCh:
		// already stopped — reset channel so we can restart.
		s.stopCh = make(chan struct{})
	default:
		// still running; no-op.
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	go s.loop()
}

// Stop halts the firing loop.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.fireDue(now)
		}
	}
}

// fireDue runs every task whose NextRun is at or before now, sequentially (a
// prompt run can be long; parallelism would multiply token cost unpredictably).
func (s *Scheduler) fireDue(now time.Time) {
	s.mu.Lock()
	due := make([]int, 0)
	for i, t := range s.tasks {
		if t.Enabled && !t.NextRun.IsZero() && !t.NextRun.After(now) {
			due = append(due, i)
		}
	}
	runner := s.runner
	pusher := s.imPusher
	emailer := s.emailer
	notifier := s.notifier
	s.mu.Unlock()

	for _, idx := range due {
		s.mu.Lock()
		t := s.tasks[idx]
		s.mu.Unlock()
		result := s.runOne(runner, t)
		// Deliver to the configured output channel. Best-effort: a delivery
		// failure doesn't fail the run (the result is stored on the task
		// regardless).
		s.deliverOutput(pusher, emailer, notifier, t, result)
		s.mu.Lock()
		// Re-validate under the lock: a task may have been deleted, swapped, or
		// — critically — updated while runOne was executing (runs can take up to
		// 10 minutes). The previous version overwrote NextRun using the STALE
		// t.Expression captured before the run, silently discarding a user's
		// mid-run Update. We re-read the current Expression so the next fire
		// honors the latest schedule. We still write LastRun/RunCount/LastResult
		// because those are facts about THIS run, not the schedule.
		if idx < len(s.tasks) && s.tasks[idx].ID == t.ID {
			currentExpr := s.tasks[idx].Expression
			currentOneShot := s.tasks[idx].OneShot
			s.tasks[idx].LastRun = now
			s.tasks[idx].RunCount++
			s.tasks[idx].LastResult = truncate(result, 500)
			// One-shot tasks auto-disable after their single fire and stay in
			// the list (preserved for history). Their NextRun is zeroed.
			if currentOneShot {
				s.tasks[idx].Enabled = false
				s.tasks[idx].NextRun = time.Time{}
			} else {
				s.tasks[idx].NextRun = nextRun(currentExpr, time.Now())
			}
			s.appendHistoryLocked(RunRecord{
				TaskID:     t.ID,
				Name:       t.Name,
				At:         now,
				Status:     runStatus(result),
				Result:     truncate(result, 500),
				OutputMode: t.OutputMode,
			})
			_ = s.store.save(s.tasks)
		}
		s.mu.Unlock()
	}
}

// runStatus classifies a run result string into "ok"/"error"/"skipped".
func runStatus(result string) string {
	switch {
	case strings.HasPrefix(result, "error:"):
		return "error"
	case strings.HasPrefix(result, "skipped:"):
		return "skipped"
	default:
		return "ok"
	}
}

// appendHistoryLocked adds a record to the ring buffer and persists. Caller
// MUST hold s.mu.
func (s *Scheduler) appendHistoryLocked(r RunRecord) {
	s.history = append(s.history, r)
	if len(s.history) > historyMax {
		s.history = s.history[len(s.history)-historyMax:]
	}
	_ = saveHistory(s.historyPath, s.history)
}

func (s *Scheduler) runOne(runner Runner, t ScheduledTask) string {
	if runner == nil {
		return "skipped: no runner bound (no active controller)"
	}
	profile := t.Profile
	if profile == "" {
		profile = config.ProfileCowork
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, err := runner.Run(ctx, profile, t.Prompt)
	if err != nil {
		return "error: " + err.Error()
	}
	return truncate(out, 500)
}

// deliverOutput routes the run result to the task's configured output channel.
//   - "im": push to the IM dest (OutputDest = "platform:chatID") via the bound
//     IMPusher. Best-effort: a nil pusher or a send error is logged, not raised.
//   - "email": SMTP-deliver to OutputDest ("to" or "to;subject"). Nil emailer
//     degrades to store-only.
//   - "notify": surface via the in-app Notifier (desktop toast). Nil notifier
//     degrades to store-only.
//   - "file": append the result to OutputDest as a text file.
//   - "" (default): result stored on the task only (visible via schedule_list).
//
// Delivery never fails the run — the task's LastResult always reflects what the
// agent produced, even if the push didn't reach its destination.
func (s *Scheduler) deliverOutput(pusher IMPusher, emailer EmailSender, notifier Notifier, t ScheduledTask, result string) {
	switch strings.ToLower(strings.TrimSpace(t.OutputMode)) {
	case "im":
		if pusher == nil || strings.TrimSpace(t.OutputDest) == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		text := fmt.Sprintf("[%s] %s\n\n%s", t.Name, t.Expression, result)
		if err := pusher.Push(ctx, t.OutputDest, text); err != nil {
			s.logf("scheduler: IM push to %s failed: %v", t.OutputDest, err)
		}
	case "email":
		if emailer == nil || strings.TrimSpace(t.OutputDest) == "" {
			return
		}
		to, subject := splitEmailDest(t.OutputDest)
		if subject == "" {
			subject = "MoMAPeer 定时任务：" + t.Name
		}
		body := fmt.Sprintf("任务：%s\n计划：%s\n\n%s", t.Name, t.Expression, result)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := emailer.Send(ctx, to, subject, body); err != nil {
			s.logf("scheduler: email to %s failed: %v", to, err)
		}
	case "notify":
		if notifier == nil {
			return
		}
		notifier.Notify(t.Name, result)
	case "file":
		if strings.TrimSpace(t.OutputDest) == "" {
			return
		}
		entry := fmt.Sprintf("[%s %s] %s\n", time.Now().Format(time.RFC3339), t.Name, result)
		appendFile(t.OutputDest, entry) // best-effort; errors swallowed
	}
}

// splitEmailDest parses "to;subject" (or just "to"). The subject may be empty.
func splitEmailDest(dest string) (to, subject string) {
	dest = strings.TrimSpace(dest)
	if i := strings.Index(dest, ";"); i >= 0 {
		return strings.TrimSpace(dest[:i]), strings.TrimSpace(dest[i+1:])
	}
	return dest, ""
}

// appendFile appends text to a file, creating it if needed. Best-effort.
func appendFile(path, text string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(text)
}

// --- task CRUD (called by the schedule_* tools) -----------------------------

// Create adds a task, validates its expression, persists, and returns it.
// "in X" expressions are normalized to absolute "at ..." against now so the
// task is restart-stable. OneShot is inferred from the resulting expression.
func (s *Scheduler) Create(t ScheduledTask) (ScheduledTask, error) {
	if strings.TrimSpace(t.Name) == "" {
		return ScheduledTask{}, errors.New("name is required")
	}
	if strings.TrimSpace(t.Prompt) == "" {
		return ScheduledTask{}, errors.New("prompt is required")
	}
	// Normalize "in ..." → "at ..." so the stored expression is restart-stable.
	normalized, err := NormalizeExpression(t.Expression, time.Now())
	if err != nil {
		return ScheduledTask{}, fmt.Errorf("expression: %w", err)
	}
	t.Expression = normalized
	if t.ID == "" {
		t.ID = taskID()
	}
	if t.Profile == "" {
		t.Profile = config.ProfileCowork
	}
	t.OneShot = IsOneShot(t.Expression)
	t.Enabled = true
	t.NextRun = nextRun(t.Expression, time.Now())
	// One-shot whose instant already passed (e.g. user typed a past "at"): refuse
	// rather than create a never-firing task.
	if t.OneShot && t.NextRun.IsZero() {
		return ScheduledTask{}, errors.New("one-shot time is in the past; pick a future instant")
	}

	s.mu.Lock()
	s.tasks = append(s.tasks, t)
	err = s.store.save(s.tasks)
	s.mu.Unlock()
	if err != nil {
		return ScheduledTask{}, err
	}
	return t, nil
}

// List returns all tasks (optionally enabled-only).
func (s *Scheduler) List(enabledOnly bool) []ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ScheduledTask, 0, len(s.tasks))
	for _, t := range s.tasks {
		if enabledOnly && !t.Enabled {
			continue
		}
		out = append(out, t)
	}
	return out
}

// Delete removes a task by id.
func (s *Scheduler) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tasks {
		if t.ID == id {
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			_ = s.store.save(s.tasks)
			return true
		}
	}
	return false
}

// Update mutates a task's mutable fields (name/expression/prompt/enabled/...).
// "in ..." expressions in the mutation are normalized to absolute "at ...".
func (s *Scheduler) Update(id string, mut func(*ScheduledTask)) (ScheduledTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			mut(&s.tasks[i])
			normalized, err := NormalizeExpression(s.tasks[i].Expression, time.Now())
			if err != nil {
				return ScheduledTask{}, fmt.Errorf("expression: %w", err)
			}
			s.tasks[i].Expression = normalized
			s.tasks[i].OneShot = IsOneShot(normalized)
			if s.tasks[i].Enabled {
				nr := nextRun(s.tasks[i].Expression, time.Now())
				if s.tasks[i].OneShot && nr.IsZero() {
					return ScheduledTask{}, errors.New("one-shot time is in the past; pick a future instant")
				}
				s.tasks[i].NextRun = nr
			} else {
				s.tasks[i].NextRun = time.Time{}
			}
			_ = s.store.save(s.tasks)
			return s.tasks[i], nil
		}
	}
	return ScheduledTask{}, fmt.Errorf("task %q not found", id)
}

// History returns recent run records, newest first. If taskID is non-empty, only
// records for that task are returned. Limited to the in-memory ring buffer.
func (s *Scheduler) History(taskID string) []RunRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RunRecord, 0, len(s.history))
	for i := len(s.history) - 1; i >= 0; i-- { // newest first
		r := s.history[i]
		if taskID != "" && r.TaskID != taskID {
			continue
		}
		out = append(out, r)
	}
	return out
}

// NextRunPublic is the exported wrapper around nextRun (package-internal), for
// callers outside scheduler (the desktop preview bridge) that need to compute a
// fire time without a Scheduler instance.
func NextRunPublic(expr string, from time.Time) time.Time { return nextRun(expr, from) }

// Get returns a single task by ID (zero value + false if not found).
func (s *Scheduler) Get(id string) (ScheduledTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.ID == id {
			return t, true
		}
	}
	return ScheduledTask{}, false
}

// RunNow fires a task immediately, regardless of its schedule. Delivery runs as
// usual and a history record is appended. The task's schedule is NOT advanced
// (it still fires at its next scheduled time). Returns the truncated result.
func (s *Scheduler) RunNow(id string) (string, error) {
	s.mu.Lock()
	tp, ok := s.findLocked(id)
	var t ScheduledTask
	if ok {
		t = *tp // snapshot copy; the run can be long and the task may be mutated
	}
	runner := s.runner
	pusher := s.imPusher
	emailer := s.emailer
	notifier := s.notifier
	s.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("task %q not found", id)
	}
	result := s.runOne(runner, t)
	s.deliverOutput(pusher, emailer, notifier, t, result)
	now := time.Now()
	s.mu.Lock()
	if cur, ok := s.findLocked(id); ok {
		cur.LastRun = now
		cur.RunCount++
		cur.LastResult = truncate(result, 500)
		s.appendHistoryLocked(RunRecord{
			TaskID:     t.ID,
			Name:       t.Name,
			At:         now,
			Status:     runStatus(result),
			Result:     truncate(result, 500),
			OutputMode: t.OutputMode,
		})
		_ = s.store.save(s.tasks)
	}
	s.mu.Unlock()
	return result, nil
}

// findLocked returns a pointer to the task with the given id. Caller MUST hold s.mu.
func (s *Scheduler) findLocked(id string) (*ScheduledTask, bool) {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			return &s.tasks[i], true
		}
	}
	return nil, false
}

func taskID() string {
	return fmt.Sprintf("sched_%d", time.Now().UnixNano())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// loadHistory reads the persisted run-history sidecar. Missing file = empty.
func loadHistory(path string) ([]RunRecord, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var recs []RunRecord
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil, err
	}
	return recs, nil
}

// saveHistory writes the run-history sidecar atomically.
func saveHistory(path string, recs []RunRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
