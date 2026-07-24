package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zzycxz/momapeer/internal/config"
	"github.com/zzycxz/momapeer/internal/ppttemplate"
	"github.com/zzycxz/momapeer/internal/secret"
	"github.com/zzycxz/momapeer/internal/tool/builtin"
)

// CoWorkSettingsView is the settings-panel view of the coWork profile config.
// It mirrors config.CoworkConfig but presents secrets (SMTP/IMAP passwords) as
// plain values the user types — they're stored in a momapeer-managed .env file
// (loaded at startup), NOT in config.toml. The *_env fields in config point at
// env var names; this view resolves them to/from the .env so the user never
// touches environment variables manually.
type CoWorkSettingsView struct {
	BrowserPath    string `json:"browserPath"` // Chromium browser exe; "" = auto-detect
	EmbeddingModel string `json:"embeddingModel"`
	// PPTActiveTemplate is the id of the active PPT template (or "" for none).
	// PPTTemplates is the read-only list of available templates for the dropdown.
	// PPTTemplateDir is the absolute path to the templates dir, shown so the user
	// knows where to drop JSON files.
	PPTActiveTemplate string             `json:"pptActiveTemplate"`
	PPTTemplates      []ppttemplate.View `json:"pptTemplates"`
	PPTTemplateDir    string             `json:"pptTemplateDir"`
	// PPTMode controls the PPT generation quality mode:
	// "fast" = one-shot generation, no rework (default, reliable)
	// "validate" = generate + check + rework up to 3 rounds (higher quality)
	PPTMode string       `json:"pptMode"`
	SMTP    SMTPSettings `json:"smtp"`
	IMAP    IMAPSettings `json:"imap"`
	// SMTPPassword/IMAPPassword are WRITE-ONLY: the user types a new password
	// here when changing it. On load they are ALWAYS empty — the panel never
	// receives the stored secret. On save, a non-empty value updates the
	// encrypted store; an empty value leaves the existing password untouched
	// ("leave blank to keep"). The set/unset state is reported via the *Set
	// booleans below so the panel can mark the field without holding the value.
	SMTPPassword string `json:"smtpPassword"`
	IMAPPassword string `json:"imapPassword"`
	// SMTPPasswordSet/IMAPPasswordSet report whether an encrypted secret is
	// stored for the configured password_env. True = "已设置", false = "未设置".
	SMTPPasswordSet bool `json:"smtpPasswordSet"`
	IMAPPasswordSet bool `json:"imapPasswordSet"`
	// DetectedBrowser is read-only diagnostic: which browser auto-detection
	// found (e.g. "Chrome"), so the panel can show "auto-detected: Chrome" and
	// let the user skip manual entry. "" = nothing detected.
	DetectedBrowser string `json:"detectedBrowser"`
	// Screenshot fields: global-hotkey screenshot-to-VLM feature. Disabled by
	// default; the user opts in via the cowork settings tab.
	ScreenshotEnabled  bool   `json:"screenshotEnabled"`
	ScreenshotHotkey   string `json:"screenshotHotkey"`
	ScreenshotVLMModel string `json:"screenshotVlmModel"`
	// EStopHotkey is the global emergency-stop combo for desktop automation.
	EStopHotkey string `json:"estopHotkey"`
}

