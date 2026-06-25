package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Bitemporal field round-trip ---

func TestBitemporalRenderRoundTrip(t *testing.T) {
	m := Memory{
		Name:        "user-residence",
		Title:       "User residence",
		Description: "Where the user lives",
		Type:        TypeUser,
		Body:        "Lives in Shanghai.",
		CreatedAt:   time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 6, 24, 10, 30, 0, 0, time.UTC),
		ValidFrom:   "2026-05-01",
		ValidTo:     "",
		Status:      "active",
	}
	rendered := render(m, "user-residence")
	fm, body := splitFrontmatter(rendered)

	if fm["created_at"] == "" {
		t.Error("created_at not written to frontmatter")
	}
	if fm["updated_at"] == "" {
		t.Error("updated_at not written to frontmatter")
	}
	if fm["valid_from"] != "2026-05-01" {
		t.Errorf("valid_from = %q", fm["valid_from"])
	}
	if fm["status"] != "active" {
		t.Errorf("status = %q", fm["status"])
	}
	if !strings.Contains(body, "Shanghai") {
		t.Errorf("body = %q", body)
	}
}

func TestBitemporalLoadMemoryParsesFields(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "residence.md")
	content := `---
name: residence
description: Where user lives
metadata:
  type: user
created_at: "2026-05-01T10:00:00Z"
updated_at: "2026-06-24T10:30:00Z"
valid_from: 2026-05-01
valid_to: 2026-06-01
status: superseded
supersedes: old-residence
superseded_by: new-residence
---

Lived in Beijing until June.`
	os.WriteFile(f, []byte(content), 0o644)

	m, ok := loadMemory(f)
	if !ok {
		t.Fatal("loadMemory failed")
	}
	if m.CreatedAt.IsZero() {
		t.Error("CreatedAt not parsed")
	}
	if m.CreatedAt.Year() != 2026 || m.CreatedAt.Month() != 5 {
		t.Errorf("CreatedAt = %v", m.CreatedAt)
	}
	if m.UpdatedAt.IsZero() {
		t.Error("UpdatedAt not parsed")
	}
	if m.ValidFrom != "2026-05-01" {
		t.Errorf("ValidFrom = %q", m.ValidFrom)
	}
	if m.ValidTo != "2026-06-01" {
		t.Errorf("ValidTo = %q", m.ValidTo)
	}
	if m.Status != "superseded" {
		t.Errorf("Status = %q", m.Status)
	}
	if m.Supersedes != "old-residence" {
		t.Errorf("Supersedes = %q", m.Supersedes)
	}
	if m.SupersededBy != "new-residence" {
		t.Errorf("SupersededBy = %q", m.SupersededBy)
	}
}

// --- Backward compat: old files without bitemporal fields ---

func TestBackwardCompatOldFileDefaultsToActive(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "old-fact.md")
	// Simulate a pre-v0.3.0 file: no bitemporal fields.
	os.WriteFile(f, []byte("---\nname: old-fact\ndescription: an old fact\nmetadata:\n  type: project\n---\n\nSome body."), 0o644)

	m, ok := loadMemory(f)
	if !ok {
		t.Fatal("loadMemory failed")
	}
	if m.Status != "active" {
		t.Errorf("old file should default to active, got %q", m.Status)
	}
	// CreatedAt should fall back to file mtime.
	if m.CreatedAt.IsZero() {
		t.Error("CreatedAt should fall back to file mtime, got zero")
	}
}

func TestBackwardCompatOldFileListShowsActive(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	// Write an old-style file.
	os.WriteFile(filepath.Join(dir, "old.md"), []byte("---\nname: old\ndescription: old fact\nmetadata:\n  type: project\n---\nbody"), 0o644)
	// Write a new-style superseded file.
	os.WriteFile(filepath.Join(dir, "gone.md"), []byte("---\nname: gone\ndescription: gone fact\nmetadata:\n  type: project\nstatus: superseded\n---\nbody"), 0o644)

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("want 1 active memory, got %d", len(list))
	}
	if list[0].Name != "old" {
		t.Errorf("got %q, want old", list[0].Name)
	}
}

