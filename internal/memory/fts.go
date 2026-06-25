package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

const ftsSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
	title,
	description,
	body,
	path UNINDEXED,
	scope UNINDEXED,
	type UNINDEXED,
	status UNINDEXED,
	valid_from UNINDEXED,
	valid_to UNINDEXED,
	fingerprint UNINDEXED,
	last_indexed_at UNINDEXED,
	tokenize='unicode61 remove_diacritics 2'
);
`

// factsSchema is the bitemporal fact index — a normal (non-FTS) table that
// mirrors the time-bearing fields of every memory file on disk, INCLUDING
// superseded/archived history under .archive/. FTS answers "find text matching
// X"; facts answers "what was true at time T" or "what active facts are of
// type U" — both via cheap SQL instead of scanning every .md file.
//
// path is the primary key: one row per on-disk file version (an active record
// and each of its superseded archive copies are distinct rows, distinguished by
// path). status lets queries exclude archived rows by default while keeping
// them available for time-point lookups.
const factsSchema = `
CREATE TABLE IF NOT EXISTS facts (
	path TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	title TEXT,
	description TEXT,
	type TEXT,
	category TEXT,
	status TEXT,
	scope TEXT,
	valid_from TEXT,
	valid_to TEXT,
	created_at TEXT,
	updated_at TEXT,
	supersedes TEXT,
	superseded_by TEXT,
	importance TEXT,
	ttl TEXT,
	body_hash TEXT,
	fingerprint TEXT,
	last_indexed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_facts_status ON facts(status);
CREATE INDEX IF NOT EXISTS idx_facts_name ON facts(name);
CREATE INDEX IF NOT EXISTS idx_facts_type_status ON facts(type, status);
`

const schemaVersionSchema = `
CREATE TABLE IF NOT EXISTS schema_version (version INTEGER);
INSERT OR IGNORE INTO schema_version VALUES (1);
`

// currentSchemaVersion is bumped whenever the FTS or facts schema changes.
// EnsureSchema rebuilds the index when an on-disk db is older than this. v3
// adds the facts bitemporal table; v4 makes title/description searchable in FTS
// (previously only body was indexed, so title-keyword searches returned nothing).
const currentSchemaVersion = 4

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
	// WAL + busy_timeout + single connection: the FTS index is a single-writer
	// store, so allowing the sql.DB pool to open multiple connections only
	// multiplies SQLITE_BUSY contention (two writers both grabbing the SQLite
	// writer lock). Capping at one connection serializes writes through one
	// lock holder, and _busy_timeout makes a contended writer wait up to 5s
	// instead of failing immediately. Without these, desktop multi-tab memory
	// writes intermittently error with "database is locked".
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open fts db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(ftsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create fts schema: %w", err)
	}
	// Bitemporal fact index (v3). Kept in the same db so the two stay consistent
	// under one writer lock and one Reconcile pass.
	if _, err := db.Exec(factsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create facts schema: %w", err)
	}
	// Schema versioning for migrations.
	if _, err := db.Exec(schemaVersionSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema_version: %w", err)
	}
	return &FTSStore{db: db, dir: dir}, nil
}

// SchemaVersion returns the current schema version, or 0 if the table doesn't exist.
func (s *FTSStore) SchemaVersion() int {
	if s == nil || s.db == nil {
		return 0
	}
	var v int
	if err := s.db.QueryRow("SELECT version FROM schema_version").Scan(&v); err != nil {
		return 0
	}
	return v
}

// SetSchemaVersion updates the schema version number.
func (s *FTSStore) SetSchemaVersion(v int) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec("UPDATE schema_version SET version = ?", v)
	return err
}

func (s *FTSStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Upsert inserts or updates a memory file in the FTS index. FTS5 virtual tables
// do not support ON CONFLICT, so we delete-then-insert. title/desc default to
// "" (callers that have them should use UpsertWithTime directly).
func (s *FTSStore) Upsert(path, scope, typ, body, fingerprint string) error {
	return s.UpsertWithTime(path, scope, typ, "", "", body, "active", "", "", fingerprint)
}

// UpsertWithTime inserts or updates a memory file with bitemporal metadata.
// title and description are now indexed (searchable) alongside body, so a query
// for a title keyword finally matches — previously only body was searchable.
func (s *FTSStore) UpsertWithTime(path, scope, typ, title, desc, body, status, validFrom, validTo, fingerprint string) error {
	if _, err := s.db.Exec("DELETE FROM memory_fts WHERE path = ?", path); err != nil {
		return err
	}
	_, err := s.db.Exec(`
		INSERT INTO memory_fts (title, description, body, path, scope, type, status, valid_from, valid_to, fingerprint, last_indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`, title, desc, body, path, scope, typ, status, validFrom, validTo, fingerprint)
	return err
}

// FactRow is the projection of a Memory into the facts index. Timestamps are
// stored as RFC3339 strings (or "" when zero) so SQL lexicographic ordering on
// valid_from/valid_to works for date-prefix queries.
type FactRow struct {
	Path          string
	Name          string
	Title         string
	Description   string
	Type          string
	Category      string
	Status        string
	Scope         string
	ValidFrom     string
	ValidTo       string
	CreatedAt     string
	UpdatedAt     string
	Supersedes    string
	SupersededBy  string
	Importance    string
	TTL           string
	BodyHash      string
	Fingerprint   string
	LastIndexedAt string
}

// UpsertFact inserts or replaces a full bitemporal row in the facts table. It
// is called by Reconcile for every on-disk file (active AND archived), so the
// facts index holds the complete history needed for time-point queries and
// same-type conflict scans without touching the filesystem.
func (s *FTSStore) UpsertFact(f FactRow) error {
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO facts
		(path, name, title, description, type, category, status, scope,
		 valid_from, valid_to, created_at, updated_at, supersedes,
		 superseded_by, importance, ttl, body_hash, fingerprint, last_indexed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,datetime('now'))
	`,
		f.Path, f.Name, f.Title, f.Description, f.Type, f.Category, f.Status, f.Scope,
		f.ValidFrom, f.ValidTo, f.CreatedAt, f.UpdatedAt, f.Supersedes,
		f.SupersededBy, f.Importance, f.TTL, f.BodyHash, f.Fingerprint,
	)
	return err
}

