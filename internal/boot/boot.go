// Package boot assembles a ready-to-drive control.Controller from configuration:
// it loads config, resolves the model(s), builds the tool registry (built-ins +
// plugins), wires the permission gate, and constructs the executor — optionally
// wrapping it in a two-model Coordinator. It is the one place that turns "what the
// user configured" into "a Controller a frontend can drive", so every frontend —
// the terminal TUI, the HTTP/SSE server, the desktop webview — shares the exact
// same assembly instead of each re-deriving it. Frontends pass only a sink and a
// couple of run knobs; everything else comes from config.
package boot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zzycxz/momapeer/internal/agent"
	"github.com/zzycxz/momapeer/internal/builtinmcp"
	"github.com/zzycxz/momapeer/internal/codegraph"
	"github.com/zzycxz/momapeer/internal/command"
	"github.com/zzycxz/momapeer/internal/config"
	"github.com/zzycxz/momapeer/internal/control"
	"github.com/zzycxz/momapeer/internal/event"
	"github.com/zzycxz/momapeer/internal/hook"
	"github.com/zzycxz/momapeer/internal/installsource"
	"github.com/zzycxz/momapeer/internal/instruction"
	"github.com/zzycxz/momapeer/internal/jobs"
	"github.com/zzycxz/momapeer/internal/lsp"
	"github.com/zzycxz/momapeer/internal/memory"
	"github.com/zzycxz/momapeer/internal/netclient"
	"github.com/zzycxz/momapeer/internal/outputstyle"
	"github.com/zzycxz/momapeer/internal/permission"
	"github.com/zzycxz/momapeer/internal/plugin"
	"github.com/zzycxz/momapeer/internal/provider"
	"github.com/zzycxz/momapeer/internal/sandbox"
	"github.com/zzycxz/momapeer/internal/skill"
	"github.com/zzycxz/momapeer/internal/tool"
	"github.com/zzycxz/momapeer/internal/tool/builtin"
)

// ErrUnknownModel is returned by Build when the configured model can't be
// resolved to a provider — e.g. a default_model left over from a renamed or
// removed provider. Callers can detect it (errors.Is) to re-run setup.
var ErrUnknownModel = errors.New("unknown model")

// globalBudget is the process-level LLM request budget, set once per Build from
// [llm] config. NewProviderWithProxy wraps every provider with it so main-agent
// + subagent + background tasks share one per-API-key RPM quota. nil/zero-RPM
// disables limiting (RateLimitedProvider passes through unwrapped).
var globalBudget *provider.RequestBudget

// buildingMainProvider is true while Build constructs the main executor's
// provider, so NewProviderWithProxy marks it high-priority (always granted RPM
// slots). Subagent/spawned providers are built with this false (background
// priority, respect reserve).
var buildingMainProvider bool

// GlobalBudget exposes the budget for the desktop layer (status display, cost
// estimates). May be nil when limiting is off.
func GlobalBudget() *provider.RequestBudget { return globalBudget }

// Options carries the per-run knobs a frontend chooses; everything else is read
// from configuration. Model "" falls back to the configured default_model;
// MaxSteps 0 uses the config/default. RequireKey forces the executor's API key to
// be present (run/serve pass true so a missing key fails fast; chat/desktop pass
// false so the UI is reachable before a key is set). Sink receives the agent's
// typed event stream.
type Options struct {
	Model      string
	MaxSteps   int
	RequireKey bool
	Sink       event.Sink
	// EffortOverride is a session-local reasoning effort override. Nil means use
	// the resolved provider config; a non-nil empty string means provider default.
	EffortOverride *string
	// Stderr is the writer for diagnostic warnings and plugin subprocess
	// stderr output. When nil, defaults to os.Stderr. Set to io.Discard
	// during model switch inside a bubbletea session to prevent any output
	// from corrupting the TUI's terminal raw mode.
	Stderr io.Writer
	// WorkspaceRoot is the project root directory for config, skills, memory,
	// commands, hooks, and tool confinement. When empty, the current working
	// directory is used (CLI default). Desktop tabs pass their project root here
	// so each tab loads its own config/skills/hooks without changing the process
	// cwd — enabling concurrent multi-project sessions.
	WorkspaceRoot string
	// ExtraPlugins are session-scoped MCP servers supplied by a host transport
	// (for example ACP session/new). They are connected eagerly for this
	// controller but are not persisted to momapeer.toml.
	ExtraPlugins []plugin.Spec
	// SessionDir overrides where persisted chat transcripts are written. When
	// empty, the shared CLI/global session directory is used.
	SessionDir string
	// Host is an externally-owned plugin.Host the controller should adopt instead
	// of allocating its own. When set, Build does NOT create or Close the host —
	// the caller owns its lifecycle (e.g. desktop shares one host per workspace
	// root across tabs so a multi-tab project doesn't spawn N codegraph
	// processes). The host still receives this session's configured plugins via
	// Add, but Close leaves it untouched. Nil (the default) keeps the legacy
	// per-session host that Build creates and closes.
	Host *plugin.Host
	// Profile layers product-mode overrides on top of the resolved config: a
	// different model/effort, an appended/replaced system prompt, a skill
	// whitelist/blacklist, and a plugin whitelist. It is the mechanism behind
	// app.SwitchProfile("cowork") — the same rebuild flow as model switching,
	// generalized to a bundle. Nil means "dev" / unprofiled behaviour (Model,
	// prompt, skills, plugins all come from config unchanged). When both Model
	// and Profile.Model are set, Model wins (caller's explicit knob beats the
	// profile default); same for EffortOverride vs Profile.Effort.
	Profile *config.Profile
}

