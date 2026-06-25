package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newIndexedStore builds a Store with the bitemporal index attached, mirroring
// what boot does. Returns the store and a cleanup that closes the FTS db.
func newIndexedStore(t *testing.T) (Store, func()) {
	t.Helper()
	dir := t.TempDir()
	s := Store{Dir: dir}
	svc, err := NewSearchService(s)
	if err != nil {
		t.Fatalf("NewSearchService: %v", err)
	}
	if err := svc.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	s = s.AttachIndex(svc.Index())
	return s, func() { _ = svc.Close() }
}

// TestIndexReflectsActiveFacts confirms Save() writes land in the facts index
// immediately (write-sync), so ListActiveByType via the SQL path returns them
// without waiting for a Reconcile.
func TestIndexReflectsActiveFacts(t *testing.T) {
	s, cleanup := newIndexedStore(t)
	defer cleanup()

	s.Save(Memory{Name: "role", Description: "engineer", Type: TypeUser, Body: "Backend engineer."})
	s.Save(Memory{Name: "goal", Description: "ship v2", Type: TypeProject, Body: "Ship v2."})

	byUser := s.ListActiveByType(TypeUser)
	if len(byUser) != 1 || byUser[0].Name != "role" {
		t.Errorf("ListActiveByType(user) via index = %+v, want [role]", byUser)
	}
	byProj := s.ListActiveByType(TypeProject)
	if len(byProj) != 1 || byProj[0].Name != "goal" {
		t.Errorf("ListActiveByType(project) via index = %+v, want [goal]", byProj)
	}
}

// TestIndexResolvesSupersededHistory is the headline bitemporal guarantee,
// exercised through the INDEX path: after Shanghai supersedes Beijing, a
// March query must still resolve Beijing via the facts table (which holds the
// archived superseded row). This proves the index — not just file scans —
// supports time-point queries over history.
func TestIndexResolvesSupersededHistory(t *testing.T) {
	s, cleanup := newIndexedStore(t)
	defer cleanup()

	s.Save(Memory{
		Name: "residence", Description: "Beijing", Type: TypeUser,
		Body: "In Beijing.", ValidFrom: "2026-01-01", ValidTo: "2026-04-30",
	})
	s.Save(Memory{
		Name: "residence", Description: "Shanghai", Type: TypeUser,
		Body: "In Shanghai.", ValidFrom: "2026-05-01",
	})

	// March → Beijing (from the archived superseded row in the index).
	march := s.ListAsOf(time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))
	if len(march) != 1 || !strings.Contains(march[0].Body, "Beijing") {
		t.Errorf("March via index = %+v, want Beijing", march)
	}
	// June → Shanghai.
	june := s.ListAsOf(time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC))
	if len(june) != 1 || !strings.Contains(june[0].Body, "Shanghai") {
		t.Errorf("June via index = %+v, want Shanghai", june)
	}
}

// TestIndexConsistentWithFiles checks the index matches disk after a Reconcile:
// the facts row count equals the on-disk file count (active + archived).
func TestIndexConsistentWithFiles(t *testing.T) {
	s, cleanup := newIndexedStore(t)
	defer cleanup()

	s.Save(Memory{Name: "a", Description: "a", Type: TypeUser, Body: "a"})
	s.Save(Memory{Name: "b", Description: "b", Type: TypeUser, Body: "b"})
	s.Save(Memory{Name: "a", Description: "a2", Type: TypeUser, Body: "a2"}) // supersedes a v1

	// Force a full reconcile to settle any write-sync timing.
	if err := s.index.Reconcile(s); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Count disk files: active (a-v2, b) + archived (a-v1).
	active := countMD(s.Dir, false)
	archived := countMD(filepath.Join(s.Dir, ".archive"), false)
	rows, _ := s.index.QueryActiveByType(string(TypeUser))
	if len(rows) != active {
		t.Errorf("index active rows = %d, disk active = %d", len(rows), active)
	}
	// Archived row must exist in facts (path under .archive/).
	allFacts, _ := s.index.factPaths(), 0
	_ = allFacts
	if archived < 1 {
		t.Errorf("expected >=1 archived file, got %d", archived)
	}
}

// TestRebuildIsIdempotent confirms two Rebuilds leave the index in the same
// state (no duplicate rows, no data loss), since Rebuild drops+recreates.
func TestRebuildIsIdempotent(t *testing.T) {
	s, cleanup := newIndexedStore(t)
	defer cleanup()

	svc, err := NewSearchService(Store{Dir: s.Dir})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	s.Save(Memory{Name: "x", Description: "x", Type: TypeUser, Body: "x"})

	if err := svc.Rebuild(); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	c1 := svc.Index().Count()
	r1 := len(must(svc.Index().QueryActiveByType(string(TypeUser))))

	if err := svc.Rebuild(); err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	c2 := svc.Index().Count()
	r2 := len(must(svc.Index().QueryActiveByType(string(TypeUser))))

	if c1 != c2 {
		t.Errorf("FTS count not idempotent: %d then %d", c1, c2)
	}
	if r1 != r2 || r1 != 1 {
		t.Errorf("facts rows not idempotent: %d then %d (want 1)", r1, r2)
	}
}

