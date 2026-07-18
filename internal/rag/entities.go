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
	"encoding/json"
	"fmt"
	"strings"
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
		_ = json.Unmarshal([]byte(existingSourcesJSON), &existingSources)
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
	if srcName == "" || tgtName == "" {
		return nil
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
		_, err := s.db.Exec(`INSERT INTO rag_relations (collection, source, target, type, description, sources) VALUES (?, ?, ?, ?, ?, ?)`,
			collection, srcName, tgtName, r.Type, r.Description, string(sj))
		return err
	case err != nil:
		return fmt.Errorf("query relation: %w", err)
	}

	var existingSources []Source
	if existingSourcesJSON != "" {
		_ = json.Unmarshal([]byte(existingSourcesJSON), &existingSources)
	}
	merged := mergeSources(existingSources, src)
	descVal := existingDesc
	if len(r.Description) > len(descVal) {
		descVal = r.Description
	}
	sj, _ := json.Marshal(merged)
	_, err = s.db.Exec(`UPDATE rag_relations SET description = ?, sources = ? WHERE id = ?`,
		descVal, string(sj), existingID)
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
	rows, err := s.db.Query(`SELECT id, collection, name, name_raw, COALESCE(type,''), COALESCE(description,''), COALESCE(sources,'')
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
		if err := rows.Scan(&e.ID, &e.Collection, &e.Name, &e.NameRaw, &e.Type, &e.Description, &sourcesJSON); err != nil {
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
	defer tx.Rollback()
	nowTS := time.Now().UTC()
	now := nowTS.Format(time.RFC3339)
	if j.ID == "" {
		j.ID = fmt.Sprintf("job_%d", nowTS.UnixNano())
	}
	if _, err := tx.Exec(`INSERT INTO rag_jobs (id, collection, path, rel_path, root_path, is_dir, status, total_chunks, done_chunks, error_msg, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)
		ON CONFLICT(collection, path) DO UPDATE SET status=excluded.status, total_chunks=excluded.total_chunks, done_chunks=0, error_msg=NULL, updated_at=excluded.updated_at`,
		j.ID, normalizeCollection(j.Collection), j.Path, j.RelPath, j.RootPath, boolToInt(j.IsDir), j.Status, len(chunkTexts), j.ErrorMsg, now, now); err != nil {
		return "", err
	}
	// Reset chunks for this job (delete-then-insert, in case of re-extract).
	if _, err := tx.Exec(`DELETE FROM rag_chunks WHERE job_id = ?`, j.ID); err != nil {
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
	defer tx.Rollback()
	status := ChunkDone
	errMsg := ""
	if err != nil {
		status = ChunkError
		errMsg = err.Error()
	}
	if _, e := tx.Exec(`UPDATE rag_chunks SET status = ?, latency_ms = ?, error_msg = ?, attempts = attempts + 1 WHERE id = ?`,
		status, latencyMs, errMsg, chunkID); e != nil {
		return e
	}
	if _, e := tx.Exec(`UPDATE rag_jobs SET done_chunks = done_chunks + 1, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), jobID); e != nil {
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
	rows, err := s.db.Query(`SELECT id, collection, path, COALESCE(rel_path,''), COALESCE(root_path,''), is_dir, status, total_chunks, done_chunks, COALESCE(error_msg,''), COALESCE(created_at,''), COALESCE(updated_at,'') FROM rag_jobs ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanJobs(rows)
}

// PendingChunksForJob returns the (chunkID, idx) pairs still pending/errored
// for a job — used by Pipeline.Resume to rehydrate the queue after a restart.
// The chunk TEXT is re-read from FTS5 by (path, idx).
func (s *Store) PendingChunksForJob(jobID string) ([]struct{ ChunkID string; Idx int }, error) {
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

func scanJobs(rows *sql.Rows) ([]JobRow, error) {
	var out []JobRow
	for rows.Next() {
		var j JobRow
		var isDir int
		var created, updated string
		if err := rows.Scan(&j.ID, &j.Collection, &j.Path, &j.RelPath, &j.RootPath, &isDir, &j.Status, &j.TotalChunks, &j.DoneChunks, &j.ErrorMsg, &created, &updated); err != nil {
			continue
		}
		j.IsDir = isDir != 0
		j.Collection = normalizeCollection(j.Collection)
		j.CreatedAt, _ = time.Parse(time.RFC3339, created)
		j.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, j)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// EntityRelationView is the JSON-friendly projection for entity detail views.
type EntityRelationView struct {
	Source      string  `json:"source"`
	Target      string  `json:"target"`
	Type        string  `json:"type"`
	Description string  `json:"description,omitempty"`
	Direction   string  `json:"direction,omitempty"`
	Peer        string  `json:"peer,omitempty"`
	Strength    float64 `json:"strength,omitempty"`
}

// MergeCandidate represents two entities that might be aliases of each other.
type MergeCandidate struct {
	Name      string  `json:"name"`
	Raw       string  `json:"raw,omitempty"`
	Score     float64 `json:"score,omitempty"`
	KeepName  string  `json:"keepName,omitempty"`
	KeepRaw   string  `json:"keepRaw,omitempty"`
	MergeName string  `json:"mergeName,omitempty"`
	MergeRaw  string  `json:"mergeRaw,omitempty"`
}
