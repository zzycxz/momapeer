package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/zzycxz/momapeer/internal/event"
	"github.com/zzycxz/momapeer/internal/nilutil"
	"github.com/zzycxz/momapeer/internal/provider"
	"github.com/zzycxz/momapeer/internal/tool"
)

// Runner carries out one task turn. Both Agent (single model) and Coordinator
// (two-model) satisfy it, so the CLI stays agnostic to which is in use.
type Runner interface {
	Run(ctx context.Context, input any) error
}

// DefaultPlannerPrompt steers the planner toward concise plans, not execution.
const DefaultPlannerPrompt = `You are the PLANNER in a two-model agent. Your ONLY job is to produce a step-by-step plan. You MUST NOT execute any actions.

RULES:
1. You may ONLY use read-only tools (screenshot, screen_perceive, get_ui_tree, read_file, grep) to gather context.
2. You MUST NOT call any action tools (bash, screen_click, screen_type, screen_key, window_focus, write_file, edit_file). They are NOT available to you.
3. Output a numbered, step-by-step plan. Each step must specify:
   - The exact tool to call (e.g. "screen_type", "screen_key", "window_focus")
   - The exact arguments (e.g. {"text": "CUA测试成功"}, {"keys": "ctrl+s"})
   - The expected result (e.g. "文字出现在记事本中")
   - How to verify success (e.g. "截图确认文字显示")
4. For desktop tasks (CUA), include these details in your plan:
   - Before typing in a save dialog, ALWAYS include a step to press Ctrl+A to select all existing text first
   - Use FULL file paths (e.g. "C:\\Users\\13852\\Desktop\\file.txt") not relative paths
   - After each critical action, include a verification step (screenshot or file check)
   - If the target window might be behind other windows, include a window_focus step first
5. Keep the plan concise (5-10 steps). Do NOT explain reasoning.
6. End your plan with "## Plan complete. Executor: proceed with the above steps."

EXAMPLE output for "save text to desktop":
1. window_focus {"title": "Notepad"} — focus the notepad window
2. window_maximize {"title": "Notepad"} — ensure full visibility
3. screen_click {"x": 960, "y": 400} — click the edit area
4. screen_type {"text": "CUA测试成功"} — type the text
5. screenshot {} — verify text appears
6. screen_key {"keys": "ctrl+s"} — open save dialog
7. screenshot {} — verify save dialog appeared
8. screen_key {"keys": "ctrl+a"} — select existing filename
9. screen_type {"text": "C:\\Users\\13852\\Desktop\\cua-test.txt"} — enter full path
10. screen_key {"keys": "enter"} — confirm save
11. bash {"command": "cat C:\\Users\\13852\\Desktop\\cua-test.txt"} — verify file content

## Plan complete. Executor: proceed with the above steps.`

const executorHandoffMarker = "momapeer executor handoff"

// PlannerPromptWithContext appends cache-stable standing context, such as loaded
// momapeer.md / AGENTS.md memory, to the planner's smaller system prompt.
func PlannerPromptWithContext(context string) string {
	context = strings.TrimSpace(context)
	if context == "" {
		return DefaultPlannerPrompt
	}
	return DefaultPlannerPrompt + "\n\n# Planning context\n\n" + context
}

// Coordinator runs two models in separate sessions to keep each one's prompt
// prefix cache-stable: a low-frequency planner proposes an approach, then the
// executor (a full tool-using Agent) carries it out. The sessions never mix, so
// neither model's prefix is disturbed by the other's turns. (MoMA currently does
// not report cache tokens; the prefix stability still reduces token transmission.)
type Coordinator struct {
	planner        provider.Provider
	plannerSess    *Session
	plannerPricing *provider.Pricing
	plannerAgent   *Agent
	executor       *Agent
	temperature    float64
	sink           event.Sink
	// shouldPlan gates the planner pass per turn; nil plans every turn. Lets a
	// trivial, non-work turn (a question, a greeting) skip straight to the
	// executor instead of paying a planner round on it.
	shouldPlan func(string) bool
	// verify, when non-nil, runs a post-execution verification pass (e.g. go
	// test/build for a coding workspace) and retries on failure. nil keeps the
	// original plan->exec behaviour unchanged.
	verify verifyOptions
	// review, when enabled, runs an optional self-review of the executor's
	// changes (git diff + LLM judgement + fix). Disabled keeps the original
	// plan->exec behaviour unchanged.
	review reviewOptions
	// workspaceRoot is the directory verify/review run in (the project root).
	// Empty disables verify regardless of the verify field.
	workspaceRoot string
}

