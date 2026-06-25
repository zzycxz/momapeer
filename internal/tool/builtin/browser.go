package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/zzycxz/momapeer/internal/tool"
)

// Browser automation tools (Phase 1 of coWork). These drive a real Chromium via
// the Chrome DevTools Protocol (chromedp) — navigation, clicking, typing,
// scrolling, content extraction, screenshots, and JS evaluation. They are the
// "precise control" channel for office web tasks (research, form filling, data
// scraping) and complement the VLM screenshot channel.
//
// Session model: browser_open starts a Chromium subprocess and returns a session
// id; the other tools take that session id and reuse the same browser tab. A
// process-global pool holds live sessions so multiple agent sub-tasks can drive
// independent tabs without each spawning a browser. Sessions time out after
// browserIdleTimeout of inactivity and are closed on process exit.
//
// Build footprint: chromedp is pure Go (no CGO), so it does not compromise the
// CLI's single-static-binary guarantee. The browser subprocess is only spawned
// on first browser_open — dev mode never pays for it. These tools are
// profile-gated in boot.go (registered only when the cowork profile is active),
// so they don't appear in the dev tool list at all.

// BrowserTools returns the full set of browser automation tools, for
// registration when the cowork profile is active. Unlike the compile-time
// built-ins (which self-register via init() and are therefore in every tool
// list), browser tools are intentionally NOT in the global set — they are
// office-specific and should not appear in dev mode. boot.go calls this only
// under the cowork profile, so the dev tool list stays clean and the browser
// subprocess is never reachable from a coding session.
func BrowserTools() []tool.Tool {
	return []tool.Tool{
		browserOpen{},
		browserNavigate{},
		browserClick{},
		browserType{},
		browserScroll{},
		browserExtract{},
		browserScreenshot{},
		browserEvaluate{},
		browserSnapshot{},
		browserSelectOption{},
		browserSetPath{},
	}
}

const (
	// browserIdleTimeout closes a browser session after this long without a tool
	// call, so a forgotten browser_open doesn't leak a Chromium process forever.
	browserIdleTimeout = 10 * time.Minute
	// browserActionTimeout caps any single CDP action, so a hung page can't block
	// the agent loop indefinitely.
	browserActionTimeout = 30 * time.Second
	// browserExtractMaxChars caps extracted text so a huge page doesn't blow the
	// model's context. The agent can narrow with a selector or paginate.
	browserExtractMaxChars = 200_000
)

// --- session pool -----------------------------------------------------------

// browserSession is one live Chromium tab driven via CDP.
type browserSession struct {
	id          string
	allocCancel context.CancelFunc
	ctxCancel   context.CancelFunc
	ctx         context.Context // chromedp context tied to this tab
	lastUsed    atomic.Int64    // unix seconds; drives idle reaping
	browser     string          // display name of the driven browser ("Chrome"/"Edge"/…)
	// refs holds the ref→node map from the most recent browser_snapshot. It's an
	// atomic pointer (not a plain field) because navigate clears it (sets nil)
	// while a concurrent click/type may read it — a plain field would data-race.
	// The refs map itself is never mutated in place, only wholesale-replaced,
	// which is exactly the atomic-store pattern. Cleared on navigate (refs expire
	// when the page changes). Action tools resolve a ref via resolveRefToObjectID.
	refs atomic.Pointer[snapshotRefs]
}

var (
	browserMu        sync.Mutex
	browserSessions  = map[string]*browserSession{}
	browserSeq       atomic.Int64
	browserReaperOnce sync.Once
)

// browserPoolCtx is the parent allocator context for all browser sessions. It is
// created lazily on first browser_open and never cancelled (process-lifetime).
var browserPoolCtx context.Context

// ensureBrowserAllocator creates the shared chromedp allocator on first use. It
// auto-detects an installed Chromium-based browser (Chrome → Edge → Brave → …)
// so the agent works with zero config on most machines; CHROME_PATH still
// overrides. If nothing is found, the returned error is ErrNoBrowser with
// install guidance — callers surface that to the user instead of a low-level
// chromedp failure.
//
// Returns the allocator context, the detected browser's display name (for the
// "ready" message), and an error if no browser is available.
func ensureBrowserAllocator() (context.Context, string, error) {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserPoolCtx != nil {
		return browserPoolCtx, detectedBrowserName, nil
	}
	exePath, name, err := detectBrowserPath()
	if err != nil {
		return nil, "", err
	}
	opts := append([]chromedp.ExecAllocatorOption{},
		chromedp.ExecPath(exePath),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Headless,
		chromedp.DisableGPU,
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("enable-automation", true),
	)
	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	// Keep cancel alive for the process lifetime; we never tear down the
	// allocator itself, only individual sessions.
	_ = cancel
	browserPoolCtx = allocCtx
	return allocCtx, name, nil
}

