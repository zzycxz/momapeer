package builtin

// browserauto.go defines the browser_auto tool: an autonomous-browsing entry
// point. Unlike the low-level browser_* tools (which the agent drives
// step-by-step via chromedp), browser_auto takes a high-level GOAL and hands
// it to a Python browser-use sidecar that runs its own perception→LLM→action
// loop. This is the "give it a goal, it figures out the clicks" capability.
//
// Wiring: the sidecar client, browser launcher, and (optional) in-app panel
// are owned by the desktop layer and injected here via SetBrowserAutoRuntime.
// We can't import desktop (import cycle), so the dependency is a small set of
// function values with this package's own types. The zero values return a
// clear "not initialized" error so a misconfigured boot surfaces fast.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// BrowserAutoStep is one event from the sidecar's agentic loop. It's defined
// here (not imported from internal/browseruse) so builtin has no dependency on
// that package and the wiring closure does the conversion.
type BrowserAutoStep struct {
	Type string // thought | action | screenshot | done | error
	Step int
	Text string
	Done bool
}

// BrowserAutoRunRequest is the goal-oriented request the runtime executes.
// The LLM model/base_url/proxy are resolved from config inside the runtime
// (runBrowserAuto), NOT carried here — this struct only holds the per-call goal.
type BrowserAutoRunRequest struct {
	Goal     string
	URL      string
	MaxSteps int
}

// BrowserAutoRuntime is the injected dependency. Each field is a function the
// boot layer supplies; nil fields cause browser_auto to report that the
// feature is unavailable rather than crash.
type BrowserAutoRuntime struct {
	// Available reports whether autonomous browsing is ready to use (sidecar up
	// + a browser can be found). Returns (ok, reason) where reason explains why
	// not (for a helpful tool error).
	Available func() (ok bool, reason string)
	// Run launches the shared browser, mirrors it to the in-app panel, drives
	// the sidecar loop, and returns the streamed steps. The context governs
	// cancellation (user Stop / turn cancel). It must close the browser when
	// done (or on error).
	Run func(ctx context.Context, req BrowserAutoRunRequest) (steps []BrowserAutoStep, finalSummary string, err error)
}

var browserAutoRT = &BrowserAutoRuntime{}

// SetBrowserAutoRuntime injects the boot-owned runtime. Called at each
// boot.Build, mirroring SetProviderChatRunner / SetBrowserLaunchOptions.
func SetBrowserAutoRuntime(rt BrowserAutoRuntime) {
	browserAutoRT = &rt
}

type browserAuto struct{}

func (browserAuto) Name() string { return "browser_auto" }

func (browserAuto) Description() string {
	return "Autonomous web browsing: give a natural-language GOAL and (optional) starting URL, " +
		"and an agent autonomously perceives the page and performs the clicks/types/navigation " +
		"needed to complete it end-to-end — no need to call browser_click/browser_type yourself. " +
		"Use for multi-step web tasks (research, form filling, sign-in flows, scraping). " +
		"For a single precise action on a known element, prefer the explicit browser_* tools."
}

func (browserAuto) Schema() json.RawMessage {
	return json.RawMessage(`{
"type": "object",
"properties": {
  "goal": {"type": "string", "description": "The task to complete in natural language, e.g. \"search for X on the news site and return the top 3 headlines\"."},
  "url": {"type": "string", "description": "Optional starting URL to navigate to before the agent begins. If omitted, the agent navigates as part of the task."},
  "max_steps": {"type": "integer", "description": "Optional cap on the number of agent steps. Omit to use the configured default."}
},
"required": ["goal"]
}`)
}

func (browserAuto) ReadOnly() bool { return false }

func (browserAuto) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Goal     string `json:"goal"`
		URL      string `json:"url"`
		MaxSteps int    `json:"max_steps"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
	}
	if strings.TrimSpace(p.Goal) == "" {
		return "", errors.New("browser_auto: 'goal' is required")
	}

	rt := browserAutoRT
	if rt.Available == nil || rt.Run == nil {
		return "", errors.New("browser_auto is not available (the autonomous-browsing sidecar is not configured)")
	}
	if ok, reason := rt.Available(); !ok {
		return "", fmt.Errorf("browser_auto unavailable: %s", reason)
	}

	steps, summary, err := rt.Run(ctx, BrowserAutoRunRequest{
		Goal:     p.Goal,
		URL:      p.URL,
		MaxSteps: p.MaxSteps,
	})
	if err != nil {
		// Still surface the steps taken before the failure — they often explain
		// where things went wrong (e.g. a login wall on step 4).
		return renderBrowserAutoSteps(steps) + "\nERROR: " + err.Error(), err
	}
	out := renderBrowserAutoSteps(steps)
	if summary != "" {
		out += "\n\nSUMMARY: " + summary
	}
	return out, nil
}

// renderBrowserAutoSteps turns the streamed events into a concise transcript
// for the calling agent. Thoughts are condensed (the agent doesn't need every
// reasoning token); actions are kept verbatim since they're the observable
// behavior. Screenshots are noted but not inlined (they'd bloat the context).
func renderBrowserAutoSteps(steps []BrowserAutoStep) string {
	if len(steps) == 0 {
		return "(agent produced no observable steps)"
	}
	var b strings.Builder
	for _, s := range steps {
		switch s.Type {
		case "action":
			fmt.Fprintf(&b, "[step %d] action: %s\n", s.Step, strings.TrimSpace(s.Text))
		case "error":
			fmt.Fprintf(&b, "[step %d] error: %s\n", s.Step, strings.TrimSpace(s.Text))
		case "done":
			if strings.TrimSpace(s.Text) != "" {
				fmt.Fprintf(&b, "[done] %s\n", strings.TrimSpace(s.Text))
			}
		case "screenshot":
			// Intentionally omitted from text; the panel mirrors it live.
		case "thought":
			// Condensed: keep the last thought of each step only if short.
			t := strings.TrimSpace(s.Text)
			if t != "" && len(t) < 240 {
				fmt.Fprintf(&b, "[step %d] thought: %s\n", s.Step, t)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
