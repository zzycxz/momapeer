package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/zzycxz/momapeer/internal/agent"
	"github.com/zzycxz/momapeer/internal/boot"
	"github.com/zzycxz/momapeer/internal/bot"
	"github.com/zzycxz/momapeer/internal/browseruse"
	"github.com/zzycxz/momapeer/internal/builtinmcp"
	calendarpkg "github.com/zzycxz/momapeer/internal/calendar"
	"github.com/zzycxz/momapeer/internal/config"
	"github.com/zzycxz/momapeer/internal/control"
	"github.com/zzycxz/momapeer/internal/event"
	expertspkg "github.com/zzycxz/momapeer/internal/experts"
	"github.com/zzycxz/momapeer/internal/fileref"
	fileenc "github.com/zzycxz/momapeer/internal/fileutil/encoding"
	"github.com/zzycxz/momapeer/internal/i18n"
	"github.com/zzycxz/momapeer/internal/mcpdiag"
	"github.com/zzycxz/momapeer/internal/memory"
	"github.com/zzycxz/momapeer/internal/plugin"
	"github.com/zzycxz/momapeer/internal/provider"
	ragpkg "github.com/zzycxz/momapeer/internal/rag"
	schedulerpkg "github.com/zzycxz/momapeer/internal/scheduler"
	"github.com/zzycxz/momapeer/internal/skill"
	"github.com/zzycxz/momapeer/internal/tool/builtin"
)

// eventChannel is the Wails runtime event name the frontend subscribes to for the
// agent's typed event stream. One channel carries every event kind; the payload's
// `kind` field discriminates — the desktop analogue of the serve transport's SSE
// `data:` frames.
const eventChannel = "agent:event"

// singleInstanceID is used by Wails to route a second desktop launch back to the
// running instance. Keep it stable across releases so launcher/Dock/taskbar
// reopen behavior remains predictable on every platform.
const singleInstanceID = "com.momapeer.desktop"

// App is the Wails-bound application object: the desktop frontend's command
// surface. Its exported methods (Submit/Cancel/Approve/…) are generated into JS
// bindings. The app manages multiple WorkspaceTabs — each with its own controller
// scoped to a project workspace — and routes commands to the active tab. Events
// flow the other way: each tab's controller emits to a tabEventSink that
// forwards events tagged with tabId to the webview via runtime.EventsEmit.
type App struct {
	ctx context.Context

	// mu protects the tab map, tabOrder, activeTabID, and per-tab fields that are read
	// from bound methods. All bound methods that touch a controller use activeCtrl().
	mu          sync.RWMutex
	tabs        map[string]*WorkspaceTab
	tabOrder    []string
	activeTabID string
	readyHook   func()

	// metrics holds the opt-in aggregate agent-metrics aggregator (non-nil only
	// when desktop.metrics is enabled in config). Swapped live by
	// SetDesktopMetrics so the toggle takes effect without a restart.
	metrics atomic.Pointer[metricsAggregator]

	forceQuit           atomic.Bool
	backgroundMaximised atomic.Bool
	trayReady           bool
	tray                *desktopTray

	mediaTokens *mediaTokenStore
	botInstalls map[string]*botInstallSession
	botGW       atomic.Pointer[bot.BotGateway] // nil when bot is disabled or not started; atomic for lock-free reads from Push/hotkey
	hotkeyMgr   *hotkeyManager                 // screenshot hotkey manager; nil when feature off/stopped
	estopMgr    *estopManager                  // emergency-stop hotkey manager; nil when feature off/stopped
	// knownRemoteIDs 记录已回写过 SessionMappings 的远端 ID（"provider:remoteID"），
	// 避免同一用户每发一条消息就 read-modify-write 整个 config 文件。goroutine 启
	// 动时从已有 SessionMappings 预热，之后每条消息 LoadOrStore 命中即跳过。
	knownRemoteIDs sync.Map

	// sharedHosts shares one plugin.Host per workspace root across desktop tabs
	// so opening N tabs on the same project spawns MCP subprocesses (CodeGraph,
	// etc.) once, not N times. See desktop/shared_host.go.
	sharedHosts   map[string]*sharedPluginHost
	sharedHostsMu sync.Mutex

	// scheduler is the app-level scheduled-task engine (coWork). Created once at
	// startup; bound to the active cowork controller via schedulerRunner so
	// scheduled prompts fire into whichever tab is active. Persists tasks to
	// ~/.config/momapeer/scheduled_tasks.json across restarts.
	scheduler *schedulerpkg.Scheduler
	// calendarStore is the cowork calendar store (SQLite). Created once at
	// startup; persists to ~/.config/momapeer/calendar.db.
	calendarStore  *calendarpkg.Store
	calendarRemind *calendarpkg.ReminderEngine

	// ragStore is the cowork knowledge-base store (FTS5 + structured entities).
	// ragPipeline is the background deep-extraction engine (chunks → LLM →
	// entity/relation graph). Both persist to the user config dir.
	ragStore    *ragpkg.Store
	ragPipeline *ragpkg.Pipeline
	ragSession  *ragpkg.SessionRAGContext
	// ragExtractor holds the configured extraction model (jiutianExtractor, or
	// nil when no extract model is set). Kept on the App so boot.RebindRAGBudget
	// can re-inject the global RPM budget after each boot.Build — without it,
	// a runtime RPM change (settings rebuild) wouldn't reach RAG extraction
	// until an app restart, and extraction would stay on a stale/disabled budget.
	ragExtractor ragpkg.Extractor
	// heService manages the Hyper-Extract Python server lifecycle.
	heService *HEService
	// buService manages the browser-use Python sidecar (autonomous browsing).
	buService *BrowserUseService
	// expertStore + expertOrchestrator power the 专家团 (expert-team) panel:
	// multi-model collaboration with persistent team rosters.
	expertStore        *expertspkg.Store
	expertOrchestrator *expertspkg.Orchestrator
	// expertRuns tracks in-flight expert-team runs keyed by teamID, so a panel
	// remounted after the CoWorkLayout was torn down (tab/profile switch) can
	// query whether a run is still going and re-subscribe to its stream. The
	// backend goroutine runs independent of the frontend, so this survives
	// unmounting. Guarded by expertRunsMu.
	expertRuns   map[string]*expertRunState
	expertRunsMu sync.Mutex
	// screenshotHwnd is the hidden message-only window receiving WM_HOTKEY for
	// the global screenshot hotkey. 0 when the feature is off.
	screenshotHwnd uintptr
	// estopHwnd is the hidden message-only window receiving WM_HOTKEY for the
	// global emergency-stop hotkey. 0 when the feature is off.
	estopHwnd uintptr
}

// mediaTokenEntry holds metadata for a workspace media file served via temporary URL.
type mediaTokenEntry struct {
	absPath   string
	filename  string
	mime      string
	kind      string
	size      int64
	modTime   time.Time
	createdAt time.Time
	expiresAt time.Time
}

// mediaTokenStore manages temporary tokens that grant access to workspace files
// through the AssetServer middleware. Tokens expire after a fixed TTL and are
// capped at a maximum count; creating a new token evicts the oldest entry when
// the store is full.
type mediaTokenStore struct {
	mu    sync.Mutex
	byTok map[string]*mediaTokenEntry
	order []string // oldest first
	maxN  int
	ttl   time.Duration
}

const mediaTokenMax = 256

func newMediaTokenStore() *mediaTokenStore {
	return &mediaTokenStore{
		byTok: map[string]*mediaTokenEntry{},
		maxN:  mediaTokenMax,
		ttl:   10 * time.Minute,
	}
}

func (s *mediaTokenStore) cleanupLocked() {
	now := time.Now()
	for len(s.order) > 0 {
		tok := s.order[0]
		e := s.byTok[tok]
		if e == nil {
			s.order = s.order[1:]
			continue
		}
		if !now.Before(e.expiresAt) {
			delete(s.byTok, tok)
			s.order = s.order[1:]
			continue
		}
		break
	}
	for len(s.order) > s.maxN {
		oldest := s.order[0]
		delete(s.byTok, oldest)
		s.order = s.order[1:]
	}
}

func (s *mediaTokenStore) create(absPath, filename, mime, kind string, size int64, modTime time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked()

	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	token := hex.EncodeToString(tok)

	now := time.Now()
	s.byTok[token] = &mediaTokenEntry{
		absPath:   absPath,
		filename:  filename,
		mime:      mime,
		kind:      kind,
		size:      size,
		modTime:   modTime,
		createdAt: now,
		expiresAt: now.Add(s.ttl),
	}
	s.order = append(s.order, token)

	// Trim oldest if the new token pushed us over the limit.
	for len(s.order) > s.maxN {
		oldest := s.order[0]
		delete(s.byTok, oldest)
		s.order = s.order[1:]
	}

	return token
}

func (s *mediaTokenStore) get(token string) *mediaTokenEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.byTok[token]
	if e == nil {
		return nil
	}
	if time.Now().After(e.expiresAt) {
		delete(s.byTok, token)
		return nil
	}
	return e
}

func (a *App) ensureMediaTokenStore() *mediaTokenStore {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mediaTokens == nil {
		a.mediaTokens = newMediaTokenStore()
	}
	return a.mediaTokens
}

// workspaceMediaMiddleware returns an HTTP middleware that intercepts
// /__momapeer_workspace_media/{token}/{filename} requests and serves the
// corresponding workspace file. All other paths pass through to the Wails
// default asset handler unchanged.
func (a *App) workspaceMediaMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			prefix := "/__momapeer_workspace_media/"
			if !strings.HasPrefix(r.URL.Path, prefix) {
				next.ServeHTTP(w, r)
				return
			}

			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			rest := strings.TrimPrefix(r.URL.Path, prefix)
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) == 0 || parts[0] == "" {
				http.NotFound(w, r)
				return
			}
			token := parts[0]

			entry := a.ensureMediaTokenStore().get(token)
			if entry == nil {
				http.NotFound(w, r)
				return
			}

			f, err := os.Open(entry.absPath)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()

			w.Header().Set("Content-Type", entry.mime)
			w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": entry.filename}))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "private, max-age=600")
			http.ServeContent(w, r, entry.filename, entry.modTime, f)
		})
	}
}

// NewApp constructs the bound object. Tabs are restored in startup from the
// last session's desktop-tabs.json.
func NewApp() *App {
	return &App{tabs: map[string]*WorkspaceTab{}, mediaTokens: newMediaTokenStore(), botInstalls: map[string]*botInstallSession{}, expertRuns: map[string]*expertRunState{}}
}

func (a *App) bootContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// Platform exposes the native OS to the frontend so chrome/layout affordances can
// stay platform-scoped instead of relying on browser user-agent guesses.
func (a *App) Platform() string {
	return goruntime.GOOS
}

// startup runs once the webview process is up, before the frontend can issue any
// bound call. It captures the Wails context (needed for EventsEmit), then kicks
// off the initialization in a background goroutine so the webview loads immediately.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Route slog to a file in the config dir so diagnostic logs (mail save,
	// probe, panic traces) are visible in a packaged GUI build, where stdout/
	// stderr are not attached. Truncates on each launch to avoid unbounded
	// growth; rotation isn't needed for short debug sessions.
	if cfgDir := desktopConfigDir(); cfgDir != "" {
		if err := os.MkdirAll(cfgDir, 0o755); err == nil {
			if f, err := os.OpenFile(filepath.Join(cfgDir, "app.log"),
				os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
				slog.SetDefault(slog.New(slog.NewTextHandler(f, nil)))
			}
		}
	}
	// Relocate legacy un-profiled session/topic data into the "dev" partition
	// BEFORE tabs are restored, so the sidebar lists sessions at their new
	// paths. Idempotent (guarded by a marker) and best-effort: failures are
	// logged inside, never crashing startup.
	migrateToProfilePartition()
	// Prune ghost projects left by the migration's workspace union: a project
	// copied into BOTH profile files that has no topics/sessions in one of them
	// should not appear in that profile's sidebar. Runs every startup (idempotent)
	// so already-migrated disks are healed too.
	pruneGhostProjects()
	installSystemQuitHook()
	a.startTray()

	// Initialize OS-level notifications so scheduled-task reminders (and other
	// notify-mode output) surface as Windows toast — visible even when the window
	// is minimized to tray / in the background, and persisted to Action Center.
	// Best-effort: a failure only logs and continues (the in-app toast path is
	// the fallback). Registering a response callback brings the window to front
	// when the user clicks the toast.
	if err := runtime.InitializeNotifications(ctx); err != nil {
		slog.Warn("notifications: init failed; reminders will only show in-app", "err", err)
	} else {
		runtime.OnNotificationResponse(ctx, func(_ runtime.NotificationResult) {
			go a.showMainWindow()
		})
	}

	// 启动内嵌 bot gateway
	if cfg, err := config.Load(); err == nil && cfg.Bot.Enabled {
		a.startBotGateway(cfg)
	}

	go a.restoreOrBuildTabs()
	go a.sendStartupPing()
	// Load coWork secrets (SMTP/IMAP passwords) from the momapeer-managed .env
	// into the process environment BEFORE initScheduler/initRAG, so coWork tools
	// find them via os.Getenv without the user setting system env vars manually.
	loadCoworkEnvAtStartup()
	a.initScheduler()
	a.initCalendar()
	a.initRAG()
	a.initExperts()
	a.StartScreenshotHotkey()
	// Start the emergency-stop hotkey AFTER the screenshot hotkey so both
	// global combos are registered before the app reports ready. E-stop is the
	// safety baseline for screen_* tools; registering it unconditionally (rather
	// than only when a turn is running) keeps the kill switch always-available.
	a.StartEStopHotkey()
}

// initRAG opens the coWork knowledge-base store (FTS5 + structured entities)
// and binds it to the rag_* tools. It also constructs the deep-extraction
// pipeline (chunks → LLM → entity/relation graph) from the loaded config — the
// pipeline's worker goroutines start here and run for the app's lifetime.
// Safe to call once at startup; a failure to open logs a warning but doesn't
// block boot (rag_* tools will report "offline").
func (a *App) initRAG() {
	dbPath := filepath.Join(desktopConfigDir(), "rag.db")
	store, err := ragpkg.Open(dbPath)
	if err != nil {
		slog.Warn("rag: open failed", "err", err)
		return
	}
	a.ragStore = store
	a.ragSession = ragpkg.NewSessionRAGContext()
	builtin.SetRAGStore(store)
	// Bridge the desktop UI's "activate collection" control to the agent's
	// rag_search calls: when the user narrows the session to one collection,
	// LLM-driven rag_search (with no explicit collection arg) auto-scopes to it.
	builtin.SetRAGSessionResolver(func() []string {
		if a.ragSession == nil {
			return nil
		}
		return a.ragSession.GetActiveCollections()
	})

	// Build the extraction pipeline from [cowork] extract_* settings (with
	// conservative defaults: 1 concurrent chunk, 3s between chunks). When no
	// extract model is configured the pipeline uses a noop extractor — FTS5
	// still works, but "deep extract" is a no-op until the user sets a model.
	cfg := ragpkg.DefaultPipelineConfig()
	var extractor ragpkg.Extractor
	if c, err := config.Load(); err == nil {
		if c.Cowork.ExtractInterval != "" {
			if d, err := time.ParseDuration(c.Cowork.ExtractInterval); err == nil && d > 0 {
				cfg.Interval = d
			}
		}
		if c.Cowork.ExtractConcurrency > 0 {
			cfg.Concurrency = c.Cowork.ExtractConcurrency
		}
		// Resolve which model the RAG extractor uses, in priority order:
		//   1. [cowork] extract_model — explicit override (rarely needed);
		//   2. [agent] fast_task_model — the "迅捷任务模型" from Settings → Model,
		//      same one dream/distill use (designed for fast background work);
		//   3. default_model — the main chat model.
		// All three go through config.ResolveModel, which yields a ProviderEntry
		// with the right base_url + api_key_env + bare model name — so the
		// extractor always hits the correct endpoint with the correct key, no
		// hardcoding. This mirrors exactly how the main agent and subagents
		// resolve their models.
		modelRef := strings.TrimSpace(c.Cowork.ExtractModel)
		if modelRef == "" {
			modelRef = strings.TrimSpace(c.Agent.FastTaskModel)
		}
		if modelRef == "" {
			modelRef = strings.TrimSpace(c.DefaultModel)
		}
		if modelRef != "" {
			extCfg := ragpkg.JiutianExtractorConfig{TwoStage: true}
			if e, ok := c.ResolveModel(modelRef); ok {
				// ResolveModel gives us the concrete provider (base_url, api_key_env)
				// + bare model name. This is the same resolution the main agent uses.
				extCfg.BaseURL = e.BaseURL
				extCfg.APIKey = e.APIKeyEnv
				extCfg.Model = e.Model
				slog.Info("rag: extraction model resolved", "ref", modelRef, "model", e.Model, "provider", e.Name)
			} else {
				// Fallback: send the ref as-is to the default 九天 endpoint.
				extCfg.Model = modelRef
				slog.Warn("rag: could not resolve model ref, using as-is", "ref", modelRef)
			}
			extractor = ragpkg.NewJiutianExtractor(extCfg)
		}
	}
	if extractor == nil {
		slog.Warn("rag: extract model not configured — deep extraction disabled (FTS5 still works)")
	} else {
		// Stash the extractor so the global RPM budget can be wired into it once
		// boot.Build has initialized globalBudget (it runs later, in
		// restoreOrBuildTabs). boot.RebindRAGBudget — called from the first
		// successful Build and again on every settings rebuild — injects the
		// shared budget so RAG extraction draws from the same per-minute quota
		// as the main conversation, instead of a separate local budget that
		// could exceed the configured RPM. Extraction runs at background
		// priority (reserve_main protects interactive requests).
		a.ragExtractor = extractor
	}
	a.ragPipeline = ragpkg.NewPipeline(store, extractor, cfg, func(ev ragpkg.ProgressEvent) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "rag:progress", ev)
		}
	})
	a.ragPipeline.SetLogger(func(format string, args ...any) {
		slog.Debug("rag: "+format, args...)
	})
	a.ragPipeline.Start()
	// Rehydrate any extraction jobs interrupted by a prior shutdown. Pending/
	// extracting jobs are re-enqueued from their FTS5 chunk text so restarts no
	// longer silently drop in-flight work.
	if n := a.ragPipeline.Resume(); n > 0 {
		slog.Info("rag: resumed interrupted extraction", "chunks", n)
	}

	// Start Hyper-Extract Python server (optional — failure is non-fatal).
	// The port defaults to 18900 but can be overridden via [cowork] he_port.
	scriptPath := FindScript()
	if scriptPath != "" {
		hePort := 0
		if c, err := config.Load(); err == nil && c.Cowork.HEPort > 0 {
			hePort = c.Cowork.HEPort
		}
		a.heService = NewHEService("", scriptPath, hePort)
		if err := a.heService.Start(); err != nil {
			slog.Warn("Hyper-Extract service not started", "err", err)
			a.heService = nil
		}
	} else {
		slog.Info("Hyper-Extract script not found — HE extraction unavailable")
	}

	// Start the browser-use autonomous-browsing sidecar (optional — failure is
	// non-fatal). Disabled by config ([cowork] browser_use_enabled=false) or if
	// the script is absent. The sidecar is lazy in effect: even when started,
	// it only does work when browser_auto posts a /run.
	if cfgCowork, cerr := config.Load(); cerr == nil && cfgCowork.Cowork.BrowserUseEnabled {
		buScript := FindBrowserUseScript()
		if buScript != "" {
			a.buService = NewBrowserUseService(cfgCowork.Cowork.BrowserUsePython, buScript, cfgCowork.Cowork.BrowserUsePort)
			if err := a.buService.Start(); err != nil {
				slog.Warn("browser-use sidecar not started", "err", err)
				a.buService = nil
			}
		} else {
			slog.Info("browser-use script not found — autonomous browsing unavailable (browseruse_server.py)")
		}
	}

	// Wire the browser_auto runtime hooks into boot. The client provider is
	// resolved lazily (returns nil until the sidecar is ready), and the panel
	// sink attaches a screencast to the in-app browser view. These are set once
	// at startup; boot.Build's injected runtime reads them at tool-exec time.
	boot.SetBrowserUseClientProvider(func() *browseruse.Client {
		if a.buService == nil || !a.buService.IsReady() {
			return nil
		}
		return a.buService.Client()
	})

	// Try to wire the global RPM budget into the extractor now. restoreOrBuildTabs
	// runs concurrently (started earlier as a goroutine), so globalBudget may or
	// may not be ready yet; RebindRAGBudget is nil-safe and idempotent. If the
	// first Build hasn't finished, buildTabController rebinds again once it does.
	// This call covers the race where Build already completed before initRAG set
	// up a.ragExtractor (otherwise the extractor would never get the budget).
	rebindCfg, _ := config.Load()
	boot.RebindRAGBudget(a.ragExtractor, rebindCfg)
}

// initScheduler creates the app-level scheduled-task engine, loads persisted
// tasks, binds the runner bridge to the active cowork controller, and starts
// firing. The scheduler lives for the app's lifetime; a scheduled prompt runs in
// whichever tab is active (cowork profile). Safe to call once at startup.
func (a *App) initScheduler() {
	storePath := filepath.Join(desktopConfigDir(), "scheduled_tasks.json")
	a.scheduler = schedulerpkg.New(storePath)
	a.scheduler.SetLogger(func(format string, args ...any) {
		slog.Debug("scheduler: "+format, args...)
	})
	if err := a.scheduler.Load(); err != nil {
		slog.Warn("scheduler: load failed", "err", err)
	}
	a.scheduler.SetRunner(schedulerRunner{app: a})
	a.scheduler.SetIMPusher(schedulerIMPusher{app: a})
	a.scheduler.SetEmailSender(schedulerEmailSender{})
	a.scheduler.SetNotifier(schedulerNotifier{app: a})
	a.scheduler.Start()
	builtin.SetScheduler(a.scheduler)
	builtin.SetAuthNotifier(authNotifier{app: a})
	a.scheduler.SetAccountProber(imapProber{})

	// Surface any one-shot reminders that were due while the app was down. Load
	// captured them; we drain now (after the notifier + OS-toast init are bound)
	// and fire a catch-up notification for each, so a reminder the user set isn't
	// silently lost just because momapeer wasn't running at the fire instant.
	// Delayed briefly so the OS notification registration (InitializeNotifications
	// in startup) has settled before we send.
	if missed := a.scheduler.DrainMissedReminders(); len(missed) > 0 {
		go func() {
			time.Sleep(2 * time.Second)
			for _, m := range missed {
				(schedulerNotifier{app: a}).Notify("⏰ 错过的提醒："+m.Name, m.Body)
			}
		}()
	}
}

