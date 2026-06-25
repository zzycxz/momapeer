package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zzycxz/momapeer/internal/config"
)

// CoWorkSettingsView is the settings-panel view of the coWork profile config.
// It mirrors config.CoworkConfig but presents secrets (SMTP/IMAP passwords) as
// plain values the user types — they're stored in a momapeer-managed .env file
// (loaded at startup), NOT in config.toml. The *_env fields in config point at
// env var names; this view resolves them to/from the .env so the user never
// touches environment variables manually.
type CoWorkSettingsView struct {
	BrowserPath      string         `json:"browserPath"`      // Chromium browser exe; "" = auto-detect
	WPSPPTServerPath string         `json:"wpsPptServerPath"` // server.py path; "" = PPT disabled
	WPSPPTPython     string         `json:"wpsPptPython"`     // python exe; "" = auto (python/py)
	EmbeddingModel   string         `json:"embeddingModel"`  // "" = FTS5-only RAG
	SMTP             SMTPSettings   `json:"smtp"`
	IMAP             IMAPSettings   `json:"imap"`
	// SMTPPassword/IMAPPassword are the SECRET values (not in config.toml). When
	// non-empty on save, they're written to the .env under the configured
	// password_env names. On load, they're read back from .env so the panel can
	// show whether a password is set (masked). Empty on load = not yet set.
	SMTPPassword string `json:"smtpPassword"`
	IMAPPassword string `json:"imapPassword"`
	// DetectedBrowser is read-only diagnostic: which browser auto-detection
	// found (e.g. "Chrome"), so the panel can show "auto-detected: Chrome" and
	// let the user skip manual entry. "" = nothing detected.
	DetectedBrowser string `json:"detectedBrowser"`
	// WPSPPTDepsMissing lists missing Python deps for the PPT server (e.g.
	// ["fastmcp","pywin32"]); empty = deps OK. Read-only diagnostic.
	WPSPPTDepsMissing []string `json:"wpsPptDepsMissing"`
	// Screenshot fields: global-hotkey screenshot-to-VLM feature. Disabled by
	// default; the user opts in via the cowork settings tab.
	ScreenshotEnabled  bool   `json:"screenshotEnabled"`
	ScreenshotHotkey   string `json:"screenshotHotkey"`
	ScreenshotVLMModel string `json:"screenshotVlmModel"`
}

// SMTPSettings mirrors config.SMTPConfig minus the password (which lives in
// CoWorkSettingsView.SMTPPassword / the .env).
type SMTPSettings struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	From        string `json:"from"`
	Username    string `json:"username"`
	PasswordEnv string `json:"passwordEnv"` // env var name holding the password
	UseTLS      bool   `json:"useTLS"`
}

// IMAPSettings mirrors config.IMAPConfig minus the password.
type IMAPSettings struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	PasswordEnv string `json:"passwordEnv"`
}

// coworkSettingsView builds the view from config + the .env file. Passwords are
// resolved from the .env so the panel shows their set/unset state (masked in UI).
func coworkSettingsView(c config.CoworkConfig) CoWorkSettingsView {
	env := loadCoworkEnv()
	v := CoWorkSettingsView{
		BrowserPath:      c.BrowserPath,
		WPSPPTServerPath: c.WPSPPTServerPath,
		WPSPPTPython:     c.WPSPPTPython,
		EmbeddingModel:     c.EmbeddingModel,
		ScreenshotEnabled:  c.ScreenshotEnabled,
		ScreenshotHotkey:   c.ScreenshotHotkey,
		ScreenshotVLMModel: c.ScreenshotVLMModel,
		SMTP: SMTPSettings{
			Host:        c.SMTP.Host,
			Port:        c.SMTP.Port,
			From:        c.SMTP.From,
			Username:    c.SMTP.Username,
			PasswordEnv: c.SMTP.PasswordEnv,
			UseTLS:      c.SMTP.UseTLS,
		},
		IMAP: IMAPSettings{
			Host:        c.IMAP.Host,
			Port:        c.IMAP.Port,
			Username:    c.IMAP.Username,
			PasswordEnv: c.IMAP.PasswordEnv,
		},
	}
	// Resolve secrets from .env for the masked display.
	if c.SMTP.PasswordEnv != "" {
		v.SMTPPassword = env[c.SMTP.PasswordEnv]
	}
	if c.IMAP.PasswordEnv != "" {
		v.IMAPPassword = env[c.IMAP.PasswordEnv]
	}
	return v
}