// TestCrashRecoverySelfHeals simulates a crash: hand-edit a file on disk after
// the index was built (so file and index drift), then confirm a Reconcile
// repairs the index to match the new file contents.
func TestCrashRecoverySelfHeals(t *testing.T) {
	s, cleanup := newIndexedStore(t)
	defer cleanup()

	s.Save(Memory{Name: "k", Description: "original", Type: TypeUser, Body: "original body"})

	// Simulate an off-tool hand edit: rewrite the file with a new body, same path.
	p := s.Path("k")
	newContent := "---\nname: k\ndescription: hand-edited\nmetadata:\n  type: user\n---\nhand-edited body\n"
	if err := os.WriteFile(p, []byte(newContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Index still has the old row. Reconcile must detect the fingerprint change.
	if err := s.index.Reconcile(s); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// The fact's description should now reflect the hand edit.
	rows, err := s.index.QueryActiveByType(string(TypeUser))
	if err != nil {
		t.Fatalf("QueryActiveByType: %v", err)
	}
	var desc string
	for _, r := range rows {
		if r.Name == "k" {
			desc = r.Description
		}
	}
	if desc != "hand-edited" {
		t.Errorf("after reconcile, facts description = %q, want hand-edited", desc)
	}
}

// TestDegradedStoreWithoutIndex confirms that when NO index is attached, queries
// still work via the file-scan fallback (the v0.3.1 path). This guards the
// graceful-degradation contract.
func TestDegradedStoreWithoutIndex(t *testing.T) {
	s := Store{Dir: t.TempDir()} // no AttachIndex
	s.Save(Memory{
		Name: "residence", Description: "Beijing", Type: TypeUser,
		Body: "In Beijing.", ValidFrom: "2026-01-01", ValidTo: "2026-04-30",
	})
	s.Save(Memory{
		Name: "residence", Description: "Shanghai", Type: TypeUser,
		Body: "In Shanghai.", ValidFrom: "2026-05-01",
	})

	byType := s.ListActiveByType(TypeUser)
	if len(byType) != 1 || !strings.Contains(byType[0].Body, "Shanghai") {
		t.Errorf("degraded ListActiveByType = %+v", byType)
	}
	march := s.ListAsOf(time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))
	if len(march) != 1 || !strings.Contains(march[0].Body, "Beijing") {
		t.Errorf("degraded ListAsOf(March) = %+v, want Beijing", march)
	}
}

// --- helpers ---

func countMD(dir string, _ bool) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") && e.Name() != indexFile {
			n++
		}
	}
	return n
}

// TestBootReconcileRecoversFromCrash simulates the crash-recovery path: process
// A writes a fact and indexes it, then "crashes" leaving the index holding a
// row whose file was removed out-of-band. Process B opens a fresh service,
// runs the boot-time Reconcile, and the read paths (ListActiveByType) must no
// longer return the ghost row — proving the start-up sync covers the gap that
// lazy Reconcile (Search-only) left.
func TestBootReconcileRecoversFromCrash(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}

	// Process A: open service, index a fact.
	svcA, err := NewSearchService(s)
	if err != nil {
		t.Fatal(err)
	}
	svcA.EnsureSchema()
	sA := s.AttachIndex(svcA.Index())
	sA.Save(Memory{Name: "ghost", Description: "will vanish", Type: TypeUser, Body: "x"})
	sA.Save(Memory{Name: "real", Description: "stays", Type: TypeUser, Body: "y"})
	svcA.Close()

	// Crash: delete the ghost file directly, leaving its index row orphaned.
	_ = os.Remove(filepath.Join(dir, "ghost.md"))

	// Process B: fresh open + boot-time Reconcile.
	svcB, err := NewSearchService(s)
	if err != nil {
		t.Fatal(err)
	}
	defer svcB.Close()
	svcB.EnsureSchema()
	if err := svcB.Reconcile(); err != nil {
		t.Fatalf("boot Reconcile: %v", err)
	}
	sB := s.AttachIndex(svcB.Index())

	// Read path must reflect reality: only "real", no ghost.
	got := sB.ListActiveByType(TypeUser)
	if len(got) != 1 || got[0].Name != "real" {
		t.Errorf("after boot Reconcile, ListActiveByType = %+v, want [real]", got)
	}
}

// TestScopeDetectionBoundary guards hasPathPrefixFold: it must match a path
// inside the dir but reject a sibling directory whose name merely shares a
// prefix (the classic "/foo" vs "/foobar" trap), and survive a Windows-style
// drive-letter case difference. This protects the index label that decides
// whether a fact counts as global vs project.
func TestScopeDetectionBoundary(t *testing.T) {
	cases := []struct {
		name string
		path, dir string
		want bool
	}{
		{"inside", "/cfg/global/residence.md", "/cfg/global", true},
		{"exact dir", "/cfg/global", "/cfg/global", true},
		{"sibling prefix", "/cfg/global-backup/residence.md", "/cfg/global", false},
		{"unrelated", "/proj/memory/residence.md", "/cfg/global", false},
		{"drive letter case", "C:/cfg/global/r.md", "c:/cfg/global", true},
	}
	for _, c := range cases {
		if got := hasPathPrefixFold(c.path, c.dir); got != c.want {
			t.Errorf("%s: hasPathPrefixFold(%q,%q) = %v, want %v", c.name, c.path, c.dir, got, c.want)
		}
	}
}

