package rag

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestEnqueueFileDedupSkipsReExtraction verifies that re-importing an unchanged
// file whose extraction already completed does NOT reset its job or re-enqueue
// chunks (which would burn LLM quota).
func TestEnqueueFileDedupSkipsReExtraction(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	p := NewPipeline(store, noopExtractor{}, DefaultPipelineConfig(), nil)
	p.SetLogger(func(format string, args ...any) {})

	fpath := filepath.Join(dir, "doc.md")
	writeFile(t, fpath, "# Doc\n\nSome content for extraction here.\n\nMore content.")

	// First import + extract.
	jid1, err := p.EnqueuePaths("c1", []string{fpath}, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jid1) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jid1))
	}
	// Force the job to "done" so dedup kicks in on re-import.
	if err := store.SetJobStatus(jid1[0], JobDone); err != nil {
		t.Fatal(err)
	}

	// Second import of the SAME file — should dedup-skip.
	jid2, err := p.EnqueuePaths("c1", []string{fpath}, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jid2) != 1 || jid2[0] != jid1[0] {
		t.Fatalf("dedup should return same job id %q, got %v", jid1[0], jid2)
	}
	// Job status must remain "done" (not reset to pending).
	_, status, _, doneChunks, err := store.JobStatusForPath("c1", fpath)
	if err != nil {
		t.Fatal(err)
	}
	if status != JobDone {
		t.Fatalf("after re-import of done file, status = %q, want %q", status, JobDone)
	}
	if doneChunks != 0 {
		t.Fatalf("done_chunks should be unchanged, got %d", doneChunks)
	}
}

// TestEnqueueFileDedupRetriggersWhenChanged verifies that a file whose content
// changed (different chunk count) re-triggers extraction even if a prior job
// was done — content drift must not be masked by dedup.
func TestEnqueueFileDedupRetriggersWhenChanged(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	p := NewPipeline(store, noopExtractor{}, DefaultPipelineConfig(), nil)
	p.SetLogger(func(format string, args ...any) {})

	fpath := filepath.Join(dir, "doc.md")
	writeFile(t, fpath, "# Doc\n\nShort content.")
	jid1, err := p.EnqueuePaths("c1", []string{fpath}, "", "", false)
	if err != nil || len(jid1) != 1 {
		t.Fatalf("first import: err=%v jobs=%v", err, jid1)
	}
	store.SetJobStatus(jid1[0], JobDone)

	// Rewrite with more content → more chunks → different chunk count.
	writeFile(t, fpath, "# Doc\n\n"+strings.Repeat("Much longer content. ", 200))
	jid2, err := p.EnqueuePaths("c1", []string{fpath}, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jid2) != 1 {
		t.Fatal("expected 1 job")
	}
	// The job should have been reset to pending (re-extraction triggered).
	_, status2, _, _, err := store.JobStatusForPath("c1", fpath)
	if err != nil {
		t.Fatal(err)
	}
	if status2 != JobPending {
		t.Fatalf("changed file should re-trigger extraction; status=%q want %q", status2, JobPending)
	}
}

