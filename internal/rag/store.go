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
	// vecCache is an in-memory cache of entity embedding vectors for fast
	// parallel cosine similarity. Lazily loaded on first semantic search;
	// invalidated on any entity/embedding mutation.
	vecCache vecCache
}

// vecCache holds entity embeddings in a flat contiguous slice for cache-friendly
// parallel cosine similarity. One entry per (collection, model) key.
type vecCache struct {
	mu         sync.RWMutex
	loaded     bool
	collection string
	model      string
	// Flat vector storage: all vecs concatenated, dims inferred from len/dims.
	vecs  []float32
	dims  int
	metas []vecMeta // parallel metadata (entity info) per vector
}

type vecMeta struct {
	id      int64
	coll    string
	name    string
	nameRaw string
	typ     string
	desc    string
	sources []Source
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
	if _, err := db.Exec(baseSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create rag schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate rag schema: %w", err)
	}
	// Clean up orphaned chunks (FK violations from historical writes where
	// ON DELETE CASCADE didn't fire because PRAGMA foreign_keys was off).
	// Safe no-op if none exist.
	_, _ = db.Exec(`DELETE FROM rag_chunks WHERE job_id NOT IN (SELECT id FROM rag_jobs)`)
	// Clean up dangling relations (source/target entity no longer exists).
	_, _ = db.Exec(`DELETE FROM rag_relations WHERE source NOT IN (SELECT name FROM rag_entities) OR target NOT IN (SELECT name FROM rag_entities)`)
	return &Store{db: db}, nil
}

// baseSchema is the initial CREATE TABLE block (schema version 1). All tables
// use IF NOT EXISTS so a fresh database gets them in one shot.
const baseSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS rag_fts USING fts5(
	collection UNINDEXED,
	path UNINDEXED,
	chunk UNINDEXED,
	body,
	body_raw UNINDEXED,
	tokenize='unicode61 remove_diacritics 2'
);