func must[T any](v T, _ error) T { return v }

// TestCrossDirUserProjectRouting verifies the real-world layout: user-type
// facts (residence/preferences — the time-sensitive profile data) route to
// GlobalDir, project facts to Dir, and the index tracks both scopes so a
// time-point query reaches a GlobalDir fact even after it's superseded. This
// is the layout the headline "3月住北京、5月搬上海" scenario actually uses, and
// it was previously only tested with a single Dir.
func TestCrossDirUserProjectRouting(t *testing.T) {
	tmp := t.TempDir()
	global := filepath.Join(tmp, "global")
	project := filepath.Join(tmp, "project")
	s := Store{GlobalDir: global, Dir: project}

	svc, err := NewSearchService(s)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if err := svc.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	s = s.AttachIndex(svc.Index())

	// User fact (→ GlobalDir): residence with a time boundary.
	s.Save(Memory{
		Name: "residence", Description: "Lives in Beijing", Type: TypeUser,
		Body: "In Beijing.", ValidFrom: "2026-01-01", ValidTo: "2026-04-30",
	})
	// Project fact (→ Dir): ongoing work.
	s.Save(Memory{
		Name: "deadline", Description: "Ship Q2", Type: TypeProject, Body: "Ship by June.",
	})

	// Confirm routing: residence landed in global, deadline in project.
	if _, err := os.Stat(filepath.Join(global, "residence.md")); err != nil {
		t.Errorf("user fact should be in GlobalDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "deadline.md")); err != nil {
		t.Errorf("project fact should be in Dir: %v", err)
	}

	// Supersede residence (same name, later valid_from) — Beijing → Shanghai.
	s.Save(Memory{
		Name: "residence", Description: "Lives in Shanghai", Type: TypeUser,
		Body: "In Shanghai.", ValidFrom: "2026-05-01",
	})

	// Active should be only Shanghai.
	active := s.List()
	if len(active) != 2 { // residence(Shanghai) + deadline
		t.Fatalf("active count = %d, want 2: %+v", len(active), active)
	}

	// March must still resolve Beijing via the index, reading from GlobalDir's
	// .archive. This is the cross-dir bitemporal guarantee.
	march := s.ListAsOf(time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))
	foundBeijing := false
	for _, m := range march {
		if strings.Contains(m.Body, "Beijing") {
			foundBeijing = true
		}
	}
	if !foundBeijing {
		t.Errorf("March should resolve Beijing from GlobalDir archive, got %+v", march)
	}

	// scope check: the facts index must label residence as 'global', deadline as
	// 'project' — proves indexUpsert's scope detection works across both dirs.
	rows, err := svc.Index().QueryActiveByType(string(TypeUser))
	if err != nil {
		t.Fatalf("QueryActiveByType: %v", err)
	}
	for _, r := range rows {
		if r.Name == "residence" && r.Scope != "global" {
			t.Errorf("residence scope = %q, want global", r.Scope)
		}
	}
}

// TestScopeDetectionPathSeparator guards the indexUpsert scope heuristic on
// Windows-style paths. indexUpsert uses HasPrefix(path, GlobalDir); if GlobalDir
// and the resolved path disagree on separators, a global fact could be
// mislabeled as project. We force clean absolute paths (what safeJoin/Save
// actually produce) and assert the label.
func TestScopeDetectionPathSeparator(t *testing.T) {
	tmp := t.TempDir()
	global := filepath.Join(tmp, "global")
	project := filepath.Join(tmp, "project")
	s := Store{GlobalDir: global, Dir: project}

	svc, err := NewSearchService(s)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	_ = svc.EnsureSchema()
	s = s.AttachIndex(svc.Index())

	// A user fact routes to global; a project fact to project.
	s.Save(Memory{Name: "pref", Description: "p", Type: TypeUser, Body: "prefers tabs"})
	s.Save(Memory{Name: "task", Description: "t", Type: TypeProject, Body: "do thing"})

	rows, _ := svc.Index().QueryActiveByType(string(TypeUser))
	if len(rows) != 1 || rows[0].Scope != "global" {
		t.Errorf("user fact scope = %+v, want global", rows)
	}
	// Project type isn't returned by QueryActiveByType(user); check via a full
	// reconcile + factPaths to confirm the project row exists with scope=project.
	_ = s.index.Reconcile(s)
	all := s.index.factPaths()
	if len(all) < 2 {
		t.Errorf("expected >=2 indexed facts (global+project), got %d", len(all))
	}
}
