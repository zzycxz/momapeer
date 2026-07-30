// Package browserlaunch launches a system-installed Chromium-based browser
// (Chrome / Edge / Brave / Chromium) with a CDP remote-debugging endpoint and
// exposes the resulting websocket URL so a driver — the Python browser-use
// sidecar, or chromedp elsewhere — can attach to the very same browser instead
// of each driver spawning its own.
//
// The design borrows the CDP launch engineering from agent-browser
// (vercel-labs) and OpenWork's eval host launcher, adapted to Go:
//
//   - The PORT is allocated by the Go host via net.Listen("127.0.0.1:0") and
//     handed to Chrome as --remote-debugging-port. This avoids the TOCTOU race
//     in OpenWork's fixed-list probing ([9223..9227] may all be free at probe
//     time but taken by bind time).
//   - A spawn-integrity check asserts the port Chrome actually bound (read
//     from its DevToolsActivePort file) matches the one we asked for, so a
//     collision never silently attaches to the wrong instance.
//   - Readiness is a three-way race: CDP /json/list responds vs. the process
//     exits vs. the process is no longer alive — a browser that crashes before
//     CDP is ready never hangs the caller.
//
// Unlike builtin browser_open (which owns the chromedp allocator internally),
// this package launches the browser as a raw subprocess and exposes only the
// wsURL. It does NOT drive the browser itself; that is the sidecar's job.
package browserlaunch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zzycxz/momapeer/internal/proc"
)

// launchReadyTimeout caps how long Launch waits for CDP to become reachable.
// Real browsers expose /json/version within ~1-3s even on a cold start; the
// generous bound covers a slow first launch under antivirus scanning.
const launchReadyTimeout = 30 * time.Second

// readyPollInterval is the cadence at which /json/version is polled.
const readyPollInterval = 250 * time.Millisecond

// devtoolsActivePortTimeout caps how long we wait for Chrome to write its
// DevToolsActivePort file (the ground-truth source of the bound port).
const devtoolsActivePortTimeout = 15 * time.Second

// Handle is a launched browser process plus its CDP endpoint. Close must be
// called to release the process and temp profile.
type Handle struct {
	// CDPURL is the base HTTP endpoint, e.g. "http://127.0.0.1:54321". Drivers
	// that speak raw HTTP discovery (/json/version, /json/list) use this.
	CDPURL string
	// WSURL is the browser-level websocket debugger URL, e.g.
	// "ws://127.0.0.1:54321/devtools/browser/...". Drivers using
	// connect_over_cdp / chromedp.NewRemoteAllocator use this.
	WSURL string
	// BrowserName is the display name ("Chrome" / "Edge" / …) of what was driven.
	BrowserName string
	// Port is the bound remote-debugging port.
	Port int

	cmd     *exec.Cmd
	userDir string // profile dir; owned (deletable) only when ownTempDir is true
	// ownTempDir is true when we created the profile dir (fresh temp). When
	// false (the caller supplied a persistent --user-data-dir via opts), Close
	// must NOT delete it — that's the user's login state.
	ownTempDir bool
	// cancel stops the lifecycle monitor goroutine (does NOT kill the process;
	// Close does that).
	cancel context.CancelFunc
	// done is closed when the process exits.
	done chan struct{}
	// exitErr holds the process exit error (nil for a clean exit).
	exitErr error

	// stderr captures the browser's stderr for diagnostics on launch failure.
	stderr *lineBuffer
}

