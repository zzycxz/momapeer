package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zzycxz/momapeer/internal/config"
	"github.com/zzycxz/momapeer/internal/event"
	"github.com/zzycxz/momapeer/internal/i18n"
	"github.com/zzycxz/momapeer/internal/notify"
	"github.com/zzycxz/momapeer/internal/provider"
)

func TestChdirTo(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if rc := chdirTo(""); rc != 0 {
		t.Fatalf(`chdirTo("") = %d, want 0`, rc)
	}
	if cwd, _ := os.Getwd(); cwd != orig {
		t.Fatalf(`chdirTo("") moved cwd to %q`, cwd)
	}

	tmp := t.TempDir()
	// Restore CWD before TempDir's RemoveAll runs (LIFO ordering): Windows can't
	// delete a directory that is still the process working directory.
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if rc := chdirTo(tmp); rc != 0 {
		t.Fatalf("chdirTo(tmp) = %d, want 0", rc)
	}
	got, _ := filepath.EvalSymlinks(mustGetwd(t))
	want, _ := filepath.EvalSymlinks(tmp)
	if got != want {
		t.Fatalf("cwd = %q, want %q", got, want)
	}

	if rc := chdirTo(filepath.Join(tmp, "does-not-exist")); rc != 2 {
		t.Fatalf("chdirTo(missing) = %d, want 2", rc)
	}
}

func TestReserveNativeScrollbackFrameWritesOnlyNewlines(t *testing.T) {
	var b bytes.Buffer
	reserveNativeScrollbackFrame(&b, 3)
	if got := b.String(); got != "\n\n\n" {
		t.Fatalf("reserveNativeScrollbackFrame wrote %q, want only three newlines", got)
	}

	reserveNativeScrollbackFrame(&b, 0)
	if got := b.String(); got != "\n\n\n" {
		t.Fatalf("reserveNativeScrollbackFrame(0) changed output to %q", got)
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return cwd
}

func isolateCLIConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Chdir(t.TempDir())
	return home
}