// startBrowserReaper launches a background goroutine (once) that periodically
// closes idle sessions. It's safe to call repeatedly.
func startBrowserReaper() {
	browserReaperOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				reapIdleBrowserSessions()
			}
		}()
	})
}

func reapIdleBrowserSessions() {
	now := time.Now().Unix()
	var stale []string
	browserMu.Lock()
	for id, s := range browserSessions {
		last := s.lastUsed.Load()
		if now-last > int64(browserIdleTimeout.Seconds()) {
			stale = append(stale, id)
		}
	}
	browserMu.Unlock()
	for _, id := range stale {
		closeBrowserSession(id)
	}
}

// closeBrowserSession tears down a session's tab and removes it from the pool.
// Missing id is a no-op.
func closeBrowserSession(id string) {
	browserMu.Lock()
	s, ok := browserSessions[id]
	if !ok {
		browserMu.Unlock()
		return
	}
	delete(browserSessions, id)
	browserMu.Unlock()
	// Cancelling the tab context closes the CDP target; the allocator stays up.
	if s.ctxCancel != nil {
		s.ctxCancel()
	}
}

// getBrowserSession returns the session for id, refreshing its lastUsed. It
// returns an error if the session doesn't exist (closed/never opened/expired).
func getBrowserSession(id string) (*browserSession, error) {
	browserMu.Lock()
	s, ok := browserSessions[id]
	browserMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("browser session %q not found (open one with browser_open; idle sessions close after %s)", id, browserIdleTimeout)
	}
	s.lastUsed.Store(time.Now().Unix())
	return s, nil
}

// newBrowserSession creates and registers a fresh Chromium tab.
func newBrowserSession() (*browserSession, error) {
	allocCtx, browserName, err := ensureBrowserAllocator()
	if err != nil {
		// No browser found. If detection was cached but the browser was since
		// uninstalled, re-probe once before giving up — otherwise the user would
		// have to restart the app after installing a browser.
		if errors.Is(err, ErrNoBrowser) && detectedBrowserPath != "" {
			resetBrowserDetection()
			allocCtx, browserName, err = ensureBrowserAllocator()
		}
		if err != nil {
			return nil, err
		}
	}
	// Task context = one tab. Its cancellation closes just this target.
	ctx, cancel := chromedp.NewContext(allocCtx)
	// Force the target to actually connect within a timeout, so a missing or
	// broken browser binary fails here rather than on the first action.
	bootCtx, bootCancel := context.WithTimeout(ctx, 20*time.Second)
	defer bootCancel()
	if err := chromedp.Run(bootCtx); err != nil {
		cancel()
		// If the cached path failed to launch, clear the cache so the next
		// attempt re-detects (covers the case where the file exists but is
		// corrupt/wrong-arch, or a new browser was installed).
		resetBrowserDetection()
		return nil, fmt.Errorf("launch %s (try setting CHROME_PATH to a working browser): %w", browserName, err)
	}
	id := fmt.Sprintf("br_%d", browserSeq.Add(1))
	s := &browserSession{
		id:        id,
		ctx:       ctx,
		ctxCancel: cancel,
		browser:   browserName,
	}
	s.lastUsed.Store(time.Now().Unix())
	browserMu.Lock()
	browserSessions[id] = s
	browserMu.Unlock()
	startBrowserReaper()
	return s, nil
}

// runBrowserAction runs a chromedp action list against session s with the shared
// per-action timeout. The context is derived from the session's tab context.
func runBrowserAction(s *browserSession, actions ...chromedp.Action) error {
	actx, cancel := context.WithTimeout(s.ctx, browserActionTimeout)
	defer cancel()
	return chromedp.Run(actx, actions...)
}

// --- shared arg helpers -----------------------------------------------------

// selectorFromArgs resolves a target spec into one of three forms:
//   - a snapshot ref (string like "e5", from browser_snapshot) → isRef=true
//   - a CSS/XPath selector string → selector set, both flags false
//   - a coordinate pair {x, y} → isCoord=true
//
// A plain string is classified by shape: a leading "e" followed by digits is a
// ref, otherwise a selector. This three-way form lets the model target elements
// by ref (most reliable, from the accessibility tree), selector (when known), or
// coordinate (from a VLM screenshot) — whichever it has.
func selectorFromArgs(raw json.RawMessage) (selector string, x, y float64, isCoord, isRef bool, err error) {
	// Try string first.
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		s = strings.TrimSpace(s)
		if looksLikeRef(s) {
			return s, 0, 0, false, true, nil
		}
		return s, 0, 0, false, false, nil
	}
	// Try {x, y}.
	var c struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if json.Unmarshal(raw, &c) == nil && (c.X != 0 || c.Y != 0) {
		return "", c.X, c.Y, true, false, nil
	}
	return "", 0, 0, false, false, fmt.Errorf("expected a ref (e.g. \"e5\"), a selector string, or a {x, y} object")
}

