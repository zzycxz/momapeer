package builtin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// detectBrowser finds a Chromium-based browser executable to drive via CDP. The
// discovery order is:
//  1. CHROME_PATH env var (user's explicit override — highest priority).
//  2. Previously-detected path cached on disk (so the user only configures once).
//  3. Standard install locations + PATH lookup for Chrome, Edge, Brave, etc.
//
// Only Chromium-based browsers work with chromedp (they speak CDP). Firefox and
// Safari use different protocols and cannot be driven here. On Windows, Edge is
// almost always present (preinstalled), so this rarely fails.
//
// The result is cached after the first successful detection (the cache is only
// reset if the cached path stops existing — a browser uninstall). Callers thus
// pay the filesystem probe once per process.

var (
	detectedBrowserOnce sync.Once
	detectedBrowserPath string
	detectedBrowserName string
	detectBrowserErr    error

	// configuredBrowserPath is the path the user set via [cowork] browser_path.
	// Injected from config at boot; when non-empty it takes priority over env and
	// auto-detection. Set via SetConfiguredBrowserPath.
	configuredBrowserPath string
)

// SetConfiguredBrowserPath injects the config's [cowork] browser_path override.
// Called once at boot (per controller build). Empty means "auto-detect". This
// is the user-facing way to point at a non-standard browser location, surfaced
// via the agent when no browser is found.
func SetConfiguredBrowserPath(p string) { configuredBrowserPath = strings.TrimSpace(p) }

// ErrNoBrowser is returned when no Chromium-based browser can be found. The
// error message guides the user to install one or set CHROME_PATH, rather than
// surfacing the low-level chromedp launch failure.
var ErrNoBrowser = errors.New("no Chromium-based browser found")

// detectBrowserPath returns the absolute path to a usable Chromium-based
// browser, or an error wrapping ErrNoBrowser with install guidance. The first
// successful result is cached for the process lifetime.
func detectBrowserPath() (path string, name string, err error) {
	detectedBrowserOnce.Do(func() {
		detectedBrowserPath, detectedBrowserName, detectBrowserErr = detectBrowserPathUncached()
	})
	return detectedBrowserPath, detectedBrowserName, detectBrowserErr
}

// resetBrowserDetection clears the cache. Used when a cached path is found
// missing at launch time, so the next browser_open re-probes instead of
// failing forever on a stale path. Not safe to call concurrently with
// detectBrowserPath; the single caller is the allocator-under-mutex path.
func resetBrowserDetection() {
	detectedBrowserOnce = sync.Once{}
	detectedBrowserPath = ""
	detectedBrowserName = ""
	detectBrowserErr = nil
}

func detectBrowserPathUncached() (path string, name string, err error) {
	// 1. Config [cowork] browser_path — the user-facing override (set via the
	//    agent when no browser is found). Highest priority.
	if configuredBrowserPath != "" {
		if p, ok := verifyBrowserExe(configuredBrowserPath); ok {
			return p, browserDisplayName(p), nil
		}
		// A configured-but-invalid path is a clear error — don't silently fall
		// through, the user explicitly chose this one.
		return "", "", fmt.Errorf("%w: [cowork] browser_path = %q does not exist; fix the path in your config", ErrNoBrowser, configuredBrowserPath)
	}
	// 2. CHROME_PATH env — the dev/ops override.
	if env := strings.TrimSpace(os.Getenv("CHROME_PATH")); env != "" {
		if p, ok := verifyBrowserExe(env); ok {
			return p, browserDisplayName(p), nil
		}
		// Fall through to discovery rather than failing — the env hint may be a
		// leftover from an uninstalled browser.
	}

	// 3. Standard locations + PATH, in priority order.
	for _, c := range browserCandidates() {
		if p, ok := findBrowser(c); ok {
			return p, browserDisplayName(p), nil
		}
	}
	return "", "", fmt.Errorf("%w: install Chrome or Edge, or set [cowork] browser_path (or CHROME_PATH) to a Chromium-based browser executable", ErrNoBrowser)
}

// browserCandidates returns the prioritized list of browser binary names/paths
// to search for on the current OS. Order matters: Chrome first (canonical),
// then Edge (preinstalled on Windows), then other Chromium variants.
func browserCandidates() []browserCandidate {
	type c = browserCandidate
	switch runtime.GOOS {
	case "windows":
		// Windows: probe the two Program Files roots + the App Paths registry is
		// overkill when the standard dirs cover ~all installs. Edge is in
		// Program Files (x86) on 64-bit Windows. Names are the bare exe for the
		// PATH fallback.
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
		// macOS: browsers live in /Applications as .app bundles; the executable
		// is inside Contents/MacOS.
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
	default: // linux & other unix
		return []c{
			{Name: "google-chrome", Paths: nil},
			{Name: "google-chrome-stable", Paths: nil},
			{Name: "chromium", Paths: nil},
			{Name: "chromium-browser", Paths: nil},
			{Name: "microsoft-edge", Paths: nil},
			{Name: "brave-browser", Paths: nil},
		}
	}
}

type browserCandidate struct {
	Name  string   // bare command name for PATH lookup
	Paths []string // absolute install paths to probe first (may be empty)
}

// findBrowser locates a candidate: try each explicit Path, then PATH lookup by
// Name. Returns the first existing executable.
func findBrowser(c browserCandidate) (string, bool) {
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

// verifyBrowserExe checks that a user-supplied path points to an existing file.
// It does not validate that the file is actually a Chromium browser — a wrong
// binary fails fast at chromedp launch with a clear enough error.
func verifyBrowserExe(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	// Allow the user to pass just the exe name if it's on PATH.
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	if abs, err := exec.LookPath(p); err == nil {
		return abs, true
	}
	return "", false
}

// browserDisplayName returns a short human-readable name for the detected
// browser, derived from the binary path. Used in the "browser ready" message so
// the user knows which browser was driven (e.g. "Chrome" vs "Edge").
func browserDisplayName(path string) string {
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