// SetCoWorkSettings persists the coWork settings: non-secret fields to
// config.toml via the edit pipeline, secret fields (SMTP/IMAP passwords) to the
// momapeer-managed .env. The password_env names are auto-assigned when empty so
// the user doesn't have to invent env var names.
func (a *App) SetCoWorkSettings(v CoWorkSettingsView) error {
	// Assign default password_env names when unset, so .env keys are stable.
	if v.SMTP.PasswordEnv == "" {
		v.SMTP.PasswordEnv = "COWORK_SMTP_PASSWORD"
	}
	if v.IMAP.PasswordEnv == "" {
		v.IMAP.PasswordEnv = "COWORK_IMAP_PASSWORD"
	}

	// Persist secrets to the .env first (independent of config edit).
	env := loadCoworkEnv()
	if strings.TrimSpace(v.SMTPPassword) != "" {
		env[v.SMTP.PasswordEnv] = strings.TrimSpace(v.SMTPPassword)
	}
	if strings.TrimSpace(v.IMAPPassword) != "" {
		env[v.IMAP.PasswordEnv] = strings.TrimSpace(v.IMAPPassword)
	}
	if err := saveCoworkEnv(env); err != nil {
		return fmt.Errorf("save cowork secrets: %w", err)
	}

	// Persist non-secret config via the standard edit pipeline.
	return a.applyConfigOnly(func(c *config.Config) error {
		c.Cowork.BrowserPath = strings.TrimSpace(v.BrowserPath)
		c.Cowork.WPSPPTServerPath = strings.TrimSpace(v.WPSPPTServerPath)
		c.Cowork.WPSPPTPython = strings.TrimSpace(v.WPSPPTPython)
		c.Cowork.EmbeddingModel = strings.TrimSpace(v.EmbeddingModel)
	c.Cowork.ScreenshotEnabled = v.ScreenshotEnabled
	c.Cowork.ScreenshotHotkey = strings.TrimSpace(v.ScreenshotHotkey)
	c.Cowork.ScreenshotVLMModel = strings.TrimSpace(v.ScreenshotVLMModel)
		c.Cowork.SMTP = config.SMTPConfig{
			Host:        strings.TrimSpace(v.SMTP.Host),
			Port:        v.SMTP.Port,
			From:        strings.TrimSpace(v.SMTP.From),
			Username:    strings.TrimSpace(v.SMTP.Username),
			PasswordEnv: v.SMTP.PasswordEnv,
			UseTLS:      v.SMTP.UseTLS,
		}
		c.Cowork.IMAP = config.IMAPConfig{
			Host:        strings.TrimSpace(v.IMAP.Host),
			Port:        v.IMAP.Port,
			Username:    strings.TrimSpace(v.IMAP.Username),
			PasswordEnv: v.IMAP.PasswordEnv,
		}
		return nil
	})
}

// CheckCoworkBrowser runs browser auto-detection and returns the display name
// ("Chrome"/"Edge"/…) or "" if none found. Powers the "detect" button in the
// panel so users see what auto-detect found before overriding.
func (a *App) CheckCoworkBrowser() string {
	// builtin.SetConfiguredBrowserPath is the inject point; detection lives in
	// the tool package. We call into it via a thin export to avoid an import
	// cycle (desktop → tool/builtin). Reuse the detection helper.
	return detectBrowserForSettings()
}

// CheckWPSPPTDeps reports missing Python deps for the wps-ppt server. Powers the
// "deps status" indicator + "install deps" button in the PPT section.
func (a *App) CheckWPSPPTDeps() []string {
	return checkWPSPPTDepsForSettings()
}

// InstallWPSPPTDeps runs `pip install fastmcp pywin32` for the wps-ppt server.
// Returns combined output. Powers the "install deps" button.
func (a *App) InstallWPSPPTDeps() (string, error) {
	return installWPSPPTDepsForSettings()
}

// --- .env management --------------------------------------------------------

// coworkEnvPath is the momapeer-managed .env holding coWork secrets (SMTP/IMAP
// passwords). Lives in the user config dir alongside config.toml. Loaded at
// startup into the process env so the tools see the passwords via os.Getenv.
func coworkEnvPath() string {
	dir := desktopConfigDir()
	return filepath.Join(dir, "cowork.env")
}

// loadCoworkEnv reads KEY=VALUE lines from the .env into a map. Missing file =
// empty map (not an error — first run has no secrets yet).
func loadCoworkEnv() map[string]string {
	out := map[string]string{}
	f, err := os.Open(coworkEnvPath())
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			// Strip optional surrounding quotes.
			val = strings.Trim(val, "\"")
			out[key] = val
		}
	}
	return out
}

// saveCoworkEnv writes the map back as KEY=VALUE lines (atomic via tmp+rename).
// Also updates the live process env so a save takes effect without restart.
func saveCoworkEnv(m map[string]string) error {
	path := coworkEnvPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# coWork secrets — managed by the settings panel. Do not edit by hand.\n")
	for key, val := range m {
		b.WriteString(key + "=" + val + "\n")
		// Mirror into the live process env so tools see it immediately.
		os.Setenv(key, val)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadCoworkEnvAtStartup is called once during desktop startup to populate the
// process environment from the .env, so coWork tools (which read passwords via
// os.Getenv) find them without the user manually setting system env vars.
func loadCoworkEnvAtStartup() {
	for key, val := range loadCoworkEnv() {
		// Don't override an existing env var (user/system env wins over file).
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}