// --- Get method ---

func TestGetActiveMemory(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{Name: "my-fact", Description: "test", Type: TypeProject, Body: "b"})

	m, ok := s.Get("my-fact")
	if !ok {
		t.Fatal("Get should find active memory")
	}
	if m.Name != "my-fact" {
		t.Errorf("name = %q", m.Name)
	}
}

func TestGetNonexistentReturnsFalse(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	_, ok := s.Get("does-not-exist")
	if ok {
		t.Error("Get should return false for missing memory")
	}
}

// --- Supersede mechanism ---

func TestSupersedeMovesToArchive(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	// Save original.
	s.Save(Memory{Name: "residence", Description: "Lives in Beijing", Type: TypeUser, Body: "Beijing."})
	if _, err := os.Stat(filepath.Join(s.Dir, "residence.md")); err != nil {
		t.Fatal("original should exist")
	}

	// Supersede (no successor name in this test).
	if err := s.Supersede("residence", "2026-05-01", ""); err != nil {
		t.Fatal(err)
	}

	// Original should be gone from active dir.
	if _, err := os.Stat(filepath.Join(s.Dir, "residence.md")); !os.IsNotExist(err) {
		t.Fatal("original should be moved to archive")
	}

	// Archive should contain it with status=superseded.
	list := s.ListArchived()
	found := false
	for _, a := range list {
		if a.Name == "residence" {
			found = true
			if a.Status != "superseded" {
				t.Errorf("archive status = %q, want superseded", a.Status)
			}
			if a.ValidTo != "2026-05-01" {
				t.Errorf("archive ValidTo = %q", a.ValidTo)
			}
		}
	}
	if !found {
		t.Error("superseded memory not found in archive")
	}

	// Index should not contain it.
	if strings.Contains(s.Index(), "residence.md") {
		t.Error("superseded entry still in index")
	}
}

func TestSupersedeChainSaveOverwrite(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	// Save first version.
	s.Save(Memory{Name: "residence", Description: "Beijing", Type: TypeUser, Body: "In Beijing."})
	// Save second version (same name) — should auto-supersede.
	s.Save(Memory{Name: "residence", Description: "Shanghai", Type: TypeUser, Body: "In Shanghai."})

	// Active list should have the new version.
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("want 1 active, got %d", len(list))
	}
	if !strings.Contains(list[0].Body, "Shanghai") {
		t.Errorf("active body = %q", list[0].Body)
	}
	if list[0].Supersedes != "residence" {
		t.Errorf("Supersedes = %q", list[0].Supersedes)
	}

	// Archive should have the old version.
	archived := s.ListArchived()
	found := false
	for _, a := range archived {
		if a.Name == "residence" && a.Status == "superseded" {
			found = true
			if !strings.Contains(a.Body, "Beijing") {
				t.Errorf("archived body = %q", a.Body)
			}
		}
	}
	if !found {
		t.Error("old version not in archive")
	}
}

func TestListSuperseded(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	// Create two versions.
	s.Save(Memory{Name: "residence", Description: "v1", Type: TypeUser, Body: "Beijing."})
	s.Save(Memory{Name: "residence", Description: "v2", Type: TypeUser, Body: "Shanghai."})

	history := s.ListSuperseded("residence")
	if len(history) != 1 {
		t.Fatalf("want 1 superseded, got %d", len(history))
	}
	if !strings.Contains(history[0].Body, "Beijing") {
		t.Errorf("superseded body = %q", history[0].Body)
	}
}

// --- ListAsOf ---

