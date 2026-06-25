package memory

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zzycxz/momapeer/internal/frontmatter"
)

// indexMu serializes MEMORY.md read-modify-write per directory. The store is a
// value type copied freely (Store{Dir:...}), so a per-instance mutex would
// split across copies and fail to serialize. A package-level map keyed by the
// canonical directory path makes every Save/Archive/Delete touching the same
// index file atomic, regardless of which Store value initiated it. Without
// this, concurrent remember calls (desktop panel edit + model tool call) race
// and one update clobbers the other's index line.
var indexMu = struct {
	sync.Mutex
	m map[string]*sync.Mutex
}{m: map[string]*sync.Mutex{}}

// accessMu serializes per-file access tracking writes in Get(). Concurrent
// Get() calls on the same memory file would otherwise race on Read-Modify-Write
// of AccessCount and risk file corruption on Windows (NTFS non-atomic writes).
var accessMu sync.Map // map[string]*sync.Mutex

// indexLockFor returns the mutex guarding dir's MEMORY.md, lazily creating it.
// The outer mutex protects the map itself; the returned mutex serializes index
// writes for that directory.
func indexLockFor(dir string) *sync.Mutex {
	indexMu.Lock()
	defer indexMu.Unlock()
	mu, ok := indexMu.m[dir]
	if !ok {
		mu = &sync.Mutex{}
		indexMu.m[dir] = mu
	}
	return mu
}

// Store is the per-project auto-memory: a directory of one-fact-per-file
// Markdown notes with frontmatter, plus a MEMORY.md index of one line per fact.
// The model maintains it through the `remember` tool; the index loads into the
// cached system-prompt prefix at boot so the model always knows what it has
// saved, and reads individual facts on demand with read_file. The whole thing is
// plain files the user can edit by hand.
type Store struct {
	Dir       string // ...momapeer/projects/<slug>/memory (project-specific)
	GlobalDir string // ...momapeer/memory/global (shared across projects)

	// index is the optional bitemporal index (FTS + facts). When attached,
	// writes keep it in sync and queries prefer it over a directory walk,
	// falling back to file scans when nil or on error. It is a pointer so the
	// value-type Store can be copied freely (controller reload, tool binding)
	// while every copy shares one index handle.
	index *FTSStore
}

// AttachIndex attaches the bitemporal index so writes stay in sync and queries
// can use it. Returns s for chaining. Passing nil detaches (queries fall back
// to file scans). Called by boot after NewSearchService opens the index.
func (s Store) AttachIndex(idx *FTSStore) Store {
	s.index = idx
	return s
}

// hasIndex reports whether a usable index is attached.
func (s Store) hasIndex() bool { return s.index != nil && s.index.db != nil }

// ArchivedMemory is a saved fact that has been removed from active memory but
// kept on disk for traceability.
type ArchivedMemory struct {
	Memory
	Path       string
	ArchivedAt time.Time
}

// Type classifies a memory, mirroring the auto-memory taxonomy.
type Type string

const (
	TypeUser      Type = "user"      // who the user is: role, preferences, expertise
	TypeFeedback  Type = "feedback"  // guidance on how to work (with why + how-to-apply)
	TypeProject   Type = "project"   // ongoing work / goals / constraints not in the code
	TypeReference Type = "reference" // pointers to external resources (URLs, tickets)
)

// validTypes is the closed set the `remember` tool accepts; anything else
// normalises to TypeProject.
var validTypes = map[Type]bool{TypeUser: true, TypeFeedback: true, TypeProject: true, TypeReference: true}

// NormalizeType coerces an arbitrary string to a known Type, defaulting to
// TypeProject so a sloppy tool argument never blocks a save.
func NormalizeType(s string) Type {
	t := Type(strings.ToLower(strings.TrimSpace(s)))
	if validTypes[t] {
		return t
	}
	return TypeProject
}

// validCategories is the closed set of profile buckets the `remember` tool's
// category field accepts. These mirror the groupings memory_profile renders, so
// a user-typed fact lands in the right profile section instead of defaulting
// every fact to the catch-all "Other" bucket (which is what happened when the
// remember schema omitted category entirely).
var validCategories = map[string]bool{
	"identity": true, // who the user is: role, name, residence
	"style":    true, // work preferences, communication style
	"belief":   true, // technical opinions
	"temporal": true, // time-sensitive attributes
	"feedback": true, // guidance to the agent
}

// NormalizeCategory coerces an arbitrary string to a known profile category,
// returning "" for anything unrecognized so an unknown value simply stays
// unbucketed (falling through to "Other" in memory_profile) rather than
// polluting the taxonomy.
func NormalizeCategory(s string) string {
	c := strings.ToLower(strings.TrimSpace(s))
	if validCategories[c] {
		return c
	}
	return ""
}

// Memory is one stored fact. The bitemporal fields track both system time
// (CreatedAt/UpdatedAt — when the record was written) and valid time
// (ValidFrom/ValidTo — when the fact holds true in the real world). Status
// governs visibility: "active" (default) shows in List/Search/prompt;
// "superseded" means a newer record replaced it; "archived" means it was
// explicitly forgotten; "dormant" means it decayed due to inactivity.
type Memory struct {
	Name        string // kebab-case slug; also the file stem (<name>.md)
	Title       string // human-readable index label; falls back to a de-kebabed Name
	Description string // one-line summary used for the index and recall
	Type        Type
	Body        string // the fact itself (Markdown)

	// === Bitemporal fields (added v0.3.0) ===
	CreatedAt    time.Time `json:"created_at,omitempty"`     // system time: first write (immutable)
	UpdatedAt    time.Time `json:"updated_at,omitempty"`     // system time: last modification
	ValidFrom    string    `json:"valid_from,omitempty"`     // valid time: YYYY-MM-DD, when fact became true
	ValidTo      string    `json:"valid_to,omitempty"`       // valid time: YYYY-MM-DD, when fact stopped being true ("" = still valid)
	Status       string    `json:"status,omitempty"`         // active / superseded / archived / dormant
	Supersedes   string    `json:"supersedes,omitempty"`     // name of the record this one replaces
	SupersededBy string    `json:"superseded_by,omitempty"`  // name of the record that replaced this one

	// === Access tracking (Phase 7) ===
	LastAccessedAt time.Time `json:"last_accessed_at,omitempty"` // last time this fact was read
	AccessCount    int       `json:"access_count,omitempty"`     // number of times read

	// === Lifecycle (Phase 7) ===
	TTL        string `json:"ttl,omitempty"`        // YYYY-MM-DD, auto-archive when expired
	Importance string `json:"importance,omitempty"` // high / medium / low (default)

	// === Classification (Phase 8) ===
	Category string   `json:"category,omitempty"` // identity / style / belief / temporal / feedback
	Tags     []string `json:"tags,omitempty"`      // free-form labels for grouping
}

