package experts

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// fakeRunner is a deterministic ExpertRunner for orchestrator tests. It
// returns "{expert}: {perspective} on {task}" so tests can verify each expert
// ran with the right inputs. It also records stream deltas.
type fakeRunner struct {
	mu    sync.Mutex
	calls []fakeCall
}

type fakeCall struct {
	model, prompt, task string
	allowSearch         bool
}

func (f *fakeRunner) Run(ctx context.Context, model, systemPrompt, task string, allowSearch bool, streamFn func(delta string)) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{model, systemPrompt, task, allowSearch})
	f.mu.Unlock()
	// Derive a short answer from the prompt (the expert name is embedded).
	name := "expert"
	if i := strings.Index(systemPrompt, "「"); i >= 0 {
		end := strings.Index(systemPrompt[i+3:], "」")
		if end >= 0 {
			name = systemPrompt[i+3 : i+3+end]
		}
	}
	out := name + " answered: " + truncate(task, 30)
	if streamFn != nil {
		streamFn(out) // single delta = whole answer
	}
	return out, nil
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir() + "/teams.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func sampleTeam() Team {
	return Team{
		ID:   "t1",
		Name: "测试团",
		Experts: []Expert{
			{Name: "甲", Model: "m1", Perspective: "批判"},
			{Name: "乙", Model: "m2", Perspective: "建设"},
		},
		DefaultMode:   "debate",
		DefaultRounds: 2,
	}
}

// TestOrchestratorParallel confirms each expert answers once + synthesis runs.
func TestOrchestratorParallel(t *testing.T) {
	store := newTestStore(t)
	team := sampleTeam()
	store.Create(team)
	runner := &fakeRunner{}
	var events []CollabEvent
	o := NewOrchestrator(store, runner, func(e CollabEvent) { events = append(events, e) })

	res, err := o.Run(context.Background(), "t1", "评估X", "parallel", 0)
	if err != nil {
		t.Fatal(err)
	}
	// 2 experts + 1 synthesis = 3 runner calls.
	if got := len(runner.calls); got != 3 {
		t.Errorf("parallel: %d runner calls, want 3", got)
	}
	if len(res.Rounds) != 1 || len(res.Rounds[0]) != 2 {
		t.Errorf("parallel rounds = %+v, want 1 round of 2 answers", res.Rounds)
	}
	if res.Synthesis == "" {
		t.Error("parallel synthesis should be non-empty")
	}
}

// TestOrchestratorDebate confirms 2 rounds × 2 experts + synthesis.
func TestOrchestratorDebate(t *testing.T) {
	store := newTestStore(t)
	store.Create(sampleTeam())
	runner := &fakeRunner{}
	o := NewOrchestrator(store, runner, nil)

	res, err := o.Run(context.Background(), "t1", "评估Y", "debate", 2)
	if err != nil {
		t.Fatal(err)
	}
	// 2 experts × 2 rounds + 1 synthesis = 5 calls.
	if got := len(runner.calls); got != 5 {
		t.Errorf("debate: %d runner calls, want 5", got)
	}
	if len(res.Rounds) != 2 {
		t.Errorf("debate: %d rounds, want 2", len(res.Rounds))
	}
	// Round 2 experts should have seen round-1 answers in their input.
	// runner.calls: [甲r1, 乙r1, 甲r2, 乙r2, synth]. 甲r2 (index 2) should
	// mention 乙's round-1 answer ("乙 answered").
	if !strings.Contains(runner.calls[2].task, "乙 answered") {
		t.Errorf("debate round 2 should include others' answers; task=%q", runner.calls[2].task)
	}
}

// TestOrchestratorDebateClampsRounds verifies an out-of-range rounds request
// (e.g. the chat tool passing rounds=1000) is clamped to maxDebateRounds, so a
// runaway request can't trigger a multi-thousand-round cost blowup.
func TestOrchestratorDebateClampsRounds(t *testing.T) {
	store := newTestStore(t)
	store.Create(sampleTeam())
	runner := &fakeRunner{}
	o := NewOrchestrator(store, runner, nil)

	res, err := o.Run(context.Background(), "t1", "评估", "debate", 1000)
	if err != nil {
		t.Fatal(err)
	}
	// 2 experts × maxDebateRounds rounds + 1 synthesis.
	wantCalls := 2*maxDebateRounds + 1
	if got := len(runner.calls); got != wantCalls {
		t.Errorf("rounds=1000 → %d runner calls, want %d (clamped to %d rounds)", got, wantCalls, maxDebateRounds)
	}
	if len(res.Rounds) != maxDebateRounds {
		t.Errorf("rounds=1000 → %d rounds in result, want %d", len(res.Rounds), maxDebateRounds)
	}
}

// TestOrchestratorPipeline confirms experts chain (B sees A's output).
func TestOrchestratorPipeline(t *testing.T) {
	store := newTestStore(t)
	store.Create(sampleTeam())
	runner := &fakeRunner{}
	o := NewOrchestrator(store, runner, nil)

	_, err := o.Run(context.Background(), "t1", "流水线Z", "pipeline", 0)
	if err != nil {
		t.Fatal(err)
	}
	// 2 experts + 1 synthesis = 3 calls.
	if got := len(runner.calls); got != 3 {
		t.Errorf("pipeline: %d calls, want 3", got)
	}
	// 2nd expert (index 1) should see 1st expert's output.
	if !strings.Contains(runner.calls[1].task, "甲 answered") {
		t.Errorf("pipeline: 2nd expert should see 1st output; task=%q", runner.calls[1].task)
	}
}

