package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- splitFrontmatter ---

func TestSplitFrontmatterNoFence(t *testing.T) {
	fm, body := splitFrontmatter("just plain text\nno frontmatter")
	if len(fm) != 0 {
		t.Errorf("expected empty fm, got %v", fm)
	}
	if !strings.Contains(body, "just plain text") {
		t.Errorf("body should contain original text: %q", body)
	}
}

func TestSplitFrontmatterUnclosedFence(t *testing.T) {
	input := "---\nname: test\ndescription: desc\n\nsome body without closing fence"
	fm, body := splitFrontmatter(input)
	// Unclosed fence: treat all as body.
	if len(fm) != 0 {
		t.Errorf("unclosed fence should return empty fm, got %v", fm)
	}
	if !strings.Contains(body, "---") {
		t.Errorf("body should contain the original content: %q", body)
	}
}

func TestSplitFrontmatterEmptyBody(t *testing.T) {
	input := "---\nname: test\n---\n"
	fm, body := splitFrontmatter(input)
	if fm["name"] != "test" {
		t.Errorf("name = %q", fm["name"])
	}
	if strings.TrimSpace(body) != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestSplitFrontmatterNestedMetadata(t *testing.T) {
	input := "---\nname: my-fact\ndescription: a desc\nmetadata:\n  type: user\n---\n\nbody here"
	fm, body := splitFrontmatter(input)
	if fm["name"] != "my-fact" {
		t.Errorf("name = %q", fm["name"])
	}
	if fm["description"] != "a desc" {
		t.Errorf("description = %q", fm["description"])
	}
	// The nested "  type: user" should flatten to fm["type"].
	if fm["type"] != "user" {
		t.Errorf("type = %q, expected flattened from metadata", fm["type"])
	}
	if !strings.Contains(body, "body here") {
		t.Errorf("body = %q", body)
	}
}

func TestSplitFrontmatterCRLF(t *testing.T) {
	input := "---\r\nname: test\r\n---\r\nbody\r\n"
	fm, body := splitFrontmatter(input)
	if fm["name"] != "test" {
		t.Errorf("name = %q", fm["name"])
	}
	if !strings.Contains(body, "body") {
		t.Errorf("body = %q", body)
	}
}

func TestSplitFrontmatterQuotedValues(t *testing.T) {
	input := "---\nname: test\ndescription: \"quoted desc\"\n---\n"
	fm, _ := splitFrontmatter(input)
	if fm["description"] != "quoted desc" {
		t.Errorf("description should be unquoted: %q", fm["description"])
	}
}

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

// --- render ---

func TestRenderRoundTrip(t *testing.T) {
	m := Memory{
		Name:        "test-fact",
		Description: "A test fact",
		Type:        TypeUser,
		Body:        "The body of the fact.",
	}
	rendered := render(m, "test-fact")
	fm, body := splitFrontmatter(rendered)
	if fm["name"] != "test-fact" {
		t.Errorf("name = %q", fm["name"])
	}
	if fm["description"] != "A test fact" {
		t.Errorf("description = %q", fm["description"])
	}
	if fm["type"] != "user" {
		t.Errorf("type = %q", fm["type"])
	}
	if !strings.Contains(body, "The body of the fact.") {
		t.Errorf("body = %q", body)
	}
}

func TestRenderNormalizesType(t *testing.T) {
	m := Memory{Name: "x", Description: "d", Type: Type("unknown"), Body: "b"}
	rendered := render(m, "x")
	fm, _ := splitFrontmatter(rendered)
	if fm["type"] != "project" {
		t.Errorf("unknown type should normalize to project, got %q", fm["type"])
	}
}

// --- loadMemory ---

func TestLoadMemoryNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "no-fm.md")
	os.WriteFile(f, []byte("just a body\nno frontmatter"), 0o644)
	m, ok := loadMemory(f)
	if !ok {
		t.Fatal("loadMemory should succeed for files without frontmatter")
	}
	// Name should be derived from filename.
	if m.Name != "no-fm" {
		t.Errorf("name = %q, want no-fm", m.Name)
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

func TestLoadMemoryEmptyFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "empty.md")
	os.WriteFile(f, nil, 0o644)
	m, ok := loadMemory(f)
	if !ok {
		t.Fatal("loadMemory should succeed for empty files")
	}
	if m.Name != "empty" {
		t.Errorf("name = %q", m.Name)
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
	_, err := s.Save(Memory{Name: "", Description: "d", Body: "b"})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestSaveCreatesDir(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: filepath.Join(dir, "deep", "nested", "memory")}
	_, err := s.Save(Memory{Name: "test", Description: "d", Body: "b"})
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

// --- Supersede integration (Phase 2) ---

// TestSaveSupersedeSetsValidTo verifies that saving with the same name archives
// the old record as superseded with ValidTo set.
func TestSaveSupersedeSetsValidTo(t *testing.T) {
	s := Store{Dir: t.TempDir()}

	// Save initial record.
	_, err := s.Save(Memory{
		Name:        "user-location",
		Description: "User lives in Beijing",
		Type:        TypeUser,
		Body:        "User lives in Beijing since 2026-01.",
		ValidFrom:   "2026-01-01",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Save replacement with ValidFrom — old record's ValidTo should be set.
	_, err = s.Save(Memory{
		Name:        "user-location",
		Description: "User lives in Shanghai",
		Type:        TypeUser,
		Body:        "User moved to Shanghai in May.",
		ValidFrom:   "2026-05-01",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Active record should be the new one.
	active, ok := s.Get("user-location")
	if !ok {
		t.Fatal("active record not found")
	}
	if active.Body != "User moved to Shanghai in May." {
		t.Errorf("active body = %q", active.Body)
	}
	if active.Supersedes != "user-location" {
		t.Errorf("Supersedes = %q, want user-location", active.Supersedes)
	}

	// Archive should contain the old record with ValidTo set.
	superseded := s.ListSuperseded("user-location")
	if len(superseded) != 1 {
		t.Fatalf("want 1 superseded, got %d", len(superseded))
	}
	old := superseded[0]
	if old.Status != "superseded" {
		t.Errorf("archived status = %q, want superseded", old.Status)
	}
	if old.ValidTo != "2026-05-01" {
		t.Errorf("archived ValidTo = %q, want 2026-05-01", old.ValidTo)
	}
	if old.SupersededBy != "user-location" {
		t.Errorf("archived SupersededBy = %q", old.SupersededBy)
	}
}

// TestSaveSupersedeValidToDefaultsToToday checks that when the new record has
// no ValidFrom, the old record's ValidTo defaults to today's date.
func TestSaveSupersedeValidToDefaultsToToday(t *testing.T) {
	s := Store{Dir: t.TempDir()}

	_, err := s.Save(Memory{
		Name:        "pref",
		Description: "old preference",
		Type:        TypeUser,
		Body:        "old body",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.Save(Memory{
		Name:        "pref",
		Description: "new preference",
		Type:        TypeUser,
		Body:        "new body",
	})
	if err != nil {
		t.Fatal(err)
	}

	superseded := s.ListSuperseded("pref")
	if len(superseded) != 1 {
		t.Fatalf("want 1 superseded, got %d", len(superseded))
	}
	// ValidTo should be today's date (YYYY-MM-DD format, 10 chars).
	if len(superseded[0].ValidTo) != 10 {
		t.Errorf("ValidTo = %q, want today's date", superseded[0].ValidTo)
	}
}

// TestSaveSupersedeIndexUpdated ensures the MEMORY.md index reflects the new
// description after supersede, not the old one.
func TestSaveSupersedeIndexUpdated(t *testing.T) {
	s := Store{Dir: t.TempDir()}

	s.Save(Memory{Name: "x", Description: "version 1", Type: TypeProject, Body: "b1"})
	s.Save(Memory{Name: "x", Description: "version 2", Type: TypeProject, Body: "b2"})

	idx := s.Index()
	if strings.Contains(idx, "version 1") {
		t.Fatalf("index still contains old description:\n%s", idx)
	}
	if !strings.Contains(idx, "version 2") {
		t.Fatalf("index missing new description:\n%s", idx)
	}
	if n := strings.Count(idx, "x.md"); n != 1 {
		t.Fatalf("want exactly 1 index line, got %d:\n%s", n, idx)
	}
}

// TestSaveSupersedeCreatedAtPreserved checks that CreatedAt is immutable across
// supersede — the new record inherits the original CreatedAt.
func TestSaveSupersedeCreatedAtPreserved(t *testing.T) {
	s := Store{Dir: t.TempDir()}

	// Save with explicit CreatedAt.
	original := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	_, err := s.Save(Memory{
		Name:        "fact",
		Description: "original",
		Type:        TypeProject,
		Body:        "body v1",
		CreatedAt:   original,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Overwrite.
	_, err = s.Save(Memory{
		Name:        "fact",
		Description: "updated",
		Type:        TypeProject,
		Body:        "body v2",
	})
	if err != nil {
		t.Fatal(err)
	}

	active, _ := s.Get("fact")
	if !active.CreatedAt.Equal(original) {
		t.Errorf("CreatedAt = %v, want %v (should be preserved)", active.CreatedAt, original)
	}
}

// --- Phase 7: Decay / TTL / Dormant / Access tracking ---

func TestGetUpdatesAccessTracking(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{Name: "f", Description: "d", Type: TypeProject, Body: "b"})

	m, ok := s.Get("f")
	if !ok {
		t.Fatal("Get failed")
	}
	if m.AccessCount < 1 {
		t.Errorf("AccessCount = %d, want >= 1", m.AccessCount)
	}
	if m.LastAccessedAt.IsZero() {
		t.Error("LastAccessedAt should be set after Get")
	}

	// Second Get should increment.
	m2, _ := s.Get("f")
	if m2.AccessCount < 2 {
		t.Errorf("AccessCount = %d, want >= 2", m2.AccessCount)
	}
}

func TestDecayDowngradesInactive(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	// Save with CreatedAt 60 days ago, no access.
	_, err := s.Save(Memory{
		Name:        "old-fact",
		Description: "old",
		Type:        TypeProject,
		Body:        "b",
		CreatedAt:   time.Now().UTC().Add(-60 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := DecayConfig{DecayDays: 30, ColdDays: 90, HotLimit: 50}
	n, err := s.Decay(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("Decay count = %d, want 1", n)
	}

	// Should be dormant now, not in List().
	list := s.List()
	if len(list) != 0 {
		t.Errorf("List() = %d, want 0 (dormant should be excluded)", len(list))
	}

	// Should be in ListDormant().
	dormant := s.ListDormant()
	if len(dormant) != 1 {
		t.Fatalf("ListDormant() = %d, want 1", len(dormant))
	}
	if dormant[0].Status != "dormant" {
		t.Errorf("status = %q, want dormant", dormant[0].Status)
	}
}

func TestDecayExemptsHighImportance(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{
		Name:        "core-identity",
		Description: "who I am",
		Type:        TypeUser,
		Body:        "engineer",
		Importance:  "high",
		CreatedAt:   time.Now().UTC().Add(-365 * 24 * time.Hour),
	})

	cfg := DecayConfig{DecayDays: 30}
	n, _ := s.Decay(cfg)
	if n != 0 {
		t.Errorf("high-importance fact should not decay, got %d decays", n)
	}
}

func TestDecayLowImportanceFaster(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	// 20 days old, low importance (threshold = 30/2 = 15 days).
	s.Save(Memory{
		Name:        "temp",
		Description: "temp",
		Type:        TypeProject,
		Body:        "b",
		Importance:  "low",
		CreatedAt:   time.Now().UTC().Add(-20 * 24 * time.Hour),
	})

	cfg := DecayConfig{DecayDays: 30}
	n, _ := s.Decay(cfg)
	if n != 1 {
		t.Errorf("low-importance should decay at 15 days, got %d decays", n)
	}
}

func TestExpireTTLArchives(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{
		Name:        "week-goal",
		Description: "this week",
		Type:        TypeProject,
		Body:        "finish phase 7",
		TTL:         "2026-01-01", // already past
	})

	n, err := s.ExpireTTL()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expired = %d, want 1", n)
	}

	// Should be gone from active list.
	if len(s.List()) != 0 {
		t.Errorf("expired fact still in List()")
	}
}

func TestActivateRecallsDormant(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{
		Name:        "hobby",
		Description: "piano",
		Type:        TypeUser,
		Body:        "likes piano",
		CreatedAt:   time.Now().UTC().Add(-60 * 24 * time.Hour),
	})

	// Decay it.
	s.Decay(DecayConfig{DecayDays: 30})
	if len(s.ListDormant()) != 1 {
		t.Fatal("should be dormant")
	}

	// Activate it.
	if err := s.Activate("hobby"); err != nil {
		t.Fatal(err)
	}

	// Should be back in active list.
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("List() = %d, want 1 after activate", len(list))
	}
	if list[0].Status != "active" {
		t.Errorf("status = %q, want active", list[0].Status)
	}

	// Should be gone from dormant.
	if len(s.ListDormant()) != 0 {
		t.Error("still in ListDormant after activate")
	}
}

func TestRememberToolTTLAndImportance(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	tl := NewRememberTool(s, nil)
	ctx := context.Background()

	_, err := tl.Execute(ctx, json.RawMessage(`{
		"name": "goal",
		"description": "weekly goal",
		"type": "project",
		"body": "finish tests",
		"ttl": "2026-12-31",
		"importance": "high"
	}`))
	if err != nil {
		t.Fatal(err)
	}

	m, ok := s.Get("goal")
	if !ok {
		t.Fatal("memory not found")
	}
	if m.TTL != "2026-12-31" {
		t.Errorf("TTL = %q", m.TTL)
	}
	if m.Importance != "high" {
		t.Errorf("Importance = %q", m.Importance)
	}
}

func TestRecallTool(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{
		Name: "x", Description: "d", Type: TypeProject, Body: "b",
		CreatedAt: time.Now().UTC().Add(-60 * 24 * time.Hour),
	})
	s.Decay(DecayConfig{DecayDays: 30})

	tl := NewRecallTool(s)
	ctx := context.Background()
	_, err := tl.Execute(ctx, json.RawMessage(`{"name": "x"}`))
	if err != nil {
		t.Fatal(err)
	}

	m, ok := s.Get("x")
	if !ok || m.Status != "active" {
		t.Errorf("after recall: status = %q, found = %v", m.Status, ok)
	}
}

// --- Phase 8-9: Profile / Compact / Status ---

func TestProfileToolEmpty(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	tl := NewProfileTool(s)
	result, err := tl.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "No active memories") {
		t.Errorf("unexpected: %s", result)
	}
}

func TestProfileToolGroupsByCategory(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{Name: "role", Description: "Backend engineer", Type: TypeUser, Body: "b", Category: "identity"})
	s.Save(Memory{Name: "style", Description: "Concise", Type: TypeUser, Body: "b", Category: "style"})
	s.Save(Memory{Name: "loc", Description: "Shanghai", Type: TypeUser, Body: "b", Category: "temporal", ValidFrom: "2026-05-01"})

	tl := NewProfileTool(s)
	result, err := tl.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Identity") {
		t.Error("missing Identity section")
	}
	if !strings.Contains(result, "Style") {
		t.Error("missing Style section")
	}
	if !strings.Contains(result, "Backend engineer") {
		t.Error("missing identity fact")
	}
	if !strings.Contains(result, "since 2026-05-01") {
		t.Error("missing valid_from on temporal fact")
	}
}

func TestCompactTool(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	// Old fact with no access.
	s.Save(Memory{
		Name: "old", Description: "old fact", Type: TypeProject, Body: "b",
		CreatedAt: time.Now().UTC().Add(-60 * 24 * time.Hour),
	})
	// Recent fact.
	s.Save(Memory{Name: "new", Description: "new fact", Type: TypeProject, Body: "b"})

	tl := NewCompactTool(s, DecayConfig{DecayDays: 30, ColdDays: 90})
	result, err := tl.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "1 fact(s) downgraded") {
		t.Errorf("unexpected compact result: %s", result)
	}
}

func TestStatusTool(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{Name: "a", Description: "active fact", Type: TypeProject, Body: "b"})
	s.Save(Memory{
		Name: "d", Description: "old fact", Type: TypeProject, Body: "b",
		CreatedAt: time.Now().UTC().Add(-60 * 24 * time.Hour),
	})
	s.Decay(DecayConfig{DecayDays: 30})

	tl := NewStatusTool(s, DecayConfig{DecayDays: 30, HotLimit: 50})
	result, err := tl.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Active:") {
		t.Error("missing Active count")
	}
	if !strings.Contains(result, "Dormant:") {
		t.Error("missing Dormant count")
	}
	if !strings.Contains(result, "Hot layer:") {
		t.Error("missing Hot layer info")
	}
}

func TestListByCategory(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{Name: "a", Description: "d", Type: TypeUser, Body: "b", Category: "identity"})
	s.Save(Memory{Name: "b", Description: "d", Type: TypeUser, Body: "b", Category: "style"})
	s.Save(Memory{Name: "c", Description: "d", Type: TypeUser, Body: "b", Category: "identity"})

	if got := len(s.ListByCategory("identity")); got != 2 {
		t.Errorf("identity: got %d, want 2", got)
	}
	if got := len(s.ListByCategory("style")); got != 1 {
		t.Errorf("style: got %d, want 1", got)
	}
	if got := len(s.ListByCategory("")); got != 3 {
		t.Errorf("empty filter: got %d, want 3", got)
	}
}