// SMTPSettings mirrors config.SMTPConfig minus the password (which lives in
// CoWorkSettingsView.SMTPPassword / the .env).
type SMTPSettings struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	From           string `json:"from"`
	Username       string `json:"username"`
	PasswordEnv    string `json:"passwordEnv"` // env var name holding the password
	UseTLS         bool   `json:"useTLS"`
	EncryptionMode string `json:"encryptionMode"` // "tls" (465) | "starttls" (587) | "none" (25)
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
	// Load PPT templates from the skill's template directory.
	// Scan for .pptx files and present them as available templates.
	skillTplDir := ppttemplate.SkillTemplatesDir()
	templates := scanPPTXTemplates(skillTplDir)

	// Resolve SMTP/IMAP from EmailAccounts (preferred) or legacy fields.
	smtp := c.SMTP
	imap := c.IMAP
	if a, ok := c.DefaultEmailAccount(); ok {
		smtp = a.SMTP
		imap = a.IMAP
	}

	v := CoWorkSettingsView{
		BrowserPath:        c.BrowserPath,
		EmbeddingModel:     c.EmbeddingModel,
		PPTActiveTemplate:  c.PPTActiveTemplate,
		PPTTemplates:       templates,
		PPTTemplateDir:     skillTplDir,
		PPTMode:            c.PPTMode,
		ScreenshotEnabled:  c.ScreenshotEnabled,
		ScreenshotHotkey:   c.ScreenshotHotkey,
		ScreenshotVLMModel: c.ScreenshotVLMModel,
		EStopHotkey:        c.EStopHotkey,
		SMTP: SMTPSettings{
			Host:           smtp.Host,
			Port:           smtp.Port,
			From:           smtp.From,
			Username:       smtp.Username,
			PasswordEnv:    smtp.PasswordEnv,
			UseTLS:         smtp.UseTLS,
			EncryptionMode: smtp.EncryptionMode,
		},
		IMAP: IMAPSettings{
			Host:        imap.Host,
			Port:        imap.Port,
			Username:    imap.Username,
			PasswordEnv: imap.PasswordEnv,
		},
	}
	if v.PPTMode == "" {
		v.PPTMode = "fast"
	}
	// Report set/unset WITHOUT ever returning the secret value. Check the
	// encrypted store first, then the legacy cowork.env as a pre-migration
	// fallback (so the panel still shows "已设置" right after upgrade, before
	// the first startup migration runs).
	if smtp.PasswordEnv != "" {
		v.SMTPPasswordSet = secretIsSet(smtp.PasswordEnv, env)
	}
	if imap.PasswordEnv != "" {
		v.IMAPPasswordSet = secretIsSet(imap.PasswordEnv, env)
	}
	return v
}

// scanPPTXTemplates scans a directory for .pptx files and returns them as template views.
func scanPPTXTemplates(dir string) []ppttemplate.View {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []ppttemplate.View
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".pptx" {
			continue
		}
		name := e.Name()
		id := strings.TrimSuffix(name, filepath.Ext(name))
		out = append(out, ppttemplate.View{
			ID:   id,
			Name: name,
		})
	}
	return out
}

