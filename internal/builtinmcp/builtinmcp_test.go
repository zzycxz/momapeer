package builtinmcp

import (
	"fmt"
	"testing"

	"github.com/zzycxz/momapeer/internal/config"
)

func TestEntriesContainsContext7(t *testing.T) {
	entries := Entries()
	if len(entries) == 0 {
		t.Fatal("Entries() returned empty")
	}
	var found bool
	for _, e := range entries {
		if e.Name == Context7Name {
			found = true
			if e.Type != "stdio" {
				t.Errorf("context7 type = %q, want stdio", e.Type)
			}
			if e.Tier != "lazy" {
				t.Errorf("context7 tier = %q, want lazy", e.Tier)
			}
		}
	}
	if !found {
		t.Fatal("context7 missing from Entries()")
	}
}

func TestContext7CommandFallsBackThroughJSRunners(t *testing.T) {
	orig := lookPath
	defer func() { lookPath = orig }()

	// Simulate: only pnpm on PATH
	lookPath = func(name string) (string, error) {
		if name == "pnpm" {
			return "/usr/bin/pnpm", nil
		}
		return "", fmt.Errorf("not found: %s", name)
	}
	cmd, args := context7Command()
	if cmd != "pnpm" {
		t.Errorf("context7Command cmd = %q, want pnpm", cmd)
	}
	wantArgs := []string{"dlx", "@upstash/context7-mcp"}
	if len(args) != len(wantArgs) {
		t.Fatalf("context7Command args = %v, want %v", args, wantArgs)
	}
	for i, a := range args {
		if a != wantArgs[i] {
			t.Errorf("context7Command args[%d] = %q, want %q", i, a, wantArgs[i])
		}
	}
}

func TestContext7CommandFallsBackToNpx(t *testing.T) {
	orig := lookPath
	defer func() { lookPath = orig }()

	// Simulate: nothing on PATH
	lookPath = func(name string) (string, error) {
		return "", fmt.Errorf("not found: %s", name)
	}
	cmd, args := context7Command()
	if cmd != "npx" {
		t.Errorf("context7Command cmd = %q, want npx (fallback)", cmd)
	}
	if len(args) != 2 || args[0] != "-y" || args[1] != "@upstash/context7-mcp" {
		t.Errorf("context7Command args = %v, want [-y @upstash/context7-mcp]", args)
	}
}

func TestEntry(t *testing.T) {
	e, ok := Entry(Context7Name)
	if !ok {
		t.Fatal("Entry(context7) not found")
	}
	if e.Name != Context7Name {
		t.Errorf("Entry(context7).Name = %q", e.Name)
	}

	_, ok = Entry("nonexistent")
	if ok {
		t.Error("Entry(nonexistent) should not be found")
	}
}

func TestIsBuiltIn(t *testing.T) {
	if !IsBuiltIn(Context7Name) {
		t.Error("IsBuiltIn(context7) = false, want true")
	}
	if IsBuiltIn("nonexistent") {
		t.Error("IsBuiltIn(nonexistent) = true, want false")
	}
}

func TestAppendEnabled(t *testing.T) {
	configured := []config.PluginEntry{
		{Name: "my-server", Command: "my-server"},
	}

	got := AppendEnabled(nil, configured, []string{Context7Name})
	if len(got) != 1 || got[0].Name != Context7Name {
		t.Fatalf("AppendEnabled = %v, want [context7]", got)
	}
}

func TestAppendEnabledSkipsAlreadyConfigured(t *testing.T) {
	configured := []config.PluginEntry{
		{Name: Context7Name, Command: "custom-cmd"},
	}

	got := AppendEnabled(nil, configured, []string{Context7Name})
	if len(got) != 0 {
		t.Fatalf("AppendEnabled should skip configured context7, got %v", got)
	}
}

func TestAppendEnabledSkipsReserved(t *testing.T) {
	got := AppendEnabled(nil, nil, []string{Context7Name}, Context7Name)
	if len(got) != 0 {
		t.Fatalf("AppendEnabled should skip reserved context7, got %v", got)
	}
}

func TestAppendEnabledOnlyAppendsEnabled(t *testing.T) {
	// Request context7 but don't include it in enabledNames
	got := AppendEnabled(nil, nil, []string{})
	if len(got) != 0 {
		t.Fatalf("AppendEnabled with empty enabled = %v, want empty", got)
	}
}
