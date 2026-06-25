// Package rag implements a knowledge-base store for the coWork profile: import
// documents, chunk them, and search them. Phase 3 ships full-text search via
// SQLite FTS5 (reusing the same proven CJK-aware tokenizer as the memory
// subsystem) — no external embedding API or vector store required, so it works
// offline with zero new deps. An embedding/vector layer can be added behind the
// same rag_search interface later without changing the tool surface.
//
// The store lives in the user config dir (persists across restarts), one DB per
// collection so collections are independent and deletable by file.
package rag

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	_ "modernc.org/sqlite"
)

// Store is a FTS5-backed knowledge base. One store holds multiple collections;
// each collection is a set of imported documents. Search is scoped to a
// collection (or all collections when empty).
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// Open creates/opens the RAG store at dbPath. The schema is a single FTS5 table
// keyed by collection + path + chunk index, plus four side tables for the
// structured-extraction layer (jobs/chunks/entities/relations). Existing FTS5
// data and behavior are unchanged; the new tables simply add alongside.
func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open rag db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	const schema = `
	CREATE VIRTUAL TABLE IF NOT EXISTS rag_fts USING fts5(
		collection UNINDEXED,
		path UNINDEXED,
		chunk UNINDEXED,
		body,
		tokenize='unicode61 remove_diacritics 2'
	);

	-- Extract jobs: one row per imported document being/having-been extracted.
	CREATE TABLE IF NOT EXISTS rag_jobs (
		id           TEXT PRIMARY KEY,
		collection   TEXT NOT NULL,
		path         TEXT NOT NULL,
		rel_path     TEXT,
		root_path    TEXT,
		is_dir       INTEGER DEFAULT 0,
		status       TEXT NOT NULL,
		total_chunks INTEGER DEFAULT 0,
		done_chunks  INTEGER DEFAULT 0,
		error_msg    TEXT,
		created_at   TEXT,
		updated_at   TEXT,
		UNIQUE(collection, path)
	);

	-- Per-chunk extraction state for fine-grained progress + retry tracking.
	CREATE TABLE IF NOT EXISTS rag_chunks (
		id         TEXT PRIMARY KEY,
		job_id     TEXT NOT NULL REFERENCES rag_jobs(id) ON DELETE CASCADE,
		idx        INTEGER NOT NULL,
		status     TEXT NOT NULL,
		attempts   INTEGER DEFAULT 0,
		latency_ms INTEGER,
		error_msg  TEXT,
		UNIQUE(job_id, idx)
	);

	-- Extracted entities. SIMPLE merge: (collection, name) unique where name is
	-- the normalized key; sources JSON accumulates provenance across chunks.
	CREATE TABLE IF NOT EXISTS rag_entities (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		collection  TEXT NOT NULL,
		name        TEXT NOT NULL,
		name_raw    TEXT NOT NULL,
		type        TEXT,
		description TEXT,
		sources     TEXT,
		UNIQUE(collection, name)
	);
	CREATE INDEX IF NOT EXISTS idx_ent_name ON rag_entities(collection, name);

	-- Extracted relations (directed edges between normalized entity names).
	CREATE TABLE IF NOT EXISTS rag_relations (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		collection  TEXT NOT NULL,
		source      TEXT NOT NULL,
		target      TEXT NOT NULL,
		type        TEXT,
		description TEXT,
		sources     TEXT,
		UNIQUE(collection, source, target, type)
	);
	CREATE INDEX IF NOT EXISTS idx_rel_src ON rag_relations(collection, source);
	CREATE INDEX IF NOT EXISTS idx_rel_tgt ON rag_relations(collection, target);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create rag schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Import adds a document to a collection, splitting it into chunks first. Re-
// importing the same path replaces its chunks (delete-then-insert). Returns the
// number of chunks stored. Tags are informational metadata (not yet indexed).
func (s *Store) Import(collection, path string, tags []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	body, ext, err := readDoc(path)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(body) == "" {
		return 0, fmt.Errorf("document %q has no extractable text", path)
	}
	collection = normalizeCollection(collection)
	chunks := chunkDoc(body, ext)

	// Delete existing chunks for this path+collection, then insert fresh.
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM rag_fts WHERE collection = ? AND path = ?", collection, path); err != nil {
		return 0, err
	}
	for i, c := range chunks {
		if _, err := tx.Exec("INSERT INTO rag_fts (collection, path, chunk, body) VALUES (?, ?, ?, ?)",
			collection, path, i, c); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(chunks), nil
}

// Result is one search hit.
type Result struct {
	Collection string
	Path       string
	Chunk      int
	Snippet    string
	Score      float64
}

// Search runs an FTS5 MATCH query scoped to collection (empty = all). Returns
// ranked results (BM25, higher = better) up to limit.
func (s *Store) Search(query, collection string, limit int) ([]Result, error) {
	collection = normalizeCollection(collection)
	if limit <= 0 {
		limit = 10
	}
	ftsQuery := buildQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var rows *sql.Rows
	var err error
	if collection == "" {
		rows, err = s.db.Query(`
			SELECT collection, path, chunk,
				snippet(rag_fts, 3, '<<', '>>', '...', 40) AS snip,
				bm25(rag_fts) AS score
			FROM rag_fts
			WHERE rag_fts MATCH ?
			ORDER BY score
			LIMIT ?`, ftsQuery, limit)
	} else {
		rows, err = s.db.Query(`
			SELECT collection, path, chunk,
				snippet(rag_fts, 3, '<<', '>>', '...', 40) AS snip,
				bm25(rag_fts) AS score
			FROM rag_fts
			WHERE rag_fts MATCH ? AND collection = ?
			ORDER BY score
			LIMIT ?`, ftsQuery, collection, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("rag search: %w", err)
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.Collection, &r.Path, &r.Chunk, &r.Snippet, &r.Score); err != nil {
			continue
		}
		r.Score = -r.Score // BM25: lower = better; negate.
		out = append(out, r)
	}
	return out, rows.Err()
}

// Delete removes all chunks for a path in a collection (or the whole collection
// when path is empty).
func (s *Store) Delete(collection, path string) error {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	if path == "" {
		_, err := s.db.Exec("DELETE FROM rag_fts WHERE collection = ?", collection)
		return err
	}
	_, err := s.db.Exec("DELETE FROM rag_fts WHERE collection = ? AND path = ?", collection, path)
	return err
}

// CollectionInfo describes one collection for rag_list.
type CollectionInfo struct {
	Name      string
	Documents int    // distinct paths
	Chunks    int    // total chunks
	Size      int64  // bytes of body text indexed (approx)
}

// List returns a summary per collection (all collections when name is empty).
func (s *Store) List(name string) ([]CollectionInfo, error) {
	name = normalizeCollection(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	q := `SELECT collection, count(DISTINCT path), count(*), coalesce(sum(length(body)),0)
	      FROM rag_fts`
	var rows *sql.Rows
	var err error
	if name == "" {
		rows, err = s.db.Query(q + " GROUP BY collection ORDER BY collection")
	} else {
		rows, err = s.db.Query(q+" WHERE collection = ? GROUP BY collection", name)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CollectionInfo
	for rows.Next() {
		var c CollectionInfo
		if err := rows.Scan(&c.Name, &c.Documents, &c.Chunks, &c.Size); err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func normalizeCollection(c string) string { return strings.ToLower(strings.TrimSpace(c)) }

// readDoc reads a file and returns its text + an extension hint for chunking.
// Phase 3 supports text-like formats (txt, md, code, csv, json, html). Binary
// Office formats (docx/xlsx/pdf) are handled by the doc_* tools later; here we
// return an error so the caller can fall back to those.
func readDoc(path string) (string, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return "", "", errors.New("path is a directory; pass a file")
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	// Binary formats we can't yet parse as text.
	switch ext {
	case "docx", "xlsx", "pptx", "pdf":
		return "", ext, fmt.Errorf("binary %s format — use doc_read/xlsx_read first, or import a text/markdown version", ext)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	// Strip HTML tags for .html.
	body := string(data)
	if ext == "html" || ext == "htm" {
		body = stripHTML(body)
	}
	return body, ext, nil
}

// chunkDoc splits a document into indexable chunks. Strategy: split on double
// newlines (paragraphs); merge short paragraphs; cap chunk size so snippets stay
// focused. Code files (no blank-line structure) fall back to fixed-size windows.
func chunkDoc(body, ext string) []string {
	const maxChunk = 1200 // chars; keeps snippets readable + token-cheap.
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	// Paragraph split (markdown, text). Short paragraphs merge up to maxChunk;
	// a single huge paragraph (no blank-line breaks) still needs windowing.
	if ext == "" || ext == "md" || ext == "txt" || ext == "markdown" {
		paras := strings.Split(body, "\n\n")
		var chunks []string
		var cur strings.Builder
		flush := func() {
			if cur.Len() > 0 {
				// Split an over-long accumulated block into windows so a giant
				// single-paragraph doc doesn't become one un-indexable blob.
				chunks = append(chunks, windowChunk(cur.String(), maxChunk)...)
				cur.Reset()
			}
		}
		for _, p := range paras {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if cur.Len()+len(p)+2 > maxChunk && cur.Len() > 0 {
				flush()
			}
			if cur.Len() > 0 {
				cur.WriteString("\n\n")
			}
			cur.WriteString(p)
		}
		flush()
		if len(chunks) > 0 {
			return chunks
		}
	}
	// Fallback: fixed-size windows (code, single-paragraph docs).
	return windowChunk(body, maxChunk)
}

// windowChunk splits s into <=max-byte windows. A single short string yields one
// chunk; a long string yields multiple. Used for code files and oversized blobs.
func windowChunk(s string, max int) []string {
	if len(s) <= max {
		return []string{s}
	}
	var chunks []string
	for i := 0; i < len(s); i += max {
		end := i + max
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

// stripHTML is a minimal tag stripper for .html imports (the agent can also use
// web_fetch for richer reduction). Good enough for indexing body text.
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	// Collapse whitespace.
	out := strings.Join(strings.Fields(b.String()), " ")
	return out
}

// buildQuery turns the search string into an FTS5 MATCH expression. We split on
// whitespace/punctuation into terms (preserving CJK runs as whole terms —
// unicode61 indexes "渲染" as one token, so per-character splitting would miss
// it) and join them with OR so any-term matches surface. Quote each term for
// safe phrase handling.
func buildQuery(input string) string {
	tokens := tokenize(input)
	if len(tokens) == 0 {
		return ""
	}
	var parts []string
	for _, t := range tokens {
		escaped := strings.ReplaceAll(t, `"`, `""`)
		parts = append(parts, `"`+escaped+`"`)
	}
	return strings.Join(parts, " OR ")
}