// looksLikeRef reports whether s is a snapshot ref: "e" followed by one or more
// digits. Kept strict so CSS selectors starting with "e" (rare but possible,
// like "email-input") aren't misclassified — those don't match ^e\d+$.
func looksLikeRef(s string) bool {
	if len(s) < 2 || s[0] != 'e' {
		return false
	}
	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// --- tools ------------------------------------------------------------------

// browserOpen
type browserOpen struct{}

func (browserOpen) Name() string { return "browser_open" }

func (browserOpen) Description() string {
	return "Launch a browser tab (headless Chromium via Chrome DevTools Protocol) and return a session id used by the other browser_* tools. Pass an optional url to navigate immediately. Auto-detects an installed Chromium-based browser (Chrome, then Edge, then Brave); set the CHROME_PATH env var to force a specific one. Use for web research, form filling, scraping, and any task needing a real browser. The session stays open for 10 minutes of inactivity; reuse its id across calls."
}

func (browserOpen) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "url":{"type":"string","description":"Optional URL to navigate to on open (absolute http(s) or about:blank). Omit for a blank tab."}
},
"required":[]
}`)
}

func (browserOpen) ReadOnly() bool { return false } // spawns a process

func (browserOpen) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		URL string `json:"url"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
	}
	s, err := newBrowserSession()
	if err != nil {
		// When no browser is found, point the agent at the recovery flow: ask
		// the user for a browser path, then browser_set_path it. This is the
		// "guide the user to input a Chromium browser" requirement.
		if errors.Is(err, ErrNoBrowser) {
			return "", fmt.Errorf("%w\n\nTo fix: ask the user for the path to their Chrome or Edge executable (e.g. on Windows: \"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe\" or \"C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe\"), then call browser_set_path with it and retry browser_open", err)
		}
		return "", err
	}
	if strings.TrimSpace(p.URL) != "" {
		if err := runBrowserAction(s, chromedp.Navigate(p.URL)); err != nil {
			closeBrowserSession(s.id)
			return "", fmt.Errorf("navigate to %s: %w", p.URL, err)
		}
	}
	return fmt.Sprintf("browser session %q ready (driving %s)%s", s.id, s.browser, navSuffix(p.URL)), nil
}

func navSuffix(url string) string {
	if strings.TrimSpace(url) == "" {
		return " (blank tab)"
	}
	return fmt.Sprintf(" at %s", url)
}

// browserNavigate
type browserNavigate struct{}

func (browserNavigate) Name() string { return "browser_navigate" }

func (browserNavigate) Description() string {
	return "Navigate an open browser session to a URL. Replaces the current page. Use after browser_open to move between pages in the same tab."
}

func (browserNavigate) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "session_id":{"type":"string","description":"Session id from browser_open"},
  "url":{"type":"string","description":"Absolute URL to navigate to"}
},
"required":["session_id","url"]
}`)
}

func (browserNavigate) ReadOnly() bool { return false }

func (browserNavigate) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SessionID string `json:"session_id"`
		URL       string `json:"url"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.SessionID == "" || p.URL == "" {
		return "", errors.New("session_id and url are required")
	}
	s, err := getBrowserSession(p.SessionID)
	if err != nil {
		return "", err
	}
	if err := runBrowserAction(s, chromedp.Navigate(p.URL)); err != nil {
		return "", fmt.Errorf("navigate: %w", err)
	}
	// Refs from any prior snapshot are now stale — the new page has different
	// nodes. Clear so a stray ref use fails fast with "no snapshot" rather than
	// silently acting on the wrong element. Atomic store (no lock needed; refs
	// is an atomic.Pointer to avoid racing a concurrent snapshot/click).
	s.refs.Store(nil)
	return fmt.Sprintf("navigated to %s", p.URL), nil
}

// browserClick
type browserClick struct{}

func (browserClick) Name() string { return "browser_click" }

func (browserClick) Description() string {
	return "Click an element in an open browser session. The target is one of: (1) a snapshot ref like \"e5\" from browser_snapshot — PREFERRED, unambiguous and doesn't need guessing selectors; (2) a CSS selector string (e.g. \"button#submit\"); (3) a coordinate object {x, y} from a screenshot. Refs are the most reliable: take a snapshot, read the accessibility tree, and click by ref. Use coordinates only when the element has no selector or ref."
}

