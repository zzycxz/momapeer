package rag

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestParseExtractJSONCanonical confirms canonical shape parses to entities +
// relations.
func TestParseExtractJSONCanonical(t *testing.T) {
	in := []byte(`{
		"entities": [
			{"name": "张三", "type": "person", "description": "技术总监"},
			{"name": "MoMAPeer", "type": "project", "description": "办公助手"}
		],
		"relations": [
			{"source": "张三", "target": "MoMAPeer", "type": "负责", "description": "牵头开发"}
		]
	}`)
	res, err := ParseExtractJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entities) != 2 {
		t.Errorf("entities = %d, want 2", len(res.Entities))
	}
	if len(res.Relations) != 1 {
		t.Errorf("relations = %d, want 1", len(res.Relations))
	}
	if res.Relations[0].Source != "张三" || res.Relations[0].Target != "MoMAPeer" {
		t.Errorf("relation = %+v", res.Relations[0])
	}
}

// TestStripCodeFence confirms ```json fences get stripped before parsing.
func TestStripCodeFence(t *testing.T) {
	fenced := "```json\n" + `{"entities":[],"relations":[]}` + "\n```"
	res, err := ParseExtractJSON([]byte(stripCodeFence(fenced)))
	if err != nil {
		t.Fatalf("stripCodeFence + parse failed: %v", err)
	}
	if len(res.Entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(res.Entities))
	}
}

// fakeExtractor is a programmable Extractor for pipeline tests.
type fakeExtractor struct {
	calls    int32
	perChunk map[string]ExtractResult // keyed by chunk text
	failN    int32                    // fail the first N calls
	delay    time.Duration
}

func (f *fakeExtractor) Extract(ctx context.Context, chunk string, _, _ string) (ExtractResult, error) {
	n := atomic.AddInt32(&f.calls, 1)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return ExtractResult{}, ctx.Err()
		}
	}
	if n <= atomic.LoadInt32(&f.failN) {
		return ExtractResult{}, errFailed
	}
	if r, ok := f.perChunk[chunk]; ok {
		return r, nil
	}
	return ExtractResult{}, nil
}

// writeFile is a tiny helper to drop a test file. (store_test.go already
// declares a writeFile(t,...) helper with a *testing.T first arg; ours is the
// plain two-arg variant used only in this file.)
func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestPipelineEnqueueAndProcess confirms enqueue → worker → upsert → done flow
// end-to-end with a fake extractor + no rate limiting.
func TestPipelineEnqueueAndProcess(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	fpath := dir + "/doc.md"
	content := "张三是技术总监。\n\n他负责 MoMAPeer 项目。\n\n他向李四汇报。"
	if err := writeTestFile(fpath, content); err != nil {
		t.Fatal(err)
	}

	ext := &fakeExtractor{
		perChunk: map[string]ExtractResult{
			"张三是技术总监。\n\n他负责 MoMAPeer 项目。\n\n他向李四汇报。": {
				Entities: []Entity{
					{NameRaw: "张三", Type: "person", Description: "技术总监"},
					{NameRaw: "MoMAPeer", Type: "project"},
				},
				Relations: []Relation{
					{Source: "张三", Target: "MoMAPeer", Type: "负责"},
				},
			},
		},
	}
	cfg := PipelineConfig{Concurrency: 1, Interval: 0, MaxRetries: 1, RetryBase: 0}
	p := NewPipeline(s, ext, cfg, nil)
	p.Start()
	defer p.Stop()

	jobIDs, err := p.EnqueuePaths("docs", []string{fpath}, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobIDs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobIDs))
	}

	// Wait for completion (poll up to 5s).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, _, _ := s.JobByID(jobIDs[0])
		if j.Status == JobDone || j.Status == JobError {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	j, _, _ := s.JobByID(jobIDs[0])
	if j.Status != JobDone {
		t.Fatalf("job status = %q, want done", j.Status)
	}

	// Verify entities/relations landed.
	n, _ := s.EntityCount("docs")
	if n < 2 {
		t.Errorf("entity count = %d, want ≥2", n)
	}
	rels, _ := s.RelationsOf("docs", "张三", false)
	if len(rels) == 0 {
		t.Error("expected ≥1 relation for 张三")
	}
}

