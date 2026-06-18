package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zzycxz/momapeer/internal/config"
	"github.com/zzycxz/momapeer/internal/event"
	"github.com/zzycxz/momapeer/internal/provider"
	"github.com/zzycxz/momapeer/internal/tool"
)

// minSpawnGap prevents rapid-fire automatic dream/distill triggers within a
// single session (e.g. several turns fired in quick succession). It only
// applies to the automatic path; manual triggers are gated by inFlight instead.
const minSpawnGap = 10 * time.Second

// dreamTimeout / distillTimeout bound the background sub-agent runs.
const (
	dreamTimeout   = 5 * time.Minute
	distillTimeout = 10 * time.Minute
)

// dreamStateName is the JSON file recording Dream/Distill run history, written
// in the workspace's .momapeer/ directory (the parent of sessionDir). It exists
// because the sub-agents spawned here reuse the parent session in memory and
// never persist their own .jsonl transcript — so the previous design of scanning
// sessions/*.jsonl.meta for a topicTitle marker could never match (nothing was
// ever written). This dedicated state file is the single source of truth for
// "when did Dream/Distill last run" and the cadence gate.
const dreamStateName = "dream_state.json"

// dreamState caps how many run records we keep per kind, keeping the file tiny.
const dreamStateHistory = 20

// DreamKind identifies which self-evolution agent a record describes.
type DreamKind string

const (
	KindDream   DreamKind = "dream"
	KindDistill DreamKind = "distill"
)

// DreamTrigger records how a run was initiated.
type DreamTrigger string

const (
	TriggerAuto   DreamTrigger = "auto"
	TriggerManual DreamTrigger = "manual"
)

// DreamRun is one completed (or failed) Dream/Distill invocation.
type DreamRun struct {
	Kind      DreamKind    `json:"kind"`
	Trigger   DreamTrigger `json:"trigger"`
	StartedAt time.Time    `json:"started_at"`
	Duration  string       `json:"duration,omitempty"`
	Status    string       `json:"status"`             // "ok" | "error" | "timeout"
	Error     string       `json:"error,omitempty"`    // set when status != ok
	Memories  int          `json:"memories,omitempty"` // best-effort count when discoverable
}

// dreamStateFile is the on-disk shape.
type dreamStateFile struct {
	Runs []DreamRun `json:"runs"`
}

// spawnCoordinator serialises Dream/Distill spawning within one process. The
// previous design used package-level time.Time vars with no lock, which raced
// once manual triggers (from the desktop UI) and automatic triggers (from the
// turn loop) could fire concurrently. inFlight is the hard concurrency gate
// (only one Dream and one Distill run at a time, per kind); lastAuto is the
// automatic-path debounce that replaces minSpawnGap's stateful side effect.
//
// The cadence decision itself NEVER consults this state — it reads the on-disk
// dream_state.json so a manual run cannot perturb the automatic schedule.
type spawnCoordinator struct {
	mu       sync.Mutex
	inFlight map[DreamKind]bool
	lastAuto map[DreamKind]time.Time
}

var dreamCoord = &spawnCoordinator{
	inFlight: make(map[DreamKind]bool),
	lastAuto: make(map[DreamKind]time.Time),
}

// DreamTask is the prompt fed to a background agent for memory consolidation.
const DreamTask = `You are a memory consolidation agent. Your job is to review recent session history and extract durable knowledge into project memory.

## Instructions

1. Read the current MEMORY.md and any existing memory files to understand what's already saved.
2. Review recent sessions for:
   - Architecture decisions and their rationale
   - Patterns discovered (coding conventions, project structure, gotchas)
   - User preferences and feedback
   - Solutions to problems that took significant effort
3. For each piece of durable knowledge:
   - Use the remember tool to save it with type "project" or "reference"
   - Include WHY it matters and HOW to apply it
   - Avoid duplicating existing memories
4. If you find memories that are now outdated or contradicted by recent sessions, use the forget tool to archive them.
5. Do NOT save transient information (specific file contents, temporary debugging notes).
6. Focus on knowledge that would help a future session be more effective.

## What to save
- Project architecture and key file locations
- Build/test/lint commands and their quirks
- Coding conventions specific to this project
- Known issues and their workarounds
- User's communication preferences and expertise level
- External service integrations and their configurations

## What NOT to save
- File contents that can be re-read
- Temporary debugging state
- Information already in the codebase
- Generic programming knowledge`