func (browserClick) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "session_id":{"type":"string"},
  "target":{"description":"A snapshot ref (\"e5\"), a CSS selector (\"button.submit\"), or a coordinate object {\"x\":320,\"y\":240}. Prefer refs from browser_snapshot."}
},
"required":["session_id","target"]
}`)
}

func (browserClick) ReadOnly() bool { return false }

func (browserClick) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SessionID string          `json:"session_id"`
		Target    json.RawMessage `json:"target"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.SessionID == "" {
		return "", errors.New("session_id is required")
	}
	s, err := getBrowserSession(p.SessionID)
	if err != nil {
		return "", err
	}
	sel, x, y, isCoord, isRef, err := selectorFromArgs(p.Target)
	if err != nil {
		return "", err
	}
	if isRef {
		// Resolve the ref to a DOM node and click via JS — same path Playwright
		// uses (dom.ResolveNode + callFunctionOn). this.click() fires the real
		// click event the page listens for.
		if _, err := callOnRef(s, sel, "function() { this.click(); return this.outerHTML.slice(0,80); }"); err != nil {
			return "", fmt.Errorf("click ref %q: %w", sel, err)
		}
		return fmt.Sprintf("clicked ref %q", sel), nil
	}
	if isCoord {
		if err := runBrowserAction(s, chromedp.MouseClickXY(x, y)); err != nil {
			return "", fmt.Errorf("click (%.0f, %.0f): %w", x, y, err)
		}
		return fmt.Sprintf("clicked (%.0f, %.0f)", x, y), nil
	}
	if err := runBrowserAction(s, chromedp.Click(sel)); err != nil {
		return "", fmt.Errorf("click %q: %w", sel, err)
	}
	return fmt.Sprintf("clicked %q", sel), nil
}

// browserType
type browserType struct{}

func (browserType) Name() string { return "browser_type" }

func (browserType) Description() string {
	return "Type text into an input element in an open browser session. Prefer passing a ref (from browser_snapshot) — it targets the exact element without guessing selectors. Set clear=true to empty the field first. Dispatches input + change events so React/Vue style frameworks register the value. Use to fill forms, search boxes, and text areas."
}

func (browserType) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "session_id":{"type":"string"},
  "ref":{"type":"string","description":"Snapshot ref of the input element (e.g. \"e5\"), from browser_snapshot. Preferred over selector."},
  "selector":{"type":"string","description":"CSS selector of the input element. Used when no ref is given. Omit both ref and selector to type into the currently-focused element."},
  "text":{"type":"string","description":"Text to type"},
  "clear":{"type":"boolean","description":"Clear the field before typing (default false)"}
},
"required":["session_id","text"]
}`)
}

func (browserType) ReadOnly() bool { return false }

func (browserType) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SessionID string `json:"session_id"`
		Ref       string `json:"ref"`
		Selector  string `json:"selector"`
		Text      string `json:"text"`
		Clear     bool   `json:"clear"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.SessionID == "" {
		return "", errors.New("session_id is required")
	}
	s, err := getBrowserSession(p.SessionID)
	if err != nil {
		return "", err
	}
	ref := strings.TrimSpace(p.Ref)
	sel := strings.TrimSpace(p.Selector)

	// Ref path: resolve ref → DOM node, set value + dispatch input/change events.
	// The event dispatch is essential for React/Vue/Svelte which read from events,
	// not from the DOM value directly (a bare .value= wouldn't register). We use
	// the native value setter from the prototype so React-controlled inputs pick
	// up the change — React tracks value via its own setter, and the documented
	// workaround is to call the prototype's setter then dispatch input.
	if ref != "" {
		fn := `function(text, clear) {
  if (clear) { this.value = ''; }
  this.focus();
  var setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value');
  if (!setter) { setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value'); }
  if (setter && setter.set) { setter.set.call(this, text); }
  else { this.value = text; }
  this.dispatchEvent(new Event('input', {bubbles: true}));
  this.dispatchEvent(new Event('change', {bubbles: true}));
  return this.value;
}`
		result, err := callOnRef(s, ref, fn, p.Text, p.Clear)
		if err != nil {
			return "", fmt.Errorf("type into ref %q: %w", ref, err)
		}
		return fmt.Sprintf("typed %d chars into ref %q (value now: %s)", len(p.Text), ref, truncate(result, 60)), nil
	}

	// Selector / focused-element path (unchanged behavior).
	var actions []chromedp.Action
	if sel != "" {
		if p.Clear {
			actions = append(actions, chromedp.Clear(sel))
		}
		actions = append(actions, chromedp.SendKeys(sel, p.Text))
	} else {
		actions = append(actions, chromedp.Evaluate(fmt.Sprintf(
			`(function(){var el=document.activeElement;if(!el||el===document.body){return 'no focused element'};el.value=%q;el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}));return 'ok'})()`,
			p.Text,
		), nil))
	}
	if err := runBrowserAction(s, actions...); err != nil {
		return "", fmt.Errorf("type: %w", err)
	}
	return fmt.Sprintf("typed %d chars%s", len(p.Text), fieldSuffix(sel, p.Clear)), nil
}

