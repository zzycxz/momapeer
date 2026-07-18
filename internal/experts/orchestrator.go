package experts

// orchestrator.go runs an expert team against a task in one of three
// collaboration modes, streaming each expert's output to the UI. It is
// model-agnostic: an ExpertRunner (supplied by the desktop layer) does the
// actual LLM call, so this package has no dependency on the agent/provider
// packages and is easy to test with a fake runner.
//
// Modes:
//   - parallel: each expert answers independently → moderator synthesizes.
//     Fastest; best for "give me multiple angles".
//   - debate (default 2 rounds): round 1 each expert answers; round 2+ each
//     expert sees the others' answers and refines/rebuts. Moderator synthesizes
//     at the end, flagging disagreements. Best for review/decision tasks.
//   - pipeline: expert A → B (sees A's answer) → C (sees B's). Best for
//     division-of-labor production (research → draft → proofread).
//
// All runs are serialized within the orchestrator (one expert at a time). Under
// RPM=5 this is forced anyway; under higher RPM it still keeps the streaming UI
// readable (experts speak in turn, not simultaneously).

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ExpertRunResult is the output of one expert's turn.
type ExpertRunResult struct {
	ExpertName string
	Text       string
}

// ExpertRunner runs one expert (model + perspective) on a task, returning the
// full text answer. The desktop layer supplies an implementation that builds a
// provider (with background priority so it respects [llm] reserve_main) and a
// one-shot agent. streamFn receives text deltas for live UI display.
type ExpertRunner interface {
	Run(ctx context.Context, model, systemPrompt, task string, streamFn func(delta string)) (string, error)
}

// CollabPhase labels a collaboration event for the UI.
type CollabPhase string

const (
	PhaseExpertStart CollabPhase = "expert_start"
	PhaseExpertChunk CollabPhase = "expert_chunk"
	PhaseExpertDone  CollabPhase = "expert_done"
	PhaseSynthesis   CollabPhase = "synthesis"
	PhaseRunDone     CollabPhase = "run_done"
	PhaseError       CollabPhase = "error"
)

// CollabEvent is one streamed event during a collaboration run.
type CollabEvent struct {
	RunID      string      `json:"runId"`
	TeamID     string      `json:"teamId"`
	TeamName   string      `json:"teamName"`
	Phase      CollabPhase `json:"phase"`
	ExpertIdx  int         `json:"expertIdx"`
	ExpertName string      `json:"expertName"`
	Round      int         `json:"round"`
	Text       string      `json:"text"`   // expert_chunk: delta; others: full or empty
	Message    string      `json:"message"`
	Mode       string      `json:"mode"`
}

// CollabResult is the final outcome of a run.
type CollabResult struct {
	Synthesis string            `json:"synthesis"`
	Rounds    [][]ExpertAnswer  `json:"rounds"` // per-round answers
}

// ExpertAnswer is one expert's answer in one round (for the result transcript).
type ExpertAnswer struct {
	ExpertName string `json:"expertName"`
	Text       string `json:"text"`
}

// Orchestrator coordinates expert-team runs.
type Orchestrator struct {
	store  *Store
	runner ExpertRunner
	emit   func(CollabEvent)
}

// NewOrchestrator builds an orchestrator. emit may be nil (events dropped).
func NewOrchestrator(store *Store, runner ExpertRunner, emit func(CollabEvent)) *Orchestrator {
	return &Orchestrator{store: store, runner: runner, emit: emit}
}

