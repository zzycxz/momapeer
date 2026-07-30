package rag

// entities.go implements the structured-knowledge layer that sits alongside the
// FTS5 full-text index. When a document is "deep-extracted" (via the extract
// Pipeline), each chunk's entities + relations are upserted here. rag_search
// then merges FTS5 hits (original text) with structured hits (entities/relations),
// giving the agent both precise facts AND quotable source text.
//
// Merge strategy is SIMPLE (no LLM disambiguation): the entity key is the
// normalized name (lowercased + trimmed). Two chunks that both extract "张三"
// produce ONE entity row with merged sources + the longer description. Synonym
// entities ("Apple Inc." vs "苹果公司") are NOT merged here — that's a future
// LLM-merge enhancement. The trade-off is fully predictable extraction cost.

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entity is one extracted entity (person/org/project/...). Name is the
// normalized key; NameRaw preserves the original surface form for display.
type Entity struct {
	ID          int64
	Collection  string
	Name        string // normalized key (lower+trim)
	NameRaw     string // display form
	Type        string
	Description string
	Sources     []Source
	RelationCnt int // number of relations touching this entity (0 if unknown)
	Community   int // Louvain community ID (-1 = unassigned)
}

// Relation is one directed edge between two entity names.
type Relation struct {
	ID          int64
	Collection  string
	Source      string // normalized entity name
	Target      string // normalized entity name
	Type        string
	Description string
	Sources     []Source
	Weight      float64 // co-occurrence frequency (incremented per source chunk)
	Strength    float64 // LLM-assigned semantic strength 1-10 (5=neutral)
}

// Source records where an entity/relation came from (for provenance/溯源).
type Source struct {
	Path  string `json:"path"`
	Chunk int    `json:"chunk"`
}

// JobStatus constants for rag_jobs.status.
const (
	JobPending    = "pending"    // queued, not yet extracting
	JobExtracting = "extracting" // worker is processing chunks
	JobDone       = "done"       // all chunks processed (some may have errored)
	JobError      = "error"      // fatal: could not even start / all chunks failed
	JobCancelled  = "cancelled"  // user cancelled
)

// ChunkStatus constants for rag_chunks.status.
const (
	ChunkPending = "pending"
	ChunkRunning = "running"
	ChunkDone    = "done"
	ChunkError   = "error"
)

// normalizeName is the SIMPLE merge key: lowercase + trim. Two entities with
// the same normalized name are treated as the same entity.
func normalizeName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// UpsertEntity inserts a new entity or merges into an existing one. Merging:
//   - append the new Source to Sources (dedup by path+chunk)
//   - keep the longer non-empty Description (later chunks may refine it)
//   - keep the first non-empty Type (stable classification)
func (s *Store) UpsertEntity(collection string, e Entity, src Source) error {
	collection = normalizeCollection(collection)
	name := normalizeName(e.NameRaw)
	if name == "" {
		return nil // skip empty entities (LLM noise)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var existingID int64
	var existingRaw, existingType, existingDesc, existingSourcesJSON string
	err := s.db.QueryRow(`SELECT id, name_raw, COALESCE(type,''), COALESCE(description,''), COALESCE(sources,'') FROM rag_entities WHERE collection = ? AND name = ?`,
		collection, name).Scan(&existingID, &existingRaw, &existingType, &existingDesc, &existingSourcesJSON)

	switch {
	case err == sql.ErrNoRows:
		// Insert new.
		sources := []Source{src}
		sj, _ := json.Marshal(sources)
		_, err := s.db.Exec(`INSERT INTO rag_entities (collection, name, name_raw, type, description, sources) VALUES (?, ?, ?, ?, ?, ?)`,
			collection, name, e.NameRaw, e.Type, e.Description, string(sj))
		return err
	case err != nil:
		return fmt.Errorf("query entity: %w", err)
	}

	// Merge into existing.
	var existingSources []Source
	if existingSourcesJSON != "" {
		if err := json.Unmarshal([]byte(existingSourcesJSON), &existingSources); err != nil {
			// Corrupted JSON — log and start fresh with just this source.
			existingSources = nil
		}
	}
	merged := mergeSources(existingSources, src)
	typeVal := existingType
	if typeVal == "" {
		typeVal = e.Type
	}
	descVal := existingDesc
	if len(e.Description) > len(descVal) {
		descVal = e.Description
	}
	rawVal := existingRaw
	if rawVal == "" {
		rawVal = e.NameRaw
	}
	sj, _ := json.Marshal(merged)
	_, err = s.db.Exec(`UPDATE rag_entities SET name_raw = ?, type = ?, description = ?, sources = ? WHERE id = ?`,
		rawVal, typeVal, descVal, string(sj), existingID)
	return err
}

// UpsertRelation inserts a new relation or merges into an existing one. The
// unique key is (collection, normalized source, normalized target, type).
func (s *Store) UpsertRelation(collection string, r Relation, src Source) error {
	collection = normalizeCollection(collection)
	srcName := normalizeName(r.Source)
	tgtName := normalizeName(r.Target)
	if srcName == "" || tgtName == "" || srcName == tgtName {
		return nil // skip empty or self-loop relations
	}
	r.Type = strings.TrimSpace(r.Type)
	if r.Type == "" {
		r.Type = "related_to"
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var existingID int64
	var existingDesc, existingSourcesJSON string
	err := s.db.QueryRow(`SELECT id, COALESCE(description,''), COALESCE(sources,'') FROM rag_relations WHERE collection = ? AND source = ? AND target = ? AND type = ?`,
		collection, srcName, tgtName, r.Type).Scan(&existingID, &existingDesc, &existingSourcesJSON)

	switch {
	case err == sql.ErrNoRows:
		sources := []Source{src}
		sj, _ := json.Marshal(sources)
		strength := r.Strength
		if strength < 1 || strength > 10 {
			strength = 5 // clamp to neutral for invalid/missing
		}
		_, err := s.db.Exec(`INSERT INTO rag_relations (collection, source, target, type, description, sources, weight, strength) VALUES (?, ?, ?, ?, ?, ?, 1.0, ?)`,
			collection, srcName, tgtName, r.Type, r.Description, string(sj), strength)
		if err != nil {
			return err
		}
		// Maintain the relation_cnt denormalized column: increment both endpoints.
		_, _ = s.db.Exec(`UPDATE rag_entities SET relation_cnt = relation_cnt + 1 WHERE collection = ? AND name = ?`, collection, srcName)
		_, _ = s.db.Exec(`UPDATE rag_entities SET relation_cnt = relation_cnt + 1 WHERE collection = ? AND name = ?`, collection, tgtName)
		return nil
	case err != nil:
		return fmt.Errorf("query relation: %w", err)
	}

	var existingSources []Source
	if existingSourcesJSON != "" {
		if err := json.Unmarshal([]byte(existingSourcesJSON), &existingSources); err != nil {
			existingSources = nil
		}
	}
	merged := mergeSources(existingSources, src)
	descVal := existingDesc
	if len(r.Description) > len(descVal) {
		descVal = r.Description
	}
	sj, _ := json.Marshal(merged)
	// Weight = number of distinct source chunks that extracted this edge.
	// Strength = running average of LLM-assigned scores (merge new into existing).
	newStrength := r.Strength
	if newStrength < 1 || newStrength > 10 {
		newStrength = 5
	}
	_, err = s.db.Exec(`UPDATE rag_relations SET description = ?, sources = ?, weight = ?, strength = (strength * ? + ?) / (? + 1) WHERE id = ?`,
		descVal, string(sj), float64(len(merged)),
		float64(len(merged)-1), newStrength, float64(len(merged)), existingID)
	return err
}

// mergeSources appends src to existing if not already present (dedup by
// path+chunk). Returns a new slice; does not mutate existing.
func mergeSources(existing []Source, src Source) []Source {
	for _, e := range existing {
		if e.Path == src.Path && e.Chunk == src.Chunk {
			return existing
		}
	}
	out := make([]Source, 0, len(existing)+1)
	out = append(out, existing...)
	out = append(out, src)
	return out
}

// DeleteCollectionEntities removes all entities and relations for a collection.
// Used before re-extracting with a different template. Wrapped in a transaction
// so a crash mid-delete doesn't leave dangling edges.
func (s *Store) DeleteCollectionEntities(collection string) error {
	collection = normalizeCollection(collection)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM rag_entities WHERE collection = ?`, collection); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM rag_relations WHERE collection = ?`, collection); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidateVecCache()
	return nil
}

// PruneDanglingRelations deletes relation rows whose source or target entity
// no longer exists in rag_entities. The relations table has no FK constraint
// (source/target are name strings, not ID references), so dangling edges can
// accumulate from cross-chunk extraction or HE-side mutations. Returns the
// number of deleted rows. When collection is empty, prunes all collections.
func (s *Store) PruneDanglingRelations(collection string) (int, error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`
		DELETE FROM rag_relations
		WHERE (? = '' OR collection = ?)
		  AND (
		    source NOT IN (SELECT name FROM rag_entities WHERE (? = '' OR collection = ?))
		    OR target NOT IN (SELECT name FROM rag_entities WHERE (? = '' OR collection = ?))
		  )`,
		collection, collection, collection, collection, collection, collection)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		s.invalidateVecCache()
	}
	return int(n), nil
}