func fieldSuffix(sel string, cleared bool) string {
	var parts []string
	if sel != "" {
		parts = append(parts, fmt.Sprintf(" into %q", sel))
	}
	if cleared {
		parts = append(parts, " (cleared first)")
	}
	return strings.Join(parts, "")
}

// browserScroll
type browserScroll struct{}

func (browserScroll) Name() string { return "browser_scroll" }

func (browserScroll) Description() string {
	return "Scroll the page in an open browser session. Direction is up/down/left/right; amount is in pixels (default 600). Use to reach content below the fold before extracting or screenshotting."
}

func (browserScroll) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "session_id":{"type":"string"},
  "direction":{"type":"string","enum":["up","down","left","right"],"description":"Scroll direction"},
  "amount":{"type":"integer","description":"Pixels to scroll (default 600)"}
},
"required":["session_id","direction"]
}`)
}

func (browserScroll) ReadOnly() bool { return false }

func (browserScroll) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SessionID string `json:"session_id"`
		Direction string `json:"direction"`
		Amount    int    `json:"amount"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.SessionID == "" {
		return "", errors.New("session_id is required")
	}
	dir := strings.ToLower(strings.TrimSpace(p.Direction))
	amt := p.Amount
	if amt == 0 {
		amt = 600
	}
	var expr string
	switch dir {
	case "down":
		expr = fmt.Sprintf("window.scrollBy(0, %d)", amt)
	case "up":
		expr = fmt.Sprintf("window.scrollBy(0, -%d)", amt)
	case "right":
		expr = fmt.Sprintf("window.scrollBy(%d, 0)", amt)
	case "left":
		expr = fmt.Sprintf("window.scrollBy(-%d, 0)", amt)
	default:
		return "", fmt.Errorf("direction must be up/down/left/right, got %q", p.Direction)
	}
	s, err := getBrowserSession(p.SessionID)
	if err != nil {
		return "", err
	}
	if err := runBrowserAction(s, chromedp.Evaluate(expr, nil)); err != nil {
		return "", fmt.Errorf("scroll: %w", err)
	}
	return fmt.Sprintf("scrolled %s %dpx", dir, amt), nil
}

// browserExtract
type browserExtract struct{}

func (browserExtract) Name() string { return "browser_extract" }

func (browserExtract) Description() string {
	return "Extract text content from an open browser session. With no selector, returns the visible text of the whole page; with a selector, returns that element's text. Output is capped at 200k chars — narrow with a selector or scroll+extract in chunks for long pages."
}

func (browserExtract) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "session_id":{"type":"string"},
  "selector":{"type":"string","description":"Optional CSS selector to extract from a specific element. Omit for the whole page body."}
},
"required":["session_id"]
}`)
}

func (browserExtract) ReadOnly() bool { return true }

func (browserExtract) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SessionID string `json:"session_id"`
		Selector  string `json:"selector"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.SessionID == "" {
		return "", errors.New("session_id is required")
	}
	s, err := getBrowserSession(p.SessionID)
	if err != nil {
		return "", err
	}
	sel := strings.TrimSpace(p.Selector)
	var text string
	if sel != "" {
		if err := runBrowserAction(s, chromedp.Text(sel, &text, chromedp.NodeVisible)); err != nil {
			return "", fmt.Errorf("extract %q: %w", sel, err)
		}
	} else {
		if err := runBrowserAction(s, chromedp.OuterHTML("body", &text)); err != nil {
			return "", fmt.Errorf("extract body: %w", err)
		}
		// OuterHTML still has tags; reduce to text for the model. Keep it simple —
		// the full HTML reducer lives in web_fetch; here we want quick readable text.
		text = htmlToText(text)
	}
	if len(text) > browserExtractMaxChars {
		text = text[:browserExtractMaxChars] + fmt.Sprintf("\n\n[...truncated, %d more chars]", len(text)-browserExtractMaxChars)
	}
	return strings.TrimSpace(text), nil
}

// browserScreenshot
type browserScreenshot struct{}

func (browserScreenshot) Name() string { return "browser_screenshot" }

func (browserScreenshot) Description() string {
	return "Capture a screenshot of an open browser session as a PNG and return its file path plus a base64 thumbnail. Pass to image_understand for visual analysis, or use the path as an attachment. full_page=true captures the whole scrollable page."
}