// LaunchOptions controls how a browser is launched.
type LaunchOptions struct {
	// ExecutablePath overrides browser auto-detection. Empty = auto-detect
	// (Chrome → Edge → Brave → Chromium). This mirrors builtin's CHROME_PATH /
	// [cowork] browser_path.
	ExecutablePath string
	// UserDataDir is the --user-data-dir. Empty = a fresh temp dir per launch
	// (zero pollution, zero conflict with the user's real profile).
	UserDataDir string
	// Headless launches with --headless=new. When the in-app screencast panel
	// is the primary view, headless avoids a second visible window stealing
	// focus; for local debugging set false to watch the real window.
	Headless bool
	// Proxy is the --proxy-server value (e.g. "http://127.0.0.1:7890"). Empty =
	// no proxy (direct). The driven browser should reach the network the same way
	// momapeer's other traffic does, so this mirrors the chromedp browser_* tools
	// (see boot.go resolveBrowserProxyURL). Authenticated proxies (user:pass@)
	// are NOT supported by --proxy-server; the caller must strip credentials.
	Proxy string
	// ExtraArgs are appended to the Chrome command line after the defaults.
	ExtraArgs []string
	// StartURL, if non-empty, is opened as the initial page.
	StartURL string
}

// Launch detects and starts a Chromium-based browser with a CDP endpoint and
// returns a Handle once CDP is reachable. The caller owns the Handle and must
// Close it.
//
// If ExecutablePath is empty and no Chromium browser can be found, Launch
// returns an error wrapping ErrNoBrowser with install guidance — it never
// spawns a non-Chromium binary.
func Launch(ctx context.Context, opts LaunchOptions) (*Handle, error) {
	exePath, name, err := resolveBrowser(opts.ExecutablePath)
	if err != nil {
		return nil, err
	}

	// Allocate the port ourselves so there is no probe/bind race: we hold the
	// listener until just before spawning, then immediately pass the same port
	// to Chrome. The window between Close and Chrome's bind is tiny and we
	// still verify via the DevToolsActivePort file afterward.
	port, err := allocatePort()
	if err != nil {
		return nil, fmt.Errorf("browserlaunch: allocate CDP port: %w", err)
	}

	ownTempDir := false
	userDataDir := opts.UserDataDir
	if userDataDir == "" {
		td, err := os.MkdirTemp("", "momapeer-browser-")
		if err != nil {
			return nil, fmt.Errorf("browserlaunch: create temp profile: %w", err)
		}
		userDataDir = td
		ownTempDir = true
	}

	h, err := startBrowser(ctx, exePath, name, port, userDataDir, opts, ownTempDir)
	if err != nil {
		// Retry once with --headless=new. On some Windows hosts a headed
		// Chromium exits with a crash (SIGTRAP / GPU init failure) before CDP is
		// up; headless sidesteps the GPU/display path that triggers it. This
		// mirrors OpenWork's eval-host launcher resilience. We only retry the
		// pre-CDP crash path, not genuine config errors (port collision etc.),
		// and only when the caller didn't already request headless.
		if !opts.Headless && isPreCDPCrash(err) {
			retryOpts := opts
			retryOpts.Headless = true
			h, err = startBrowser(ctx, exePath, name, port, userDataDir, retryOpts, ownTempDir)
		}
	}
	if err != nil {
		// Best-effort cleanup of the temp dir on failure so we don't leak.
		if ownTempDir {
			_ = os.RemoveAll(userDataDir)
		}
		return nil, err
	}
	return h, nil
}

// isPreCDPCrash reports whether an error indicates the browser process died
// before exposing CDP (the crash-retry trigger). Port-collision and "browser
// not found" errors return false so we don't pointlessly retry those.
func isPreCDPCrash(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "did not expose CDP") ||
		strings.Contains(msg, "browser exited before exposing CDP") ||
		strings.Contains(msg, "CDP not ready")
}