func TestListAsOfTimePoint(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{
		Name: "residence", Description: "Beijing", Type: TypeUser,
		Body: "In Beijing.", ValidFrom: "2026-01-01", ValidTo: "2026-04-30",
	})
	s.Save(Memory{
		Name: "residence2", Description: "Shanghai", Type: TypeUser,
		Body: "In Shanghai.", ValidFrom: "2026-05-01",
	})
	s.Save(Memory{
		Name: "timeless", Description: "Always true", Type: TypeProject,
		Body: "Some fact.",
	})

	// March: should get Beijing + timeless.
	march := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	results := s.ListAsOf(march)
	names := map[string]bool{}
	for _, m := range results {
		names[m.Name] = true
	}
	if !names["residence"] {
		t.Error("March should include Beijing residence")
	}
	if names["residence2"] {
		t.Error("March should NOT include Shanghai residence")
	}
	if !names["timeless"] {
		t.Error("March should include timeless fact")
	}

	// June: should get Shanghai + timeless (Beijing expired).
	june := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	results = s.ListAsOf(june)
	names = map[string]bool{}
	for _, m := range results {
		names[m.Name] = true
	}
	if names["residence"] {
		t.Error("June should NOT include Beijing (expired)")
	}
	if !names["residence2"] {
		t.Error("June should include Shanghai")
	}
	if !names["timeless"] {
		t.Error("June should include timeless fact")
	}
}

func TestListAsOfNoTimeConstraint(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{Name: "fact", Description: "a fact", Type: TypeProject, Body: "b"})

	results := s.ListAsOf(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if len(results) != 1 {
		t.Fatalf("timeless fact should appear at any date, got %d", len(results))
	}
}

// --- List excludes superseded ---

func TestListExcludesSuperseded(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{Name: "active-fact", Description: "active", Type: TypeProject, Body: "b"})
	s.Save(Memory{Name: "gone-fact", Description: "was active", Type: TypeProject, Body: "old"})
	s.Supersede("gone-fact", "2026-01-01", "")

	list := s.List()
	for _, m := range list {
		if m.Name == "gone-fact" {
			t.Error("List should not include superseded memories")
		}
	}
}

// --- memory_query tool ---

func TestMemoryQueryToolListsAll(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	store.Save(Memory{Name: "fact1", Description: "first fact", Type: TypeProject, Body: "b1"})
	store.Save(Memory{Name: "fact2", Description: "second fact", Type: TypeUser, Body: "b2"})

	tl := NewMemoryQueryTool(store, nil)
	out, err := tl.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "fact1") || !strings.Contains(out, "fact2") {
		t.Errorf("output missing memories: %s", out)
	}
}