func (browserScreenshot) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "session_id":{"type":"string"},
  "full_page":{"type":"boolean","description":"Capture the entire scrollable page, not just the viewport (default false)"}
},
"required":["session_id"]
}`)
}

func (browserScreenshot) ReadOnly() bool { return true }

func (browserScreenshot) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SessionID string `json:"session_id"`
		FullPage  bool   `json:"full_page"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.SessionID == "" {
		return "", errors.New("session_id is required")
	}
	s, err := getBrowserSession(p.SessionID)
	if err != nil {
		return "", err
	}
	var buf []byte
	if p.FullPage {
		// FullScreenshot captures the entire scrollable page at a JPEG quality
		// (it returns JPEG, not PNG — quality 80 keeps it small for VLM input).
		if err := runBrowserAction(s, chromedp.FullScreenshot(&buf, 80)); err != nil {
			return "", fmt.Errorf("screenshot: %w", err)
		}
	} else {
		// CaptureScreenshot grabs the current viewport as PNG.
		if err := runBrowserAction(s, chromedp.CaptureScreenshot(&buf)); err != nil {
			return "", fmt.Errorf("screenshot: %w", err)
		}
	}
	// Persist to the attachments dir so it doubles as a file attachment. The
	// extension reflects the actual format: FullScreenshot is JPEG, the viewport
	// capture is PNG — image_understand handles both.
	dir := browserAttachmentsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create attachments dir: %w", err)
	}
	ext := ".png"
	if p.FullPage {
		ext = ".jpg"
	}
	name := fmt.Sprintf("browser-%s-%d%s", p.SessionID, time.Now().Unix(), ext)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return "", fmt.Errorf("write screenshot: %w", err)
	}
	thumb := base64.StdEncoding.EncodeToString(buf)
	if len(thumb) > 4096 {
		thumb = thumb[:4096] + "…"
	}
	return fmt.Sprintf("screenshot saved: %s\nbase64 (first 4k): %s", path, thumb), nil
}

// browserEvaluate
type browserEvaluate struct{}

func (browserEvaluate) Name() string { return "browser_evaluate" }

func (browserEvaluate) Description() string {
	return "Evaluate a JavaScript expression in an open browser session and return the result as JSON. Use for custom DOM queries, triggering handlers, or reading computed state the other tools can't reach. Avoid for tasks a dedicated tool covers (click/type/extract)."
}

func (browserEvaluate) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "session_id":{"type":"string"},
  "expression":{"type":"string","description":"JavaScript expression to evaluate. Must return a JSON-serializable value."}
},
"required":["session_id","expression"]
}`)
}

func (browserEvaluate) ReadOnly() bool { return false } // JS can mutate the page

func (browserEvaluate) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SessionID string `json:"session_id"`
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.SessionID == "" || strings.TrimSpace(p.Expression) == "" {
		return "", errors.New("session_id and expression are required")
	}
	s, err := getBrowserSession(p.SessionID)
	if err != nil {
		return "", err
	}
	var result any
	// AwaitPromise lets async expressions resolve; ReturnByValue marshals the
	// result into Go rather than returning a remote object handle.
	if err := runBrowserAction(s, chromedp.Evaluate(p.Expression, &result, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true).WithReturnByValue(true)
	})); err != nil {
		return "", fmt.Errorf("evaluate: %w", err)
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", result), nil
	}
	return string(out), nil
}

// browserSelectOption picks an option in a <select> dropdown. Office forms are
// full of these (department, date, type, status), and setting .value directly on
// a <select> + dispatching change is the reliable cross-framework way (React's
// onChange listens for the event). The select element is addressed by ref
// (preferred, from browser_snapshot) or CSS selector.
type browserSelectOption struct{}

func (browserSelectOption) Name() string { return "browser_select_option" }

func (browserSelectOption) Description() string {
	return "Select an option in a <select> dropdown. Pass the select element's ref (from browser_snapshot) and either the option's value attribute or its visible label. Handles native selects by setting .value and dispatching the change event so React/Vue form handlers register the selection. Use for office form dropdowns (department, date, type, status, etc.)."
}

func (browserSelectOption) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "session_id":{"type":"string"},
  "ref":{"type":"string","description":"Snapshot ref of the <select> element (e.g. \"e9\"). Preferred over selector."},
  "selector":{"type":"string","description":"CSS selector of the <select> element. Used when no ref is given."},
  "value":{"type":"string","description":"The option's value attribute. Preferred over label (matches exactly)."},
  "label":{"type":"string","description":"The option's visible text. Used when value is unknown; matched by visible label."}
},
"required":["session_id"]
}`)
}

