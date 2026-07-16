package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// v0.4: this file holds the store basics that survived the slim-down. The old
// second half (supersede/decay/TTL/recall/compact/profile/status tests) was
// removed with the bitemporal machinery it exercised; splitFrontmatter tests
// moved out since the helper is gone (frontmatter.Split is used directly now).

// --- slug ---

func TestSlug(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Prefers Tabs", "prefers-tabs"},
		{"  spaces  ", "spaces"},
		{"CamelCase", "camelcase"},
		{"with/slash", "with-slash"},
		{"", ""},
		{"---", ""},
		{"hello_world", "hello-world"},
	}
	for _, c := range cases {
		got := slug(c.input)
		if got != c.want {
			t.Errorf("slug(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// --- oneLine ---

func TestOneLine(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello world", "hello world"},
		{"  multiple   spaces  ", "multiple spaces"},
		{"tabs\there", "tabs here"},
		{"\n\nnewlines\n\n", "newlines"},
		{"", ""},
	}
	for _, c := range cases {
		got := oneLine(c.input)
		if got != c.want {
			t.Errorf("oneLine(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// --- render / loadMemory round-trip ---

func TestRenderRoundTrip(t *testing.T) {
	m := Memory{
		Name: "test-fact",
		Type: TypeUser,
		Body: "The body of the fact.",
	}
	rendered := render(m, "test-fact")
	// The rendered file must round-trip through loadMemory with the same fields.
	got, ok := loadMemory(writeTemp(t, rendered))
	if !ok {
		t.Fatal("loadMemory failed on rendered output")
	}
	if got.Name != "test-fact" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Type != TypeUser {
		t.Errorf("type = %q", got.Type)
	}
	if !strings.Contains(got.Body, "The body of the fact.") {
		t.Errorf("body = %q", got.Body)
	}
}

func TestRenderPreservesCreatedAt(t *testing.T) {
	m := Memory{Name: "x", Type: TypeProject, Body: "b", CreatedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}
	got, ok := loadMemory(writeTemp(t, render(m, "x")))
	if !ok {
		t.Fatal("loadMemory failed")
	}
	if !got.CreatedAt.Equal(m.CreatedAt) {
		t.Errorf("created_at not preserved: got %v want %v", got.CreatedAt, m.CreatedAt)
	}
}

// --- loadMemory edge cases ---

func TestLoadMemoryNoFrontmatter(t *testing.T) {
	m, ok := loadMemory(writeTemp(t, "just a body\nno frontmatter"))
	if !ok {
		t.Fatal("loadMemory should succeed for files without frontmatter")
	}
	if !strings.Contains(m.Body, "just a body") {
		t.Errorf("body = %q", m.Body)
	}
}

func TestLoadMemoryMissingFile(t *testing.T) {
	_, ok := loadMemory("/nonexistent/path.md")
	if ok {
		t.Error("loadMemory should return false for missing file")
	}
}

// --- Store.List edge cases ---

func TestListSkipsNonMdFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "fact.md"), []byte("---\nname: fact\n---\nbody"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a memory"), 0o644)
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# Memory\n"), 0o644)
	s := Store{Dir: dir}
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("want 1 memory, got %d", len(list))
	}
	if list[0].Name != "fact" {
		t.Errorf("name = %q", list[0].Name)
	}
}

func TestListEmptyDir(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if list := s.List(); len(list) != 0 {
		t.Errorf("empty dir should return empty list, got %d", len(list))
	}
}

func TestListSortedByName(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"zebra", "alpha", "middle"} {
		os.WriteFile(filepath.Join(dir, name+".md"), []byte("---\nname: "+name+"\n---\nbody"), 0o644)
	}
	s := Store{Dir: dir}
	list := s.List()
	if len(list) != 3 {
		t.Fatalf("want 3, got %d", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "middle" || list[2].Name != "zebra" {
		t.Errorf("not sorted: %v %v %v", list[0].Name, list[1].Name, list[2].Name)
	}
}

// --- Store.Save edge cases ---

func TestSaveEmptyName(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	_, err := s.Save(Memory{Name: "", Body: "b"})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestSaveCreatesDir(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: filepath.Join(dir, "deep", "nested", "memory")}
	_, err := s.Save(Memory{Name: "test", Body: "b"})
	if err != nil {
		t.Fatalf("Save should create dirs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "test.md")); err != nil {
		t.Fatal("memory file should exist")
	}
}

// --- Store.Path ---

func TestStorePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := Store{Dir: dir}
	got := s.Path("My Fact")
	if want := filepath.Join(dir, "my-fact.md"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

// writeTemp writes content to a temp file and returns its path, for loadMemory
// round-trip tests that don't want to go through Save.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

// TestSafeJoinRejectsSiblingPrefixEscape is the security regression for A10:
// before the fix, safeJoin used strings.HasPrefix which a sibling directory
// sharing base's name prefix could bypass (base=".../memory",
// name="../memoryevil/x" → joined ".../memoryevil/x" passed the prefix check).
func TestSafeJoinRejectsSiblingPrefixEscape(t *testing.T) {
	base := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, evil := range []string{
		"../memoryevil/secret",
		"../../etc/passwd",
		"..\\memoryevil\\secret",
	} {
		if _, err := safeJoin(base, evil); err == nil {
			t.Errorf("safeJoin(%q) should have errored, got nil", evil)
		}
	}
	// A legitimate name still works.
	legit, err := safeJoin(base, "normal-fact")
	if err != nil {
		t.Errorf("safeJoin(normal-fact) errored: %v", err)
	}
	if !strings.HasSuffix(legit, "normal-fact") {
		t.Errorf("safeJoin(normal-fact) = %q, want suffix normal-fact", legit)
	}
}