// Delete removes a path from BOTH the FTS and facts indexes.
func (s *FTSStore) Delete(path string) error {
	if _, err := s.db.Exec("DELETE FROM memory_fts WHERE path = ?", path); err != nil {
		return err
	}
	_, err := s.db.Exec("DELETE FROM facts WHERE path = ?", path)
	return err
}

// FactIsCurrent reports whether the facts row for path matches fingerprint, so
// Reconcile can skip unchanged files for the facts index too.
func (s *FTSStore) FactIsCurrent(path, fingerprint string) bool {
	var got string
	err := s.db.QueryRow("SELECT fingerprint FROM facts WHERE path = ?", path).Scan(&got)
	if err != nil {
		return false
	}
	return got == fingerprint
}

// factColumns lists facts columns in FactRow field order, shared by SELECT and
// Scan so the two stay in sync.
const factColumns = `path, name, title, description, type, category, status, scope,
	valid_from, valid_to, created_at, updated_at, supersedes,
	superseded_by, importance, ttl, body_hash, fingerprint, last_indexed_at`

// QueryActiveByType returns fact rows with the given type and an active-ish
// status. Powers the conflict-detector's same-type scan without a directory walk.
func (s *FTSStore) QueryActiveByType(typ string) ([]FactRow, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	return s.queryFacts(`SELECT `+factColumns+` FROM facts
		WHERE type = ? AND (status = 'active' OR status = '' OR status IS NULL)`, typ)
}