// Run executes a collaboration. mode overrides the team's default when non-empty;
// rounds overrides DefaultRounds when >0 (ignored for parallel/pipeline).
// Returns the final synthesis + per-round answers.
func (o *Orchestrator) Run(ctx context.Context, teamID, task string, mode string, rounds int) (*CollabResult, error) {
	team, ok := o.store.Get(teamID)
	if !ok {
		return nil, fmt.Errorf("team %q not found", teamID)
	}
	if mode == "" {
		mode = team.DefaultMode
	}
	if mode == "" {
		mode = "debate"
	}
	if rounds <= 0 {
		rounds = team.DefaultRounds
	}
	if rounds <= 0 {
		rounds = 2
	}
	runID := fmt.Sprintf("run_%d", time.Now().UnixNano())
	emit := o.emit
	if emit == nil {
		emit = func(CollabEvent) {}
	}
	emit(CollabEvent{RunID: runID, TeamID: team.ID, TeamName: team.Name, Mode: mode, Phase: PhaseExpertStart, Message: fmt.Sprintf("协作开始 · %s 模式", modeLabel(mode))})

	result := &CollabResult{}
	var err error
	switch mode {
	case "parallel":
		result, err = o.runParallel(ctx, team, task, runID, emit)
	case "debate":
		result, err = o.runDebate(ctx, team, task, rounds, runID, emit)
	case "pipeline":
		result, err = o.runPipeline(ctx, team, task, runID, emit)
	default:
		return nil, fmt.Errorf("unknown mode %q (use parallel/debate/pipeline)", mode)
	}
	if err != nil {
		emit(CollabEvent{RunID: runID, TeamID: team.ID, Phase: PhaseError, Message: err.Error()})
		return nil, err
	}
	emit(CollabEvent{RunID: runID, TeamID: team.ID, Phase: PhaseRunDone, Message: "协作完成"})
	return result, nil
}

// runParallel: each expert answers independently, then a moderator synthesizes.
func (o *Orchestrator) runParallel(ctx context.Context, team Team, task string, runID string, emit func(CollabEvent)) (*CollabResult, error) {
	var answers []ExpertAnswer
	for i, ex := range team.Experts {
		ans, err := o.runExpert(ctx, ex, i, 1, task, runID, team, emit)
		if err != nil {
			return nil, err
		}
		answers = append(answers, ans)
	}
	synthesis, err := o.synthesize(ctx, team, task, [][]ExpertAnswer{answers}, runID, emit)
	if err != nil {
		return nil, err
	}
	return &CollabResult{Synthesis: synthesis, Rounds: [][]ExpertAnswer{answers}}, nil
}

// runDebate: N rounds. Round 1 = independent answers. Round 2+ = each expert
// sees all prior answers and refines/rebuts. Then synthesize.
func (o *Orchestrator) runDebate(ctx context.Context, team Team, task string, rounds int, runID string, emit func(CollabEvent)) (*CollabResult, error) {
	allRounds := make([][]ExpertAnswer, 0, rounds)
	for r := 1; r <= rounds; r++ {
		var roundAnswers []ExpertAnswer
		for i, ex := range team.Experts {
			// Build this expert's input: original task + (round>1) all prior answers.
			input := task
			if r > 1 && len(allRounds) > 0 {
				input = task + "\n\n--- 以下是前几轮其他专家的发言 ---\n" + formatPriorRounds(allRounds, ex.Name) + "\n--- 请在此基础上补充、反驳或深化你的观点 ---"
			}
			ans, err := o.runExpert(ctx, ex, i, r, input, runID, team, emit)
			if err != nil {
				return nil, err
			}
			roundAnswers = append(roundAnswers, ans)
		}
		allRounds = append(allRounds, roundAnswers)
	}
	synthesis, err := o.synthesize(ctx, team, task, allRounds, runID, emit)
	if err != nil {
		return nil, err
	}
	return &CollabResult{Synthesis: synthesis, Rounds: allRounds}, nil
}

// runPipeline: experts in sequence, each seeing the prior expert's output.
func (o *Orchestrator) runPipeline(ctx context.Context, team Team, task string, runID string, emit func(CollabEvent)) (*CollabResult, error) {
	var chain []ExpertAnswer
	input := task
	for i, ex := range team.Experts {
		if i > 0 && len(chain) > 0 {
			input = task + "\n\n--- 上一位专家（" + chain[len(chain)-1].ExpertName + "）的产出 ---\n" + chain[len(chain)-1].Text + "\n--- 请在此基础上继续你的部分 ---"
		}
		ans, err := o.runExpert(ctx, ex, i, 1, input, runID, team, emit)
		if err != nil {
			return nil, err
		}
		chain = append(chain, ans)
	}
	synthesis, err := o.synthesize(ctx, team, task, [][]ExpertAnswer{chain}, runID, emit)
	if err != nil {
		return nil, err
	}
	return &CollabResult{Synthesis: synthesis, Rounds: [][]ExpertAnswer{chain}}, nil
}