func (browserSelectOption) ReadOnly() bool { return false }

func (browserSelectOption) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SessionID string `json:"session_id"`
		Ref       string `json:"ref"`
		Selector  string `json:"selector"`
		Value     string `json:"value"`
		Label     string `json:"label"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.SessionID == "" {
		return "", errors.New("session_id is required")
	}
	if strings.TrimSpace(p.Value) == "" && strings.TrimSpace(p.Label) == "" {
		return "", errors.New("either value or label is required")
	}
	s, err := getBrowserSession(p.SessionID)
	if err != nil {
		return "", err
	}
	ref := strings.TrimSpace(p.Ref)
	sel := strings.TrimSpace(p.Selector)
	if ref == "" && sel == "" {
		return "", errors.New("either ref or selector is required to identify the <select> element")
	}

	// The JS: set .value (by value attr, or fall back to matching by visible
	// label), then dispatch change so framework handlers fire. Returns the
	// selected option's label + value for confirmation.
	js := `function(value, label) {
  var select = this;
  if (select.tagName !== 'SELECT') {
    return 'error: element is ' + select.tagName + ', not SELECT (wrong ref/selector?)';
  }
  var matched = null;
  if (value) {
    for (var i = 0; i < select.options.length; i++) {
      if (select.options[i].value === value) { matched = select.options[i]; break; }
    }
  }
  if (!matched && label) {
    for (var j = 0; j < select.options.length; j++) {
      if (select.options[j].textContent.trim() === label) { matched = select.options[j]; break; }
    }
  }
  if (!matched) {
    var avail = [];
    for (var k = 0; k < select.options.length; k++) {
      avail.push(select.options[k].value + '=' + select.options[k].textContent.trim());
    }
    return 'error: no matching option. Available: ' + avail.join(', ');
  }
  select.value = matched.value;
  select.dispatchEvent(new Event('input', {bubbles: true}));
  select.dispatchEvent(new Event('change', {bubbles: true}));
  return 'selected: ' + matched.textContent.trim() + ' (value=' + matched.value + ')';
}`

	if ref != "" {
		result, err := callOnRef(s, ref, js, p.Value, p.Label)
		if err != nil {
			return "", fmt.Errorf("select option on ref %q: %w", ref, err)
		}
		// callOnRef returns JSON; unwrap a plain string result for a clean message.
		return unwrapJSONString(result), nil
	}
	// Selector path: run the same logic, but locate the element via querySelector
	// inside the JS. We pass sel/value/label as a JSON array the IIFE unpacks.
	var result any
	actx, cancel := context.WithTimeout(s.ctx, browserActionTimeout)
	defer cancel()
	// Strip the outer "function(value, label){...}" to get the body, then wrap in
	// an IIFE that finds the element by selector and applies the body.
	body := js
	body = strings.TrimSpace(body)
	body = strings.TrimPrefix(body, "function(value, label) {")
	body = strings.TrimSuffix(body, "}")
	// Replace `this` references with `el` so it runs against the queried element.
	body = strings.ReplaceAll(body, "this", "el")
	expr := fmt.Sprintf(`(function(){var el=document.querySelector(%q);if(!el){return 'error: selector matched nothing'};%s})()`, sel, body)
	if err := chromedp.Run(actx, chromedp.Evaluate(expr, &result)); err != nil {
		return "", fmt.Errorf("select option on %q: %w", sel, err)
	}
	out, _ := json.Marshal(result)
	return unwrapJSONString(string(out)), nil
}

// unwrapJSONString turns a JSON-encoded string result (like "\"selected: ...\"")
// into the plain string for a clean tool message. Non-string JSON is returned as-is.
func unwrapJSONString(s string) string {
	var str string
	if json.Unmarshal([]byte(s), &str) == nil {
		return str
	}
	return s
}

// browserSetPath lets the agent persist a user-supplied browser path when
// browser_open failed with ErrNoBrowser. The flow: browser_open fails → the
// agent asks the user for their Chrome/Edge exe path → calls browser_set_path
// to validate + persist it to config → retries browser_open. This is the
// "guide the user to input a Chromium browser path" requirement: one-shot,
// remembered across restarts.
type browserSetPath struct{}

func (browserSetPath) Name() string { return "browser_set_path" }

func (browserSetPath) Description() string {
	return "Persist the path to a Chromium-based browser (Chrome/Edge/Brave/Chromium) so browser_* tools can find it. Use after browser_open reports no browser found: ask the user for their browser's exe path (e.g. \"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe\"), then call this to validate and save it. The path is written to the user config ([cowork] browser_path) and takes effect on the next browser_open — no restart needed. Pass an empty path to clear the override and revert to auto-detection."
}

func (browserSetPath) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"Absolute path to a Chromium-based browser executable (.exe on Windows). Pass \"\" to clear a previously-set override."}
},
"required":["path"]
}`)
}