-- Extract jobs: one row per imported document being/having-been extracted.
CREATE TABLE IF NOT EXISTS rag_jobs (
	id            TEXT PRIMARY KEY,
	collection    TEXT NOT NULL,
	path          TEXT NOT NULL,
	rel_path      TEXT,
	root_path     TEXT,
	is_dir        INTEGER DEFAULT 0,
	status        TEXT NOT NULL,
	total_chunks  INTEGER DEFAULT 0,
	done_chunks   INTEGER DEFAULT 0,
	error_msg     TEXT,
	content_hash  TEXT,
	stat_key      TEXT,
	node_prompt   TEXT,
	edge_prompt   TEXT,
	created_at    TEXT,
	updated_at    TEXT,
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
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	collection   TEXT NOT NULL,
	name         TEXT NOT NULL,
	name_raw     TEXT NOT NULL,
	type         TEXT,
	description  TEXT,
	sources      TEXT,
	relation_cnt INTEGER DEFAULT 0,
	community    INTEGER DEFAULT -1,
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
	weight      REAL DEFAULT 1.0,
	strength    REAL DEFAULT 5.0,
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
);
-- Entity embeddings for semantic search.
CREATE TABLE IF NOT EXISTS rag_entity_embeddings (
	entity_id  INTEGER NOT NULL REFERENCES rag_entities(id) ON DELETE CASCADE,
	collection TEXT NOT NULL,
	model      TEXT NOT NULL,
	vec        BLOB NOT NULL,
	PRIMARY KEY (entity_id, model)
);
CREATE INDEX IF NOT EXISTS idx_entity_emb_collection ON rag_entity_embeddings(collection);`

// ragSchemaVersion is the current schema version. Bump this whenever a migration
// step is added to ragMigrations below. PRAGMA user_version tracks the version
// on disk so existing databases upgrade forward without manual intervention.
var ragSchemaVersion = 7

// ragMigrations maps a target version → the statements to run when upgrading
// FROM version-1 TO that version. Each step runs inside a transaction; SQLite
// does not support "ALTER TABLE ADD COLUMN IF NOT EXISTS", so use addColumn
// helper which checks pragma_table_info first. New tables should instead go in
// baseSchema (they get created via IF NOT EXISTS on fresh DBs, and are absent
// from this map for existing DBs that predate them — a one-time CREATE IF NOT
// EXISTS step handles that case via migrateNewTables).
var ragMigrations = map[int][]string{
	// Version 1 is the base schema (created by baseSchema). No ALTER needed.
	1: {},
	// Version 2: rag_jobs gains content_hash/node_prompt/edge_prompt columns
	// (for content-based dedup + prompt persistence across resume). rag_fts is
	// rebuilt to add a body_raw UNINDEXED column (snippet/resume read the
	// original text while only the indexed body column is bigram-expanded for
	// Chinese substring recall). The rebuild re-derives bigram bodies from the
	// pre-v2 raw body. See migrateV2RebuildFTS for the FTS5-specific steps
	// (FTS5 virtual tables cannot be ALTERed, only dropped+recreated).
	2: {
		"ALTER TABLE rag_jobs ADD COLUMN content_hash TEXT",
		"ALTER TABLE rag_jobs ADD COLUMN node_prompt TEXT",
		"ALTER TABLE rag_jobs ADD COLUMN edge_prompt TEXT",
	},
	// Version 3: rag_entities gains a relation_cnt column (denormalized degree
	// counter) so TopEntities/GraphBatch can ORDER BY relation_cnt without a
	// correlated subquery per row. The column is backfilled by RecalcRelationCounts
	// (called in the migrate function after the ALTER). Maintained incrementally
	// on every UpsertRelation.
	3: {
		"ALTER TABLE rag_entities ADD COLUMN relation_cnt INTEGER DEFAULT 0",
	},
	// Version 4: rag_relations gains a weight column (co-occurrence frequency).
	// Each time the same (source, target, type) edge is extracted from a different
	// chunk, weight is incremented — strong edges (seen many times) render thicker
	// in the graph. Backfilled from the sources array length post-migration.
	4: {
		"ALTER TABLE rag_relations ADD COLUMN weight REAL DEFAULT 1.0",
	},
	// Version 5: rag_entities gains a community column (Louvain community ID).
	// Populated by DetectCommunities; -1 = not yet assigned.
	5: {
		"ALTER TABLE rag_entities ADD COLUMN community INTEGER DEFAULT -1",
	},
	// Version 6: rag_relations gains a strength column (LLM-assigned semantic
	// strength 1-10, distinct from weight which is co-occurrence count). Default
	// 5.0 = neutral for backfilled rows that predate the strength prompt.
	6: {
		"ALTER TABLE rag_relations ADD COLUMN strength REAL DEFAULT 5.0",
	},
	// Version 7: rag_jobs gains a stat_key column ("size:mtime" of the source
	// file). This powers a cheap re-import dedup that runs BEFORE the expensive
	// readDoc (markitdown/OCR) call: if size+mtime are unchanged, the body is
	// identical and we skip re-reading + re-extraction entirely. Catches the
	// "re-import a folder containing already-extracted files" case without
	// burning a markitdown subprocess (which was flashing CMD windows).
	7: {
		"ALTER TABLE rag_jobs ADD COLUMN stat_key TEXT",
	},
}

// migrateNewTables contains CREATE TABLE IF NOT EXISTS statements for tables
// introduced after schema version 1. They run for every database (fresh or
// existing) so an older DB that predates a table gets it created harmlessly.
// (ALTER TABLE for new columns goes in ragMigrations; brand-new tables go here.)
var migrateNewTables = []string{
	// Example: a table added in v3:
	//   "CREATE TABLE IF NOT EXISTS rag_doc_meta (...)",
}

func migrate(db *sql.DB) error {
	// Run any brand-new-table CREATE statements (idempotent).
	for _, stmt := range migrateNewTables {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("new-table step: %w", err)
		}
	}
	// Read current on-disk version.
	var current int
	if err := db.QueryRow(`SELECT user_version FROM pragma_user_version`).Scan(&current); err != nil {
		return err
	}
	// Apply forward migrations in order.
	for v := current + 1; v <= ragSchemaVersion; v++ {
		stmts, ok := ragMigrations[v]
		if !ok {
			continue // no statements for this version (e.g. v1)
		}
		for _, stmt := range stmts {
			if err := execMigrationStep(db, stmt); err != nil {
				return fmt.Errorf("migration v%d: %w", v, err)
			}
		}
		// v2 also rebuilds rag_fts to add the body_raw column. FTS5 virtual
		// tables cannot be ALTERed, so this is a drop+recreate+reinsert that
		// re-derives the bigram body from the pre-v2 raw body.
		if v == 2 {
			if err := migrateV2RebuildFTS(db); err != nil {
				return fmt.Errorf("migration v%d fts rebuild: %w", v, err)
			}
		}
		// v3 backfills relation_cnt for all pre-existing entities.
		if v == 3 {
			if _, err := db.Exec(`
					UPDATE rag_entities SET relation_cnt = (
						SELECT COUNT(*) FROM rag_relations r
						WHERE r.collection = rag_entities.collection
						  AND (r.source = rag_entities.name OR r.target = rag_entities.name)
					)`); err != nil {
				return fmt.Errorf("migration v%d backfill relation_cnt: %w", v, err)
			}
		}
		// v4 backfills relation weight from the sources JSON array length
		// (each entry = one chunk that extracted this edge). Guard with
		// json_valid so malformed JSON in the sources column doesn't block
		// migration — those rows keep the DEFAULT 1.0 weight.
		if v == 4 {
			if _, err := db.Exec(`
					UPDATE rag_relations SET weight = MAX(1.0, COALESCE(json_array_length(sources), 1))
				 WHERE sources IS NOT NULL AND sources != '' AND json_valid(sources)
				`); err != nil {
				return fmt.Errorf("migration v%d backfill weight: %w", v, err)
			}
		}
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", v)); err != nil {
			return fmt.Errorf("set user_version=%d: %w", v, err)
		}
	}
	// Ensure the version is at least 1 even on a fresh DB (baseSchema doesn't set it).
	if current == 0 && ragSchemaVersion >= 1 {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", ragSchemaVersion)); err != nil {
			return fmt.Errorf("set user_version: %w", err)
		}
	}
	return nil
}

// execMigrationStep runs one migration statement. For ALTER TABLE ADD COLUMN,
// SQLite lacks IF NOT EXISTS, so we check pragma_table_info and skip if the
// column already exists (makes migrations idempotent / re-runnable).
func execMigrationStep(db *sql.DB, stmt string) error {
	// Detect "ALTER TABLE <t> ADD COLUMN <col>" and guard with an existence check.
	if col, table, ok := parseAddColumn(stmt); ok {
		exists, err := columnExists(db, table, col)
		if err != nil {
			return err
		}
		if exists {
			return nil // already applied; idempotent
		}
	}
	_, err := db.Exec(stmt)
	return err
}

// parseAddColumn extracts (column, table) from a statement like
// "ALTER TABLE rag_entities ADD COLUMN aliases TEXT". Returns ok=false if the
// statement is not an ADD COLUMN form.
func parseAddColumn(stmt string) (col, table string, ok bool) {
	s := strings.TrimSpace(stmt)
	up := strings.ToUpper(s)
	if !strings.HasPrefix(up, "ALTER TABLE") {
		return "", "", false
	}
	if !strings.Contains(up, "ADD COLUMN") {
		return "", "", false
	}
	rest := strings.TrimSpace(s[len("ALTER TABLE"):])
	// table name = first token
	sp := strings.IndexByte(rest, ' ')
	if sp < 0 {
		return "", "", false
	}
	table = rest[:sp]
	rest = strings.TrimSpace(rest[sp+1:])
	// skip "ADD COLUMN"
	idx := strings.Index(strings.ToUpper(rest), "ADD COLUMN")
	if idx < 0 {
		return "", "", false
	}
	rest = strings.TrimSpace(rest[idx+len("ADD COLUMN"):])
	// column name = first token of what remains
	sp = strings.IndexByte(rest, ' ')
	if sp < 0 {
		col = rest
	} else {
		col = rest[:sp]
	}
	col = strings.Trim(col, `"[]`)
	return col, table, true
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	// FTS5 virtual tables don't expose columns via pragma_table_info reliably;
	// for rag_fts we detect the body_raw column by probing a SELECT instead.
	if table == "rag_fts" {
		return ftsColumnExists(db, column)
	}
	rows, err := db.Query(fmt.Sprintf("SELECT name FROM pragma_table_info('%s')", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// ftsColumnExists probes an FTS5 table for a column by attempting a SELECT of
// it (pragma_table_info is unreliable for FTS5 virtual tables). Returns true
// if the column exists.
func ftsColumnExists(db *sql.DB, column string) (bool, error) {
	rs, err := db.Query(fmt.Sprintf("SELECT %s FROM rag_fts LIMIT 1", column))
	if err != nil {
		return false, nil // column doesn't exist → "no such column"
	}
	rs.Close()
	return true, nil
}

// migrateV2RebuildFTS rebuilds the rag_fts virtual table to add the body_raw
// UNINDEXED column. FTS5 tables cannot be ALTERed, so this reads all existing
// rows, drops the table, recreates it with the new schema, and reinserts each
// row with body=expandCJKBigrams(oldBody) and body_raw=oldBody. For pre-v2
// databases the stored body is the original (un-expanded) text, so it becomes
// body_raw and the bigram expansion is computed fresh. Idempotent: if body_raw
// already exists (fresh DB created with baseSchema v2), this is a no-op.
func migrateV2RebuildFTS(db *sql.DB) error {
	hasRaw, err := ftsColumnExists(db, "body_raw")
	if err != nil {
		return err
	}
	if hasRaw {
		return nil // already migrated (fresh DB or re-run)
	}
	// Read all existing rows (old schema: collection, path, chunk, body).
	type ftsRow struct {
		collection, path string
		chunk            int
		body             string
	}
	rows, err := db.Query(`SELECT collection, path, chunk, body FROM rag_fts`)
	if err != nil {
		return err
	}
	var existing []ftsRow
	for rows.Next() {
		var r ftsRow
		if err := rows.Scan(&r.collection, &r.path, &r.chunk, &r.body); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	// Drop and recreate with the new schema (body + body_raw).
	if _, err := db.Exec(`DROP TABLE IF EXISTS rag_fts`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE VIRTUAL TABLE rag_fts USING fts5(
		collection UNINDEXED,
		path UNINDEXED,
		chunk UNINDEXED,
		body,
		body_raw UNINDEXED,
		tokenize='unicode61 remove_diacritics 2'
	)`); err != nil {
		return err
	}
	// Reinsert: old body becomes body_raw; body gets the bigram expansion.
	for _, r := range existing {
		if _, err := db.Exec(`INSERT INTO rag_fts (collection, path, chunk, body, body_raw) VALUES (?, ?, ?, ?, ?)`,
			r.collection, r.path, r.chunk, expandCJKBigrams(r.body), r.body); err != nil {
			return err
		}
	}
	return nil
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
	// Also clean up any placeholder docs for this collection (they exist only
	// to make empty collections visible in ListRagCollections).
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("DELETE FROM rag_fts WHERE collection = ? AND (path = ? OR path LIKE 'placeholder://%')", collection, path); err != nil {
		return 0, err
	}
	for i, c := range chunks {
		// body holds the bigram-expanded text for CJK substring recall; body_raw
		// holds the original text for snippet display + extraction resume (the
		// expansion must NOT leak into user-visible snippets or LLM input).
		if _, err := tx.Exec("INSERT INTO rag_fts (collection, path, chunk, body, body_raw) VALUES (?, ?, ?, ?, ?)",
			collection, path, i, expandCJKBigrams(c), c); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(chunks), nil
}

// ImportContent imports pre-read content into FTS5 (avoids re-reading the file).
// Use this when the caller already has the body and ext from a prior readDoc call.
func (s *Store) ImportContent(collection, path, body, ext string) (int, error) {
	if strings.TrimSpace(body) == "" {
		return 0, fmt.Errorf("document %q has no extractable text", path)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	collection = normalizeCollection(collection)
	chunks := chunkDoc(body, ext)

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("DELETE FROM rag_fts WHERE collection = ? AND path = ?", collection, path); err != nil {
		return 0, err
	}
	for i, c := range chunks {
		// body holds the bigram-expanded text for CJK substring recall; body_raw
		// holds the original text for snippet display + extraction resume (the
		// expansion must NOT leak into user-visible snippets or LLM input).
		if _, err := tx.Exec("INSERT INTO rag_fts (collection, path, chunk, body, body_raw) VALUES (?, ?, ?, ?, ?)",
			collection, path, i, expandCJKBigrams(c), c); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(chunks), nil
}

// ImportText adds raw text directly to a collection (for incremental updates).
// The text is chunked and indexed into FTS5 immediately.
func (s *Store) ImportText(collection, virtualPath, text string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(text) == "" {
		return 0, fmt.Errorf("text is empty")
	}
	collection = normalizeCollection(collection)
	chunks := chunkDoc(text, "txt")
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("DELETE FROM rag_fts WHERE collection = ? AND path = ?", collection, virtualPath); err != nil {
		return 0, err
	}
	for i, c := range chunks {
		if _, err := tx.Exec("INSERT INTO rag_fts (collection, path, chunk, body, body_raw) VALUES (?, ?, ?, ?, ?)",
			collection, virtualPath, i, expandCJKBigrams(c), c); err != nil {
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

	// Filter out placeholder rows (virtual paths like "placeholder://...")
	// so they never pollute search results. Placeholder rows exist only to
	// give a collection at least one FTS5 entry so it shows up in the list.
	const placeholderFilter = ` AND path NOT LIKE 'placeholder://%'`

	var rows *sql.Rows
	var err error
	if collection == "" {
		rows, err = s.db.Query(`
			SELECT collection, path, chunk,
				snippet(rag_fts, 4, '<<', '>>', '...', 40) AS snip,
				bm25(rag_fts) AS score
			FROM rag_fts
			WHERE rag_fts MATCH ?`+placeholderFilter+`
			ORDER BY score
			LIMIT ?`, ftsQuery, limit)
	} else {
		// Path-prefix matching: selecting "工作" should also search "工作/领导材料".
		// We match collection = ? OR collection LIKE ? || '/%'.
		rows, err = s.db.Query(`
			SELECT collection, path, chunk,
				snippet(rag_fts, 4, '<<', '>>', '...', 40) AS snip,
				bm25(rag_fts) AS score
			FROM rag_fts
			WHERE rag_fts MATCH ?`+placeholderFilter+` AND (collection = ? OR collection LIKE ?)
			ORDER BY score
			LIMIT ?`, ftsQuery, collection, collection+"/%", limit)
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
		// Whole collection: wrap in transaction for atomicity.
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		for _, stmt := range []string{
			`DELETE FROM rag_chunks WHERE job_id IN (SELECT id FROM rag_jobs WHERE collection = ?)`,
			`DELETE FROM rag_jobs WHERE collection = ?`,
			`DELETE FROM rag_entities WHERE collection = ?`,
			`DELETE FROM rag_relations WHERE collection = ?`,
			`DELETE FROM rag_fts WHERE collection = ?`,
		} {
			if _, err := tx.Exec(stmt, collection); err != nil {
				return err
			}
		}
		// Only clear embeddings if no other collections remain; embeddings are
		// keyed by content hash with no collection column, so clearing them for
		// one collection would penalize all others.
		var count int
		if err := tx.QueryRow(`SELECT COUNT(DISTINCT collection) FROM rag_jobs`).Scan(&count); err == nil && count == 0 {
			if _, err := tx.Exec(`DELETE FROM rag_embeddings`); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		s.invalidateVecCache()
		return nil
	}
	// Single path: wrap in transaction for atomicity.
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`DELETE FROM rag_fts WHERE collection = ? AND path = ?`,
		`DELETE FROM rag_chunks WHERE job_id IN (SELECT id FROM rag_jobs WHERE collection = ? AND path = ?)`,
		`DELETE FROM rag_jobs WHERE collection = ? AND path = ?`,
	} {
		if _, err := tx.Exec(stmt, collection, path); err != nil {
			return err
		}
	}
	if err := pruneSourcesByPath(tx, "rag_entities", "id", collection, path); err != nil {
		return err
	}
	if err := pruneSourcesByPath(tx, "rag_relations", "id", collection, path); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidateVecCache()
	return nil
}

// pruneSourcesByPath trims a knowledge-graph table's rows by their `sources`
// JSON: remove Source entries pointing at `path`, drop rows left with no
// sources, and keep (with updated sources) rows that still have other origins.
// This makes a single-file delete leave the graph consistent instead of
// orphaning entities/relations whose provenance was only that file. Scoped to
// `collection` so cross-collection name collisions can't be touched.
// sqlExecQuerier is satisfied by both *sql.DB and *sql.Tx.
type sqlExecQuerier interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
}

func pruneSourcesByPath(db sqlExecQuerier, table, idCol, collection, path string) error {
	// Escape LIKE wildcards in the path so a literal % or _ in a filename
	// doesn't expand the candidate set. We still over-fetch rows whose sources
	// merely contain the path as a substring, but the exact s.Path == path
	// check below guarantees only the right rows are pruned.
	likePath := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(path) + "%"
	rows, err := db.Query(
		`SELECT `+idCol+`, COALESCE(sources,'') FROM `+table+
			` WHERE collection = ? AND sources LIKE ? ESCAPE '\'`,
		collection, likePath)
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
			if err := json.Unmarshal([]byte(sj), &srcs); err != nil {
				// Corrupted JSON — keep row as-is, don't risk deleting valid data.
				continue
			}
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
	Documents int   // distinct paths
	Chunks    int   // total chunks
	Size      int64 // bytes of body text indexed (approx)
}

// List returns a summary per collection (all collections when name is empty).
func (s *Store) List(name string) ([]CollectionInfo, error) {
	name = normalizeCollection(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	q := `SELECT collection,
	             count(DISTINCT CASE WHEN path NOT LIKE 'placeholder://%' AND path NOT LIKE 'virtual://%' THEN path END),
	             count(CASE WHEN path NOT LIKE 'placeholder://%' AND path NOT LIKE 'virtual://%' THEN 1 END),
	             coalesce(sum(CASE WHEN path NOT LIKE 'placeholder://%' AND path NOT LIKE 'virtual://%' THEN length(body) ELSE 0 END), 0)
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

// RenameCollection updates all tables from oldName to newName (a path prefix
// rename: "工作" → "工作资料" also updates "工作/领导材料" → "工作资料/领导材料").
// Used by the collection tree's right-click rename. Transaction-wrapped so the
// rename either fully applies or not at all.
func (s *Store) RenameCollection(oldName, newName string) error {
	oldName = normalizeCollection(oldName)
	newName = normalizeCollection(newName)
	if oldName == "" || newName == "" || oldName == newName {
		return fmt.Errorf("invalid rename: %q → %q", oldName, newName)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Rename exact match and path-prefix children ("工作" → "工作资料",
	// "工作/领导材料" → "工作资料/领导材料").
	tables := []string{"rag_fts", "rag_jobs", "rag_entities", "rag_relations", "rag_chunks"}
	for _, table := range tables {
		// Exact match.
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET collection = ? WHERE collection = ?`, table), newName, oldName); err != nil {
			return err
		}
		// Path-prefix children: "工作/xxx" → "工作资料/xxx"
		oldPrefix := oldName + "/"
		newPrefix := newName + "/"
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET collection = ? || substr(collection, ?) WHERE collection LIKE ?`, table), newPrefix, len(oldPrefix)+1, oldPrefix+"%"); err != nil {
			// rag_chunks may not have collection column — skip gracefully.
			continue
		}
	}
	return tx.Commit()
}

// CreateCollection creates an empty collection by inserting a placeholder
// FTS5 row so it appears in List(). The placeholder is filtered from Search()
// results by the "path NOT LIKE 'placeholder://%'" clause. When the user
// imports real documents the placeholder is replaced.
func (s *Store) CreateCollection(name string) error {
	name = normalizeCollection(name)
	if name == "" {
		return fmt.Errorf("collection name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check if already exists.
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM rag_fts WHERE collection = ?`, name).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil // already exists, no-op
	}
	virtualPath := "placeholder://" + name
	_, err := s.db.Exec(`INSERT INTO rag_fts (collection, path, chunk, body) VALUES (?, ?, 0, '')`,
		name, virtualPath)
	return err
}