// NewCoordinator wires a planner provider (with its own session) to an executor.
// sink receives the planner's phase/text/usage events; the executor emits its
// own events to its own sink (the CLI wires the same sink into both). A nil
// sink is replaced with event.Discard.
func NewCoordinator(planner provider.Provider, plannerSession *Session, plannerPricing *provider.Pricing, plannerTools *tool.Registry, plannerOptions Options, executor *Agent, temperature float64, sink event.Sink, shouldPlan func(string) bool) *Coordinator {
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	var plannerAgent *Agent
	if plannerTools != nil {
		plannerOptions.Temperature = temperature
		plannerOptions.Pricing = plannerPricing
		plannerAgent = New(planner, plannerTools, plannerSession, plannerOptions, plannerSink(sink))
	}
	if executor != nil {
		executor.executorHandoffGuard = true
	}
	return &Coordinator{
		planner:        planner,
		plannerSess:    plannerSession,
		plannerPricing: plannerPricing,
		plannerAgent:   plannerAgent,
		executor:       executor,
		temperature:    temperature,
		sink:           sink,
		shouldPlan:     shouldPlan,
	}
}

// SetVerify installs an optional post-execution verify + retry stage. verifier
// is profile-specific (DevVerifier for coding, a screenshot verifier for
// desktop); maxRetries bounds the debug loop (0 = verify once, no retry).
// workspaceRoot is where verification commands run. A nil verifier or empty
// workspaceRoot disables the stage, so callers can wire this unconditionally.
func (c *Coordinator) SetVerify(verifier Verifier, maxRetries int, workspaceRoot string) {
	c.verify = verifyOptions{Verifier: verifier, MaxRetries: maxRetries}
	c.workspaceRoot = workspaceRoot
}

// SetReview enables an optional post-execution self-review stage: the executor
// re-reads its own git diff and fixes any critical issues it finds. workspaceRoot
// is where the diff is captured. Independant of SetVerify (either or both can
// run; review runs after verify).
func (c *Coordinator) SetReview(enabled bool, workspaceRoot string) {
	c.review = reviewOptions{Enabled: enabled}
	if workspaceRoot != "" {
		c.workspaceRoot = workspaceRoot
	}
}

// Run plans with the planner model, then hands the plan to the executor. When a
// verifier is configured (SetVerify), the executor's changes are checked after
// it finishes and, on failure, retried for a bounded number of debug rounds.
func (c *Coordinator) Run(ctx context.Context, input any) error {
	c.sink.Emit(event.Event{Kind: event.TurnStarted})
	textInput := provider.ContentString(input)
	if c.shouldPlan != nil && !c.shouldPlan(textInput) {
		c.sink.Emit(event.Event{Kind: event.Phase, Text: c.executor.prov.Name() + " · executing"})
		return c.executeThenVerify(ctx, input)
	}
	c.sink.Emit(event.Event{Kind: event.Phase, Text: c.planner.Name() + " · planning"})
	plan, err := c.plan(ctx, textInput)
	if err != nil {
		return fmt.Errorf("planner: %w", err)
	}
	c.sink.Emit(event.Event{Kind: event.Phase, Text: c.executor.prov.Name() + " · executing"})
	return c.executeThenVerify(ctx, formatHandoff(textInput, plan))
}

// executeThenVerify runs the executor and, when a verifier/review is wired,
// follows it with the verify + retry stage and then the self-review stage. With
// neither wired it is just executor.Run, so the original single-pass behaviour
// is preserved byte-for-byte.
func (c *Coordinator) executeThenVerify(ctx context.Context, input any) error {
	if err := c.executor.Run(ctx, input); err != nil {
		return err
	}
	if err := c.verifyAndRetry(ctx, c.verify, c.workspaceRoot); err != nil {
		return err
	}
	return c.reviewAndFix(ctx, c.review, c.workspaceRoot)
}