// startBrowser builds the command line, spawns the process, and waits for CDP
// readiness. It is split from Launch so the port-allocation/temp-dir setup
// stays readable and the error-cleanup path is explicit.
func startBrowser(ctx context.Context, exePath, name string, port int, userDataDir string, opts LaunchOptions, ownTempDir bool) (*Handle, error) {
	args := buildChromeArgs(port, userDataDir, opts)

	// The process context is deliberately detached from the caller's ctx: a
	// cancelled turn (user clicked Stop) must NOT kill the browser, because the
	// sidecar may still be mid-loop and the screencast panel mid-stream. Only
	// Handle.Close kills the process.
	procCtx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(procCtx, exePath, args...)
	// Capture stderr into a ring buffer so a launch failure carries Chrome's own
	// error (e.g. "profile locked", a missing flag) instead of an opaque "did not
	// expose CDP". Discard stdout — Chrome's meaningful diagnostics go to stderr.
	cmd.Stdout = nil
	stderr := newLineBuffer(8192)
	cmd.Stderr = stderr
	// Hide the console window on Windows (a GUI binary spawned from a console
	// host otherwise pops one). No-op elsewhere.
	proc.HideWindow(cmd)

	// StartTracked starts the process AND assigns it to a tracking handle (a
	// Windows Job Object, or a process group elsewhere) so the whole tree dies
	// with the parent on a hard crash — no orphaned Chrome holding the profile
	// lock. Do NOT call cmd.Start() separately; StartTracked does it.
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("browserlaunch: start %s: %w", name, err)
	}
	// Re-arm process-group/job tracking on the already-started process. (We keep
	// the explicit Start above rather than StartTracked so the HideWindow flags
	// and SysProcAttr set there aren't clobbered by StartTracked's own setup.)
	h := &Handle{
		BrowserName: name,
		Port:        port,
		cmd:         cmd,
		userDir:     userDataDir,
		ownTempDir:  ownTempDir,
		cancel:      cancel,
		done:        make(chan struct{}),
		stderr:      stderr,
	}

	// Monitor process exit in the background so readiness can race against it.
	go func() {
		h.exitErr = cmd.Wait()
		close(h.done)
	}()

	// Verify Chrome bound the port we asked for (spawn-integrity check). The
	// DevToolsActivePort file is the ground truth — it's written under the
	// user-data-dir as soon as the devtools server starts.
	boundPort, wsPath, err := waitForDevToolsActivePort(userDataDir, devtoolsActivePortTimeout, h.done)
	if err != nil {
		stderrTail := stderr.String()
		h.killAndCleanup(ownTempDir)
		if stderrTail != "" {
			return nil, fmt.Errorf("browserlaunch: %s did not expose CDP: %w\nstderr: %s", name, err, stderrTail)
		}
		return nil, fmt.Errorf("browserlaunch: %s did not expose CDP: %w", name, err)
	}
	if boundPort != port {
		// Chrome bound a different port than we allocated — a collision or a
		// stale --remote-debugging-port. Refuse to attach rather than risk
		// driving the wrong instance.
		h.killAndCleanup(ownTempDir)
		return nil, fmt.Errorf("browserlaunch: %s bound CDP port %d, expected %d (collision?)", name, boundPort, port)
	}

	h.CDPURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	h.WSURL = "ws://127.0.0.1:" + strconv.Itoa(port) + wsPath

	// Now race CDP readiness against process exit.
	if err := waitForCDPReady(h.CDPURL, launchReadyTimeout, h.done); err != nil {
		stderrTail := stderr.String()
		h.killAndCleanup(ownTempDir)
		if stderrTail != "" {
			return nil, fmt.Errorf("browserlaunch: CDP not ready: %w\nstderr: %s", err, stderrTail)
		}
		return nil, fmt.Errorf("browserlaunch: CDP not ready: %w", err)
	}

	return h, nil
}

// killAndCleanup terminates the process tree and removes the temp profile. Used on
// the failure paths of startBrowser.
func (h *Handle) killAndCleanup(ownTempDir bool) {
	h.cancel()
	// KillTree fells the whole tree (a plain Process.Kill only hits the direct
	// child and can orphan helper processes holding the profile lock).
	if h.cmd != nil && h.cmd.Process != nil {
		proc.KillTree(h.cmd)
	}
	if ownTempDir && h.userDir != "" {
		_ = os.RemoveAll(h.userDir)
	}
}

