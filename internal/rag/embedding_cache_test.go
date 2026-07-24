package rag

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// countingEmbedder records every text it's asked to embed, so the cache test
// can prove a repeated search re-embeds only the query (chunk hits the cache).
type countingEmbedder struct {
	calls atomic.Int32
}

func (c *countingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	c.calls.Add(int32(len(texts)))
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0.1 * float32(i+1), 0.2, 0.3}
	}
	return out, nil
}

// TestEmbeddingCacheHitsOnRepeatSearch proves the rerank embedding cache works:
// the first search embeds query + all candidate chunks; the second search over
// the same corpus embeds ONLY the query (chunks are served from cache). This is
// the cost/latency win — repeated searches stop re-calling the embedding API.
func TestEmbeddingCacheHitsOnRepeatSearch(t *testing.T) {
	store := newTempStore(t)
	defer store.Close()
	// Seed a few chunks via Import so Rerank has real Result rows.
	p := filepath.Join(t.TempDir(), "doc.md")
	writeFile(t, p, "alpha paragraph about cats.\n\nbeta paragraph about dogs.\n\ngamma about birds.")
	if _, err := store.Import("c", p, nil); err != nil {
		t.Fatal(err)
	}
	emb := &countingEmbedder{}

	// First search: embeds query + N candidate chunks.
	store.Rerank(context.Background(), "cats", mustSearch(t, store, "c", "cats"), emb, 0.5)
	first := emb.calls.Load()
	if first < 2 {
		t.Fatalf("first search should embed query + ≥1 chunk, got %d embeds", first)
	}

	// Second identical search: chunks now cached, so only the query is embedded.
	emb.calls.Store(0)
	store.Rerank(context.Background(), "cats", mustSearch(t, store, "c", "cats"), emb, 0.5)
	second := emb.calls.Load()
	if second != 1 {
		t.Errorf("second search should embed only the query (1), got %d — cache miss", second)
	}
}

// TestChunkHashChangesWithBody confirms a re-imported chunk (edited body) gets a
// different cache key, so stale embeddings don't poison rerank after an edit.
func TestChunkHashChangesWithBody(t *testing.T) {
	a := chunkHash("c", "/p.md", 0, "old body")
	b := chunkHash("c", "/p.md", 0, "new body")
	if a == b {
		t.Error("chunk hash must change when body changes (stale-cache risk)")
	}
	// Same content → same hash (idempotent, reuseable).
	h1 := chunkHash("c", "/p.md", 0, "same")
	h2 := chunkHash("c", "/p.md", 0, "same")
	if h1 != h2 {
		t.Error("chunk hash must be stable for identical content")
	}
}

func mustSearch(t *testing.T, store *Store, collection, query string) []Result {
	t.Helper()
	res, err := store.Search(query, collection, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("search returned no results to rerank")
	}
	return res
}