// QueryAsOf returns fact rows valid at the given day (YYYY-MM-DD), spanning
// active AND superseded (and dormant) rows so historical records still resolve
// — this is the whole point of bitemporal queries. Only 'archived' (forgotten /
// TTL-expired) rows are excluded. A row with neither valid_from nor valid_to is
// included (timeless), since an active timeless fact is valid at any date.
func (s *FTSStore) QueryAsOf(dayISO string) ([]FactRow, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	q := `SELECT ` + factColumns + ` FROM facts WHERE
		(status IS NULL OR status = '' OR status = 'active' OR status = 'superseded' OR status = 'dormant')
		AND (valid_from = '' OR valid_from IS NULL OR valid_from <= ?)
		AND (valid_to = '' OR valid_to IS NULL OR valid_to >= ?)`
	return s.queryFacts(q, dayISO, dayISO)
}

func (s *FTSStore) queryFacts(query string, args ...any) ([]FactRow, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FactRow
	for rows.Next() {
		var f FactRow
		if err := rows.Scan(&f.Path, &f.Name, &f.Title, &f.Description, &f.Type,
			&f.Category, &f.Status, &f.Scope, &f.ValidFrom, &f.ValidTo,
			&f.CreatedAt, &f.UpdatedAt, &f.Supersedes, &f.SupersededBy,
			&f.Importance, &f.TTL, &f.BodyHash, &f.Fingerprint, &f.LastIndexedAt); err != nil {
			continue
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FTSResult is one search result.
type FTSResult struct {
	Path   string
	Scope  string
	Type   string
	Status string // active / dormant / superseded / archived
	Snippet string
	Score  float64
}

// Search runs an FTS5 MATCH query with BM25 ranking, filtered to active
// memories only. Results scoring below floorRatio of the top hit are filtered
// out. Returns up to limit results.
func (s *FTSStore) Search(query string, limit int, floorRatio float64) ([]FTSResult, error) {
	return s.searchInternal(query, limit, floorRatio, "")
}

// SearchAsOf runs an FTS5 MATCH query filtered to memories valid at the given
// date. A memory is included if: status=active AND (valid_from <= t OR valid_from
// is empty) AND (valid_to >= t OR valid_to is empty). Pass zero time to get all
// active memories (same as Search).
func (s *FTSStore) SearchAsOf(query string, limit int, floorRatio float64, asOf time.Time) ([]FTSResult, error) {
	return s.searchInternal(query, limit, floorRatio, asOf.Format("2006-01-02"))
}

// searchInternal is the shared implementation for Search and SearchAsOf.
// asOfDate is "" for current-only (active), or "YYYY-MM-DD" for time-point queries.
func (s *FTSStore) searchInternal(query string, limit int, floorRatio float64, asOfDate string) ([]FTSResult, error) {
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

	// Build WHERE clause for temporal filtering. asOfDate is bound as a
	// parameter (never string-interpolated) to keep the query injection-safe
	// even if a future caller passes untrusted input — the rest of this file
	// already uses ? placeholders, this was the lone holdout.
	where := "memory_fts MATCH ? AND (status = 'active' OR status = '' OR status IS NULL)"
	args := []any{ftsQuery}
	if asOfDate != "" {
		// Time-point query: also filter by valid_from/valid_to.
		where += " AND (valid_from = '' OR valid_from IS NULL OR valid_from <= ?)" +
			" AND (valid_to = '' OR valid_to IS NULL OR valid_to >= ?)"
		args = append(args, asOfDate, asOfDate)
	}
	args = append(args, fetchLimit)

	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT path, scope, type, status,
			snippet(memory_fts, 2, '<<', '>>', '...', 32) AS snip,
			bm25(memory_fts) AS score
		FROM memory_fts
		WHERE %s
		ORDER BY score
		LIMIT ?
	`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer rows.Close()

	var results []FTSResult
	for rows.Next() {
		var r FTSResult
		if err := rows.Scan(&r.Path, &r.Scope, &r.Type, &r.Status, &r.Snippet, &r.Score); err != nil {
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
		// FTS5 phrase queries are double-quoted; a literal " inside a token
		// would terminate the phrase early and produce malformed MATCH syntax.
		// Escape it as "" (the FTS5 phrase-escape rule). tokenize strips most
		// punctuation so this is defensive, but a token can still contain "
		// via isTokenChar if that set is ever widened.
		escaped := strings.ReplaceAll(t, `"`, `""`)
		parts = append(parts, `"`+escaped+`"`)
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
