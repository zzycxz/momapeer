package main

// rag_app.go exposes the coWork knowledge-base (FTS5 + structured entities) and
// its deep-extraction pipeline to the frontend via Wails bindings. The UI uses
// these to: list collections, show a file/folder tree with per-file extraction
// status + progress, import paths, start/cancel deep extraction, and run a
// search to verify results — all without touching the chat.
//
// Events:
//   - "rag:progress" (ProgressEvent) — emitted by the pipeline on each chunk;
//     the panel updates the corresponding tree node's progress bar.
//   - "rag:changed" (no payload) — emitted on import/remove/status change so
//     the panel can re-fetch the tree.

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zzycxz/momapeer/internal/rag"
)

// RagNodeView is one node in the RAG file/folder tree.
type RagNodeView struct {
	Key        string       `json:"key"`        // unique (path)
	Label      string       `json:"label"`      // display name (basename)
	Kind       string       `json:"kind"`       // "folder" | "file"
	Path       string       `json:"path"`       // absolute path
	RelPath    string       `json:"relPath"`    // relative to import root
	IsDir      bool         `json:"isDir"`
	Collection string       `json:"collection"`
	// Status for files: "indexed" (FTS5 only) | "extracting" | "enriched" | "error" | "cancelled"
	// Folders aggregate: "" (no status) unless all children share one.
	Status      string `json:"status"`
	HasFTS5     bool   `json:"hasFts5"`     // FTS5 chunks exist for this file
	JobID       string `json:"jobId"`       // current/last extraction job
	DoneChunks  int    `json:"doneChunks"`  // extraction progress
	TotalChunks int    `json:"totalChunks"`
	EntityCount int    `json:"entityCount"` // extracted entities attributed to this file (best-effort)
	ErrorMsg    string `json:"errorMsg"`
	Children    []RagNodeView `json:"children,omitempty"` // folder recursion
}

// RagCollectionView is one named collection summary (for the dropdown).
type RagCollectionView struct {
	Name      string `json:"name"`
	Documents int    `json:"documents"`
	Chunks    int    `json:"chunks"`
	Entities  int    `json:"entities"`
}

// RagImportResult is returned by RagImportPaths — what the UI shows immediately.
type RagImportResult struct {
	JobIDs    []string `json:"jobIds"`
	Files     int      `json:"files"`     // files found under the paths
	FTSChunks int      `json:"ftsChunks"` // total FTS5 chunks indexed (instant layer)
	Message   string   `json:"message"`
}

// RagSearchHitView is one search result layer (entities/relations or text).
type RagSearchHitView struct {
	Entities  []RagEntityView `json:"entities"`
	Relations []RagRelView    `json:"relations"`
	Snippets  []RagSnippetView `json:"snippets"`
}

