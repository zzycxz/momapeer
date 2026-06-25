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
	mu     sync.Mutex
	calls  []fakeCall
}

type fakeCall struct {
	model, prompt, task string
}

func (f *fakeRunner) Run(ctx context.Context, model, systemPrompt, task string, streamFn func(delta string)) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{model, systemPrompt, task})
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