// schedulerIMPusher implements scheduler.IMPusher by routing through the bot
// gateway. The gateway is bound lazily (a.botGW is set when the bot starts,
// which may be after the scheduler init), so we read it at push time. When the
// bot isn't running, Push returns nil (best-effort — a scheduled task shouldn't
// fail because IM is offline).
// authNotifier implements builtin.AuthNotifier by emitting a Wails event.
type authNotifier struct{ app *App }

func (n authNotifier) NotifyAuthExpired(account string) {
	if n.app.ctx != nil {
		runtime.EventsEmit(n.app.ctx, "auth:expired", account)
	}
}

// imapProber implements scheduler.AccountProber by delegating to the real
// IMAP probe (builtin.ProbeAccountIMAP), which resolves the named account to
// its configured IMAP credentials and verifies connect+login without fetching
// mail. Returns nil for send-only accounts (no IMAP host) so they don't block.
type imapProber struct{}

func (imapProber) Probe(account string) error {
	return builtin.ProbeAccountIMAP(account)
}

// ExpertSessionMeta holds metadata for an expert collaboration session.
type ExpertSessionMeta struct {
	TeamID   string `json:"teamId"`
	TeamName string `json:"teamName"`
}

type schedulerIMPusher struct{ app *App }

func (p schedulerIMPusher) Push(ctx context.Context, dest, text string) error {
	gw := p.app.botGW.Load()
	if gw == nil {
		// Signal "offline" so the scheduler records a skipped delivery with a
		// clear reason, rather than silently reporting success. The user then
		// sees "bot 未启动" and knows to start the bot.
		return schedulerpkg.ErrIMOffline
	}
	return gw.Push(ctx, dest, text)
}

// schedulerRunner implements scheduler.Runner by running a prompt in the active
// cowork tab's controller. A scheduled task fires headlessly into the agent
// loop; the result text (or error) is returned to the scheduler, which stores it
// on the task for schedule_list inspection. If no cowork tab is active, the run
// is skipped with a clear message.
type schedulerRunner struct{ app *App }

func (r schedulerRunner) Run(ctx context.Context, profile, prompt string) (string, error) {
	r.app.mu.RLock()
	tab := r.app.tabs[r.app.activeTabID]
	r.app.mu.RUnlock()
	if tab != nil && tab.Ctrl != nil && tab.Ready {
		return runScheduledPrompt(ctx, tab.Ctrl, prompt)
	}
	// No usable active tab — previously this returned an error and the scheduled
	// task silently failed (scheduler.runOne records the error but does not
	// retry, so the prompt was lost). The scheduler package comment explicitly
	// promises tasks "must fire even when no chat tab is open"; build a temporary
	// headless cowork controller so the task still runs. See audit finding C7.
	//
	// Headless mode is STRICTER than interactive: boot installs a headless
	// permission gate (deny-by-default for unconfigured writers), so email_send
	// and other irreversible ops fail closed (no UI to ask) rather than silently
	// proceeding. The A1/A2/A3 policy hardening we just landed applies here too.
	return r.app.runHeadlessScheduled(ctx, profile, prompt)
}

// runScheduledPrompt runs a prompt on the given controller and returns a short
// summary of the result (the last assistant message text), shared by the
// active-tab and headless paths.
func runScheduledPrompt(ctx context.Context, ctrl *control.Controller, prompt string) (string, error) {
	if err := ctrl.Run(ctx, prompt); err != nil {
		return "", err
	}
	hist := ctrl.History()
	if len(hist) == 0 {
		return "ran (no output)", nil
	}
	last := hist[len(hist)-1]
	if txt := assistantText(last); txt != "" {
		return txt, nil
	}
	return "ran", nil
}

// runHeadlessScheduled builds a throwaway headless cowork controller, runs the
// prompt, then tears the controller down (releasing the shared plugin host).
// WorkspaceRoot is the global cowork root (~/.momapeer) since scheduled tasks
// are not bound to a specific project. See C7.
func (a *App) runHeadlessScheduled(ctx context.Context, profileName, prompt string) (string, error) {
	root := globalTabWorkspaceRoot()
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return "", fmt.Errorf("scheduled task: load config: %w", err)
	}
	// Resolve the product profile (default cowork for scheduled tasks). Empty
	// or "dev" yields a nil profile → unprofiled coding behaviour; any other
	// name resolves against config + builtins, mirroring tabs.go.
	var profile *config.Profile
	name := strings.TrimSpace(profileName)
	if name == "" {
		name = config.ProfileCowork
	}
	if !strings.EqualFold(name, config.ProfileDev) {
		if p, perr := cfg.ResolveProfile(name); perr == nil {
			profile = p
		}
	}
	sharedHost := a.acquireSharedHost(root)
	ctrl, err := boot.Build(a.bootContext(), boot.Options{
		Model:         "", // config default_model
		RequireKey:    false,
		WorkspaceRoot: root,
		Host:          sharedHost,
		Profile:       profile,
	})
	if err != nil {
		a.releaseSharedHost(root) // Build failed: drop the acquire
		return "", fmt.Errorf("scheduled task: build headless controller: %w", err)
	}
	// Headless gate is the boot default (no EnableInteractiveApproval), so writes
	// and irreversible ops resolve via config policy — fail-closed for anything
	// not explicitly allowed. Do NOT enable plan/goal (no UI to exit them).
	defer func() {
		ctrl.Close()
		a.releaseSharedHost(root)
	}()
	return runScheduledPrompt(ctx, ctrl, prompt)
}

// assistantText extracts the text content of an assistant message (empty for
// non-assistant or tool-call-only messages). Content is typed as any (string or
// structured parts); we coerce the common string case. Used by the scheduler to
// summarize a fired prompt's result.
func assistantText(m provider.Message) string {
	if m.Role != provider.RoleAssistant {
		return ""
	}
	if s, ok := m.Content.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func (a *App) beforeClose(ctx context.Context) bool {
	if a.forceQuit.Swap(false) || consumeSystemQuitRequested() {
		return false
	}
	cfg, _, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		cfg = config.LoadForEdit(config.UserConfigPath())
	}
	if cfg.DesktopCloseBehavior() == "background" {
		a.backgroundMaximised.Store(runtime.WindowIsMaximised(ctx))
		a.saveWindowStateSync()
		a.snapshotAllTabs()
		hideForBackground(ctx)
		return true
	}
	return false
}

func (a *App) showMainWindow() {
	if a.ctx != nil {
		showFromBackground(a.ctx, a.backgroundMaximised.Swap(false))
	}
}

func (a *App) secondInstanceLaunch() {
	a.showMainWindow()
}

func (a *App) quitApp() {
	if a.ctx == nil {
		return
	}
	a.forceQuit.Store(true)
	runtime.Quit(a.ctx)
}

func hideForBackground(ctx context.Context) {
	if backgroundCloseUsesApplicationHide(goruntime.GOOS) {
		runtime.Hide(ctx)
		return
	}
	runtime.WindowHide(ctx)
}

func showFromBackground(ctx context.Context, wasMaximised bool) {
	if backgroundCloseUsesApplicationHide(goruntime.GOOS) {
		runtime.Show(ctx)
	}
	plan := backgroundRestorePlanFor(goruntime.GOOS, wasMaximised)
	if plan.maximiseBeforeShow {
		runtime.WindowMaximise(ctx)
	}
	runtime.WindowShow(ctx)
	if plan.unminimiseAfterShow {
		runtime.WindowUnminimise(ctx)
	}
}

func backgroundCloseUsesApplicationHide(goos string) bool {
	return goos == "darwin"
}

type backgroundRestorePlan struct {
	maximiseBeforeShow  bool
	unminimiseAfterShow bool
}

func backgroundRestorePlanFor(goos string, wasMaximised bool) backgroundRestorePlan {
	if backgroundRestoreShouldMaximise(goos, wasMaximised) {
		return backgroundRestorePlan{maximiseBeforeShow: true}
	}
	return backgroundRestorePlan{unminimiseAfterShow: true}
}

func backgroundRestoreShouldMaximise(goos string, wasMaximised bool) bool {
	return wasMaximised && !backgroundCloseUsesApplicationHide(goos)
}

// restoreOrBuildTabs restores the tabs from the last session, or creates a
// default Global tab on first launch.
func (a *App) restoreOrBuildTabs() {
	ctx := a.ctx
	ensureWorkspace()

	// Load i18n from the first available config.
	// Prefer DesktopLanguage (desktop UI setting) over Language (CLI setting),
	// so the user's language choice in desktop settings takes effect.
	if cfg, err := config.Load(); err == nil {
		lang := cfg.DesktopLanguage()
		if lang == "" {
			lang = cfg.Language
		}
		i18n.DetectLanguage(lang)
	}

	f := loadTabsFile()
	if len(f.Tabs) > 0 {
		toBuild := make([]*WorkspaceTab, 0, len(f.Tabs))
		for _, entry := range f.Tabs {
			a.mu.Lock()
			id := a.restoredTabIDLocked(entry.ID)
			a.mu.Unlock()

			var tab *WorkspaceTab
			if entry.IsExpertSession {
				// Expert-session tabs are their own scope; restore verbatim.
				tab = a.createTabEntryWithID("expert", "", entry.Profile, "", id)
			} else if entry.Scope == "project" {
				tab = a.createTabEntryWithID(entry.Scope, entry.WorkspaceRoot, entry.Profile, entry.TopicID, id)
			} else {
				tab = a.createTabEntryWithID("global", globalTabWorkspaceRoot(), entry.Profile, entry.TopicID, id)
			}
			tab.model = entry.Model
			tab.effort = cloneStringPtr(entry.Effort)
			tab.mode = persistedTabMode(entry.Mode)
			tab.goal = strings.TrimSpace(entry.Goal)
			tab.toolApprovalMode = normalizeToolApprovalMode(entry.ToolApprovalMode)
			if tab.toolApprovalMode == control.ToolApprovalAsk && tabModeHasAutoApproveTools(entry.Mode) {
				tab.toolApprovalMode = control.ToolApprovalYolo
			}
			tab.ragScope = strings.TrimSpace(entry.RagScope)
			tab.profile = strings.TrimSpace(entry.Profile)
			tab.SessionPath = strings.TrimSpace(entry.SessionPath)
			tab.IsExpertSession = entry.IsExpertSession
			tab.ExpertTeamID = strings.TrimSpace(entry.ExpertTeamID)
			tab.ExpertTeamName = strings.TrimSpace(entry.ExpertTeamName)
			tab.sink = &tabEventSink{tabID: tab.ID, app: a, ctx: ctx}
			a.mu.Lock()
			a.tabs[tab.ID] = tab
			a.tabOrder = append(a.tabOrder, tab.ID)
			a.mu.Unlock()
			toBuild = append(toBuild, tab)
		}
		a.mu.Lock()
		if _, ok := a.tabs[f.ActiveTab]; ok {
			a.activeTabID = f.ActiveTab
		} else {
			ordered := a.orderedTabIDsLocked()
			if len(ordered) > 0 {
				a.activeTabID = ordered[0]
			}
		}
		a.mu.Unlock()
		for _, tab := range toBuild {
			a.startTabControllerBuild(tab)
		}
		return
	}

	// First launch: create a default Global tab.
	tab := a.createTabEntry("global", globalTabWorkspaceRoot(), "", "")
	tab.sink = &tabEventSink{tabID: tab.ID, app: a, ctx: ctx}
	tab.TopicTitle = "Global"
	a.mu.Lock()
	a.tabs[tab.ID] = tab
	a.tabOrder = append(a.tabOrder, tab.ID)
	a.activeTabID = tab.ID
	a.mu.Unlock()
	a.startTabControllerBuild(tab)
}

func (a *App) createTabEntry(scope, workspaceRoot, profile, topicID string) *WorkspaceTab {
	return a.createTabEntryWithID(scope, workspaceRoot, profile, topicID, newTabID())
}

func (a *App) createTabEntryWithID(scope, workspaceRoot, profile, topicID, id string) *WorkspaceTab {
	return &WorkspaceTab{
		ID:               id,
		Scope:            scope,
		WorkspaceRoot:    workspaceRoot,
		TopicID:          topicID,
		TopicTitle:       topicTitleForTab(scope, workspaceRoot, topicID),
		profile:          normalizeProfileName(profile),
		mode:             "normal",
		toolApprovalMode: control.ToolApprovalAsk,
		disabledMCP:      map[string]ServerView{},
	}
}

func (a *App) snapshotAllTabs() {
	a.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, t := range a.tabs {
		tabs = append(tabs, t)
	}
	a.mu.RUnlock()
	for _, t := range tabs {
		if t.Ctrl != nil {
			_ = t.Ctrl.Snapshot()
		}
	}
}

// shutdown snapshots all tabs, saves the final window geometry, and closes tabs.
func (a *App) shutdown(context.Context) {
	a.stopBotGateway()
	a.stopTray()
	// Stop the screenshot hotkey loop so its goroutine exits cleanly instead of
	// leaking (it now uses non-blocking PeekMessage + stopCh, so this returns
	// within one tick).
	a.StopScreenshotHotkey()
	// Stop the emergency-stop hotkey loop likewise.
	a.StopEStopHotkey()
	// Stop the RAG extraction pipeline so its worker goroutines exit cleanly
	// (pending chunks are durable — Resume() rehydrates them on next launch).
	if a.ragPipeline != nil {
		a.ragPipeline.Stop()
	}
	// Kill the Hyper-Extract Python subprocess so it doesn't leak as an orphan
	// when the app exits (Windows especially leaves it running otherwise).
	if a.heService != nil {
		a.heService.Stop()
	}
	// Stop the browser-use sidecar so it doesn't leak as an orphan on exit.
	if a.buService != nil {
		a.buService.Stop()
	}
	// Save window geometry synchronously from Go so it's persisted even if the
	// frontend's beforeunload promise hasn't resolved yet.
	a.saveWindowStateSync()

	a.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, t := range a.tabs {
		tabs = append(tabs, t)
	}
	a.mu.RUnlock()
	for _, t := range tabs {
		if t.Ctrl != nil {
			_ = t.Ctrl.Snapshot()
			t.Ctrl.Close()
			a.releaseSharedHost(t.WorkspaceRoot)
		}
	}
}

// domReady is called (via OnDomReady) after the webview finishes loading its DOM
// but before the window is shown (StartHidden). It restores the saved window
// position and size, then calls WindowShow so the user never sees the default
// size/position flash.
func (a *App) domReady(_ context.Context) {
	state, ok := loadWindowState()
	if ok {
		// Validate saved position against current screens. Wails v2 doesn't
		// expose per-screen origin (x,y offsets) so we can only do a basic
		// sanity check: ensure the window origin falls within a generous
		// estimate of the screen area. If the user unplugged an external
		// display, negative or out-of-bounds coordinates are caught here.
		valid := state.X >= 0 && state.Y >= 0
		if valid {
			screens, err := runtime.ScreenGetAll(a.ctx)
			if err == nil && len(screens) > 0 {
				maxW, maxH := 0, 0
				for _, sc := range screens {
					if sc.Size.Width > maxW {
						maxW = sc.Size.Width
					}
					if sc.Size.Height > maxH {
						maxH = sc.Size.Height
					}
				}
				if state.X > maxW*2 || state.Y > maxH*2 {
					valid = false
				}
			}
		}
		if valid {
			runtime.WindowSetPosition(a.ctx, state.X, state.Y)
		} else {
			runtime.WindowCenter(a.ctx)
		}
	} else {
		runtime.WindowCenter(a.ctx)
	}

	if ok && state.Maximised {
		runtime.WindowMaximise(a.ctx)
	}

	runtime.WindowShow(a.ctx)
}

// --- bound command surface (frontend → controller) ---
// Each method guards on a nil controller so a pre-startup or failed-build call is
// a no-op, never a panic.

// Submit runs raw user input as a turn; slash commands and @-references are
// resolved by the controller. Output arrives asynchronously on eventChannel.
func (a *App) Submit(input string) {
	a.SubmitToTab("", input)
}

func (a *App) SubmitToTab(tabID, input string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "/effort" || strings.HasPrefix(trimmed, "/effort ") {
		a.runEffortCommandForTab(tabID, trimmed)
		return
	}
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.SubmitDisplay(input, input)
	}
}

// RunShell executes a shell command directly (bypassing the model) and streams
// output as events on eventChannel.
func (a *App) RunShell(command string) {
	a.RunShellForTab("", command)
}

func (a *App) RunShellForTab(tabID, command string) {
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.RunShell(command)
	}
}

// SubmitDisplay runs input as a turn while recording a shorter UI-only display
// string for the saved desktop transcript. The model still receives input.
func (a *App) SubmitDisplay(display, input string) {
	a.SubmitDisplayToTab("", display, input)
}

func (a *App) SubmitDisplayToTab(tabID, display, input string) {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return
	}
	ctrl.SubmitDisplay(display, input)
}

func (a *App) bindControllerDisplayRecorder(ctrl *control.Controller) {
	if ctrl == nil {
		return
	}
	ctrl.SetDisplayRecorder(func(content, display string) {
		dir := ctrl.SessionDir()
		if dir == "" {
			dir = config.SessionDir()
		}
		_ = recordSessionDisplay(dir, ctrl.SessionPath(), content, display)
	})
}

// bindControllerContextFilter wires the expert-collab context projection onto a
// freshly built controller, so a full-fidelity expert-collab message in the
// transcript is shown to the model as its synthesis-only summary (keeping the
// multi-expert transcript from bloating the context window). Called at every
// controller build alongside bindControllerDisplayRecorder.
func (a *App) bindControllerContextFilter(ctrl *control.Controller) {
	if ctrl == nil {
		return
	}
	ctrl.SetContextFilter(expertspkg.CollabContextMessages)
}

// Cancel aborts the in-flight turn.
func (a *App) Cancel() {
	a.CancelTab("")
}

func (a *App) CancelTab(tabID string) {
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.Cancel()
	}
}

// Steer sends mid-turn guidance to the agent without interrupting the in-flight request.
func (a *App) Steer(text string) {
	a.SteerForTab("", text)
}

// SteerForTab sends mid-turn guidance to a specific tab's agent.
func (a *App) SteerForTab(tabID, text string) {
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.Steer(text)
	}
}

// Pause requests a graceful pause of the active tab's in-flight turn. The agent
// finishes its current step, then freezes with full state preserved. Contrast
// Cancel, which aborts and discards partial work. No-op when nothing is running.
func (a *App) Pause() {
	a.PauseTab("")
}

// PauseTab requests a graceful pause on a specific tab.
func (a *App) PauseTab(tabID string) {
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.Pause()
	}
}

// ResumeTurn unblocks a paused turn on the active tab. No-op when not paused.
func (a *App) ResumeTurn() {
	a.ResumeTurnTab("")
}

// ResumeTurnTab unblocks a paused turn on a specific tab.
func (a *App) ResumeTurnTab(tabID string) {
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.ResumeTurn()
	}
}

// PausedTab reports whether the given tab's turn is frozen on a graceful pause.
func (a *App) PausedTab(tabID string) bool {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return false
	}
	return ctrl.Paused()
}

// Approve answers a pending approval_request by ID: allow runs the call, session
// also remembers the grant for the rest of the session.
func (a *App) Approve(id string, allow, session, persist bool) {
	ctrl := a.ctrlByTabID("")
	if ctrl != nil {
		ctrl.Approve(id, allow, session, persist)
	}
}

// ApproveTab is like Approve but scoped to a specific tab.
func (a *App) ApproveTab(tabID, id string, allow, session, persist bool) {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl != nil {
		ctrl.Approve(id, allow, session, persist)
	}
}

// ReplayPendingPrompts asks every tab's controller to re-emit any approval/ask
// prompt that is currently blocking its run loop. The frontend calls this once
// its event subscription is live (on load/reconnect) so a session that was
// already awaiting confirmation rebuilds its modal instead of showing a
// "waiting" status with no way to answer — and no way to stop.
func (a *App) ReplayPendingPrompts() {
	a.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, t := range a.tabs {
		tabs = append(tabs, t)
	}
	a.mu.RUnlock()
	for _, t := range tabs {
		if t.Ctrl != nil {
			t.Ctrl.ReplayPendingPrompts()
		}
	}
}

// SetPlanMode toggles the read-only plan axis while preserving the current
// tool-auto-approval axis.
func (a *App) SetPlanMode(on bool) {
	a.setPlanModeForTab("", on)
}

func (a *App) setPlanModeForTab(tabID string, on bool) {
	current := a.currentModeForTab(tabID)
	a.SetModeForTab(tabID, tabModeFromAxes(on, tabModeHasAutoApproveTools(current)))
}

// SetMode applies a composer gating mode ("plan" | "yolo" | "plan-yolo" |
// anything else =
// normal) in one call, so a turn submitted right after the switch can't race a
// half-applied plan/tool-auto-approval pair.
func (a *App) SetMode(mode string) {
	a.SetModeForTab("", mode)
}

func (a *App) SetModeForTab(tabID, mode string) {
	normalized := normalizeTabMode(mode)
	a.mu.Lock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.Unlock()
		return
	}
	tab.mode = normalized
	tab.toolApprovalMode = normalizeToolApprovalMode(tab.toolApprovalMode)
	if tabModeHasAutoApproveTools(normalized) {
		tab.toolApprovalMode = control.ToolApprovalYolo
	} else if tab.toolApprovalMode == control.ToolApprovalYolo {
		tab.toolApprovalMode = control.ToolApprovalAsk
	}
	ctrl := tab.Ctrl
	approvalMode := tab.toolApprovalMode
	tabIDForSave := tab.ID
	a.mu.Unlock()
	applyTabModeToController(ctrl, normalized)
	applyTabToolApprovalModeToController(ctrl, approvalMode)
	a.mu.Lock()
	if a.tabs[tabIDForSave] == tab {
		a.saveTabsLocked()
	}
	a.mu.Unlock()
}

func applyTabModeToController(ctrl *control.Controller, mode string) {
	if ctrl == nil {
		return
	}
	switch normalizeTabMode(mode) {
	case "plan":
		ctrl.SetMode(true, false)
	case "yolo":
		ctrl.SetMode(false, true)
	case "plan-yolo":
		ctrl.SetMode(true, true)
	default:
		ctrl.SetMode(false, false)
	}
}

func applyTabToolApprovalModeToController(ctrl *control.Controller, mode string) {
	if ctrl == nil {
		return
	}
	ctrl.SetToolApprovalMode(normalizeToolApprovalMode(mode))
}

// applyTabRagScopeToController pushes the tab's knowledge-base auto-injection
// scope onto a (re)built controller. Mirrors applyTabToolApprovalModeToController.
func applyTabRagScopeToController(ctrl *control.Controller, scope string) {
	if ctrl == nil {
		return
	}
	ctrl.SetRAGScope(scope)
}

func (a *App) currentModeForTab(tabID string) string {
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	mode := "normal"
	if tab != nil {
		mode = currentTabMode(tab)
	}
	a.mu.RUnlock()
	return mode
}

func normalizeCollaborationMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "plan":
		return "plan"
	case "goal":
		return "goal"
	default:
		return "normal"
	}
}

func (a *App) SetCollaborationMode(mode string) {
	a.SetCollaborationModeForTab("", mode)
}

func (a *App) SetCollaborationModeForTab(tabID, mode string) {
	mode = normalizeCollaborationMode(mode)
	a.mu.Lock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.Unlock()
		return
	}
	approvalMode := currentTabToolApprovalMode(tab)
	switch mode {
	case "plan":
		tab.mode = tabModeFromAxes(true, approvalMode == control.ToolApprovalYolo)
		tab.goal = ""
	case "goal":
		tab.mode = tabModeFromAxes(false, approvalMode == control.ToolApprovalYolo)
	default:
		tab.mode = tabModeFromAxes(false, approvalMode == control.ToolApprovalYolo)
		tab.goal = ""
	}
	ctrl := tab.Ctrl
	goal := tab.goal
	plan := tabModeHasPlan(tab.mode)
	tabIDForSave := tab.ID
	a.mu.Unlock()
	if ctrl != nil {
		ctrl.SetPlanMode(plan)
		ctrl.SetGoal(goal)
	}
	a.mu.Lock()
	if a.tabs[tabIDForSave] == tab {
		a.saveTabsLocked()
	}
	a.mu.Unlock()
}

// QuestionAnswer is the frontend's reply to one question in an ask_request.
type QuestionAnswer struct {
	QuestionID string   `json:"questionId"`
	Selected   []string `json:"selected"`
}

// AnswerQuestion resolves a pending ask_request (the `ask` tool) by ID with the
// user's selections per question.
func (a *App) AnswerQuestion(id string, answers []QuestionAnswer) {
	a.AnswerQuestionForTab("", id, answers)
}

func (a *App) AnswerQuestionForTab(tabID, id string, answers []QuestionAnswer) {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return
	}
	out := make([]event.AskAnswer, len(answers))
	for i, an := range answers {
		out[i] = event.AskAnswer{QuestionID: an.QuestionID, Selected: an.Selected}
	}
	ctrl.AnswerQuestion(id, out)
}

// Compact runs one compaction pass on demand.
// Compact runs a plain compaction pass (the "compact now" button). Focus-guided
// compaction goes through Submit("/compact <focus>") instead.
func (a *App) Compact() error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.Compact(a.ctx, "")
}

// workspaceNotReadyErr names why a session action arrived before the tab's
// controller existed: still starting, or failed to start. Silently returning
// nil here swallowed the click with no feedback (#3938).
func workspaceNotReadyErr(tab *WorkspaceTab) error {
	if tab != nil && strings.TrimSpace(tab.StartupErr) != "" {
		return fmt.Errorf("workspace failed to start: %s", tab.StartupErr)
	}
	return fmt.Errorf("workspace is still starting")
}

// NewSession snapshots the current conversation and rotates to a fresh one.
func (a *App) NewSession() error {
	a.mu.RLock()
	tab := a.activeTabLocked()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return workspaceNotReadyErr(tab)
	}
	// Tab is already blank — just persist and skip the new-session dance.
	if !ctrl.Running() && !messagesHaveConversationContent(ctrl.History()) {
		a.persistTabSessionPath(tab, ctrl.SessionPath())
		return nil
	}

	if err := ctrl.NewSession(); err != nil {
		return err
	}
	a.persistTabSessionPath(tab, ctrl.SessionPath())
	return nil
}

func messagesHaveConversationContent(messages []provider.Message) bool {
	for _, msg := range messages {
		if msg.Role != provider.RoleSystem {
			return true
		}
	}
	return false
}

// ClearSession discards the current conversation and rotates to a fresh unsaved one.
func (a *App) ClearSession() error {
	a.mu.RLock()
	tab := a.activeTabLocked()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return workspaceNotReadyErr(tab)
	}
	if err := ctrl.ClearSession(); err != nil {
		return err
	}
	a.persistTabSessionPath(tab, ctrl.SessionPath())
	return nil
}

// CheckpointMeta summarises one rewind point (a user turn) for the desktop.
type CheckpointMeta struct {
	Turn            int      `json:"turn"`
	Prompt          string   `json:"prompt"`
	Files           []string `json:"files"` // paths changed during the turn
	Time            int64    `json:"time"`  // unix milliseconds
	CanCode         bool     `json:"canCode"`
	CanConversation bool     `json:"canConversation"`
}

// Checkpoints lists the session's rewind points, oldest first, for the rewind UI.
func (a *App) Checkpoints() []CheckpointMeta {
	return a.CheckpointsForTab("")
}

func (a *App) CheckpointsForTab(tabID string) []CheckpointMeta {
	a.mu.RLock()
	var ctrl *control.Controller
	if tab := a.tabByIDLocked(tabID); tab != nil {
		ctrl = tab.Ctrl
	}
	a.mu.RUnlock()
	if ctrl == nil {
		return []CheckpointMeta{}
	}
	metas := ctrl.Checkpoints()
	out := make([]CheckpointMeta, 0, len(metas))
	for _, m := range metas {
		out = append(out, CheckpointMeta{
			Turn:            m.Turn,
			Prompt:          m.Prompt,
			Files:           m.Paths,
			Time:            m.Time.UnixMilli(),
			CanCode:         len(m.Paths) > 0,
			CanConversation: ctrl.CheckpointHasBoundary(m.Turn),
		})
	}
	// RestoreCode(turn) reverts every file touched in this turn or any later one, so
	// a turn can rewind code even when it changed no files itself — as long as a
	// later turn did. Propagate CanCode backwards over the oldest-first list.
	hasCodeAfter := false
	for i := len(out) - 1; i >= 0; i-- {
		if len(out[i].Files) > 0 {
			hasCodeAfter = true
		}
		out[i].CanCode = hasCodeAfter
	}
	return out
}

// Rewind restores the session to the start of turn. scope is "code",
// "conversation", or "both" (anything else is treated as "both"). The frontend
// re-reads History after this resolves.
func (a *App) Rewind(turn int, scope string) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	s := control.RewindBoth
	switch scope {
	case "code":
		s = control.RewindCode
	case "conversation":
		s = control.RewindConversation
	}
	return ctrl.Rewind(turn, s)
}

// Fork branches the conversation at the start of turn into a new session tab
// (preserving the current tab), keeping code intact, and switches to the new tab.
func (a *App) Fork(turn int) (TabMeta, error) {
	a.mu.RLock()
	sourceTab := a.activeTabLocked()
	ctrl := a.activeCtrlLocked()
	if sourceTab == nil || ctrl == nil {
		a.mu.RUnlock()
		return TabMeta{}, nil
	}
	scope := sourceTab.Scope
	workspaceRoot := sourceTab.WorkspaceRoot
	sourceTitle := sourceTab.TopicTitle
	model := sourceTab.model
	effort := cloneStringPtr(sourceTab.effort)
	mode := currentTabMode(sourceTab)
	profileKey := normalizeProfileName(sourceTab.profile)
	disabledMCP := cloneServerViewMap(sourceTab.disabledMCP)
	mcpOrder := append([]string(nil), sourceTab.mcpOrder...)
	a.mu.RUnlock()

	newPath, err := ctrl.ForkSession(turn, "")
	if err != nil {
		return TabMeta{}, err
	}
	topicID := newTopicID()
	topicTitle := forkTopicTitle(sourceTitle)
	titleRoot := workspaceRoot
	if scope == "global" {
		titleRoot = ""
	}
	if err := setTopicTitle(titleRoot, topicID, topicTitle); err != nil {
		return TabMeta{}, err
	}
	m, _ := agent.EnsureBranchMeta(newPath)
	m.Scope = scope
	m.WorkspaceRoot = workspaceRoot
	m.TopicID = topicID
	m.TopicTitle = topicTitle
	m.Profile = profileKey
	if err := agent.SaveBranchMeta(newPath, m); err != nil {
		return TabMeta{}, err
	}

	a.mu.Lock()
	tabID := a.newUniqueTabIDLocked()
	tab := &WorkspaceTab{
		ID:            tabID,
		Scope:         scope,
		WorkspaceRoot: workspaceRoot,
		TopicID:       topicID,
		TopicTitle:    topicTitle,
		SessionPath:   newPath,
		model:         model,
		effort:        effort,
		mode:          mode,
		profile:       profileKey,
		disabledMCP:   disabledMCP,
		mcpOrder:      mcpOrder,
	}
	tab.sink = &tabEventSink{tabID: tabID, app: a}
	a.tabs[tabID] = tab
	a.tabOrder = append(a.tabOrder, tabID)
	a.activeTabID = tabID
	a.saveTabsLocked()
	meta := a.tabMeta(tab, true)
	a.mu.Unlock()

	a.emitProjectTreeChanged()
	a.startTabControllerBuild(tab)
	return meta, nil
}

// SummarizeFrom / SummarizeUpTo compress the conversation from / up to the start
// of turn into one summary (Claude Code's "summarize from/up to here"), keeping
// code intact. The frontend re-reads History after this resolves.
func (a *App) SummarizeFrom(turn int) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.SummarizeFrom(a.ctx, turn)
}

func (a *App) SummarizeUpTo(turn int) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.SummarizeUpTo(a.ctx, turn)
}

// SessionMeta summarises one saved session for the history panel.
type SessionMeta struct {
	Path           string `json:"path"`
	Preview        string `json:"preview"`         // first user message
	Title          string `json:"title,omitempty"` // user-chosen name, when set (overrides preview)
	Turns          int    `json:"turns"`
	CreatedAt      int64  `json:"createdAt"`      // unix milliseconds
	LastActivityAt int64  `json:"lastActivityAt"` // unix milliseconds
	ModTime        int64  `json:"modTime"`        // compatibility alias for lastActivityAt
	DeletedAt      int64  `json:"deletedAt,omitempty"`
	Current        bool   `json:"current"`
	Open           bool   `json:"open"`
	Scope          string `json:"scope,omitempty"`
	WorkspaceRoot  string `json:"workspaceRoot,omitempty"`
	TopicID        string `json:"topicId,omitempty"`
	TopicTitle     string `json:"topicTitle,omitempty"`
	Profile        string `json:"profile,omitempty"`
}

type WorkspaceMeta struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Current bool   `json:"current"`
}

func controllerSessionDir(ctrl *control.Controller) string {
	if ctrl != nil {
		if dir := ctrl.SessionDir(); dir != "" {
			return dir
		}
	}
	return desktopSessionDir("")
}

func tabSessionDir(tab *WorkspaceTab) string {
	if tab != nil {
		// The controller's session dir was fixed at boot.Build time from the
		// tab's profile, so it is already profile-correct — prefer it.
		if tab.Ctrl != nil {
			if dir := tab.Ctrl.SessionDir(); dir != "" {
				return dir
			}
		}
		// Controller not built yet (tab is being constructed): derive from the
		// workspace root + the tab's profile so even pre-build paths partition.
		if tab.WorkspaceRoot != "" {
			return desktopSessionDirFor(tab.WorkspaceRoot, tab.profile)
		}
		return desktopSessionDirFor("", tab.profile)
	}
	return desktopSessionDir("")
}

func (a *App) activeSessionDir() string {
	a.mu.RLock()
	tab := a.activeTabLocked()
	dir := tabSessionDir(tab)
	a.mu.RUnlock()
	return dir
}

// ListSessions returns the saved sessions newest-first for the history panel,
// marking the one the current conversation is writing to and attaching any
// user-chosen titles.
func (a *App) ListSessions() []SessionMeta {
	dir := a.activeSessionDir()
	infos, err := agent.ListSessions(dir)
	if err != nil {
		return []SessionMeta{}
	}
	titles := loadSessionTitles(dir)
	open := a.openSessionPaths(dir)
	active := a.activeSessionPath(dir)
	out := make([]SessionMeta, 0, len(infos))
	for _, s := range infos {
		_, isOpen := open[s.Path]
		out = append(out, sessionMetaFromInfo(s, titles[filepath.Base(s.Path)], s.Path == active, isOpen, 0))
	}
	return out
}

// ListTrashedSessions returns sessions that were moved to the local trash,
// newest-deleted first. These can be previewed, restored, or permanently purged.
func (a *App) ListTrashedSessions() []SessionMeta {
	out := []SessionMeta{}
	for _, dir := range a.knownSessionDirs() {
		paths, err := listTrashedSessionFiles(dir)
		if err != nil {
			continue
		}
		titles := loadSessionTitles(dir)
		for _, path := range paths {
			infos, err := agent.ListSessions(filepath.Dir(path))
			if err != nil || len(infos) == 0 {
				continue
			}
			deletedAt := trashedSessionDeletedAt(path)
			out = append(out, sessionMetaFromInfo(infos[0], titles[filepath.Base(path)], false, false, deletedAt))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DeletedAt == out[j].DeletedAt {
			return out[i].LastActivityAt > out[j].LastActivityAt
		}
		return out[i].DeletedAt > out[j].DeletedAt
	})
	return out
}

func (a *App) trashedSessionDir(path string) (string, error) {
	for _, dir := range a.knownSessionDirs() {
		if _, _, _, err := validateTrashedSessionPath(dir, path); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("trashed session path outside known session dirs: %s", path)
}

func (a *App) sessionDirForPath(path string) (string, string, error) {
	for _, dir := range a.knownSessionDirs() {
		sessionPath, _, err := validateSessionPath(dir, path)
		if err == nil {
			return dir, sessionPath, nil
		}
	}
	return "", "", fmt.Errorf("session path outside known session dirs: %s", path)
}

func sessionMetaFromInfo(s agent.SessionInfo, title string, current, open bool, deletedAt int64) SessionMeta {
	return SessionMeta{
		Path:           s.Path,
		Preview:        s.Preview,
		Title:          title,
		Turns:          s.Turns,
		CreatedAt:      s.CreatedAt.UnixMilli(),
		LastActivityAt: s.LastActivityAt.UnixMilli(),
		ModTime:        s.LastActivityAt.UnixMilli(),
		DeletedAt:      deletedAt,
		Current:        current,
		Open:           open,
		Scope:          s.Scope,
		WorkspaceRoot:  s.WorkspaceRoot,
		TopicID:        s.TopicID,
		TopicTitle:     s.TopicTitle,
		Profile:        s.Profile,
	}
}

// DeleteSession moves a saved session to the local trash. It refuses any open
// session because tab auto-save would recreate or append to the file later.
func (a *App) DeleteSession(path string) error {
	dir := a.activeSessionDir()
	sessionPath, key, err := validateSessionPath(dir, path)
	if err != nil {
		return err
	}
	if _, ok := a.openSessionPaths(dir)[sessionPath]; ok {
		return errActiveSession
	}
	if err := trashSessionArtifacts(dir, sessionPath, key); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

func (a *App) openSessionPaths(dir string) map[string]struct{} {
	a.mu.RLock()
	paths := make([]string, 0, len(a.tabs))
	for _, tab := range a.tabs {
		if tab != nil {
			paths = append(paths, tab.currentSessionPath())
		}
	}
	a.mu.RUnlock()

	out := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		currentPath, _, err := validateSessionPath(dir, path)
		if err == nil {
			out[currentPath] = struct{}{}
		}
	}
	return out
}

func (a *App) activeSessionPath(dir string) string {
	a.mu.RLock()
	var path string
	if tab := a.tabs[a.activeTabID]; tab != nil {
		path = tab.currentSessionPath()
	}
	a.mu.RUnlock()
	currentPath, _, err := validateSessionPath(dir, path)
	if err != nil {
		return ""
	}
	return currentPath
}

// RestoreSession moves a trashed session back into the saved-session list.
func (a *App) RestoreSession(path string) error {
	dir, err := a.trashedSessionDir(path)
	if err != nil {
		return err
	}
	_, key, _, err := validateTrashedSessionPath(dir, path)
	if err != nil {
		return err
	}
	if err := restoreTrashedSessionFile(dir, path); err != nil {
		return err
	}
	if err := restoreSessionTopicIndex(dir, filepath.Join(dir, key), a.activeProfileKey()); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

// PurgeTrashedSession permanently removes a trashed session and its title/display
// sidecars.
func (a *App) PurgeTrashedSession(path string) error {
	dir, err := a.trashedSessionDir(path)
	if err != nil {
		return err
	}
	return purgeTrashedSessionFile(dir, path)
}

// RenameSession sets a custom display name for a session (empty clears it back to
// the preview). It only affects the history panel; the file on disk is unchanged.
func (a *App) RenameSession(path, title string) error {
	return setSessionTitle(a.activeSessionDir(), path, title)
}

// ResumeSession snapshots the current conversation, then loads the session at
// path and continues it on the active tab. The model and working folder are
// unchanged; only the transcript is swapped. Returns the resumed messages for
// the frontend to render.
func (a *App) ResumeSession(path string) ([]HistoryMessage, error) {
	return a.ResumeSessionForTab("", path)
}

// ResumeSessionForTab is the tab-scoped form of ResumeSession. History rows
// carry scope/workspace/topic metadata, so callers that opened or selected a
// matching tab should resume on that exact controller instead of whichever tab is
// active by the time the async call reaches the backend.
func (a *App) ResumeSessionForTab(tabID, path string) ([]HistoryMessage, error) {
	tab := a.tabByID(tabID)
	if tab == nil {
		return []HistoryMessage{}, fmt.Errorf("tab is not ready")
	}
	// Snapshot ctrl under RLock to avoid TOCTOU.
	a.mu.RLock()
	ctrl := tab.Ctrl
	a.mu.RUnlock()
	if ctrl == nil {
		return []HistoryMessage{}, fmt.Errorf("tab is not ready")
	}
	sessionPath, _, err := validateSessionPath(controllerSessionDir(ctrl), path)
	if err != nil {
		return nil, err
	}
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		return nil, err
	}
	_ = ctrl.Snapshot() // persist the current session before switching away
	ctrl.Resume(loaded, sessionPath)
	a.rememberTabSessionPath(tab, sessionPath)
	return a.HistoryForTab(tabID), nil
}

// PreviewSession reads a saved session for display only. It does not snapshot or
// swap the active controller, so the history drawer can call it while a turn runs.
func (a *App) PreviewSession(path string) ([]HistoryMessage, error) {
	sessionDir, sessionPath, err := a.sessionDirForPath(path)
	if err != nil {
		return nil, err
	}
	return previewSessionMessages(sessionDir, sessionPath)
}

// PickWorkspace opens a folder chooser and, on a pick, opens a new project tab
// scoped to that folder. Returns the chosen path ("" if cancelled).
func (a *App) PickWorkspace() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	cur, _ := os.Getwd()
	a.mu.RLock()
	if tab := a.activeTabLocked(); tab != nil && tab.WorkspaceRoot != "" {
		cur = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "选择工作文件夹",
		DefaultDirectory: dialogDefaultDirectory(cur),
	})
	if err != nil || dir == "" {
		return "", err
	}
	return a.SwitchWorkspace(dir)
}

// PickImportFolder opens a directory dialog WITHOUT switching workspace.
// Used by the RAG import flow so importing documents into a knowledge-base
// collection doesn't pollute the project tree with a new workspace entry.
func (a *App) PickImportFolder() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	cur, _ := os.Getwd()
	a.mu.RLock()
	if tab := a.activeTabLocked(); tab != nil && tab.WorkspaceRoot != "" {
		cur = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "选择要导入的文件夹",
		DefaultDirectory: dialogDefaultDirectory(cur),
	})
	if err != nil || dir == "" {
		return "", err
	}
	return dir, nil
}

// PickImportFiles opens a multi-file dialog WITHOUT switching workspace.
// Supports selecting individual files (not just folders) for import.
func (a *App) PickImportFiles() ([]string, error) {
	if a.ctx == nil {
		return nil, nil
	}
	cur, _ := os.Getwd()
	a.mu.RLock()
	if tab := a.activeTabLocked(); tab != nil && tab.WorkspaceRoot != "" {
		cur = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	selection, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "选择要导入的文件",
		DefaultDirectory: dialogDefaultDirectory(cur),
		Filters: []runtime.FileFilter{
			{DisplayName: "文档文件", Pattern: "*.pdf;*.docx;*.xlsx;*.pptx;*.xls;*.epub;*.txt;*.md;*.csv;*.tsv;*.json;*.html;*.htm;*.py;*.go;*.js;*.ts;*.yaml;*.yml"},
			{DisplayName: "所有文件", Pattern: "*"},
		},
	})
	if err != nil || len(selection) == 0 {
		return nil, err
	}
	return selection, nil
}

func dialogDefaultDirectory(preferred string) string {
	if dir := nearestExistingDirectory(preferred); dir != "" {
		return dir
	}
	if cwd, err := os.Getwd(); err == nil {
		if dir := nearestExistingDirectory(cwd); dir != "" {
			return dir
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if dir := nearestExistingDirectory(home); dir != "" {
			return dir
		}
	}
	return ""
}

func nearestExistingDirectory(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	for {
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				return path
			}
			path = filepath.Dir(path)
			continue
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}

func (a *App) ListWorkspaces() []WorkspaceMeta {
	profileKey := a.activeProfileKey()
	migrateLegacyWorkspacesIntoProjects(profileKey)
	activeRoot := ""
	a.mu.RLock()
	if tab := a.activeTabLocked(); tab != nil && tab.WorkspaceRoot != "" {
		activeRoot = normalizeProjectRoot(tab.WorkspaceRoot)
	}
	a.mu.RUnlock()
	openRoots := map[string]bool{}
	if activeRoot != "" {
		openRoots[activeRoot] = true
	}
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab != nil && tab.WorkspaceRoot != "" {
			if profileKey == "" || tab.profile == "" || config.ProfileNameKey(tab.profile) == profileKey {
				openRoots[normalizeProjectRoot(tab.WorkspaceRoot)] = true
			}
		}
	}
	a.mu.RUnlock()

	projects := loadProjectsFile(profileKey).Projects
	out := make([]WorkspaceMeta, 0, len(projects))
	for _, project := range projects {
		if len(project.Topics) == 0 && len(loadTopicTitles(project.Root)) == 0 && !openRoots[normalizeProjectRoot(project.Root)] {
			continue
		}
		out = append(out, WorkspaceMeta{
			Path:    project.Root,
			Name:    projectDisplayName(project),
			Current: activeRoot != "" && project.Root == activeRoot,
		})
	}
	return out
}

func (a *App) RemoveWorkspace(dir string) error {
	if dir == "" {
		return fmt.Errorf("workspace path is required")
	}
	profileKey := a.activeProfileKey()
	dir = normalizeProjectRoot(dir)
	forgetWorkspace(dir)
	if err := removeProject(dir, profileKey); err != nil {
		return err
	}
	// If the removed workspace was the active one, clear the pointer
	// so we don't leave a stale reference to a deleted project.
	if loadWorkspace() == dir {
		if remaining := loadProjectsFile(profileKey); len(remaining.Projects) > 0 {
			// Fall back to the first remaining project
			saveWorkspace(remaining.Projects[0].Root)
		} else {
			// No projects left; clear the active pointer entirely
			clearWorkspace()
		}
	}
	a.emitProjectTreeChanged()
	return nil
}