// StoreFor resolves the auto-memory directory for a project working dir under
// the user config root, e.g. ~/.config/momapeer/projects/-Users-me-proj/memory.
// A "" userDir (config dir unresolvable) yields a zero Store, which all methods
// treat as a disabled no-op.
func StoreFor(userDir, cwd string) Store {
	if userDir == "" {
		return Store{}
	}
	return Store{
		Dir:       filepath.Join(userDir, "projects", slugify(absOf(cwd)), "memory"),
		GlobalDir: filepath.Join(userDir, "memory", "global"),
	}
}

// DirFor returns the directory a memory of the given type should be stored in.
// TypeUser and TypeFeedback go to GlobalDir (shared across all projects);
// everything else goes to the project-specific Dir. When GlobalDir is empty,
// all types fall back to Dir.
func (s Store) DirFor(t Type) string {
	if s.GlobalDir != "" && (t == TypeUser || t == TypeFeedback) {
		return s.GlobalDir
	}
	return s.Dir
}

// dirs returns the directories to read from, in order: GlobalDir first (shared
// memories), then Dir (project-specific).
func (s Store) dirs() []string {
	if s.GlobalDir != "" && s.GlobalDir != s.Dir {
		return []string{s.GlobalDir, s.Dir}
	}
	return []string{s.Dir}
}

// indexFile is the human-readable index of saved memories.
const indexFile = "MEMORY.md"

// slugify turns an absolute project path into a single filesystem-safe segment,
// matching the auto-memory convention (path separators → '-'), e.g.
// "/Users/me/proj" → "-Users-me-proj".
func slugify(absPath string) string {
	r := strings.NewReplacer(string(os.PathSeparator), "-", "/", "-", "\\", "-", ":", "-")
	return r.Replace(absPath)
}