// CountEntities returns the number of entities in a collection.
func (s *Store) CountEntities(collection string) (int, error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM rag_entities WHERE collection = ?`, collection).Scan(&n)
	return n, err
}

// CountRelations returns the number of relations in a collection.
func (s *Store) CountRelations(collection string) (int, error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM rag_relations WHERE collection = ?`, collection).Scan(&n)
	return n, err
}

// EntityWithRelCount is an Entity augmented with its relation count.
type EntityWithRelCount struct {
	Entity
	RelationCount int
}

// TopEntities returns the N entities with the most relations in a collection.
func (s *Store) TopEntities(collection string, limit int) ([]EntityWithRelCount, error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
		SELECT e.name, e.name_raw, e.type, e.description, e.sources,
			   (SELECT COUNT(*) FROM rag_relations r WHERE r.collection = e.collection AND (r.source = e.name OR r.target = e.name)) AS rel_count
		FROM rag_entities e
		WHERE e.collection = ?
		ORDER BY rel_count DESC
		LIMIT ?`, collection, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntityWithRelCount
	for rows.Next() {
		var e EntityWithRelCount
		var srcJSON string
		if err := rows.Scan(&e.Name, &e.NameRaw, &e.Type, &e.Description, &srcJSON, &e.RelationCount); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(srcJSON), &e.Sources)
		out = append(out, e)
	}
	return out, rows.Err()
}

// GraphBatch returns entities + their relations + relation counts in a single
// query pass (one LEFT JOIN), eliminating the N+1 pattern where GetGraphData
// called RelationsOf once per entity. Designed for large knowledge bases
// (thousands to tens of thousands of entities).
//
// The result is a flat row stream: each entity may appear multiple times (once
// per relation). The caller groups rows by entity name in memory. This is far
// cheaper than N separate RelationsOf queries, each of which acquires the store
// mutex and runs an independent SQL.
//
// When limit > 0, entities are ordered by relation_cnt DESC (highest-connected
// "hub" entities first) so the caller gets the most visually important nodes
// upfront — ideal for paginated / progressive graph rendering.
func (s *Store) GraphBatch(collection string, limit int) ([]Entity, map[string][]Relation, error) {
	collection = normalizeCollection(collection)
	if limit <= 0 {
		limit = 10000
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// One JOIN: entities left-joined to their outgoing relations. We fetch
	// outgoing (source = name) only here; the relation_cnt column (maintained on
	// every UpsertRelation/Delete) captures the full degree including incoming,
	// and the caller can fetch incoming relations for specific entities via
	// RelationsOf if needed (e.g. EntityDetail). For graph visualization,
	// outgoing edges suffice because every edge is stored once as source→target.
	// CRITICAL: LIMIT must apply to ENTITIES (before JOIN), not to JOIN rows.
	// If we LIMIT after the LEFT JOIN, a hub entity with 190 relations consumes
	// 190 of the LIMIT budget alone — so LIMIT 200 returns only ~2 entities.
	// Fix: use a CTE to select the top-N entities FIRST, then JOIN relations.
	rows, err := s.db.Query(`
		WITH top_entities AS (
			SELECT id, collection, name, name_raw, type, description, sources, relation_cnt, community
			FROM rag_entities
			WHERE (? = '' OR collection = ?)
			ORDER BY relation_cnt DESC, id
			LIMIT ?
		)
		SELECT e.id, e.collection, e.name, e.name_raw,
		       COALESCE(e.type,''), COALESCE(e.description,''), COALESCE(e.sources,''),
		       COALESCE(e.relation_cnt,0), COALESCE(e.community,-1),
		       COALESCE(r.source,''), COALESCE(r.target,''), COALESCE(r.type,''), COALESCE(r.description,''),
		       COALESCE(r.weight,1.0), COALESCE(r.strength,5.0)
		FROM top_entities e
		LEFT JOIN rag_relations r ON r.collection = e.collection AND r.source = e.name
		ORDER BY e.relation_cnt DESC, e.id`, collection, collection, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var entities []Entity
	relMap := make(map[string][]Relation)
	seen := make(map[string]bool) // dedup entities (LEFT JOIN repeats entity rows per relation)

	for rows.Next() {
		var id int64
		var coll, name, nameRaw, typ, desc, srcJSON string
		var relCnt, community int
		var rSrc, rTgt, rType, rDesc string
		var rWeight, rStrength float64
		if err := rows.Scan(&id, &coll, &name, &nameRaw, &typ, &desc, &srcJSON, &relCnt, &community, &rSrc, &rTgt, &rType, &rDesc, &rWeight, &rStrength); err != nil {
			continue
		}
		if !seen[name] {
			seen[name] = true
			var sources []Source
			if srcJSON != "" {
				_ = json.Unmarshal([]byte(srcJSON), &sources)
			}
			entities = append(entities, Entity{
				ID: id, Collection: coll, Name: name, NameRaw: nameRaw,
				Type: typ, Description: desc, Sources: sources, RelationCnt: relCnt,
				Community: community,
			})
		}
		// Collect the relation (if the JOIN matched — rSrc non-empty means a row existed).
		if rSrc != "" {
			relMap[name] = append(relMap[name], Relation{
				Source: rSrc, Target: rTgt, Type: rType, Description: rDesc, Weight: rWeight, Strength: rStrength,
			})
		}
	}
	return entities, relMap, rows.Err()
}

// RecalcRelationCounts recomputes the relation_cnt column for all entities in a
// collection (or all collections when empty). Called by the v3 migration to
// backfill the column for pre-existing data, and after bulk deletes.
func (s *Store) RecalcRelationCounts(collection string) error {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		UPDATE rag_entities SET relation_cnt = (
			SELECT COUNT(*) FROM rag_relations r
			WHERE r.collection = rag_entities.collection
			  AND (r.source = rag_entities.name OR r.target = rag_entities.name)
		) WHERE (? = '' OR collection = ?)`, collection, collection)
	return err
}