func migrateLegacyWorkspacesIntoProjects(profileKey string) {
	legacy := loadWorkspaces()
	if len(legacy) == 0 {
		return
	}
	f := loadProjectsFile(profileKey)
	seen := make(map[string]bool, len(f.Projects)+len(legacy))
	for _, p := range f.Projects {
		seen[p.Root] = true
	}
	changed := false
	for _, path := range legacy {
		root := normalizeProjectRoot(path)
		if root == "" || seen[root] {
			continue
		}
		f.Projects = append(f.Projects, desktopProject{Root: root})
		seen[root] = true
		changed = true
	}
	if changed {
		_ = saveProjectsFile(f, profileKey)
	}
}

func workspaceName(path string) string {
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return path
	}
	return name
}

func (a *App) SwitchWorkspace(dir string) (string, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = home
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	saveWorkspace(dir)

	// Open a registered topic so the new workspace appears in the project tree
	// immediately instead of only existing as an in-memory tab. New workspaces
	// open in the active tab's profile (dev by default).
	profile := a.activeProfileKey()
	topic, err := a.CreateTopic("project", dir, profile, "")
	if err != nil {
		return "", err
	}
	meta, err := a.OpenProjectTab(dir, topic.ID, profile)
	if err != nil {
		return "", err
	}
	return meta.WorkspaceRoot, nil
}

// HistoryMessage is one prior turn, for the frontend to repopulate its transcript
// after a reload.
type HistoryMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	Reasoning  string            `json:"reasoning,omitempty"`
	Level      string            `json:"level,omitempty"`
	ToolCalls  []HistoryToolCall `json:"toolCalls,omitempty"`
	ToolCallID string            `json:"toolCallId,omitempty"`
	ToolName   string            `json:"toolName,omitempty"`
	Pending    bool              `json:"pending,omitempty"`
	Trigger    string            `json:"trigger,omitempty"`
	Messages   int               `json:"messages,omitempty"`
	Summary    string            `json:"summary,omitempty"`
	Archive    string            `json:"archive,omitempty"`
}

type HistoryToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// History returns the session's message log.
func (a *App) History() []HistoryMessage {
	return a.HistoryForTab("")
}

func (a *App) HistoryForTab(tabID string) []HistoryMessage {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return []HistoryMessage{}
	}
	msgs := ctrl.History()
	return historyMessages(msgs, sessionDisplayResolver(controllerSessionDir(ctrl), ctrl.SessionPath()))
}

func historyMessages(msgs []provider.Message, resolveUserContent func(string) string) []HistoryMessage {
	out := make([]HistoryMessage, 0, len(msgs))
	for _, m := range msgs {
		content := provider.ContentString(m.Content)
		if m.Role == provider.RoleUser {
			content = resolveUserContent(content)
			if control.IsSyntheticUserMessage(content) {
				continue
			}
		}
		reasoning := ""
		if m.Role == provider.RoleAssistant {
			reasoning = m.ReasoningContent
		}
		hm := HistoryMessage{Role: string(m.Role), Content: content, Reasoning: reasoning}
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			hm.ToolCalls = make([]HistoryToolCall, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				hm.ToolCalls[i] = HistoryToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}
			}
		}
		if m.Role == provider.RoleTool {
			hm.ToolCallID = m.ToolCallID
			hm.ToolName = m.Name
		}
		out = append(out, hm)
	}
	return out
}

func previewSessionMessages(sessionDir, path string) ([]HistoryMessage, error) {
	sessionPath, _, err := validateSessionPath(sessionDir, path)
	if err != nil {
		return nil, err
	}
	if out, ok, err := previewEventSessionMessages(sessionPath); ok || err != nil {
		return out, err
	}
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		return nil, err
	}
	return historyMessages(loaded.Snapshot(), sessionDisplayResolver(sessionDir, sessionPath)), nil
}

type previewEventRecord struct {
	Kind             string             `json:"kind"`
	Type             string             `json:"type"`
	Role             string             `json:"role"`
	Text             string             `json:"text"`
	Content          string             `json:"content"`
	Reasoning        string             `json:"reasoning"`
	ReasoningContent string             `json:"reasoningContent"`
	Level            string             `json:"level"`
	ToolCalls        []previewToolCall  `json:"toolCalls"`
	CallID           string             `json:"callId"`
	ToolCallID       string             `json:"toolCallId"`
	ToolName         string             `json:"toolName"`
	Name             string             `json:"name"`
	Output           string             `json:"output"`
	Compaction       *previewCompaction `json:"compaction"`
	Trigger          string             `json:"trigger"`
	Messages         int                `json:"messages"`
	Summary          string             `json:"summary"`
	Archive          string             `json:"archive"`
}

type previewToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Function  struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type previewCompaction struct {
	Trigger  string `json:"trigger"`
	Messages int    `json:"messages"`
	Summary  string `json:"summary"`
	Archive  string `json:"archive"`
}

func previewEventSessionMessages(path string) ([]HistoryMessage, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	out := []HistoryMessage{}
	toolName := map[string]string{}
	sawEvent := false
	for {
		var rec previewEventRecord
		if err := dec.Decode(&rec); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if sawEvent {
				return out, true, nil
			}
			return nil, false, nil
		}
		eventName := strings.TrimSpace(rec.Kind)
		if eventName == "" {
			eventName = strings.TrimSpace(rec.Type)
		}
		if eventName == "" {
			continue
		}
		sawEvent = true
		switch eventName {
		case "user.message":
			if rec.Text != "" {
				out = append(out, HistoryMessage{Role: "user", Content: rec.Text})
			}
		case "model.final":
			hm := HistoryMessage{Role: "assistant", Content: rec.Content, Reasoning: firstNonEmpty(rec.Reasoning, rec.ReasoningContent)}
			for _, tc := range rec.ToolCalls {
				id := tc.ID
				name := firstNonEmpty(tc.Name, tc.Function.Name)
				args := firstNonEmpty(tc.Arguments, tc.Function.Arguments)
				hm.ToolCalls = append(hm.ToolCalls, HistoryToolCall{ID: id, Name: name, Arguments: args})
				if id != "" {
					toolName[id] = name
				}
			}
			out = append(out, hm)
		case "tool.result":
			callID := firstNonEmpty(rec.CallID, rec.ToolCallID)
			out = append(out, HistoryMessage{
				Role:       "tool",
				ToolCallID: callID,
				ToolName:   firstNonEmpty(rec.ToolName, rec.Name, toolName[callID]),
				Content:    firstNonEmpty(rec.Output, rec.Content),
			})
		case "phase":
			out = append(out, HistoryMessage{Role: "phase", Content: firstNonEmpty(rec.Text, rec.Content)})
		case "notice":
			level := rec.Level
			if level != "warn" {
				level = "info"
			}
			out = append(out, HistoryMessage{Role: "notice", Level: level, Content: firstNonEmpty(rec.Text, rec.Content)})
		case "compaction_started":
			c := rec.compactionPayload()
			out = append(out, HistoryMessage{Role: "compaction", Pending: true, Trigger: c.Trigger})
		case "compaction_done":
			c := rec.compactionPayload()
			out = append(out, HistoryMessage{
				Role:     "compaction",
				Trigger:  c.Trigger,
				Messages: c.Messages,
				Summary:  c.Summary,
				Archive:  c.Archive,
			})
		}
	}
	return out, sawEvent, nil
}

func (r previewEventRecord) compactionPayload() previewCompaction {
	if r.Compaction != nil {
		return *r.Compaction
	}
	return previewCompaction{Trigger: r.Trigger, Messages: r.Messages, Summary: r.Summary, Archive: r.Archive}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// ContextInfo is the prompt-vs-window gauge payload plus session totals. Used
// and Window both zero means no context-window data yet.
type ContextInfo struct {
	Used          int     `json:"used"`
	Window        int     `json:"window"`
	SessionTokens int     `json:"sessionTokens"`
	CompactRatio  float64 `json:"compactRatio,omitempty"`
}

// ContextUsage returns the latest context-window gauge numbers.
func (a *App) ContextUsage() ContextInfo {
	return a.ContextUsageForTab("")
}

func (a *App) ContextUsageForTab(tabID string) ContextInfo {
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	var ctrl *control.Controller
	if tab != nil {
		ctrl = tab.Ctrl
	}
	a.mu.RUnlock()

	var sessionTokens int
	if tab != nil {
		sessionTokens = tab.telemetrySnapshot().Usage.TotalTokens
	}
	if ctrl == nil {
		return ContextInfo{SessionTokens: sessionTokens}
	}
	used, window := ctrl.ContextSnapshot()
	return ContextInfo{Used: used, Window: window, SessionTokens: sessionTokens, CompactRatio: ctrl.CompactRatio()}
}

// JobView is one running background job (bash/task started with
// run_in_background) for the status-bar indicator.
type JobView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	StartedAt int64  `json:"startedAt"`
}

// Jobs returns the still-running background jobs for the status bar. It refreshes
// on demand (mount, turn end, and on each notice the frontend receives).
func (a *App) Jobs() []JobView {
	out := []JobView{}
	ctrl := a.ctrlByTabID("")
	return a.jobsForCtrl(ctrl, out)
}

func (a *App) JobsForTab(tabID string) []JobView {
	out := []JobView{}
	ctrl := a.ctrlByTabID(tabID)
	return a.jobsForCtrl(ctrl, out)
}

func (a *App) jobsForCtrl(ctrl *control.Controller, out []JobView) []JobView {
	if ctrl == nil {
		return out
	}
	for _, v := range ctrl.Jobs() {
		out = append(out, JobView{ID: v.ID, Kind: v.Kind, Label: v.Label, Status: v.Status, StartedAt: v.StartedAt})
	}
	return out
}

// Meta describes the session for the frontend's header and status line.
type Meta struct {
	Label            string `json:"label"`
	Ready            bool   `json:"ready"`
	StartupErr       string `json:"startupErr,omitempty"`
	EventChannel     string `json:"eventChannel"`
	Cwd              string `json:"cwd"`
	AutoApproveTools bool   `json:"autoApproveTools"`
	Bypass           bool   `json:"bypass"` // legacy JSON key for YOLO/full-access tool auto-approval
	ToolApprovalMode string `json:"toolApprovalMode"`
	RagScope         string `json:"ragScope,omitempty"`
	Goal             string `json:"goal,omitempty"`
	GoalStatus       string `json:"goalStatus,omitempty"`
	// ExpertSession is set when this tab is an expert-team collaboration session.
	ExpertSession *ExpertSessionMeta `json:"expertSession,omitempty"`
}

// Meta reports the model label, readiness, any startup error, the working
// directory (for the status line), and the runtime event channel the frontend
// subscribes to.
func (a *App) Meta() Meta {
	return a.MetaForTab("")
}

func (a *App) MetaForTab(tabID string) Meta {
	// Snapshot ctrl pointer + scalar fields under a single RLock to avoid torn
	// reads. Controller methods are called AFTER releasing the lock to avoid
	// deadlock (controller internals may also acquire a.mu).
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.RUnlock()
		return Meta{EventChannel: eventChannel}
	}
	ctrl := tab.Ctrl
	label := tab.Label
	ready := tab.Ready
	startupErr := tab.StartupErr
	cwd := tab.WorkspaceRoot
	goal := tab.goal
	toolApprovalMode := tab.toolApprovalMode
	ragScope := tab.ragScope
	isExpert := tab.IsExpertSession
	expertTeamID := tab.ExpertTeamID
	expertTeamName := tab.ExpertTeamName
	a.mu.RUnlock()

	// Resolve the team's current name (it may have been renamed since the tab
	// was created/restored). Falls back to the persisted name if the team was
	// deleted or the store is offline.
	if isExpert && expertTeamID != "" {
		if fresh := a.teamDisplayName(expertTeamID); fresh != expertTeamID {
			expertTeamName = fresh
		}
	}

	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	autoApproveTools := ctrl != nil && ctrl.AutoApproveTools()
	if ctrl != nil {
		toolApprovalMode = ctrl.ToolApprovalMode()
		goal = ctrl.Goal()
	}
	var goalStatus string
	if ctrl != nil {
		goalStatus = ctrl.GoalStatus()
	} else if strings.TrimSpace(goal) != "" {
		goalStatus = control.GoalStatusRunning
	} else {
		goalStatus = control.GoalStatusStopped
	}
	if toolApprovalMode == "" {
		toolApprovalMode = control.ToolApprovalAsk
	}
	var expertSession *ExpertSessionMeta
	if isExpert {
		expertSession = &ExpertSessionMeta{TeamID: expertTeamID, TeamName: expertTeamName}
	}
	return Meta{
		Label:            label,
		Ready:            ready,
		StartupErr:       startupErr,
		EventChannel:     eventChannel,
		Cwd:              cwd,
		AutoApproveTools: autoApproveTools,
		Bypass:           autoApproveTools,
		ToolApprovalMode: toolApprovalMode,
		RagScope:         ragScope,
		Goal:             goal,
		GoalStatus:       goalStatus,
		ExpertSession:    expertSession,
	}
}

func (a *App) SetGoal(goal string) {
	a.SetGoalForTab("", goal)
}

func (a *App) SetGoalForTab(tabID, goal string) {
	goal = strings.TrimSpace(goal)
	a.mu.Lock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.Unlock()
		return
	}
	tab.goal = goal
	if goal != "" {
		tab.mode = tabModeFromAxes(false, currentTabToolApprovalMode(tab) == control.ToolApprovalYolo)
	}
	ctrl := tab.Ctrl
	plan := tabModeHasPlan(tab.mode)
	tabIDForSave := tab.ID
	a.mu.Unlock()
	if ctrl != nil {
		ctrl.SetPlanMode(plan)
		ctrl.SetGoal(goal)
	}
	a.mu.Lock()
	if a.tabs[tabIDForSave] == tab {
		a.saveTabsLocked()
	}
	a.mu.Unlock()
}

func (a *App) ClearGoal() {
	a.SetGoal("")
}

func (a *App) ClearGoalForTab(tabID string) {
	a.SetGoalForTab(tabID, "")
}

// SetAutoApproveTools toggles YOLO/full-access tool auto-approval:
// approval-gated tool calls run without asking, while ask questions and plan
// approvals still wait for the user. Runtime-only — not written to config.
func (a *App) SetAutoApproveTools(on bool) {
	if on {
		a.SetToolApprovalModeForTab("", control.ToolApprovalYolo)
		return
	}
	a.SetToolApprovalModeForTab("", control.ToolApprovalAsk)
}

// SetBypass is the legacy Wails binding for SetAutoApproveTools.
func (a *App) SetBypass(on bool) {
	a.SetAutoApproveTools(on)
}

func (a *App) SetToolApprovalMode(mode string) {
	a.SetToolApprovalModeForTab("", mode)
}

func (a *App) SetToolApprovalModeForTab(tabID, mode string) {
	mode = normalizeToolApprovalMode(mode)
	a.mu.Lock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.Unlock()
		return
	}
	tab.toolApprovalMode = mode
	tab.mode = tabModeFromAxes(tabModeHasPlan(currentTabMode(tab)), mode == control.ToolApprovalYolo)
	ctrl := tab.Ctrl
	tabIDForSave := tab.ID
	a.mu.Unlock()
	applyTabToolApprovalModeToController(ctrl, mode)
	a.mu.Lock()
	if a.tabs[tabIDForSave] == tab {
		a.saveTabsLocked()
	}
	a.mu.Unlock()
}

// SetRagScope sets the knowledge-base auto-injection scope for the active tab
// ("" = 不使用, the default). Wired from the Composer "知识库" dropdown.
func (a *App) SetRagScope(scope string) {
	a.SetRagScopeForTab("", scope)
}

// SetRagScopeForTab sets the knowledge-base auto-injection scope for a tab,
// applies it to the live controller, and persists the selection. Mirrors
// SetToolApprovalModeForTab.
func (a *App) SetRagScopeForTab(tabID, scope string) {
	scope = strings.TrimSpace(scope)
	a.mu.Lock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.Unlock()
		return
	}
	tab.ragScope = scope
	ctrl := tab.Ctrl
	tabIDForSave := tab.ID
	a.mu.Unlock()
	applyTabRagScopeToController(ctrl, scope)
	a.mu.Lock()
	if a.tabs[tabIDForSave] == tab {
		a.saveTabsLocked()
	}
	a.mu.Unlock()
}

// CommandInfo describes one available slash command for the composer's "/" menu.
type CommandInfo struct {
	Name        string `json:"name"` // without the leading slash
	Description string `json:"description"`
	Hint        string `json:"hint,omitempty"` // argument hint, if any
	Kind        string `json:"kind"`           // "builtin" | "custom" | "mcp"
}

// Commands lists the slash commands available this session — built-in actions,
// custom commands (.momapeer/commands), and MCP prompts — for the composer's "/"
// autocomplete menu.
func (a *App) Commands() []CommandInfo {
	out := []CommandInfo{
		{Name: "new", Description: i18n.M.CmdNew, Kind: "builtin"},
		{Name: "clear", Description: i18n.M.CmdClear, Kind: "builtin"},
		{Name: "compact", Description: i18n.M.CmdCompact, Kind: "builtin"},
		{Name: "model", Description: i18n.M.CmdModel, Kind: "builtin"},
		{Name: "provider", Description: i18n.M.CmdProvider, Kind: "builtin"},
		{Name: "effort", Description: i18n.M.CmdEffort, Kind: "builtin"},
		{Name: "memory", Description: i18n.M.CmdMemory, Kind: "builtin"},
		{Name: "goal", Description: i18n.M.CmdGoal, Kind: "builtin"},
		{Name: "remember", Description: i18n.M.CmdRemember, Kind: "builtin"},
		{Name: "mcp", Description: i18n.M.CmdMcp, Kind: "builtin"},
		{Name: "hooks", Description: i18n.M.CmdHooks, Kind: "builtin"},
		{Name: "theme", Description: i18n.M.CmdTheme, Kind: "builtin"},
		{Name: "skill", Description: i18n.M.CmdSkill, Kind: "builtin"},
	}
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return out
	}
	// Skills are invocable as /<name> (the model runs inline ones; subagent ones
	// run isolated). Listing them here is what surfaces /init, /explore, … in the
	// composer's slash menu; selecting one submits "/<name>", which the controller
	// resolves via RunSkill.
	for _, s := range ctrl.Skills() {
		out = append(out, CommandInfo{Name: s.Name, Description: s.Description, Kind: "skill"})
	}
	for _, c := range ctrl.Commands() {
		out = append(out, CommandInfo{Name: c.Name, Description: c.Description, Hint: c.ArgHint, Kind: "custom"})
	}
	if h := ctrl.Host(); h != nil {
		for _, p := range h.Prompts() {
			out = append(out, CommandInfo{Name: p.Name, Description: p.Description, Kind: "mcp"})
		}
	}
	return out
}

// SlashArgItem is one sub-command / argument suggestion for the composer's slash
// menu (the part after the command word). Mirrors the CLI's arg completion via
// the shared control.SlashArgItems, so desktop and CLI offer the same hints.
type SlashArgItem struct {
	Label   string `json:"label"`
	Insert  string `json:"insert"`
	Hint    string `json:"hint"`
	Descend bool   `json:"descend"`
}

// SlashArgsResult carries the suggestions plus the byte offset in the input where
// the current token begins, so the composer replaces just that token.
type SlashArgsResult struct {
	Items []SlashArgItem `json:"items"`
	From  int            `json:"from"`
}

// SlashArgs completes the arguments of a management slash command (/mcp, /model,
// /skill, /hooks) for the composer — the same logic the chat TUI uses. Empty
// Items means the input has no structured arguments to complete.
func (a *App) SlashArgs(input string) SlashArgsResult {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	model := ""
	if tab := a.activeTabLocked(); tab != nil {
		model = tab.model
	}
	a.mu.RUnlock()
	if ctrl == nil {
		return SlashArgsResult{Items: []SlashArgItem{}}
	}
	data := control.ArgData{
		Skills:          ctrl.Skills(),
		DisabledSkills:  ctrl.DisabledSkills(),
		ConfiguredMCP:   ctrl.ConfiguredMCPNames(),
		DisconnectedMCP: ctrl.DisconnectedMCPNames(),
		CurrentModel:    model,
	}
	seen := map[string]bool{}
	for _, m := range a.Models() {
		data.ModelRefs = append(data.ModelRefs, m.Ref)
		if m.Provider != "" && !seen[m.Provider] {
			seen[m.Provider] = true
			data.ProviderNames = append(data.ProviderNames, m.Provider)
		}
		if m.Current {
			data.CurrentProvider = m.Provider
		}
	}
	if h := ctrl.Host(); h != nil {
		data.ServerNames = h.ServerNames()
	}
	items, from := control.SlashArgItems(input, data)
	// Non-nil so it serializes as a JSON array, never null — the frontend filters
	// over it directly.
	out := SlashArgsResult{Items: []SlashArgItem{}, From: from}
	for _, it := range items {
		out.Items = append(out.Items, SlashArgItem{Label: it.Label, Insert: it.Insert, Hint: it.Hint, Descend: it.Descend})
	}
	return out
}

// CapabilitiesView is the MCP & Skills drawer's data: connected/failed MCP
// servers and the discoverable skills, the GUI counterpart to `/mcp` + `/skill`.
type CapabilitiesView struct {
	Servers      []ServerView      `json:"servers"`
	Skills       []SkillView       `json:"skills"`
	SkillRoots   []SkillRootView   `json:"skillRoots"`
	JiutianTools []JiutianToolView `json:"jiutianTools"`
}

// JiutianToolView represents one Jiutian multimodal tool with its toggle state.
type JiutianToolView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// ServerView is one MCP server for the drawer. Status is "connected" (with
// tool/prompt/resource counts), "deferred" (lazy/on-demand startup enabled),
// "failed" (with the connection error), "initializing" (background startup in
// progress), or "disabled".
type ServerView struct {
	Name           string     `json:"name"`
	Transport      string     `json:"transport"`
	Status         string     `json:"status"`
	BuiltIn        bool       `json:"builtIn,omitempty"`
	Configured     bool       `json:"configured,omitempty"`
	AutoStart      bool       `json:"autoStart"`
	Tier           string     `json:"tier,omitempty"`
	Command        string     `json:"command,omitempty"`
	Args           []string   `json:"args,omitempty"`
	URL            string     `json:"url,omitempty"`
	EnvKeys        []string   `json:"envKeys,omitempty"`
	Tools          int        `json:"tools"`
	Prompts        int        `json:"prompts"`
	Resources      int        `json:"resources"`
	Error          string     `json:"error,omitempty"`
	ToolList       []ToolView `json:"toolList,omitempty"`
	AuthStatus     string     `json:"authStatus,omitempty"`
	AuthURL        string     `json:"authUrl,omitempty"`
	AuthConfigured bool       `json:"authConfigured,omitempty"`
}