// Build loads config, resolves the model(s), and returns a Controller wrapping a
// single Agent, or a two-model Coordinator when agent.planner_model is set. The
// returned controller owns plugin subprocesses; call Close (via Controller.Close)
// to release them.
func Build(ctx context.Context, opts Options) (*control.Controller, error) {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	root := resolveWorkspaceRoot(opts.WorkspaceRoot)
	// One-time import of v1/v0.5 legacy config — runs before Load so the freshly
	// written config + ~/.env are picked up this same boot. CLI Run also calls this
	// before config-only commands; this call stays as the shared frontend fallback.
	migrated, migErr := config.MigrateLegacyIfNeeded()
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return nil, err
	}
	modelName := opts.Model
	// A profile can pin a model (e.g. cowork uses a cheaper office model). The
	// caller's explicit opts.Model wins over the profile default, so a manual
	// model pick inside a profile is respected.
	if modelName == "" && opts.Profile != nil && strings.TrimSpace(opts.Profile.Model) != "" {
		modelName = opts.Profile.Model
	}
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	entry, ok := cfg.ResolveModel(modelName)
	if !ok {
		return nil, fmt.Errorf("%w %q (configured: %s); note: defining [[providers]] replaces the built-in presets, so add a [[providers]] entry for it or use a configured name, or run `momapeer setup` to reconfigure", ErrUnknownModel, modelName, providerNames(cfg))
	}
	if opts.EffortOverride != nil {
		entry.Effort = *opts.EffortOverride
		if entry.Kind == "anthropic" && strings.TrimSpace(entry.Effort) != "" && strings.TrimSpace(entry.Thinking) == "" {
			entry.Thinking = "adaptive"
		}
	} else if opts.Profile != nil && strings.TrimSpace(opts.Profile.Effort) != "" {
		// No explicit caller effort: adopt the profile's effort as the base before
		// the normal anthropic thinking heuristic applies.
		entry.Effort = opts.Profile.Effort
		if entry.Kind == "anthropic" && strings.TrimSpace(entry.Effort) != "" && strings.TrimSpace(entry.Thinking) == "" {
			entry.Thinking = "adaptive"
		}
	}
	if opts.RequireKey {
		if err := cfg.Validate(modelName); err != nil {
			return nil, err
		}
	}

	// Serialize the frontend's sink once: background jobs (below) emit from their
	// own goroutines, which can overlap a running turn's emission, so every emitter
	// shares this synchronized sink. The job manager is session-scoped — its jobs
	// outlive a turn and are cancelled by Controller.Close.
	sink := event.Sync(opts.Sink)

	if migErr != nil {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "config migration from ~/.momapeer failed: " + migErr.Error()})
	} else if migrated != nil {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: migrated.Notice()})
	}
	migrateLegacySessionSources(sink)

	// A resolvable model whose API key env is unset would otherwise build fine
	// (RequireKey is false so the UI stays reachable) and then fail silently on the
	// first request, showing as an empty/dead model. Surface the cause up front.
	if !opts.RequireKey && entry.APIKeyEnv != "" && entry.APIKey() == "" {
		sink.Emit(event.Event{Kind: event.Notice, Text: fmt.Sprintf("model %q is selected but its API key %s is not set — requests will fail until you set it", modelName, entry.APIKeyEnv)})
	}
	jm := jobs.NewManager(sink)

	proxySpec := cfg.NetworkProxySpec()
	if err := netclient.Validate(proxySpec); err != nil {
		return nil, err
	}

	// Initialize the global LLM request budget from [llm] config. RPM=0 (the
	// default) disables limiting; NewProviderWithProxy then passes providers
	// through unwrapped, preserving backward compatibility.
	globalBudget = provider.NewRequestBudget(cfg.LLM.RPM, cfg.LLM.ReserveMain)
	httpClient, err := netclient.NewHTTPClient(proxySpec, netclient.TransportOptions{})
	if err != nil {
		return nil, err
	}

	// The executor's provider is the main-agent provider — mark it
	// high-priority so it's always granted RPM slots (reserve_main protects it
	// from background tasks). Subagent providers built later default to
	// background priority.
	buildingMainProvider = true
	execProv, err := NewProviderWithProxy(entry, proxySpec, cfg.Jiutian.ImageUnderstand)
	buildingMainProvider = false
	if err != nil {
		return nil, err
	}

	sysPrompt, err := cfg.ResolveSystemPrompt()
	if err != nil {
		return nil, err
	}
	// A profile can replace the resolved system prompt wholesale (file) or
	// append to it (addon). File takes precedence and reads the file the same way
	// cfg.ResolveSystemPrompt does, so a profile's system_prompt_file is an
	// override of agent.system_prompt_file, not a third prompt. The addon is
	// folded early so it precedes instruction/output-style/memory/skill appends —
	// a profile's mode bias should be part of the base, not the tail.
	if opts.Profile != nil {
		if pf := strings.TrimSpace(opts.Profile.SystemPromptFile); pf != "" {
			b, rerr := os.ReadFile(pf)
			if rerr != nil {
				return nil, fmt.Errorf("profile %q system_prompt_file: %w", opts.Profile.Name, rerr)
			}
			sysPrompt = strings.TrimSpace(string(b))
		}
		if addon := strings.TrimSpace(opts.Profile.SystemPromptAddon); addon != "" {
			sysPrompt += "\n\n" + addon
		}
	}
	// Model-specific prompt addon (thinking encouragement, serial constraint, etc.)
	if addon := instruction.ForModel(entry.Model); addon != "" {
		sysPrompt += "\n\n" + addon
	}
	// Output style: fold the selected persona/tone block into the base prompt
	// before language/memory/skills append, so a "replace" style (keep-coding
	// false) still keeps those. Applied once, into the cache-stable prefix.
	// (MoMA currently does not report cache tokens; the prefix stability still helps.)
	if st, ok := outputstyle.Resolve(cfg.Agent.OutputStyle, outputstyle.Dirs()); ok {
		sysPrompt = outputstyle.Apply(sysPrompt, st)
	}
	sysPrompt += "\n\n" + config.LanguagePolicy

	// Persistent memory (momapeer.md / AGENTS.md hierarchy + auto-memory index)
	// folds into the system prompt exactly here, once: it becomes part of the
	// durable, cache-stable prefix every turn reuses, so memory costs nothing per
	// turn. Mid-session changes never touch this prefix — they ride the
	// controller's transient turn-injection and fold in on the next session.
	mem := memory.Load(memory.Options{CWD: root, UserDir: config.MemoryUserDir()})
	projectChecks := instruction.ExtractHostChecks(mem.Docs)
	sysPrompt = memory.Compose(sysPrompt, mem)

	// Skills: discover playbooks (built-in + project/custom/global) and fold their
	// one-liner index into the same cache-stable prefix — names + descriptions
	// only; bodies load on demand via run_skill or "/<name>". Bodies never enter
	// the prefix, so the index costs a fixed, small amount per turn.
	//
	// Profile skill filtering: a profile can disable additional skills and/or
	// expose a whitelist (only those skills stay callable). We resolve the merged
	// disabled set once here and pass it to BOTH stores so the live store and the
	// "all skills" index agree on what a profile hides.
	disabledNames := cfg.DisabledSkillNames()
	if opts.Profile != nil {
		disabledNames = applyProfileToSkillDisabled(opts.Profile, disabledNames)
	}
	// The LIVE store filters by disabledNames so disabled/profile-hidden skills
	// are uncallable. The ALL store deliberately does NOT filter — it lists every
	// discovered skill (including disabled) so the pinned index can tag them as
	// [关闭] for management (re-enable hints). Profile whitelist hiding for the
	// index is applied in the loop below via the Disabled flag.
	skillStore := skill.New(skill.Options{
		ProjectRoot:   root,
		CustomPaths:   cfg.SkillCustomPaths(),
		ExcludedPaths: cfg.SkillExcludedPaths(),
		DisabledNames: disabledNames,
		MaxDepth:      cfg.SkillMaxDepth(),
		Stderr:        opts.Stderr,
	})
	skills := skillStore.List()
	allSkillStore := skill.New(skill.Options{ProjectRoot: root, CustomPaths: cfg.SkillCustomPaths(), ExcludedPaths: cfg.SkillExcludedPaths(), MaxDepth: cfg.SkillMaxDepth(), Stderr: io.Discard})
	allSkills := allSkillStore.List()
	// A profile whitelist can only be enforced once we know every skill name:
	// anything not whitelisted is disabled. We compute the effective disabled
	// status per skill here — config-disabled OR profile-additive-disabled OR
	// (whitelist active AND not whitelisted). The store already hid the first two
	// from the live Skills() list; this re-marks them for the index tags.
	whitelist := profileSkillWhitelist(opts.Profile)
	indexedSkills := make([]skill.Skill, 0, len(allSkills))
	for _, s := range allSkills {
		disabled := cfg.IsSkillDisabled(s.Name) || isSkillDisabledByName(disabledNames, s.Name)
		if !disabled && whitelist != nil && !whitelist[config.SkillNameKey(s.Name)] {
			disabled = true
		}
		s.Disabled = disabled
		indexedSkills = append(indexedSkills, s)
	}
	sysPrompt = skill.ApplyIndex(sysPrompt, indexedSkills)

	reg := tool.NewRegistry()
	bashSpec := sandbox.Spec{Mode: cfg.BashMode(), WriteRoots: cfg.WriteRootsForRoot(root), Network: cfg.Sandbox.Network}
	if bashSpec.Mode == "enforce" && !sandbox.Available() {
		fmt.Fprintln(stderr, "warning: bash sandbox requested but unavailable on this platform; running bash unconfined")
	}
	if sandbox.ResolveShell().Kind == sandbox.ShellPowerShell {
		fmt.Fprintln(stderr, "warning: bash not found on PATH; the shell tool will run commands under Windows PowerShell. Install Git for Windows or WSL to use bash.")
	}
	searchSpec := builtin.ResolveSearch(cfg.Tools.Search.Engine, cfg.Tools.Search.RgPath, stderr)
	bashTimeout := time.Duration(cfg.BashTimeoutSeconds()) * time.Second
	addBuiltins(reg, cfg.Tools.Enabled, cfg.WriteRootsForRoot(root), bashSpec, bashTimeout, searchSpec, stderr, root, proxySpec)
	// Register Jiutian multimodal tools based on config (not via init(), so they
	// can be toggled per-capability in [jiutian] config section).
	for _, t := range builtin.JiutianTools(&cfg.Jiutian) {
		reg.Add(t)
	}
	// coWork-only browser automation tools. These spawn a Chromium subprocess on
	// first browser_open and are intentionally absent from the dev tool list — a
	// coding session has no use for them and should never pay the process cost.
	// Phase 1: the full browser_* set. Later cowork capabilities (screen/doc/rag)
	// register here the same way.
	// Browser automation is a general capability (on par with web_search),
	// available in both dev and cowork — a developer needs it to check docs,
	// debug frontends, and inspect API responses. browser path is read from
	// [cowork] browser_path for backward compat (existing configs keep working);
	// empty = auto-detect Chrome/Edge/Brave.
	builtin.SetConfiguredBrowserPath(cfg.Cowork.BrowserPath)
	for _, t := range builtin.BrowserTools() {
		reg.Add(t)
	}

	// coWork-only capabilities: desktop automation, scheduled tasks, email,
	// RAG, PPT. These are office-specific and stay gated to the cowork profile
	// so the dev tool list stays focused on coding.
	if opts.Profile != nil && strings.EqualFold(opts.Profile.Name, config.ProfileCowork) {
		// Desktop automation tools (screenshot, screen_click/type/scroll,
		// get_ui_tree). Windows-native (Win32 BitBlt/SendInput); on other
		// platforms ScreenTools returns nil so nothing registers and cowork
		// still works minus desktop control.
		for _, t := range builtin.ScreenTools() {
			reg.Add(t)
		}
		// Scheduled-task tools. The scheduler instance is injected by the
		// desktop app (see app.go) via builtin.SetScheduler; boot just
		// registers the tool surface here. When no scheduler is bound (CLI/TUI
		// cowork), the tools return a clear "offline" error.
		for _, t := range builtin.SchedulerTools() {
			reg.Add(t)
		}
		// Email tools (SMTP send + IMAP read/search). Config injected from
		// [cowork.smtp] and [cowork.imap]; when a side is unset, that side's
		// tool returns a config error (the other still works).
		builtin.SetEmailConfig(&cfg.Cowork.SMTP)
		builtin.SetIMAPConfig(&cfg.Cowork.IMAP)
		for _, t := range builtin.EmailTools() {
			reg.Add(t)
		}
		// RAG knowledge-base tools. The store is injected by the desktop app
		// (app.go) via builtin.SetRAGStore; boot registers the tool surface.
		for _, t := range builtin.RAGTools() {
			reg.Add(t)
		}
		// VLM backend for screen_perceive (desktop automation). Default 九天,
		// switchable to provider multimodal (minimax/kimi) via [cowork] vlm_*.
		builtin.SetVLMConfig(builtin.VLMConfig{
			Backend: cfg.Cowork.VLMBackend,
			Model:   cfg.Cowork.VLMModel,
		})
		// Hybrid RAG: when an embedding model is configured, inject an embedder so
		// rag_search reranks FTS5 hits with semantic similarity. Empty model =
		// FTS5-only (the default, works offline).
		builtin.SetRAGEmbedder(builtin.ResolveRAGEmbedder(cfg.Cowork.EmbeddingModel))
		// Document tools (csv/json/md/txt read + write + convert). Text-based
		// formats only; binary Office handled elsewhere (ppt via WPS MCP).
		for _, t := range builtin.DocumentTools() {
			reg.Add(t)
		}
		// Expert-team tools. The orchestrator + store are injected by the
		// desktop app (app.go) via builtin.SetExpertOrchestrator/SetExpertStore;
		// boot registers the tool surface here. When unbound (CLI/TUI cowork),
		// the tools return a clear "offline" error.
		for _, t := range builtin.ExpertTools() {
			reg.Add(t)
		}
	}
	// Always construct a host, even with no plugins configured, so the controller's
	// host pointer is stable for the session and `/mcp add` can hot-add into it.
	// When the caller supplies an externally-owned host (desktop shares one per
	// workspace root across tabs), adopt it instead of allocating — and remember
	// not to Close it on controller teardown (the owner refcounts it).
	pluginHost := opts.Host
	hostOwned := pluginHost == nil
	if hostOwned {
		pluginHost = plugin.NewHost()
	}

	// Partition configured plugins by tier so eager/lazy/background can each
	// take the path that fits them. User entries default to background: the
	// session starts immediately while enabled MCP servers warm up.
	autoStartEntries := builtinmcp.AppendEnabled(
		cfg.AutoStartPlugins(),
		cfg.Plugins,
		cfg.BuiltInMCP.EnabledNames(),
		pluginSpecNames(opts.ExtraPlugins)...,
	)
	// coWork PPT capability: when the user configured the wps-ppt-mcp-server
	// path, register it as a session MCP server so ppt_create/from_template/etc.
	// are reachable. The server is a separate Python project (FastMCP + pywin32);
	// deps are checked separately (agent surfaces an install hint if missing).
	if opts.Profile != nil && strings.EqualFold(opts.Profile.Name, config.ProfileCowork) && strings.TrimSpace(cfg.Cowork.WPSPPTServerPath) != "" {
		if entry, err := builtinmcp.WPSPPTEntry(cfg.Cowork.WPSPPTServerPath, cfg.Cowork.WPSPPTPython); err == nil {
			// Avoid duplicate if the user also declared it in [[plugins]].
			already := false
			for _, e := range autoStartEntries {
				if e.Name == builtinmcp.WPSPPTName {
					already = true
					break
				}
			}
			if !already {
				autoStartEntries = append(autoStartEntries, entry)
			}
		} else {
			fmt.Fprintf(stderr, "warning: cowork wps-ppt server not registered: %v\n", err)
		}
	}
	// A profile plugin whitelist hides MCP servers not named in it. Empty list =
	// all plugins (dev default). We filter after AppendEnabled so built-in MCPs
	// (time, …) and session ExtraPlugins are also subject to the whitelist — a
	// coWork profile that lists only its office servers should not quietly expose
	// the dev codegraph server. Names match case-insensitively via PluginAllowedByProfile.
	if opts.Profile != nil && len(opts.Profile.Plugins) > 0 {
		kept := autoStartEntries[:0]
		for _, e := range autoStartEntries {
			if config.PluginAllowedByProfile(opts.Profile, e.Name) {
				kept = append(kept, e)
			}
		}
		autoStartEntries = kept
	}
	eagerEntries, lazyEntries, bgEntries := partitionByTier(autoStartEntries)

	// Auto-demote: any eager plugin that has been chronically slow (recent
	// samples repeatedly hit the blocking startup budget) drops to lazy
	// for this session. The user keeps eager intent, just doesn't pay for it
	// on a server that's been misbehaving. A notice surfaces the demotion.
	var demoteMessages []string
	budget := plugin.DefaultStartupBudget()
	kept := eagerEntries[:0]
	for _, e := range eagerEntries {
		rec := plugin.Recommend(e.Name, budget, 0)
		if rec.Demote {
			demoteMessages = append(demoteMessages, rec.Reason)
			lazyEntries = append(lazyEntries, e)
			continue
		}
		kept = append(kept, e)
	}
	eagerEntries = kept

	eagerSpecs := PluginSpecs(eagerEntries)
	lazySpecs := PluginSpecs(lazyEntries)
	bgSpecs := PluginSpecs(bgEntries)

	// CodeGraph is a built-in MCP server fetched on first use. When it resolves,
	// inject it as one more stdio plugin pinned to the project root (it is
	// cwd-aware); EnsureInit only creates .codegraph/ (fast, size-independent),
	// serve's daemon then indexes in the background, so startup never blocks even
	// on a large repo. When it is not yet installed, fetch it in the background
	// (one-time, ~45MB) if auto_install is on — startup still never blocks, the
	// tools come online next session — otherwise point the user at the explicit
	// install command. A failed init or fetch is a notice, not fatal.
	//
	// CodeGraph is fixed to background startup. Legacy tier values are ignored so
	// enabling it never blocks chat startup.
	if cfg.Codegraph.Enabled {
		bin, ok := codegraph.Resolve(cfg.Codegraph.Path)
		switch {
		case ok && !codegraph.IndexableRoot(root):
			sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
				Text: "codegraph: project root is a filesystem root — skipped to avoid indexing the whole volume"})
		case ok:
			spec := plugin.Spec{
				Name:              "codegraph",
				StripRawPrefix:    "codegraph_",
				Command:           bin,
				Args:              []string{"serve", "--mcp"},
				Dir:               root,
				ReadOnlyToolNames: codegraph.ReadOnlyToolNames(),
				// The daemon walks and indexes the whole tree; below-normal
				// priority keeps it from starving the user's machine (#3797).
				LowPriority: true,
			}
			warm := codegraph.Initialized(root)
			if err := codegraph.EnsureInit(ctx, bin, root); err != nil {
				sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
					Text: "codegraph: init failed (" + err.Error() + ") — symbol-graph tools disabled this session"})
				break
			}
			bgNotice := func() {
				if !warm {
					sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
						Text: "codegraph: preparing code-intelligence tools in the background — tools will appear when ready"})
				}
			}
			bgSpecs = append(bgSpecs, spec)
			bgNotice()
		case cfg.Codegraph.AutoInstall:
			notify := func(msg string) { sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: msg}) }
			notify("codegraph: fetching code-intelligence runtime in the background (one-time) — symbol-graph tools available next session")
			codegraphClient, err := netclient.NewHTTPClient(proxySpec, netclient.TransportOptions{})
			if err != nil {
				notify("codegraph: install skipped (" + err.Error() + ")")
			} else {
				go func() {
					if _, err := codegraph.InstallWithClient(context.WithoutCancel(ctx), codegraphClient, nil); err != nil {
						notify("codegraph: install failed (" + err.Error() + ") — using grep/glob; retries next session")
					} else {
						notify("codegraph: installed — symbol-graph tools available next session")
					}
				}()
			}
		default:
			sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
				Text: "codegraph: not installed — run `momapeer codegraph install` to enable symbol-graph tools"})
		}
	}
	eagerSpecs = append(eagerSpecs, opts.ExtraPlugins...)

	// Apply caller-supplied stderr override to every spec across tiers.
	if opts.Stderr != nil {
		for i := range eagerSpecs {
			eagerSpecs[i].Stderr = opts.Stderr
		}
		for i := range lazySpecs {
			lazySpecs[i].Stderr = opts.Stderr
		}
		for i := range bgSpecs {
			bgSpecs[i].Stderr = opts.Stderr
		}
	}

	// Eager: block until handshake. Failures show up in /mcp.
	if len(eagerSpecs) > 0 {
		if hostOwned {
			// Legacy per-session path: StartAvailable allocates a fresh host and
			// hands its handshaked plugins back to this controller.
			host, ptools := plugin.StartAvailable(ctx, eagerSpecs)
			pluginHost = host
			for _, t := range ptools {
				reg.Add(t)
			}
			go host.StartPhaseB(ctx, sink)
			if text, ok := MCPStartupNotice(host.Failures()); ok {
				sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: text})
			}
		} else {
			// Shared-host path (desktop): add eager specs into the externally-owned
			// host so every tab sharing this workspace root shares one set of MCP
			// subprocesses. Concurrency mirrors StartAvailable's fan-out.
			ptools := plugin.StartAvailableInto(ctx, pluginHost, eagerSpecs)
			for _, t := range ptools {
				reg.Add(t)
			}
			go pluginHost.StartPhaseB(ctx, sink)
			if text, ok := MCPStartupNotice(pluginHost.Failures()); ok {
				sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: text})
			}
		}
	}

	// Lazy / background: register placeholder tools now; the real spawn waits
	// for either the first model call (lazy) or a goroutine kicked off here
	// (background). Both share the same pluginHost so /mcp status, hot-add,
	// and Close see one cohesive set of servers regardless of tier.
	registerDeferred := func(specs []plugin.Spec, kick bool) {
		for _, s := range specs {
			cs, _ := plugin.LoadCachedSchema(s.Name, plugin.SpecFingerprint(s))
			for _, t := range plugin.LazyToolset(s, cs, pluginHost, reg, ctx, kick) {
				reg.Add(t)
			}
		}
	}
	registerDeferred(lazySpecs, false)
	registerDeferred(bgSpecs, true)

	// Inject codegraph steering into the system prompt when symbol-graph tools
	// are available, so the model knows to prefer them for architecture / call-graph
	// questions over grep/read_file. Also register codegraph tool names in the
	// subagent allowed-tools list so explore/research/review can use them.
	if cfg.Codegraph.Enabled {
		prefix := plugin.ToolPrefix("codegraph")
		var cgTools []string
		for _, name := range reg.Names() {
			if strings.HasPrefix(name, prefix) {
				cgTools = append(cgTools, name)
			}
		}
		if len(cgTools) > 0 {
			sysPrompt += "\n\n" + codegraph.SteerText
			skill.SetExtraReadTools(cgTools)
		}
	}

	for _, msg := range demoteMessages {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: msg})
	}

	// Close the plugin host only when Build owns it. A shared host (desktop's
	// per-workspace-root host) is refcounted by its owner; controller teardown
	// releases the owner's reference instead of tearing down subprocesses other
	// tabs still depend on.
	var cleanup func()
	// memSearchSvc holds the bitemporal search/index service when one could be
	// opened; built before the memory tools so its index can be attached to the
	// store, and reused by memory_query below.
	var memSearchSvc *memory.SearchService
	if hostOwned {
		cleanup = pluginHost.Close
	}

	// LSP tools resolve their servers on PATH and spawn lazily on first query, so
	// registering them is cheap even when no server is installed (a query then
	// returns an install hint). The manager is session-scoped; chain its shutdown
	// into the controller's cleanup so servers stop with the session, not the turn.
	if cfg.LSP.Enabled {
		lspMgr := lsp.NewManager(root, LSPSpecs(cfg.LSP))
		for _, t := range lsp.Tools(lspMgr) {
			reg.Add(t)
		}
		prev := cleanup
		cleanup = func() {
			if prev != nil {
				prev()
			}
			lspMgr.Close()
		}
	}

	maxSteps := cfg.Agent.MaxSteps
	if opts.MaxSteps > 0 {
		maxSteps = opts.MaxSteps
	}
	subagentStore := newSubagentStore(config.SessionDir())

	// Permission policy gates every tool call. The headless gate (no Approver)
	// resolves "ask" to allow — preserving `momapeer run` autonomy — while deny
	// rules hard-block in every mode. Interactive frontends (chat, desktop) swap
	// in an interactive gate later via Controller.EnableInteractiveApproval.
	// Sub-agents always run headless: they have no UI to answer a prompt, so they
	// inherit this same gate.
	policy := permission.New(cfg.Permissions.Mode, cfg.Permissions.Allow, cfg.Permissions.Ask, cfg.Permissions.Deny)
	headlessGate := permission.NewGate(policy, nil)

	// Hooks: load the global settings.json plus the project's (only when trusted —
	// project hooks run arbitrary shell commands, so cloning a repo must not
	// silently execute them). Non-blocking hook output is surfaced to the user as
	// a Notice through the shared sink. The runner fires PreToolUse/PostToolUse in
	// the agent loop and UserPromptSubmit/Stop at the controller's turn boundary.
	hooksTrusted := hook.IsTrusted(root, "")
	hookRunner := hook.NewRunner(
		hook.Load(hook.LoadOptions{ProjectRoot: root, Trusted: hooksTrusted}),
		root, nil,
		func(msg string) { sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: msg}) },
	)
	if hook.ProjectDefinesHooks(root) && !hooksTrusted {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: "this project defines hooks but they are not trusted — run /hooks trust to enable them"})
	}

	// The `task` tool spawns sub-agents that reuse the parent's provider and
	// tool registry. Wired here after the built-ins / plugins are loaded so
	// sub-agents inherit the full tool set (minus `task` itself, to keep
	// nesting out of the picture). It registers into the same reg the
	// executor uses, so the model surfaces it like any other tool.
	resolveSubagentProvider := func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error) {
		me := *entry
		if strings.TrimSpace(modelRef) != "" {
			resolved, ok := cfg.ResolveModel(modelRef)
			if !ok {
				return nil, nil, 0, fmt.Errorf("unknown model %q", modelRef)
			}
			me = *resolved
		}
		if strings.TrimSpace(effort) != "" {
			normalized, err := config.NormalizeEffort(&me, effort)
			if err != nil {
				return nil, nil, 0, err
			}
			me.Effort = normalized
			if me.Kind == "anthropic" && strings.TrimSpace(me.Effort) != "" && strings.TrimSpace(me.Thinking) == "" {
				me.Thinking = "adaptive"
			}
		}
		p, err := NewProviderWithProxy(&me, proxySpec)
		if err != nil {
			return nil, nil, 0, err
		}
		return p, me.Price, me.ContextWindow, nil
	}
	subagentIdentity := func(modelRef, effort string) (string, string) {
		return subagentEffectiveIdentity(cfg, modelName, entry, modelRef, effort)
	}
	taskModel := firstNonEmpty(cfg.Agent.SubagentModels["task"], cfg.Agent.SubagentModel)
	taskEffort := firstNonEmpty(cfg.Agent.SubagentEfforts["task"], cfg.Agent.SubagentEffort)
	taskTool := agent.NewTaskTool(execProv, entry.Price, reg, maxSteps,
		entry.ContextWindow, cfg.Agent.SoftCompactRatio, cfg.Agent.CompactRatio, cfg.Agent.CompactForceRatio,
		cfg.Agent.Temperature, config.ArchiveDir(), "", headlessGate,
		taskModel, taskEffort, resolveSubagentProvider).
		WithTranscripts(subagentStore, root, modelName, entry.Effort).
		WithTranscriptIdentityResolver(subagentIdentity)
	reg.Add(taskTool)
	// parallel_tasks dispatches independent sub-agents concurrently, reusing the
	// task tool's provider/tool/transcript machinery. Each sub-task may override
	// the model, so a caller can route independent work to different models on the
	// same platform (e.g. a reasoning model for planning, a code model for impl).
	reg.Add(agent.NewParallelTasksTool(taskTool, reg))

	// The `remember` tool lets the model persist durable facts to the project's
	// auto-memory store; `forget` prunes ones that turn out wrong. The saved index
	// loads into the prefix on the next session.
	// ConflictDetector uses the main provider for LLM-based contradiction detection.
	// Open the bitemporal index (FTS + facts) BEFORE constructing the memory
	// tools so it can be attached to the store: writes then keep it in sync and
	// queries (ListAsOf / ListActiveByType) prefer the SQL path. Falls back to
	// file scans when no index can be opened.
	if mem.Store.Dir != "" {
		if svc, err := memory.NewSearchService(mem.Store); err == nil {
			// migrate FTS/facts schema if needed. A failure here (e.g. SQLite
			// version too old, permission error) silently degrades memory search
			// to file scans — log it so the degradation is diagnosable rather
			// than invisible.
			if err := svc.EnsureSchema(); err != nil {
				slog.Warn("memory: FTS schema migration failed; search will fall back to file scans", "err", err)
			}
			// Recover from any crash that left the index mid-write: re-sync the
			// whole index with disk so List/ListAsOf/ListActiveByType (which read
			// the index directly, without the lazy Reconcile that Search runs)
			// never observe a drifted state carried over from the last session.
			// Non-fatal: a stale index still serves correct (if slower) queries.
			if err := svc.Reconcile(); err != nil {
				slog.Warn("memory: index reconcile failed after startup", "err", err)
			}
			mem.Store = mem.Store.AttachIndex(svc.Index())
			// Chain FTS DB cleanup so the file handle is released on session end.
			prev := cleanup
			cleanup = func() {
				if prev != nil {
					prev()
				}
				svc.Close()
			}
			// Keep a handle so memory_query can use the same service instance.
			memSearchSvc = svc
		}
	}

	// Same-name overwrites are handled by Save() inline supersede (Phase 2);
	// the detector catches different-name contradictions (Phase 4).
	chatFn := providerChatFunc(execProv)
	conflictDetector := memory.NewLLMConflictDetector(chatFn)
	reg.Add(memory.NewRememberTool(mem.Store, conflictDetector))
	reg.Add(memory.NewForgetTool(mem.Store))
	reg.Add(memory.NewRecallTool(mem.Store))
	// Phase 8-9: compact, profile, status tools.
	memCfg := memory.DefaultDecayConfig()
	reg.Add(memory.NewCompactTool(mem.Store, memCfg))
	reg.Add(memory.NewProfileTool(mem.Store))
	reg.Add(memory.NewStatusTool(mem.Store, memCfg))

	// Passive memory capture (P1): after each turn, a lightweight LLM pass
	// extracts durable facts from the conversation and saves them as Status
	// "pending" for the user to confirm in the timeline panel. The extractor
	// is fire-and-forget (10s timeout, swallows errors) so it never blocks the
	// turn. Wired into the controller's OnTurnEnd hook below.
	factExtractor := memory.NewLLMFactExtractor(chatFn)

	// The `memory_query` tool lets the model search saved memories with optional
	// keyword and time-point filtering (e.g. "where did I live in March?").
	if memSearchSvc != nil {
		reg.Add(memory.NewMemoryQueryTool(mem.Store, memSearchSvc))
	}

	// The `ask` tool puts structured multiple-choice questions to the user. It
	// reaches them through the Asker on the call context, which interactive
	// frontends wire to the controller (EnableInteractiveApproval); a headless run
	// has none, so ask resolves to "decide for yourself".
	reg.Add(agent.NewAskTool())

	// Skill tools: run_skill / install_skill plus the dedicated subagent wrappers
	// (explore / research / review / security_review). A subagent skill reuses the
	// sub-agent machinery via this runner — an isolated loop with the skill body
	// as system prompt, a tool set scoped to the skill's allowed-tools (minus the
	// task/skill meta-tools, to bar recursion), and an optional per-skill model.
	// Its tool activity nests under the invoking call, like `task`.
	skillRunner := func(sctx context.Context, sk skill.Skill, task string, runOpts skill.SubagentRunOptions) (string, error) {
		prov, price, ctxWin := execProv, entry.Price, entry.ContextWindow
		modelRef := subagentModelRef(cfg, sk)
		effortRef := subagentEffortRef(cfg, sk)
		if modelRef != "" || effortRef != "" {
			p, pr, cw, err := resolveSubagentProvider(modelRef, effortRef)
			if err != nil {
				return "", fmt.Errorf("subagent skill %q profile: %w", sk.Name, err)
			}
			prov, price, ctxWin = p, pr, cw
		}
		subReg := agent.FilterRegistry(reg, sk.AllowedTools, agent.SubagentMetaTools()...)
		continueFrom, forkFrom := strings.TrimSpace(runOpts.ContinueFrom), strings.TrimSpace(runOpts.ForkFrom)
		if continueFrom != "" && forkFrom != "" {
			return "", fmt.Errorf("continue_from and fork_from are mutually exclusive")
		}
		parentID, _, _, _ := agent.CallContext(sctx)
		parentSession := agent.ParentSession(sctx)
		var run *agent.SubagentRun
		if subagentStore == nil || parentSession == "" {
			// Headless runs (e.g. `momapeer run`) have no persistent session to
			// own a transcript. Run the skill sub-agent ephemerally, as before
			// persisted transcripts existed, instead of failing. Continuation and
			// fork need a persisted owner, so they error here.
			if continueFrom != "" || forkFrom != "" {
				return "", fmt.Errorf("continue_from/fork_from require a persisted session; none is active in this run")
			}
			run = agent.EphemeralSubagentRun(sk.Body)
		} else {
			identityModel, identityEffort := subagentIdentity(modelRef, effortRef)
			spec := agent.SubagentSpec{
				Kind:             "skill",
				Name:             sk.Name,
				WorkspaceRoot:    root,
				ParentSession:    parentSession,
				ParentToolCallID: parentID,
				SystemPrompt:     sk.Body,
				Registry:         subReg,
				Model:            identityModel,
				Effort:           identityEffort,
			}
			var prepErr error
			switch {
			case continueFrom != "":
				run, prepErr = subagentStore.PrepareContinue(continueFrom, spec)
			case forkFrom != "":
				run, prepErr = subagentStore.PrepareFork(forkFrom, spec)
			default:
				run, prepErr = subagentStore.PrepareFresh(spec)
			}
			if prepErr != nil {
				return "", prepErr
			}
		}
		defer run.Release()
		steps := maxSteps
		if steps > 0 {
			if steps /= 2; steps < 5 {
				steps = 5
			}
		}
		answer, err := agent.RunSubAgentWithSession(sctx, prov, subReg, run.Session, task, agent.Options{
			MaxSteps:      steps,
			Temperature:   cfg.Agent.Temperature,
			Pricing:       price,
			Gate:          headlessGate,
			ContextWindow: ctxWin,
			ArchiveDir:    config.ArchiveDir(),
		}, agent.NestedSink(sctx, event.Discard))
		if err != nil {
			return "", errors.Join(err, subagentStore.SaveFailed(run))
		}
		if err := subagentStore.SaveCompleted(run); err != nil {
			return "", errors.Join(err, subagentStore.SaveFailed(run))
		}
		return agent.FormatSubagentResult(answer, run.Ref, false), nil
	}
	skillProfile := func(sk skill.Skill) *event.Profile {
		model, effort := subagentModelRef(cfg, sk), subagentEffortRef(cfg, sk)
		if model == "" && effort == "" {
			return nil
		}
		return &event.Profile{Model: model, Effort: effort}
	}
	reg.Add(skill.NewRunSkillTool(skillStore, skillRunner, skillProfile))
	reg.Add(skill.NewReadSkillTool(skillStore))
	reg.Add(skill.NewInstallSkillTool(skillStore, nil))
	reg.Add(installsource.NewTool(installsource.Options{
		ProjectRoot: root,
		HTTPClient:  httpClient,
		ConnectMCP: func(e config.PluginEntry) (installsource.MCPConnectResult, error) {
			exp := e.ExpandedPlugin()
			spec := plugin.Spec{
				Name:    exp.Name,
				Type:    exp.Type,
				Command: exp.Command,
				Args:    exp.Args,
				Env:     exp.Env,
				URL:     exp.URL,
				Headers: exp.Headers,
			}
			if opts.Stderr != nil {
				spec.Stderr = opts.Stderr
			}
			tools, err := pluginHost.Add(ctx, spec)
			if err != nil {
				return installsource.MCPConnectResult{}, err
			}
			reg.RemovePrefix(plugin.ToolPrefix(spec.Name))
			for _, t := range tools {
				reg.Add(t)
			}
			// Disconnect closes the server and drops its namespaced tools.
			// Used by the install_source rollback path when SaveTo fails.
			disconnect := func() {
				if prefix, ok := pluginHost.Remove(spec.Name); ok {
					reg.RemovePrefix(prefix)
				}
			}
			return installsource.MCPConnectResult{
				ToolCount:  len(tools),
				Disconnect: disconnect,
			}, nil
		},
		OnDisconnect: func(serverName string) bool {
			if prefix, ok := pluginHost.Remove(serverName); ok {
				reg.RemovePrefix(prefix)
				return true
			}
			return false
		},
	}))
	for _, t := range skill.BuiltinSubagentTools(skillStore, skillRunner, skillProfile) {
		reg.Add(t)
	}

	execSess := agent.NewSession(sysPrompt)
	executor := agent.New(execProv, reg, execSess, agent.Options{
		MaxSteps:          maxSteps,
		Temperature:       cfg.Agent.Temperature,
		Pricing:           entry.Price,
		Gate:              headlessGate,
		Hooks:             hookRunner,
		Jobs:              jm,
		ProjectChecks:     projectChecks,
		ContextWindow:     entry.ContextWindow,
		SoftCompactRatio:  cfg.Agent.SoftCompactRatio,
		CompactRatio:      cfg.Agent.CompactRatio,
		CompactForceRatio: cfg.Agent.CompactForceRatio,
		ArchiveDir:        config.ArchiveDir(),
	}, sink)

	// Custom slash commands (.momapeer/commands + user dir). Best-effort: a malformed
	// file is skipped, and a load error never blocks the session.
	cmds, _ := command.Load(config.CommandDirsForRoot(root)...)

	// Expose the loaded slash commands (skills + custom commands) to the model via
	// the slash_command tool, so it can invoke a project playbook by name the way a
	// user types "/name". Skills are added first, then commands, so a command wins
	// a name clash — matching the prompt's command-over-skill precedence.
	var slashEntries []command.SlashEntry
	for _, sk := range skills {
		sk := sk
		slashEntries = append(slashEntries, command.SlashEntry{
			Name:        sk.Name,
			Description: sk.Description,
			Render:      func(args []string) string { return skill.Render(sk, strings.Join(args, " ")) },
		})
	}
	for _, cmd := range cmds {
		cmd := cmd
		slashEntries = append(slashEntries, command.SlashEntry{
			Name:        cmd.Name,
			Description: cmd.Description,
			ArgHint:     cmd.ArgHint,
			Render:      func(args []string) string { return cmd.Render(args) },
		})
	}
	reg.Add(command.NewSlashCommandTool(slashEntries))

	var runner agent.Runner = executor
	label := entry.Model
	var classifier *control.ProviderAutoPlanClassifier

	// Two-model collaboration: a distinct planner_model wraps the executor in a
	// Coordinator with its own session, kept separate for cache stability. The
	// planner gets the same standing memory context and a filtered read-only
	// research tool set, so it can inspect rules/code without side effects.
	if pm := cfg.Agent.PlannerModel; pm != "" {
		pe, ok := cfg.ResolveModel(pm)
		if !ok {
			return nil, fmt.Errorf("planner_model %q is not a configured provider", pm)
		}
		if pe.Model != entry.Model {
			plannerProv, err := NewProviderWithProxy(pe, proxySpec)
			if err != nil {
				return nil, fmt.Errorf("planner %q: %w", pm, err)
			}
			plannerSess := agent.NewSession(agent.PlannerPromptWithContext(mem.Block()))
			plannerTools := agent.PlannerToolRegistry(reg)
			runner = agent.NewCoordinator(plannerProv, plannerSess, pe.Price, plannerTools, agent.Options{
				MaxSteps:          cfg.Agent.PlannerMaxSteps,
				MaxStepsKey:       "agent.planner_max_steps",
				Gate:              headlessGate,
				ContextWindow:     pe.ContextWindow,
				SoftCompactRatio:  cfg.Agent.SoftCompactRatio,
				CompactRatio:      cfg.Agent.CompactRatio,
				CompactForceRatio: cfg.Agent.CompactForceRatio,
				ArchiveDir:        config.ArchiveDir(),
			}, executor, cfg.Agent.Temperature, sink, control.TaskWarrantsPlanner)
			label = entry.Model + " + planner " + pe.Model
		}
	}
	if !strings.EqualFold(strings.TrimSpace(cfg.Agent.AutoPlan), "off") && cfg.Agent.AutoPlanClassifier != "" {
		cm := cfg.Agent.AutoPlanClassifier
		ce, ok := cfg.ResolveModel(cm)
		if !ok {
			return nil, fmt.Errorf("auto_plan_classifier %q is not a configured provider", cm)
		}
		classifierProv, err := NewProviderWithProxy(ce, proxySpec)
		if err != nil {
			return nil, fmt.Errorf("auto_plan_classifier %q: %w", cm, err)
		}
		classifier = control.NewProviderAutoPlanClassifier(classifierProv)
	}

	sessionDir := opts.SessionDir
	if sessionDir == "" {
		sessionDir = config.SessionDir()
	}

	ctrlOpts := control.Options{
		Runner:        runner,
		Executor:      executor,
		Sink:          sink,
		Policy:        policy,
		Label:         label,
		SystemPrompt:  sysPrompt,
		SessionDir:    sessionDir,
		Host:          pluginHost,
		Commands:      cmds,
		Skills:        skills,
		AllSkills:     allSkills,
		SkillStore:    skillStore,
		AllSkillStore: allSkillStore,
		Hooks:         hookRunner,
		Memory:        mem,
		Cleanup:       cleanup,
		Jobs:          jm,
		Registry:      reg,
		PluginCtx:     ctx,
		WorkspaceRoot: root,
		AutoPlan:      cfg.Agent.AutoPlan,
		OnRemember: func(rule string) control.RememberResult {
			return rememberPermissionRule(root, rule)
		},
		OnTurnEnd: buildTurnEndExtractor(factExtractor, mem.Store),
	}
	if classifier != nil {
		ctrlOpts.Classifier = classifier
	}
	return control.New(ctrlOpts), nil
}

