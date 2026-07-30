package boot

// browserauto_runtime.go builds the browser_auto runtime injected into the
// builtin tool. It wires together:
//
//   - internal/browserlaunch (launch a system Chrome/Edge with a CDP endpoint)
//   - a desktop-registered screencast sink (so the in-app panel mirrors the
//     agent's browser), optional
//   - the browser-use sidecar client (desktop-owned, resolved lazily so the
//     runtime can be built before the sidecar process is up)
//
// The runtime is rebuilt on each boot.Build (per profile switch) because it
// captures the current cfg (model/proxy/limits). The sidecar client and panel
// sink are process-global, set by the desktop, so they survive rebuilds.

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/zzycxz/momapeer/internal/browserlaunch"
	"github.com/zzycxz/momapeer/internal/browseruse"
	"github.com/zzycxz/momapeer/internal/config"
	"github.com/zzycxz/momapeer/internal/tool/builtin"
)

// --- desktop-registered process-global hooks --------------------------------

// browserUseClientProvider returns the live sidecar client (or nil if the
// sidecar isn't running). Set by the desktop once the service is up, so the
// runtime (built at boot.Build time, before the sidecar exists) can resolve
// the client lazily at tool-execution time.
var browserUseClientProvider func() *browseruse.Client

// SetBrowserUseClientProvider injects the sidecar-client resolver. Called by
// the desktop app once its BrowserUseService is created.
func SetBrowserUseClientProvider(fn func() *browseruse.Client) {
	browserUseClientProvider = fn
}

// buildBrowserAutoRuntime constructs the runtime closure the browser_auto tool
// calls. It resolves the sidecar client + browser-launch at call time (not
// build time) so a runtime that was built before the sidecar came up still
// works once the sidecar is ready.
func buildBrowserAutoRuntime(cfg *config.Config, opts Options) builtin.BrowserAutoRuntime {
	return builtin.BrowserAutoRuntime{
		Available: func() (bool, string) {
			if browserUseClientProvider == nil {
				return false, "autonomous-browsing sidecar is not wired up (desktop build only)"
			}
			if browserUseClientProvider() == nil {
				return false, "autonomous-browsing sidecar is not running (check that browser-use + a provider client are installed)"
			}
			return true, ""
		},
		Run: func(ctx context.Context, req builtin.BrowserAutoRunRequest) ([]builtin.BrowserAutoStep, string, error) {
			return runBrowserAuto(ctx, cfg, req)
		},
	}
}