// runExpert calls the runner for one expert, streaming chunks to the UI.
func (o *Orchestrator) runExpert(ctx context.Context, ex Expert, idx, round int, input, runID string, team Team, emit func(CollabEvent)) (ExpertAnswer, error) {
	systemPrompt := buildExpertPrompt(ex)
	emit(CollabEvent{RunID: runID, TeamID: team.ID, TeamName: team.Name, Phase: PhaseExpertStart, ExpertIdx: idx, ExpertName: ex.Name, Round: round, Mode: team.DefaultMode, Message: fmt.Sprintf("%s 正在思考…", ex.Name)})
	text, err := o.runner.Run(ctx, ex.Model, systemPrompt, input, func(delta string) {
		emit(CollabEvent{RunID: runID, TeamID: team.ID, Phase: PhaseExpertChunk, ExpertIdx: idx, ExpertName: ex.Name, Round: round, Text: delta})
	})
	if err != nil {
		return ExpertAnswer{}, fmt.Errorf("expert %q: %w", ex.Name, err)
	}
	emit(CollabEvent{RunID: runID, TeamID: team.ID, Phase: PhaseExpertDone, ExpertIdx: idx, ExpertName: ex.Name, Round: round})
	return ExpertAnswer{ExpertName: ex.Name, Text: text}, nil
}

// synthesize asks the moderator (using the first expert's model, or a default)
// to produce a final conclusion that flags disagreements + gives a ruling.
func (o *Orchestrator) synthesize(ctx context.Context, team Team, task string, rounds [][]ExpertAnswer, runID string, emit func(CollabEvent)) (string, error) {
	emit(CollabEvent{RunID: runID, TeamID: team.ID, Phase: PhaseSynthesis, Message: "主持人正在综合各专家发言…"})
	transcript := formatAllRounds(rounds)
	// Use the first expert's model for the moderator; fall back to "" (runner picks default).
	modModel := ""
	if len(team.Experts) > 0 {
		modModel = team.Experts[0].Model
	}
	modPrompt := `你是专家团的主持人。下面是各位专家就同一任务的发言记录。请：
1. 综合出明确结论
2. 标明分歧点（哪位专家不同意哪位，为什么）
3. 给出你的裁决理由
保持客观、简洁。`
	input := fmt.Sprintf("任务：%s\n\n%s", task, transcript)
	text, err := o.runner.Run(ctx, modModel, modPrompt, input, func(delta string) {
		emit(CollabEvent{RunID: runID, TeamID: team.ID, Phase: PhaseSynthesis, Text: delta})
	})
	if err != nil {
		return "", fmt.Errorf("moderator synthesis: %w", err)
	}
	return text, nil
}

// buildExpertPrompt crafts the system prompt for an expert from its perspective.
func buildExpertPrompt(ex Expert) string {
	perspective := strings.TrimSpace(ex.Perspective)
	if perspective == "" {
		perspective = "请从你的专业角度给出深入、具体的分析。"
	}
	return fmt.Sprintf(`你是专家团的一员，名为「%s」。你的职责：%s

要求：
- 给出深入、具体的分析，不要泛泛而谈
- 如果看到其他专家的发言，明确指出你同意/不同意什么及原因
- 聚焦你的视角，不必面面俱到`, ex.Name, perspective)
}

// formatPriorRounds renders all prior rounds' answers EXCEPT the current expert's
// own prior answers (so they focus on others' views, not their own echo).
func formatPriorRounds(rounds [][]ExpertAnswer, currentExpert string) string {
	var b strings.Builder
	for r, round := range rounds {
		for _, a := range round {
			if a.ExpertName == currentExpert {
				continue // skip own prior answer
			}
			fmt.Fprintf(&b, "【%s】（第%d轮）：%s\n\n", a.ExpertName, r+1, truncate(a.Text, 800))
		}
	}
	return strings.TrimSpace(b.String())
}

// formatAllRounds renders the full transcript for the moderator.
func formatAllRounds(rounds [][]ExpertAnswer) string {
	var b strings.Builder
	for r, round := range rounds {
		fmt.Fprintf(&b, "=== 第 %d 轮 ===\n", r+1)
		for _, a := range round {
			fmt.Fprintf(&b, "【%s】：%s\n\n", a.ExpertName, a.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func modeLabel(mode string) string {
	switch mode {
	case "parallel":
		return "并行（各自独立）"
	case "debate":
		return "讨论（互相补充）"
	case "pipeline":
		return "流水线（分工协作）"
	}
	return mode
}

// PriorRun carries the compact context from a previous collaboration run.
type PriorRun struct {
	Task      string `json:"task"`
	Synthesis string `json:"synthesis"`
}