func migrateLegacySessionSources(sink event.Sink) {
	dest := config.SessionDir()
	if strings.TrimSpace(dest) == "" {
		return
	}
	type legacySource struct {
		dir     string
		label   string
		migrate func(srcDir, globalDest string, projectDir func(string) string) (int, error)
	}
	var sources []legacySource
	if home, herr := os.UserHomeDir(); herr == nil {
		sources = append(sources, legacySource{
			dir:     filepath.Join(home, ".momapeer", "sessions"),
			label:   "~/.momapeer/sessions",
			migrate: agent.MigrateLegacySessions,
		})
	}
	// Back-fill v0.x sessions from the current user config session directory as
	// well. This covers users whose platform config root was redirected before the
	// Go rewrite; their event logs can already live where v2 stores sessions.
	sources = append(sources, legacySource{
		dir:     dest,
		label:   dest,
		migrate: agent.MigrateLegacySessionsFromConfigDir,
	})

	seen := map[string]bool{}
	for _, src := range sources {
		if strings.TrimSpace(src.dir) == "" {
			continue
		}
		key := filepath.Clean(src.dir)
		if seen[key] {
			continue
		}
		seen[key] = true
		if n, serr := src.migrate(src.dir, dest, config.ProjectSessionDir); serr == nil && n > 0 {
			sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf("imported %d past session(s) from %s — resume them with --resume or the history panel", n, src.label)})
		}
	}
}