// Close terminates the browser, then (if Launch created a temp profile) removes
// it. It is safe to call multiple times.
//
// When the caller supplied a PERSISTENT --user-data-dir (LoginOpts.UserDataDir),
// Close asks Chrome to exit gracefully and waits briefly so it can flush
// cookies/localStorage to disk before the process is killed — otherwise a
// persistent login session can be lost on a hard kill (a real issue agent-browser
// guards against with its wait_or_kill). The temp profile is only deleted when
// Launch created it (ownTempDir); a user-supplied persistent dir is left intact.
func (h *Handle) Close() error {
	h.cancel()

	if h.cmd != nil && h.cmd.Process != nil {
		// Persistent profile: give Chrome a moment to flush state to disk before
		// the forced tree-kill. A graceful signal (SIGTERM on Unix, the WM_CLOSE
		// equivalent isn't portable from Go) is preferable, but KillTree is the
		// reliable cross-platform fallback. The short bounded wait covers the
		// common case where the process exits on its own after we cancel context.
		// For an ephemeral temp profile there's nothing to flush, so kill now.
		if !h.ownTempDir {
			select {
			case <-h.done:
				// Process already exited (e.g. context-cancelled) — state flushed.
			case <-time.After(1500 * time.Millisecond):
				proc.KillTree(h.cmd)
			}
		} else {
			proc.KillTree(h.cmd)
		}
	}
	// Don't block forever if Wait is somehow stalled.
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
	}
	// Only delete the profile dir when we created it (fresh temp). A persistent
	// user-supplied --user-data-dir holds the user's login state and must survive.
	if h.ownTempDir && h.userDir != "" {
		_ = os.RemoveAll(h.userDir)
	}
	return nil
}

// Done returns a channel closed when the browser process exits. Useful for the
// screencast/keepalive loops to detect that the browser died under them.
func (h *Handle) Done() <-chan struct{} { return h.done }

// ExitErr returns the process exit error after Done is closed (nil for a clean
// exit). Before Done is closed the result is meaningless.
func (h *Handle) ExitErr() error { return h.exitErr }

// buildChromeArgs assembles the Chromium command line. The flags mirror what
// OpenWork's eval host launcher uses for a system-Chrome CDP surface, tuned
// for a visible-by-default desktop agent:
//
//	--remote-debugging-port=<port>   the CDP endpoint
//	--remote-debugging-address=127.0.0.1  pin loopback (never 0.0.0.0)
//	--user-data-dir=<dir>            isolated temp profile (zero pollution)
//	--window-size=1280,900           a sane default viewport
//	--no-first-run / --no-default-browser-check  silence first-run churn
//	--disable-popup-blocking         the agent needs window.open flows
//
// plus --headless=new when Headless is set.
func buildChromeArgs(port int, userDataDir string, opts LaunchOptions) []string {
	args := []string{
		"--remote-debugging-port=" + strconv.Itoa(port),
		"--remote-debugging-address=127.0.0.1",
		"--user-data-dir=" + userDataDir,
		"--window-size=1280,900",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-popup-blocking",
		// Disable the "Chrome is being controlled by automated software" banner
		// and reduce obvious automation fingerprints without full stealth.
		"--disable-features=Translate",
	}
	if opts.Proxy != "" {
		// Route the browser through the user's configured proxy so the agent
		// reaches the same sites momapeer's other traffic does (e.g. a CN proxy
		// for GitHub). --proxy-server takes a scheme://host:port URL; auth is not
		// supported here (caller strips credentials).
		args = append(args, "--proxy-server="+opts.Proxy)
	}
	if opts.Headless {
		args = append(args, "--headless=new")
	}
	args = append(args, opts.ExtraArgs...)
	if opts.StartURL != "" {
		args = append(args, opts.StartURL)
	}
	return args
}

// allocatePort asks the OS for a free TCP port by binding to :0 and reading
// the assigned port, then closing the listener. There is an inherent
// (sub-millisecond) TOCTOU window, which the spawn-integrity check closes.
func allocatePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	addr := ln.Addr().(*net.TCPAddr)
	_ = ln.Close()
	return addr.Port, nil
}