type ToolView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SkillView is one discoverable skill for the drawer.
type SkillView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
	RunAs       string `json:"runAs"`
	Enabled     bool   `json:"enabled"`
	// Active reports whether the skill is in effect under the current product
	// profile's skill set: true when it is surfaced to the model (in the pinned
	// index / callable), false when the active profile's whitelist hides it. This
	// is distinct from Enabled (a user toggle): a profile-hidden skill can be
	// Enabled=true yet Active=false. The skills page uses this to separate "in
	// effect for this mode" from "available after switching mode".
	Active bool `json:"active"`
}

type SkillRootSkillView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
	RunAs       string `json:"runAs"`
}

// SkillRootView is one skill discovery root for the drawer's Sources section.
type SkillRootView struct {
	Dir        string               `json:"dir"`
	Scope      string               `json:"scope"`
	Priority   int                  `json:"priority"`
	Status     string               `json:"status"`
	Configured bool                 `json:"configured"`
	Removable  bool                 `json:"removable"`
	Skills     int                  `json:"skills"`
	SkillItems []SkillRootSkillView `json:"skillItems,omitempty"`
	Warning    string               `json:"warning,omitempty"`
}

// Capabilities projects the session's MCP servers (connected + failed) and skills
// for the MCP & Skills drawer. Non-nil slices so the frontend can map over them.
func (a *App) Capabilities() CapabilitiesView {
	out := CapabilitiesView{Servers: []ServerView{}, Skills: []SkillView{}, SkillRoots: []SkillRootView{}}
	// Snapshot all tab fields we need under the read lock, THEN release. Reading
	// tab.disabledMCP (a map) outside the lock races with RemoveMCPServer's
	// delete() under a write lock — that is a concurrent map read/write crash.
	var ctrl *control.Controller
	disabled := map[string]ServerView{}
	var order []string
	var workspaceRoot string
	var profileName string
	a.mu.RLock()
	if tab := a.activeTabLocked(); tab != nil {
		ctrl = tab.Ctrl
		workspaceRoot = tab.WorkspaceRoot
		profileName = tab.profile
		for name, s := range tab.disabledMCP {
			disabled[name] = s
		}
		order = append([]string(nil), tab.mcpOrder...)
	}
	a.mu.RUnlock()
	if ctrl == nil {
		return out
	}
	seen := map[string]bool{}
	connected := map[string]bool{}
	retainedDisabled := map[string]ServerView{}
	var loadedCfg *config.Config
	configured := map[string]config.PluginEntry{}
	var configuredEntries []config.PluginEntry
	if cfg, err := config.LoadForRoot(workspaceRoot); err == nil {
		loadedCfg = cfg
		configuredEntries = append(configuredEntries, cfg.Plugins...)
		for _, p := range configuredEntries {
			configured[p.Name] = p
		}
	}
	if h := ctrl.Host(); h != nil {
		for _, s := range h.Servers() {
			if seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			connected[s.Name] = true
			view := ServerView{
				Name: s.Name, Transport: s.Transport, Status: "connected",
				BuiltIn: s.Name == "codegraph",
				Tools:   s.Tools, Prompts: s.Prompts, Resources: s.Resources,
				ToolList: pluginToolsToView(s.ToolList),
			}
			if p, ok := configured[s.Name]; ok {
				view = withPluginConfig(view, p)
			} else if s.Name == "codegraph" && loadedCfg != nil {
				view = withCodegraphConfig(view, loadedCfg.Codegraph)
			}
			out.Servers = append(out.Servers, view)
		}
		for _, f := range h.Failures() {
			if seen[f.Name] {
				continue
			}
			seen[f.Name] = true
			view := ServerView{
				Name: f.Name, Transport: f.Transport, Status: "failed", BuiltIn: f.Name == "codegraph", Error: f.Error,
			}
			if p, ok := configured[f.Name]; ok {
				view = withPluginConfig(view, p)
			} else if f.Name == "codegraph" && loadedCfg != nil {
				view = withCodegraphConfig(view, loadedCfg.Codegraph)
			}
			out.Servers = append(out.Servers, view)
		}
	}
	// Configured servers that are neither connected nor failed are either lazy
	// (deferred), background/eager (initializing), or toggled off this session.
	if len(configuredEntries) > 0 || loadedCfg != nil {
		for _, p := range configuredEntries {
			if seen[p.Name] {
				continue
			}
			if s, ok := disabled[p.Name]; ok {
				s.Status = "disabled"
				s = withPluginConfig(s, p)
				s.Error = ""
				out.Servers = append(out.Servers, s)
				retainedDisabled[p.Name] = s
				seen[p.Name] = true
				delete(disabled, p.Name)
				continue
			}
			status := "disabled"
			if p.ShouldAutoStart() {
				switch p.ResolvedTier() {
				case "background", "eager":
					status = "initializing"
				default:
					status = "deferred"
				}
			}
			out.Servers = append(out.Servers, withPluginConfig(ServerView{Name: p.Name, Status: status}, p))
			seen[p.Name] = true
		}
		if loadedCfg != nil && !seen["codegraph"] {
			status := "disabled"
			if loadedCfg.Codegraph.Enabled {
				status = "initializing"
			}
			if s, ok := disabled["codegraph"]; ok {
				s.Status = "disabled"
				s.Transport = "stdio"
				s.BuiltIn = true
				s = withCodegraphConfig(s, loadedCfg.Codegraph)
				s.Error = ""
				out.Servers = append(out.Servers, s)
				retainedDisabled["codegraph"] = s
				delete(disabled, "codegraph")
			} else {
				out.Servers = append(out.Servers, withCodegraphConfig(ServerView{Name: "codegraph", Status: status}, loadedCfg.Codegraph))
			}
			seen["codegraph"] = true
		}
		for _, p := range builtinmcp.Entries() {
			if configured[p.Name].Name != "" || seen[p.Name] {
				continue
			}
			enabled := builtInMCPEnabled(loadedCfg, p.Name)
			if s, ok := disabled[p.Name]; ok {
				s.Status = "disabled"
				s = withBuiltInMCPConfig(s, p, enabled)
				s.Error = ""
				out.Servers = append(out.Servers, s)
				retainedDisabled[p.Name] = s
				delete(disabled, p.Name)
			} else if enabled {
				out.Servers = append(out.Servers, withBuiltInMCPConfig(ServerView{Name: p.Name, Status: "deferred"}, p, true))
			} else {
				out.Servers = append(out.Servers, withBuiltInMCPConfig(ServerView{Name: p.Name, Status: "disabled"}, p, false))
			}
			seen[p.Name] = true
		}
	}
	out.Servers = orderServerViews(out.Servers, order)

	a.mu.Lock()
	if tab := a.activeTabLocked(); tab != nil {
		for name := range connected {
			delete(retainedDisabled, name)
		}
		tab.disabledMCP = retainedDisabled
		tab.mcpOrder = mergeServerOrder(tab.mcpOrder, out.Servers)
	}
	a.mu.Unlock()

	// Resolve the active profile's skill whitelist (if any) so each SkillView can
	// report whether it is in effect for the current mode. Mirrors boot.go's
	// profileSkillWhitelist: an empty/non-existent EnabledSkills list means "all
	// skills active"; a non-empty list is a whitelist and anything outside it is
	// hidden by the profile (Active=false). Must stay in lockstep with the index
	// logic in internal/boot/boot.go.
	var profileWhitelist map[string]bool
	if cfg, err := config.Load(); err == nil {
		if prof, perr := cfg.ResolveProfile(strings.TrimSpace(profileName)); perr == nil && len(prof.EnabledSkills) > 0 {
			profileWhitelist = make(map[string]bool, len(prof.EnabledSkills))
			for _, n := range prof.EnabledSkills {
				profileWhitelist[config.SkillNameKey(n)] = true
			}
		}
	}
	for _, s := range ctrl.AllSkills() {
		enabled := ctrl.SkillEnabled(s.Name)
		// A profile whitelist hides skills not named in it. User-disabled skills
		// are Enabled=false already; profile-hidden ones are Enabled=true but
		// Active=false.
		active := enabled
		if active && profileWhitelist != nil && !profileWhitelist[config.SkillNameKey(s.Name)] {
			active = false
		}
		out.Skills = append(out.Skills, SkillView{
			Name: s.Name, Description: s.Description,
			Scope: string(s.Scope), RunAs: string(s.RunAs),
			Enabled: enabled, Active: active,
		})
	}
	out.SkillRoots = skillRootsView()
	// Jiutian multimodal tools exposed in the skills page. image_understand
	// is intentionally NOT listed here — it's now globally unified through the
	// VLM degradation chain (qwen → 九天) configured via the model-page dropdown,
	// so there's no per-skill toggle for it. Only the 九天-specific generation
	// and video tools remain toggleable.
	if loadedCfg != nil {
		out.JiutianTools = []JiutianToolView{
			{Name: "image_generate", Description: "图片生成 — 文生图、图生图", Enabled: loadedCfg.Jiutian.ImageGenerate},
			{Name: "video_understand", Description: "视频理解 — 分析操作录屏、演示视频", Enabled: loadedCfg.Jiutian.VideoUnderstand},
		}
	}
	return out
}

// SetJiutianTool enables or disables a Jiutian multimodal tool in the config file.
// It goes through applyConfigChange (loadDesktopUserConfigForEdit → SaveTo → rebuild)
// so the write uses the same source path that Settings() reads from. The previous
// hand-rolled config.Load()+WriteFile read via LoadForRoot (merging project toml)
// but wrote the user file, so the success-path re-read in the UI rolled the toggle
// back to a stale value — the switch appeared unclickable.
func (a *App) SetJiutianTool(name string, enabled bool) error {
	return a.applyConfigChange(func(cfg *config.Config) error {
		switch name {
		case "image_understand":
			// No longer surfaced in the skills UI — image understanding is
			// globally unified via the VLM chain. The field is kept for
			// backward compat: it still toggles the in-conversation image
			// degradation path (openai provider), which is the only remaining
			// use. Future config schemas may rename it.
			cfg.Jiutian.ImageUnderstand = enabled
		case "image_generate":
			cfg.Jiutian.ImageGenerate = enabled
		case "video_understand":
			cfg.Jiutian.VideoUnderstand = enabled
		default:
			return fmt.Errorf("unknown jiutian tool: %s", name)
		}
		return nil
	})
}