func rememberPermissionRule(workspaceRoot, rule string) control.RememberResult {
	path := rememberPermissionConfigPath(workspaceRoot)
	edit := config.LoadForEdit(path)
	result := control.RememberResult{Rule: strings.TrimSpace(rule), Path: path}
	if coveredBy := coveredPermissionRule(edit.Permissions.Allow, result.Rule); coveredBy != "" {
		result.CoveredBy = coveredBy
		return result
	}
	edit.Permissions.Allow = pruneCoveredPermissionRules(edit.Permissions.Allow, result.Rule)
	if err := edit.AddPermissionRule("allow", rule); err != nil {
		slog.Warn("persist permission rule", "rule", rule, "err", err)
		result.Err = err
		return result
	}
	if err := edit.SaveTo(path); err != nil {
		slog.Warn("save config after permission rule", "err", err)
		result.Err = err
		return result
	}
	result.Saved = true
	return result
}

// buildTurnEndExtractor wires the passive-memory-capture hook: it returns the
// OnTurnEnd closure the controller calls after each turn, or nil if extraction
// is disabled (no extractor configured, or the store is unavailable). The
// closure extracts candidate facts and saves each as Status "pending" so they
// surface in the timeline panel for user confirmation without polluting the
// active prompt. All errors are logged and swallowed — auto-capture is purely
// best-effort background work and must never affect the foreground turn.
func buildTurnEndExtractor(extractor memory.FactExtractor, store memory.Store) func(ctx context.Context, lastUserMsg, lastAssistant string) {
	if extractor == nil || store.Dir == "" && store.GlobalDir == "" {
		return nil // nothing to capture into
	}
	return func(ctx context.Context, lastUserMsg, lastAssistant string) {
		// Extract handles its own timeout/error-degradation, so this won't
		// propagate failures; the only thing left to guard is the save loop.
		candidates := extractor.Extract(ctx, lastUserMsg, lastAssistant)
		saved := 0
		for _, m := range candidates {
			// Save assigns CreatedAt/UpdatedAt and persists with our Status
			// "pending" intact (Save only defaults empty Status to "active").
			if _, err := store.Save(m); err != nil {
				slog.Debug("auto-memory save failed", "name", m.Name, "err", err)
				continue
			}
			saved++
		}
		if saved > 0 {
			slog.Info("auto-memory captured", "count", saved, "from_turn", truncateForLog(lastUserMsg, 60))
		}
	}
}