func (s *Store) TopRelations(collection string, limit int) ([]Relation, error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
		SELECT source, target, type, description, sources
		FROM rag_relations
		WHERE collection = ?
		ORDER BY id DESC
		LIMIT ?`, collection, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Relation
	for rows.Next() {
		var r Relation
		var srcJSON string
		if err := rows.Scan(&r.Source, &r.Target, &r.Type, &r.Description, &srcJSON); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(srcJSON), &r.Sources)
		out = append(out, r)
	}
	return out, rows.Err()
}

// dedupSources removes duplicate sources (same path+chunk) from a slice.
func dedupSources(srcs []Source) []Source {
	seen := make(map[string]bool, len(srcs))
	out := make([]Source, 0, len(srcs))
	for _, s := range srcs {
		key := fmt.Sprintf("%s:%d", s.Path, s.Chunk)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

// SearchEntities returns entities whose normalized name contains the query
// (substring match, case-insensitive) OR whose description contains it. This
// is a lightweight lexical match over the (small) extracted set — for large
// entity counts an FTS5 mirror table would be better, but office collections
// rarely exceed thousands of entities. Limited to `limit` results.
func (s *Store) SearchEntities(query, collection string, limit int) ([]Entity, error) {
	collection = normalizeCollection(collection)
	if limit <= 0 {
		limit = 5
	}
	like := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, collection, name, name_raw, COALESCE(type,''), COALESCE(description,''), COALESCE(sources,''), COALESCE(relation_cnt,0), COALESCE(community,-1)
		FROM rag_entities
		WHERE (? = '' OR collection = ?) AND (name LIKE ? OR description LIKE ? OR name_raw LIKE ?)
		ORDER BY length(description) DESC LIMIT ?`,
		collection, collection, like, like, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntities(rows)
}