// Index returns the MEMORY.md contents (the per-line index of saved memories),
// or "" if there are none yet. This is what loads into the cached prefix.
// When both GlobalDir and Dir have indexes, they are merged with deduplication,
// preserving source grouping: global memories first, then project memories,
// each group sorted alphabetically — rather than a single flat alphabetical
// sort that interleaves the two scopes and loses the "who I am" vs "what I'm
// working on" structure. A name present in both resolves to its global entry
// (global is the broader, user-level truth).
func (s Store) Index() string {
	// group 0 = global, group 1 = project. Collected per-source so output keeps
	// the grouping; names dedup across groups (global wins on collision).
	type entry struct{ line string }
	groups := [2]map[string]entry{{}, {}}
	seen := map[string]int{} // name → group index where it first appeared
	for gi, dir := range s.dirs() {
		if dir == "" || gi > 1 {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, indexFile))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if mt := indexLineRe.FindStringSubmatch(line); mt != nil {
				name := mt[1]
				if _, exists := seen[name]; exists {
					continue // already captured by an earlier (higher-precedence) group
				}
				seen[name] = gi
				groups[gi][name] = entry{line: strings.TrimRight(line, "\r")}
			}
		}
	}
	if len(groups[0])+len(groups[1]) == 0 {
		return ""
	}
	var b strings.Builder
	for gi := 0; gi < 2; gi++ {
		names := make([]string, 0, len(groups[gi]))
		for n := range groups[gi] {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			b.WriteString(groups[gi][n].line)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// Path returns the absolute file path a memory with the given name lives at.
// It checks GlobalDir first, then Dir.
func (s Store) Path(name string) string {
	stem := slug(name) + ".md"
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		p, err := safeJoin(dir, stem)
		if err != nil {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Not found; return the project-dir path as the default save location.
	if s.Dir != "" {
		p, _ := safeJoin(s.Dir, stem)
		return p
	}
	return ""
}

// Save writes (or overwrites) a memory file and refreshes its MEMORY.md index
// line. It is the single mutation entry point — the `remember` tool, the desktop
// editor, and any future importer all go through here so the index never drifts
// from the files. Returns the path written. Memories of type user/feedback are
// routed to GlobalDir; others go to the project-specific Dir.
//
// Bitemporal handling: if an existing file has a CreatedAt, it is preserved
// (CreatedAt is immutable). UpdatedAt is always set to now. Status defaults to
// "active" when empty.
func (s Store) Save(m Memory) (string, error) {
	dir := s.DirFor(m.Type)
	if dir == "" {
		return "", fmt.Errorf("memory store unavailable (no user config dir)")
	}
	name := slug(m.Name)
	if name == "" {
		return "", fmt.Errorf("memory needs a name")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path, err := safeJoin(dir, name+".md")
	if err != nil {
		return "", err
	}

	// Supersede chain: if an active record exists with the same name, archive
	// it as superseded before overwriting. This preserves history and sets
	// ValidTo on the old record so time-point queries (ListAsOf) work correctly.
	if existing, ok := loadMemory(path); ok && (existing.Status == "" || existing.Status == "active") {
		// Preserve CreatedAt from existing record (immutable).
		if !existing.CreatedAt.IsZero() {
			m.CreatedAt = existing.CreatedAt
		}
		// Set ValidTo on the old record: new fact's ValidFrom, or today.
		if existing.ValidTo == "" {
			if m.ValidFrom != "" {
				existing.ValidTo = m.ValidFrom
			} else {
				existing.ValidTo = time.Now().UTC().Format("2006-01-02")
			}
		}
		// Archive the old version.
		existing.Status = "superseded"
		existing.SupersededBy = name
		supersededPath := archiveAsSuperseded(dir, existing)
		m.Supersedes = existing.Name
		// Sync the index for the archived superseded copy so time-point queries
		// reach it immediately (QueryAsOf reads from facts, including history).
		if supersededPath != "" {
			s.indexUpsert(supersededPath)
		}
		// And drop the now-superseded active row.
		s.indexRemove(path)
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	m.UpdatedAt = time.Now().UTC()
	if m.Status == "" {
		m.Status = "active"
	}

	if err := os.WriteFile(path, []byte(render(m, name)), 0o644); err != nil {
		return "", err
	}
	// Reindex in the target directory.
	if err := reindexIn(dir, name, m); err != nil {
		return path, err
	}
	// Remove stale copies from other directories.
	for _, other := range s.dirs() {
		if other != dir {
			stalePath, _ := safeJoin(other, name+".md")
			_ = removeActiveMemoryInDir(other, name) // best-effort; the primary save succeeded
			if stalePath != "" {
				s.indexRemove(stalePath) // drop the stale active row; archive row arrives via Reconcile
			}
		}
	}
	// Keep the bitemporal index in sync with the just-written truth source.
	s.indexUpsert(path)
	return path, nil
}

// Archive removes a memory from the active store and moves its file under
// .archive/ for traceability. A missing file is not an error; the goal state
// (not active) already holds. It returns the archive path, or "" when no file
// existed to archive. When both GlobalDir and Dir exist, it archives from every
// directory the memory appears in (handles migration duplicates).
func (s Store) Archive(name string) (string, error) {
	if s.Dir == "" && s.GlobalDir == "" {
		return "", fmt.Errorf("memory store unavailable (no user config dir)")
	}
	name = slug(name)
	if name == "" {
		return "", fmt.Errorf("memory needs a name")
	}
	var lastPath string
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		activePath, _ := safeJoin(dir, name+".md")
		p, err := archiveInDir(dir, name)
		if err != nil {
			return "", err
		}
		if p != "" || indexContainsIn(dir, name) {
			mu := indexLockFor(dir)
			mu.Lock()
			err := flushIndexIn(dir, indexLinesExceptIn(dir, name))
			mu.Unlock()
			if err != nil {
				return "", err
			}
		}
		if p != "" {
			lastPath = p
			// Drop the now-gone active row from the index; indexUpsert the new
			// archive path so time-point queries see it immediately.
			s.indexRemove(activePath)
			s.indexUpsert(p)
		}
	}
	return lastPath, nil
}

// Delete removes a memory from the active store and its MEMORY.md line — the
// model's `forget` path and the user's way to prune a stale fact. It archives
// the file instead of permanently deleting it so wrong memories remain
// traceable. A missing file is not an error; the goal state (gone) holds either
// way.
func (s Store) Delete(name string) error {
	_, err := s.Archive(name)
	return err
}

// ListArchived returns archived memories parsed from .archive/, newest first.
// Archived files stay out of List() and the prompt index, so stale facts remain
// inspectable without being reused as active truth. Reads from both GlobalDir
// and Dir.
func (s Store) ListArchived() []ArchivedMemory {
	if s.Dir == "" && s.GlobalDir == "" {
		return nil
	}
	var out []ArchivedMemory
	for _, base := range s.dirs() {
		if base == "" {
			continue
		}
		dir := filepath.Join(base, ".archive")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			m, ok := loadMemory(path)
			if !ok {
				continue
			}
			when := archiveTimeFromName(e.Name())
			if when.IsZero() {
				if info, err := e.Info(); err == nil {
					when = info.ModTime()
				}
			}
			out = append(out, ArchivedMemory{Memory: m, Path: path, ArchivedAt: when})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ArchivedAt.After(out[j].ArchivedAt)
	})
	return out
}

func archiveInDir(dir, name string) (string, error) {
	return archiveInDirWithStatus(dir, name, "archived")
}

// archiveAsSuperseded writes a memory to .archive/ with status=superseded.
// Used by Save() to preserve history when a same-name record is overwritten.
// Returns the archive path (or "" on failure) so the caller can sync the index.
func archiveAsSuperseded(dir string, m Memory) string {
	archiveDir := filepath.Join(dir, ".archive")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return ""
	}
	dest, err := archivePath(archiveDir, slug(m.Name), time.Now().UTC())
	if err != nil {
		return ""
	}
	if err := os.WriteFile(dest, []byte(render(m, slug(m.Name))), 0o644); err != nil {
		return ""
	}
	return dest
}

// archiveInDirWithStatus moves a memory file to .archive/ and sets its status.
// Used by Archive (status=archived) and Supersede (status=superseded).
func archiveInDirWithStatus(dir, name, status string) (string, error) {
	file := name + ".md"
	src := filepath.Join(dir, file)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	archiveDir := filepath.Join(dir, ".archive")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", err
	}
	dest, err := archivePath(archiveDir, name, time.Now().UTC())
	if err != nil {
		return "", err
	}
	// Load, update status, and re-render before moving.
	if m, ok := loadMemory(src); ok {
		m.Status = status
		if err := os.WriteFile(dest, []byte(render(m, slug(m.Name))), 0o644); err != nil {
			return "", err
		}
		_ = os.Remove(src)
		return dest, nil
	}
	// Fallback: just move the file.
	if err := renameMemoryFile(src, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// Supersede marks an active memory as superseded, sets its ValidTo, archives it,
// and removes it from the active store. replacedBy is the name of the new
// record that replaces this one and is written into the archived record's
// SupersededBy field, keeping the supersede chain intact for history traversal.
// Pass "" only when there is genuinely no successor. Returns nil if the memory
// doesn't exist.
func (s Store) Supersede(name, validTo, replacedBy string) error {
	name = slug(name)
	if name == "" {
		return nil
	}
	replacedBy = slug(replacedBy)
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		p, err := safeJoin(dir, name+".md")
		if err != nil {
			continue
		}
		m, ok := loadMemory(p)
		if !ok {
			continue
		}
		// Update the record before archiving.
		m.Status = "superseded"
		m.SupersededBy = replacedBy // force-set so the chain can never break
		if validTo != "" {
			m.ValidTo = validTo
		}
		// Write updated version to archive.
		archiveDir := filepath.Join(dir, ".archive")
		if mkErr := os.MkdirAll(archiveDir, 0o755); mkErr != nil {
			return mkErr
		}
		dest, aErr := archivePath(archiveDir, name, time.Now().UTC())
		if aErr != nil {
			return aErr
		}
		if wErr := os.WriteFile(dest, []byte(render(m, name)), 0o644); wErr != nil {
			return wErr
		}
		_ = os.Remove(p)
		// Remove index line.
		mu := indexLockFor(dir)
		mu.Lock()
		err = flushIndexIn(dir, indexLinesExceptIn(dir, name))
		mu.Unlock()
		if err != nil {
			return err
		}
		// Sync the bitemporal index: drop the active row, add the archive row
		// (status=superseded + ValidTo) so time-point queries reach it at once.
		s.indexRemove(p)
		s.indexUpsert(dest)
	}
	return nil
}

func archivePath(archiveDir, name string, when time.Time) (string, error) {
	stem := when.Format("20060102-150405.000") + "-" + name
	path := filepath.Join(archiveDir, stem+".md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path, nil
	} else if err != nil {
		return "", err
	}
	for i := 1; ; i++ {
		path = filepath.Join(archiveDir, fmt.Sprintf("%s-%d.md", stem, i))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
}

func renameMemoryFile(src, dest string) error {
	err := os.Rename(src, dest)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	if !os.IsPermission(err) {
		return err
	}
	// Repair permissions and retry.
	_ = os.Chmod(src, 0o600)
	_ = os.Chmod(filepath.Dir(src), 0o700)
	_ = os.Chmod(filepath.Dir(dest), 0o700)
	err = os.Rename(src, dest)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}

// archiveTimeRe matches the leading timestamp an archive file is named with
// (see archivePath: "20060102-150405.000-name.md"). Anchored at the start so a
// long or oddly-named memory slug can't shift where we look — previously the
// code took name[:20], which broke if the naming format ever gained a prefix.
var archiveTimeRe = regexp.MustCompile(`^(\d{8}-\d{6}\.\d{3})-`)

func archiveTimeFromName(name string) time.Time {
	m := archiveTimeRe.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}
	}
	t, err := time.Parse("20060102-150405.000", m[1])
	if err != nil {
		return time.Time{}
	}
	return t
}

// hasPathPrefixFold reports whether path is located under dir, comparing
// case-insensitively and respecting a path-separator boundary (so "/foo" does
// not count as a prefix of "/foobar"). Used by indexUpsert's scope detection so
// a Windows drive-letter case difference or mixed-case config path can't flip a
// global fact into project scope.
func hasPathPrefixFold(path, dir string) bool {
	if len(path) < len(dir) {
		return false
	}
	if !strings.EqualFold(path[:len(dir)], dir) {
		return false
	}
	// Exact match (path == dir) or a separator right after dir means path is
	// inside dir; anything else (e.g. dir="/a/fo", path="/a/foobar") is not.
	return len(path) == len(dir) ||
		path[len(dir)] == os.PathSeparator ||
		path[len(dir)] == '/'
}

// removeActiveMemoryInDir archives a memory from one directory if it exists
// there, and removes its index line.
func removeActiveMemoryInDir(dir, name string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	p, err := archiveInDir(dir, name)
	if err != nil {
		return err
	}
	if p != "" || indexContainsIn(dir, name) {
		mu := indexLockFor(dir)
		mu.Lock()
		defer mu.Unlock()
		return flushIndexIn(dir, indexLinesExceptIn(dir, name))
	}
	return nil
}

// reindexIn rewrites the MEMORY.md line for name in the given directory,
// preserving every other managed line.
func reindexIn(dir, name string, m Memory) error {
	mu := indexLockFor(dir)
	mu.Lock()
	defer mu.Unlock()
	lines := indexLinesExceptIn(dir, name)
	lines[name] = fmt.Sprintf("- [%s](%s.md) — %s", displayTitle(m.Title, name), name, oneLine(m.Description))
	return flushIndexIn(dir, lines)
}

// indexContainsIn reports whether the MEMORY.md in dir has an entry for name.
func indexContainsIn(dir, name string) bool {
	existing, _ := os.ReadFile(filepath.Join(dir, indexFile))
	return strings.Contains(string(existing), "("+name+".md)")
}

// indexLinesExceptIn returns the managed MEMORY.md lines keyed by filename stem
// from a specific directory, dropping the entry for name.
func indexLinesExceptIn(dir, name string) map[string]string {
	existing, _ := os.ReadFile(filepath.Join(dir, indexFile))
	keep := map[string]string{}
	for _, line := range strings.Split(string(existing), "\n") {
		if mt := indexLineRe.FindStringSubmatch(line); mt != nil && mt[1] != name {
			keep[mt[1]] = strings.TrimRight(line, "\r")
		}
	}
	return keep
}

// flushIndexIn rewrites MEMORY.md in the given directory from managed lines.
func flushIndexIn(dir string, lines map[string]string) error {
	names := make([]string, 0, len(lines))
	for n := range lines {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("# Memory\n\n")
	for _, n := range names {
		b.WriteString(lines[n])
		b.WriteString("\n")
	}
	return os.WriteFile(filepath.Join(dir, indexFile), []byte(b.String()), 0o644)
}

// render serializes a memory to frontmatter + body. The frontmatter mirrors the
// auto-memory shape (name / description / metadata.type) so the files are
// interchangeable with that ecosystem and re-readable by loadMemory.
// Bitemporal fields are written at the top level (flat keys) so the simple
// frontmatter parser can read them back without changes.
func render(m Memory, name string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + name + "\n")
	if t := oneLine(m.Title); t != "" {
		b.WriteString("title: " + t + "\n")
	}
	b.WriteString("description: " + oneLine(m.Description) + "\n")
	b.WriteString("metadata:\n")
	b.WriteString("  type: " + string(NormalizeType(string(m.Type))) + "\n")
	// Bitemporal fields.
	if !m.CreatedAt.IsZero() {
		fmt.Fprintf(&b, "created_at: %q\n", m.CreatedAt.UTC().Format(time.RFC3339))
	}
	if !m.UpdatedAt.IsZero() {
		fmt.Fprintf(&b, "updated_at: %q\n", m.UpdatedAt.UTC().Format(time.RFC3339))
	}
	if m.ValidFrom != "" {
		b.WriteString("valid_from: " + m.ValidFrom + "\n")
	}
	if m.ValidTo != "" {
		b.WriteString("valid_to: " + m.ValidTo + "\n")
	}
	if m.Status != "" {
		b.WriteString("status: " + m.Status + "\n")
	}
	if m.Supersedes != "" {
		b.WriteString("supersedes: " + m.Supersedes + "\n")
	}
	if m.SupersededBy != "" {
		b.WriteString("superseded_by: " + m.SupersededBy + "\n")
	}
	// Access tracking / lifecycle fields (Phase 7).
	if !m.LastAccessedAt.IsZero() {
		fmt.Fprintf(&b, "last_accessed_at: %q\n", m.LastAccessedAt.UTC().Format(time.RFC3339))
	}
	if m.AccessCount > 0 {
		fmt.Fprintf(&b, "access_count: %d\n", m.AccessCount)
	}
	if m.TTL != "" {
		b.WriteString("ttl: " + m.TTL + "\n")
	}
	if m.Importance != "" {
		b.WriteString("importance: " + m.Importance + "\n")
	}
	if m.Category != "" {
		b.WriteString("category: " + m.Category + "\n")
	}
	if len(m.Tags) > 0 {
		if tagsJSON, err := json.Marshal(m.Tags); err == nil {
			b.WriteString("tags: " + string(tagsJSON) + "\n")
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(m.Body))
	b.WriteString("\n")
	return b.String()
}

// indexLineRe matches a managed index line so reindexIn/Delete can target the
// line for one memory by its filename without disturbing the rest of a
// hand-edited MEMORY.md.
var indexLineRe = regexp.MustCompile(`\]\(([^)]+)\.md\)`)

// List returns the active memories parsed from their files, sorted by name.
// Used by `/memory` and the desktop memory panel. Reads from both GlobalDir and
// Dir, merging results. Files that fail to parse are skipped so one bad file
// never hides the rest. Non-active records (superseded/archived) are filtered
// out — use ListAll() or ListSuperseded() to inspect history.
func (s Store) List() []Memory {
	if s.Dir == "" && s.GlobalDir == "" {
		return nil
	}
	var out []Memory
	seen := map[string]bool{}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == indexFile || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if m, ok := loadMemory(filepath.Join(dir, e.Name())); ok {
				if m.Status != "" && m.Status != "active" {
					continue // skip superseded/archived
				}
				if !seen[m.Name] {
					out = append(out, m)
					seen[m.Name] = true
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ListAll returns all memories including non-active ones (superseded/archived).
// Used for debugging and the memory history panel.
func (s Store) ListAll() []Memory {
	if s.Dir == "" && s.GlobalDir == "" {
		return nil
	}
	var out []Memory
	seen := map[string]bool{}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == indexFile || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if m, ok := loadMemory(filepath.Join(dir, e.Name())); ok {
				if !seen[m.Name] {
					out = append(out, m)
					seen[m.Name] = true
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// fileLockFor returns a per-file mutex for access tracking writes.
func fileLockFor(path string) *sync.Mutex {
	v, _ := accessMu.LoadOrStore(path, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Get returns a single active memory by name, or (zero, false) if not found or
// not active. Checks GlobalDir first, then Dir. Also updates access tracking
// (LastAccessedAt, AccessCount) so the decay mechanism knows this fact was read.
//
// The whole read-modify-write of access tracking is held under the per-file
// lock, including the initial load: loading outside the lock let two
// concurrent Get() calls read the same stale AccessCount and each write it back
// incremented once, silently dropping a count. Doing loadMemory inside the lock
// makes the increment atomic.
func (s Store) Get(name string) (Memory, bool) {
	stem := slug(name)
	if stem == "" {
		return Memory{}, false
	}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		p, err := safeJoin(dir, stem+".md")
		if err != nil {
			continue
		}
		fl := fileLockFor(p)
		fl.Lock()
		m, ok := loadMemory(p)
		if !ok || (m.Status != "" && m.Status != "active") {
			fl.Unlock()
			continue
		}
		m.LastAccessedAt = time.Now().UTC()
		m.AccessCount++
		_ = os.WriteFile(p, []byte(render(m, stem)), 0o644)
		fl.Unlock()
		return m, true
	}
	return Memory{}, false
}

// PromoteMemory flips a "pending" auto-captured memory to "active", admitting
// it into the prompt/profile. It's the user's "confirm" action in the timeline
// panel. Only pending records can be promoted — promoting an already-active or
// superseded record is a no-op (returns false) so the UI can't accidentally
// resurrect a superseded fact. The read-modify-write is held under the per-file
// lock (same pattern as Get's access-tracking write) so concurrent promotes on
// the same name don't clobber each other.
func (s Store) PromoteMemory(name string) bool {
	return s.setStatusIf(name, "pending", "active")
}

// RejectMemory deletes a "pending" auto-captured memory the user dismissed in
// the timeline panel. Only pending records can be rejected this way — rejecting
// an active/superseded record is a no-op (returns false) so a misclick can't
// destroy a confirmed or historical fact (those go through Forget/Archive).
func (s Store) RejectMemory(name string) bool {
	stem := slug(name)
	if stem == "" {
		return false
	}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		p, err := safeJoin(dir, stem+".md")
		if err != nil {
			continue
		}
		fl := fileLockFor(p)
		fl.Lock()
		defer fl.Unlock()
		m, ok := loadMemory(p)
		if !ok || m.Status != "pending" {
			continue
		}
		if err := os.Remove(p); err != nil {
			return false
		}
		s.indexRemove(p)
		return true
	}
	return false
}

// setStatusIf atomically transitions a memory's Status from→to, but only if the
// record currently has the expected "from" status. Returns false if the record
// is missing or in a different status (so callers can treat it as idempotent).
// The lock mirrors Get's: loadMemory inside the lock makes the transition atomic.
func (s Store) setStatusIf(name, from, to string) bool {
	stem := slug(name)
	if stem == "" {
		return false
	}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		p, err := safeJoin(dir, stem+".md")
		if err != nil {
			continue
		}
		fl := fileLockFor(p)
		fl.Lock()
		defer fl.Unlock()
		m, ok := loadMemory(p)
		if !ok || m.Status != from {
			continue
		}
		m.Status = to
		m.UpdatedAt = time.Now().UTC()
		if err := os.WriteFile(p, []byte(render(m, stem)), 0o644); err != nil {
			return false
		}
		if err := reindexIn(dir, stem, m); err != nil {
			slog.Debug("reindex after status change", "name", stem, "err", err)
		}
		return true
	}
	return false
}

// ListSuperseded returns the superseded history for a given memory name, from
func (s Store) ListSuperseded(name string) []ArchivedMemory {
	stem := slug(name)
	if stem == "" {
		return nil
	}
	var out []ArchivedMemory
	for _, base := range s.dirs() {
		if base == "" {
			continue
		}
		dir := filepath.Join(base, ".archive")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			// Check if this archive file is for the requested memory.
			if !strings.Contains(e.Name(), stem) {
				continue
			}
			path := filepath.Join(dir, e.Name())
			m, ok := loadMemory(path)
			if !ok || m.Name != stem {
				continue
			}
			if m.Status != "superseded" {
				continue
			}
			when := archiveTimeFromName(e.Name())
			if when.IsZero() {
				if info, err := e.Info(); err == nil {
					when = info.ModTime()
				}
			}
			out = append(out, ArchivedMemory{Memory: m, Path: path, ArchivedAt: when})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ArchivedAt.After(out[j].ArchivedAt)
	})
	return out
}

// ListAsOf returns memories that were valid at the given point in time.
//
// It scans BOTH active memories and superseded memories still held on disk in
// .archive/. This is essential: Save()/Supersede() move a replaced record out
// of the active set, so restricting ListAsOf to List() would make historical
// queries ("where did I live in March?") silently return nothing after a fact
// is superseded — the bitemporal guarantee would be hollow.
//
// A memory with no ValidFrom/ValidTo is always included when active (timeless
// truth); a superseded memory without a ValidTo cannot be time-resolved and is
// skipped, since it was replaced rather than naturally expiring. Otherwise the
// query date must fall within [ValidFrom, ValidTo], inclusive at day
// granularity (ValidTo == query date still counts as valid on that day).
func (s Store) ListAsOf(t time.Time) []Memory {
	// Fast path: the facts index already holds active + superseded history and
	// can filter by valid_from/valid_to in SQL, so we only load the matching
	// paths from disk. The SQL comparison is day-granular and matches
	// timeFilter's semantics (valid_to == t still in range).
	if s.hasIndex() {
		dayISO := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		if rows, err := s.index.QueryAsOf(dayISO); err == nil {
			out := make([]Memory, 0, len(rows))
			for _, r := range rows {
				if m, ok := loadMemory(r.Path); ok {
					// The facts row carries the stored ValidFrom/ValidTo; loadMemory
					// re-reads them from the file, which is the truth source, so they
					// agree. No second filter needed.
					out = append(out, m)
				}
			}
			return out
		}
		// index query failed: degrade to file walk
	}
	return s.timeFilter(s.activeAndSuperseded(), t)
}

// ListAllAsOf is an alias for ListAsOf, named for callers that want to express
// the "search active + superseded history" intent explicitly. Same behavior.
func (s Store) ListAllAsOf(t time.Time) []Memory { return s.ListAsOf(t) }

// ListTimeline returns the full bitemporal surface for UI presentation: active
// + dormant records plus every superseded record still held in .archive/, plus
// any "pending" records (auto-extracted facts awaiting user confirmation).
// This is what the memory panel's timeline view renders — it lets the user see
// the version chain (when a fact changed, what it was superseded by) and review
// auto-captured facts, rather than only the confirmed current truth. It is
// read-only and never mutates state. Records are returned newest-first by
// ValidFrom so the timeline reads top-down; timeless and pending records sort
// last. Use List() for the prompt/profile view (active only) and ListAsOf(t)
// for a point-in-time query (which intentionally excludes pending).
func (s Store) ListTimeline() []Memory {
	out := s.activeAndSuperseded()
	out = append(out, s.listPending()...)
	sort.Slice(out, func(i, j int) bool {
		// Pending records sort to the very bottom so the user's confirmed
		// truth isn't buried under unreviewed auto-extracts.
		ip, jp := isPending(out[i]), isPending(out[j])
		if ip != jp {
			return jp // j pending → i (non-pending) sorts first
		}
		ti, tj := timelineSortKey(out[i]), timelineSortKey(out[j])
		if ti != tj {
			return ti > tj // newest first
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// listPending returns active-directory records whose Status is "pending" —
// facts auto-extracted by the turn-end extractor that await user confirmation.
// These are skipped by List() (active-only) and by activeAndSuperseded(), so
// this is the only entry point that surfaces them for the timeline UI.
func (s Store) listPending() []Memory {
	var out []Memory
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == indexFile || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if m, ok := loadMemory(filepath.Join(dir, e.Name())); ok && m.Status == "pending" {
				out = append(out, m)
			}
		}
	}
	return out
}

func isPending(m Memory) bool { return m.Status == "pending" }

// timelineSortKey returns the YYYYMMDD integer used to order timeline rows by
// their VALID time (when the fact became true), not by when the system wrote
// it. A record with ValidFrom sorts newest by that date; a timeless record
// (no ValidFrom) deliberately returns 0 so it sinks to the bottom — permanent
// facts (preferences, beliefs) have no place at the top of a "what changed"
// timeline. Ties break by CreatedAt, then by name (stable).
func timelineSortKey(m Memory) int {
	if m.ValidFrom != "" {
		if t, ok := parseDate(m.ValidFrom); ok {
			return t.Year()*10000 + int(t.Month())*100 + t.Day()
		}
	}
	return 0
}

// activeAndSuperseded returns active memories plus superseded ones still in
// activeAndSuperseded returns active + dormant memories plus superseded ones
// still in .archive/. This must mirror what QueryAsOf considers in-range, so
// the index path and the file-scan fallback agree: dormant records are still
// "current truth" (they decayed only for inactivity, not because the fact
// changed), so a time-point query must include them just like the index path
// does. Without this, the same ListAsOf query would return different results
// depending on whether the index happened to be attached.
func (s Store) activeAndSuperseded() []Memory {
	var out []Memory
	for _, m := range s.List() { // List() filters to active only
		out = append(out, m)
	}
	for _, m := range s.ListDormant() { // dormant: still current, just cold
		out = append(out, m)
	}
	for _, a := range s.ListArchived() {
		if a.Status != "superseded" {
			continue
		}
		out = append(out, a.Memory)
	}
	return out
}

// timeFilter keeps memories valid at t, day-granular. A timeless record (no
// ValidFrom and no ValidTo) is always kept. Otherwise t must be within
// [ValidFrom, ValidTo] inclusive, with a date treated as the whole day so that
// valid_to == t still counts as valid on that day (avoids a one-day gap when a
// successor's valid_from equals the predecessor's valid_to).
func (s Store) timeFilter(memories []Memory, t time.Time) []Memory {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	var result []Memory
	for _, m := range memories {
		from, fromOK := parseDate(m.ValidFrom)
		to, toOK := parseDate(m.ValidTo)
		if !fromOK && !toOK {
			result = append(result, m)
			continue
		}
		if fromOK && day.Before(from) {
			continue
		}
		// ValidTo is the last valid day: a query on that exact day is in range.
		if toOK && day.After(to) {
			continue
		}
		result = append(result, m)
	}
	return result
}

// parseDate parses a YYYY-MM-DD string. Returns (time.Time, true) on success,
// (zero, false) on empty or malformed input.
func parseDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

// loadMemory parses one fact file back into a Memory. It tolerates the minimal
// frontmatter render writes; a file without frontmatter still loads with its
// body and a name derived from the filename. Bitemporal fields are optional —
// missing ones default to zero values, and CreatedAt falls back to file mtime
// so pre-v0.3.0 files remain usable.
func loadMemory(path string) (Memory, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Memory{}, false
	}
	fm, body := splitFrontmatter(string(b))
	m := Memory{
		Name:        fm["name"],
		Title:       fm["title"],
		Description: fm["description"],
		Type:        NormalizeType(fm["type"]),
		Body:        strings.TrimSpace(body),
		ValidFrom:   fm["valid_from"],
		ValidTo:     fm["valid_to"],
		Status:      fm["status"],
		Supersedes:  fm["supersedes"],
		SupersededBy: fm["superseded_by"],
	}
	if m.Name == "" {
		m.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	// Parse timestamps; ignore parse errors (zero value = not set).
	if v := fm["created_at"]; v != "" {
		m.CreatedAt, _ = time.Parse(time.RFC3339, v)
	}
	if v := fm["updated_at"]; v != "" {
		m.UpdatedAt, _ = time.Parse(time.RFC3339, v)
	}
	// Backward compat: missing CreatedAt falls back to file mtime.
	if m.CreatedAt.IsZero() {
		if fi, fiErr := os.Stat(path); fiErr == nil {
			m.CreatedAt = fi.ModTime().UTC()
		}
	}
	// Backward compat: missing status defaults to "active".
	if m.Status == "" {
		m.Status = "active"
	}
	// Phase 7: access tracking / lifecycle fields.
	if v := fm["last_accessed_at"]; v != "" {
		m.LastAccessedAt, _ = time.Parse(time.RFC3339, v)
	}
	if v := fm["access_count"]; v != "" {
		fmt.Sscanf(v, "%d", &m.AccessCount)
	}
	m.TTL = fm["ttl"]
	m.Importance = fm["importance"]
	m.Category = fm["category"]
	// Parse tags from JSON array format: ["tag1", "tag2"]
	if v := fm["tags"]; v != "" {
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "[") {
			var tags []string
			if err := json.Unmarshal([]byte(v), &tags); err == nil {
				m.Tags = tags
			}
		}
	}
	return m, true
}

// splitFrontmatter is a thin wrapper; the real parser lives in
// internal/frontmatter.
func splitFrontmatter(s string) (map[string]string, string) {
	return frontmatter.Split(s)
}

// safeJoin resolves name against base and rejects anything escaping it.
// Prevents path-traversal attacks via crafted memory names like "../../etc/passwd".
func safeJoin(base, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty name")
	}
	joined := filepath.Join(base, name)
	abs := filepath.Clean(joined)
	if base == "" {
		return abs, nil
	}
	r := filepath.Clean(base)
	rel, err := filepath.Rel(r, abs)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("path %q escapes store directory", name)
	}
	return abs, nil
}

// slugRe strips everything but lowercase alphanumerics and dashes.
var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slug normalises a name into a kebab-case, filesystem-safe stem.
func slug(s string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
}

// oneLine collapses whitespace so a description can't break the single-line
// index or frontmatter format.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// displayTitle is the index link label: the given title, or a de-kebabed name
// when none was supplied, so a bare slug never leaks into the index.
func displayTitle(title, name string) string {
	if t := oneLine(title); t != "" {
		return t
	}
	return strings.ReplaceAll(name, "-", " ")
}

// --- Phase 7: Decay / TTL / Dormant ---

// indexUpsert syncs the index (FTS + facts) for a single just-written file at
// path, reading its current contents from disk. It is the write-side mirror of
// Reconcile's per-file step, called after Save/Activate/Decay mutate a file so
// the index never lags the truth source between Reconcile passes. No-op when no
// index is attached; errors are best-effort (a failed sync self-heals on the
// next search's lazy Reconcile).
func (s Store) indexUpsert(path string) {
	if !s.hasIndex() {
		return
	}
	m, ok := loadMemory(path)
	if !ok {
		return
	}
	body, err := readFileBody(path)
	if err != nil {
		return
	}
	fp := fileFingerprint(path)
	scope := "project"
	if s.GlobalDir != "" {
		// Determine scope by which top-level directory path lives under. Compare
		// case-insensitively on the cleaned paths so a Windows drive letter
		// difference (C: vs c:) or mixed-case config path doesn't mislabel a
		// global fact as project. EvalSymlinks is intentionally avoided here —
		// it's I/O on the write path, and Reconcile's full sweep is the
		// authoritative fallback for any residual mismatch.
		global := filepath.Clean(s.GlobalDir)
		p := filepath.Clean(path)
		if hasPathPrefixFold(p, global) {
			scope = "global"
		}
	}
	typ := string(TypeProject)
	if m.Type != "" {
		typ = string(m.Type)
	}
	status := "active"
	if m.Status != "" {
		status = m.Status
	}
	if err := s.index.UpsertWithTime(path, scope, typ, m.Title, m.Description, body, status, m.ValidFrom, m.ValidTo, fp); err != nil {
		// Self-heals on the next search's lazy Reconcile; log so a persistent
		// failure (e.g. db locked) is visible instead of silently dropping.
		slog.Warn("memory: index fts sync failed", "path", path, "err", err)
	}
	if err := s.index.UpsertFact(FactRow{
		Path: path, Name: m.Name, Title: m.Title, Description: m.Description,
		Type: typ, Category: m.Category, Status: status, Scope: scope,
		ValidFrom: m.ValidFrom, ValidTo: m.ValidTo,
		CreatedAt: rfc3339(m.CreatedAt), UpdatedAt: rfc3339(m.UpdatedAt),
		Supersedes: m.Supersedes, SupersededBy: m.SupersededBy,
		Importance: m.Importance, TTL: m.TTL, BodyHash: hashBody(body), Fingerprint: fp,
	}); err != nil {
		slog.Warn("memory: index facts sync failed", "path", path, "err", err)
	}
}

// indexRemove drops a path from the index (both tables). Called after a file is
// archived/moved/deleted so the active index no longer references it; the new
// archive location is picked up by the next Reconcile pass.
func (s Store) indexRemove(path string) {
	if s.hasIndex() {
		if err := s.index.Delete(path); err != nil {
			slog.Warn("memory: index remove failed", "path", path, "err", err)
		}
	}
}

// DecayConfig holds the parameters for the automatic decay mechanism.
type DecayConfig struct {
	DecayDays int // default 30: days of inactivity before dormant
	ColdDays  int // default 90: days dormant before archive suggestion
	HotLimit  int // default 50: max active facts in MEMORY.md
}

// DefaultDecayConfig returns sensible defaults.
func DefaultDecayConfig() DecayConfig {
	return DecayConfig{DecayDays: 30, ColdDays: 90, HotLimit: 50}
}

// Decay scans active memories and downgrades long-unaccessed ones to dormant.
// importance=high is exempt. importance=low decays at half the threshold.
// Dormant files are kept in-place (not moved) but removed from MEMORY.md index.
func (s Store) Decay(cfg DecayConfig) (dormantCount int, err error) {
	if cfg.DecayDays <= 0 {
		cfg.DecayDays = 30
	}
	now := time.Now().UTC()
	for _, m := range s.ListAll() {
		if m.Status != "active" {
			continue
		}
		if m.Importance == "high" {
			continue // exempt
		}
		threshold := cfg.DecayDays
		if m.Importance == "low" {
			threshold /= 2
		}
		// Use LastAccessedAt if set, otherwise CreatedAt.
		refTime := m.LastAccessedAt
		if refTime.IsZero() {
			refTime = m.CreatedAt
		}
		if refTime.IsZero() {
			continue // can't determine age
		}
		if now.Sub(refTime) > time.Duration(threshold)*24*time.Hour {
			m.Status = "dormant"
			m.UpdatedAt = now
			targetDir := s.DirFor(m.Type)
			targetPath, _ := safeJoin(targetDir, slug(m.Name)+".md")
			if targetPath != "" {
				_ = os.WriteFile(targetPath, []byte(render(m, slug(m.Name))), 0o644)
			}
			// Remove from MEMORY.md index in target dir.
			mu := indexLockFor(targetDir)
			mu.Lock()
			_ = flushIndexIn(targetDir, indexLinesExceptIn(targetDir, slug(m.Name)))
			mu.Unlock()
			// Archive stale copies in other directories (matching Save()'s
			// cross-dir hygiene). We archive rather than os.Remove so the old
			// copy is preserved in .archive/ — permanent deletion would violate
			// the "old facts are never lost" bitemporal guarantee.
			for _, other := range s.dirs() {
				if other == targetDir {
					continue
				}
				otherPath, _ := safeJoin(other, slug(m.Name)+".md")
				if otherPath == "" {
					continue
				}
				if _, statErr := os.Stat(otherPath); statErr != nil {
					continue
				}
				archivedPath, _ := archiveInDir(other, slug(m.Name)) // best-effort; primary write already succeeded
				omu := indexLockFor(other)
				omu.Lock()
				_ = flushIndexIn(other, indexLinesExceptIn(other, slug(m.Name)))
				omu.Unlock()
				// Sync index: drop the active row in the other dir, add its archive row.
				s.indexRemove(otherPath)
				if archivedPath != "" {
					s.indexUpsert(archivedPath)
				}
			}
			// The dormant record stays in place (now status=dormant); refresh its index row.
			if targetPath != "" {
				s.indexUpsert(targetPath)
			}
			dormantCount++
		}
	}
	return
}

// ExpireTTL archives memories whose TTL date has passed. The comparison is
// time-based, not string-based: a malformed TTL (not YYYY-MM-DD) is skipped
// rather than silently archived by lexicographic accident. "Passed" means the
// TTL day is strictly before today (a TTL of today still counts as valid on
// that day, mirroring ListAsOf's day-granular ValidTo handling).
func (s Store) ExpireTTL() (expiredCount int, err error) {
	today := time.Now().UTC()
	for _, m := range s.List() {
		if m.TTL == "" {
			continue
		}
		ttl, ok := parseDate(m.TTL)
		if !ok {
			continue // malformed TTL: skip rather than risk a wrong archive
		}
		// TTL day is the last valid day; expire only once it is fully in the past.
		if today.After(time.Date(ttl.Year(), ttl.Month(), ttl.Day(), 23, 59, 59, 0, time.UTC)) {
			if _, aErr := s.Archive(slug(m.Name)); aErr == nil {
				expiredCount++
			} else if err == nil {
				err = aErr // surface the first error but keep trying the rest
			}
		}
	}
	return
}

// ListDormant returns memories with status=dormant (warm layer, searchable).
func (s Store) ListDormant() []Memory {
	if s.Dir == "" && s.GlobalDir == "" {
		return nil
	}
	var out []Memory
	seen := map[string]bool{}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == indexFile || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if m, ok := loadMemory(filepath.Join(dir, e.Name())); ok {
				if m.Status == "dormant" && !seen[m.Name] {
					out = append(out, m)
					seen[m.Name] = true
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Activate changes a dormant memory back to active and re-adds it to the
// MEMORY.md index. Used by the memory_recall tool.
func (s Store) Activate(name string) error {
	stem := slug(name)
	if stem == "" {
		return fmt.Errorf("memory needs a name")
	}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		p, err := safeJoin(dir, stem+".md")
		if err != nil {
			continue
		}
		m, ok := loadMemory(p)
		if !ok || m.Status != "dormant" {
			continue
		}
		m.Status = "active"
		m.LastAccessedAt = time.Now().UTC()
		m.AccessCount++
		m.UpdatedAt = time.Now().UTC()
		if err := os.WriteFile(p, []byte(render(m, stem)), 0o644); err != nil {
			return err
		}
		// Re-add to MEMORY.md index.
		if err := reindexIn(dir, stem, m); err != nil {
			return err
		}
		// Reflect the reactivation in the bitemporal index.
		s.indexUpsert(p)
		return nil
	}
	return fmt.Errorf("dormant memory %q not found", name)
}

// ListByCategory returns active memories filtered by category (identity/style/
// belief/temporal/feedback). If category is "", returns all active memories.
func (s Store) ListByCategory(category string) []Memory {
	var out []Memory
	for _, m := range s.List() {
		if category == "" || m.Category == category {
			out = append(out, m)
		}
	}
	return out
}

// profileCategories is the ordered set of profile buckets, mirroring
// memory_profile's grouping so the injected block and the tool output agree.
var profileCategories = []struct{ key, name string }{
	{"identity", "Identity"},
	{"style", "Style"},
	{"belief", "Technical Beliefs"},
	{"temporal", "Temporal (time-sensitive)"},
	{"feedback", "Feedback to Agent"},
	{"", "Other"},
}

// ProfileBlock renders the active TypeUser memories as a structured profile,
// grouped by category with validity dates, as Markdown. Returns "" when there
// are no user facts, so Block() can omit the section entirely. This is the
// "always present" user profile: it folds into the system-prompt prefix at boot
// so the model knows who the user is without calling a tool.
func (s Store) ProfileBlock() string {
	var userFacts []Memory
	for _, m := range s.List() {
		if m.Type == TypeUser {
			userFacts = append(userFacts, m)
		}
	}
	if len(userFacts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## User Profile\n\n")
	b.WriteString("Who the user is, from saved memories. Current as of the saved-memories index above; use `memory_profile` for the full view.\n\n")
	for _, cat := range profileCategories {
		var items []Memory
		for _, m := range userFacts {
			if cat.key == "" {
				// "Other": user facts with no/unknown category.
				known := false
				for _, c := range profileCategories[:5] {
					if m.Category == c.key {
						known = true
						break
					}
				}
				if !known {
					items = append(items, m)
				}
			} else if m.Category == cat.key {
				items = append(items, m)
			}
		}
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n", cat.name)
		for _, m := range items {
			validity := ""
			if m.ValidFrom != "" {
				validity = fmt.Sprintf(" (since %s)", m.ValidFrom)
			}
			fmt.Fprintf(&b, "- %s%s\n", oneLine(m.Description), validity)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// ListActiveByType returns active memories of a given type. Used by the
// conflict detector, which must scan every same-type record to catch
// different-name contradictions (e.g. "住北京" name=address vs "住上海"
// name=location) — the same-name-only check in remember missed these, leaving
// contradictory facts both active. An empty type returns all active memories.
//
// When the bitemporal index is attached, the type filter runs in SQL (cheap)
// and only the matching paths are loaded from disk; otherwise it falls back to
// a full List() walk. Uses loadMemory (pure read) rather than Get() so the
// conflict scan has no access-tracking side effects.
func (s Store) ListActiveByType(t Type) []Memory {
	if t != "" && s.hasIndex() {
		if rows, err := s.index.QueryActiveByType(string(t)); err == nil {
			// Index is authoritative: SQL already filtered to active + type, so
			// load each hit from the truth-source file. Empty rows is a real
			// "no matches", not a fallback trigger.
			out := make([]Memory, 0, len(rows))
			for _, r := range rows {
				if m, ok := loadMemory(r.Path); ok {
					out = append(out, m)
				}
			}
			return out
		}
		// index query failed: degrade to file walk
	}
	var out []Memory
	for _, m := range s.List() {
		if t == "" || m.Type == t {
			out = append(out, m)
		}
	}
	return out
}