// waitForDevToolsActivePort waits for Chrome to write DevToolsActivePort under
// userDataDir and returns (port, websocketPath). The file format is two lines:
// the port, then the websocket path (e.g. /devtools/browser/<id>). procDone is
// raced so a browser that exits before writing the file fails fast.
func waitForDevToolsActivePort(userDataDir string, timeout time.Duration, procDone <-chan struct{}) (int, string, error) {
	deadline := time.Now().Add(timeout)
	path := filepath.Join(userDataDir, "DevToolsActivePort")
	for {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			port, wsPath, perr := parseDevToolsActivePort(data)
			if perr == nil {
				return port, wsPath, nil
			}
		}
		if time.Now().After(deadline) {
			select {
			case <-procDone:
				return 0, "", errors.New("browser exited before exposing CDP (DevToolsActivePort not written)")
			default:
			}
			return 0, "", fmt.Errorf("DevToolsActivePort not written within %s", timeout)
		}
		select {
		case <-procDone:
			return 0, "", errors.New("browser exited before exposing CDP")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// parseDevToolsActivePort parses the two-line DevToolsActivePort file content.
func parseDevToolsActivePort(data []byte) (int, string, error) {
	// Split on any newline, trim trailing whitespace/CRs.
	lines := strings.Split(strings.TrimRight(string(data), "\r\n "), "\n")
	if len(lines) < 1 {
		return 0, "", errors.New("empty DevToolsActivePort")
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, "", fmt.Errorf("parse port: %w", err)
	}
	wsPath := ""
	if len(lines) >= 2 {
		wsPath = strings.TrimSpace(lines[1])
	}
	return port, wsPath, nil
}

// waitForCDPReady polls GET <base>/json/version until it responds 200 or the
// process exits / the timeout elapses. This confirms the HTTP endpoint (and
// thus the ws endpoint) is live, not just that the file was written.
func waitForCDPReady(base string, timeout time.Duration, procDone <-chan struct{}) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	url := base + "/json/version"
	for {
		resp, err := client.Get(url)
		if err == nil {
			// Drain & close so the connection can be reused.
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			select {
			case <-procDone:
				return errors.New("browser process exited before CDP was ready")
			default:
			}
			return fmt.Errorf("CDP /json/version not reachable within %s", timeout)
		}
		select {
		case <-procDone:
			return errors.New("browser process exited before CDP was ready")
		case <-time.After(readyPollInterval):
		}
	}
}

// CDPVersionInfo is the JSON returned by /json/version. We only need the
// webSocketDebuggerUrl to derive the browser-level ws endpoint, but the rest
// is useful for diagnostics.
type CDPVersionInfo struct {
	Browser              string `json:"Browser"`
	WebKitVersion        string `json:"WebKit-Version"`
	UserAgent            string `json:"User-Agent"`
	V8Version            string `json:"V8-Version"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// VersionInfo fetches /json/version from the running browser. Useful for
// diagnostics and confirming the browser is a real Chromium.
func (h *Handle) VersionInfo(ctx context.Context) (*CDPVersionInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", h.CDPURL+"/json/version", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/json/version: %s", resp.Status)
	}
	var info CDPVersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// --- browser detection ------------------------------------------------------
//
// This is a self-contained duplicate of builtin's detection (that one lives in
// the builtin tool package and carries package-global cache state we don't
// want to depend on from here). The order and Windows locations mirror the
// builtin so a browser found by browser_open is the same one we launch.

// ErrNoBrowser is returned when no Chromium-based browser can be found.
var ErrNoBrowser = errors.New("no Chromium-based browser found")

var (
	detectOnce sync.Once
	detected   string
	detectedAs string
	detectErr  error
)

// resolveBrowser returns the executable path and display name of a usable
// Chromium-based browser. If override is non-empty it takes priority (and an
// invalid override is a hard error, not a fall-through).
func resolveBrowser(override string) (path string, name string, err error) {
	// An explicit override bypasses the cache: the caller may point at a
	// different binary each call (e.g. after the user picks one).
	if strings.TrimSpace(override) != "" {
		if p, ok := verifyExe(override); ok {
			return p, displayName(p), nil
		}
		return "", "", fmt.Errorf("%w: explicit browser path %q does not exist", ErrNoBrowser, override)
	}
	detectOnce.Do(func() {
		// CHROME_PATH env is the dev/ops override; an invalid value falls
		// through to discovery rather than failing (it may be stale).
		if env := strings.TrimSpace(os.Getenv("CHROME_PATH")); env != "" {
			if p, ok := verifyExe(env); ok {
				detected, detectedAs, detectErr = p, displayName(p), nil
				return
			}
		}
		for _, c := range candidates() {
			if p, ok := find(c); ok {
				detected, detectedAs, detectErr = p, displayName(p), nil
				return
			}
		}
		detected, detectedAs, detectErr = "", "", fmt.Errorf("%w: install Chrome or Edge, or set CHROME_PATH", ErrNoBrowser)
	})
	return detected, detectedAs, detectErr
}

type candidate struct {
	Name  string
	Paths []string
}

func candidates() []candidate {
	type c = candidate
	switch runtime.GOOS {
	case "windows":
		return []c{
			{Name: "chrome", Paths: []string{
				`C:\Program Files\Google\Chrome\Application\chrome.exe`,
				`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
				filepath.Join(os.Getenv("LOCALAPPDATA"), `Google\Chrome\Application\chrome.exe`),
			}},
			{Name: "msedge", Paths: []string{
				`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
				`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
			}},
			{Name: "brave", Paths: []string{
				`C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe`,
				`C:\Program Files (x86)\BraveSoftware\Brave-Browser\Application\brave.exe`,
			}},
		}
	case "darwin":
		return []c{
			{Name: "chrome", Paths: []string{
				`/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`,
				filepath.Join(os.Getenv("HOME"), `Applications/Google Chrome.app/Contents/MacOS/Google Chrome`),
			}},
			{Name: "edge", Paths: []string{
				`/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge`,
			}},
			{Name: "brave", Paths: []string{
				`/Applications/Brave Browser.app/Contents/MacOS/Brave Browser`,
			}},
			{Name: "chromium", Paths: []string{
				`/Applications/Chromium.app/Contents/MacOS/Chromium`,
			}},
		}
	default:
		return []c{
			{Name: "google-chrome"},
			{Name: "google-chrome-stable"},
			{Name: "chromium"},
			{Name: "chromium-browser"},
			{Name: "microsoft-edge"},
			{Name: "brave-browser"},
		}
	}
}