// DistillTask is the prompt fed to a background agent for workflow extraction.
const DistillTask = `You are a workflow distillation agent. Your job is to review recent sessions and identify repeated manual workflows that could be automated.

## Instructions

1. Review recent session history for patterns where the same sequence of steps was repeated across multiple sessions.
2. For each repeated workflow:
   - Create a skill file (.md) that documents the workflow
   - Include clear step-by-step instructions
   - Reference specific tools and commands needed
   - Make it reusable across similar tasks
3. Save skills to .momapeer/skills/ directory.
4. Focus on workflows that would save significant time if automated.

## Good candidates for skills
- Common debugging sequences (e.g., "investigate test failure" → check logs → reproduce → fix → verify)
- Project setup patterns (e.g., "add new feature" → create branch → implement → test → PR)
- Repetitive code patterns (e.g., "add new API endpoint" → handler → route → test → docs)
- Multi-step build/deploy processes

## Not good candidates
- One-off tasks unlikely to repeat
- Tasks too specific to a single bug/feature
- Simple single-tool operations`

// dreamConfig loads the live Dream config (live-load so a settings change takes
// effect on the next turn without restarting the session). A nil error with a
// zero-value config means "feature disabled / unavailable" — callers treat that
// as "do nothing".
func dreamConfig() (config.DreamConfig, bool) {
	cfg, err := config.Load()
	if err != nil {
		return config.DreamConfig{}, false
	}
	return cfg.Dream, true
}

// dreamStatePath resolves the state file location for the given session dir.
// sessionDir is .../.momapeer/sessions; the state file lives one level up in
// .../.momapeer/. An empty sessionDir yields "" (state disabled).
func dreamStatePath(sessionDir string) string {
	if sessionDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionDir), dreamStateName)
}

// loadDreamState reads the run history. A missing/corrupt file is not an error
// — it just means no runs recorded yet.
func loadDreamState(sessionDir string) dreamStateFile {
	var st dreamStateFile
	path := dreamStatePath(sessionDir)
	if path == "" {
		return st
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	return st
}

// appendDreamRun records a run and trims to the last dreamStateHistory per kind.
// Write failures are non-fatal: the run still happened; we just lose the record.
func appendDreamRun(sessionDir string, run DreamRun) {
	path := dreamStatePath(sessionDir)
	if path == "" {
		return
	}
	st := loadDreamState(sessionDir)
	st.Runs = append(st.Runs, run)
	st.Runs = trimDreamRuns(st.Runs)
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0o644)
}

// trimDreamRuns keeps only the most recent dreamStateHistory records of each
// kind, preserving chronological order.
func trimDreamRuns(runs []DreamRun) []DreamRun {
	byKind := make(map[DreamKind][]DreamRun)
	order := []DreamKind{KindDream, KindDistill}
	for _, r := range runs {
		byKind[r.Kind] = append(byKind[r.Kind], r)
	}
	var out []DreamRun
	for _, k := range order {
		all := byKind[k]
		if len(all) > dreamStateHistory {
			all = all[len(all)-dreamStateHistory:]
		}
		out = append(out, all...)
	}
	return out
}

// LastDreamRun returns the most recent recorded run of the given kind (zero
// DreamRun if none). It reads only from disk — the cadence gate uses this so a
// manual run is visible to the next automatic decision.
func LastDreamRun(sessionDir string, kind DreamKind) (DreamRun, bool) {
	st := loadDreamState(sessionDir)
	for i := len(st.Runs) - 1; i >= 0; i-- {
		if st.Runs[i].Kind == kind {
			return st.Runs[i], true
		}
	}
	return DreamRun{}, false
}