// truncateForLog keeps a string to roughly n runes for a concise log line.
func truncateForLog(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func rememberPermissionConfigPath(workspaceRoot string) string {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot != "" {
		return filepath.Join(workspaceRoot, "momapeer.toml")
	}
	path := config.SourcePath()
	if path == "" {
		path = "momapeer.toml" // match Config.Save() fallback
	}
	return path
}

func coveredPermissionRule(rules []string, rule string) string {
	for _, existing := range rules {
		if permission.RuleCoversString(existing, rule) {
			return strings.TrimSpace(existing)
		}
	}
	return ""
}

func pruneCoveredPermissionRules(rules []string, rule string) []string {
	out := rules[:0]
	for _, existing := range rules {
		if strings.TrimSpace(existing) == "" || permission.RuleCoversString(rule, existing) {
			continue
		}
		out = append(out, existing)
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func subagentModelRef(cfg *config.Config, sk skill.Skill) string {
	if cfg != nil {
		for _, key := range subagentModelKeys(sk.Name) {
			if m := strings.TrimSpace(cfg.Agent.SubagentModels[key]); m != "" {
				return m
			}
		}
	}
	if m := strings.TrimSpace(sk.Model); m != "" {
		return m
	}
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Agent.SubagentModel)
}

func subagentEffortRef(cfg *config.Config, sk skill.Skill) string {
	if cfg != nil {
		for _, key := range subagentModelKeys(sk.Name) {
			if e := strings.TrimSpace(cfg.Agent.SubagentEfforts[key]); e != "" {
				return e
			}
		}
	}
	if e := strings.TrimSpace(sk.Effort); e != "" {
		return e
	}
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Agent.SubagentEffort)
}

func subagentModelKeys(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	keys := []string{name}
	for _, alias := range []string{
		strings.ReplaceAll(name, "-", "_"),
		strings.ReplaceAll(name, "_", "-"),
	} {
		if alias == "" {
			continue
		}
		seen := false
		for _, key := range keys {
			if key == alias {
				seen = true
				break
			}
		}
		if !seen {
			keys = append(keys, alias)
		}
	}
	return keys
}

func resolveWorkspaceRoot(explicit string) string {
	if explicit != "" {
		return explicit
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	if root, ok := nearestGitRoot(wd); ok {
		return root
	}
	return wd
}

func nearestGitRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		dir = filepath.Clean(start)
	}
	for {
		if isGitMarker(filepath.Join(dir, ".git")) {
			return dir, true
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", false
		}
		dir = next
	}
}

