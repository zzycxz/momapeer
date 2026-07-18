package rag

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// TestFreshDatabaseGetsCurrentVersion verifies that a newly-created database is
// stamped with the current schema version after Open.
func TestFreshDatabaseGetsCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var v int
	if err := store.db.QueryRow(`SELECT user_version FROM pragma_user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != ragSchemaVersion {
		t.Fatalf("fresh DB user_version = %d, want %d", v, ragSchemaVersion)
	}
}

// TestMigrationAddsColumnToOldDatabase simulates a database created with the v1
// base schema (no user_version) that then needs a v2 ADD COLUMN migration. This
// validates the core migration mechanism without polluting the production
// ragMigrations map — we call migrate() against a DB we manually staged.
func TestMigrationAddsColumnToOldDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rag.db")

	// 1. Stage a database that predates user_version: just the base schema,
	//    with user_version = 0 (as if created before migrations existed).
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(baseSchema); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// 2. Temporarily register a v2 migration that adds a column, then bump the
	//    schema version so migrate() picks it up.
	savedVersion := ragSchemaVersion
	savedMigrations := ragMigrations
	defer func() {
		ragSchemaVersion = savedVersion
		ragMigrations = savedMigrations
	}()
	ragMigrations = map[int][]string{
		2: {fmt.Sprintf("ALTER TABLE rag_entities ADD COLUMN %s TEXT", "_test_aliases")},
	}
	ragSchemaVersion = 2

	// 3. Open the existing DB — migrate() should run the v2 step.
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open after migration: %v", err)
	}
	defer store.Close()

	// 4. user_version should now be 2.
	var v int
	if err := store.db.QueryRow(`SELECT user_version FROM pragma_user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != ragSchemaVersion {
		t.Fatalf("post-migration user_version = %d, want %d", v, ragSchemaVersion)
	}

	// 5. The new column should exist and be usable.
	has, err := columnExists(store.db, "rag_entities", "_test_aliases")
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("rag_entities._test_aliases column missing after migration")
	}
	// Write through the new column to prove it's real.
	if _, err := store.db.Exec(`INSERT INTO rag_entities (collection, name, name_raw, _test_aliases) VALUES ('test', 'x', 'x', 'a;b')`); err != nil {
		t.Fatalf("insert via new column: %v", err)
	}
}