// DreamRunView is one Dream/Distill run record for the settings panel.
type DreamRunView struct {
	Kind      string `json:"kind"`
	Trigger   string `json:"trigger"`
	StartedAt string `json:"startedAt"`
	Duration  string `json:"duration,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

// DreamStatusView is the self-evolution panel's data: the live config (master
// switch + cadence), the most recent run of each kind, and whether one is in
// flight. The "last run" times come from the on-disk dream_state.json so manual
// and automatic runs are both reflected.
type DreamStatusView struct {
	Enabled         bool           `json:"enabled"`
	DreamInterval   int            `json:"dreamInterval"`
	DistillInterval int            `json:"distillInterval"`
	DreamInFlight   bool           `json:"dreamInFlight"`
	DistillInFlight bool           `json:"distillInFlight"`
	LastDream       *DreamRunView  `json:"lastDream,omitempty"`
	LastDistill     *DreamRunView  `json:"lastDistill,omitempty"`
	History         []DreamRunView `json:"history"`
}

// DreamStatus returns the self-evolution status for the active session's panel.
func (a *App) DreamStatus() DreamStatusView {
	view := DreamStatusView{History: []DreamRunView{}}
	if cfg, err := config.Load(); err == nil {
		view.Enabled = cfg.Dream.Enabled
		view.DreamInterval = cfg.Dream.DreamIntervalDays()
		view.DistillInterval = cfg.Dream.DistillIntervalDays()
	} else {
		d := config.Default().Dream
		view.Enabled, view.DreamInterval, view.DistillInterval = d.Enabled, d.DreamIntervalDays(), d.DistillIntervalDays()
	}
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	view.DreamInFlight = agent.DreamInFlight(agent.KindDream)
	view.DistillInFlight = agent.DreamInFlight(agent.KindDistill)
	if ctrl == nil {
		return view
	}
	if r, ok := ctrl.LastDreamRun(agent.KindDream); ok {
		view.LastDream = dreamRunView(r)
	}
	if r, ok := ctrl.LastDreamRun(agent.KindDistill); ok {
		view.LastDistill = dreamRunView(r)
	}
	for _, r := range agent.DreamHistory(ctrl.SessionDir(), agent.KindDream) {
		view.History = append(view.History, *dreamRunView(r))
	}
	for _, r := range agent.DreamHistory(ctrl.SessionDir(), agent.KindDistill) {
		view.History = append(view.History, *dreamRunView(r))
	}
	return view
}

func dreamRunView(r agent.DreamRun) *DreamRunView {
	return &DreamRunView{
		Kind:      string(r.Kind),
		Trigger:   string(r.Trigger),
		StartedAt: r.StartedAt.UTC().Format(time.RFC3339),
		Duration:  r.Duration,
		Status:    r.Status,
		Error:     r.Error,
	}
}

// SetDreamEnabled toggles the self-evolution master switch in the config file.
func (a *App) SetDreamEnabled(enabled bool) error {
	return a.applyConfigChange(func(cfg *config.Config) error {
		cfg.SetDreamEnabled(enabled)
		return nil
	})
}

// SetDreamIntervals sets the Dream and Distill cadence (days) in the config.
func (a *App) SetDreamIntervals(dreamDays, distillDays int) error {
	return a.applyConfigChange(func(cfg *config.Config) error {
		return cfg.SetDreamIntervals(dreamDays, distillDays)
	})
}

// TriggerDream runs a Dream consolidation pass now (blocking) and returns the
// outcome. The frontend uses this for the "run now" button.
func (a *App) TriggerDream() (DreamRunView, error) {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return DreamRunView{}, fmt.Errorf("no active session")
	}
	r, ran := ctrl.TriggerDream(context.Background())
	if !ran {
		return DreamRunView{}, fmt.Errorf("dream did not run: %s", r.Error)
	}
	return *dreamRunView(r), nil
}

// TriggerDistill runs a Distill workflow-extraction pass now (blocking).
func (a *App) TriggerDistill() (DreamRunView, error) {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return DreamRunView{}, fmt.Errorf("no active session")
	}
	r, ran := ctrl.TriggerDistill(context.Background())
	if !ran {
		return DreamRunView{}, fmt.Errorf("distill did not run: %s", r.Error)
	}
	return *dreamRunView(r), nil
}

func withPluginConfig(v ServerView, p config.PluginEntry) ServerView {
	tt := p.Type
	if tt == "" {
		tt = "stdio"
	}
	v.Transport = tt
	v.Configured = true
	v.AutoStart = p.ShouldAutoStart()
	v.Tier = p.ResolvedTier()
	v.Command = p.Command
	v.Args = append([]string(nil), p.Args...)
	v.URL = p.URL
	v.AuthConfigured = mcpdiag.HasAuthConfig(p.Headers, p.Env, p.URL)
	if len(p.Env) > 0 {
		v.EnvKeys = make([]string, 0, len(p.Env))
		for k := range p.Env {
			v.EnvKeys = append(v.EnvKeys, k)
		}
		sort.Strings(v.EnvKeys)
	}
	auth := mcpdiag.DiagnoseAuth(v.Transport, v.Status, v.Error, v.URL, v.AuthConfigured)
	v.AuthStatus = auth.Status
	v.AuthURL = auth.URL
	return v
}

func withCodegraphConfig(v ServerView, c config.CodegraphConfig) ServerView {
	v.Name = "codegraph"
	v.Transport = "stdio"
	v.BuiltIn = true
	v.Configured = true
	v.AutoStart = c.ShouldAutoStart()
	v.Tier = c.ResolvedTier()
	v.AuthStatus = mcpdiag.AuthNone
	return v
}

func withBuiltInMCPConfig(v ServerView, p config.PluginEntry, enabled bool) ServerView {
	v = withPluginConfig(v, p)
	v.Name = p.Name
	v.BuiltIn = true
	v.Configured = true
	v.AutoStart = enabled
	v.AuthStatus = mcpdiag.AuthNone
	return v
}

func builtInMCPEnabled(cfg *config.Config, name string) bool {
	return cfg != nil && cfg.BuiltInMCP.Enabled(name)
}

func skillRootsView() []SkillRootView {
	cwd, _ := os.Getwd()
	cfg, _ := config.Load()
	userCfg := config.LoadForEdit(config.UserConfigPath())
	var custom []string
	var excluded []string
	maxDepth := 3
	if cfg != nil {
		custom = cfg.SkillCustomPaths()
		excluded = cfg.SkillExcludedPaths()
		maxDepth = cfg.SkillMaxDepth()
	}
	st := skill.New(skill.Options{ProjectRoot: cwd, CustomPaths: custom, ExcludedPaths: excluded, MaxDepth: maxDepth, DisableBuiltins: true, Stderr: io.Discard})
	counts := map[string]int{}
	skillItems := map[string][]SkillRootSkillView{}
	roots := st.Roots()
	for _, sk := range st.List() {
		root := skillDisplayRoot(sk, roots)
		counts[root]++
		skillItems[root] = append(skillItems[root], SkillRootSkillView{
			Name:        sk.Name,
			Description: sk.Description,
			Scope:       string(sk.Scope),
			RunAs:       string(sk.RunAs),
		})
	}
	for root := range skillItems {
		sort.Slice(skillItems[root], func(i, j int) bool {
			return skillItems[root][i].Name < skillItems[root][j].Name
		})
	}
	userConfigured := map[string]bool{}
	if userCfg != nil {
		for _, p := range userCfg.Skills.Paths {
			userConfigured[config.CanonicalSkillPath(p)] = true
		}
	}
	out := []SkillRootView{}
	for _, r := range roots {
		dir := config.CanonicalSkillPath(r.Dir)
		view := SkillRootView{
			Dir:        r.Dir,
			Scope:      string(r.Scope),
			Priority:   r.Priority + 1,
			Status:     string(r.Status),
			Configured: r.Scope == skill.ScopeCustom && userConfigured[dir],
			Removable:  true,
			Skills:     counts[dir],
			SkillItems: skillItems[dir],
		}
		out = append(out, view)
	}
	if userCfg != nil {
		for _, p := range userCfg.Skills.Paths {
			if rootActive(out, p) {
				continue
			}
			out = append(out, SkillRootView{
				Dir:        p,
				Scope:      string(skill.ScopeCustom),
				Status:     "inactive",
				Configured: true,
				Removable:  true,
				Warning:    "configured in user config but not active in this workspace; project [skills].paths may override it",
			})
		}
	}
	return out
}

func rootActive(roots []SkillRootView, path string) bool {
	want := config.CanonicalSkillPath(path)
	for _, r := range roots {
		if r.Scope == string(skill.ScopeCustom) && config.CanonicalSkillPath(r.Dir) == want {
			return true
		}
	}
	return false
}

// PickSkillFolder opens a directory picker for adding custom skill roots. It only
// returns a path; AddSkillPath performs normalization and writes config.
func (a *App) PickSkillFolder() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	cur, _ := os.Getwd()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Choose skills folder",
		DefaultDirectory: dialogDefaultDirectory(cur),
	})
	if err != nil || dir == "" {
		return "", err
	}
	return normalizeSkillPath(dir), nil
}

// PickDirectory opens a system directory picker and returns the chosen path.
// Used by settings fields that would otherwise be "fill-in-the-blank" path
// inputs (sandbox workspace_root, bot workspace_root, allow_write paths): a
// folder picker is far less error-prone than hand-typing an absolute path.
// Returns ("", nil) if the user cancels. title localizes the dialog caption.
func (a *App) PickDirectory(title string) (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	cur, _ := os.Getwd()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: dialogDefaultDirectory(cur),
	})
	if err != nil || dir == "" {
		return "", err
	}
	return dir, nil
}

// AddSkillPath adds a custom skill root to the user config and rebuilds the
// controller so the skills index and slash menu reflect it immediately.
func (a *App) AddSkillPath(path string) error {
	path = normalizeSkillPath(path)
	workspaceRoot := a.activeWorkspaceRoot()
	return a.applyConfigChange(func(c *config.Config) error {
		if isConventionSkillRoot(path, workspaceRoot) {
			return c.RestoreSkillPath(path)
		}
		return c.AddSkillPath(path)
	})
}

// RemoveSkillPath removes a skill source from the user config and rebuilds. For
// convention roots, it records a pseudo-delete in excluded_paths.
func (a *App) RemoveSkillPath(path string) error {
	path = normalizeSkillPath(path)
	return a.applyConfigChange(func(c *config.Config) error {
		removed, err := c.RemoveSkillPath(path)
		if err != nil || removed {
			return err
		}
		return c.ExcludeSkillPath(path)
	})
}

// RefreshSkills rebuilds the controller without changing config, reloading skill
// discovery, the system prompt index, and slash completions.
func (a *App) RefreshSkills() error {
	return a.rebuild()
}

// SetSkillEnabled persists a skill toggle and rebuilds the controller so the
// prompt index, slash menu, and skill tools reflect it immediately.
func (a *App) SetSkillEnabled(name string, enabled bool) error {
	return a.applyConfigChange(func(c *config.Config) error {
		return c.SetSkillEnabled(name, enabled)
	})
}

func normalizeSkillPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				path = home
			} else {
				path = filepath.Join(home, path[2:])
			}
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	info, err := os.Stat(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if info.Mode().IsRegular() {
		if filepath.Base(path) == skill.SkillFile {
			return filepath.Clean(filepath.Dir(filepath.Dir(path)))
		}
		return filepath.Clean(filepath.Dir(path))
	}
	if info.IsDir() {
		if _, err := os.Stat(filepath.Join(path, skill.SkillFile)); err == nil {
			return filepath.Clean(filepath.Dir(path))
		}
	}
	return filepath.Clean(path)
}

func isConventionSkillRoot(path, workspaceRoot string) bool {
	want := config.CanonicalSkillPath(path)
	if want == "" {
		return false
	}
	bases := []string{workspaceRoot}
	if home, err := os.UserHomeDir(); err == nil {
		bases = append(bases, home)
	}
	for _, base := range bases {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		for _, dir := range config.ConventionDirs {
			if want == config.CanonicalSkillPath(filepath.Join(base, dir, skill.SkillsDirname)) {
				return true
			}
		}
	}
	return false
}

func skillRootPath(path string) string {
	if filepath.Base(path) == skill.SkillFile {
		return filepath.Dir(path)
	}
	return path
}

func skillDisplayRoot(sk skill.Skill, roots []skill.Root) string {
	cleanPath := filepath.Clean(sk.Path)
	for _, r := range roots {
		if r.Scope != sk.Scope {
			continue
		}
		cleanRoot := filepath.Clean(r.Dir)
		prefix := cleanRoot + string(filepath.Separator)
		if cleanPath == cleanRoot || strings.HasPrefix(cleanPath, prefix) {
			return config.CanonicalSkillPath(r.Dir)
		}
	}
	return config.CanonicalSkillPath(filepath.Dir(skillRootPath(sk.Path)))
}

// MCPServerInput is the drawer's "add server" form. Transport is "stdio" (Command
// + Args + Env) or "http"/"sse" (URL). Mirrors config.PluginEntry's writable shape.
type MCPServerInput struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	URL       string            `json:"url"`
	Env       map[string]string `json:"env"`
}

// AddMCPServer connects a server live and persists it to config (Customize → MCP →
// Add). Returns the number of tools it exposed.
func (a *App) AddMCPServer(in MCPServerInput) (int, error) {
	ctrl := a.activeCtrl()
	if ctrl == nil {
		return 0, fmt.Errorf("no active session")
	}
	entry := config.PluginEntry{
		Name:    in.Name,
		Type:    normalizeMCPTransport(in.Transport),
		Command: in.Command,
		Args:    in.Args,
		URL:     in.URL,
		Env:     in.Env,
	}
	entry, _ = config.NormalizePluginCommandLine(entry)
	if err := a.saveDesktopMCPServer(entry); err != nil {
		return 0, err
	}
	return ctrl.ConnectMCPServer(entry)
}

// UpdateMCPServer edits a persisted external MCP server. The name is the stable
// identity; callers must remove + add if they want to rename a server.
func (a *App) UpdateMCPServer(name string, in MCPServerInput) error {
	if name == "codegraph" {
		return fmt.Errorf("codegraph is built in; configure it with [codegraph]")
	}
	ctrl := a.activeCtrl()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	if strings.TrimSpace(in.Name) != "" && strings.TrimSpace(in.Name) != name {
		return fmt.Errorf("renaming MCP servers is not supported; remove and add a new server")
	}
	updated, found, err := a.desktopMCPServerForEdit(name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no configured MCP server named %q", name)
	}
	updated.Type = normalizeMCPTransport(in.Transport)
	updated.Command = strings.TrimSpace(in.Command)
	updated.Args = append([]string(nil), in.Args...)
	updated.URL = strings.TrimSpace(in.URL)
	updated.Tier = ""
	if in.Env != nil {
		updated.Env = in.Env
	}
	updated, _ = config.NormalizePluginCommandLine(updated)
	if updated.Type == "stdio" {
		updated.URL = ""
	} else {
		updated.Command = ""
		updated.Args = nil
	}
	if err := a.saveDesktopMCPServer(updated); err != nil {
		return err
	}

	a.mu.RLock()
	tab := a.activeTabLocked()
	sessionDisabled := false
	if tab != nil {
		_, sessionDisabled = tab.disabledMCP[name]
	}
	a.mu.RUnlock()
	wasConnected := mcpConnected(ctrl, name)
	wasFailed := mcpFailed(ctrl, name)
	if wasConnected {
		ctrl.DisconnectMCPServer(name)
	}
	if !sessionDisabled && (wasConnected || wasFailed || updated.ResolvedTier() != "lazy") {
		if _, err := ctrl.ConnectMCPServer(updated); err != nil {
			recordMCPFailure(ctrl, updated, err)
			return nil
		}
	}
	return nil
}

// RemoveMCPServer disconnects a live server and drops it from config (the row's ✕).
func (a *App) RemoveMCPServer(name string) error {
	if name == "codegraph" {
		return fmt.Errorf("codegraph is built in; it cannot be removed")
	}
	// Use activeCtrl (self-locking, returns a snapshot) instead of activeTab +
	// tab.Ctrl: the latter reads tab.Ctrl outside the lock, racing with rebuild()
	// which sets tab.Ctrl = nil — a nil deref panic.
	ctrl := a.activeCtrl()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	disconnected := ctrl.DisconnectMCPServer(name)
	removed, err := a.removeDesktopMCPServer(name)
	if err != nil {
		return err
	}
	if disconnected || removed {
		a.mu.Lock()
		if tab := a.activeTabLocked(); tab != nil {
			delete(tab.disabledMCP, name)
			tab.mcpOrder = removeServerOrder(tab.mcpOrder, name)
		}
		a.mu.Unlock()
		return nil
	}
	return fmt.Errorf("no MCP server named %q", name)
}

// ReconnectMCPServer disconnects the server if it is already connected (to force
// a fresh handshake and tool re-registration), then reconnects.  Failures are
// recorded on the Host so the UI can render them.
func (a *App) ReconnectMCPServer(name string) error {
	// Snapshot ctrl + workspaceRoot under the lock; accessing tab.Ctrl outside
	// the lock races rebuild() → nil panic (TOCTOU, same class as RemoveMCPServer).
	a.mu.RLock()
	tab := a.activeTabLocked()
	ctrl := (*control.Controller)(nil)
	root := ""
	if tab != nil {
		ctrl = tab.Ctrl
		root = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	if mcpConnected(ctrl, name) {
		ctrl.DisconnectMCPServer(name)
	}
	_, err := a.connectConfiguredMCPServer(ctrl, root, name)
	if err != nil {
		recordMCPFailure(ctrl, config.PluginEntry{Name: name}, err)
		return err
	}
	a.mu.Lock()
	if tab := a.activeTabLocked(); tab != nil {
		delete(tab.disabledMCP, name)
	}
	a.mu.Unlock()
	return nil
}

// ClearMCPServerAuthentication removes local auth-like config for one MCP and
// clears the current session's cached connection failure. It does not remove the
// server itself or try to sign the user out of the third-party browser session.
func (a *App) ClearMCPServerAuthentication(name string) error {
	if name == "codegraph" {
		return fmt.Errorf("codegraph is built in; it has no stored MCP authentication")
	}
	ctrl := a.activeCtrl()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	if _, _, _, err := config.ClearPluginAuthenticationInSource(name); err != nil {
		return err
	}
	ctrl.DisconnectMCPServer(name)
	if h := ctrl.Host(); h != nil {
		h.ClearFailure(name)
	}
	return nil
}

// SetMCPServerEnabled is the connector toggle: on reconnects a configured server
// for this session, off disconnects it (config untouched either way — like Claude
// Code's per-conversation enable/disable, it resets on the next session start).
func (a *App) SetMCPServerEnabled(name string, enabled bool) error {
	ctrl, root := a.activeCtrlAndRoot()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	tab := a.activeTab()
	if tab == nil {
		return fmt.Errorf("no active session")
	}
	if name == "codegraph" {
		return a.setCodegraphEnabled(enabled)
	}
	configuredEntry, hasConfiguredEntry, err := a.desktopMCPServerForEdit(name)
	if err != nil {
		return err
	}
	if builtinmcp.IsBuiltIn(name) && !hasConfiguredEntry {
		return a.setBuiltinMCPEnabled(name, enabled)
	}
	_ = configuredEntry
	if enabled {
		_, err := a.connectConfiguredMCPServer(ctrl, root, name)
		if err == nil {
			a.mu.Lock()
			delete(tab.disabledMCP, name)
			a.mu.Unlock()
		}
		return err
	}
	if s, ok := findMCPServerView(ctrl, name); ok {
		s.Status = "disabled"
		s.Error = ""
		a.mu.Lock()
		if tab.disabledMCP == nil {
			tab.disabledMCP = map[string]ServerView{}
		}
		tab.disabledMCP[name] = s
		tab.mcpOrder = mergeServerOrder(tab.mcpOrder, []ServerView{s})
		a.mu.Unlock()
	}
	ctrl.DisconnectMCPServer(name)
	return nil
}

// connectConfiguredMCPServer connects a configured MCP server using an
// already-snapshotted controller + workspace root, so callers avoid holding a
// *WorkspaceTab (and its TOCTOU-prone tab.Ctrl) across the call.
func (a *App) connectConfiguredMCPServer(ctrl *control.Controller, root, name string) (int, error) {
	if ctrl == nil {
		return 0, fmt.Errorf("no active session")
	}
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return 0, err
	}
	for _, p := range cfg.Plugins {
		if p.Name == name {
			return ctrl.ConnectMCPServer(p)
		}
	}
	if name == "codegraph" {
		return ctrl.ConnectCodegraphMCPServer(cfg)
	}
	return 0, fmt.Errorf("no configured MCP server named %q", name)
}

// SetMCPServerTier is kept for old desktop bindings. New config writes drop the
// retired tier field; for CodeGraph this now means "enable and start in the
// background".
func (a *App) SetMCPServerTier(name, tier string) error {
	if name == "codegraph" {
		return a.setCodegraphTier(tier)
	}
	tier = normalizeMCPTier(tier)
	updated, found, err := a.desktopMCPServerForEdit(name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no configured MCP server named %q", name)
	}
	updated.Tier = tier
	if !updated.ShouldAutoStart() {
		on := true
		updated.AutoStart = &on
	}
	if err := a.saveDesktopMCPServer(updated); err != nil {
		return err
	}
	ctrl := a.activeCtrl()
	if tier != "lazy" && ctrl != nil && !mcpConnected(ctrl, name) {
		if _, err := ctrl.ConnectMCPServer(updated); err != nil {
			recordMCPFailure(ctrl, updated, err)
			return nil
		}
		tab := a.activeTab()
		if tab != nil {
			a.mu.Lock()
			delete(tab.disabledMCP, name)
			a.mu.Unlock()
		}
	}
	return nil
}

func (a *App) setCodegraphEnabled(enabled bool) error {
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	ctrl, _ := a.activeCtrlAndRoot()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	tab := a.activeTab()
	if tab == nil {
		return fmt.Errorf("no active session")
	}
	cfg.Codegraph.Enabled = enabled
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	if err := a.syncProjectCodegraphOverride(cfg.Codegraph); err != nil {
		return err
	}
	if enabled {
		a.mu.Lock()
		delete(tab.disabledMCP, "codegraph")
		a.mu.Unlock()
		if !mcpConnected(ctrl, "codegraph") {
			if _, err := ctrl.ConnectCodegraphMCPServer(cfg); err != nil {
				recordCodegraphFailure(ctrl, cfg.Codegraph, err)
				return nil
			}
		}
		return nil
	}
	if h := ctrl.Host(); h != nil {
		h.ClearFailure("codegraph")
	}
	ctrl.DisconnectMCPServer("codegraph")
	s := withCodegraphConfig(ServerView{Name: "codegraph", Status: "disabled"}, cfg.Codegraph)
	a.mu.Lock()
	if tab.disabledMCP == nil {
		tab.disabledMCP = map[string]ServerView{}
	}
	tab.disabledMCP["codegraph"] = s
	tab.mcpOrder = mergeServerOrder(tab.mcpOrder, []ServerView{s})
	a.mu.Unlock()
	return nil
}

// setBuiltinMCPEnabled toggles a built-in MCP server (e.g. context7) on or off.
// It persists the change to the user config and connects/disconnects for the
// current session, following the same pattern as setCodegraphEnabled.
func (a *App) setBuiltinMCPEnabled(name string, enabled bool) error {
	entry, ok := builtinmcp.Entry(name)
	if !ok {
		return fmt.Errorf("no built-in MCP server named %q", name)
	}
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	if !cfg.BuiltInMCP.SetEnabled(name, enabled) {
		return fmt.Errorf("no built-in MCP server named %q", name)
	}
	ctrl, _ := a.activeCtrlAndRoot()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	tab := a.activeTab()
	if tab == nil {
		return fmt.Errorf("no active session")
	}
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	if err := a.syncProjectBuiltInMCPOverride(cfg.BuiltInMCP); err != nil {
		return err
	}
	if enabled {
		a.mu.Lock()
		delete(tab.disabledMCP, name)
		a.mu.Unlock()
		if !mcpConnected(ctrl, name) {
			_, err := ctrl.ConnectMCPServer(entry)
			if err != nil {
				recordMCPFailure(ctrl, entry, err)
				return nil
			}
		}
		return nil
	}
	if h := ctrl.Host(); h != nil {
		h.ClearFailure(name)
	}
	ctrl.DisconnectMCPServer(name)
	s := withBuiltInMCPConfig(ServerView{Name: name, Status: "disabled"}, entry, false)
	a.mu.Lock()
	if tab.disabledMCP == nil {
		tab.disabledMCP = map[string]ServerView{}
	}
	tab.disabledMCP[name] = s
	tab.mcpOrder = mergeServerOrder(tab.mcpOrder, []ServerView{s})
	a.mu.Unlock()
	return nil
}

func (a *App) syncProjectBuiltInMCPOverride(c config.BuiltInMCPConfig) error {
	path := projectConfigPathForRoot(a.activeWorkspaceRoot())
	userPath := config.UserConfigPath()
	if path == "" || sameConfigPath(path, userPath) {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cfg := config.LoadForEdit(path)
	cfg.BuiltInMCP = c
	return cfg.SaveTo(path)
}

func (a *App) setCodegraphTier(_ string) error {
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	cfg.Codegraph.Enabled = true
	cfg.Codegraph.Tier = ""
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	if err := a.syncProjectCodegraphOverride(cfg.Codegraph); err != nil {
		return err
	}
	ctrl := a.activeCtrl()
	if ctrl == nil {
		return nil
	}
	tab := a.activeTab()
	if tab != nil {
		a.mu.Lock()
		delete(tab.disabledMCP, "codegraph")
		a.mu.Unlock()
	}
	if !mcpConnected(ctrl, "codegraph") {
		if _, err := ctrl.ConnectCodegraphMCPServer(cfg); err != nil {
			recordCodegraphFailure(ctrl, cfg.Codegraph, err)
			return nil
		}
	}
	return nil
}

func (a *App) desktopMCPServerForEdit(name string) (config.PluginEntry, bool, error) {
	cfg, _, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return config.PluginEntry{}, false, err
	}
	if p, ok := findPluginEntry(cfg.Plugins, name); ok {
		return p, true, nil
	}
	if merged, err := config.LoadForRoot(a.activeWorkspaceRoot()); err == nil {
		if p, ok := findPluginEntry(merged.Plugins, name); ok {
			return p, true, nil
		}
	}
	return config.PluginEntry{}, false, nil
}

func (a *App) saveDesktopMCPServer(entry config.PluginEntry) error {
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	if err := cfg.UpsertPlugin(entry); err != nil {
		return err
	}
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	_, err = a.removeProjectMCPOverride(entry.Name)
	return err
}

func (a *App) removeDesktopMCPServer(name string) (bool, error) {
	removed := false
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return false, err
	}
	if cfg.RemovePlugin(name) {
		removed = true
		if err := cfg.SaveTo(path); err != nil {
			return false, err
		}
	}
	projectRemoved, err := a.removeProjectMCPOverride(name)
	if err != nil {
		return removed, err
	}
	return removed || projectRemoved, nil
}

func (a *App) removeProjectMCPOverride(name string) (bool, error) {
	path := projectConfigPathForRoot(a.activeWorkspaceRoot())
	userPath := config.UserConfigPath()
	if path == "" || sameConfigPath(path, userPath) {
		return false, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	cfg := config.LoadForEdit(path)
	if !cfg.RemovePlugin(name) {
		return false, nil
	}
	if err := cfg.SaveTo(path); err != nil {
		return false, err
	}
	return true, nil
}

func (a *App) syncProjectCodegraphOverride(c config.CodegraphConfig) error {
	path := projectConfigPathForRoot(a.activeWorkspaceRoot())
	userPath := config.UserConfigPath()
	if path == "" || sameConfigPath(path, userPath) {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cfg := config.LoadForEdit(path)
	cfg.Codegraph = c
	return cfg.SaveTo(path)
}

func findPluginEntry(entries []config.PluginEntry, name string) (config.PluginEntry, bool) {
	for _, p := range entries {
		if p.Name == name {
			return p, true
		}
	}
	return config.PluginEntry{}, false
}

func normalizeMCPTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "eager":
		return "eager"
	case "background":
		return "background"
	case "":
		return "background"
	default:
		return "lazy"
	}
}

func normalizeMCPTransport(transport string) string {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "http", "streamable-http":
		return "http"
	case "sse":
		return "sse"
	default:
		return "stdio"
	}
}

func mcpConnected(ctrl *control.Controller, name string) bool {
	if ctrl == nil || ctrl.Host() == nil {
		return false
	}
	for _, s := range ctrl.Host().Servers() {
		if s.Name == name {
			return true
		}
	}
	return false
}

func mcpFailed(ctrl *control.Controller, name string) bool {
	if ctrl == nil || ctrl.Host() == nil {
		return false
	}
	for _, f := range ctrl.Host().Failures() {
		if f.Name == name {
			return true
		}
	}
	return false
}

func recordMCPFailure(ctrl *control.Controller, e config.PluginEntry, err error) {
	if ctrl == nil || ctrl.Host() == nil || err == nil {
		return
	}
	exp := e.ExpandedPlugin()
	ctrl.Host().RecordFailure(plugin.Spec{
		Name:    exp.Name,
		Type:    exp.Type,
		Command: exp.Command,
		Args:    exp.Args,
		Env:     exp.Env,
		URL:     exp.URL,
		Headers: exp.Headers,
	}, err)
}

func recordCodegraphFailure(ctrl *control.Controller, c config.CodegraphConfig, err error) {
	if ctrl == nil || ctrl.Host() == nil || err == nil {
		return
	}
	cmd := strings.TrimSpace(c.Path)
	if cmd == "" {
		cmd = "codegraph"
	}
	ctrl.Host().RecordFailure(plugin.Spec{
		Name:    "codegraph",
		Type:    "stdio",
		Command: cmd,
		Args:    []string{"serve", "--mcp"},
	}, err)
}

func findMCPServerView(ctrl *control.Controller, name string) (ServerView, bool) {
	if ctrl == nil || ctrl.Host() == nil {
		return ServerView{}, false
	}
	for _, s := range ctrl.Host().Servers() {
		if s.Name == name {
			return ServerView{
				Name: s.Name, Transport: s.Transport, Status: "connected",
				Tools: s.Tools, Prompts: s.Prompts, Resources: s.Resources,
				ToolList: pluginToolsToView(s.ToolList),
			}, true
		}
	}
	for _, f := range ctrl.Host().Failures() {
		if f.Name == name {
			return ServerView{Name: f.Name, Transport: f.Transport, Status: "failed", Error: f.Error}, true
		}
	}
	return ServerView{}, false
}

func pluginToolsToView(tools []plugin.ToolInfo) []ToolView {
	if len(tools) == 0 {
		return nil
	}
	out := make([]ToolView, 0, len(tools))
	for _, t := range tools {
		out = append(out, ToolView{Name: t.Name, Description: t.Description})
	}
	return out
}

func orderServerViews(servers []ServerView, order []string) []ServerView {
	pos := make(map[string]int, len(order))
	for i, name := range order {
		pos[name] = i
	}
	sort.SliceStable(servers, func(i, j int) bool {
		pi, iok := pos[servers[i].Name]
		pj, jok := pos[servers[j].Name]
		switch {
		case iok && jok:
			return pi < pj
		case iok:
			return true
		case jok:
			return false
		default:
			return false
		}
	})
	return servers
}

func mergeServerOrder(order []string, servers []ServerView) []string {
	seen := make(map[string]bool, len(order)+len(servers))
	next := make([]string, 0, len(order)+len(servers))
	for _, name := range order {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		next = append(next, name)
	}
	for _, s := range servers {
		if s.Name == "" || seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		next = append(next, s.Name)
	}
	return next
}

func removeServerOrder(order []string, name string) []string {
	if name == "" || len(order) == 0 {
		return order
	}
	next := order[:0]
	for _, n := range order {
		if n != name {
			next = append(next, n)
		}
	}
	return next
}

// ModelInfo is one (provider, model) the bottom switcher can pick. Ref ("provider/
// model") is what SetModel takes; Provider/Model are for display.
type ModelInfo struct {
	Ref      string `json:"ref"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Current  bool   `json:"current"`
}

type EffortInfo struct {
	Supported bool     `json:"supported"`
	Current   string   `json:"current"`
	Default   string   `json:"default"`
	Levels    []string `json:"levels"`
}

// Models flattens the configured providers into their (provider, model) pairs —
// the switcher's options — marking the active one. A vendor with a `models` list
// yields one entry per model, all sharing the same endpoint/key. Unconfigured
// providers are skipped. Result is non-nil: the frontend reads .length, so a nil
// slice (JSON null) would crash the switcher on an empty list.
func (a *App) Models() []ModelInfo {
	return a.ModelsForTab("")
}

func (a *App) ModelsForTab(tabID string) []ModelInfo {
	a.mu.RLock()
	curModel := ""
	workspaceRoot := ""
	if tab := a.tabByIDLocked(tabID); tab != nil {
		curModel = tab.model
		workspaceRoot = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	cfg, err := config.LoadForRoot(workspaceRoot)
	if err != nil {
		return []ModelInfo{}
	}
	if entry, ok := cfg.ResolveModel(curModel); ok {
		curModel = entry.Name + "/" + entry.Model
		// normalize aliases to main provider so they highlight correctly in UI
		if entry.Name == "moma-alias1" || entry.Name == "moma-alias2" {
			curModel = "moma/" + entry.Model
		}
	}
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	out := []ModelInfo{}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !modelProviderAccessAllowed(access, p.Name) || !p.Configured() {
			continue
		}
		if p.Name == "custom" || p.Name == "anthropic" {
			continue
		}
		for _, m := range p.ChatModelList() {
			ref := p.Name + "/" + m
			out = append(out, ModelInfo{Ref: ref, Provider: p.Name, Model: m, Current: ref == curModel})
		}
	}
	return out
}

func modelProviderAccessAllowed(access map[string]bool, name string) bool {
	if len(access) == 0 {
		return true
	}
	return access[strings.TrimSpace(name)]
}

// SetModel switches the active model and carries the current conversation into the
// new model's session, so the chat continues seamlessly and subsequent turns use
// the new model. No-op if name is already active or the controller is down.
func (a *App) SetModel(name string) error {
	return a.SetModelForTab("", name)
}