func isGitMarker(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && (fi.IsDir() || fi.Mode().IsRegular())
}

func newSubagentStore(sessionDir string) *agent.SubagentStore {
	sessionDir = strings.TrimSpace(sessionDir)
	if sessionDir == "" {
		return nil
	}
	return agent.NewSubagentStore(filepath.Join(sessionDir, "subagents"))
}

func subagentEffectiveIdentity(cfg *config.Config, baseModelRef string, base *config.ProviderEntry, modelRef, effort string) (string, string) {
	var entry config.ProviderEntry
	if base != nil {
		entry = *base
	}
	ref := strings.TrimSpace(modelRef)
	if ref == "" {
		ref = strings.TrimSpace(baseModelRef)
	}
	if cfg != nil && ref != "" {
		if resolved, ok := cfg.ResolveModel(ref); ok {
			entry = *resolved
		} else if strings.TrimSpace(modelRef) != "" {
			entry.Model = ref
		}
	} else if strings.TrimSpace(modelRef) != "" {
		entry.Model = strings.TrimSpace(modelRef)
	}
	if rawEffort := strings.TrimSpace(effort); rawEffort != "" {
		if normalized, err := config.NormalizeEffort(&entry, rawEffort); err == nil {
			entry.Effort = normalized
		} else {
			entry.Effort = rawEffort
		}
	}
	modelID := strings.TrimSpace(entry.Name)
	model := strings.TrimSpace(entry.Model)
	if modelID != "" && model != "" {
		modelID += "/" + model
	} else if model != "" {
		modelID = model
	} else if modelID == "" {
		modelID = ref
	}
	return modelID, strings.TrimSpace(config.EffectiveEffort(&entry))
}