// TestPipelineRetryThenSucceed confirms a transient extractor failure is retried
// and the chunk ultimately succeeds.
func TestPipelineRetryThenSucceed(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	fpath := dir + "/r.md"
	content := "Alice is a researcher. She works on RAG."
	if err := writeTestFile(fpath, content); err != nil {
		t.Fatal(err)
	}
	ext := &fakeExtractor{
		failN: 2, // first two calls fail, third succeeds
		perChunk: map[string]ExtractResult{
			content: {
				Entities: []Entity{{NameRaw: "Alice", Type: "person"}},
			},
		},
	}
	cfg := PipelineConfig{Concurrency: 1, Interval: 0, MaxRetries: 3, RetryBase: 5 * time.Millisecond}
	p := NewPipeline(s, ext, cfg, nil)
	p.Start()
	defer p.Stop()

	jobIDs, _ := p.EnqueuePaths("docs", []string{fpath}, "", "", false)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, _, _ := s.JobByID(jobIDs[0])
		if j.Status == JobDone || j.Status == JobError {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	j, _, _ := s.JobByID(jobIDs[0])
	if j.Status != JobDone {
		t.Errorf("after retry, status = %q, want done", j.Status)
	}
	if calls := atomic.LoadInt32(&ext.calls); calls < 3 {
		t.Errorf("extractor called %d times, want ≥3 (retry happened)", calls)
	}
}

// TestPipelineCancelDropsQueuedTasks confirms CancelJob removes pending tasks so
// they never run. We use a blocking extractor so the worker can't drain the
// queue naturally, then verify cancel drops the rest.
func TestPipelineCancelDropsQueuedTasks(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	fpath := dir + "/c.md"
	if err := writeTestFile(fpath, "test content for cancellation"); err != nil {
		t.Fatal(err)
	}
	ext := &blockingExtractor{started: make(chan struct{})}
	cfg := PipelineConfig{Concurrency: 1, Interval: 0, MaxRetries: 1, RetryBase: 0}
	p := NewPipeline(s, ext, cfg, nil)
	p.Start()
	defer p.Stop()

	jobIDs, _ := p.EnqueuePaths("docs", []string{fpath}, "", "", false)
	if len(jobIDs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobIDs))
	}
	// Wait for the worker to pick up the first task (proving the queue worked).
	<-ext.started
	// The single chunk is now in-flight; cancelling flips job status + drops
	// any other queued tasks for this job (none here, but the call must succeed).
	if err := p.CancelJob(jobIDs[0]); err != nil {
		t.Fatal(err)
	}
	j, _, _ := s.JobByID(jobIDs[0])
	if j.Status != JobCancelled {
		t.Errorf("after cancel, job status = %q, want %q", j.Status, JobCancelled)
	}
}

type blockingExtractor struct {
	started chan struct{}
	once    sync.Once
}

func (b *blockingExtractor) Extract(ctx context.Context, chunk string, _, _ string) (ExtractResult, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done() // block until the test's ctx times out
	return ExtractResult{}, ctx.Err()
}

// TestSlidingWindow confirms the rolling average degrades to the default when
// empty and updates as samples arrive.
func TestSlidingWindow(t *testing.T) {
	w := newSlidingWindow(3)
	if got := w.Avg(); got != 3*time.Second {
		t.Errorf("empty Avg = %v, want default 3s", got)
	}
	w.Add(2 * time.Second)
	w.Add(4 * time.Second)
	if got := w.Avg(); got != 3*time.Second {
		t.Errorf("Avg(2s,4s) = %v, want 3s", got)
	}
	w.Add(6 * time.Second) // buf full (cap=3), avg = (2+4+6)/3 = 4s
	if got := w.Avg(); got != 4*time.Second {
		t.Errorf("Avg(2s,4s,6s) = %v, want 4s", got)
	}
	w.Add(10 * time.Second) // rolls over 2s; buf now 4,6,10 → avg 20/3 ≈ 6.67s
	got := w.Avg()
	if got < 6*time.Second || got > 7*time.Second {
		t.Errorf("rolling Avg = %v, want ~6.67s", got)
	}
}