type RagEntityView struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}
type RagRelView struct {
	Source      string `json:"source"`
	Target      string `json:"target"`
	Type        string `json:"type"`
	Description string `json:"description"`
}
type RagSnippetView struct {
	Collection string  `json:"collection"`
	Path       string  `json:"path"`
	Chunk      int     `json:"chunk"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
}

// RagETAView is the on-demand ETA probe for hover tooltips.
type RagETAView struct {
	JobID        string `json:"jobId"`
	DoneChunks   int    `json:"doneChunks"`
	TotalChunks  int    `json:"totalChunks"`
	AvgLatencyMs int64  `json:"avgLatencyMs"`
	ETASeconds   int64  `json:"etaSeconds"`
}

// ListRagCollections returns all collections with their doc/chunk/entity counts.
func (a *App) ListRagCollections() []RagCollectionView {
	if a.ragStore == nil {
		return []RagCollectionView{}
	}
	cols, err := a.ragStore.List("")
	if err != nil {
		return []RagCollectionView{}
	}
	out := make([]RagCollectionView, 0, len(cols))
	for _, c := range cols {
		ent, _ := a.ragStore.EntityCount(c.Name)
		out = append(out, RagCollectionView{
			Name: c.Name, Documents: c.Documents, Chunks: c.Chunks, Entities: ent,
		})
	}
	return out
}

// ListRagTree builds the file/folder tree for one collection (or all when
// empty). The tree is synthesized from rag_jobs (one job per imported file) —
// folders are reconstructed by splitting each job's rel_path. Status + progress
// come from the job row. Empty collection = aggregate across all collections.
func (a *App) ListRagTree(collection string) []RagNodeView {
	if a.ragStore == nil {
		return []RagNodeView{}
	}
	jobs, err := a.ragStore.AllJobs()
	if err != nil {
		return []RagNodeView{}
	}
	// Build a flat list of file nodes, then nest by folder.
	root := &RagNodeView{Key: "__root__", Kind: "folder", Children: []RagNodeView{}}
	for _, j := range jobs {
		if collection != "" && j.Collection != strings.ToLower(strings.TrimSpace(collection)) {
			continue
		}
		ent := a.fileEntityCount(j.Path)
		node := RagNodeView{
			Key:          j.Path,
			Label:        filepath.Base(j.Path),
			Kind:         "file",
			Path:         j.Path,
			RelPath:      j.RelPath,
			IsDir:        false,
			Collection:   j.Collection,
			Status:       fileStatus(j),
			HasFTS5:      true, // jobs only exist for FTS5-imported files
			JobID:        j.ID,
			DoneChunks:   j.DoneChunks,
			TotalChunks:  j.TotalChunks,
			EntityCount:  ent,
			ErrorMsg:     j.ErrorMsg,
		}
		insertIntoTree(root, j.RelPath, node)
	}
	return root.Children
}

// fileStatus maps a job's status to the UI status vocabulary.
func fileStatus(j rag.JobRow) string {
	switch j.Status {
	case rag.JobPending:
		return "indexed" // FTS5 ready, extraction queued (not yet started)
	case rag.JobExtracting:
		return "extracting"
	case rag.JobDone:
		if j.DoneChunks > 0 {
			return "enriched"
		}
		return "indexed"
	case rag.JobError:
		return "error"
	case rag.JobCancelled:
		return "cancelled"
	}
	return "indexed"
}

// fileEntityCount returns entities whose sources include this path (best-effort
// — we approximate by counting entities in the collection since per-file entity
// attribution requires parsing the sources JSON for every row, which is too
// costly per tree render). For an accurate per-file count, the UI can call
// rag_graph with a filter.
func (a *App) fileEntityCount(path string) int {
	// Per-file attribution is expensive; surface 0 here and let the UI show the
	// collection-level count on the collection row instead.
	return 0
}

// insertIntoTree walks relPath's folder segments under root, creating folder
// nodes as needed, and appends the file leaf at the end.
func insertIntoTree(root *RagNodeView, relPath string, file RagNodeView) {
	if relPath == "" {
		// No relative path (single-file import at root): put file directly under root.
		root.Children = append(root.Children, file)
		return
	}
	// Use forward slashes for consistency (rel_path may use OS separators).
	rel := filepath.ToSlash(relPath)
	segs := strings.Split(rel, "/")
	cur := root
	for i, seg := range segs {
		if seg == "" || i == len(segs)-1 {
			// Last segment = the file itself.
			file.Label = seg
			cur.Children = append(cur.Children, file)
			return
		}
		// Find or create the folder child.
		var child *RagNodeView
		for k := range cur.Children {
			if cur.Children[k].Kind == "folder" && cur.Children[k].Label == seg {
				child = &cur.Children[k]
				break
			}
		}
		if child == nil {
			cur.Children = append(cur.Children, RagNodeView{
				Key:    filepath.Join(cur.Path, seg),
				Label:  seg,
				Kind:   "folder",
				IsDir:  true,
				Children: []RagNodeView{},
			})
			child = &cur.Children[len(cur.Children)-1]
		}
		cur = child
	}
}

// RagImportPaths imports one or more files/folders into a collection. FTS5
// indexing is synchronous (the user can search immediately); deep extraction is
// enqueued to run in the background. Returns job IDs + counts for the UI.
// When collection is empty, "default" is used.
func (a *App) RagImportPaths(collection string, paths []string) (RagImportResult, error) {
	if a.ragPipeline == nil {
		return RagImportResult{}, fmt.Errorf("RAG pipeline offline")
	}
	if len(paths) == 0 {
		return RagImportResult{}, fmt.Errorf("no paths given")
	}
	jobIDs, err := a.ragPipeline.EnqueuePaths(collection, paths)
	if err != nil {
		return RagImportResult{}, err
	}
	a.emitRagChanged()
	// Count files + FTS chunks from the resulting jobs for the result message.
	files, ftsChunks := 0, 0
	for _, jid := range jobIDs {
		j, ok, _ := a.ragStore.JobByID(jid)
		if ok {
			files++
			ftsChunks += j.TotalChunks
		}
	}
	return RagImportResult{
		JobIDs:    jobIDs,
		Files:     files,
		FTSChunks: ftsChunks,
		Message:   fmt.Sprintf("已导入 %d 个文件（%d chunks）FTS5 即时可搜；深度抽取已在后台开始", files, ftsChunks),
	}, nil
}

// RagStartExtract is a no-op when the pipeline already enqueued extraction at
// import time (which it does). We expose it so the UI's "深度提取" button can
// re-trigger extraction for a file that was imported but whose extraction was
// cancelled or errored. It re-enqueues by re-importing (FTS5 is idempotent,
// re-extract creates a fresh job).
func (a *App) RagStartExtract(collection, path string) error {
	if a.ragPipeline == nil {
		return fmt.Errorf("RAG pipeline offline")
	}
	if _, err := a.ragPipeline.EnqueuePaths(collection, []string{path}); err != nil {
		return err
	}
	a.emitRagChanged()
	return nil
}

// RagCancelExtract cancels a running extraction job. Pending chunks are dropped;
// in-flight chunks finish naturally (we don't interrupt an LLM call).
func (a *App) RagCancelExtract(jobID string) error {
	if a.ragPipeline == nil {
		return fmt.Errorf("RAG pipeline offline")
	}
	if err := a.ragPipeline.CancelJob(jobID); err != nil {
		return err
	}
	a.emitRagChanged()
	return nil
}

// RagRemovePath removes a file or folder from the knowledge base (FTS5 chunks +
// best-effort entity cleanup). Entities are collection-scoped, so we only wipe
// the whole collection's entities when the path matches the whole collection;
// otherwise entities remain (they may have been contributed by other files).
func (a *App) RagRemovePath(collection, path string) error {
	if a.ragStore == nil {
		return fmt.Errorf("RAG store offline")
	}
	if err := a.ragStore.Delete(collection, path); err != nil {
		return err
	}
	a.emitRagChanged()
	return nil
}

// RagClear removes an entire collection (all docs, chunks, entities, relations,
// extraction jobs) and reclaims the freed space with VACUUM. This is the
// "reset this knowledge base" entrypoint — irreversible, so callers (UI) should
// confirm. An empty collection name clears nothing; pass an explicit name.
func (a *App) RagClear(collection string) error {
	if a.ragStore == nil {
		return fmt.Errorf("RAG store offline")
	}
	if strings.TrimSpace(collection) == "" {
		return fmt.Errorf("collection name is required to clear (use RagRemovePath per-doc instead)")
	}
	if err := a.ragStore.Delete(collection, ""); err != nil {
		return err
	}
	if err := a.ragStore.Vacuum(); err != nil {
		// VACUUM failing is non-fatal: the data is already gone, only space
		// reclaim didn't happen. Log and proceed.
		slog.Warn("rag: vacuum after clear failed", "err", err)
	}
	a.emitRagChanged()
	return nil
}

// RagSearch runs a combined search (structured entities + FTS5 snippets) for
// the in-panel search bar. Mirrors rag_search tool output but as JSON.
func (a *App) RagSearch(collection, query string, topK int) (RagSearchHitView, error) {
	if a.ragStore == nil {
		return RagSearchHitView{}, fmt.Errorf("RAG store offline")
	}
	if topK <= 0 {
		topK = 5
	}
	out := RagSearchHitView{}
	has, _ := a.ragStore.HasEntities(collection)
	if has {
		ents, _ := a.ragStore.SearchEntities(query, collection, topK)
		for _, e := range ents {
			out.Entities = append(out.Entities, RagEntityView{
				Name: e.NameRaw, Type: e.Type, Description: e.Description,
			})
			rels, _ := a.ragStore.RelationsOf(collection, e.Name, true)
			for _, r := range rels {
				out.Relations = append(out.Relations, RagRelView{
					Source: r.Source, Target: r.Target, Type: r.Type, Description: r.Description,
				})
			}
		}
	}
	res, err := a.ragStore.Search(query, collection, topK)
	if err == nil {
		for _, r := range res {
			out.Snippets = append(out.Snippets, RagSnippetView{
				Collection: r.Collection, Path: r.Path, Chunk: r.Chunk,
				Snippet: r.Snippet, Score: r.Score,
			})
		}
	}
	return out, nil
}

// RagPreviewETA returns the current ETA for a job (for hover tooltips). Pulls
// the latest job state + the pipeline's in-memory average latency.
func (a *App) RagPreviewETA(jobID string) (RagETAView, error) {
	if a.ragStore == nil {
		return RagETAView{}, fmt.Errorf("RAG store offline")
	}
	j, ok, err := a.ragStore.JobByID(jobID)
	if err != nil || !ok {
		return RagETAView{}, fmt.Errorf("job not found")
	}
	var avgMs int64
	if a.ragPipeline != nil {
		avgMs = a.ragPipeline.LatencyAvgMs()
	}
	if avgMs == 0 {
		// Fall back to the persisted average (across all done chunks).
		avgMs, _ = a.ragStore.AvgChunkLatencyMs(j.Collection)
	}
	remaining := j.TotalChunks - j.DoneChunks
	if remaining < 0 {
		remaining = 0
	}
	etaSec := int64(float64(avgMs) * float64(remaining) / 1000.0)
	return RagETAView{
		JobID:        jobID,
		DoneChunks:   j.DoneChunks,
		TotalChunks:  j.TotalChunks,
		AvgLatencyMs: avgMs,
		ETASeconds:   etaSec,
	}, nil
}

// RagListTemplates returns the supported file extensions (informational — for
// the import dialog's "supported formats" hint).
func (a *App) RagListTemplates() []string {
	return []string{".txt", ".md", ".csv", ".tsv", ".json", ".html", ".py", ".go", ".js", ".ts", ".yaml"}
}

// emitRagChanged notifies the frontend that the tree/collections mutated.
func (a *App) emitRagChanged() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "rag:changed")
}

// sortRagNodes sorts folder-first, then label — for stable tree rendering.
// (Currently unused; kept for future deterministic ordering.)
func sortRagNodes(nodes []RagNodeView) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Kind != nodes[j].Kind {
			return nodes[i].Kind == "folder"
		}
		return nodes[i].Label < nodes[j].Label
	})
}