func (browserSetPath) ReadOnly() bool { return false }

func (browserSetPath) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	path := strings.TrimSpace(p.Path)

	// Empty path = clear override.
	if path == "" {
		SetConfiguredBrowserPath("")
		resetBrowserDetection()
		if err := persistBrowserPath(""); err != nil {
			return "", fmt.Errorf("clear browser_path: %w", err)
		}
		return "browser_path cleared — auto-detection restored (Chrome/Edge/Brave)", nil
	}

	// Validate the path exists before persisting — a typo would otherwise make
	// every future browser_open fail with a confusing "path does not exist".
	if verified, ok := verifyBrowserExe(path); !ok {
		return "", fmt.Errorf("path %q does not exist or is not executable; ask the user for the correct browser path", path)
	} else {
		path = verified
	}

	SetConfiguredBrowserPath(path)
	resetBrowserDetection()
	if err := persistBrowserPath(path); err != nil {
		return "", fmt.Errorf("save browser_path: %w", err)
	}
	return fmt.Sprintf("browser_path saved: %s (%s) — retry browser_open", path, browserDisplayName(path)), nil
}

// persistBrowserPath writes the [cowork] browser_path value into the user's
// config TOML. It reads the existing file (if any), updates just that field,
// and writes back atomically — preserving all other settings. This is a minimal
// targeted edit rather than a full re-render, so unrelated config is untouched.
func persistBrowserPath(path string) error {
	cfgPath := browserConfigFilePath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return err
	}
	existing, _ := os.ReadFile(cfgPath)
	updated := upsertCoworkBrowserPath(string(existing), path)
	tmp := cfgPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(updated), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, cfgPath)
}

// browserConfigFilePath returns the user config TOML path, mirroring
// config.UserConfigPath() without the import cycle (this package is in
// internal/tool/builtin; config is a sibling). We resolve the same XDG/home
// location directly.
func browserConfigFilePath() string {
	// Respect XDG_CONFIG_HOME if set, else ~/.config/momapeer/config.toml — same
	// logic as config.userConfigPath.
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "momapeer", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "momapeer", "config.toml")
}

// upsertCoworkBrowserPath inserts or replaces the browser_path line under the
// [cowork] section of a TOML string. If the section or file is missing, it
// appends a fresh [cowork] section. Existing [cowork] keys other than
// browser_path are preserved.
func upsertCoworkBrowserPath(toml, path string) string {
	lines := strings.Split(toml, "\n")
	// Escape backslashes for TOML string value (Windows paths).
	escaped := strings.ReplaceAll(path, "\\", "\\\\")

	sectionIdx := -1
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == "[cowork]" {
			sectionIdx = i
			break
		}
	}

	if sectionIdx == -1 {
		// No [cowork] section — append one. Avoid a leading blank line when the
		// file is empty/new.
		var b strings.Builder
		if strings.TrimSpace(toml) != "" {
			b.WriteString(toml)
			if !strings.HasSuffix(toml, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
		b.WriteString("[cowork]\n")
		if path != "" {
			b.WriteString("browser_path = \"" + escaped + "\"\n")
		}
		return b.String()
	}

	// Section exists: find the existing browser_path line within it (before the
	// next section header) and replace, or insert after the section header.
	nextSection := len(lines)
	for i := sectionIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") && strings.HasSuffix(strings.TrimSpace(lines[i]), "]") {
			nextSection = i
			break
		}
	}
	for i := sectionIdx + 1; i < nextSection; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "browser_path") {
			if path == "" {
				// Remove the line entirely.
				lines = append(lines[:i], lines[i+1:]...)
			} else {
				lines[i] = "browser_path = \"" + escaped + "\""
			}
			return strings.Join(lines, "\n")
		}
	}
	// No existing browser_path in section — insert one after the header.
	insert := "browser_path = \"" + escaped + "\""
	if path == "" {
		return strings.Join(lines, "\n") // nothing to add when clearing an absent key
	}
	result := append([]string{}, lines[:sectionIdx+1]...)
	result = append(result, insert)
	result = append(result, lines[sectionIdx+1:]...)
	return strings.Join(result, "\n")
}

// --- helpers ----------------------------------------------------------------

func browserAttachmentsDir() string {
	// Mirror web_fetch / image_understand's attachment convention so screenshots
	// are discovered by the same attachment UI.
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, ".momapeer", "attachments")
	}
	return filepath.Join(os.TempDir(), "momapeer-browser")
}
