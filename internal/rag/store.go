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
	"encoding/csv"
	"encoding/json"
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
	CREATE INDEX IF NOT EXISTS idx_rel_tgt ON rag_relations(collection, target);

	-- Cached embeddings for rerank. Keyed by a content hash so a re-imported
	-- chunk (changed body) gets a fresh embedding automatically, and the same
	-- chunk queried repeatedly reuses the vector without re-calling the API.
	-- One row per (chunk content, model) pair; model is included so switching
	-- embedders doesn't poison the cache with mismatched vectors.
	CREATE TABLE IF NOT EXISTS rag_embeddings (
		chunk_hash TEXT NOT NULL,
		model      TEXT NOT NULL,
		vec        BLOB NOT NULL,
		PRIMARY KEY (chunk_hash, model)
	);`
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
// number of chunks stored.
//
// The tags parameter is currently ignored (deprecated): it was reserved for a
// metadata feature that was never wired through to storage or search. It is
// kept in the signature for source compatibility; pass nil. If per-document
// tags become wanted later, add a rag_doc_meta side table rather than reusing
// this param.
func (s *Store) Import(collection, path string, tags []string) (int, error) {
	_ = tags // reserved/unused — see doc comment.
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
// when path is empty). It cascades across every RAG table so no structured
// knowledge is orphaned: rag_fts (text), rag_jobs + rag_chunks (extraction
// state), and rag_entities + rag_relations (the knowledge graph). For a
// single-path delete the entity/relation rows are pruned precisely by their
// sources JSON — rows whose only source was this path are dropped; rows that
// also came from other files keep those sources and survive.
func (s *Store) Delete(collection, path string) error {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	if path == "" {
		// Whole collection: pure SQL across every table (the high-frequency
		// reset path). Order: chunks → jobs (chunks FK-cascade, but explicit is
		// harmless) → entities/relations → FTS5.
		for _, stmt := range []string{
			`DELETE FROM rag_chunks WHERE job_id IN (SELECT id FROM rag_jobs WHERE collection = ?)`,
			`DELETE FROM rag_jobs WHERE collection = ?`,
			`DELETE FROM rag_entities WHERE collection = ?`,
			`DELETE FROM rag_relations WHERE collection = ?`,
			`DELETE FROM rag_fts WHERE collection = ?`,
			`DELETE FROM rag_embeddings`, // no collection column — clear all; chunks are gone
		} {
			if _, err := s.db.Exec(stmt, collection); err != nil {
				return err
			}
		}
		return nil
	}
	// Single path: FTS5 + job/chunk state by exact collection+path, then a
	// precise sources-based prune of the knowledge graph.
	for _, stmt := range []string{
		`DELETE FROM rag_fts WHERE collection = ? AND path = ?`,
		`DELETE FROM rag_chunks WHERE job_id IN (SELECT id FROM rag_jobs WHERE collection = ? AND path = ?)`,
		`DELETE FROM rag_jobs WHERE collection = ? AND path = ?`,
	} {
		if _, err := s.db.Exec(stmt, collection, path); err != nil {
			return err
		}
	}
	if err := pruneSourcesByPath(s.db, "rag_entities", "id", collection, path); err != nil {
		return err
	}
	return pruneSourcesByPath(s.db, "rag_relations", "id", collection, path)
}

// pruneSourcesByPath trims a knowledge-graph table's rows by their `sources`
// JSON: remove Source entries pointing at `path`, drop rows left with no
// sources, and keep (with updated sources) rows that still have other origins.
// This makes a single-file delete leave the graph consistent instead of
// orphaning entities/relations whose provenance was only that file. Scoped to
// `collection` so cross-collection name collisions can't be touched.
func pruneSourcesByPath(db *sql.DB, table, idCol, collection, path string) error {
	rows, err := db.Query(
		`SELECT `+idCol+`, COALESCE(sources,'') FROM `+table+
			` WHERE collection = ? AND sources LIKE ?`,
		collection, "%"+path+"%")
	if err != nil {
		return err
	}
	type edit struct {
		id     string
		keep   []Source
		dropMe bool
	}
	var edits []edit
	for rows.Next() {
		var id, sj string
		if err := rows.Scan(&id, &sj); err != nil {
			rows.Close()
			return err
		}
		var srcs []Source
		if sj != "" {
			_ = json.Unmarshal([]byte(sj), &srcs)
		}
		kept := srcs[:0]
		for _, s := range srcs {
			if s.Path != path {
				kept = append(kept, s)
			}
		}
		edits = append(edits, edit{id: id, keep: kept, dropMe: len(kept) == 0})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, e := range edits {
		if e.dropMe {
			if _, err := db.Exec(`DELETE FROM `+table+` WHERE `+idCol+` = ?`, e.id); err != nil {
				return err
			}
			continue
		}
		sj, _ := json.Marshal(e.keep)
		if _, err := db.Exec(`UPDATE `+table+` SET sources = ? WHERE `+idCol+` = ?`, string(sj), e.id); err != nil {
			return err
		}
	}
	return nil
}

// Vacuum reclaims free space left by deletions. SQLite marks deleted rows as
// free internally but never shrinks the file without VACUUM, so a knowledge
// base that's been heavily imported-then-deleted can balloon. Safe but
// moderately expensive (rewrites the whole DB) — call after a collection clear
// or a large prune, not on every delete.
func (s *Store) Vacuum() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`VACUUM`)
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
// Tabular formats (csv/tsv) use a row-aware chunker with per-chunk header
// retention so the vertical (column) semantics survive splitting.
func chunkDoc(body, ext string) []string {
	const maxChunk = 1200 // chars; keeps snippets readable + token-cheap.
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	// Tabular formats: row-aware chunking with per-chunk header retention so the
	// vertical (column) semantics survive splitting. Rendered as Markdown pipe
	// tables, which carry column names into every chunk.
	if ext == "csv" || ext == "tsv" {
		comma := ','
		if ext == "tsv" {
			comma = '\t'
		}
		if c := chunkTabular(body, comma); len(c) > 0 {
			return c
		}
		// Malformed input where csv.Parse failed → fall through to generic window.
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

// windowChunk splits s into <=max-rune windows. A single short string yields one
// chunk; a long string yields multiple. Used for code files and oversized blobs.
// Slices by rune (not byte) so multi-byte characters — CJK in particular — are
// never split mid-character, which would produce invalid UTF-8. The byte-based
// slice this replaced corrupted mixed ASCII+CJK text whenever a leading ASCII
// byte misaligned the window boundary onto the middle of a 3-byte rune.
func windowChunk(s string, max int) []string {
	if max <= 0 {
		return []string{s}
	}
	if len(s) <= max {
		return []string{s}
	}
	r := []rune(s)
	var chunks []string
	for i := 0; i < len(r); i += max {
		end := i + max
		if end > len(r) {
			end = len(r)
		}
		chunks = append(chunks, string(r[i:end]))
	}
	return chunks
}

// chunkTabular parses a CSV/TSV document and chunks it into Markdown pipe
// tables, retaining the header row in EVERY chunk. This preserves the vertical
// (column) semantics across splits so a downstream search/extract reading any
// chunk still knows which column each value belongs to.
//
// Returns nil on parse failure or a header-only file, so the caller can fall
// back to generic windowing without losing the data. Reads row-by-row with
// FieldsPerRecord=-1 (a wrong field count on one row never aborts the whole
// file) and LazyQuotes (an unterminated quote degrades to a literal instead of
// a fatal error): the good rows survive and bad rows are skipped. A leading
// UTF-8 BOM (common from Excel/Notepad saves) is stripped so it doesn't pollute
// the first column name. Rows whose column count differs from the header are
// padded/truncated to the header width so every emitted table stays
// well-formed. Cells containing `|` or newlines are escaped so they cannot
// break the Markdown table structure.
func chunkTabular(body string, comma rune) []string {
	body = strings.TrimPrefix(body, "\uFEFF") // strip UTF-8 BOM (Excel/Notepad)
	r := csv.NewReader(strings.NewReader(body))
	r.Comma = comma
	r.FieldsPerRecord = -1 // tolerate ragged rows: don't abort on a field-count mismatch
	r.LazyQuotes = true    // tolerate stray quotes: degrade to literal, don't fail

	header, err := r.Read()
	if err != nil || len(header) == 0 {
		return nil // unreadable or empty → let the caller fall back
	}
	const maxChunk = 1200
	cols := len(header)
	mdHeader := renderTableRow(header) + renderTableSeparator(cols)

	var chunks []string
	var cur strings.Builder
	cur.WriteString(mdHeader)
	dataRows := 0
	flush := func() {
		// Only emit a chunk that actually carries data rows; a header-only
		// tail means every row fit in the previous chunk.
		if dataRows > 0 {
			chunks = append(chunks, cur.String())
		}
		cur.Reset()
		cur.WriteString(mdHeader)
		dataRows = 0
	}
	for {
		row, err := r.Read()
		if err != nil {
			break // EOF, or a malformed tail row we skip (good rows already kept)
		}
		mdRow := renderTableRow(padRow(row, cols))
		// If adding this row would overflow AND we already have data, start a
		// new chunk (which re-prepends the header). A single oversized row is
		// still emitted as its own chunk rather than dropped.
		if cur.Len()+len(mdRow) > maxChunk && dataRows > 0 {
			flush()
		}
		cur.WriteString(mdRow)
		dataRows++
	}
	flush()
	if len(chunks) == 0 {
		return nil
	}
	return chunks
}

// renderTableRow renders a record as a Markdown table row: "| a | b |".
func renderTableRow(row []string) string {
	for i, c := range row {
		row[i] = escapeCell(c)
	}
	return "| " + strings.Join(row, " | ") + " |\n"
}

// renderTableSeparator emits the Markdown table delimiter: "| --- | --- |".
func renderTableSeparator(cols int) string {
	dashes := make([]string, cols)
	for i := range dashes {
		dashes[i] = "---"
	}
	return "| " + strings.Join(dashes, " | ") + " |\n"
}

// escapeCell makes a cell safe for a Markdown pipe table: `|` would start a new
// column and newlines would break the row, so replace them with look-alikes.
func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// padRow forces row to width cols: short rows get empty trailing cells, long
// rows are truncated. Keeps every emitted table row well-formed.
func padRow(row []string, cols int) []string {
	if len(row) == cols {
		return row
	}
	if len(row) > cols {
		return row[:cols]
	}
	out := make([]string, cols)
	copy(out, row)
	return out
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
