package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/zzycxz/momapeer/internal/config"
)

// TestModelRefsFromConfig verifies the /model picker enumerates configured
// provider/model refs (built-in defaults when no momapeer.toml is present), and
// only those whose provider API key is set.
//
// Uses isolateUserConfig (not just t.Chdir) because modelRefs() reads the USER
// config dir (~/.config/momapeer or %AppData%\momapeer), not the CWD. Without
// isolating it, a real user config on the machine would override the built-in
// defaults and make this test flaky (machine-dependent refs).
func TestModelRefsFromConfig(t *testing.T) {
	isolateUserConfig(t) // no momapeer.toml → built-in default providers
	t.Setenv("JIUTIAN_API_KEY", "test-key")
	refs := modelRefs()
	if len(refs) == 0 {
		t.Fatal("expected default provider/model refs, got none")
	}
	for _, r := range refs {
		if !strings.Contains(r, "/") {
			t.Errorf("ref %q should be provider/model", r)
		}
	}
}

// TestModelRefsSkipsUnconfigured verifies that with no provider keys set, the
// picker offers nothing rather than listing models the user can't select.
func TestModelRefsSkipsUnconfigured(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("JIUTIAN_API_KEY", "")
	if refs := modelRefs(); len(refs) != 0 {
		t.Errorf("no keys set → no refs, got %v", refs)
	}
}

// TestModelArgCompletion verifies "/model " completes to the configured refs
// through the shared completion path.
func TestModelArgCompletion(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("JIUTIAN_API_KEY", "test-key")
	m := newTestChatTUI()
	items, _, ok := m.slashArgItems("/model ")
	if !ok || len(items) == 0 {
		t.Fatalf("/model arg completion should offer refs, ok=%v n=%d", ok, len(items))
	}
}

// TestPersistModelWritesDefaultModel verifies that calling persistModel with a
// "provider/model" ref writes default_model = "<ref>" to the user config file
// in TOML form. This is the fix for the "default model resets on every launch"
// regression: previously /model only mutated the in-memory controller and the
// next startup read the global default.
func TestPersistModelWritesDefaultModel(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("JIUTIAN_API_KEY", "test-key")

	m := newTestChatTUI()
	m.persistModel("moma/qwen/qwen3.6-35b")

	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(body), `default_model = "moma/qwen/qwen3.6-35b"`) {
		t.Fatalf("saved config missing default_model ref:\n%s", body)
	}
}

// TestPersistModelRejectsUnknownRef verifies that an unresolvable ref is
// silently dropped (logged to slog, not pushed to the TUI notice channel)
// and never lands in the config file. Reason: surface a "persist failed"
// notice on the input box would make /model feel broken to users whose
// stored config doesn't list the exact model ref they picked; the in-
// memory switch still goes through.
func TestPersistModelRejectsUnknownRef(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("JIUTIAN_API_KEY", "test-key")

	m := newTestChatTUI()
	m.persistModel("ghost/never-existed")

	if _, err := os.Stat(config.UserConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("unknown ref must not create config file, stat err=%v", err)
	}
}