func (a *App) SetModelForTab(tabID, name string) error {
	if a.ctx == nil || name == "" {
		return nil
	}
	tab := a.tabByID(tabID)
	if tab == nil {
		return nil
	}
	// Snapshot old controller + scalar fields under RLock to avoid TOCTOU.
	a.mu.RLock()
	oldCtrl := tab.Ctrl
	oldModel := tab.model
	oldEffort := tab.effort
	root := tab.WorkspaceRoot
	sink := tab.sink
	a.mu.RUnlock()

	if name == oldModel {
		return nil
	}
	if oldCtrl != nil && oldCtrl.Running() {
		return fmt.Errorf("finish or cancel the current turn before changing model")
	}
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return err
	}
	entry, ok := cfg.ResolveModel(name)
	if !ok {
		return fmt.Errorf("unknown model %q", name)
	}
	if !modelProviderAccessAllowed(providerAccessSet(cfg.Desktop.ProviderAccess), entry.Name) {
		return fmt.Errorf("model %q is not available because provider %q is not added", name, entry.Name)
	}
	name = entry.Name + "/" + entry.Model
	effortOverride := cloneStringPtr(oldEffort)
	if effortOverride != nil {
		normalized, err := config.NormalizeEffort(entry, config.EffortDisplay(&config.ProviderEntry{Effort: *effortOverride}))
		if err != nil {
			effortOverride = nil
		} else {
			effortOverride = &normalized
		}
	}

	var carried []provider.Message
	prevPath := ""
	// Acquire the shared host BEFORE closing the old controller so the refcount
	// never hits zero mid-switch (which would tear down subprocesses the new
	// controller is about to reuse). The matching release runs after the old
	// controller's Close, keeping the net reference count unchanged.
	sharedHost := a.acquireSharedHost(root)
	if oldCtrl != nil {
		prevPath = oldCtrl.SessionPath()
		_ = oldCtrl.Snapshot()
		carried = oldCtrl.History()
	}

	newCtrl, err := boot.Build(a.bootContext(), boot.Options{
		Model:          name,
		RequireKey:     false,
		Sink:           sink,
		WorkspaceRoot:  root,
		SessionDir:     tabSessionDir(tab),
		EffortOverride: cloneStringPtr(effortOverride),
		Host:           sharedHost,
	})
	if err != nil {
		a.releaseSharedHost(root) // Build failed: drop our acquire
		return err
	}
	a.bindControllerDisplayRecorder(newCtrl)
	a.bindControllerContextFilter(newCtrl)
	a.mu.Lock()
	if tab.Ctrl == oldCtrl {
		tab.Ctrl = newCtrl
		if oldCtrl != nil {
			oldCtrl.Close()
			a.releaseSharedHost(root) // drop the old controller's reference
		}
	} else {
		// Another goroutine swapped this tab's controller between our RUnlock and
		// Lock (e.g. a concurrent rebuild from a config change), so the
		// concurrently-installed controller supersedes our snapshot. Rather than
		// overwrite it (and leak the concurrent controller's host ref), roll back
		// this attempt: discard newCtrl and release the host ref we acquired for
		// it. Surface a clear error so the user knows the switch didn't take and
		// can retry, rather than silently no-op'ing.
		a.mu.Unlock()
		newCtrl.Close()
		a.releaseSharedHost(root)
		return errors.New("model switch aborted: this tab was concurrently rebuilt (e.g. by a config change); please retry")
	}
	tab.model = name
	tab.effort = cloneStringPtr(effortOverride)
	tab.Label = newCtrl.Label()
	a.saveTabsLocked()
	a.mu.Unlock()
	newCtrl.EnableInteractiveApproval()
	applyTabModeToController(newCtrl, tab.mode)
	applyTabToolApprovalModeToController(newCtrl, tab.toolApprovalMode)
	applyTabRagScopeToController(newCtrl, tab.ragScope)
	newCtrl.SetGoal(tab.goal)

	path := agent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	if len(carried) > 0 {
		newCtrl.Resume(&agent.Session{Messages: carried}, path)
	} else if path != "" {
		newCtrl.SetSessionPath(path)
	}
	a.persistTabSessionPath(tab, path)
	return nil
}

// ProfileInfo describes one selectable product profile for the frontend picker.
type ProfileInfo struct {
	Name          string `json:"name"`
	DisplayName   string `json:"displayName"`
	WorkspaceType string `json:"workspaceType,omitempty"`
}

// Profile returns the active profile name for the current tab ("dev" | "cowork").
// Empty/unprofiled resolves to "dev" so the frontend always sees a concrete mode.
func (a *App) Profile() string {
	return a.ProfileForTab("")
}

// ProfileForTab returns the active profile name for the given tab (current tab
// when tabID is ""). Empty resolves to "dev".
func (a *App) ProfileForTab(tabID string) string {
	tab := a.tabByID(tabID)
	if tab == nil {
		return config.ProfileDev
	}
	name := strings.TrimSpace(tab.profile)
	if name == "" {
		return config.ProfileDev
	}
	return strings.ToLower(name)
}

// Profiles lists every profile the user can switch to (builtins + configured),
// for the profile picker. Names are lowercased to match resolution.
func (a *App) Profiles() []ProfileInfo {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		cfg = config.Default()
	}
	seen := map[string]bool{}
	var out []ProfileInfo
	// Builtins first so dev is always available even with an empty config.
	for _, b := range config.DefaultProfiles() {
		k := strings.ToLower(b.Name)
		if !seen[k] {
			seen[k] = true
			out = append(out, ProfileInfo{Name: k, DisplayName: b.DisplayName, WorkspaceType: b.WorkspaceType})
		}
	}
	for _, p := range cfg.Profiles {
		k := strings.ToLower(strings.TrimSpace(p.Name))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		disp := strings.TrimSpace(p.DisplayName)
		if disp == "" {
			disp = k
		}
		out = append(out, ProfileInfo{Name: k, DisplayName: disp, WorkspaceType: p.WorkspaceType})
	}
	return out
}

// SwitchProfile switches the current tab to a product profile (dev/cowork) and
// rebuilds its controller so the new model/prompt/skill/plugin bundle takes
// effect immediately, carrying the conversation history across. Equivalent to
// SetModel but for a whole profile bundle instead of a single model. No-op when
// the profile is already active; refuses while a turn is running.
func (a *App) SwitchProfile(name string) error {
	return a.SwitchProfileForTab("", name)
}

// SwitchProfileForTab switches a specific tab (current when tabID is "") to the
// named profile. The rebuild flow mirrors SetModelForTab: acquire the shared MCP
// host so subprocesses survive, snapshot history, close the old controller,
// boot.Build with the profile, rebind, and Resume the history onto the fresh
// session. Emits "profile:changed" so the frontend swaps layouts.
func (a *App) SwitchProfileForTab(tabID, name string) error {
	if a.ctx == nil {
		return nil
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = config.ProfileDev
	}
	tab := a.tabByID(tabID)
	if tab == nil {
		return nil
	}
	// Expert-session tabs are always cowork and cannot be switched to dev —
	// switching would rebuild the controller and lose the IsExpertSession flag.
	if tab.IsExpertSession {
		return fmt.Errorf("cannot switch profile of an expert-session tab")
	}
	// Snapshot old controller + scalar fields under RLock to avoid TOCTOU.
	a.mu.RLock()
	oldCtrl := tab.Ctrl
	oldProfile := tab.profile
	oldModel := tab.model
	oldEffort := tab.effort
	root := tab.WorkspaceRoot
	sink := tab.sink
	a.mu.RUnlock()

	current := strings.ToLower(strings.TrimSpace(oldProfile))
	if current == "" {
		current = config.ProfileDev
	}
	if name == current {
		return nil
	}
	if oldCtrl != nil && oldCtrl.Running() {
		return fmt.Errorf("finish or cancel the current turn before switching profile")
	}
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return err
	}
	prof, err := cfg.ResolveProfile(name)
	if err != nil {
		return err
	}

	// Resolve the effective model: keep the tab's current model unless the profile
	// pins one. We pass BOTH to boot.Build — Profile for the bundle, Model as the
	// explicit winner. This lets a user pick a model inside cowork and keep it on
	// switch-back, while a profile that pins a model still overrides the default.
	modelName := strings.TrimSpace(oldModel)
	if modelName == "" {
		if strings.TrimSpace(prof.Model) != "" {
			modelName = prof.Model
		} else {
			modelName = cfg.DefaultModel
		}
	}
	// Normalize effort against the resolved model, mirroring SetModelForTab. A
	// profile switch can change the model (via prof.Model), so an effort level
	// valid for the old model (e.g. "high" on a MoMA model) may be unsupported on
	// the new one — drop it then rather than passing an invalid level that boot
	// would silently keep.
	effortOverride := cloneStringPtr(oldEffort)
	if entry, ok := cfg.ResolveModel(modelName); ok && effortOverride != nil {
		if normalized, err := config.NormalizeEffort(entry, config.EffortDisplay(&config.ProviderEntry{Effort: *effortOverride})); err != nil {
			effortOverride = nil
		} else {
			effortOverride = &normalized
		}
	}

	// Acquire the shared host BEFORE closing the old controller so the refcount
	// never hits zero mid-switch (subprocess teardown). Same invariant as
	// SetModelForTab — the matching release runs after the old Close.
	sharedHost := a.acquireSharedHost(root)
	if oldCtrl != nil {
		_ = oldCtrl.Snapshot() // flush the in-memory turn buffer to the old file
		// Hard profile isolation: the old conversation is NOT carried across —
		// it stays on disk under the old profile (see comment further down).
	}

	// We CANNOT use tabSessionDir(tab) here because it prefers
	// tab.Ctrl.SessionDir(), and tab.Ctrl is still the OLD controller until the
	// swap below — so it would resolve the OLD profile's dir and break hard
	// isolation. Pass the new profile's dir explicitly. tab.profile itself is
	// flipped inside a.mu below (not here) so concurrent readers never see a
	// half-switched tab.
	newSessionDir := desktopSessionDirFor(root, name)

	newCtrl, err := boot.Build(a.bootContext(), boot.Options{
		Model:          modelName,
		RequireKey:     false,
		Sink:           sink,
		WorkspaceRoot:  root,
		SessionDir:     newSessionDir,
		EffortOverride: effortOverride, // already cloned+normalized above; no double-clone
		Host:           sharedHost,
		Profile:        prof,
	})
	if err != nil {
		// No state was mutated yet (tab.profile not flipped until the Lock below),
		// so there's nothing to roll back — just release the acquired host.
		a.releaseSharedHost(root)
		return err
	}
	a.bindControllerDisplayRecorder(newCtrl)
	a.bindControllerContextFilter(newCtrl)
	a.mu.Lock()
	if tab.Ctrl == oldCtrl {
		tab.Ctrl = newCtrl
		if oldCtrl != nil {
			oldCtrl.Close()
			a.releaseSharedHost(root)
		}
	} else {
		tab.Ctrl = newCtrl
	}
	tab.profile = name
	tab.effort = cloneStringPtr(effortOverride) // persist the normalized effort
	tab.Label = newCtrl.Label()
	// Clear the goal on a profile switch (hard isolation): the goal belongs to
	// the previous profile's conversation. The old goal remains in the old
	// profile's session on disk and is restored when switching back; the new
	// profile's fresh session starts goal-less.
	tab.goal = ""
	a.saveTabsLocked()
	// Snapshot the post-switch scalar fields under the lock so the unlocked
	// persist/index calls below don't race with a concurrent tabMeta/update*.
	snapScope := tab.Scope
	snapRoot := tab.WorkspaceRoot
	snapTopicID := strings.TrimSpace(tab.TopicID)
	snapTopicTitle := tab.TopicTitle
	snapMode := tab.mode
	snapToolApprovalMode := tab.toolApprovalMode
	snapRagScope := tab.ragScope
	a.mu.Unlock()
	newCtrl.EnableInteractiveApproval()
	applyTabModeToController(newCtrl, snapMode)
	applyTabToolApprovalModeToController(newCtrl, snapToolApprovalMode)
	applyTabRagScopeToController(newCtrl, snapRagScope)
	// Hard isolation extends to the goal: a dev goal (e.g. "refactor auth.go")
	// must NOT carry into the cowork controller, because Compose would inject it
	// as an <active-goal> block on every cowork turn. The goal lives with the
	// profile's conversation, so a fresh session starts goal-less. The old
	// profile's goal stays recorded on disk in its own session and is restored
	// when the user switches back (buildTabController re-reads it).
	newCtrl.SetGoal("")

	// Hard isolation: a profile switch starts a FRESH session in the new
	// profile's directory. The old conversation stays on disk under the old
	// profile, fully preserved — switching back reveals it again. We never carry
	// history across (that would leak dev context into cowork or vice versa) and
	// never reuse the old session path (ContinueSessionPath would return it).
	path := agent.NewSessionPath(newCtrl.SessionDir(), newCtrl.Label())
	if path != "" {
		newCtrl.SetSessionPath(path)
	}
	a.persistTabSessionPath(tab, path)
	// Index the (existing) topic under the NEW profile so it shows up in the
	// sidebar immediately, rather than waiting for the next restart's
	// buildTabController → ensureTopicIndexed pass. Best-effort: a failure just
	// means the topic appears after the first turn or restart.
	if snapTopicID != "" {
		if err := ensureTopicIndexed(snapScope, snapRoot, name, snapTopicID, snapTopicTitle, loadTopicTitleSource(topicTitleRoot(snapScope, snapRoot), name, snapTopicID)); err == nil {
			a.emitProjectTreeChanged()
		}
	}

	// Tell the frontend this tab's profile changed so it swaps the layout. The
	// payload carries the tab id + normalized profile name. Emit agent:ready too
	// so the frontend reloads (now-empty) history for the fresh session.
	a.emitReady(a.ctx)
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "profile:changed", map[string]string{
			"tabId":   tab.ID,
			"profile": name,
		})
	}
	return nil
}

func (a *App) Effort() EffortInfo {
	return a.EffortForTab("")
}

func (a *App) EffortForTab(tabID string) EffortInfo {
	entry, err := a.currentProviderEntryForTab(tabID)
	if err != nil {
		return EffortInfo{Current: "auto", Levels: []string{}}
	}
	cap := config.EffortCapabilityForEntry(entry)
	if !cap.Supported {
		return EffortInfo{Supported: false, Current: "auto", Default: cap.Default, Levels: []string{}}
	}
	levels := cap.Levels
	if levels == nil {
		levels = []string{}
	}
	return EffortInfo{Supported: true, Current: config.EffortDisplay(entry), Default: cap.Default, Levels: levels}
}

func (a *App) SetEffort(level string) error {
	return a.SetEffortForTab("", level)
}

func (a *App) SetEffortForTab(tabID, level string) error {
	tab := a.tabByID(tabID)
	if tab == nil {
		if strings.TrimSpace(tabID) == "" {
			entry, err := a.currentProviderEntryForTab("")
			if err != nil {
				return err
			}
			effort, err := config.NormalizeEffort(entry, level)
			if err != nil {
				return err
			}
			return a.applyProviderEffortConfig(entry, effort)
		}
		return fmt.Errorf("tab %q not found", tabID)
	}
	// Snapshot old controller + scalar fields under RLock to avoid TOCTOU.
	a.mu.RLock()
	oldCtrl := tab.Ctrl
	oldModel := tab.model
	root := tab.WorkspaceRoot
	sink := tab.sink
	a.mu.RUnlock()

	if oldCtrl != nil && oldCtrl.Running() {
		return fmt.Errorf("finish or cancel the current turn before changing effort")
	}
	entry, err := a.currentProviderEntryForTab(tabID)
	if err != nil {
		return err
	}
	effort, err := config.NormalizeEffort(entry, level)
	if err != nil {
		return err
	}
	var carried []provider.Message
	prevPath := ""
	// Acquire before release so the shared host survives the controller swap.
	sharedHost := a.acquireSharedHost(root)
	if oldCtrl != nil {
		prevPath = oldCtrl.SessionPath()
		_ = oldCtrl.Snapshot()
		carried = oldCtrl.History()
	}
	newCtrl, err := boot.Build(a.bootContext(), boot.Options{
		Model:          oldModel,
		RequireKey:     false,
		Sink:           sink,
		WorkspaceRoot:  root,
		SessionDir:     tabSessionDir(tab),
		EffortOverride: &effort,
		Host:           sharedHost,
	})
	if err != nil {
		a.releaseSharedHost(root) // Build failed: drop our acquire
		return err
	}
	a.bindControllerDisplayRecorder(newCtrl)
	a.bindControllerContextFilter(newCtrl)
	a.mu.Lock()
	if tab.Ctrl == oldCtrl {
		tab.Ctrl = newCtrl
		if oldCtrl != nil {
			oldCtrl.Close()
			a.releaseSharedHost(root)
		}
	} else {
		tab.Ctrl = newCtrl
	}
	tab.effort = &effort
	tab.Label = newCtrl.Label()
	tab.StartupErr = ""
	tab.Ready = true
	a.saveTabsLocked()
	a.mu.Unlock()
	newCtrl.EnableInteractiveApproval()
	applyTabModeToController(newCtrl, tab.mode)
	applyTabToolApprovalModeToController(newCtrl, tab.toolApprovalMode)
	applyTabRagScopeToController(newCtrl, tab.ragScope)
	newCtrl.SetGoal(tab.goal)
	path := agent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	if len(carried) > 0 {
		newCtrl.Resume(&agent.Session{Messages: carried}, path)
	} else if path != "" {
		newCtrl.SetSessionPath(path)
	}
	a.persistTabSessionPath(tab, path)
	return nil
}

func (a *App) applyProviderEffortConfig(entry *config.ProviderEntry, effort string) error {
	return a.applyConfigChange(func(cfg *config.Config) error {
		if _, ok := cfg.Provider(entry.Name); !ok {
			if err := cfg.UpsertProvider(*entry); err != nil {
				return err
			}
		}
		if entry.Kind == "anthropic" && effort != "" && entry.Thinking == "" {
			if err := cfg.SetProviderThinking(entry.Name, "adaptive"); err != nil {
				return err
			}
		}
		for _, name := range providerEffortTargetNames(cfg, entry) {
			if err := cfg.SetProviderEffort(name, effort); err != nil {
				return err
			}
		}
		return nil
	})
}

