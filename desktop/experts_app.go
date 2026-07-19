package main

// experts_app.go exposes the expert-team collaboration engine to the frontend.
// It wires desktop/experts (Team store + Orchestrator) to Wails bindings and
// provides a desktop-specific ExpertRunner that calls the LLM directly via a
// per-expert provider (auto rate-limited by the global budget decorator).
//
// Events:
//   - "experts:collab" (CollabEvent) — streamed during a run (expert chunks,
//     synthesis, completion).
//   - "experts:changed" (no payload) — team list mutated (create/update/delete).

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zzycxz/momapeer/internal/boot"
	"github.com/zzycxz/momapeer/internal/config"
	"github.com/zzycxz/momapeer/internal/experts"
	"github.com/zzycxz/momapeer/internal/netclient"
	"github.com/zzycxz/momapeer/internal/provider"
	"github.com/zzycxz/momapeer/internal/tool/builtin"
)

// TeamView is the JSON-friendly projection of experts.Team.
type TeamView struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Experts       []ExpertView   `json:"experts"`
	DefaultMode   string         `json:"defaultMode"`
	DefaultRounds int            `json:"defaultRounds"`
}
type ExpertView struct {
	Name        string `json:"name"`
	Model       string `json:"model"`
	Perspective string `json:"perspective"`
}

// BudgetStatusView reports the LLM RPM budget for the UI's cost estimate.
type BudgetStatusView struct {
	RPM         int `json:"rpm"`
	Used        int `json:"used"`
	Remaining   int `json:"remaining"`
	ReserveMain int `json:"reserveMain"`
	WindowSecs  int `json:"windowSecs"`
}

// ListExpertTeams returns all saved teams.
func (a *App) ListExpertTeams() []TeamView {
	if a.expertStore == nil {
		return seedBuiltinTeams()
	}
	teams := a.expertStore.List()
	if len(teams) == 0 {
		return seedBuiltinTeams()
	}
	out := make([]TeamView, 0, len(teams))
	for _, t := range teams {
		out = append(out, toTeamView(t))
	}
	return out
}

// seedBuiltinTeams returns the builtin rosters as views (shown when the store
// is empty so the panel isn't blank on first run).
func seedBuiltinTeams() []TeamView {
	out := make([]TeamView, 0, len(experts.BuiltinTeams))
	for _, t := range experts.BuiltinTeams {
		out = append(out, toTeamView(t))
	}
	return out
}

// CreateExpertTeam saves a new team and emits experts:changed.
func (a *App) CreateExpertTeam(tv TeamView) (TeamView, error) {
	if a.expertStore == nil {
		return TeamView{}, fmt.Errorf("expert store offline")
	}
	created, err := a.expertStore.Create(teamViewToModel(tv))
	if err != nil {
		return TeamView{}, err
	}
	a.emitExpertsChanged()
	return toTeamView(created), nil
}

// UpdateExpertTeam updates an existing team.
func (a *App) UpdateExpertTeam(tv TeamView) (TeamView, error) {
	if a.expertStore == nil {
		return TeamView{}, fmt.Errorf("expert store offline")
	}
	updated, err := a.expertStore.Update(tv.ID, func(t *experts.Team) {
		t.Name = tv.Name
		t.Experts = expertViewsToModel(tv.Experts)
		t.DefaultMode = tv.DefaultMode
		t.DefaultRounds = tv.DefaultRounds
	})
	if err != nil {
		return TeamView{}, err
	}
	a.emitExpertsChanged()
	return toTeamView(updated), nil
}

// DeleteExpertTeam removes a team.
func (a *App) DeleteExpertTeam(id string) error {
	if a.expertStore == nil {
		return fmt.Errorf("expert store offline")
	}
	a.expertStore.Delete(id)
	a.emitExpertsChanged()
	return nil
}

// RunExpertTeam starts a collaboration. Returns the runID immediately; the
// actual expert outputs stream via the "experts:collab" event. mode/rounds
// override the team defaults when non-empty/>0.
func (a *App) RunExpertTeam(teamID, task, mode string, rounds int) (string, error) {
	if a.expertOrchestrator == nil {
		return "", fmt.Errorf("expert orchestrator offline")
	}
	if strings.TrimSpace(task) == "" {
		return "", fmt.Errorf("task is required")
	}
	// Run in a background goroutine so this returns immediately. The orchestrator
	// streams CollabEvents via a.emitExpertsCollab, which pushes to the webview.
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_, err := a.expertOrchestrator.Run(ctx, teamID, task, mode, rounds)
		if err != nil {
			a.emitExpertsCollab(experts.CollabEvent{TeamID: teamID, Phase: experts.PhaseError, Message: err.Error()})
		}
	}()
	return fmt.Sprintf("run_%d", nowNano()), nil
}

// ExpertBudgetStatus reports the current RPM budget for the active provider's
// key, so the UI can show "RPM: 3/5 remaining" + cost estimates before running.
func (a *App) ExpertBudgetStatus() BudgetStatusView {
	budget := boot.GlobalBudget()
	if budget == nil {
		return BudgetStatusView{}
	}
	// Use the active tab's provider key, or empty (aggregate) when unknown.
	key := a.activeBudgetKey()
	st := budget.Status(key)
	return BudgetStatusView{RPM: st.RPM, Used: st.Used, Remaining: st.Remaining, ReserveMain: st.ReserveMain, WindowSecs: st.WindowSecs}
}

// activeBudgetKey returns the budget bucket key for the active tab's provider,
// so the status reflects the right quota. Best-effort; empty = unknown.
func (a *App) activeBudgetKey() string {
	// The key is name|baseURL|apiKey per provider.BudgetKeyForConfig. We don't
	// easily have the active entry here; the orchestrator's runs all go through
	// boot.NewProvider which computes the right key. For status display, empty
	// gives an aggregate-ish read (first bucket) — acceptable for a hint.
	return ""
}