func TestMetadataCommandsDoNotProbeTerminalTheme(t *testing.T) {
	defer func(prev func() (terminalRGB, bool)) {
		queryTerminalBackgroundForTheme = prev
	}(queryTerminalBackgroundForTheme)
	queryTerminalBackgroundForTheme = func() (terminalRGB, bool) {
		t.Fatal("metadata command should not query terminal background")
		return terminalRGB{}, false
	}

	out := captureStdout(t, func() {
		if rc := Run([]string{"version"}, "test-version"); rc != 0 {
			t.Fatalf("version rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "momapeer test-version") {
		t.Fatalf("version output = %q", out)
	}

	out = captureStdout(t, func() {
		if rc := Run([]string{"help"}, "test-version"); rc != 0 {
			t.Fatalf("help rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "Usage:") && !strings.Contains(out, "用法：") {
		t.Fatalf("help output missing usage:\n%s", out)
	}
	if !strings.Contains(out, "momapeer run  [--model NAME] [--max-steps N] [-c|--continue] [--resume PATH] <task>") {
		t.Fatalf("help output missing run resume flags:\n%s", out)
	}
}

func TestRunDispatchesACPLongFlagAlias(t *testing.T) {
	errOut := captureStderr(t, func() {
		if rc := Run([]string{"--acp", "-h"}, "test-version"); rc != 2 {
			t.Fatalf("Run --acp -h rc = %d, want 2", rc)
		}
	})
	if !strings.Contains(errOut, "Usage of acp:") {
		t.Fatalf("--acp should dispatch to the ACP command, got stderr:\n%s", errOut)
	}
	if strings.Contains(errOut, "unknown command") {
		t.Fatalf("--acp should not be treated as an unknown command:\n%s", errOut)
	}
}

func TestRunMigratesLegacyConfigBeforeConfigOnlyCommands(t *testing.T) {
	isolateCLIConfigHome(t)
	legacyPath := filepath.Join(filepath.Dir(config.UserConfigPath()), "momapeer.toml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte(`
default_model = "moma"

[[plugins]]
name = "legacy-cli"
command = "legacy-bin"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if rc := Run([]string{"mcp", "list"}, "test-version"); rc != 0 {
			t.Fatalf("mcp list rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "legacy-cli") {
		t.Fatalf("mcp list should include migrated legacy config:\n%s", out)
	}

	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read migrated user config: %v", err)
	}
	for _, want := range []string{`config_version = 2`, `[desktop]`, `name    = "legacy-cli"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("migrated config missing %q:\n%s", want, body)
		}
	}
}

func TestRunMetadataCommandsDoNotMigrateLegacyConfig(t *testing.T) {
	isolateCLIConfigHome(t)
	legacyPath := filepath.Join(filepath.Dir(config.UserConfigPath()), "momapeer.toml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte(`default_model = "moma"`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if rc := Run([]string{"version"}, "test-version"); rc != 0 {
			t.Fatalf("version rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "momapeer test-version") {
		t.Fatalf("version output = %q", out)
	}
	if _, err := os.Stat(config.UserConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("version should not migrate legacy config, stat err=%v", err)
	}
}

func TestConfigAutoPlanCommandWritesUserConfig(t *testing.T) {
	isolateCLIConfigHome(t)

	out := captureStdout(t, func() {
		if rc := Run([]string{"config", "auto-plan", "on"}, "test-version"); rc != 0 {
			t.Fatalf("config auto-plan rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, `auto_plan = "on"`) {
		t.Fatalf("config auto-plan output = %q", out)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.AutoPlan != "on" {
		t.Fatalf("saved auto_plan = %q, want on", cfg.Agent.AutoPlan)
	}
}

func TestConfigAutoPlanLocalCreatesMinimalProjectOverride(t *testing.T) {
	isolateCLIConfigHome(t)

	userCfg := config.Default()
	userCfg.DefaultModel = "moma"
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	out := captureStdout(t, func() {
		if rc := Run([]string{"config", "auto-plan", "--local", "on"}, "test-version"); rc != 0 {
			t.Fatalf("config auto-plan --local rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, `auto_plan = "on"`) {
		t.Fatalf("config auto-plan --local output = %q", out)
	}

	body, err := os.ReadFile("momapeer.toml")
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if strings.Contains(string(body), "default_model") {
		t.Fatalf("project auto-plan override should not pin default_model:\n%s", body)
	}
	if !strings.Contains(string(body), "[agent]") || !strings.Contains(string(body), `auto_plan = "on"`) {
		t.Fatalf("project config missing auto_plan override:\n%s", body)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load merged config: %v", err)
	}
	if cfg.DefaultModel != "moma" {
		t.Fatalf("default_model = %q, want global moma", cfg.DefaultModel)
	}
	if cfg.Agent.AutoPlan != "on" {
		t.Fatalf("auto_plan = %q, want local on", cfg.Agent.AutoPlan)
	}
}

func TestWelcomePromptMissingKeysRequiresConfigSource(t *testing.T) {
	if welcomeShouldPromptMissingKeys("", nil) {
		t.Fatal("built-in defaults without a config source should not prompt for missing provider keys")
	}
	if welcomeShouldPromptMissingKeys("momapeer.toml", errors.New("bad config")) {
		t.Fatal("invalid config should not enter the missing-key prompt path")
	}
	if !welcomeShouldPromptMissingKeys("momapeer.toml", nil) {
		t.Fatal("valid config source should enter the missing-key prompt path")
	}
}

func TestProvidersWithMissingKeysOnlyChecksActiveDefaultModel(t *testing.T) {
	cfg := config.Default()
	t.Setenv("JIUTIAN_API_KEY", "")
	t.Setenv("JIUTIAN_API_KEY", "")

	missing := providersWithMissingKeys(cfg)
	if len(missing) != 1 {
		t.Fatalf("missing providers = %+v, want only active default model provider", missing)
	}
	if missing[0].APIKeyEnv != "JIUTIAN_API_KEY" {
		t.Fatalf("missing key env = %q, want JIUTIAN_API_KEY", missing[0].APIKeyEnv)
	}
}

func TestProvidersWithMissingKeysIgnoresUnusedBuiltInPresets(t *testing.T) {
	cfg := config.Default()
	t.Setenv("JIUTIAN_API_KEY", "test-key")

	if missing := providersWithMissingKeys(cfg); len(missing) != 0 {
		t.Fatalf("missing providers = %+v, want none when key is set", missing)
	}
}

func TestProvidersWithMissingKeysIncludesReferencedSecondaryModels(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.PlannerModel = "moma"
	cfg.Agent.SubagentModel = "moma"
	cfg.Agent.SubagentModels = map[string]string{
		"review": "moma/jiutian/jiutian-lan-35b",
	}
	cfg.Agent.AutoPlanClassifier = "moma/jiutian/jiutian-lan-35b"
	t.Setenv("JIUTIAN_API_KEY", "test-key")
	t.Setenv("JIUTIAN_API_KEY", "")

	missing := providersWithMissingKeys(cfg)
	if len(missing) != 1 {
		t.Fatalf("missing providers = %+v, want MoMA once", missing)
	}
	if missing[0].APIKeyEnv != "JIUTIAN_API_KEY" {
		t.Fatalf("missing key env = %q, want JIUTIAN_API_KEY", missing[0].APIKeyEnv)
	}
}

func TestProvidersWithMissingKeysSkipsDisabledAutoPlanClassifier(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.AutoPlan = "off"
	cfg.Agent.AutoPlanClassifier = "moma/qwen/qwen3.6-35b"
	t.Setenv("JIUTIAN_API_KEY", "")

	if missing := providersWithMissingKeys(cfg); len(missing) != 1 {
		t.Fatalf("missing providers = %+v, want 1 (default model key missing)", missing)
	}

	cfg.Agent.AutoPlan = "on"
	missing := providersWithMissingKeys(cfg)
	// Both default model and auto-plan classifier use the same provider (moma),
	// so only 1 unique missing key entry.
	if len(missing) != 1 {
		t.Fatalf("missing providers = %+v, want 1 (same provider for both)", missing)
	}
	if missing[0].APIKeyEnv != "JIUTIAN_API_KEY" {
		t.Fatalf("missing key env = %q, want JIUTIAN_API_KEY", missing[0].APIKeyEnv)
	}
}

type cliRecordSink struct {
	events []event.Kind
}

func (s *cliRecordSink) Emit(e event.Event) {
	s.events = append(s.events, e.Kind)
}

type cliRecordSender struct {
	messages []notify.Message
}

func (s *cliRecordSender) Send(m notify.Message) error {
	s.messages = append(s.messages, m)
	return nil
}

func TestWithNotificationsWrapsCLISinkWithConfiguredSender(t *testing.T) {
	inner := &cliRecordSink{}
	sender := &cliRecordSender{}
	calls := 0
	prev := newNotificationSender
	newNotificationSender = func() notify.Sender {
		calls++
		return sender
	}
	t.Cleanup(func() { newNotificationSender = prev })

	cfg := config.Default()
	cfg.Notifications.Enabled = true

	wrapped := withNotifications(inner, cfg)
	wrapped.Emit(event.Event{Kind: event.TurnDone})

	if calls != 1 {
		t.Fatalf("newNotificationSender calls = %d, want 1", calls)
	}
	if len(inner.events) != 1 || inner.events[0] != event.TurnDone {
		t.Fatalf("forwarded events = %v, want [TurnDone]", inner.events)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Body != "Turn finished" {
		t.Fatalf("notification body = %q, want Turn finished", sender.messages[0].Body)
	}
}

func TestSetupOverwritePromptShowsYNDefault(t *testing.T) {
	t.Cleanup(func() { i18n.DetectLanguage("en") })
	for _, lang := range []string{"en", "zh"} {
		i18n.DetectLanguage(lang)
		var out bytes.Buffer
		if confirmReconfigureExistingConfig("config.toml", bufio.NewScanner(strings.NewReader("\n")), &out) {
			t.Fatalf("%s empty overwrite answer should keep existing config", lang)
		}
		if !strings.Contains(out.String(), "[y/N]:") {
			t.Fatalf("%s overwrite prompt should show explicit [y/N] default, got %q", lang, out.String())
		}
	}
}

// TestConfigureKeys verifies that a shared api_key_env (each vendor's SKUs use
// the same env var) is asked only once, and entered keys become env lines.
func TestConfigureKeys(t *testing.T) {
	t.Setenv("JIUTIAN_API_KEY", "")
	selected := config.Default().Providers // moma

	input := "ji-key\n"
	env := configureKeys(selected, strings.NewReader(input), io.Discard)

	if len(env) != 1 {
		t.Fatalf("env = %v (want 1: JIUTIAN asked once)", env)
	}
	if env[0] != "JIUTIAN_API_KEY=ji-key" {
		t.Errorf("env[0] = %q", env[0])
	}
}

// TestConfigureKeysReusesExistingEnv covers the "user already typed the key
// in the URL-fetch flow, don't ask again" path. When the env var is set
// (either from .env or from a prior os.Setenv in the wizard), configureKeys
// must NOT consume from the input stream — otherwise the user's next typed
// line bleeds into the next provider's prompt. It also must include the
// existing value in envLines so the value is re-pinned into .env on
// re-runs of setup.
func TestConfigureKeysReusesExistingEnv(t *testing.T) {
	t.Setenv("JIUTIAN_API_KEY", "preset-ji-key") // reuse this one

	selected := config.Default().Providers
	var output bytes.Buffer
	env := configureKeys(selected, strings.NewReader("\n"), &output)

	if len(env) != 1 {
		t.Fatalf("env = %v (want 1: JIUTIAN reused)", env)
	}
	if env[0] != "JIUTIAN_API_KEY=preset-ji-key" {
		t.Errorf("env[0] = %q, want re-pinned existing value", env[0])
	}
	if !strings.Contains(output.String(), "JIUTIAN_API_KEY") {
		t.Errorf("expected a 'reusing' confirmation for JIUTIAN_API_KEY, got:\n%s", output.String())
	}
}

func TestConfigureKeysCanResetExistingEnv(t *testing.T) {
	t.Setenv("JIUTIAN_API_KEY", "stale-ji-key") // reset this one

	selected := config.Default().Providers
	var output bytes.Buffer
	env := configureKeys(selected, strings.NewReader("y\nfresh-ji-key\n"), &output)

	if len(env) != 1 {
		t.Fatalf("env = %v (want 1: JIUTIAN reset)", env)
	}
	if env[0] != "JIUTIAN_API_KEY=fresh-ji-key" {
		t.Errorf("env[0] = %q, want freshly entered value", env[0])
	}
	if !strings.Contains(output.String(), "[y/N]:") || !strings.Contains(output.String(), "JIUTIAN_API_KEY") {
		t.Errorf("expected a reset confirmation for JIUTIAN_API_KEY, got:\n%s", output.String())
	}
}

// TestConfigureKeysAllSetDefaultsToReusingInput ensures that when every env var
// is already populated, pressing Enter at each confirmation keeps the values.
func TestConfigureKeysAllSetDefaultsToReusingInput(t *testing.T) {
	t.Setenv("JIUTIAN_API_KEY", "ji")

	selected := config.Default().Providers
	env := configureKeys(selected, strings.NewReader("\n"), io.Discard)
	if len(env) != 1 {
		t.Errorf("env = %v, want 1 (reused)", env)
	}
}

// TestAppendEnvUpsertReplacesExistingKey covers the bug where re-running the
// wizard with a corrected key would append a second line for the same env
// var. loadDotEnv is first-wins, so without dedupe the stale key kept
// authenticating, and the user saw a 401 with no obvious cause.
func TestAppendEnvUpsertReplacesExistingKey(t *testing.T) {
	t.Setenv("JIUTIAN_API_KEY", "") // also covers the os.Setenv pin path
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("# initial\nJIUTIAN_API_KEY=stale\nJIUTIAN_API_KEY=keepme\n"), 0o600)

	if err := appendEnv(p, []string{"JIUTIAN_API_KEY=fresh"}); err != nil {
		t.Fatalf("appendEnv: %v", err)
	}
	got, _ := os.ReadFile(p)
	// appendEnv deduplicates: all old JIUTIAN_API_KEY lines are replaced with the new one.
	want := "# initial\nJIUTIAN_API_KEY=fresh\n"
	if string(got) != want {
		t.Errorf("after upsert =\n%s\nwant =\n%s", got, want)
	}
	if got := os.Getenv("JIUTIAN_API_KEY"); got != "fresh" {
		t.Errorf("process env JIUTIAN_API_KEY = %q, want %q (upsert should pin in-process)", got, "fresh")
	}
}

// TestAppendEnvUpsertHandlesExportPrefix proves `export FOO=...` style lines
// also get replaced, since users might hand-edit .env in shell-friendly form.
func TestAppendEnvUpsertHandlesExportPrefix(t *testing.T) {
	t.Setenv("FOO", "")
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("export FOO=old\nKEEP=yes\n"), 0o600)
	if err := appendEnv(p, []string{"FOO=new"}); err != nil {
		t.Fatalf("appendEnv: %v", err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "FOO=new") || strings.Contains(string(got), "FOO=old") {
		t.Errorf("export-prefixed line not replaced:\n%s", got)
	}
}

// TestGroupByFamily verifies the wizard groups the default preset into
// one family: moma.
func TestGroupByFamily(t *testing.T) {
	order, members, info := groupByFamily(config.Default().Providers)

	if len(order) != 1 {
		t.Fatalf("family count = %d, want 1: %v", len(order), order)
	}
	if _, ok := members["moma"]; !ok {
		t.Errorf("moma family missing from members")
	}
	if info["moma"].key != "moma" {
		t.Errorf("moma family key = %q, want moma", info["moma"].key)
	}
}

// TestFetchOrFallbackLiveReturns covers the happy path: a live /models call
// succeeds and its result wins over the preset's static list. We can't run
// the real probe (no key) so the FetchModels call is expected to 401 and the
// fallback path runs; the assertion below is that fallback works (static
// list returned) and that an empty base URL short-circuits to the static
// list with no network call.
func TestFetchOrFallback(t *testing.T) {
	t.Run("empty base URL returns static list", func(t *testing.T) {
		probe := config.ProviderEntry{
			BaseURL: "",
			Models:  []string{"preset-a", "preset-b"},
		}
		got := fetchOrFallback(&probe, "Test")
		if !reflect.DeepEqual(got, []string{"preset-a", "preset-b"}) {
			t.Errorf("got %v, want preset-a/b", got)
		}
	})

	t.Run("no key set returns static list (offline first-run)", func(t *testing.T) {
		t.Setenv("MOMAPEER_FETCH_TEST_KEY", "")
		probe := config.ProviderEntry{
			BaseURL:   "http://127.0.0.1:1", // unreachable, no listener
			APIKeyEnv: "MOMAPEER_FETCH_TEST_KEY",
			Models:    []string{"preset-a"},
		}
		got := fetchOrFallback(&probe, "Test")
		if !reflect.DeepEqual(got, []string{"preset-a"}) {
			t.Errorf("got %v, want preset-a", got)
		}
	})
}

// TestFetchModelListCompatWalksCandidates covers the wizard's custom-provider
// model probe. Previously the probe was a single URL (baseURL+"/models"),
// which worked for OpenAI vendors with a /v1 base URL but silently failed
// for Anthropic-style root URLs (no /v1) and Anthropic-compatible proxies
// (a /v1 base URL but a /v1/messages endpoint). The new helper walks
// BuildModelFetchURLs's candidate list — root + /v1 + known compat
// suffixes — so the same probe now succeeds for both shapes, matching
// what the conversation-time client URL will actually be.
func TestFetchModelListCompatWalksCandidates(t *testing.T) {
	t.Run("anthropic root form resolves via v1 fallback", func(t *testing.T) {
		var gotPath atomic.Value
		gotPath.Store("")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath.Store(r.URL.Path)
			if r.URL.Path == "/v1/models" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[{"id":"claude-test"}]}`)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		models, err := fetchModelListCompat(context.Background(), srv.URL, "k")
		if err != nil {
			t.Fatalf("fetchModelListCompat: %v", err)
		}
		if !reflect.DeepEqual(models, []string{"claude-test"}) {
			t.Errorf("models = %v, want [claude-test]", models)
		}
		if got := gotPath.Load().(string); got != "/v1/models" {
			t.Errorf("probe path = %q, want /v1/models (root form should fall through to v1 candidate)", got)
		}
	})

	t.Run("versioned v1 base URL hits models directly", func(t *testing.T) {
		var gotPath atomic.Value
		gotPath.Store("")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath.Store(r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"id":"model-a"}]}`)
		}))
		defer srv.Close()

		models, err := fetchModelListCompat(context.Background(), srv.URL+"/v1", "k")
		if err != nil {
			t.Fatalf("fetchModelListCompat: %v", err)
		}
		if !reflect.DeepEqual(models, []string{"model-a"}) {
			t.Errorf("models = %v, want [model-a]", models)
		}
		if got := gotPath.Load().(string); got != "/v1/models" {
			t.Errorf("probe path = %q, want /v1/models", got)
		}
	})

	t.Run("endpoint-miss on every candidate returns empty (manual flow)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		models, err := fetchModelListCompat(context.Background(), srv.URL, "k")
		if err != nil {
			t.Fatalf("expected graceful empty result on all-miss, got err: %v", err)
		}
		if len(models) != 0 {
			t.Errorf("expected empty models on all-miss, got %v", models)
		}
	})

	t.Run("non-404 network error short-circuits with the real error", func(t *testing.T) {
		// Point at a closed port — connection refused, not a 404.
		models, err := fetchModelListCompat(context.Background(), "http://127.0.0.1:1", "k")
		if err == nil {
			t.Fatalf("expected error for unreachable host, got models=%v", models)
		}
	})
}

// TestFamilyStaticModels proves the offline fallback unions every member of a
// family (the flash + pro SKUs), not just the first — the regression that left
// users with only flash when the live /models probe failed.
func TestFamilyStaticModels(t *testing.T) {
	providers := []config.ProviderEntry{
		{Name: "moma", Model: "qwen3.6-35b"},
		{Name: "moma", Model: "qwen3.6-27b"},
		{Name: "moma", Model: "jiutian-lan.5"},
	}
	got := familyStaticModels(providers, []int{0, 1})
	want := []string{"qwen3.6-35b", "qwen3.6-27b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFamilyStaticModelsDedupes(t *testing.T) {
	providers := []config.ProviderEntry{
		{Name: "a", Models: []string{"x", "y"}},
		{Name: "b", Models: []string{"y", "z"}},
	}
	got := familyStaticModels(providers, []int{0, 1})
	if !reflect.DeepEqual(got, []string{"x", "y", "z"}) {
		t.Errorf("got %v, want x/y/z deduped", got)
	}
}

// TestBuildFamilyEntriesGroupsModels proves models under the same provider name
// land in one entry with all selected models.
func TestBuildFamilyEntriesGroupsModels(t *testing.T) {
	moma := config.ProviderEntry{Name: "moma", BaseURL: "https://api.jiutian.10086.cn", Model: "qwen3.6-35b", Price: &provider.Pricing{Input: 1, Output: 2}}
	got := buildFamilyEntries(moma, []config.ProviderEntry{moma}, []string{"qwen3.6-35b", "qwen3.6-27b"})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got[0].Name != "moma" {
		t.Errorf("entry name = %q, want moma", got[0].Name)
	}
	if len(got[0].Models) != 2 {
		t.Errorf("models = %v, want 2", got[0].Models)
	}
}

// TestBuildFamilyEntriesUnknownModelUsesProbe puts a live-only SKU (no matching
// preset) under the probe entry rather than dropping it.
func TestBuildFamilyEntriesUnknownModelUsesProbe(t *testing.T) {
	flash := config.ProviderEntry{Name: "moma", Model: "qwen3.6-35b", Price: &provider.Pricing{Input: 1}}
	got := buildFamilyEntries(flash, []config.ProviderEntry{flash}, []string{"qwen3.6-35b", "MoMA-v9-experimental"})
	if len(got) != 1 || got[0].Name != "moma" {
		t.Fatalf("got %+v, want one moma entry", got)
	}
	if !reflect.DeepEqual(got[0].Models, []string{"qwen3.6-35b", "MoMA-v9-experimental"}) {
		t.Errorf("Models = %v, want both under the probe entry", got[0].Models)
	}
}

// TestBuildFamilyEntry covers the three observable behaviors:
//   - The selected models land in the entry's Models field, with Model
//     pointed at the first one so legacy single-model lookups still work.
//   - A preset Default that points to a model the user didn't pick is
//     reset to the first selected model (otherwise resolve-by-default
//     would silently break).
//   - A preset Default that IS in the selection is preserved.
func TestBuildFamilyEntry(t *testing.T) {
	t.Run("default reset when not in selection", func(t *testing.T) {
		probe := config.ProviderEntry{
			Name: "MoMA", Kind: "openai",
			BaseURL: "https://api.jiutian.10086.cn",
			Models:  []string{"qwen3.6-35b", "qwen3.6-27b"},
			Default: "qwen3.6-27b",
		}
		got := buildFamilyEntry(probe, []string{"qwen3.6-35b"})
		if got.Model != "qwen3.6-35b" {
			t.Errorf("Model = %q, want qwen3.6-35b", got.Model)
		}
		if got.Default != "qwen3.6-35b" {
			t.Errorf("Default = %q, want reset to first selected", got.Default)
		}
		if !reflect.DeepEqual(got.Models, []string{"qwen3.6-35b"}) {
			t.Errorf("Models = %v", got.Models)
		}
		if got.BaseURL != "https://api.jiutian.10086.cn" {
			t.Errorf("BaseURL lost: %q", got.BaseURL)
		}
	})

	t.Run("default preserved when in selection", func(t *testing.T) {
		probe := config.ProviderEntry{
			Name: "MoMA", Default: "qwen3.6-27b",
			BaseURL: "https://api.jiutian.10086.cn",
		}
		got := buildFamilyEntry(probe, []string{"qwen3.6-35b", "qwen3.6-27b"})
		if got.Default != "qwen3.6-27b" {
			t.Errorf("Default = %q, want preserved", got.Default)
		}
	})

	t.Run("empty default filled from first selected", func(t *testing.T) {
		probe := config.ProviderEntry{Name: "x", BaseURL: "u"}
		got := buildFamilyEntry(probe, []string{"alpha", "beta"})
		if got.Default != "alpha" {
			t.Errorf("Default = %q, want alpha", got.Default)
		}
	})
}

// TestProviderSlug covers the host-derivation rules and the sha1 fallback
// for unparseable URLs. The exact format isn't load-bearing — what matters
// is that the slug (a) starts with the kind prefix, (b) is stable across
// calls with the same URL, and (c) never produces the bare "custom" /
// "anthropic" magic names that would collide with the wizard menu items.
func TestProviderSlug(t *testing.T) {
	cases := []struct {
		name, kind, url, want string
	}{
		{"standard host with port", "custom", "https://token.sensenova.cn/v1", "custom-token-sensenova-cn"},
		{"api subdomain", "custom", "https://api.openai.com/v1", "custom-api-openai-com"},
		{"www stripped", "custom", "https://www.example.com/v1", "custom-example-com"},
		{"port preserved", "custom", "http://localhost:11434/v1", "custom-localhost-11434"},
		{"anthropic kind", "anthropic", "https://api.anthropic.com", "anthropic-api-anthropic-com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := providerSlug(tc.kind, tc.url); got != tc.want {
				t.Errorf("providerSlug(%q, %q) = %q, want %q", tc.kind, tc.url, got, tc.want)
			}
		})
	}

	t.Run("stable across calls", func(t *testing.T) {
		a := providerSlug("custom", "https://token.sensenova.cn/v1")
		b := providerSlug("custom", "https://token.sensenova.cn/v1")
		if a != b {
			t.Errorf("not stable: %q vs %q", a, b)
		}
		if a == "custom" {
			t.Error("slug degenerated to bare magic name — collision risk")
		}
	})

	t.Run("sha1 fallback for unparseable URL", func(t *testing.T) {
		got := providerSlug("custom", "://not a url::://")
		if !strings.HasPrefix(got, "custom-") || got == "custom" {
			t.Errorf("fallback slug = %q, want custom-<hex>", got)
		}
		// sha1 is 40 hex chars; we take 4 bytes (8 hex chars).
		if len(got) != len("custom-")+8 {
			t.Errorf("fallback slug = %q, want 8 hex chars after prefix", got)
		}
	})
}

// TestFilterStaleCustomEntries covers the wizard's auto-cleanup of legacy
// "custom" / "anthropic" magic-name entries that previous versions wrote
// into momapeer.toml. These collide with the wizard's own menu items, so
// they're dropped from the providers list before grouping — but the caller
// still gets them back in the dropped slice to surface a warning.
func TestFilterStaleCustomEntries(t *testing.T) {
	in := []config.ProviderEntry{
		{Name: "MoMA", Kind: "openai", BaseURL: "https://api.jiutian.10086.cn"},
		{Name: "custom", Kind: "openai", BaseURL: "https://old.example/v1"},                // stale
		{Name: "anthropic", Kind: "anthropic", BaseURL: "https://old.example/v1/messages"}, // stale
		{Name: "MoMA-tp", Kind: "openai", BaseURL: "https://token-plan-cn.jiutian.10086.cn/v1"},
	}
	kept, dropped := filterStaleCustomEntries(in)
	if len(kept) != 2 {
		t.Errorf("kept = %d entries, want 2: %+v", len(kept), kept)
	}
	if len(dropped) != 2 {
		t.Errorf("dropped = %d entries, want 2: %+v", len(dropped), dropped)
	}
	for _, k := range kept {
		if k.Name == "custom" || k.Name == "anthropic" {
			t.Errorf("magic name leaked through: %q", k.Name)
		}
	}

	t.Run("non-magic names with kind anthropic are kept", func(t *testing.T) {
		// An entry someone deliberately named "claude" (kind=anthropic) must
		// not be touched by the filter — only the bare "anthropic" magic name.
		in := []config.ProviderEntry{
			{Name: "claude", Kind: "anthropic", BaseURL: "https://api.anthropic.com"},
		}
		kept, dropped := filterStaleCustomEntries(in)
		if len(kept) != 1 || len(dropped) != 0 {
			t.Errorf("claude should be kept, got kept=%d dropped=%d", len(kept), len(dropped))
		}
	})

	t.Run("custom kind anthropic is kept", func(t *testing.T) {
		// Name="custom" with kind=anthropic is ambiguous — keep it.
		in := []config.ProviderEntry{
			{Name: "custom", Kind: "anthropic", BaseURL: "https://x"},
		}
		kept, dropped := filterStaleCustomEntries(in)
		if len(kept) != 1 || len(dropped) != 0 {
			t.Errorf("custom+anthropic should be kept (ambiguous), got kept=%d dropped=%d", len(kept), len(dropped))
		}
	})
}

func TestWithBuiltinFamiliesAddsMissingMoMA(t *testing.T) {
	// The user's case: a momapeer.toml that defines only moma providers.
	cfg := []config.ProviderEntry{
		{Name: "moma", Kind: "openai", BaseURL: "https://api.jiutian.10086.cn"},
	}
	order, _, info := groupByFamily(withBuiltinFamilies(cfg))
	seen := map[string]bool{}
	for _, k := range order {
		seen[info[k].name] = true
	}
	if !seen["MoMA (九天)"] {
		t.Fatalf("wizard families = %v, want MoMA (九天)", order)
	}
	// A user's customized moma must not be duplicated.
	if n := len(groupByFamilyKeys(withBuiltinFamilies(cfg), "moma")); n != 1 {
		t.Fatalf("moma members = %d, want the user's 1 (no injected duplicate)", n)
	}
}

func groupByFamilyKeys(ps []config.ProviderEntry, key string) []int {
	_, members, _ := groupByFamily(ps)
	return members[key]
}

func TestWriteDefaultConfigDisablesCodegraph(t *testing.T) {
	path := filepath.Join(t.TempDir(), "momapeer.toml")
	if rc := writeDefaultConfig(path); rc != 0 {
		t.Fatalf("writeDefaultConfig rc = %d", rc)
	}
	if c := config.LoadForEdit(path); c.Codegraph.Enabled {
		t.Fatal("a freshly scaffolded config left codegraph enabled; new users should start without it")
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestProvidersWithMissingKeysOnlyReferenced(t *testing.T) {
	t.Setenv("JIUTIAN_API_KEY", "")
	cfg := config.Default()

	got := providersWithMissingKeys(cfg)
	envs := map[string]bool{}
	for _, p := range got {
		envs[p.APIKeyEnv] = true
	}
	if !envs["JIUTIAN_API_KEY"] {
		t.Errorf("the default model's missing key must be prompted, got %v", got)
	}
}

func TestProvidersWithMissingKeysIncludesPlannerModel(t *testing.T) {
	t.Setenv("JIUTIAN_API_KEY", "set")
	t.Setenv("JIUTIAN_API_KEY", "")
	cfg := config.Default()
	cfg.Agent.PlannerModel = "moma"

	got := providersWithMissingKeys(cfg)
	if len(got) != 1 || got[0].APIKeyEnv != "JIUTIAN_API_KEY" {
		t.Errorf("planner model's missing key must be prompted, got %+v", got)
	}
}