func providerEffortTargetNames(cfg *config.Config, entry *config.ProviderEntry) []string {
	if cfg == nil || entry == nil {
		return nil
	}
	out := []string{entry.Name}
	seen := map[string]bool{entry.Name: true}
	kind := officialProviderKindFromEntry(*entry)
	if kind == "" {
		return out
	}
	var family []string
	switch kind {
	case "moma":
		family = []string{"moma"}

	}
	for _, name := range family {
		if seen[name] {
			continue
		}
		p, ok := cfg.Provider(name)
		if !ok || officialProviderKindFromEntry(*p) != kind {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// DirEntry is one entry in the "@" file-reference menu.
type DirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

// FilePreview is a bounded, read-only file payload for the workspace side panel.
type FilePreview struct {
	Path      string `json:"path"`
	Body      string `json:"body"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Binary    bool   `json:"binary"`
	Kind      string `json:"kind,omitempty"`
	Mime      string `json:"mime,omitempty"`
	URL       string `json:"url,omitempty"`
	Err       string `json:"err,omitempty"`
}

type WorkspaceChangeView struct {
	Path         string   `json:"path"`
	OldPath      string   `json:"oldPath,omitempty"`
	Sources      []string `json:"sources"`
	GitStatus    string   `json:"gitStatus,omitempty"`
	Turns        []int    `json:"turns,omitempty"`
	LatestPrompt string   `json:"latestPrompt,omitempty"`
	LatestTime   int64    `json:"latestTime,omitempty"`
}

type WorkspaceChangesView struct {
	Files        []WorkspaceChangeView `json:"files"`
	GitAvailable bool                  `json:"gitAvailable"`
	GitErr       string                `json:"gitErr,omitempty"`
	GitBranch    string                `json:"gitBranch,omitempty"`
}

// workspaceNoiseNames are local cache/vendor entries hidden from the file tree
// and "@" menu regardless of where they appear.
var workspaceNoiseNames = map[string]bool{
	".codex":       true,
	".codegraph":   true,
	".DS_Store":    true,
	".git":         true,
	".npm":         true,
	".pnpm-store":  true,
	"node_modules": true,
	"Thumbs.db":    true,
}

var workspaceNoiseDirs = map[string]bool{
	"bin":                      true,
	"desktop/build":            true,
	"desktop/frontend/dist":    true,
	"desktop/frontend/wailsjs": true,
	"dist":                     true,
	"npm/.stage":               true,
	"site/.astro":              true,
	"site/dist":                true,
	"stage":                    true,
	"tmp":                      true,
}

const filePreviewLimit = 256 * 1024
const fileRefSearchLimit = 20

var previewMediaMIMEs = map[string]string{
	".bmp":  "image/bmp",
	".gif":  "image/gif",
	".jpeg": "image/jpeg",
	".jpg":  "image/jpeg",
	".pdf":  "application/pdf",
	".png":  "image/png",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
}

func trimUTF8PartialSuffix(data []byte) []byte {
	if utf8.Valid(data) {
		return data
	}
	for i := len(data) - 1; i >= 0 && len(data)-i <= utf8.UTFMax; i-- {
		if !utf8.RuneStart(data[i]) {
			continue
		}
		if !utf8.Valid(data[:i]) || utf8.FullRune(data[i:]) {
			return data
		}
		return data[:i]
	}
	return data
}

func previewMediaKind(path string) (kind string, mime string) {
	mime = previewMediaMIMEs[strings.ToLower(filepath.Ext(path))]
	if mime == "" {
		return "", ""
	}
	if strings.HasPrefix(mime, "image/") {
		return "image", mime
	}
	if mime == "application/pdf" {
		return "pdf", mime
	}
	return "", ""
}

func workspaceEntryRel(rel, name string) string {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" || rel == "." {
		return name
	}
	return rel + "/" + name
}

func skipWorkspaceEntry(rel, name string, isDir bool) bool {
	if workspaceNoiseNames[name] {
		return true
	}
	return isDir && workspaceNoiseDirs[workspaceEntryRel(rel, name)]
}

func (a *App) activeWorkspaceBase() (string, error) {
	root := a.activeWorkspaceRoot()
	if strings.TrimSpace(root) == "" || root == "." {
		return os.Getwd()
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Clean(root), nil
}

func (a *App) workspacePath(rel string) (string, bool, error) {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return "", false, err
	}
	return workspacePathForBase(base, rel)
}

func workspacePathForBase(base, rel string) (string, bool, error) {
	base = filepath.Clean(base)
	if rel == "" {
		return "", false, os.ErrInvalid
	}
	path := rel
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, rel)
	}
	path = filepath.Clean(path)
	r, err := filepath.Rel(base, path)
	if err != nil {
		return "", false, err
	}
	if r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
		return "", false, os.ErrPermission
	}
	return path, true, nil
}

// ListDir lists one directory level (directories first, then files, each
// alphabetical) for the "@" file-reference menu. rel resolves against the active
// tab workspace. The menu navigates one level at a time, never recursively —
// bounded for huge trees.
func (a *App) ListDir(rel string) []DirEntry {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return []DirEntry{}
	}
	dir := base
	if rel != "" {
		path, ok, err := workspacePathForBase(base, rel)
		if err != nil || !ok {
			return []DirEntry{}
		}
		dir = path
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		return []DirEntry{}
	}
	dirs, files := []DirEntry{}, []DirEntry{}
	for _, e := range es {
		name := e.Name()
		if skipWorkspaceEntry(rel, name, e.IsDir()) {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, DirEntry{Name: name, IsDir: true})
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, DirEntry{Name: name, IsDir: false})
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
	return append(dirs, files...)
}

// SearchFileRefs finds workspace files by basename for bare "@token" completion.
func (a *App) SearchFileRefs(query string) []DirEntry {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil
	}
	results := fileref.Search(base, query, fileRefSearchLimit)
	out := make([]DirEntry, 0, len(results))
	for _, r := range results {
		out = append(out, DirEntry{Name: r.Path, IsDir: r.IsDir})
	}
	return out
}

// ReadFile returns a small text preview for a file under the current workspace.
func (a *App) ReadFile(rel string) FilePreview {
	out := FilePreview{Path: rel}
	path, ok, err := a.workspacePath(rel)
	if err != nil || !ok {
		out.Err = "invalid path"
		return out
	}
	info, err := os.Stat(path)
	if err != nil {
		out.Err = err.Error()
		return out
	}
	if info.IsDir() {
		out.Err = "path is a directory"
		return out
	}
	if !info.Mode().IsRegular() {
		out.Err = "path is not a regular file"
		return out
	}
	out.Size = info.Size()
	if kind, mime := previewMediaKind(path); kind != "" {
		token := a.ensureMediaTokenStore().create(path, info.Name(), mime, kind, info.Size(), info.ModTime())
		out.Kind = kind
		out.Mime = mime
		out.URL = "/__momapeer_workspace_media/" + token + "/" + url.PathEscape(info.Name())
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		out.Err = err.Error()
		return out
	}
	defer f.Close()

	buf := make([]byte, filePreviewLimit+1)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		out.Err = err.Error()
		return out
	}
	data := buf[:n]
	if len(data) > filePreviewLimit {
		data = data[:filePreviewLimit]
		out.Truncated = true
	}

	// Check for BOM first (just the first 2-3 bytes — always complete
	// even at a truncation boundary). BOM-prefixed files skip the NUL
	// check since UTF-16 normally contains 0x00 for ASCII characters.
	bomKind := fileenc.DetectQuick(data)
	if bomKind != fileenc.UTF8 {
		enc, _ := fileenc.Detect(data)
		if enc == fileenc.LossyUTF8 {
			out.Binary = true
			return out
		}
		decoded := fileenc.Decode(data, enc)
		out.Body = string(decoded)
		return out
	}

	// No BOM — NUL in raw bytes is a binary signal.
	if bytes.Contains(data, []byte{0}) {
		out.Binary = true
		return out
	}

	// Trim any partial multi-byte rune at the truncation boundary BEFORE
	// encoding detection. Without this, a large UTF-8 file truncated
	// mid-character would fail utf8.Valid and be misdetected as GB18030
	// or LossyUTF8, producing mojibake or a false binary classification.
	if out.Truncated {
		data = trimUTF8PartialSuffix(data)
	}
	enc, _ := fileenc.Detect(data)
	if enc == fileenc.LossyUTF8 {
		out.Binary = true
		return out
	}
	out.Body = string(fileenc.Decode(data, enc))
	return out
}

// OpenWorkspacePath opens a file or folder from the workspace in the OS default app.
func (a *App) OpenWorkspacePath(rel string) error {
	path, ok, err := a.workspacePath(rel)
	if err != nil || !ok {
		return os.ErrInvalid
	}
	return openWorkspacePath(path)
}

// RevealWorkspacePath shows a workspace file in the native file manager.
func (a *App) RevealWorkspacePath(rel string) error {
	path, ok, err := a.workspacePath(rel)
	if err != nil || !ok {
		return os.ErrInvalid
	}
	return revealPath(path)
}

// RevealPath shows an arbitrary absolute path in the native file manager.
func (a *App) RevealPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return os.ErrInvalid
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return revealPath(path)
}

func revealPath(path string) error {
	switch goruntime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,", path).Start()
	default:
		dir := path
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			dir = filepath.Dir(path)
		}
		return exec.Command("xdg-open", dir).Start()
	}
}

func (a *App) noticeForTab(tabID, text string) {
	tab := a.tabByID(tabID)
	if tab != nil && tab.sink != nil {
		tab.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text})
	}
}

func (a *App) runEffortCommandForTab(tabID, input string) {
	entry, err := a.currentProviderEntryForTab(tabID)
	if err != nil {
		a.noticeForTab(tabID, "effort: "+err.Error())
		return
	}
	cap := config.EffortCapabilityForEntry(entry)
	if !cap.Supported {
		a.noticeForTab(tabID, fmt.Sprintf("effort is not configurable for %s", entry.Name))
		return
	}
	args := strings.Fields(input)
	if len(args) < 2 {
		a.noticeForTab(tabID, fmt.Sprintf("effort for %s: %s (default: %s; options: %s)", entry.Name, config.EffortDisplay(entry), cap.Default, strings.Join(cap.Levels, "|")))
		return
	}
	if len(args) > 2 {
		a.noticeForTab(tabID, "usage: /effort "+strings.Join(cap.Levels, "|"))
		return
	}
	effort, err := config.NormalizeEffort(entry, args[1])
	if err != nil {
		a.noticeForTab(tabID, err.Error())
		return
	}
	if err := a.SetEffortForTab(tabID, args[1]); err != nil {
		a.noticeForTab(tabID, "effort: "+err.Error())
		return
	}
	display := effort
	if display == "" {
		display = "auto"
	}
	a.noticeForTab(tabID, fmt.Sprintf("effort for %s set to %s", entry.Name, display))
}

func (a *App) currentProviderEntryForTab(tabID string) (*config.ProviderEntry, error) {
	a.mu.RLock()
	ref := ""
	workspaceRoot := ""
	effortOverride := (*string)(nil)
	if tab := a.tabByIDLocked(tabID); tab != nil {
		ref = tab.model
		workspaceRoot = tab.WorkspaceRoot
		effortOverride = cloneStringPtr(tab.effort)
	}
	a.mu.RUnlock()
	cfg, err := config.LoadForRoot(workspaceRoot)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ref) == "" {
		ref = cfg.DefaultModel
	}
	resolved, _, ok := cfg.ResolveModelWithFallback(ref)
	if !ok {
		return nil, fmt.Errorf("unknown model %q", ref)
	}
	entry, ok := cfg.ResolveModel(resolved)
	if !ok {
		return nil, fmt.Errorf("unknown model %q", resolved)
	}
	if effortOverride != nil {
		entry.Effort = *effortOverride
	}
	return entry, nil
}

func (a *App) withActiveWorkspace(fn func() (string, error)) (string, error) {
	var result string
	err := a.withActiveWorkspaceDo(func() error {
		var err error
		result, err = fn()
		return err
	})
	return result, err
}

func (a *App) withActiveWorkspaceDo(fn func() error) error {
	root := a.activeWorkspaceRoot()
	if root != "" && root != "." {
		prev, err := os.Getwd()
		if err != nil {
			return err
		}
		if err := os.Chdir(root); err != nil {
			return err
		}
		defer func() { _ = os.Chdir(prev) }()
	}
	return fn()
}

// SavePastedImage stores a browser clipboard image data URL under the active
// tab's workspace .momapeer/attachments and returns the relative @-reference path.
func (a *App) SavePastedImage(dataURL string) (string, error) {
	return a.withActiveWorkspace(func() (string, error) {
		return control.SaveImageDataURL(dataURL)
	})
}

// SaveClipboardImage reads the native OS clipboard image under the active tab's
// workspace .momapeer/attachments and returns the relative @-reference path.
func (a *App) SaveClipboardImage() (string, error) {
	return a.withActiveWorkspace(control.SaveClipboardImage)
}

// SavePastedFile stores a dropped non-image file (the browser exposes its bytes
// as a data URL but not a real path) under the active tab's workspace
// .momapeer/attachments and returns the relative @-reference path.
func (a *App) SavePastedFile(name, dataURL string) (string, error) {
	return a.withActiveWorkspace(func() (string, error) {
		return control.SaveAttachmentDataURL(name, dataURL)
	})
}

// PickExportFile opens the native save dialog and returns the selected path. It
// returns "" when the user cancels.
func (a *App) PickExportFile(defaultFilename, mimeType string) (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	defaultFilename = safeExportFilename(defaultFilename)
	ext := strings.ToLower(filepath.Ext(defaultFilename))
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:                "Export session",
		DefaultDirectory:     dialogDefaultDirectory(a.activeWorkspaceRoot()),
		DefaultFilename:      defaultFilename,
		CanCreateDirectories: true,
		Filters:              exportFileFilters(mimeType, ext),
	})
	if err != nil || path == "" {
		return "", err
	}
	if ext != "" && filepath.Ext(path) == "" {
		path += ext
	}
	return path, nil
}

// SaveExportFile writes an exported session payload to a path previously picked
// by PickExportFile. An empty path is treated as a cancelled export.
func (a *App) SaveExportFile(path, payload string, base64Encoded bool) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	var data []byte
	var err error
	if base64Encoded {
		data, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return fmt.Errorf("decode export payload: %w", err)
		}
	} else {
		data = []byte(payload)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return nil
}

func safeExportFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "momapeer-session.md"
	}
	return filepath.Base(name)
}

func exportFileFilters(mimeType, ext string) []runtime.FileFilter {
	switch mimeType {
	case "text/markdown":
		return []runtime.FileFilter{{DisplayName: "Markdown (*.md)", Pattern: "*.md"}}
	case "application/json":
		return []runtime.FileFilter{{DisplayName: "JSON (*.json)", Pattern: "*.json"}}
	case "application/pdf":
		return []runtime.FileFilter{{DisplayName: "PDF (*.pdf)", Pattern: "*.pdf"}}
	case "image/png":
		return []runtime.FileFilter{{DisplayName: "PNG image (*.png)", Pattern: "*.png"}}
	}
	if ext != "" {
		return []runtime.FileFilter{{DisplayName: strings.ToUpper(strings.TrimPrefix(ext, ".")) + " files (*" + ext + ")", Pattern: "*" + ext}}
	}
	return []runtime.FileFilter{{DisplayName: "All files (*.*)", Pattern: "*.*"}}
}

// AttachmentDataURL returns a safe data URL for a stored image attachment.
func (a *App) AttachmentDataURL(path string) (string, error) {
	return a.withActiveWorkspace(func() (string, error) {
		return control.ImageDataURL(path)
	})
}

// DroppedItem is one OS-dropped file resolved into a composer context entry: an
// in-tree file becomes a workspace @reference (read in place, no copy), while an
// image or out-of-tree file is copied into .momapeer/attachments.
type DroppedItem struct {
	Kind       string `json:"kind"` // "workspace" | "attachment"
	Path       string `json:"path"`
	IsDir      bool   `json:"isDir,omitempty"`
	PreviewURL string `json:"previewUrl,omitempty"`
}

// AttachDropped turns an absolute path from the native file-drop bridge into a
// composer context entry. Images are stored as attachments so the chip shows a
// thumbnail; other in-workspace files are referenced relatively (no copy); files
// outside the workspace are copied into .momapeer/attachments.
func (a *App) AttachDropped(path string) (DroppedItem, error) {
	var item DroppedItem
	err := a.withActiveWorkspaceDo(func() error {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if isImageExt(path) {
			if rel, err := control.SaveImageFile(path); err == nil {
				preview, _ := control.ImageDataURL(rel)
				item = DroppedItem{Kind: "attachment", Path: rel, PreviewURL: preview}
				return nil
			}
		}
		if rel, ok := workspaceRelativeIn(path, a.activeWorkspaceRoot()); ok {
			item = DroppedItem{Kind: "workspace", Path: rel, IsDir: info.IsDir()}
			return nil
		}
		if info.IsDir() {
			return fmt.Errorf("can only attach files from outside the workspace")
		}
		rel, err := control.SaveAttachmentFile(path)
		if err != nil {
			return err
		}
		item = DroppedItem{Kind: "attachment", Path: rel}
		return nil
	})
	if err != nil {
		return DroppedItem{}, err
	}
	return item, nil
}

func isImageExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

func workspaceRelativeIn(path, workspaceRoot string) (string, bool) {
	root := workspaceRoot
	if !filepath.IsAbs(root) {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", false
		}
		root = abs
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// --- memory panel (frontend ⇄ controller) ---

// MemoryDoc is one loaded doc-memory file for the panel: path, scope, and body.
type MemoryDoc struct {
	Path  string `json:"path"`
	Scope string `json:"scope"`
	Body  string `json:"body"`
}

// MemoryFact is one saved auto-memory, surfaced read-only in the panel.
// The bitemporal fields (ValidFrom/ValidTo/Status/SupersededBy/CreatedAt)
// are populated from the store's Memory struct so the timeline view can show
// when a fact became true, whether it has expired, and what superseded it.
type MemoryFact struct {
	Name         string   `json:"name"`
	Title        string   `json:"title,omitempty"`
	Description  string   `json:"description"`
	Type         string   `json:"type"`
	Body         string   `json:"body"`
	ValidFrom    string   `json:"validFrom,omitempty"`
	ValidTo      string   `json:"validTo,omitempty"`
	Status       string   `json:"status,omitempty"`
	Category     string   `json:"category,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	SupersededBy string   `json:"supersededBy,omitempty"`
	CreatedAt    string   `json:"createdAt,omitempty"`
	UpdatedAt    string   `json:"updatedAt,omitempty"`
}

// MemoryScope is one writable quick-add target (scope id + the file it writes to).
type MemoryScope struct {
	Scope string `json:"scope"`
	Path  string `json:"path"`
}

// MemoryView is the whole memory panel payload: hierarchical docs, saved facts,
// and the writable scopes for the quick-add selector.
type MemoryView struct {
	Docs      []MemoryDoc   `json:"docs"`
	Facts     []MemoryFact  `json:"facts"`
	Scopes    []MemoryScope `json:"scopes"`
	StoreDir  string        `json:"storeDir"`
	Available bool          `json:"available"`
}

// writableScopes are the quick-add targets the panel offers, broad → specific.
var writableScopes = []memory.Scope{memory.ScopeUser, memory.ScopeProject, memory.ScopeLocal}

// Memory returns the loaded memory for the panel: the momapeer.md hierarchy, the
// saved auto-memories, and the writable scopes. Read-only; mutations go through
// Remember / SaveDoc.
func (a *App) Memory() MemoryView {
	// Always return non-nil slices: a nil Go slice marshals to JSON `null`, which
	// would crash the panel's `view.facts.length` / `.map`.
	view := MemoryView{Docs: []MemoryDoc{}, Facts: []MemoryFact{}, Scopes: []MemoryScope{}}
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return view
	}
	set := ctrl.Memory()
	if set == nil {
		return view
	}
	view.StoreDir = set.Store.Dir
	view.Available = true
	for _, d := range set.Docs {
		view.Docs = append(view.Docs, MemoryDoc{Path: d.Path, Scope: string(d.Scope), Body: d.Body})
	}
	for _, f := range set.Store.List() {
		view.Facts = append(view.Facts, memoryFactView(f))
	}
	for _, sc := range writableScopes {
		if p := set.DocPath(sc); p != "" { // user scope yields "" when no config dir
			view.Scopes = append(view.Scopes, MemoryScope{Scope: string(sc), Path: p})
		}
	}
	return view
}

// memoryFactView maps a memory.Memory onto the panel's MemoryFact DTO.
// memory.Memory was slimmed to {Name,Body,Type,Profile,CreatedAt}; the legacy
// bitemporal fields (Title/Description/ValidFrom/ValidTo/Status/Category/Tags/
// SupersededBy/UpdatedAt) are no longer on the store struct, so they stay at
// their zero value here. The frontend tolerates empty fields (omitempty on the
// JSON side). CreatedAt is still carried so the timeline can sort entries.
func memoryFactView(f memory.Memory) MemoryFact {
	out := MemoryFact{
		Name: f.Name,
		Type: string(f.Type),
		Body: f.Body,
	}
	if !f.CreatedAt.IsZero() {
		out.CreatedAt = f.CreatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

// MemoryHistory returns the full bitemporal surface — active, dormant, and
// superseded records (the latter pulled from .archive/) — for the timeline
// view. Unlike Memory() this includes docs/scopes metadata as well so the
// panel can render a self-contained history tab without a second round-trip.
// Read-only; never mutates state.
func (a *App) MemoryHistory() MemoryView {
	view := MemoryView{Docs: []MemoryDoc{}, Facts: []MemoryFact{}, Scopes: []MemoryScope{}}
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return view
	}
	set := ctrl.Memory()
	if set == nil {
		return view
	}
	view.StoreDir = set.Store.Dir
	view.Available = true
	for _, f := range set.Store.List() {
		view.Facts = append(view.Facts, memoryFactView(f))
	}
	return view
}

// Remember quick-adds a one-line note to the doc-memory file for scope — the
// panel's explicit "remember" action, equivalent to typing "/remember <note>".
// An unknown scope falls back to project. Returns the file written.
func (a *App) Remember(scope, note string) (string, error) {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return "", nil
	}
	return ctrl.QuickAdd(parseScope(scope), note)
}

// Forget deletes a saved auto-memory by name — the panel's delete action for a
// fact the model owns. A no-op when no controller is attached.
func (a *App) Forget(name string) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.ForgetMemory(name)
}

// PromoteMemory confirms an auto-captured "pending" memory. The pending-capture
// pipeline was removed when memory.Memory was slimmed, so there are no pending
// records to promote anymore — this is now a no-op that returns false so the
// frontend's confirm button stays harmless. The signature is kept so the panel
// call site doesn't need a frontend change.
func (a *App) PromoteMemory(name string) (bool, error) {
	return false, nil
}

// RejectMemory dismisses an auto-captured "pending" memory. As with PromoteMemory,
// the pending-capture pipeline no longer exists, so this is a no-op returning
// false. Signature retained for the frontend's ignore button.
func (a *App) RejectMemory(name string) (bool, error) {
	return false, nil
}

// SaveDoc overwrites a memory doc with the panel editor's contents. The controller
// validates path against the recognized memory files. Returns the file written.
func (a *App) SaveDoc(path, body string) (string, error) {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return "", nil
	}
	return ctrl.SaveDoc(path, body)
}

// ProfileView is the payload the workspace preference panel reads: the path of
// the active mode's portrait file and its current contents. Under cowork this
// is cowork.md; under dev it is dev.md. user.md / memory.md are intentionally
// not exposed — only the mode file is user-editable for now.
type ProfileView struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// PortraitProfile returns the active mode's portrait for the workspace
// preference panel. Read-only; saves go through SaveDoc (the profile path is
// whitelisted in memory.allowedDocPaths, so a normal SaveDoc call accepts it).
// Named PortraitProfile to avoid clashing with Profile() (the mode switch).
func (a *App) PortraitProfile() ProfileView {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return ProfileView{}
	}
	pv := ctrl.Profile()
	return ProfileView{Path: pv.Path, Content: pv.Content}
}

// parseScope maps a frontend scope id to a memory.Scope, defaulting to project.
func parseScope(s string) memory.Scope {
	switch memory.Scope(s) {
	case memory.ScopeUser:
		return memory.ScopeUser
	case memory.ScopeLocal:
		return memory.ScopeLocal
	default:
		return memory.ScopeProject
	}
}

// onboardingKeyEnv is the default provider (moma) key from config.Default().
const onboardingKeyEnv = "JIUTIAN_API_KEY"

// probeProviderKey validates an API key by hitting the provider's /models endpoint.
// This is a lightweight connectivity + auth check used during onboarding.
// The baseURL is read from the default config so it adapts to whatever
// provider is actually configured.
func probeProviderKey(ctx context.Context, apiKey string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	// Find the first provider that uses this key env
	for _, p := range cfg.Providers {
		if p.APIKeyEnv != onboardingKeyEnv {
			continue
		}
		url := strings.TrimRight(p.BaseURL, "/") + "/models"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("network: %w", err)
		}
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			return nil
		case http.StatusUnauthorized:
			return fmt.Errorf("invalid API key")
		default:
			return fmt.Errorf("unexpected status %d", resp.StatusCode)
		}
	}
	return fmt.Errorf("no provider configured for %s", onboardingKeyEnv)
}

// NativeConfirmRequest is the payload for ConfirmAction — a native OS confirmation
// dialog that replaces web-style confirm() for destructive or important actions.
type NativeConfirmRequest struct {
	Title        string `json:"title"`
	Message      string `json:"message"`
	Detail       string `json:"detail"`
	ConfirmLabel string `json:"confirmLabel"`
	CancelLabel  string `json:"cancelLabel"`
	Destructive  bool   `json:"destructive"`
}

// ConfirmAction shows a native confirmation dialog and returns true when the user
// clicks the confirm button. For destructive actions the dialog type is Warning so
// the platform can apply its danger styling (red tint on macOS, etc.).
func (a *App) ConfirmAction(req NativeConfirmRequest) (bool, error) {
	if a.ctx == nil {
		return false, nil
	}
	dialogType := runtime.QuestionDialog
	if req.Destructive {
		dialogType = runtime.WarningDialog
	}
	confirm := req.ConfirmLabel
	if confirm == "" {
		confirm = "OK"
	}
	cancel := req.CancelLabel
	if cancel == "" {
		cancel = "Cancel"
	}
	title := req.Title
	if title == "" {
		title = req.Message
	}
	body := req.Message
	if req.Detail != "" {
		if body != "" {
			body += "\n\n" + req.Detail
		} else {
			body = req.Detail
		}
	}
	defaultBtn := confirm
	if req.Destructive {
		// On destructive actions, make cancel the default so Enter / Space
		// does NOT accidentally confirm. ESC always maps to CancelButton.
		defaultBtn = cancel
	}
	result, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          dialogType,
		Title:         title,
		Message:       body,
		Buttons:       []string{confirm, cancel},
		DefaultButton: defaultBtn,
		CancelButton:  cancel,
	})
	if err != nil {
		return false, err
	}
	return result == confirm, nil
}

func (a *App) NeedsOnboarding() bool {
	return strings.TrimSpace(os.Getenv(onboardingKeyEnv)) == ""
}

// ConnectKey validates apiKey against the provider endpoint, persists it to the
// global credentials file, and rebuilds the controller so the new key takes effect.
func (a *App) ConnectKey(apiKey string) error {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("key is required")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
	defer cancel()
	if err := probeProviderKey(ctx, apiKey); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if err := upsertDotEnv(onboardingKeyEnv, apiKey); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	if err := a.rebuild(); err != nil {
		// Key is persisted; surface the failure but let the next rebuild load it.
		a.mu.Lock()
		if tab := a.activeTabLocked(); tab != nil {
			tab.StartupErr = err.Error()
		}
		a.mu.Unlock()
	}
	return nil
}