// TestResumeRehydratesInterruptedJobs simulates a shutdown mid-extraction:
// jobs are left in pending/extracting, the pipeline queue is empty (as after a
// restart), and Resume() must re-enqueue the pending chunks.
func TestResumeRehydratesInterruptedJobs(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create a file and import it so FTS5 has the chunk text (Resume reads back
	// chunk bodies from FTS5 since chunk text isn't persisted on rag_chunks).
	fpath := filepath.Join(dir, "doc.md")
	body := "# Doc\n\nPara one.\n\nPara two.\n\nPara three."
	writeFile(t, fpath, body)

	p := NewPipeline(store, noopExtractor{}, DefaultPipelineConfig(), nil)
	p.SetLogger(func(format string, args ...any) {})
	jid, err := p.EnqueuePaths("c1", []string{fpath}, "", "", false)
	if err != nil || len(jid) != 1 {
		t.Fatalf("enqueue: err=%v jobs=%v", err, jid)
	}
	// Simulate partial progress: all chunks are pending right after enqueue.
	pending, err := store.PendingChunksForJob(jid[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) == 0 {
		t.Fatal("expected pending chunks after enqueue")
	}
	// Drain the in-memory queue (simulate restart: queue cleared, workers gone).
	p.mu.Lock()
	p.queue = nil
	p.mu.Unlock()

	// Resume should re-enqueue the pending chunks from durable state.
	n := p.Resume()
	if n == 0 {
		t.Fatal("Resume() re-enqueued 0 chunks; expected the pending ones")
	}
	if n != len(pending) {
		t.Fatalf("Resume() re-enqueued %d, want %d (all pending)", n, len(pending))
	}
	// The rehydrated tasks must carry real text read from FTS5 (not empty).
	p.mu.Lock()
	for _, task := range p.queue {
		if task.Text == "" {
			t.Errorf("rehydrated task chunk %d has empty text", task.ChunkIdx)
		}
		if task.JobID != jid[0] {
			t.Errorf("rehydrated task jobID = %q, want %q", task.JobID, jid[0])
		}
	}
	p.mu.Unlock()
}

// TestResumeNoopOnCleanShutdown verifies Resume() does nothing when all jobs
// are done (normal steady state after a clean extraction run).
func TestResumeNoopOnCleanShutdown(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	fpath := filepath.Join(dir, "doc.md")
	writeFile(t, fpath, "# Done doc\n\nContent.")
	p := NewPipeline(store, noopExtractor{}, DefaultPipelineConfig(), nil)
	p.SetLogger(func(format string, args ...any) {})
	jid, _ := p.EnqueuePaths("c1", []string{fpath}, "", "", false)
	store.SetJobStatus(jid[0], JobDone)

	p.mu.Lock()
	p.queue = nil
	p.mu.Unlock()

	if n := p.Resume(); n != 0 {
		t.Fatalf("Resume() on all-done jobs re-enqueued %d, want 0", n)
	}
}

// TestEnqueueFileDedupRetriggersOnContentEditSameChunkCount verifies the
// content-hash dedup: a file whose content changed WITHOUT changing the chunk
// count (an edit within a single 1200-char chunk) must still re-trigger
// extraction. The earlier chunk-count-based dedup would have incorrectly
// skipped this case, leaving the entity graph stale while FTS5 updated.
func TestEnqueueFileDedupRetriggersOnContentEditSameChunkCount(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	p := NewPipeline(store, noopExtractor{}, DefaultPipelineConfig(), nil)
	p.SetLogger(func(format string, args ...any) {})

	fpath := filepath.Join(dir, "doc.md")
	// Original: short body → 1 chunk.
	writeFile(t, fpath, "# Doc\n\nOriginal content here for testing dedup.")
	jid1, err := p.EnqueuePaths("c1", []string{fpath}, "", "", false)
	if err != nil || len(jid1) != 1 {
		t.Fatalf("first import: err=%v jobs=%v", err, jid1)
	}
	store.SetJobStatus(jid1[0], JobDone)

	// Rewrite: same structure (still 1 chunk) but DIFFERENT content.
	writeFile(t, fpath, "# Doc\n\nEDITED content here for testing dedup.")
	jid2, err := p.EnqueuePaths("c1", []string{fpath}, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jid2) != 1 {
		t.Fatal("expected 1 job")
	}
	// A content edit (even with the same chunk count) must re-trigger: the job
	// should have been reset to pending, NOT dedup-skipped.
	_, status, _, _, err := store.JobStatusForPath("c1", fpath)
	if err != nil {
		t.Fatal(err)
	}
	if status != JobPending {
		t.Fatalf("content edit with same chunk count should re-trigger extraction; status=%q want %q", status, JobPending)
	}
}

// TestResumePreservesPrompts verifies that Resume restores the node/edge prompts
// stored on the job row, so a domain-template extraction (e.g. finance/graph)
// survives a restart instead of falling back to the default general prompt.
func TestResumePreservesPrompts(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	fpath := filepath.Join(dir, "doc.md")
	writeFile(t, fpath, "# Doc\n\nPara one.\n\nPara two.")
	const nodeP = "custom node prompt"
	const edgeP = "custom edge prompt"
	p := NewPipeline(store, noopExtractor{}, DefaultPipelineConfig(), nil)
	p.SetLogger(func(format string, args ...any) {})
	jid, err := p.EnqueuePaths("c1", []string{fpath}, nodeP, edgeP, false)
	if err != nil || len(jid) != 1 {
		t.Fatalf("enqueue: err=%v jobs=%v", err, jid)
	}
	// Drain the in-memory queue (simulate restart).
	p.mu.Lock()
	p.queue = nil
	p.mu.Unlock()

	p.Resume()
	p.mu.Lock()
	tasks := append([]chunkTask(nil), p.queue...)
	p.mu.Unlock()
	if len(tasks) == 0 {
		t.Fatal("Resume re-enqueued 0 chunks")
	}
	for _, task := range tasks {
		if task.NodePrompt != nodeP {
			t.Errorf("resumed task NodePrompt = %q, want %q", task.NodePrompt, nodeP)
		}
		if task.EdgePrompt != edgeP {
			t.Errorf("resumed task EdgePrompt = %q, want %q", task.EdgePrompt, edgeP)
		}
	}
}

// TestResumeFeedsOriginalTextNotBigram verifies the body_raw fix: a Chinese
// document resumed after restart must feed the ORIGINAL (un-bigrammed) chunk
// text to the extractor, not the bigram-expanded indexed body. Uses a Chinese
// doc so expandCJKBigrams actually transforms the text (the bug is invisible
// for pure-English docs).
func TestResumeFeedsOriginalTextNotBigram(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	fpath := filepath.Join(dir, "cn.md")
	writeFile(t, fpath, "# 文档\n\n渲染管线处理图形输出")
	p := NewPipeline(store, noopExtractor{}, DefaultPipelineConfig(), nil)
	p.SetLogger(func(format string, args ...any) {})
	jid, err := p.EnqueuePaths("c1", []string{fpath}, "", "", false)
	if err != nil || len(jid) != 1 {
		t.Fatalf("enqueue: err=%v jobs=%v", err, jid)
	}
	p.mu.Lock()
	p.queue = nil
	p.mu.Unlock()

	p.Resume()
	p.mu.Lock()
	tasks := append([]chunkTask(nil), p.queue...)
	p.mu.Unlock()
	if len(tasks) == 0 {
		t.Fatal("Resume re-enqueued 0 chunks")
	}
	for _, task := range tasks {
		// The resumed text must contain the ORIGINAL continuous CJK run, not
		// bigram-spaced text like "渲染 染管 管线".
		if !strings.Contains(task.Text, "渲染管线") {
			t.Errorf("resumed Chinese chunk text is not original (bigram leak?): %q", task.Text)
		}
		if strings.Contains(task.Text, "渲染 染管") {
			t.Errorf("resumed Chinese chunk text has bigram spaces: %q", task.Text)
		}
	}
}

// TestReimportFolderPreservesDoneJobs reproduces the user-reported bug: a
// folder has 5 files, 3 are extracted (done); re-importing the folder (now 7
// files) must NOT reset the 3 done jobs back to pending. The stat-key dedup
// (size+mtime unchanged) short-circuits before readDoc, so done files are
// skipped wholesale. Only the 2 genuinely-new files get enqueued.
func TestReimportFolderPreservesDoneJobs(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	p := NewPipeline(store, noopExtractor{}, DefaultPipelineConfig(), nil)
	p.SetLogger(func(format string, args ...any) {})

	// Three files in the initial import.
	f1 := filepath.Join(dir, "a.md")
	f2 := filepath.Join(dir, "b.md")
	f3 := filepath.Join(dir, "c.md")
	writeFile(t, f1, "# A\n\ncontent one for a.")
	writeFile(t, f2, "# B\n\ncontent two for b.")
	writeFile(t, f3, "# C\n\ncontent three for c.")

	jids, err := p.EnqueuePaths("c1", []string{f1, f2, f3}, "", "", false)
	if err != nil || len(jids) != 3 {
		t.Fatalf("initial import: err=%v jobs=%v", err, jids)
	}
	// Mark all three done (simulate completed extraction).
	for _, jid := range jids {
		if err := store.SetJobStatus(jid, JobDone); err != nil {
			t.Fatal(err)
		}
	}

	// Re-import the SAME three files (no writeFile in between → size+mtime
	// unchanged). Every done job must be preserved.
	jids2, err := p.EnqueuePaths("c1", []string{f1, f2, f3}, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	// Dedup returns the same job IDs (no new jobs created).
	if len(jids2) != 3 {
		t.Fatalf("re-import of unchanged files should return 3 jobs, got %d", len(jids2))
	}
	for _, fpath := range []string{f1, f2, f3} {
		_, status, _, _, err := store.JobStatusForPath("c1", fpath)
		if err != nil {
			t.Fatal(err)
		}
		if status != JobDone {
			t.Errorf("re-imported unchanged file %s: status=%q want %q", fpath, status, JobDone)
		}
	}
}

// TestReimportFolderAddsNewFilesOnly verifies the companion case: re-importing
// a folder that now contains extra files enqueues ONLY the new ones; existing
// done jobs are untouched.
func TestReimportFolderAddsNewFilesOnly(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "rag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	p := NewPipeline(store, noopExtractor{}, DefaultPipelineConfig(), nil)
	p.SetLogger(func(format string, args ...any) {})

	f1 := filepath.Join(dir, "a.md")
	f2 := filepath.Join(dir, "b.md")
	writeFile(t, f1, "# A\n\ncontent for a.")
	writeFile(t, f2, "# B\n\ncontent for b.")
	jids, err := p.EnqueuePaths("c1", []string{f1, f2}, "", "", false)
	if err != nil || len(jids) != 2 {
		t.Fatalf("initial import: err=%v jobs=%v", err, jids)
	}
	for _, jid := range jids {
		store.SetJobStatus(jid, JobDone)
	}

	// A genuinely new file is added to the folder.
	f3 := filepath.Join(dir, "c.md")
	writeFile(t, f3, "# C\n\nbrand new content for c.")

	// Re-import all three. f1+f2 should dedup-skip (done + unchanged); only
	// f3 is new.
	if _, err := p.EnqueuePaths("c1", []string{f1, f2, f3}, "", "", false); err != nil {
		t.Fatal(err)
	}
	// f1, f2 stay done; f3 is pending (newly enqueued).
	_, s1, _, _, _ := store.JobStatusForPath("c1", f1)
	_, s2, _, _, _ := store.JobStatusForPath("c1", f2)
	_, s3, _, _, _ := store.JobStatusForPath("c1", f3)
	if s1 != JobDone {
		t.Errorf("f1 status=%q want done (should be preserved)", s1)
	}
	if s2 != JobDone {
		t.Errorf("f2 status=%q want done (should be preserved)", s2)
	}
	if s3 != JobPending {
		t.Errorf("f3 status=%q want pending (newly enqueued)", s3)
	}
}