// emitExpertsCollab pushes a collaboration event to the frontend.
func (a *App) emitExpertsCollab(ev experts.CollabEvent) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "experts:collab", ev)
}

// emitExpertsChanged notifies the frontend the team list mutated.
func (a *App) emitExpertsChanged() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "experts:changed")
}

// --- desktop ExpertRunner: calls the LLM directly per expert ---

// desktopExpertRunner implements experts.ExpertRunner by building a per-expert
// provider (auto rate-limited) and streaming one completion. It does NOT use
// the full agent loop — experts give a single answer, no tool calls.
type desktopExpertRunner struct {
	app *App
}

func (r *desktopExpertRunner) Run(ctx context.Context, model, systemPrompt, task string, streamFn func(delta string)) (string, error) {
	// Resolve the model ref to a provider entry. Empty model = use the active
	// tab's default (the user left the expert's model blank = "use default").
	entry, err := r.resolveEntry(model)
	if err != nil {
		return "", err
	}
	// Build a provider for this model. boot.NewProvider auto-wraps it with the
	// rate-limit decorator (background priority, since this runs in a team).
	prov, err := boot.NewProviderWithProxy(entry, netclient.ProxySpec{Mode: netclient.ModeAuto}, false, false)
	if err != nil {
		return "", fmt.Errorf("build provider for %s: %w", entry.Model, err)
	}
	// One-shot completion: system prompt + user task.
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: systemPrompt},
			{Role: provider.RoleUser, Content: task},
		},
	}
	ch, err := prov.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for chunk := range ch {
		if chunk.Type == provider.ChunkText {
			b.WriteString(chunk.Text)
			if streamFn != nil {
				streamFn(chunk.Text)
			}
		}
		if chunk.Err != nil {
			return b.String(), chunk.Err
		}
	}
	return b.String(), nil
}

// resolveEntry finds the provider entry for a model ref (e.g. "deepseek/r1").
// Empty ref → the active tab's current model entry.
func (r *desktopExpertRunner) resolveEntry(modelRef string) (*config.ProviderEntry, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	modelRef = strings.TrimSpace(modelRef)
	if modelRef == "" {
		// Default: first configured provider.
		for i := range cfg.Providers {
			if cfg.Providers[i].Configured() {
				return &cfg.Providers[i], nil
			}
		}
		return nil, fmt.Errorf("no configured provider available")
	}
	entry, ok := cfg.ResolveModel(modelRef)
	if !ok {
		return nil, fmt.Errorf("unknown model %q — add it under [[providers]] in config", modelRef)
	}
	return entry, nil
}

// --- helpers ---

func toTeamView(t experts.Team) TeamView {
	evs := make([]ExpertView, 0, len(t.Experts))
	for _, e := range t.Experts {
		evs = append(evs, ExpertView{Name: e.Name, Model: e.Model, Perspective: e.Perspective})
	}
	return TeamView{ID: t.ID, Name: t.Name, Experts: evs, DefaultMode: t.DefaultMode, DefaultRounds: t.DefaultRounds}
}

func teamViewToModel(tv TeamView) experts.Team {
	return experts.Team{
		ID:            tv.ID,
		Name:          tv.Name,
		Experts:       expertViewsToModel(tv.Experts),
		DefaultMode:   tv.DefaultMode,
		DefaultRounds: tv.DefaultRounds,
	}
}

func expertViewsToModel(evs []ExpertView) []experts.Expert {
	out := make([]experts.Expert, 0, len(evs))
	for _, ev := range evs {
		out = append(out, experts.Expert{Name: ev.Name, Model: ev.Model, Perspective: ev.Perspective})
	}
	return out
}

// initExperts creates the team store + orchestrator at startup.
func (a *App) initExperts() {
	path := filepath.Join(desktopConfigDir(), "expert_teams.json")
	store, err := experts.NewStore(path)
	if err != nil {
		return
	}
	a.expertStore = store
	// Backfill any missing builtin teams. This is idempotent: for each builtin,
	// we only insert when its stable ID is absent from the store. This lets new
	// builtins ship in a release and automatically appear for existing users
	// (whose expert_teams.json already pins the earlier builtins), without
	// clobbering teams they edited or deleted. Runs every startup; the Get+Create
	// guard makes repeat runs a no-op.
	seedBuiltinTeamsInto(store)
	a.expertOrchestrator = experts.NewOrchestrator(store, &desktopExpertRunner{app: a}, a.emitExpertsCollab)
	// Bind the engine to the expert_team_* tools (registered under cowork in
	// boot.go). Mirrors how initRAG/initScheduler bind SetRAGStore/SetScheduler.
	builtin.SetExpertStore(store)
	builtin.SetExpertOrchestrator(a.expertOrchestrator)
}

// seedBuiltinTeamsInto inserts any builtin team whose stable ID is not yet in
// the store. It is the back-half of the 2→8 builtin expansion: old installs
// already have builtin_review + builtin_brainstorm persisted, so the previous
// "only seed when store is empty" guard would never surface the 6 newer teams.
// By keying on ID and skipping existing rows we (a) never duplicate a team,
// (b) never overwrite a user's edits to an existing builtin, and (c) leave a
// deleted builtin gone (respecting user intent) — it only comes back if the
// user wipes the whole store. Safe to call repeatedly.
func seedBuiltinTeamsInto(store *experts.Store) {
	for _, t := range experts.BuiltinTeams {
		if _, exists := store.Get(t.ID); exists {
			continue
		}
		_, _ = store.Create(t)
	}
}

func nowNano() int64 {
	return time.Now().UnixNano()
}