// tokenize splits input into indexable terms. CJK character runs are kept whole
// (unicode61 indexes them as single tokens), while latin words are split on
// whitespace/punctuation. This matches how the docs are stored.
func tokenize(input string) []string {
	var tokens []string
	var buf []rune
	flush := func() {
		if len(buf) > 0 {
			tokens = append(tokens, strings.ToLower(string(buf)))
			buf = buf[:0]
		}
	}
	for _, r := range input {
		switch {
		case isCJK(r):
			// Append to buffer: CJK runs form one token (渲染 stays together).
			// A space between CJK words separates them, which we honor below.
			if len(buf) > 0 && !isCJK(buf[len(buf)-1]) {
				flush()
			}
			buf = append(buf, r)
		case isTokenChar(r):
			if len(buf) > 0 && isCJK(buf[len(buf)-1]) {
				flush()
			}
			buf = append(buf, r)
		default:
			// Whitespace/punctuation: term boundary.
			flush()
		}
	}
	flush()
	return tokens
}

func isTokenChar(r rune) bool {
	return unicode.IsLetter(r) && !isCJK(r) || unicode.IsDigit(r) || r == '_'
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) ||
		r >= 0x3400 && r <= 0x4DBF ||
		r >= 0x20000 && r <= 0x2A6DF ||
		r >= 0xF900 && r <= 0xFAFF
}
