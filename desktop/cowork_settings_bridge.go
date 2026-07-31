package main

import (
	"os"
	"os/exec"
	"strings"
)

// These helpers bridge the coWork settings panel to the detection/deps logic
// that lives in the tool/builtin + builtinmcp packages. They're thin so the
// settings panel (desktop pkg) can call them without importing the tool
// registry (which self-registers builtins at init — undesirable from settings).

// detectBrowserForSettings reports which Chromium-based browser auto-detection
// would pick, by name ("Chrome"/"Edge"/"Brave"/…). Used by the panel's "detect"
// button. We avoid importing tool/builtin (init side effects) and instead probe
// the same standard locations directly.
func detectBrowserForSettings() string {
	for _, c := range browserProbeCandidates() {
		if p := firstExisting(c.Paths); p != "" {
			return c.Display
		}
		if c.Name != "" {
			if p, err := exec.LookPath(c.Name); err == nil {
				_ = p
				return c.Display
			}
		}
	}
	return ""
}

type browserProbe struct {
	Display string   // "Chrome", "Edge", ...
	Name    string   // bare command for PATH lookup
	Paths   []string // absolute install paths to probe
}

// browserProbeCandidates mirrors the priority order in tool/builtin/browserdetect.go
// (Chrome → Edge → Brave). Duplicated here to avoid the init side effects of
// importing the tool registry from the desktop settings layer. If a path is
// added upstream, mirror it here.
func browserProbeCandidates() []browserProbe {
	return []browserProbe{
		{Display: "Chrome", Name: "chrome", Paths: []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		}},
		{Display: "Edge", Name: "msedge", Paths: []string{
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		}},
		{Display: "Brave", Name: "brave", Paths: []string{
			`C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe`,
			`C:\Program Files (x86)\BraveSoftware\Brave-Browser\Application\brave.exe`,
		}},
	}
}

func firstExisting(paths []string) string {
	for _, p := range paths {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// fileExists reports whether a path is an existing file. Local helper so the
// browser probe doesn't pull in extra imports.
func fileExists(p string) bool {
	if p == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// stripSpace trims surrounding whitespace (tiny helper kept local).
func stripSpace(s string) string { return strings.TrimSpace(s) }
