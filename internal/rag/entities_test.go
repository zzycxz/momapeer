package rag

import (
	"testing"
)

// TestUpsertEntitySimpleMerge confirms two chunks extracting the same entity
// (by normalized name) merge into one row with merged sources + longer desc.
func TestUpsertEntitySimpleMerge(t *testing.T) {
	s := newTestStore(t)
	src1 := Source{Path: "/a.md", Chunk: 0}
	src2 := Source{Path: "/a.md", Chunk: 3}

	if err := s.UpsertEntity("docs", Entity{NameRaw: "张三", Type: "person", Description: "技术总监"}, src1); err != nil {
		t.Fatal(err)
	}
	// Same name, different case/whitespace → should merge.
	if err := s.UpsertEntity("docs", Entity{NameRaw: " 张三 ", Type: "", Description: "技术总监，负责 MoMAPeer"}, src2); err != nil {
		t.Fatal(err)
	}

	ents, err := s.SearchEntities("张三", "docs", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Fatalf("expected 1 merged entity, got %d", len(ents))
	}
	e := ents[0]
	if len(e.Sources) != 2 {
		t.Errorf("expected 2 merged sources, got %d", len(e.Sources))
	}
	want := "技术总监，负责 MoMAPeer"
	if e.Description != want {
		t.Errorf("description = %q, want longer %q", e.Description, want)
	}
}

// TestUpsertEntityDifferentNamesDontMerge confirms SIMPLE strategy does NOT
// merge synonyms (Apple Inc. vs 苹果公司) — they stay as 2 rows.
func TestUpsertEntityDifferentNamesDontMerge(t *testing.T) {
	s := newTestStore(t)
	src := Source{Path: "/x.md", Chunk: 0}
	if err := s.UpsertEntity("docs", Entity{NameRaw: "Apple Inc.", Type: "org"}, src); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEntity("docs", Entity{NameRaw: "苹果公司", Type: "org"}, src); err != nil {
		t.Fatal(err)
	}
	n, err := s.EntityCount("docs")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("SIMPLE merge should keep synonyms separate; got %d entities, want 2", n)
	}
}

// TestUpsertRelation + RelationsOf confirms relations are stored and retrieved
// (including inverse lookup).
func TestUpsertRelationAndLookup(t *testing.T) {
	s := newTestStore(t)
	src := Source{Path: "/m.md", Chunk: 1}
	if err := s.UpsertRelation("docs", Relation{Source: "张三", Target: "MoMAPeer", Type: "负责", Description: "牵头交付"}, src); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertRelation("docs", Relation{Source: "张三", Target: "李四", Type: "汇报给"}, src); err != nil {
		t.Fatal(err)
	}

	// Outgoing from 张三.
	rels, err := s.RelationsOf("docs", "张三", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 2 {
		t.Errorf("outgoing relations = %d, want 2", len(rels))
	}

	// Inverse: MoMAPeer is a target, so includeInverse should find it.
	rels, err = s.RelationsOf("docs", "MoMAPeer", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Errorf("inverse relations for MoMAPeer = %d, want 1", len(rels))
	}

	// Without inverse, MoMAPeer (only a target) has zero outgoing.
	rels, err = s.RelationsOf("docs", "MoMAPeer", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 0 {
		t.Errorf("outgoing-only for MoMAPeer = %d, want 0", len(rels))
	}
}

// TestEmptyEntitySkipped confirms empty-name entities (LLM noise) are dropped.
func TestEmptyEntitySkipped(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertEntity("docs", Entity{NameRaw: "   ", Type: "person"}, Source{Path: "/x", Chunk: 0}); err != nil {
		t.Fatalf("empty entity should not error: %v", err)
	}
	n, _ := s.EntityCount("docs")
	if n != 0 {
		t.Errorf("empty-name entity should be skipped; got %d", n)
	}
}

// TestJobCreateAndProgress confirms a job is created with pending chunks, and
// MarkChunkDone advances the counter + flips status to done at the end.
func TestJobCreateAndProgress(t *testing.T) {
	s := newTestStore(t)
	chunks := []string{"chunk0", "chunk1", "chunk2"}
	jobID, err := s.CreateJob(JobRow{Collection: "docs", Path: "/a.md", Status: JobPending}, chunks)
	if err != nil {
		t.Fatal(err)
	}
	j, ok, err := s.JobByID(jobID)
	if err != nil || !ok {
		t.Fatalf("job not found: %v %v", err, ok)
	}
	if j.TotalChunks != 3 || j.DoneChunks != 0 {
		t.Errorf("initial: total=%d done=%d, want 3/0", j.TotalChunks, j.DoneChunks)
	}
	// Complete all 3 chunks.
	for i := 0; i < 3; i++ {
		cid := jobID + "_c" + itoa(i)
		if err := s.MarkChunkDone(cid, jobID, 1500, nil); err != nil {
			t.Fatal(err)
		}
	}
	j, _, _ = s.JobByID(jobID)
	if j.Status != JobDone {
		t.Errorf("after all chunks done, status = %q, want %q", j.Status, JobDone)
	}
	if j.DoneChunks != 3 {
		t.Errorf("done_chunks = %d, want 3", j.DoneChunks)
	}
}

// TestJobAllChunksFailFlipsToError confirms the job status flips to error when
// every chunk fails (distinct from partial-success done).
func TestJobAllChunksFailFlipsToError(t *testing.T) {
	s := newTestStore(t)
	jobID, _ := s.CreateJob(JobRow{Collection: "docs", Path: "/b.md", Status: JobPending}, []string{"a", "b"})
	for i := 0; i < 2; i++ {
		cid := jobID + "_c" + itoa(i)
		if err := s.MarkChunkDone(cid, jobID, 0, errFailed); err != nil {
			t.Fatal(err)
		}
	}
	j, _, _ := s.JobByID(jobID)
	if j.Status != JobError {
		t.Errorf("all-failed status = %q, want %q", j.Status, JobError)
	}
}

var errFailed = newErr("simulated failure")

func newErr(s string) error { return &testErr{s} }

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/rag.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