// NewProvider builds a provider.Provider from a configured entry. Exported so
// custom assemblers (e.g. the ACP per-session factory) can reuse it without
// going through the full Build.
func NewProvider(e *config.ProviderEntry) (provider.Provider, error) {
	return NewProviderWithProxy(e, netclient.ProxySpec{Mode: netclient.ModeAuto})
}

// NewProviderWithProxy builds a provider.Provider with the configured ordinary
// network proxy settings, and wraps it with the global request-budget decorator
// (when [llm] rpm > 0) so it shares the per-API-key RPM quota.
func NewProviderWithProxy(e *config.ProviderEntry, proxy netclient.ProxySpec, opts ...bool) (provider.Provider, error) {
	imageUnderstand := false
	if len(opts) > 0 {
		imageUnderstand = opts[0]
	}
	p, err := provider.New(e.Kind, provider.Config{
		Name:    e.Name,
		BaseURL: e.BaseURL,
		Model:   e.Model,
		APIKey:  e.APIKey(),
		// Pass the key's env var so auth failures can name where to fix it, plus
		// provider-kind-specific knobs. EffectiveEffort applies a configured
		// default_effort when the user has not explicitly selected /effort.
		Extra: map[string]any{
			"api_key_env":              e.APIKeyEnv,
			"thinking":                 e.Thinking,
			"effort":                   config.EffectiveEffort(e),
			"reasoning_protocol":       config.ReasoningProtocolForEntry(e),
			"proxy_spec":               proxy,
			"vision":                   e.Vision,
			"vision_detail":            e.VisionDetail,
			"jiutian_image_understand": imageUnderstand,
		},
	})
	if err != nil {
		return nil, err
	}
	if globalBudget != nil && globalBudget.Status("").RPM > 0 {
		key := provider.BudgetKeyForConfig(e.Name, e.BaseURL, e.APIKey())
		return provider.NewRateLimitedProvider(p, globalBudget, key, buildingMainProvider), nil
	}
	return p, nil
}