// DreamHistory returns the recorded runs of the given kind, newest first.
func DreamHistory(sessionDir string, kind DreamKind) []DreamRun {
	st := loadDreamState(sessionDir)
	var out []DreamRun
	for i := len(st.Runs) - 1; i >= 0; i-- {
		if st.Runs[i].Kind == kind {
			out = append(out, st.Runs[i])
		}
	}
	return out
}

// workspaceOldEnough reports whether the workspace's oldest session file is
// older than interval — a cold-start gate so a brand-new project doesn't fire
// consolidation before it has any history worth consolidating. A missing or
// empty sessions directory returns false (nothing to consolidate yet).
func workspaceOldEnough(sessionDir string, interval time.Duration) bool {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return false
	}
	var oldest time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".jsonl" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mt := info.ModTime()
		if oldest.IsZero() || mt.Before(oldest) {
			oldest = mt
		}
	}
	if oldest.IsZero() {
		return false
	}
	return time.Since(oldest) >= interval
}

// shouldAutoRun decides whether an automatic Dream/Distill run is due. It
// combines: the master switch, the per-kind cadence, the inFlight gate, and the
// minSpawnGap debounce. The cadence itself is read from disk via LastDreamRun,
// never from in-memory state. A cold-start grace (first session age) only
// matters when there is no prior run at all.
func shouldAutoRun(sessionDir string, kind DreamKind, intervalDays int) bool {
	if sessionDir == "" {
		return false
	}
	dreamCoord.mu.Lock()
	if dreamCoord.inFlight[kind] {
		dreamCoord.mu.Unlock()
		return false
	}
	if time.Since(dreamCoord.lastAuto[kind]) < minSpawnGap {
		dreamCoord.mu.Unlock()
		return false
	}
	dreamCoord.mu.Unlock()

	interval := time.Duration(intervalDays) * 24 * time.Hour
	if last, ok := LastDreamRun(sessionDir, kind); ok {
		// Prior run on record: wait out the cadence.
		if time.Since(last.StartedAt) < interval {
			return false
		}
	} else {
		// Cold start: never run for this kind. Wait until the workspace has
		// accumulated enough session history (oldest .jsonl mtime as a proxy
		// for project age) that consolidation is worthwhile.
		if !workspaceOldEnough(sessionDir, interval) {
			return false
		}
	}
	dreamCoord.mu.Lock()
	dreamCoord.lastAuto[kind] = time.Now()
	dreamCoord.mu.Unlock()
	return true
}

// ShouldAutoDream reports whether the Dream agent should run this turn, based on
// the live config (master switch + cadence). It is the entry point called from
// the controller turn loop.
func ShouldAutoDream(sessionDir string) bool {
	dream, ok := dreamConfig()
	if !ok || !dream.Enabled {
		return false
	}
	return shouldAutoRun(sessionDir, KindDream, dream.DreamIntervalDays())
}

// ShouldAutoDistill reports whether the Distill agent should run this turn.
func ShouldAutoDistill(sessionDir string) bool {
	dream, ok := dreamConfig()
	if !ok || !dream.Enabled {
		return false
	}
	return shouldAutoRun(sessionDir, KindDistill, dream.DistillIntervalDays())
}

