package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	_ "modernc.org/sqlite"
)

const ftsSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
	path UNINDEXED,
	scope UNINDEXED,
	type UNINDEXED,
	body,
	fingerprint UNINDEXED,
	last_indexed_at UNINDEXED,
	tokenize='unicode61 remove_diacritics 2'
);
`

// FTSStore manages an FTS5-indexed memory database.
type FTSStore struct {
	db  *sql.DB
	dir string // project memory directory for reconciliation
}

// OpenFTSStore opens (or creates) an FTS5 database in the given directory.
func OpenFTSStore(dir string) (*FTSStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("empty directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dir, "memory_fts.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open fts db: %w", err)
	}
	if _, err := db.Exec(ftsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create fts schema: %w", err)
	}
	return &FTSStore{db: db, dir: dir}, nil
}

func (s *FTSStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Upsert inserts or updates a memory file in the FTS index. FTS5 virtual tables
// do not support ON CONFLICT, so we delete-then-insert.
func (s *FTSStore) Upsert(path, scope, typ, body, fingerprint string) error {
	if _, err := s.db.Exec("DELETE FROM memory_fts WHERE path = ?", path); err != nil {
		return err
	}
	_, err := s.db.Exec(`
		INSERT INTO memory_fts (path, scope, type, body, fingerprint, last_indexed_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'))
	`, path, scope, typ, body, fingerprint)
	return err
}

// Delete removes a path from the FTS index.
func (s *FTSStore) Delete(path string) error {
	_, err := s.db.Exec("DELETE FROM memory_fts WHERE path = ?", path)
	return err
}

// FTSResult is one search result.
type FTSResult struct {
	Path    string
	Scope   string
	Type    string
	Snippet string
	Score   float64
}

// Search runs an FTS5 MATCH query with BM25 ranking. Results scoring below
// floorRatio of the top hit are filtered out. Returns up to limit results.
func (s *FTSStore) Search(query string, limit int, floorRatio float64) ([]FTSResult, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if query = strings.TrimSpace(query); query == "" {
		return nil, nil
	}
	ftsQuery := buildFtsQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}
	// Over-fetch to allow floor filtering.
	fetchLimit := limit * 3
	if fetchLimit > 50 {
		fetchLimit = 50
	}

	rows, err := s.db.Query(`
		SELECT path, scope, type,
			snippet(memory_fts, 3, '<<', '>>', '...', 32) AS snip,
			bm25(memory_fts) AS score
		FROM memory_fts
		WHERE memory_fts MATCH ?
		ORDER BY score
		LIMIT ?
	`, ftsQuery, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer rows.Close()

	var results []FTSResult
	for rows.Next() {
		var r FTSResult
		if err := rows.Scan(&r.Path, &r.Scope, &r.Type, &r.Snippet, &r.Score); err != nil {
			continue
		}
		// BM25 returns lower = better; negate so higher = better.
		r.Score = -r.Score
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Apply relative score floor: drop results below floorRatio of top score.
	if len(results) > 1 && floorRatio > 0 {
		topScore := results[0].Score
		cutoff := topScore * floorRatio
		kept := results[:1] // Always keep the top hit.
		for _, r := range results[1:] {
			if r.Score >= cutoff {
				kept = append(kept, r)
			}
		}
		results = kept
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// Count returns the number of indexed entries.
func (s *FTSStore) Count() int {
	if s == nil || s.db == nil {
		return 0
	}
	var n int
	_ = s.db.QueryRow("SELECT count(*) FROM memory_fts").Scan(&n)
	return n
}

// Paths returns all indexed paths.
func (s *FTSStore) Paths() ([]string, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query("SELECT path FROM memory_fts")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			paths = append(paths, p)
		}
	}
	return paths, rows.Err()
}

// buildFtsQuery tokenizes user input into phrase-quoted FTS5 literals joined
// with OR. OR is used instead of AND because AND killed recall on multi-word
// queries where one descriptive word was absent from the corpus.
func buildFtsQuery(input string) string {
	tokens := tokenize(input)
	if len(tokens) == 0 {
		return ""
	}
	var parts []string
	for _, t := range tokens {
		parts = append(parts, `"`+t+`"`)
	}
	return strings.Join(parts, " OR ")
}

// tokenize splits input into Unicode-aware tokens. CJK ideographs, kana, and
// hangul are split into individual characters to match FTS5's unicode61
// tokenizer behavior (each CJK character is a separate token).
func tokenize(input string) []string {
	var tokens []string
	var buf []rune
	for _, r := range input {
		if isCJK(r) {
			// Flush any accumulated Latin/digit buffer.
			if len(buf) > 0 {
				tokens = append(tokens, strings.ToLower(string(buf)))
				buf = buf[:0]
			}
			// Each CJK character is its own token (matches unicode61 indexing).
			tokens = append(tokens, strings.ToLower(string(r)))
		} else if isTokenChar(r) {
			buf = append(buf, r)
		} else {
			if len(buf) > 0 {
				tokens = append(tokens, strings.ToLower(string(buf)))
				buf = buf[:0]
			}
		}
	}
	if len(buf) > 0 {
		tokens = append(tokens, strings.ToLower(string(buf)))
	}
	return tokens
}

// isTokenChar matches Latin letters, digits, and underscores.
func isTokenChar(r rune) bool {
	return unicode.IsLetter(r) && !isCJK(r) || unicode.IsDigit(r) || r == '_'
}

// isCJK reports whether r is a CJK ideograph, kana, or hangul — characters
// that FTS5's unicode61 tokenizer treats as individual single-character tokens.
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) ||
		r >= 0x3400 && r <= 0x4DBF || // CJK Extension A
		r >= 0x20000 && r <= 0x2A6DF || // CJK Extension B
		r >= 0xF900 && r <= 0xFAFF // CJK Compatibility Ideographs
}