func TestMemoryQueryToolAsOf(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	store.Save(Memory{
		Name: "beijing", Description: "Beijing period", Type: TypeUser,
		Body: "In Beijing.", ValidFrom: "2026-01-01", ValidTo: "2026-04-30",
	})
	store.Save(Memory{
		Name: "shanghai", Description: "Shanghai period", Type: TypeUser,
		Body: "In Shanghai.", ValidFrom: "2026-05-01",
	})

	tl := NewMemoryQueryTool(store, nil)
	out, err := tl.Execute(context.Background(), []byte(`{"as_of":"2026-03-15"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "beijing") {
		t.Errorf("March query should include beijing: %s", out)
	}
	if strings.Contains(out, "shanghai") {
		t.Errorf("March query should NOT include shanghai: %s", out)
	}
}

func TestMemoryQueryToolReadOnly(t *testing.T) {
	tl := NewMemoryQueryTool(Store{}, nil)
	if !tl.ReadOnly() {
		t.Error("memory_query should be read-only")
	}
}

// --- FTS SearchAsOf ---

func TestFTSSearchAsOf(t *testing.T) {
	dir := t.TempDir()
	fts, err := OpenFTSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fts.Close()

	// Index two memories with different valid_from/valid_to.
	fts.UpsertWithTime("/beijing.md", "global", "user", "Beijing residence", "Lives in Beijing", "Lives in Beijing", "active", "2026-01-01", "2026-04-30", "fp1")
	fts.UpsertWithTime("/shanghai.md", "global", "user", "Shanghai residence", "Lives in Shanghai", "Lives in Shanghai", "active", "2026-05-01", "", "fp2")

	// March: should find Beijing.
	march := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	results, err := fts.SearchAsOf("Lives", 10, 0.15, march)
	if err != nil {
		t.Fatal(err)
	}
	foundBeijing, foundShanghai := false, false
	for _, r := range results {
		if strings.Contains(r.Path, "beijing") {
			foundBeijing = true
		}
		if strings.Contains(r.Path, "shanghai") {
			foundShanghai = true
		}
	}
	if !foundBeijing {
		t.Error("March should find Beijing")
	}
	if foundShanghai {
		t.Error("March should NOT find Shanghai")
	}

	// June: should find Shanghai only.
	june := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	results, err = fts.SearchAsOf("Lives", 10, 0.15, june)
	if err != nil {
		t.Fatal(err)
	}
	foundBeijing, foundShanghai = false, false
	for _, r := range results {
		if strings.Contains(r.Path, "beijing") {
			foundBeijing = true
		}
		if strings.Contains(r.Path, "shanghai") {
			foundShanghai = true
		}
	}
	if foundBeijing {
		t.Error("June should NOT find Beijing (expired)")
	}
	if !foundShanghai {
		t.Error("June should find Shanghai")
	}
}

func TestFTSSearchFiltersSuperseded(t *testing.T) {
	dir := t.TempDir()
	fts, err := OpenFTSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fts.Close()

	fts.UpsertWithTime("/active.md", "project", "project", "", "", "Active fact", "active", "", "", "fp1")
	fts.UpsertWithTime("/old.md", "project", "project", "", "", "Old fact", "superseded", "", "", "fp2")

	results, err := fts.Search("fact", 10, 0.15)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if strings.Contains(r.Path, "old") {
			t.Error("Search should not return superseded memories")
		}
	}
}

// --- Schema versioning ---

func TestSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	fts, err := OpenFTSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fts.Close()

	v := fts.SchemaVersion()
	if v != 1 {
		t.Errorf("initial schema version = %d, want 1", v)
	}

	if err := fts.SetSchemaVersion(2); err != nil {
		t.Fatal(err)
	}
	v = fts.SchemaVersion()
	if v != 2 {
		t.Errorf("after update, schema version = %d, want 2", v)
	}
}

// --- Conflict detector ---

func TestConflictDetectorNilIsNoop(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	tl := NewRememberTool(store, nil) // nil detector

	// Save initial fact.
	tl.Execute(context.Background(), []byte(`{"name":"residence","description":"Beijing","type":"user","body":"In Beijing."}`))
	// Save conflicting fact — should just overwrite (no LLM detection).
	tl.Execute(context.Background(), []byte(`{"name":"residence","description":"Shanghai","type":"user","body":"In Shanghai."}`))

	list := store.List()
	if len(list) != 1 {
		t.Fatalf("want 1 active, got %d", len(list))
	}
	if !strings.Contains(list[0].Body, "Shanghai") {
		t.Errorf("should have latest version: %s", list[0].Body)
	}
}

func TestConflictDetectorMockConflicting(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	// Mock detector that always says "conflict".
	detector := NewLLMConflictDetector(func(ctx context.Context, prompt string) (string, error) {
		return "conflict", nil
	})
	tl := NewRememberTool(store, detector)

	tl.Execute(context.Background(), []byte(`{"name":"residence","description":"Beijing","type":"user","body":"In Beijing.","valid_from":"2026-01-01"}`))
	tl.Execute(context.Background(), []byte(`{"name":"residence","description":"Shanghai","type":"user","body":"In Shanghai.","valid_from":"2026-05-01"}`))

	list := store.List()
	if len(list) != 1 {
		t.Fatalf("want 1 active, got %d", len(list))
	}
	if !strings.Contains(list[0].Body, "Shanghai") {
		t.Error("new fact should be active")
	}
	if list[0].Supersedes != "residence" {
		t.Errorf("Supersedes = %q", list[0].Supersedes)
	}
}

func TestConflictDetectorMockCompatible(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	// Mock detector that always says "compatible".
	detector := NewLLMConflictDetector(func(ctx context.Context, prompt string) (string, error) {
		return "compatible", nil
	})
	tl := NewRememberTool(store, detector)

	tl.Execute(context.Background(), []byte(`{"name":"likes-go","description":"Likes Go","type":"user","body":"Go is great."}`))
	tl.Execute(context.Background(), []byte(`{"name":"likes-python","description":"Likes Python","type":"user","body":"Python is great."}`))

	list := store.List()
	if len(list) != 2 {
		t.Fatalf("compatible facts should both survive, got %d", len(list))
	}
}

// --- 1.10: regression tests for the v0.3.1 fixes ---

// TestListAsOfAfterRealSupersede is the central bitemporal guarantee: after a
// same-name record is genuinely superseded (moved to .archive/, status=
// superseded), a time-point query for the OLD window must still return the old
// fact. The pre-fix ListAsOf only scanned List() (active), so it silently
// returned nothing for any superseded period — the headline "3月住北京、5月搬
// 上海" failure. This test would have failed against the old implementation.
func TestListAsOfAfterRealSupersede(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	// Beijing, valid Jan–Apr.
	s.Save(Memory{
		Name: "residence", Description: "Lived in Beijing", Type: TypeUser,
		Body: "In Beijing.", ValidFrom: "2026-01-01", ValidTo: "2026-04-30",
	})
	// Shanghai, valid May onward — same name, so Save() supersedes Beijing.
	s.Save(Memory{
		Name: "residence", Description: "Lives in Shanghai", Type: TypeUser,
		Body: "In Shanghai.", ValidFrom: "2026-05-01",
	})

	// Only the Shanghai record should be active now.
	active := s.List()
	if len(active) != 1 || !strings.Contains(active[0].Body, "Shanghai") {
		t.Fatalf("active should be only Shanghai, got %+v", active)
	}

	// March must still resolve to Beijing, even though it is superseded.
	march := s.ListAsOf(time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))
	if len(march) != 1 {
		t.Fatalf("March should resolve to exactly 1 (Beijing), got %d: %+v", len(march), march)
	}
	if !strings.Contains(march[0].Body, "Beijing") {
		t.Errorf("March should return Beijing, got %q", march[0].Body)
	}

	// June must resolve to Shanghai.
	june := s.ListAsOf(time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC))
	if len(june) != 1 || !strings.Contains(june[0].Body, "Shanghai") {
		t.Errorf("June should return Shanghai, got %+v", june)
	}
}

// TestConflictDetectionDifferentName verifies the headline scenario: two
// contradictory facts with DIFFERENT names ("住北京" vs "住上海") must not both
// stay active. The old remember.Execute only checked the same-name record via
// Get(), so different-name contradictions coexisted and the agent would answer
// either city at random. Now we scan all same-type active records.
func TestConflictDetectionDifferentName(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	// Detector that flags anything containing "Shanghai" as conflicting.
	detector := NewLLMConflictDetector(func(ctx context.Context, prompt string) (string, error) {
		if strings.Contains(prompt, "Shanghai") {
			return "conflict", nil
		}
		return "compatible", nil
	})
	tl := NewRememberTool(store, detector)

	// Save Beijing under one name.
	tl.Execute(context.Background(), []byte(`{"name":"address","description":"住北京","type":"user","body":"User lives in Beijing.","valid_from":"2026-01-01"}`))
	// Save Shanghai under a DIFFERENT name — old code would not detect this.
	tl.Execute(context.Background(), []byte(`{"name":"location","description":"住上海","type":"user","body":"User lives in Shanghai.","valid_from":"2026-05-01"}`))

	active := store.List()
	if len(active) != 1 {
		t.Fatalf("after conflict, exactly 1 active fact should remain (Shanghai), got %d: %+v", len(active), active)
	}
	if !strings.Contains(active[0].Body, "Shanghai") {
		t.Errorf("the surviving active fact should be Shanghai, got %q", active[0].Body)
	}
	// Beijing must have been archived as superseded, not deleted.
	archived := store.ListArchived()
	foundSuperseded := false
	for _, a := range archived {
		if strings.Contains(a.Body, "Beijing") && a.Status == "superseded" {
			foundSuperseded = true
			if a.SupersededBy != "location" {
				t.Errorf("Beijing SupersededBy = %q, want location", a.SupersededBy)
			}
		}
	}
	if !foundSuperseded {
		t.Error("Beijing should be archived as superseded, not deleted")
	}
}

// TestExpireTTLInvokedByCompact confirms memory_compact now archives expired
// TTL facts (Step 0). Before the fix ExpireTTL had no caller at all, so
// time-bounded facts lived forever.
func TestExpireTTLInvokedByCompact(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	// A fact whose TTL is in the past.
	store.Save(Memory{
		Name: "weekly-goal", Description: "ship v1", Type: TypeProject,
		Body: "Ship by Friday.", TTL: "2020-01-01", // long past
	})
	tl := NewCompactTool(store, DefaultDecayConfig())

	out, err := tl.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Step 0") {
		t.Errorf("compact output should mention Step 0 (expire TTL): %s", out)
	}
	// The fact should be gone from active and present in archive.
	if list := store.List(); len(list) != 0 {
		t.Errorf("expired TTL fact should be archived, still active: %+v", list)
	}
	if archived := store.ListArchived(); len(archived) == 0 {
		t.Error("expired TTL fact should be in .archive/")
	}
}

// TestExpireTTLMalformedSkipped ensures a malformed TTL is skipped, not
// silently archived by lexicographic comparison (the old string-compare bug).
func TestExpireTTLMalformedSkipped(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{
		Name: "weird", Description: "bad ttl", Type: TypeProject,
		Body: "x", TTL: "not-a-date",
	})
	n, err := s.ExpireTTL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("malformed TTL should be skipped, archived %d", n)
	}
	if list := s.List(); len(list) != 1 {
		t.Errorf("malformed-TTL fact should remain active, got %d", len(list))
	}
}

// TestDecayArchivesNotDeletes verifies that Decay's cross-directory cleanup
// archives stale copies instead of permanently deleting them. The old code used
// os.Remove, violating "old facts are never lost".
func TestDecayArchivesNotDeletes(t *testing.T) {
	// Set up a store where GlobalDir != Dir and a user-type fact exists in BOTH
	// (simulating a pre-routing migration leftover).
	tmp := t.TempDir()
	global := filepath.Join(tmp, "global")
	project := filepath.Join(tmp, "project")
	s := Store{GlobalDir: global, Dir: project}

	// Old-style file sitting in the project dir but type=user (belongs in global).
	// We plant it directly so Decay's cross-dir cleanup has something to move.
	old := Memory{
		Name: "residence", Description: "Beijing", Type: TypeUser,
		Body: "In Beijing.", CreatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	os.MkdirAll(project, 0o755)
	os.WriteFile(filepath.Join(project, "residence.md"), []byte(render(old, "residence")), 0o644)
	os.MkdirAll(global, 0o755)
	os.WriteFile(filepath.Join(global, "residence.md"), []byte(render(old, "residence")), 0o644)

	// Decay with a 0-day threshold so it fires immediately; importance != high.
	cfg := DecayConfig{DecayDays: 1, ColdDays: 1, HotLimit: 50}
	dormantN, err := s.Decay(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if dormantN == 0 {
		t.Fatal("expected at least one fact to decay")
	}

	// The cross-dir copy in `project` must have been archived, not deleted.
	archived := s.ListArchived()
	if len(archived) == 0 {
		t.Fatal("Decay should have archived the cross-dir copy, not deleted it")
	}
}

// TestRememberWithCategoryAndTags confirms the new schema fields land on disk.
func TestRememberWithCategoryAndTags(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	tl := NewRememberTool(store, nil)
	tl.Execute(context.Background(), []byte(`{
		"name":"role","description":"Backend engineer","type":"user",
		"body":"User is a backend engineer.","category":"identity","tags":["go","distributed"]
	}`))

	m, ok := store.Get("role")
	if !ok {
		t.Fatal("Get failed")
	}
	if m.Category != "identity" {
		t.Errorf("Category = %q, want identity", m.Category)
	}
	if len(m.Tags) != 2 || m.Tags[0] != "go" || m.Tags[1] != "distributed" {
		t.Errorf("Tags = %v, want [go distributed]", m.Tags)
	}
}

// TestNormalizeCategoryUnknownIsEmpty ensures unknown categories map to "" so
// they fall through to "Other" rather than polluting the taxonomy.
func TestNormalizeCategoryUnknownIsEmpty(t *testing.T) {
	if NormalizeCategory("nonsense") != "" {
		t.Error("unknown category should normalize to empty")
	}
	if NormalizeCategory("identity") != "identity" {
		t.Error("known category should pass through")
	}
	if NormalizeCategory("IDENTITY") != "identity" {
		t.Error("category should be case-normalized")
	}
}

// TestListAsOfDayGranularity checks the boundary: valid_to == query date must
// still count as valid on that day (no one-day gap when a successor starts the
// same day the predecessor ends).
func TestListAsOfDayGranularity(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{
		Name: "a", Description: "A", Type: TypeUser,
		Body: "A", ValidFrom: "2026-01-01", ValidTo: "2026-05-01",
	})
	onLastDay := s.ListAsOf(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	if len(onLastDay) != 1 {
		t.Errorf("query on the valid_to day should still be in range, got %d", len(onLastDay))
	}
}

// --- ListTimeline (UI history view) ---

// TestListTimelineIncludesSuperseded verifies the timeline surface includes
// records that List() deliberately hides: a superseded record should appear
// (pulled from .archive/) alongside the active successor. This is the contract
// the desktop MemoryHistory() API and the timeline UI depend on.
func TestListTimelineIncludesSuperseded(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	// Save then supersede a record to move the old version into .archive/.
	s.Save(Memory{
		Name: "residence", Description: "Beijing", Type: TypeUser,
		Body: "In Beijing.", ValidFrom: "2026-01-01", ValidTo: "2026-04-30",
	})
	if err := s.Supersede("residence", "2026-04-30", "residence-shanghai"); err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	s.Save(Memory{
		Name: "residence-shanghai", Description: "Shanghai", Type: TypeUser,
		Body: "In Shanghai.", ValidFrom: "2026-05-01",
	})

	active := s.List()
	if len(active) != 1 || active[0].Name != "residence-shanghai" {
		t.Fatalf("List() = %v, want only the active successor", namesOf(active))
	}

	timeline := s.ListTimeline()
	got := namesOf(timeline)
	if !got["residence"] {
		t.Errorf("ListTimeline should include the superseded Beijing record, got %v", got)
	}
	if !got["residence-shanghai"] {
		t.Errorf("ListTimeline should include the active Shanghai record, got %v", got)
	}
}

// TestListTimelineNewestFirst confirms records sort by ValidFrom descending so
// the timeline reads top-down (most recent change first). Timeless records
// (no ValidFrom/CreatedAt) sink to the bottom.
func TestListTimelineNewestFirst(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{Name: "old", Description: "old", Type: TypeUser, Body: "b", ValidFrom: "2026-01-01"})
	s.Save(Memory{Name: "new", Description: "new", Type: TypeUser, Body: "b", ValidFrom: "2026-06-01"})
	s.Save(Memory{Name: "timeless", Description: "no date", Type: TypeProject, Body: "b"})

	timeline := s.ListTimeline()
	order := namesOf(timeline)
	// 'new' must come before 'old'; 'timeless' must be last.
	newIdx, oldIdx, timelessIdx := -1, -1, -1
	for i, m := range timeline {
		if m.Name == "new" {
			newIdx = i
		}
		if m.Name == "old" {
			oldIdx = i
		}
		if m.Name == "timeless" {
			timelessIdx = i
		}
	}
	if newIdx < 0 || oldIdx < 0 || timelessIdx < 0 {
		t.Fatalf("missing records in timeline: %+v", order)
	}
	if !(newIdx < oldIdx) {
		t.Errorf("expected new (Jun) before old (Jan), got new=%d old=%d", newIdx, oldIdx)
	}
	if !(oldIdx < timelessIdx) {
		t.Errorf("expected timeless record last, got old=%d timeless=%d", oldIdx, timelessIdx)
	}
}

func namesOf(ms []Memory) map[string]bool {
	out := map[string]bool{}
	for _, m := range ms {
		out[m.Name] = true
	}
	return out
}

// --- Promote / Reject (pending auto-captured memories, P1) ---

// TestPromoteMemoryFlipsPendingToActive verifies the user's "confirm" action
// on an auto-captured fact: a pending record becomes active (entering the
// prompt/profile) and is then visible via List().
func TestPromoteMemoryFlipsPendingToActive(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	// Simulate an auto-captured fact saved as pending by the turn-end hook.
	s.Save(Memory{
		Name: "prefers-dark-mode", Description: "Prefers dark mode",
		Type: TypeUser, Body: "User prefers dark mode UI.", Status: "pending",
	})

	// Pending records are excluded from List() (the active prompt view).
	if len(s.List()) != 0 {
		t.Fatalf("pending record should not appear in List(), got %d", len(s.List()))
	}

	if !s.PromoteMemory("prefers-dark-mode") {
		t.Fatal("PromoteMemory returned false, want true")
	}

	active := s.List()
	if len(active) != 1 || active[0].Name != "prefers-dark-mode" || active[0].Status != "active" {
		t.Fatalf("after promote, List() = %+v, want one active record", active)
	}
}

// TestPromoteMemoryRejectsNonPending confirms promote is a guarded transition:
// it only works on pending records. Promoting an already-active or superseded
// record is a no-op (returns false) so a misclick can't resurrect a superseded
// fact.
func TestPromoteMemoryRejectsNonPending(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{Name: "active-fact", Description: "active", Type: TypeProject, Body: "b"})

	if s.PromoteMemory("active-fact") {
		t.Error("PromoteMemory on an active record should return false (no-op)")
	}

	// Missing record.
	if s.PromoteMemory("does-not-exist") {
		t.Error("PromoteMemory on a missing record should return false")
	}
}

// TestRejectMemoryDeletesPending verifies the user's "dismiss" action removes
// a pending fact entirely, and that it only affects pending records (not
// active or superseded ones).
func TestRejectMemoryDeletesPending(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{Name: "pending-fact", Description: "p", Type: TypeUser, Body: "b", Status: "pending"})
	s.Save(Memory{Name: "active-fact", Description: "a", Type: TypeUser, Body: "b"})

	// Reject the pending one.
	if !s.RejectMemory("pending-fact") {
		t.Fatal("RejectMemory on pending record returned false, want true")
	}

	// The active record must be untouched.
	if len(s.List()) != 1 || s.List()[0].Name != "active-fact" {
		t.Fatalf("after rejecting pending, List() should still have the active fact, got %v", s.List())
	}

	// Rejecting an active record is a no-op (can't destroy confirmed facts).
	if s.RejectMemory("active-fact") {
		t.Error("RejectMemory on an active record should return false (protected)")
	}
	if len(s.List()) != 1 {
		t.Fatalf("active fact should survive a reject attempt, got %d", len(s.List()))
	}
}

// TestListTimelineIncludesPending verifies the timeline surface shows pending
// auto-captured records (sorted last) so the user can review them — even
// though List() and the active prompt exclude them.
func TestListTimelineIncludesPending(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Save(Memory{Name: "active", Description: "a", Type: TypeUser, Body: "b", ValidFrom: "2026-06-01"})
	s.Save(Memory{Name: "captured", Description: "c", Type: TypeUser, Body: "b", Status: "pending"})

	timeline := s.ListTimeline()
	names := namesOf(timeline)
	if !names["captured"] {
		t.Error("ListTimeline should include the pending auto-captured record")
	}
	if !names["active"] {
		t.Error("ListTimeline should include the active record")
	}

	// Pending sorts last.
	lastIdx := len(timeline) - 1
	if timeline[lastIdx].Name != "captured" {
		t.Errorf("pending record should sort last, got %q at position %d", timeline[lastIdx].Name, lastIdx)
	}
}