// DeleteCollectionTree removes a collection and all its path-prefix children
// (e.g. deleting "工作" also deletes "工作/领导材料"). Delegates to Delete with
// empty path for the exact collection, then deletes children.
func (s *Store) DeleteCollectionTree(name string) error {
	name = normalizeCollection(name)
	if name == "" {
		return fmt.Errorf("collection name is required")
	}
	// First delete the collection itself.
	if err := s.Delete(name, ""); err != nil {
		return err
	}
	// Then delete all path-prefix children.
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := name + "/%"
	for _, stmt := range []string{
		`DELETE FROM rag_chunks WHERE job_id IN (SELECT id FROM rag_jobs WHERE collection LIKE ?)`,
		`DELETE FROM rag_jobs WHERE collection LIKE ?`,
		`DELETE FROM rag_entities WHERE collection LIKE ?`,
		`DELETE FROM rag_relations WHERE collection LIKE ?`,
		`DELETE FROM rag_fts WHERE collection LIKE ?`,
	} {
		if _, err := s.db.Exec(stmt, prefix); err != nil {
			// rag_chunks may not have the column — skip gracefully.
			continue
		}
	}
	return nil
}

// readDoc reads a file and returns its text + an extension hint for chunking.
// Text-like formats (txt, md, code, csv, json, html) are parsed inline; binary
// Office formats (docx/xlsx/pptx/pdf) go through docconv/markitdown with a Go
// fallback. ReadDoc reads a document file and returns its text content and
// extension.
func ReadDoc(path string) (string, string, error) {
	return readDoc(path)
}