// addBuiltins adds enabled built-in tools to reg. An empty list means all of
// them. writeRoots confines the file-writing built-ins to the workspace: after
// the (unconfined) defaults are added, each enabled writer is replaced by an
// instance bound to writeRoots (preserving registry order).
// When workDir is non-empty, tools resolve relative paths against it instead of
// the process cwd, enabling concurrent multi-project sessions.
func addBuiltins(reg *tool.Registry, enabled, writeRoots []string, bashSpec sandbox.Spec, bashTimeout time.Duration, searchSpec builtin.SearchSpec, stderr io.Writer, workDir string, proxySpec netclient.ProxySpec) {
	// If a workspace directory is set, use workspace-bound tools that resolve
	// paths relative to that directory. Otherwise fall back to the process-cwd
	// compile-time builtins.
	if workDir != "" {
		ws := builtin.Workspace{Dir: workDir, WriteRoots: writeRoots, Bash: bashSpec, BashTimeout: bashTimeout, Search: searchSpec, ProxySpec: proxySpec}
		for _, t := range ws.Tools(enabled...) {
			reg.Add(t)
		}
		return
	}

	if len(enabled) == 0 {
		for _, t := range tool.Builtins() {
			reg.Add(t)
		}
	} else {
		for _, name := range enabled {
			if t, ok := tool.LookupBuiltin(name); ok {
				reg.Add(t)
			} else {
				fmt.Fprintf(stderr, "warning: unknown built-in tool %q\n", name)
			}
		}
	}
	// Replace the unconfined defaults with confined instances (registry order is
	// preserved on replace): file-writers bound to the workspace, bash to the OS
	// sandbox, web_fetch to the proxy. Only replace tools actually enabled/present.
	confined := append(builtin.ConfineWriters(writeRoots), builtin.ConfineBash(bashSpec, bashTimeout), builtin.ConfineSearch(searchSpec), builtin.ConfineWebFetch(proxySpec))
	for _, t := range confined {
		if _, ok := reg.Get(t.Name()); ok {
			reg.Add(t)
		}
	}
}

// partitionByTier splits configured plugin entries into the three startup
// buckets — eager (block boot until ready), lazy (placeholder until first
// model use), background (placeholder + start spawn now). Entries with an
// empty tier land in background; unrecognised non-empty tiers land in lazy so a
// typo never triggers unexpected background work.
func partitionByTier(entries []config.PluginEntry) (eager, lazy, bg []config.PluginEntry) {
	for _, e := range entries {
		switch e.ResolvedTier() {
		case "eager":
			eager = append(eager, e)
		case "background":
			bg = append(bg, e)
		default:
			lazy = append(lazy, e)
		}
	}
	return eager, lazy, bg
}

// pluginSpecNames extracts the Name from each plugin.Spec. Used to mark
// session-scoped extra plugins as "reserved" so built-in entries don't
// duplicate them.
func pluginSpecNames(specs []plugin.Spec) []string {
	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.Name
	}
	return names
}

// applyProfileToSkillDisabled merges a profile's additive skill-disables into the
// config-wide disabled list, returning a new slice. Whitelist enforcement (the
// profile's EnabledSkills) is handled separately in Build via profileSkillWhitelist
// because it needs the full discovered skill set. The result preserves first
// spelling and dedupes by SkillNameKey, matching cfg.DisabledSkillNames semantics.
func applyProfileToSkillDisabled(p *config.Profile, configDisabled []string) []string {
	if p == nil || len(p.DisabledSkills) == 0 {
		return configDisabled
	}
	seen := make(map[string]bool, len(configDisabled)+len(p.DisabledSkills))
	out := make([]string, 0, len(configDisabled)+len(p.DisabledSkills))
	for _, name := range append(append([]string{}, configDisabled...), p.DisabledSkills...) {
		name = strings.TrimSpace(name)
		if !config.IsValidSkillName(name) {
			continue
		}
		key := config.SkillNameKey(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

// profileSkillWhitelist returns the profile's EnabledSkills as a SkillNameKey set,
// or nil if the profile defines no whitelist (meaning "all skills allowed").
func profileSkillWhitelist(p *config.Profile) map[string]bool {
	if p == nil || len(p.EnabledSkills) == 0 {
		return nil
	}
	out := make(map[string]bool, len(p.EnabledSkills))
	for _, n := range p.EnabledSkills {
		if k := config.SkillNameKey(n); k != "" {
			out[k] = true
		}
	}
	return out
}

// isSkillDisabledByName reports whether name appears in a DisabledNames slice
// (case-insensitive via SkillNameKey). Used to keep the skills index in sync with
// a profile's additive disables without going through cfg.IsSkillDisabled (which
// only knows the config-level set).
func isSkillDisabledByName(disabled []string, name string) bool {
	key := config.SkillNameKey(name)
	if key == "" {
		return false
	}
	for _, d := range disabled {
		if config.SkillNameKey(d) == key {
			return true
		}
	}
	return false
}

// PluginSpecs maps configured plugin entries to plugin.Spec, expanding ${VAR}
// references. Exported so custom assemblers can connect the config's plugins
// alongside their own (e.g. ACP's per-session MCP servers).
func PluginSpecs(entries []config.PluginEntry) []plugin.Spec {
	specs := make([]plugin.Spec, len(entries))
	for i, e := range entries {
		e = e.ExpandedPlugin() // resolve ${VAR} / ${VAR:-default} from the environment
		specs[i] = plugin.Spec{
			Name:        e.Name,
			Type:        e.Type,
			Command:     e.Command,
			Args:        e.Args,
			Env:         e.Env,
			URL:         e.URL,
			Headers:     e.Headers,
			CallTimeout: parseDuration(e.CallTimeout),
		}
	}
	return specs
}

// parseDuration parses a Go duration string, returning 0 for empty/invalid.
func parseDuration(s string) time.Duration {
	d, _ := time.ParseDuration(strings.TrimSpace(s))
	return d
}

// MCPStartupNotice formats the warning shown when configured MCP servers failed
// to connect, naming the first few; ok is false when none failed.
func MCPStartupNotice(failures []plugin.Failure) (text string, ok bool) {
	if len(failures) == 0 {
		return "", false
	}
	names := make([]string, 0, min(len(failures), 3))
	for i, f := range failures {
		if i >= 3 {
			break
		}
		names = append(names, f.Name)
	}
	more := ""
	if len(failures) > len(names) {
		more = fmt.Sprintf(" (+%d more)", len(failures)-len(names))
	}
	return fmt.Sprintf("%d MCP server(s) failed to start: %s%s — run /mcp for details",
		len(failures), strings.Join(names, ", "), more), true
}

// LSPSpecs returns the language → server map: the built-in defaults overlaid with
// any user overrides. A user entry may set only the fields it wants to change;
// empty fields keep the default for that language.
func LSPSpecs(cfg config.LSPConfig) map[string]lsp.ServerSpec {
	specs := lsp.DefaultSpecs()
	for lang, s := range cfg.Servers {
		spec := specs[lang]
		if s.Command != "" {
			spec.Command = s.Command
		}
		if s.Args != nil {
			spec.Args = s.Args
		}
		if s.Env != nil {
			spec.Env = s.Env
		}
		if s.LanguageID != "" {
			spec.LanguageID = s.LanguageID
		}
		if s.Extensions != nil {
			spec.Extensions = s.Extensions
		}
		if s.InstallHint != "" {
			spec.InstallHint = s.InstallHint
		}
		if spec.LanguageID == "" {
			spec.LanguageID = lang
		}
		specs[lang] = spec
	}
	return specs
}

func providerNames(cfg *config.Config) string {
	names := make([]string, len(cfg.Providers))
	for i, p := range cfg.Providers {
		names[i] = p.Name
	}
	return strings.Join(names, "/")
}

// providerChatFunc wraps a provider.Provider into a simple synchronous chat
// function suitable for lightweight one-shot prompts (conflict detection,
// compression, etc.). It sends the prompt as a single user message, streams
// the response, and returns the accumulated text.
func providerChatFunc(prov provider.Provider) func(ctx context.Context, prompt string) (string, error) {
	if prov == nil {
		return nil
	}
	return func(ctx context.Context, prompt string) (string, error) {
		req := provider.Request{
			Messages: []provider.Message{
				{Role: provider.RoleUser, Content: prompt},
			},
			MaxTokens: 256,
		}
		ch, err := prov.Stream(ctx, req)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		for chunk := range ch {
			if chunk.Err != nil {
				return sb.String(), chunk.Err
			}
			if chunk.Type == provider.ChunkText {
				sb.WriteString(chunk.Text)
			}
		}
		return sb.String(), nil
	}
}
