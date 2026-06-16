package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Reconcile walks the memory directories and updates the FTS index to match
// the files on disk. Changed files are re-indexed; deleted files are pruned
// from the index. This is a lazy operation that runs before each search.
func (s *FTSStore) Reconcile(store Store) error {
	if s == nil || s.db == nil {
		return nil
	}

	// Collect all memory files from all directories.
	type fileEntry struct {
		path  string // absolute path on disk
		scope string // "global" or "project"
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
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == indexFile || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			files = append(files, fileEntry{path: filepath.Join(dir, e.Name()), scope: scope})
		}
	}

	// Build a set of current disk paths.
	diskPaths := map[string]bool{}
	for _, f := range files {
		diskPaths[f.path] = true
	}

	// Prune index entries whose files no longer exist on disk.
	indexedPaths, err := s.Paths()
	if err != nil {
		return fmt.Errorf("list indexed paths: %w", err)
	}
	for _, p := range indexedPaths {
		if !diskPaths[p] {
			if err := s.Delete(p); err != nil {
				return fmt.Errorf("prune %s: %w", p, err)
			}
		}
	}

	// Index or re-index changed files.
	for _, f := range files {
		fp := fileFingerprint(f.path)
		if s.isCurrent(f.path, fp) {
			continue // unchanged
		}
		body, err := readFileBody(f.path)
		if err != nil {
			continue // skip unreadable
		}
		m, ok := loadMemory(f.path)
		typ := string(TypeProject)
		if ok {
			typ = string(m.Type)
		}
		if err := s.Upsert(f.path, f.scope, typ, body, fp); err != nil {
			return fmt.Errorf("index %s: %w", f.path, err)
		}
	}
	return nil
}

// isCurrent checks if the given path is already indexed with the expected fingerprint.
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
