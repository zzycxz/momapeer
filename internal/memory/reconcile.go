package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Reconcile walks the memory directories AND their .archive/ subdirs, updating
// both the FTS and facts indexes to match the files on disk. Changed files are
// re-indexed; deleted files are pruned from both indexes.
//
// Including .archive/ is what makes superseded history searchable: a time-point
// query ("where did I live in March?") must reach the archived Beijing record
// after Shanghai superseded it, and the facts index is the cheap path for that.
func (s *FTSStore) Reconcile(store Store) error {
	if s == nil || s.db == nil {
		return nil
	}

	// Collect all memory files from all directories, including .archive/.
	type fileEntry struct {
		path    string // absolute path on disk
		scope   string // "global" or "project"
		archived bool  // true if under .archive/
	}
	var files []fileEntry
	for _, dir := range store.dirs() {
		if dir == "" {
			continue
		}
		scope := "project"
		if dir == store.GlobalDir {
			scope = "global"
		}
		// Active layer.
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() || e.Name() == indexFile || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				files = append(files, fileEntry{path: filepath.Join(dir, e.Name()), scope: scope})
			}
		}
		// Archive layer (superseded + archived history).
		archiveDir := filepath.Join(dir, ".archive")
		aEntries, err := os.ReadDir(archiveDir)
		if err == nil {
			for _, e := range aEntries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				files = append(files, fileEntry{
					path:     filepath.Join(archiveDir, e.Name()),
					scope:    scope,
					archived: true,
				})
			}
		}
	}

	// Build a set of current disk paths (for pruning both indexes).
	diskPaths := map[string]bool{}
	for _, f := range files {
		diskPaths[f.path] = true
	}

	// Prune index entries whose files no longer exist on disk. Paths() covers the
	// FTS table; factPaths() covers facts. Delete() clears both in one call.
	pruned := 0
	for _, p := range append(s.mustPaths(), s.factPaths()...) {
		if !diskPaths[p] {
			if err := s.Delete(p); err != nil {
				return fmt.Errorf("prune %s: %w", p, err)
			}
			pruned++
		}
	}

	// Index or re-index changed files into BOTH tables.
	reindexed := 0
	for _, f := range files {
		fp := fileFingerprint(f.path)
		// Only re-index if BOTH tables agree on the fingerprint; if either is
		// stale or missing, refresh both to keep them consistent.
		if s.isCurrent(f.path, fp) && s.FactIsCurrent(f.path, fp) {
			continue
		}
		m, ok := loadMemory(f.path)
		if !ok {
			continue // skip unreadable
		}
		body, err := readFileBody(f.path)
		if err != nil {
			continue
		}
		typ := string(TypeProject)
		status := "active"
		if m.Type != "" {
			typ = string(m.Type)
		}
		if m.Status != "" {
			status = m.Status
		}
		// FTS (text search) — archived bodies are indexed too so historical
		// keyword search works.
		if err := s.UpsertWithTime(f.path, f.scope, typ, m.Title, m.Description, body, status, m.ValidFrom, m.ValidTo, fp); err != nil {
			return fmt.Errorf("fts index %s: %w", f.path, err)
		}
		// facts (structured/time index) — the full bitemporal row.
		if err := s.UpsertFact(FactRow{
			Path:         f.path,
			Name:         m.Name,
			Title:        m.Title,
			Description:  m.Description,
			Type:         typ,
			Category:     m.Category,
			Status:       status,
			Scope:        f.scope,
			ValidFrom:    m.ValidFrom,
			ValidTo:      m.ValidTo,
			CreatedAt:    rfc3339(m.CreatedAt),
			UpdatedAt:    rfc3339(m.UpdatedAt),
			Supersedes:   m.Supersedes,
			SupersededBy: m.SupersededBy,
			Importance:   m.Importance,
			TTL:          m.TTL,
			BodyHash:     hashBody(body),
			Fingerprint:  fp,
		}); err != nil {
			return fmt.Errorf("facts index %s: %w", f.path, err)
		}
		reindexed++
	}
	// Only log when Reconcile actually fixed something — a clean reconcile on
	// every search would otherwise flood the log. pruned/reindexed are the key
	// signals for diagnosing drift (crash recovery, off-tool edits, db issues).
	if pruned > 0 || reindexed > 0 {
		slog.Info("memory: index reconciled", "pruned", pruned, "reindexed", reindexed)
	}
	return nil
}

// mustPaths returns the FTS-indexed paths, swallowing the error (a failed read
// just yields an empty set, which is safe — worst case we skip pruning).
func (s *FTSStore) mustPaths() []string {
	p, err := s.Paths()
	if err != nil {
		return nil
	}
	return p
}

// factPaths returns all paths present in the facts table.
func (s *FTSStore) factPaths() []string {
	if s == nil || s.db == nil {
		return nil
	}
	rows, err := s.db.Query("SELECT path FROM facts")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}

// isCurrent checks if the given path is already FTS-indexed with fingerprint.
func (s *FTSStore) isCurrent(path, fingerprint string) bool {
	var got string
	err := s.db.QueryRow("SELECT fingerprint FROM memory_fts WHERE path = ?", path).Scan(&got)
	if err != nil {
		return false
	}
	return got == fingerprint
}

// fileFingerprint returns a size+mtime fingerprint for change detection.
func fileFingerprint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d-%d", info.Size(), info.ModTime().UnixMilli())
}

// readFileBody reads a file and extracts the body (after frontmatter).
func readFileBody(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	_, body := splitFrontmatter(string(b))
	return strings.TrimSpace(body), nil
}

// rfc3339 formats a time for index storage, or "" when zero.
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// hashBody returns a short hex hash of body, for future near-duplicate detection.
func hashBody(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])[:16]
}