// TestOrchestratorStreaming confirms expert_chunk events fire with deltas.
func TestOrchestratorStreaming(t *testing.T) {
	store := newTestStore(t)
	store.Create(sampleTeam())
	runner := &fakeRunner{}
	var chunks []string
	o := NewOrchestrator(store, runner, func(e CollabEvent) {
		if e.Phase == PhaseExpertChunk {
			chunks = append(chunks, e.Text)
		}
	})
	_, err := o.Run(context.Background(), "t1", "流式测试", "parallel", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Errorf("expected ≥2 expert_chunk events (one per expert), got %d", len(chunks))
	}
}

// TestOrchestratorUnknownMode errors clearly.
func TestOrchestratorUnknownMode(t *testing.T) {
	store := newTestStore(t)
	store.Create(sampleTeam())
	o := NewOrchestrator(store, &fakeRunner{}, nil)
	if _, err := o.Run(context.Background(), "t1", "x", "bogus", 0); err == nil {
		t.Error("unknown mode should error")
	}
}

// TestOrchestratorMissingTeam errors when the team doesn't exist.
func TestOrchestratorMissingTeam(t *testing.T) {
	store := newTestStore(t)
	o := NewOrchestrator(store, &fakeRunner{}, nil)
	if _, err := o.Run(context.Background(), "nope", "x", "debate", 1); err == nil {
		t.Error("missing team should error")
	}
}

// TestTeamStoreCRUD confirms team persistence roundtrips.
func TestTeamStoreCRUD(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/teams.json"
	s1, _ := NewStore(path)
	created, _ := s1.Create(sampleTeam())
	if created.ID == "" {
		t.Fatal("Create should assign ID")
	}
	// Reopen — team should persist.
	s2, _ := NewStore(path)
	all := s2.List()
	if len(all) != 1 || all[0].Name != "测试团" {
		t.Fatalf("persistence lost team: %+v", all)
	}
	// Update.
	s2.Update(created.ID, func(t *Team) { t.Name = "改名团" })
	got, _ := s2.Get(created.ID)
	if got.Name != "改名团" {
		t.Errorf("update failed: name=%q", got.Name)
	}
	// Delete.
	if !s2.Delete(created.ID) {
		t.Error("Delete returned false")
	}
	if len(s2.List()) != 0 {
		t.Error("team not deleted")
	}
}

// TestBuildExpertPromptSearchGuidance checks the allowSearch flag bakes a
// web_search hint into the system prompt. A search-capable runner reads this to
// know when to query; a one-shot runner ignores it, but the hint must be present
// or absent exactly per the flag.
func TestBuildExpertPromptSearchGuidance(t *testing.T) {
	ex := Expert{Name: "分析师", Perspective: "查实时数据"}
	withSearch := buildExpertPrompt(ex, true, "debate")
	withoutSearch := buildExpertPrompt(ex, false, "debate")
	if !strings.Contains(withSearch, "web_search") {
		t.Errorf("allowSearch=true prompt should mention web_search, got:\n%s", withSearch)
	}
	if strings.Contains(withoutSearch, "web_search") {
		t.Errorf("allowSearch=false prompt should NOT mention web_search, got:\n%s", withoutSearch)
	}
}

// TestOrchestratorPassesAllowSearch confirms the orchestrator forwards
// team.AllowSearch to the runner (so the desktop mini-agent path activates) and
// into the system prompt (so the search hint appears).
func TestOrchestratorPassesAllowSearch(t *testing.T) {
	store := newTestStore(t)
	team := sampleTeam()
	team.AllowSearch = true
	created, err := store.Create(team)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	emit := func(CollabEvent) {}
	o := NewOrchestrator(store, runner, emit)

	if _, err := o.Run(context.Background(), created.ID, "高考600分选专业", "parallel", 1); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) == 0 {
		t.Fatal("no expert calls recorded")
	}
	// The last call is the moderator (synthesize) which always runs with
	// allowSearch=false — it works off the experts' transcript, not live data.
	// All calls before it are experts and should carry the team's flag.
	expertCalls := runner.calls[:len(runner.calls)-1]
	for i, c := range expertCalls {
		if !c.allowSearch {
			t.Errorf("expert call[%d] allowSearch=false, want true (team.AllowSearch=true)", i)
		}
		if !strings.Contains(c.prompt, "web_search") {
			t.Errorf("expert call[%d] system prompt lacks web_search hint", i)
		}
	}
	// Moderator (last call) must be false regardless of the team flag.
	modCall := runner.calls[len(runner.calls)-1]
	if modCall.allowSearch {
		t.Error("moderator call should always have allowSearch=false")
	}

	// Flip the team off and re-run: now calls should carry false + no hint.
	store.Update(created.ID, func(t *Team) { t.AllowSearch = false })
	runner.calls = nil
	if _, err := o.Run(context.Background(), created.ID, "高考600分选专业", "parallel", 1); err != nil {
		t.Fatal(err)
	}
	for i, c := range runner.calls {
		if c.allowSearch {
			t.Errorf("after flip, call[%d] allowSearch=true, want false", i)
		}
		if strings.Contains(c.prompt, "web_search") {
			t.Errorf("after flip, call[%d] prompt still has web_search hint", i)
		}
	}
}