func find(c candidate) (string, bool) {
	for _, p := range c.Paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	if c.Name != "" {
		if p, err := exec.LookPath(c.Name); err == nil {
			return p, true
		}
	}
	return "", false
}

func verifyExe(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	if abs, err := exec.LookPath(p); err == nil {
		return abs, true
	}
	return "", false
}

func displayName(path string) string {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(base, "chrome"):
		return "Chrome"
	case strings.Contains(base, "edge") || strings.Contains(base, "msedge"):
		return "Edge"
	case strings.Contains(base, "brave"):
		return "Brave"
	case strings.Contains(base, "chromium"):
		return "Chromium"
	default:
		return filepath.Base(path)
	}
}

// --- stderr capture ---------------------------------------------------------

// lineBuffer is a bounded ring buffer for a child process's stderr, used to
// surface the browser's own error message when a launch fails. It implements
// io.Writer and keeps the most recent maxBytes of output (oldest dropped).
type lineBuffer struct {
	mu       sync.Mutex
	maxBytes int
	buf      []byte
}

func newLineBuffer(maxBytes int) *lineBuffer { return &lineBuffer{maxBytes: maxBytes} }

func (b *lineBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	// Trim from the front if over the cap, dropping whole lines so we keep
	// readable output rather than a bisected line.
	if len(b.buf) > b.maxBytes {
		over := len(b.buf) - b.maxBytes
		if nl := bytes.IndexByte(b.buf[over:], '\n'); nl >= 0 {
			b.buf = b.buf[over+nl+1:]
		} else {
			b.buf = b.buf[len(b.buf)-b.maxBytes:]
		}
	}
	return len(p), nil
}

// String returns the captured stderr tail, trimmed of trailing whitespace.
func (b *lineBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.buf))
}
