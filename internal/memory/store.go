package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zzycxz/momapeer/internal/frontmatter"
)

// Store is the per-project auto-memory: a directory of one-fact-per-file
// Markdown notes with frontmatter, plus a MEMORY.md index of one line per fact.
// The model maintains it through the `remember` tool; the index loads into the
// cached system-prompt prefix at boot so the model always knows what it has
// saved, and reads individual facts on demand with read_file. The whole thing is
// plain files the user can edit by hand.
type Store struct {
	Dir       string // ...momapeer/projects/<slug>/memory (project-specific)
	GlobalDir string // ...momapeer/memory/global (shared across projects)
}

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

// Memory is one stored fact.
type Memory struct {
	Name        string // kebab-case slug; also the file stem (<name>.md)
	Title       string // human-readable index label; falls back to a de-kebabed Name
	Description string // one-line summary used for the index and recall
	Type        Type
	Body        string // the fact itself (Markdown)
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
// When both GlobalDir and Dir have indexes, they are merged with deduplication
// (global first).
func (s Store) Index() string {
	managed := map[string]string{}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, indexFile))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if mt := indexLineRe.FindStringSubmatch(line); mt != nil {
				if _, exists := managed[mt[1]]; !exists {
					managed[mt[1]] = strings.TrimRight(line, "\r")
				}
			}
		}
	}
	if len(managed) == 0 {
		return ""
	}
	names := make([]string, 0, len(managed))
	for n := range managed {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(managed[n])
		b.WriteString("\n")
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
			_ = removeActiveMemoryInDir(other, name) // best-effort; the primary save succeeded
		}
	}
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
		p, err := archiveInDir(dir, name)
		if err != nil {
			return "", err
		}
		if p != "" || indexContainsIn(dir, name) {
			if err := flushIndexIn(dir, indexLinesExceptIn(dir, name)); err != nil {
				return "", err
			}
		}
		if p != "" {
			lastPath = p
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
	if err := renameMemoryFile(src, dest); err != nil {
		return "", err
	}
	return dest, nil
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

func archiveTimeFromName(name string) time.Time {
	// Format: "20060102-150405.000-<name>.md" or "20060102-150405.000-<name>-1.md"
	if len(name) < 20 {
		return time.Time{}
	}
	t, err := time.Parse("20060102-150405.000", name[:20])
	if err != nil {
		return time.Time{}
	}
	return t
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
		return flushIndexIn(dir, indexLinesExceptIn(dir, name))
	}
	return nil
}

// reindexIn rewrites the MEMORY.md line for name in the given directory,
// preserving every other managed line.
func reindexIn(dir, name string, m Memory) error {
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
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(m.Body))
	b.WriteString("\n")
	return b.String()
}

// indexLineRe matches a managed index line so reindexIn/Delete can target the
// line for one memory by its filename without disturbing the rest of a
// hand-edited MEMORY.md.
var indexLineRe = regexp.MustCompile(`\]\(([^)]+)\.md\)`)

// List returns the saved memories parsed from their files, sorted by name. Used
// by `/memory` and the desktop memory panel. Reads from both GlobalDir and Dir,
// merging results. Files that fail to parse are skipped so one bad file never
// hides the rest.
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

// loadMemory parses one fact file back into a Memory. It tolerates the minimal
// frontmatter render writes; a file without frontmatter still loads with its
// body and a name derived from the filename.
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
	}
	if m.Name == "" {
		m.Name = strings.TrimSuffix(filepath.Base(path), ".md")
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