// runKind executes a Dream or Distill task under the coordinator, recording the
// outcome to dream_state.json. trigger distinguishes automatic vs manual. The
// automatic path has already passed the cadence + master-switch gate in
// ShouldAutoDream/ShouldAutoDistill before this is invoked, so here we only
// apply the master switch for manual runs, take the inFlight lock, run, and
// record. Returns the run record (including any error) and whether a run
// actually executed.
func runKind(ctx context.Context, sessionDir string, kind DreamKind, task string, timeout time.Duration, prov provider.Provider, reg *tool.Registry, sess *Session, sink event.Sink, trigger DreamTrigger) (DreamRun, bool) {
	run := DreamRun{Kind: kind, Trigger: trigger, StartedAt: time.Now()}

	// Manual triggers honor the master switch (a disabled feature cannot be
	// force-run). The automatic path was already gated upstream.
	if trigger == TriggerManual {
		if dream, ok := dreamConfig(); ok && !dream.Enabled {
			run.Status = "error"
			run.Error = "self-evolution is disabled in settings"
			return run, false
		}
	}

	// Concurrency gate. inFlight is per-kind so Dream and Distill can run in
	// parallel, but two Dreams cannot.
	dreamCoord.mu.Lock()
	if dreamCoord.inFlight[kind] {
		dreamCoord.mu.Unlock()
		run.Status = "error"
		run.Error = "a " + string(kind) + " run is already in progress"
		return run, false
	}
	dreamCoord.inFlight[kind] = true
	dreamCoord.mu.Unlock()
	defer func() {
		dreamCoord.mu.Lock()
		dreamCoord.inFlight[kind] = false
		dreamCoord.mu.Unlock()
	}()

	bgCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	sub := New(prov, reg, sess, Options{}, sink)
	err := sub.Run(bgCtx, task)
	run.Duration = time.Since(run.StartedAt).Truncate(time.Second).String()
	switch err {
	case nil:
		run.Status = "ok"
	case context.DeadlineExceeded:
		run.Status = "timeout"
		run.Error = err.Error()
	default:
		run.Status = "error"
		run.Error = err.Error()
	}
	appendDreamRun(sessionDir, run)
	return run, true
}

// SpawnDream kicks off a background dream agent if an automatic run is due. It
// runs asynchronously — the caller does not block on completion. Returns true if
// a dream agent was spawned.
func SpawnDream(ctx context.Context, sessionDir string, prov provider.Provider, reg *tool.Registry, sess *Session, sink event.Sink) bool {
	if !ShouldAutoDream(sessionDir) {
		return false
	}
	go func() {
		_, _ = runKind(ctx, sessionDir, KindDream, DreamTask, dreamTimeout, prov, reg, sess, sink, TriggerAuto)
	}()
	return true
}

// SpawnDistill kicks off a background distill agent if an automatic run is due.
func SpawnDistill(ctx context.Context, sessionDir string, prov provider.Provider, reg *tool.Registry, sess *Session, sink event.Sink) bool {
	if !ShouldAutoDistill(sessionDir) {
		return false
	}
	go func() {
		_, _ = runKind(ctx, sessionDir, KindDistill, DistillTask, distillTimeout, prov, reg, sess, sink, TriggerAuto)
	}()
	return true
}

// RunDreamOnce triggers a manual Dream run. It blocks until the run completes
// (or times out) and returns the resulting record + whether a run actually
// executed. The caller (controller → desktop) surfaces the status to the user.
func RunDreamOnce(ctx context.Context, sessionDir string, prov provider.Provider, reg *tool.Registry, sess *Session, sink event.Sink) (DreamRun, bool) {
	return runKind(ctx, sessionDir, KindDream, DreamTask, dreamTimeout, prov, reg, sess, sink, TriggerManual)
}

// RunDistillOnce triggers a manual Distill run. See RunDreamOnce.
func RunDistillOnce(ctx context.Context, sessionDir string, prov provider.Provider, reg *tool.Registry, sess *Session, sink event.Sink) (DreamRun, bool) {
	return runKind(ctx, sessionDir, KindDistill, DistillTask, distillTimeout, prov, reg, sess, sink, TriggerManual)
}

// DreamInFlight reports whether a run of the given kind is currently executing.
// Used by the desktop UI to show a "running" state and disable the trigger button.
func DreamInFlight(kind DreamKind) bool {
	dreamCoord.mu.Lock()
	defer dreamCoord.mu.Unlock()
	return dreamCoord.inFlight[kind]
}
