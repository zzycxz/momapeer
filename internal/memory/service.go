package memory

import (
	"fmt"
	"path/filepath"
)

const (
	// DefaultFloorRatio is the default relative score floor for BM25 results.
	// Results scoring below this fraction of the top hit are dropped. Relative
	// (not absolute) because BM25 magnitudes are corpus-size-dependent.
	DefaultFloorRatio = 0.15

	// DefaultSearchLimit is the default number of search results.
	DefaultSearchLimit = 10
)

// SearchService provides on-demand FTS5-backed memory search with BM25 ranking.
type SearchService struct {
	fts       *FTSStore
	store     Store
	floorRatio float64
	limit      int
}

// NewSearchService creates a search service for the given memory store.
// It opens (or creates) an FTS5 database in the store's project directory.
func NewSearchService(store Store) (*SearchService, error) {
	dir := store.Dir
	if dir == "" {
		return nil, fmt.Errorf("memory store unavailable")
	}
	// Reuse the project memory dir for the FTS database.
	dbDir := filepath.Join(dir, ".fts")
	fts, err := OpenFTSStore(dbDir)
	if err != nil {
		return nil, err
	}
	return &SearchService{
		fts:        fts,
		store:      store,
		floorRatio: DefaultFloorRatio,
		limit:      DefaultSearchLimit,
	}, nil
}

// Close releases the FTS database.
func (s *SearchService) Close() error {
	if s == nil || s.fts == nil {
		return nil
	}
	return s.fts.Close()
}

// SearchResult is one memory search result.
type SearchResult struct {
	Name    string // memory name (slug)
	Path    string // absolute file path
	Scope   string // "global" or "project"
	Type    string // memory type
	Snippet string // highlighted snippet
	Score   float64
}

// Search runs an FTS5 query against the memory index. It reconciles with disk
// first (lazy reconciliation) so off-tool writes are picked up automatically.
func (s *SearchService) Search(query string) ([]SearchResult, error) {
	if s == nil || s.fts == nil {
		return nil, nil
	}

	// Lazy reconciliation: sync index with disk before searching.
	if err := s.fts.Reconcile(s.store); err != nil {
		// Non-fatal: search with stale index.
		_ = err
	}

	results, err := s.fts.Search(query, s.limit, s.floorRatio)
	if err != nil {
		return nil, err
	}

	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		out = append(out, SearchResult{
			Name:    memoryNameFromPath(r.Path),
			Path:    r.Path,
			Scope:   r.Scope,
			Type:    r.Type,
			Snippet: r.Snippet,
			Score:   r.Score,
		})
	}
	return out, nil
}

// IndexCount returns the number of indexed memory entries.
func (s *SearchService) IndexCount() int {
	if s == nil || s.fts == nil {
		return 0
	}
	return s.fts.Count()
}

// Rebuild drops and re-builds the entire FTS index from disk.
func (s *SearchService) Rebuild() error {
	if s == nil || s.fts == nil {
		return nil
	}
	// Drop and recreate.
	if _, err := s.fts.db.Exec("DROP TABLE IF EXISTS memory_fts"); err != nil {
		return err
	}
	if _, err := s.fts.db.Exec(ftsSchema); err != nil {
		return err
	}
	return s.fts.Reconcile(s.store)
}

// memoryNameFromPath extracts the memory slug from its file path.
func memoryNameFromPath(path string) string {
	base := filepath.Base(path)
	return base[:len(base)-len(filepath.Ext(base))]
}

// Ensure SearchService satisfies a no-op when FTS is unavailable.
var _ interface{ Close() error } = (*SearchService)(nil)