// updatePPTSkillConfig updates a single key in the PPT skill's template_config.json.
func (a *App) updatePPTSkillConfig(key, value string) error {
	// Find the skill config file in the app directory
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, ".momapeer", "skills", "ppt-auto", "template_config.json")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil // config not found, skip silently
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read ppt config: %w", err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse ppt config: %w", err)
	}

	cfg[key] = value

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ppt config: %w", err)
	}

	return os.WriteFile(configPath, out, 0644)
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

	// Persist secrets to the encrypted store. A blank password means "leave
	// the existing one untouched" (standard password-field UX), so we only
	// write when the user typed something. The live process env is updated too
	// so the change takes effect without an app restart — the tools read via
	// os.Getenv(passwordEnv), which is the in-memory decrypted view.
	store := secret.Default()
	if pwd := strings.TrimSpace(v.SMTPPassword); pwd != "" {
		if err := store.Set(v.SMTP.PasswordEnv, pwd); err != nil {
			return fmt.Errorf("save smtp secret: %w", err)
		}
		os.Setenv(v.SMTP.PasswordEnv, pwd)
	}
	if pwd := strings.TrimSpace(v.IMAPPassword); pwd != "" {
		if err := store.Set(v.IMAP.PasswordEnv, pwd); err != nil {
			return fmt.Errorf("save imap secret: %w", err)
		}
		os.Setenv(v.IMAP.PasswordEnv, pwd)
	}

	// Persist non-secret config via the standard edit pipeline.
	if err := a.applyConfigOnly(func(c *config.Config) error {
		c.Cowork.BrowserPath = strings.TrimSpace(v.BrowserPath)
		c.Cowork.EmbeddingModel = strings.TrimSpace(v.EmbeddingModel)
		c.Cowork.PPTActiveTemplate = strings.TrimSpace(v.PPTActiveTemplate)
		c.Cowork.PPTMode = strings.TrimSpace(v.PPTMode)
		c.Cowork.ScreenshotEnabled = v.ScreenshotEnabled
		c.Cowork.ScreenshotHotkey = strings.TrimSpace(v.ScreenshotHotkey)
		c.Cowork.ScreenshotVLMModel = strings.TrimSpace(v.ScreenshotVLMModel)
		c.Cowork.EStopHotkey = strings.TrimSpace(v.EStopHotkey)
		smtp := config.SMTPConfig{
			Host:           strings.TrimSpace(v.SMTP.Host),
			Port:           v.SMTP.Port,
			From:           strings.TrimSpace(v.SMTP.From),
			Username:       strings.TrimSpace(v.SMTP.Username),
			PasswordEnv:    v.SMTP.PasswordEnv,
			UseTLS:         v.SMTP.UseTLS,
			EncryptionMode: strings.TrimSpace(v.SMTP.EncryptionMode),
		}
		imap := config.IMAPConfig{
			Host:        strings.TrimSpace(v.IMAP.Host),
			Port:        v.IMAP.Port,
			Username:    strings.TrimSpace(v.IMAP.Username),
			PasswordEnv: v.IMAP.PasswordEnv,
		}
		c.Cowork.SMTP = smtp
		c.Cowork.IMAP = imap
		// Sync to EmailAccounts: update the default account (or create one)
		// so the multi-account config stays in sync with the panel.
		if len(c.Cowork.EmailAccounts) > 0 {
			for i := range c.Cowork.EmailAccounts {
				if c.Cowork.EmailAccounts[i].Default {
					c.Cowork.EmailAccounts[i].SMTP = smtp
					c.Cowork.EmailAccounts[i].IMAP = imap
					break
				}
			}
		} else if smtp.Host != "" || imap.Host != "" {
			c.Cowork.EmailAccounts = []config.EmailAccount{{
				Name:    "primary",
				Default: true,
				SMTP:    smtp,
				IMAP:    imap,
			}}
		}
		return nil
	}); err != nil {
		return err
	}

	// Refresh the in-memory email accounts cache so the email tools see the
	// updated config without requiring a restart.
	if freshCfg, err := config.Load(); err == nil {
		builtin.SetEmailAccounts(freshCfg.Cowork.EmailAccounts)
	}

	// Also update template_config.json so the PPT skill reads the mode
	mode := strings.TrimSpace(v.PPTMode)
	if mode == "" {
		mode = "fast"
	}
	return a.updatePPTSkillConfig("mode", mode)
}

// MailProbeResult is the outcome of a mailbox connection probe, returned to the
// settings panel so it can show a green/red status dot. The error is always nil
// (a connection failure is reported via Status="error" + Message, not a Go
// error) — this avoids wails surfacing it as a system error dialog. The panel
// triggers a probe after the user saves the mailbox config.
type MailProbeResult struct {
	OK      bool   `json:"ok"`
	Status  string `json:"status"`  // "ok" | "error" | "unconfigured"
	Message string `json:"message"` // human hint, e.g. "IMAP 登录失败：检查授权码"
}

// ProbeMailAccount tests the saved mailbox's IMAP login by actually connecting.
// It reloads config (so a just-saved mailbox is tested, not the stale boot-time
// snapshot) and reuses the same go-imap wiring the email tools use. Powers the
// green/red status dot on the mail card.
func (a *App) ProbeMailAccount() (MailProbeResult, error) {
	cfg, err := config.Load()
	if err != nil {
		return MailProbeResult{Status: "error", Message: "读取配置失败：" + err.Error()}, nil
	}
	// normalizeEmailAccounts folds the legacy [cowork.imap] single-pair into the
	// default account, so reading the default account covers both old/new configs.
	acct, ok := cfg.Cowork.DefaultEmailAccount()
	if !ok || strings.TrimSpace(acct.IMAP.Host) == "" {
		return MailProbeResult{Status: "unconfigured", Message: ""}, nil
	}
	if err := builtin.ProbeIMAPConfig(acct.IMAP); err != nil {
		return MailProbeResult{Status: "error", Message: err.Error()}, nil
	}
	return MailProbeResult{OK: true, Status: "ok", Message: "连接正常"}, nil
}

// InboxItem is one row in the cowork dock's "邮件" tab preview. A trimmed-down
// view of builtin.EmailMessage — the dock only needs envelope + preview, not
// attachments, to keep the JSON payload small for a sidebar list.
type InboxItem struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Preview string `json:"preview"`
}