// richDocumentExts are rich-text or binary formats handled preferably by markitdown (Python)
// with Go fallback for some formats.
var richDocumentExts = map[string]bool{
	"docx": true, "xlsx": true, "pptx": true,
	"doc": true, "xls": true, "ppt": true,
	"epub": true, "msg": true, "html": true, "htm": true,
}

func readDoc(path string) (string, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return "", "", errors.New("path is a directory; pass a file")
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))

	// PDF: dedicated pipeline (pdfplumber + PaddleOCR for scanned pages).
	if ext == "pdf" {
		content, err := readPDF(path)
		if err != nil {
			return "", ext, fmt.Errorf("read pdf: %w", err)
		}
		return content, "markdown", nil
	}

	// Rich document formats: try markitdown first, fallback to Go parsers if available.
	if richDocumentExts[ext] {
		if findDocConverterScript() != "" {
			text, err := convertWithMarkitdown(path)
			if err == nil && len(text) > 0 {
				return text, "markdown", nil
			}
		}
		// Fallback to Go parsers.
		switch ext {
		case "docx":
			content, err := readDOCX(path)
			if err != nil {
				return "", ext, fmt.Errorf("read docx: %w", err)
			}
			return content, ext, nil
		case "xlsx":
			content, err := readXLSXAsText(path)
			if err != nil {
				return "", ext, fmt.Errorf("read xlsx: %w", err)
			}
			return content, ext, nil
		case "pptx":
			content, err := readPPTX(path)
			if err != nil {
				return "", ext, fmt.Errorf("read pptx: %w", err)
			}
			return content, ext, nil
		case "html", "htm":
			data, err := os.ReadFile(path)
			if err != nil {
				return "", "", err
			}
			return stripHTML(string(data)), ext, nil
		default:
			// Formats like doc, ppt, xls, epub, msg have no Go fallback parser.
			return "", ext, fmt.Errorf(".%s requires markitdown to parse properly (or parsing failed)", ext)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	return string(data), ext, nil
}

// chunkDoc splits a document into indexable chunks. Strategy: split on double
// newlines (paragraphs); merge short paragraphs; cap chunk size so snippets stay
// focused. Code files (no blank-line structure) fall back to fixed-size windows.
// Tabular formats (csv/tsv) use a row-aware chunker with per-chunk header
// retention so the vertical (column) semantics survive splitting.
func chunkDoc(body, ext string) []string {
	const maxChunk = 3000 // chars; balances snippet readability with fewer LLM calls (was 1200).
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
	// Markdown-aware split: preserve tables and heading boundaries.
	if ext == "markdown" || ext == "md" || ext == "txt" || ext == "" {
		chunks := chunkMarkdown(body, maxChunk)
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

// chunkMarkdown splits Markdown text into chunks, preserving tables and heading
// boundaries. Tables (lines starting with |) are kept intact up to a size limit.
// Headings at any level (# through ######) start new chunks.
func chunkMarkdown(body string, max int) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	lines := strings.Split(body, "\n")
	var chunks []string
	var cur strings.Builder
	inTable := false
	tableHeader := "" // first row of current table, for repeating on split.

	flush := func() {
		if cur.Len() > 0 {
			text := strings.TrimSpace(cur.String())
			if text != "" {
				if len(text) > max {
					chunks = append(chunks, windowChunk(text, max)...)
				} else {
					chunks = append(chunks, text)
				}
			}
			cur.Reset()
		}
		tableHeader = ""
	}

	// isMarkdownHeading checks for # through ###### followed by a space.
	isMarkdownHeading := func(s string) bool {
		if len(s) == 0 || s[0] != '#' {
			return false
		}
		rest := strings.TrimLeft(s, "#")
		return len(rest) > 0 && rest[0] == ' '
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isTableLine := strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|")

		// Flush before a heading (start new chunk).
		if isMarkdownHeading(trimmed) && cur.Len() > 0 {
			flush()
		}

		// Track table state.
		if isTableLine {
			if !inTable {
				inTable = true
				tableHeader = line // remember first row for potential repeat.
			}
		} else if inTable && trimmed != "" {
			// End of table block — flush to keep table intact.
			inTable = false
			flush()
		}

		// Large table guard: flush table block if it exceeds 4x max.
		if inTable && cur.Len()+len(line)+1 > max*4 && cur.Len() > 0 {
			flush()
			// Repeat header for the continuation.
			if tableHeader != "" {
				cur.WriteString(tableHeader)
				cur.WriteByte('\n')
			}
		}

		// If adding this line would exceed max and we're not in a table, flush.
		if !inTable && cur.Len()+len(line)+1 > max && cur.Len() > 0 {
			flush()
		}

		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(line)
	}
	flush()

	if len(chunks) == 0 {
		return nil
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
	const maxChunk = 3000
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

// tokenize splits input into indexable terms. CJK runs of length ≥ 2 are
// expanded into overlapping bigrams (matching the index transform in
// expandCJKBigrams), so a query for "管线" produces the token "管线" that was
// indexed for a document containing "渲染管线". Single CJK characters and
// latin/digit words pass through as whole terms.
func tokenize(input string) []string {
	var tokens []string
	var buf []rune
	flush := func() {
		if len(buf) == 0 {
			return
		}
		// If the buffered run is CJK, expand to bigrams; otherwise emit whole.
		if isCJK(buf[0]) {
			run := string(buf)
			tokens = append(tokens, cjkBigrams(run)...)
			// Also keep the whole run so an exact multi-char CJK phrase still
			// matches (it was indexed as bigrams, but the full run may also
			// appear when the doc had a single-char context).
			if len([]rune(run)) == 1 {
				tokens = append(tokens, strings.ToLower(run))
			}
		} else {
			tokens = append(tokens, strings.ToLower(string(buf)))
		}
		buf = buf[:0]
	}
	for _, r := range input {
		switch {
		case isCJK(r):
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
			flush()
		}
	}
	flush()
	return tokens
}

// cjkBigrams returns the overlapping character pairs of a CJK run as lowercase
// strings: "渲染管线" → ["渲染","染管","管线"]. A single-char run returns itself.
func cjkBigrams(run string) []string {
	rr := []rune(run)
	if len(rr) <= 1 {
		return []string{strings.ToLower(run)}
	}
	out := make([]string, 0, len(rr)-1)
	for i := 0; i+1 < len(rr); i++ {
		out = append(out, strings.ToLower(string(rr[i:i+2])))
	}
	return out
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

// expandCJKBigrams transforms CJK text so FTS5's unicode61 tokenizer indexes
// overlapping character pairs, enabling substring recall for Chinese.
//
// Problem: unicode61 indexes a CJK run like "渲染管线" as ONE token. Searching
// "管线" fails because FTS5 has no CJK substring matching. Solution: rewrite
// each CJK run of length N into its N-1 adjacent bigrams joined by spaces
// ("渲染 染管 管线"). Single CJK characters pass through unchanged. Latin/digit
// runs are left intact. Both the index path (ImportContent/ImportText) and the
// query path (tokenize) apply this transform, so "管线" → "管线" matches the
// indexed bigram. Non-CJK text is unaffected, so English recall is unchanged.
//
// Applied at write time to the FTS body so snippet output stays readable (the
// spaces between bigrams are invisible in snippet rendering).
func expandCJKBigrams(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	var run []rune
	// pendingBoundary is set when the last emitted char was non-CJK, so a
	// following CJK run gets a leading space (latin→CJK token break). It is
	// captured at run-start time, not overwritten as the run accumulates.
	pendingBoundary := false
	flushRun := func() {
		switch len(run) {
		case 0:
		case 1:
			if pendingBoundary {
				b.WriteByte(' ')
			}
			b.WriteRune(run[0])
		default:
			if pendingBoundary {
				b.WriteByte(' ')
			}
			for i := 0; i+1 < len(run); i++ {
				if i > 0 {
					b.WriteByte(' ')
				}
				b.WriteRune(run[i])
				b.WriteRune(run[i+1])
			}
		}
		run = run[:0]
	}
	for _, r := range s {
		if isCJK(r) {
			// pendingBoundary is only set by the non-CJK branch below, so it
			// correctly reflects whether a latin/digit run preceded this CJK
			// run (flushRun reads it at run-end, after the run accumulated).
			run = append(run, r)
		} else {
			flushRun()
			b.WriteRune(r)
			pendingBoundary = true // next CJK run needs a boundary space
		}
	}
	flushRun()
	return b.String()
}