// runBrowserAuto performs the full autonomous-browse sequence:
//  1. Launch a system browser with a CDP endpoint.
//  2. (Optional) attach it to the in-app panel via the desktop sink.
//  3. POST /run to the sidecar with the goal + cdp_url + model config.
//  4. Stream the SSE events into builtin.BrowserAutoStep records.
//  5. Close the browser (and detach the panel) on completion/cancel/error.
func runBrowserAuto(ctx context.Context, cfg *config.Config, req builtin.BrowserAutoRunRequest) ([]builtin.BrowserAutoStep, string, error) {
	if browserUseClientProvider == nil {
		return nil, "", fmt.Errorf("autonomous-browsing sidecar is not wired up")
	}
	client := browserUseClientProvider()
	if client == nil {
		return nil, "", fmt.Errorf("autonomous-browsing sidecar is not running")
	}

	// Resolve the LLM provider entry so we can hand the sidecar the bare model
	// name + base_url + api-key-env it needs. momapeer uses "provider/model"
	// refs (and may point at a custom gateway like 九天/MoMA), but browser-use's
	// ChatOpenAI/ChatAnthropic expect a bare model name and (for OpenAI-compatible
	// gateways) an explicit base_url. Without this translation the sidecar would
	// hit api.openai.com with a model name it doesn't understand.
	modelRef := cfg.Cowork.BrowserUseModel
	if modelRef == "" {
		modelRef = cfg.Cowork.VLMModel
	}
	var modelName, baseURL, providerKind, apiKeyEnv string
	if entry, ok := cfg.ResolveModel(modelRef); ok {
		modelName = entry.Model
		baseURL = entry.BaseURL
		providerKind = entry.Kind
		apiKeyEnv = entry.APIKeyEnv
	}
	// Fallbacks when the ref didn't resolve: pass the raw ref as the model and
	// let the sidecar's client default to OPENAI_API_KEY / api.openai.com. This
	// is the common dev path where the user set a model directly.
	if modelName == "" {
		modelName = modelRef
	}

	maxSteps := cfg.Cowork.BrowserUseMaxSteps
	if req.MaxSteps > 0 {
		maxSteps = req.MaxSteps
	}

	// Launch the shared browser. A fresh temp profile per run keeps sessions
	// isolated (no cross-task cookie bleed) unless the user configured a
	// persistent BrowserUserDataDir (to keep login state). The proxy routes the
	// browser through the user's network config so the agent reaches the same
	// sites momapeer's other traffic does (e.g. a CN proxy for GitHub).
	handle, err := browserlaunch.Launch(ctx, browserlaunch.LaunchOptions{
		Headless:    cfg.Cowork.BrowserHeadless,
		UserDataDir: cfg.Cowork.BrowserUserDataDir,
		Proxy:       resolveBrowserProxyURL(cfg.NetworkProxySpec()),
		StartURL:    req.URL,
	})
	if err != nil {
		return nil, "", fmt.Errorf("launch browser: %w", err)
	}
	defer handle.Close()
	slog.Info("browser_auto: launched shared browser",
		"name", handle.BrowserName, "cdp", handle.CDPURL, "ws", handle.WSURL)

	// Drive the sidecar. The cdp_url we hand it points at the browser we just
	// launched, so there is exactly one shared instance. The model/base_url/key
	// come from the resolved provider entry above. The proxy applies to the
	// sidecar's LLM client (the browser got its own --proxy-server at launch) so
	// LLM calls reach the gateway through the user's network config too.
	llmProxy := resolveBrowserProxyURL(cfg.NetworkProxySpec())
	stream, err := client.RunStream(ctx, browseruse.RunRequest{
		Goal:         req.Goal,
		URL:          req.URL,
		CDPURL:       handle.WSURL,
		MaxSteps:     maxSteps,
		Model:        modelName,
		ProviderKind: providerKind,
		BaseURL:      baseURL,
		APIKeyEnv:    apiKeyEnv,
		Proxy:        llmProxy,
	})
	if err != nil {
		return nil, "", fmt.Errorf("sidecar /run: %w", err)
	}

	// If the turn is cancelled (user clicked Stop, or the parent turn ended),
	// tell the sidecar to /stop so the agent loop breaks at its next step hook.
	// Without this the Python loop keeps running (billing LLM tokens, holding the
	// single-run lock) until the deferred handle.Close() kills the browser out
	// from under it — an orphaned run that also starves later calls with HTTP 409.
	// We use a detached context for the Stop call so it can complete even after
	// the turn ctx is gone.
	stopCtx, stopCancel := context.WithCancel(context.Background())
	defer stopCancel()
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Stop(stopCtx)
		case <-stopCtx.Done():
		}
	}()

	var steps []builtin.BrowserAutoStep
	var summary string
	for ev := range stream {
		step := builtin.BrowserAutoStep{
			Type: string(ev.Type),
			Step: ev.Step,
			Text: ev.Text,
			Done: ev.Done,
		}
		steps = append(steps, step)
		if ev.Done {
			switch ev.Type {
			case "done":
				summary = ev.Text
			case "error":
				// Return the steps taken + a clear error; the tool wraps it.
				return steps, "", fmt.Errorf("agent run failed: %s", ev.Text)
			}
		}
	}
	return steps, summary, nil
}