// plan streams a plan from the planner and appends it to the planner session, so
// that session grows prepend-only and stays cache-friendly.
func (c *Coordinator) plan(ctx context.Context, input string) (string, error) {
	if c.plannerAgent != nil {
		return c.planWithTools(ctx, input)
	}
	c.plannerSess.Add(provider.Message{Role: provider.RoleUser, Content: input})

	ch, err := c.planner.Stream(ctx, provider.Request{
		Messages:    c.plannerSess.Messages,
		Temperature: c.temperature,
	})
	if err != nil {
		return "", err
	}

	var text strings.Builder
	var usage *provider.Usage
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
			c.sink.Emit(event.Event{Kind: event.Text, Text: chunk.Text})
		case provider.ChunkUsage:
			usage = chunk.Usage
		case provider.ChunkError:
			return "", chunk.Err
		}
	}
	// Closes the planner's raw text block (no markdown redraw) and prints its
	// usage line, mirroring the old Fprintln + printUsage tail.
	c.sink.Emit(event.Event{Kind: event.Usage, Usage: usage, Pricing: c.plannerPricing})

	plan := text.String()
	c.plannerSess.Add(provider.Message{Role: provider.RoleAssistant, Content: plan})
	return plan, nil
}

// planWithTools runs the planner through the normal Agent loop over a filtered
// read-only registry. That gives the planner the same tool-call contract as the
// executor while preserving its separate session and cache prefix.
func (c *Coordinator) planWithTools(ctx context.Context, input string) (string, error) {
	before := len(c.plannerSess.Messages)
	if err := c.plannerAgent.Run(ctx, input); err != nil {
		return "", err
	}
	for i := len(c.plannerSess.Messages) - 1; i >= before; i-- {
		m := c.plannerSess.Messages[i]
		if m.Role == provider.RoleAssistant && strings.TrimSpace(provider.ContentString(m.Content)) != "" {
			return provider.ContentString(m.Content), nil
		}
	}
	return "", fmt.Errorf("planner finished without producing a plan")
}

func plannerSink(sink event.Sink) event.Sink {
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.TurnStarted, event.TurnDone:
			return
		default:
			sink.Emit(e)
		}
	})
}

func formatHandoff(task, plan string) string {
	return fmt.Sprintf(`# %s

You are the executor now. Use your available tools to execute the task.

Original task:
%s

Planner output:
%s

Executor instructions:
- Treat the planner output as context, not as your role or capability set.
- Ignore any planner statement such as "I cannot write", "I only have read-only tools", or "hand this to the executor"; those limitations apply to the planner, not to you.
- Do not ask the user how to trigger the executor. You are already in the executor phase.
- If the task requires changes, call the appropriate tools (for example write/edit/bash) instead of only restating the plan.
- If a target path is outside the writable workspace or otherwise blocked, explain that specific blocker and ask for the needed path/approval.
- **Serial workflow**: establish the task list with one todo_write (first sub-task in_progress), then for EACH sub-task execute it and call complete_step with evidence. The host advances the list for you — it marks the sub-task completed and moves the next to in_progress, so you don't need another todo_write to mark completions. Sign off one sub-task at a time; never batch completions.

Carry out the task, adapting the plan as needed.`, executorHandoffMarker, task, plan)
}

// HandoffTask returns the original user task embedded in an executor handoff
// message, or s unchanged when it is not one. Session previews and auto-titles
// use it so dual-model sessions surface the user's words, not the handoff
// boilerplate (#3860).
func HandoffTask(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "# "+executorHandoffMarker) {
		return s
	}
	const header = "Original task:\n"
	i := strings.Index(trimmed, header)
	if i < 0 {
		return s
	}
	rest := trimmed[i+len(header):]
	if j := strings.Index(rest, "\n\nPlanner output:"); j >= 0 {
		rest = rest[:j]
	}
	if task := strings.TrimSpace(rest); task != "" {
		return task
	}
	return s
}