// RelationsOf returns all relations touching entityName — outgoing (source),
// and when includeInverse, also incoming (target). entityName is normalized.
func (s *Store) RelationsOf(collection, entityName string, includeInverse bool) ([]Relation, error) {
	collection = normalizeCollection(collection)
	name := normalizeName(entityName)
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows *sql.Rows
	var err error
	if includeInverse {
		rows, err = s.db.Query(`SELECT id, collection, source, target, COALESCE(type,''), COALESCE(description,''), COALESCE(sources,'')
			FROM rag_relations WHERE collection = ? AND (source = ? OR target = ?)`,
			collection, name, name)
	} else {
		rows, err = s.db.Query(`SELECT id, collection, source, target, COALESCE(type,''), COALESCE(description,''), COALESCE(sources,'')
			FROM rag_relations WHERE collection = ? AND source = ?`,
			collection, name)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRelations(rows)
}

// EntityCount returns the number of extracted entities in a collection (or all
// when collection is empty). Used by the UI badge "✅ 已抽取 N 实体".
func (s *Store) EntityCount(collection string) (int, error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int
	var err error
	if collection == "" {
		err = s.db.QueryRow(`SELECT count(*) FROM rag_entities`).Scan(&count)
	} else {
		err = s.db.QueryRow(`SELECT count(*) FROM rag_entities WHERE collection = ?`, collection).Scan(&count)
	}
	return count, err
}

// HasEntities reports whether a collection has any extracted entities (i.e. deep
// extraction has been run). Used to decide whether rag_search should query the
// structured layer at all.
func (s *Store) HasEntities(collection string) (bool, error) {
	n, err := s.EntityCount(collection)
	return n > 0, err
}

func scanEntities(rows *sql.Rows) ([]Entity, error) {
	var out []Entity
	for rows.Next() {
		var e Entity
		var sourcesJSON string
		if err := rows.Scan(&e.ID, &e.Collection, &e.Name, &e.NameRaw, &e.Type, &e.Description, &sourcesJSON, &e.RelationCnt, &e.Community); err != nil {
			continue
		}
		if sourcesJSON != "" {
			_ = json.Unmarshal([]byte(sourcesJSON), &e.Sources)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanRelations(rows *sql.Rows) ([]Relation, error) {
	var out []Relation
	for rows.Next() {
		var r Relation
		var sourcesJSON string
		if err := rows.Scan(&r.ID, &r.Collection, &r.Source, &r.Target, &r.Type, &r.Description, &sourcesJSON); err != nil {
			continue
		}
		if sourcesJSON != "" {
			_ = json.Unmarshal([]byte(sourcesJSON), &r.Sources)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- Job/chunk state (for the extract Pipeline + UI progress) ---------------

// JobRow is one rag_jobs row, for the pipeline + UI.
type JobRow struct {
	ID          string
	Collection  string
	Path        string
	RelPath     string
	RootPath    string
	IsDir       bool
	Status      string
	TotalChunks int
	DoneChunks  int
	ErrorMsg    string
	ContentHash string // sha256 of chunked body; used for change-based dedup
	StatKey     string // "size:mtime" of the source file; cheap re-import dedup
	NodePrompt  string // persisted so Resume restores the original prompt
	EdgePrompt  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreateJob inserts a new extraction job + its pending chunks. Returns the job
// id. Chunks is the list of chunk texts (the pipeline reads them back by id
// when processing, so we store the text on the chunk row to keep the pipeline
// stateless across restarts).
func (s *Store) CreateJob(j JobRow, chunkTexts []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	nowTS := time.Now().UTC()
	now := nowTS.Format(time.RFC3339)
	if j.ID == "" {
		j.ID = fmt.Sprintf("job_%d", nowTS.UnixNano())
	}
	if _, err := tx.Exec(`INSERT INTO rag_jobs (id, collection, path, rel_path, root_path, is_dir, status, total_chunks, done_chunks, error_msg, content_hash, stat_key, node_prompt, edge_prompt, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(collection, path) DO UPDATE SET
			id=excluded.id,
			status=excluded.status,
			total_chunks=excluded.total_chunks,
			done_chunks=0,
			error_msg=NULL,
			content_hash=excluded.content_hash,
			stat_key=excluded.stat_key,
			node_prompt=excluded.node_prompt,
			edge_prompt=excluded.edge_prompt,
			updated_at=excluded.updated_at`,
		j.ID, normalizeCollection(j.Collection), j.Path, j.RelPath, j.RootPath, boolToInt(j.IsDir), j.Status, len(chunkTexts), j.ErrorMsg, j.ContentHash, j.StatKey, j.NodePrompt, j.EdgePrompt, now, now); err != nil {
		return "", err
	}
	// Reset chunks for this job (delete-then-insert, in case of re-extract).
	// Use collection+path subquery to clean up chunks from the old job ID too.
	if _, err := tx.Exec(`DELETE FROM rag_chunks WHERE job_id IN (SELECT id FROM rag_jobs WHERE collection = ? AND path = ?)`,
		normalizeCollection(j.Collection), j.Path); err != nil {
		return "", err
	}
	for i, text := range chunkTexts {
		cid := fmt.Sprintf("%s_c%d", j.ID, i)
		if _, err := tx.Exec(`INSERT INTO rag_chunks (id, job_id, idx, status, attempts, latency_ms, error_msg) VALUES (?, ?, ?, ?, 0, NULL, NULL)`,
			cid, j.ID, i, ChunkPending); err != nil {
			return "", err
		}
		// Store the chunk text in a scratch column? We don't have one — the
		// pipeline carries chunk text in-memory via the task struct. On restart,
		// pending chunks are re-read from FTS5 (by path + idx). This keeps the
		// schema clean. (See Pipeline.Resume for the rehydrate path.)
		_ = text // text is carried in-memory; not persisted on the chunk row
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return j.ID, nil
}

// MarkChunkDone updates a chunk's status + the job's done_chunks counter. When
// all chunks are processed, the job status flips to done (or error if all
// failed). err != nil marks the chunk as errored with the message.
func (s *Store) MarkChunkDone(chunkID string, jobID string, latencyMs int64, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err2 := s.db.Begin()
	if err2 != nil {
		return err2
	}
	defer func() { _ = tx.Rollback() }()
	status := ChunkDone
	errMsg := ""
	if err != nil {
		status = ChunkError
		errMsg = err.Error()
	}
	// Skip if this chunk was already marked done/error (idempotent guard
	// against double-counting from Resume or retry edge cases).
	var prevStatus string
	if e := tx.QueryRow(`SELECT status FROM rag_chunks WHERE id = ?`, chunkID).Scan(&prevStatus); e != nil {
		return e
	}
	if prevStatus == ChunkDone || prevStatus == ChunkError {
		return tx.Commit() // already processed — don't double-count
	}
	if _, e := tx.Exec(`UPDATE rag_chunks SET status = ?, latency_ms = ?, error_msg = ?, attempts = attempts + 1 WHERE id = ?`,
		status, latencyMs, errMsg, chunkID); e != nil {
		return e
	}
	// Count actual done/error chunks rather than incrementing a counter —
	// avoids overflow when done_chunks gets out of sync.
	var doneCount int
	if e := tx.QueryRow(`SELECT COUNT(*) FROM rag_chunks WHERE job_id = ? AND status IN (?, ?)`,
		jobID, ChunkDone, ChunkError).Scan(&doneCount); e != nil {
		return e
	}
	if _, e := tx.Exec(`UPDATE rag_jobs SET done_chunks = ?, updated_at = ? WHERE id = ?`,
		doneCount, time.Now().UTC().Format(time.RFC3339), jobID); e != nil {
		return e
	}
	// If all chunks done, flip job status.
	var done, total int
	var failedCount int
	if e := tx.QueryRow(`SELECT done_chunks, total_chunks, (SELECT count(*) FROM rag_chunks WHERE job_id = ? AND status = ?) FROM rag_jobs WHERE id = ?`,
		jobID, ChunkError, jobID).Scan(&done, &total, &failedCount); e != nil {
		return e
	}
	if total > 0 && done >= total {
		finalStatus := JobDone
		if failedCount == total {
			finalStatus = JobError
		}
		if _, e := tx.Exec(`UPDATE rag_jobs SET status = ?, updated_at = ? WHERE id = ?`,
			finalStatus, time.Now().UTC().Format(time.RFC3339), jobID); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// SetJobStatus sets a job's status (used for extracting/cancelled transitions).
func (s *Store) SetJobStatus(jobID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE rag_jobs SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC().Format(time.RFC3339), jobID)
	return err
}

// JobByID returns one job row. ok=false if not found.
func (s *Store) JobByID(jobID string) (JobRow, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var j JobRow
	var isDir int
	var created, updated string
	err := s.db.QueryRow(`SELECT id, collection, path, COALESCE(rel_path,''), COALESCE(root_path,''), is_dir, status, total_chunks, done_chunks, COALESCE(error_msg,''), COALESCE(created_at,''), COALESCE(updated_at,'') FROM rag_jobs WHERE id = ?`,
		jobID).Scan(&j.ID, &j.Collection, &j.Path, &j.RelPath, &j.RootPath, &isDir, &j.Status, &j.TotalChunks, &j.DoneChunks, &j.ErrorMsg, &created, &updated)
	if err == sql.ErrNoRows {
		return JobRow{}, false, nil
	}
	if err != nil {
		return JobRow{}, false, err
	}
	j.IsDir = isDir != 0
	j.Collection = normalizeCollection(j.Collection)
	j.CreatedAt, _ = time.Parse(time.RFC3339, created)
	j.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return j, true, nil
}

// JobsByPath returns all jobs for a given collection+path (usually 1, but a
// re-extract creates a new job row via ON CONFLICT replace).
func (s *Store) JobsByPath(collection, path string) ([]JobRow, error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, collection, path, COALESCE(rel_path,''), COALESCE(root_path,''), is_dir, status, total_chunks, done_chunks, COALESCE(error_msg,''), COALESCE(created_at,''), COALESCE(updated_at,'') FROM rag_jobs WHERE collection = ? AND path = ? ORDER BY updated_at DESC`,
		collection, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanJobs(rows)
}

// AllJobs returns every job (for the file-tree status view).
func (s *Store) AllJobs() ([]JobRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, collection, path, COALESCE(rel_path,''), COALESCE(root_path,''), is_dir, status, total_chunks, done_chunks, COALESCE(error_msg,''), COALESCE(content_hash,''), COALESCE(stat_key,''), COALESCE(node_prompt,''), COALESCE(edge_prompt,''), COALESCE(created_at,''), COALESCE(updated_at,'') FROM rag_jobs ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanJobs(rows)
}

// PendingChunksForJob returns the (chunkID, idx) pairs still pending/errored
// for a job — used by Pipeline.Resume to rehydrate the queue after a restart.
// The chunk TEXT is re-read from FTS5 by (path, idx).
func (s *Store) PendingChunksForJob(jobID string) ([]struct {
	ChunkID string
	Idx     int
}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, idx FROM rag_chunks WHERE job_id = ? AND status IN (?, ?) ORDER BY idx`,
		jobID, ChunkPending, ChunkError)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		ChunkID string
		Idx     int
	}
	for rows.Next() {
		var r struct {
			ChunkID string
			Idx     int
		}
		if err := rows.Scan(&r.ChunkID, &r.Idx); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AvgChunkLatencyMs returns the mean latency_ms across all done chunks in a
// collection (or all collections when empty). Used as a fallback ETA estimate
// when the in-memory sliding window is empty (e.g. right after restart).
func (s *Store) AvgChunkLatencyMs(collection string) (int64, error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	var avg sql.NullInt64
	var err error
	if collection == "" {
		err = s.db.QueryRow(`SELECT AVG(latency_ms) FROM rag_chunks WHERE status = ? AND latency_ms IS NOT NULL`, ChunkDone).Scan(&avg)
	} else {
		err = s.db.QueryRow(`SELECT AVG(c.latency_ms) FROM rag_chunks c JOIN rag_jobs j ON c.job_id = j.id WHERE c.status = ? AND c.latency_ms IS NOT NULL AND j.collection = ?`, ChunkDone, collection).Scan(&avg)
	}
	if err != nil || !avg.Valid {
		return 0, err
	}
	return avg.Int64, nil
}

// JobStatusForPath returns (jobID, status, totalChunks, doneChunks) for the job
// matching a collection+path, or ("", "", 0, 0) if none exists. Used by the
// pipeline's re-import dedup check: if a job is already done with the same
// chunk count, re-extraction is skipped to avoid burning LLM quota on unchanged
// files.
func (s *Store) JobStatusForPath(collection, path string) (jobID, status string, totalChunks, doneChunks int, err error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.db.QueryRow(
		`SELECT id, status, total_chunks, done_chunks FROM rag_jobs WHERE collection = ? AND path = ?`,
		collection, path,
	).Scan(&jobID, &status, &totalChunks, &doneChunks)
	if err == sql.ErrNoRows {
		return "", "", 0, 0, nil
	}
	return jobID, status, totalChunks, doneChunks, err
}

// JobContentHashForPath returns the stored content_hash for the job matching a
// collection+path (empty if no job or no hash). Used by the dedup check to
// detect content changes even when the chunk count is unchanged.
func (s *Store) JobContentHashForPath(collection, path string) (string, error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	var h sql.NullString
	err := s.db.QueryRow(`SELECT content_hash FROM rag_jobs WHERE collection = ? AND path = ?`, collection, path).Scan(&h)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return h.String, err
}

// JobStatKeyForPath returns the stored stat_key ("size:mtime") for the job
// matching a collection+path (empty if no job or no key). Used by the cheap
// re-import dedup: if the file's on-disk size+mtime are unchanged, the body is
// guaranteed identical, so we skip the expensive readDoc (markitdown/OCR) and
// re-extraction entirely. This is the first-line dedup; content_hash is the
// second-line (catches edits that don't change the stat key).
func (s *Store) JobStatKeyForPath(collection, path string) (string, error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	var h sql.NullString
	err := s.db.QueryRow(`SELECT stat_key FROM rag_jobs WHERE collection = ? AND path = ?`, collection, path).Scan(&h)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return h.String, err
}

// ChunksByPath returns the ordered list of chunk bodies for a given
// collection+path by reading from FTS5. Used by Pipeline.Resume to rehydrate
// chunk text that is not persisted on rag_chunks (only status is).
func (s *Store) ChunksByPath(collection, path string) ([]string, error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Read body_raw (the ORIGINAL chunk text), not body (which is bigram-
	// expanded for indexing). Resume feeds this text to the LLM extractor, so
	// it must be the un-expanded text or Chinese extraction quality degrades.
	rows, err := s.db.Query(
		`SELECT body_raw FROM rag_fts WHERE collection = ? AND path = ? ORDER BY chunk`,
		collection, path,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		out = append(out, body)
	}
	return out, rows.Err()
}

// ResumableJobs returns all jobs whose status is pending or extracting (i.e.
// interrupted mid-extraction). Used by Pipeline.Resume at startup to rebuild
// the in-memory queue from durable state.
func (s *Store) ResumableJobs() ([]JobRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, collection, path, COALESCE(rel_path,''), COALESCE(root_path,''), is_dir, status, total_chunks, done_chunks, COALESCE(error_msg,''), COALESCE(content_hash,''), COALESCE(stat_key,''), COALESCE(node_prompt,''), COALESCE(edge_prompt,''), COALESCE(created_at,''), COALESCE(updated_at,'') FROM rag_jobs WHERE status IN (?, ?)`, JobPending, JobExtracting)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanJobs(rows)
}

func scanJobs(rows *sql.Rows) ([]JobRow, error) {
	var out []JobRow
	for rows.Next() {
		var j JobRow
		var isDir int
		var created, updated string
		var contentHash, statKey, nodePrompt, edgePrompt sql.NullString
		// content_hash/stat_key/node_prompt/edge_prompt may be NULL on older
		// rows; COALESCE in the queries handles it, but scan into NullString
		// for safety.
		if err := rows.Scan(&j.ID, &j.Collection, &j.Path, &j.RelPath, &j.RootPath, &isDir, &j.Status, &j.TotalChunks, &j.DoneChunks, &j.ErrorMsg, &contentHash, &statKey, &nodePrompt, &edgePrompt, &created, &updated); err != nil {
			continue
		}
		j.IsDir = isDir != 0
		j.ContentHash = contentHash.String
		j.StatKey = statKey.String
		j.NodePrompt = nodePrompt.String
		j.EdgePrompt = edgePrompt.String
		j.Collection = normalizeCollection(j.Collection)
		j.CreatedAt, _ = time.Parse(time.RFC3339, created)
		j.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, j)
	}
	return out, rows.Err()
}

// UpdateEntity patches an entity's display fields (name_raw, type, description).
// The normalized name key is immutable; to change it, merge into a different entity.
func (s *Store) UpdateEntity(collection, name string, nameRaw, typ, desc string) error {
	collection = normalizeCollection(collection)
	name = normalizeName(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE rag_entities SET name_raw = COALESCE(NULLIF(?, ''), name_raw), type = COALESCE(NULLIF(?, ''), type), description = COALESCE(NULLIF(?, ''), description) WHERE collection = ? AND name = ?`,
		nameRaw, typ, desc, collection, name)
	if err == nil {
		// Entity metadata is cached in vecMeta; invalidate so next search reloads.
		s.invalidateVecCache()
	}
	return err
}

// MergeEntities merges mergeNames into keepName within a collection. All
// relations referencing any merged entity are rewired to keepName, then the
// merged entity rows are deleted. Sources are concatenated (deduped).
func (s *Store) MergeEntities(collection, keepName string, mergeNames []string) error {
	collection = normalizeCollection(collection)
	keepName = normalizeName(keepName)
	if keepName == "" || len(mergeNames) == 0 {
		return fmt.Errorf("keepName and mergeNames required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Collect all sources from merged entities to append to keep entity.
	var keepSources []Source
	var keepRaw, keepType, keepDesc, keepSourcesJSON string
	err = tx.QueryRow(`SELECT name_raw, COALESCE(type,''), COALESCE(description,''), COALESCE(sources,'') FROM rag_entities WHERE collection = ? AND name = ?`,
		collection, keepName).Scan(&keepRaw, &keepType, &keepDesc, &keepSourcesJSON)
	if err == sql.ErrNoRows {
		return fmt.Errorf("keep entity %q not found", keepName)
	}
	if err != nil {
		return err
	}
	if keepSourcesJSON != "" {
		_ = json.Unmarshal([]byte(keepSourcesJSON), &keepSources)
	}

	for _, mn := range mergeNames {
		mnNorm := normalizeName(mn)
		if mnNorm == "" || mnNorm == keepName {
			continue
		}
		// Read merged entity's sources.
		var sourcesJSON string
		var mRaw, mType, mDesc string
		err := tx.QueryRow(`SELECT name_raw, COALESCE(type,''), COALESCE(description,''), COALESCE(sources,'') FROM rag_entities WHERE collection = ? AND name = ?`,
			collection, mnNorm).Scan(&mRaw, &mType, &mDesc, &sourcesJSON)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		var mSources []Source
		if sourcesJSON != "" {
			_ = json.Unmarshal([]byte(sourcesJSON), &mSources)
		}
		keepSources = append(keepSources, mSources...)
		// Keep longer description.
		if len(mDesc) > len(keepDesc) {
			keepDesc = mDesc
		}
		// Keep first non-empty type.
		if keepType == "" && mType != "" {
			keepType = mType
		}

		// Rewire relations: source side.
		if _, err := tx.Exec(`UPDATE rag_relations SET source = ? WHERE collection = ? AND source = ?`,
			keepName, collection, mnNorm); err != nil {
			return err
		}
		// Rewire relations: target side.
		if _, err := tx.Exec(`UPDATE rag_relations SET target = ? WHERE collection = ? AND target = ?`,
			keepName, collection, mnNorm); err != nil {
			return err
		}
		// Delete self-loops created by merge (source == target == keepName after rewrite).
		if _, err := tx.Exec(`DELETE FROM rag_relations WHERE collection = ? AND source = ? AND target = ?`,
			collection, keepName, keepName); err != nil {
			return err
		}
		// Delete merged entity.
		if _, err := tx.Exec(`DELETE FROM rag_entities WHERE collection = ? AND name = ?`,
			collection, mnNorm); err != nil {
			return err
		}
	}

	// Update keep entity with merged sources + better description/type.
	merged := dedupSources(keepSources)
	sj, _ := json.Marshal(merged)
	if _, err := tx.Exec(`UPDATE rag_entities SET name_raw = ?, type = ?, description = ?, sources = ? WHERE collection = ? AND name = ?`,
		keepRaw, keepType, keepDesc, string(sj), collection, keepName); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidateVecCache()
	return nil
}

// EntityRelationView is a relation with direction info relative to an entity.
type EntityRelationView struct {
	Direction   string  `json:"direction"` // "out" | "in"
	Peer        string  `json:"peer"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Weight      float64 `json:"weight"`
	Strength    float64 `json:"strength"`
}

// RelationsOfEntity returns all relations for an entity with direction info.
func (s *Store) RelationsOfEntity(collection, entityName string) ([]EntityRelationView, error) {
	collection = normalizeCollection(collection)
	name := normalizeName(entityName)
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT source, target, COALESCE(type,''), COALESCE(description,''),
		COALESCE(weight,1.0), COALESCE(strength,5.0)
		FROM rag_relations WHERE collection = ? AND (source = ? OR target = ?)`,
		collection, name, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EntityRelationView
	for rows.Next() {
		var src, tgt, typ, desc string
		var weight, strength float64
		if err := rows.Scan(&src, &tgt, &typ, &desc, &weight, &strength); err != nil {
			continue
		}
		dir := "out"
		peer := tgt
		if src != name {
			dir = "in"
			peer = src
		}
		out = append(out, EntityRelationView{
			Direction:   dir,
			Peer:        peer,
			Type:        typ,
			Description: desc,
			Weight:      weight,
			Strength:    strength,
		})
	}
	return out, rows.Err()
}

// UpsertEntityEmbedding stores or updates the embedding vector for an entity.
func (s *Store) UpsertEntityEmbedding(entityID int64, collection, model string, vec []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	blob := float32SliceToBytes(vec)
	_, err := s.db.Exec(`INSERT OR REPLACE INTO rag_entity_embeddings (entity_id, collection, model, vec) VALUES (?, ?, ?, ?)`,
		entityID, normalizeCollection(collection), model, blob)
	if err == nil {
		s.invalidateVecCache()
	}
	return err
}

// maxVectorScanEntities is the soft ceiling above which a warning is logged.
// With the in-memory parallel cache this is no longer a hard limit — we can
// scan 100K+ vectors in under 200ms. Kept only as a diagnostic threshold.
const maxVectorScanEntities = 100000

// ErrVectorScaleExceeded is retained for backward compatibility but is no
// longer returned — the parallel in-memory cache handles any scale.
var ErrVectorScaleExceeded = errors.New("semantic search unavailable: entity count exceeds brute-force limit, use keyword search")

// SearchEntitiesByVector finds the topK entities most similar to the query vector
// using parallel brute-force cosine similarity over an in-memory vector cache.
// The cache is lazily loaded on first call and invalidated on any entity/embedding
// mutation. Supports 100K+ entities with sub-200ms queries on multi-core CPUs.
func (s *Store) SearchEntitiesByVector(collection, model string, queryVec []float32, topK int) ([]Entity, error) {
	collection = normalizeCollection(collection)

	// Ensure cache is loaded and matches this collection+model.
	if err := s.ensureVecCache(collection, model); err != nil {
		return nil, err
	}

	s.vecCache.mu.RLock()
	vecs := s.vecCache.vecs
	dims := s.vecCache.dims
	metas := s.vecCache.metas
	s.vecCache.mu.RUnlock()

	if dims == 0 || len(metas) == 0 {
		return nil, nil
	}
	if len(queryVec) != dims {
		return nil, fmt.Errorf("query vector dim %d != stored dim %d", len(queryVec), dims)
	}

	nEntities := len(metas)
	if nEntities > maxVectorScanEntities {
		slog.Warn("semantic search scanning large vector set", "count", nEntities, "dims", dims)
	}

	// Parallel cosine: split entities across goroutines, each computes scores
	// for its range and collects local top-K, then merge.
	numWorkers := runtime.NumCPU()
	if numWorkers > nEntities {
		numWorkers = nEntities
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	type scored struct {
		idx   int
		score float32
	}

	chunkSize := (nEntities + numWorkers - 1) / numWorkers
	var wg sync.WaitGroup
	localResults := make([][]scored, numWorkers)

	for w := 0; w < numWorkers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if end > nEntities {
			end = nEntities
		}
		wg.Add(1)
		go func(start, end, wid int) {
			defer wg.Done()
			var local []scored
			for i := start; i < end; i++ {
				vec := vecs[i*dims : (i+1)*dims]
				sc := cosineSimilarity(queryVec, vec)
				local = append(local, scored{idx: i, score: sc})
			}
			localResults[wid] = local
		}(start, end, w)
	}
	wg.Wait()

	// Merge all local results and pick global top-K.
	all := make([]scored, 0, nEntities)
	for _, lr := range localResults {
		all = append(all, lr...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })
	if topK > 0 && len(all) > topK {
		all = all[:topK]
	}

	out := make([]Entity, 0, len(all))
	for _, r := range all {
		m := metas[r.idx]
		out = append(out, Entity{
			ID:          m.id,
			Collection:  m.coll,
			Name:        m.name,
			NameRaw:     m.nameRaw,
			Type:        m.typ,
			Description: m.desc,
			Sources:     m.sources,
		})
	}
	return out, nil
}

// ensureVecCache loads the vector cache from DB if stale or for a different
// collection/model. Thread-safe; callers should NOT hold s.mu when calling.
func (s *Store) ensureVecCache(collection, model string) error {
	s.vecCache.mu.RLock()
	needReload := !s.vecCache.loaded || s.vecCache.collection != collection || s.vecCache.model != model
	s.vecCache.mu.RUnlock()
	if !needReload {
		return nil
	}

	// Load from DB under s.mu (protects db access).
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT e.id, e.collection, e.name, e.name_raw, COALESCE(e.type,''), COALESCE(e.description,''), COALESCE(e.sources,''), ee.vec
		 FROM rag_entity_embeddings ee
		 JOIN rag_entities e ON e.id = ee.entity_id
		 WHERE ee.collection = ? AND ee.model = ?`,
		collection, model)
	if err != nil {
		return err
	}
	defer rows.Close()

	var metas []vecMeta
	var vecs []float32
	dims := 0
	for rows.Next() {
		var id int64
		var coll, name, nameRaw, typ, desc, sourcesJSON string
		var vecBlob []byte
		if err := rows.Scan(&id, &coll, &name, &nameRaw, &typ, &desc, &sourcesJSON, &vecBlob); err != nil {
			continue
		}
		vec := bytesToFloat32Slice(vecBlob)
		if dims == 0 {
			dims = len(vec)
		} else if len(vec) != dims {
			continue // skip mismatched-dimension vectors
		}
		var sources []Source
		if sourcesJSON != "" {
			_ = json.Unmarshal([]byte(sourcesJSON), &sources)
		}
		metas = append(metas, vecMeta{
			id: id, coll: coll, name: name, nameRaw: nameRaw,
			typ: typ, desc: desc, sources: sources,
		})
		vecs = append(vecs, vec...)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	s.vecCache.mu.Lock()
	s.vecCache.loaded = true
	s.vecCache.collection = collection
	s.vecCache.model = model
	s.vecCache.vecs = vecs
	s.vecCache.dims = dims
	s.vecCache.metas = metas
	s.vecCache.mu.Unlock()
	return nil
}

// invalidateVecCache marks the in-memory vector cache as stale so the next
// semantic search reloads from DB. Call after any entity/embedding mutation.
func (s *Store) invalidateVecCache() {
	s.vecCache.mu.Lock()
	s.vecCache.loaded = false
	s.vecCache.vecs = nil
	s.vecCache.metas = nil
	s.vecCache.dims = 0
	s.vecCache.mu.Unlock()
}

// MergeCandidate is a pair of entities that are semantically similar (high
// cosine similarity of their embeddings) and may be the same entity under
// different names. The UI presents these as suggestions; the user confirms.
type MergeCandidate struct {
	KeepName  string  `json:"keepName"`  // higher-degree entity (the "canonical" one)
	MergeName string  `json:"mergeName"` // lower-degree entity to merge in
	KeepRaw   string  `json:"keepRaw"`
	MergeRaw  string  `json:"mergeRaw"`
	Score     float32 `json:"score"` // cosine similarity 0..1
}

// FindMergeCandidates finds entity pairs whose embeddings have cosine
// similarity ≥ threshold, suggesting they may be aliases of the same entity.
// Uses the in-memory vector cache for O(n²) pairwise comparison — practical
// up to ~50K entities (a few seconds). Returns candidates sorted by score desc.
//
// The caller should ensure embeddings exist (RagEmbedEntities) before calling;
// entities without embeddings are skipped.
func (s *Store) FindMergeCandidates(collection, model string, threshold float32) ([]MergeCandidate, error) {
	if err := s.ensureVecCache(collection, model); err != nil {
		return nil, err
	}

	s.vecCache.mu.RLock()
	metas := s.vecCache.metas
	vecs := s.vecCache.vecs
	dims := s.vecCache.dims
	s.vecCache.mu.RUnlock()

	if dims == 0 || len(metas) < 2 {
		return nil, nil
	}

	n := len(metas)

	// Scale guard: O(n²) pairwise comparison is expensive. Cap at 20K entities
	// (200M comparisons ≈ a few seconds). Above that the caller should embed a
	// subset or use a collection-specific search instead.
	if n > 20000 {
		return nil, fmt.Errorf("FindMergeCandidates: %d entities exceeds safe limit (20000); embed a subset or use keyword search", n)
	}

	// Precompute norms for cosine.
	norms := make([]float32, n)
	for i := 0; i < n; i++ {
		vec := vecs[i*dims : (i+1)*dims]
		var sum float32
		for _, v := range vec {
			sum += v * v
		}
		norms[i] = float32(math.Sqrt(float64(sum)))
	}

	type pair struct {
		i, j  int
		score float32
	}

	// Each worker maintains a bounded top-K buffer (min-heap by score) instead
	// of an unbounded slice — keeps memory O(K) per worker regardless of match
	// density, preventing OOM on large collections with low thresholds.
	const topKPerWorker = 300

	// minHeap is a min-heap of pairs by score (so the smallest score is at top,
	// making it easy to evict when the buffer is full).
	topPairs := make(pairHeap, 0, topKPerWorker)
	var heapMu sync.Mutex

	numWorkers := runtime.NumCPU()
	if numWorkers > 8 {
		numWorkers = 8
	}
	if numWorkers < 1 {
		numWorkers = 1
	}
	chunk := (n + numWorkers - 1) / numWorkers

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		start := w * chunk
		end := start + chunk
		if end > n {
			end = n
		}
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			local := make(pairHeap, 0, topKPerWorker)
			for i := start; i < end; i++ {
				if norms[i] == 0 {
					continue
				}
				vi := vecs[i*dims : (i+1)*dims]
				for j := i + 1; j < n; j++ {
					if norms[j] == 0 {
						continue
					}
					vj := vecs[j*dims : (j+1)*dims]
					var dot float32
					for k := 0; k < dims; k++ {
						dot += vi[k] * vj[k]
					}
					score := dot / (norms[i] * norms[j])
					if score >= threshold {
						local.push(pair{i: i, j: j, score: score}, topKPerWorker)
					}
				}
			}
			if len(local) > 0 {
				heapMu.Lock()
				for _, p := range local {
					topPairs.push(p, topKPerWorker*numWorkers)
				}
				heapMu.Unlock()
			}
		}(start, end)
	}
	wg.Wait()

	// Sort by score descending.
	sort.Sort(sort.Reverse(topPairs))

	// Limit to top 200 candidates.
	if len(topPairs) > 200 {
		topPairs = topPairs[:200]
	}

	// Determine keep vs merge by degree (keep = higher degree).
	out := make([]MergeCandidate, 0, len(topPairs))
	for _, p := range topPairs {
		mi := metas[p.i]
		mj := metas[p.j]
		if mi.name == mj.name {
			continue
		}
		keep, merge := mi, mj
		if len(mj.sources) > len(mi.sources) {
			keep, merge = mj, mi
		}
		out = append(out, MergeCandidate{
			KeepName:  keep.name,
			MergeName: merge.name,
			KeepRaw:   keep.nameRaw,
			MergeRaw:  merge.nameRaw,
			Score:     p.score,
		})
	}
	return out, nil
}

// pairHeap is a min-heap of entity pairs by score. Used by FindMergeCandidates
// to keep a bounded top-K buffer: push() evicts the smallest element when full.
type pairHeap []struct {
	i, j  int
	score float32
}

func (h pairHeap) Len() int           { return len(h) }
func (h pairHeap) Less(a, b int) bool { return h[a].score < h[b].score } // min-heap
func (h pairHeap) Swap(a, b int)      { h[a], h[b] = h[b], h[a] }

// push inserts a pair, keeping only the top `max` elements (evicts the
// smallest if over capacity). Implements a bounded min-heap.
func (h *pairHeap) push(p struct {
	i, j  int
	score float32
}, max int) {
	if len(*h) < max {
		// Heap not full — push down (heap.Push style: append then sift up).
		*h = append(*h, p)
		// Sift up.
		idx := len(*h) - 1
		for idx > 0 {
			parent := (idx - 1) / 2
			if (*h)[idx].score >= (*h)[parent].score {
				break
			}
			(*h)[idx], (*h)[parent] = (*h)[parent], (*h)[idx]
			idx = parent
		}
	} else if p.score > (*h)[0].score {
		// New pair beats the heap minimum — replace root and sift down.
		(*h)[0] = p
		n := len(*h)
		idx := 0
		for {
			left := 2*idx + 1
			right := 2*idx + 2
			smallest := idx
			if left < n && (*h)[left].score < (*h)[smallest].score {
				smallest = left
			}
			if right < n && (*h)[right].score < (*h)[smallest].score {
				smallest = right
			}
			if smallest == idx {
				break
			}
			(*h)[idx], (*h)[smallest] = (*h)[smallest], (*h)[idx]
			idx = smallest
		}
	}
}

// EntityExists checks if an entity exists and returns its ID.
func (s *Store) EntityExists(collection, name string) (int64, bool) {
	collection = normalizeCollection(collection)
	name = normalizeName(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	var id int64
	err := s.db.QueryRow(`SELECT id FROM rag_entities WHERE collection = ? AND name = ?`, collection, name).Scan(&id)
	if err != nil {
		return 0, false
	}
	return id, true
}

// AllEntitiesWithEmbeddings returns entity IDs that already have embeddings for a given model.
func (s *Store) EntityEmbeddingStatus(collection, model string) (map[int64]bool, error) {
	collection = normalizeCollection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT entity_id FROM rag_entity_embeddings WHERE collection = ? AND model = ?`, collection, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids[id] = true
		}
	}
	return ids, rows.Err()
}

// float32SliceToBytes converts []float32 to []byte for SQLite BLOB storage.
func float32SliceToBytes(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// bytesToFloat32Slice converts []byte back to []float32.
func bytesToFloat32Slice(data []byte) []float32 {
	n := len(data) / 4
	vec := make([]float32, n)
	for i := 0; i < n; i++ {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return vec
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