// TestMigrationIsIdempotent verifies that calling migrate() a second time (or
// opening a DB already at the current version) does not fail or re-run steps.
func TestMigrationIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rag.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// Re-open the same DB; migrate() should be a no-op (version already current).
	store2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer store2.Close()

	var v int
	if err := store2.db.QueryRow(`SELECT user_version FROM pragma_user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != ragSchemaVersion {
		t.Fatalf("re-opened user_version = %d, want %d", v, ragSchemaVersion)
	}
}

// TestParseAddColumn guards the lightweight statement parser used to make ADD
// COLUMN migrations idempotent.
func TestParseAddColumn(t *testing.T) {
	cases := []struct {
		stmt      string
		wantCol   string
		wantTable string
		wantOK    bool
	}{
		{"ALTER TABLE rag_entities ADD COLUMN aliases TEXT", "aliases", "rag_entities", true},
		{"ALTER TABLE rag_jobs ADD COLUMN content_hash TEXT", "content_hash", "rag_jobs", true},
		{"  ALTER TABLE  rag_chunks  ADD COLUMN  note TEXT  ", "note", "rag_chunks", true},
		{`ALTER TABLE "rag_entities" ADD COLUMN foo INTEGER`, "foo", `"rag_entities"`, true},
		{"CREATE TABLE IF NOT EXISTS foo (id INT)", "", "", false},
		{"ALTER TABLE rag_entities DROP COLUMN aliases", "", "", false},
	}
	for i, tc := range cases {
		col, table, ok := parseAddColumn(tc.stmt)
		if ok != tc.wantOK || col != tc.wantCol || table != tc.wantTable {
			t.Errorf("case %d: parseAddColumn(%q) = (%q,%q,%v), want (%q,%q,%v)",
				i, tc.stmt, col, table, ok, tc.wantCol, tc.wantTable, tc.wantOK)
		}
	}
}

// v1BaseSchema is the schema as it existed at version 1 (rag_fts has no
// body_raw column; rag_jobs has no content_hash/node_prompt/edge_prompt). Used
// to stage a pre-v2 database for migration testing.
const v1BaseSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS rag_fts USING fts5(
	collection UNINDEXED, path UNINDEXED, chunk UNINDEXED, body,
	tokenize='unicode61 remove_diacritics 2'
);
CREATE TABLE IF NOT EXISTS rag_jobs (
	id TEXT PRIMARY KEY, collection TEXT NOT NULL, path TEXT NOT NULL,
	rel_path TEXT, root_path TEXT, is_dir INTEGER DEFAULT 0,
	status TEXT NOT NULL, total_chunks INTEGER DEFAULT 0, done_chunks INTEGER DEFAULT 0,
	error_msg TEXT, created_at TEXT, updated_at TEXT, UNIQUE(collection, path)
);
CREATE TABLE IF NOT EXISTS rag_chunks (
	id TEXT PRIMARY KEY, job_id TEXT NOT NULL, idx INTEGER NOT NULL,
	status TEXT NOT NULL, attempts INTEGER DEFAULT 0, latency_ms INTEGER, error_msg TEXT,
	UNIQUE(job_id, idx)
);
CREATE TABLE IF NOT EXISTS rag_entities (
	id INTEGER PRIMARY KEY AUTOINCREMENT, collection TEXT NOT NULL, name TEXT NOT NULL,
	name_raw TEXT NOT NULL, type TEXT, description TEXT, sources TEXT, UNIQUE(collection, name)
);
CREATE INDEX IF NOT EXISTS idx_ent_name ON rag_entities(collection, name);
CREATE TABLE IF NOT EXISTS rag_relations (
	id INTEGER PRIMARY KEY AUTOINCREMENT, collection TEXT NOT NULL, source TEXT NOT NULL,
	target TEXT NOT NULL, type TEXT, description TEXT, sources TEXT,
	UNIQUE(collection, source, target, type)
);
CREATE INDEX IF NOT EXISTS idx_rel_src ON rag_relations(collection, source);
CREATE INDEX IF NOT EXISTS idx_rel_tgt ON rag_relations(collection, target);
CREATE TABLE IF NOT EXISTS rag_embeddings (
	chunk_hash TEXT NOT NULL, model TEXT NOT NULL, vec BLOB NOT NULL, PRIMARY KEY (chunk_hash, model)
);
CREATE TABLE IF NOT EXISTS rag_entity_embeddings (
	entity_id INTEGER NOT NULL, collection TEXT NOT NULL, model TEXT NOT NULL, vec BLOB NOT NULL,
	PRIMARY KEY (entity_id, model)
);
CREATE INDEX IF NOT EXISTS idx_entity_emb_collection ON rag_entity_embeddings(collection);`

// TestV2MigrationRebuildsFTSAndPreservesCJKRecall simulates a real upgrade: a
// v1 database with a Chinese document whose body is the RAW (un-bigrammed)
// text. After Open() (which runs the v1→v2 migration), the body_raw column
// must exist, the original text must be preserved for snippets, and CJK
// substring search (enabled by the bigram body) must work — proving存量文档
// don't silently lose Chinese search after upgrade.
func TestV2MigrationRebuildsFTSAndPreservesCJKRecall(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "rag.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	// Stage a v1 database: old schema + a Chinese doc with RAW body + user_version=1.
	if _, err := db.Exec(v1BaseSchema); err != nil {
		t.Fatal(err)
	}
	rawBody := "# 文档\n\n渲染管线处理图形输出"
	if _, err := db.Exec(`INSERT INTO rag_fts (collection, path, chunk, body) VALUES ('default', 'doc.md', 0, ?)`, rawBody); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Open with the current (v2) code → migrate() runs v1→v2, rebuilding rag_fts.
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open after v2 migration: %v", err)
	}
	defer store.Close()

	// user_version must be 2.
	var v int
	if err := store.db.QueryRow(`SELECT user_version FROM pragma_user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != ragSchemaVersion {
		t.Fatalf("post-migration user_version = %d, want %d", v, ragSchemaVersion)
	}

	// CJK substring search must now work (bigram body derived from raw body).
	results, err := store.Search("管线", "default", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("CJK substring '管线' should match after v2 migration (存量文档 bigram 重写)")
	}
	// The snippet must contain the ORIGINAL continuous text, not bigram spaces.
	if strings.Contains(results[0].Snippet, "渲染 染管") || strings.Contains(results[0].Snippet, "染管 管线") {
		t.Errorf("snippet has bigram spaces after migration: %q", results[0].Snippet)
	}
	if !strings.Contains(results[0].Snippet, "渲染管线") {
		t.Errorf("snippet lost original continuous text: %q", results[0].Snippet)
	}
}