// InboxPreview reads the most recent unread messages (up to limit) from the
// default mailbox's INBOX, for the cowork dock's "邮件" tab. Like
// ProbeMailAccount, it reloads config so a just-saved mailbox works without
// restart, and it never returns a Go error for expected states (unconfigured /
// connect failure) — those go in the returned slice's absence + an error
// message on the dedicated Err field so wails doesn't pop a system dialog.
// Returns an empty slice when no mailbox is configured.
func (a *App) InboxPreview(limit int) ([]InboxItem, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("读取配置失败：%s", err.Error())
	}
	acct, ok := cfg.Cowork.DefaultEmailAccount()
	if !ok || strings.TrimSpace(acct.IMAP.Host) == "" {
		return []InboxItem{}, nil // unconfigured — dock shows "未配置邮箱"
	}
	msgs, err := builtin.ReadInboxFor(acct.IMAP, limit, true)
	if err != nil {
		return nil, err
	}
	out := make([]InboxItem, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, InboxItem{
			From:    m.From,
			Subject: m.Subject,
			Date:    m.Date,
			Preview: m.Preview,
		})
	}
	return out, nil
}

// CheckCoworkBrowser runs browser auto-detection and returns the display name
// ("Chrome"/"Edge"/…) or "" if none found. Powers the "detect" button in the
// panel so users see what auto-detect found before overriding.
func (a *App) CheckCoworkBrowser() string {
	return detectBrowserForSettings()
}

// OpenPPTTemplateDir opens the PPT templates folder in the OS file manager so the
// user can add/edit JSON templates directly. Powers the "打开模板目录" button. The
// dir is created/seeded first so it always exists when opened.
func (a *App) OpenPPTTemplateDir() error {
	dir := ppttemplate.DefaultDir()
	if dir == "" {
		return fmt.Errorf("无法定位模板目录（用户配置目录不可用）")
	}
	return openInFileExplorer(dir)
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

var coworkMu sync.Mutex

// saveCoworkEnv writes the map back as KEY=VALUE lines (atomic via tmp+rename).
// Also updates the live process env so a save takes effect without restart.
func saveCoworkEnv(m map[string]string) error {
	coworkMu.Lock()
	defer coworkMu.Unlock()
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

// loadCoworkEnvAtStartup is called once during desktop startup. It first lifts
// any plaintext secrets from the legacy cowork.env into the encrypted store
// (one-time migration), then — as a fallback for the rare case the store is
// unavailable — still loads whatever cowork.env remains into the process env.
// CoWork tools read passwords via os.Getenv, which the boot layer also feeds
// from the encrypted store via secret.LoadIntoEnv.
func loadCoworkEnvAtStartup() {
	migrateLegacyCoworkEnv()
	for key, val := range loadCoworkEnv() {
		// Don't override an existing env var (user/system env wins over file).
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

// secretIsSet reports whether a secret for envName is stored — in the encrypted
// store (preferred) or, pre-migration, in the legacy cowork.env map. It never
// returns the value itself.
func secretIsSet(envName string, legacyEnv map[string]string) bool {
	if envName == "" {
		return false
	}
	if _, ok, _ := secret.Default().Get(envName); ok {
		return true
	}
	if legacyEnv != nil {
		if v, ok := legacyEnv[envName]; ok && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// migrateLegacyCoworkEnv moves plaintext secrets from the legacy cowork.env
// into the encrypted store, then removes the plaintext file. Called once at
// startup. No-op when cowork.env is absent. If the store rejects a secret
// (e.g. DPAPI unavailable), migration stops and the plaintext file is kept so
// the feature degrades gracefully — tools still find the password via the
// cowork.env fallback in loadCoworkEnvAtStartup.
func migrateLegacyCoworkEnv() {
	legacy := loadCoworkEnv()
	if len(legacy) == 0 {
		return
	}
	store := secret.Default()
	for key, val := range legacy {
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		// Don't clobber an already-stored secret (the user may have re-entered
		// it via the panel after upgrade).
		if _, ok, _ := store.Get(key); ok {
			continue
		}
		if err := store.Set(key, val); err != nil {
			return // store unavailable — keep the plaintext fallback
		}
	}
	// Every non-empty entry made it into the store; drop the plaintext residue.
	_ = os.Remove(coworkEnvPath())
}
