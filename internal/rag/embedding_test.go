package rag

import (
	"context"
	"testing"
)

// fakeEmbedder returns canned vectors so Rerank is deterministic and testable
// without an embedding API. It maps texts to vectors by a simple key, so we can
// stage which hit "wins" after semantic reranking.
type fakeEmbedder struct {
	vecs map[string][]float32
}

func (f fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := f.vecs[t]; ok {
			out[i] = v
		} else {
			out[i] = []float32{0, 0, 0}
		}
	}
	return out, nil
}

func TestRerankReordersBySemanticSimilarity(t *testing.T) {
	store := &Store{} // Rerank doesn't touch the DB; nil db is fine.
	query := "rendering engine"
	// FTS5 ranking (BM25) put "unrelated doc" first; semantics should promote
	// the rendering-related doc because its embedding is near the query's.
	results := []Result{
		{Snippet: "unrelated doc", Score: 2.0}, // high BM25, low semantic
		{Snippet: "rendering engine docs", Score: 0.5},
	}
	emb := fakeEmbedder{vecs: map[string][]float32{
		query:                   {1, 0, 0},
		"rendering engine docs": {0.95, 0.1, 0},
		"unrelated doc":         {0, 1, 0}, // orthogonal to query
	}}
	reranked := store.Rerank(context.Background(), query, results, emb, 0.5)
	if reranked[0].Snippet != "rendering engine docs" {
		t.Errorf("rerank didn't promote semantic match: top = %q", reranked[0].Snippet)
	}
}

func TestRerankNilEmbedderIsNoOp(t *testing.T) {
	store := &Store{}
	results := []Result{{Snippet: "a", Score: 1}, {Snippet: "b", Score: 2}}
	reranked := store.Rerank(context.Background(), "q", results, nil, 0.5)
	if len(reranked) != 2 || reranked[0].Snippet != "a" {
		t.Errorf("nil embedder should pass through unchanged: %v", reranked)
	}
}

func TestCosine(t *testing.T) {
	// Identical vectors → 1.
	if got := cosine([]float32{1, 0, 0}, []float32{1, 0, 0}); got < 0.999 {
		t.Errorf("cosine identical = %v, want ~1", got)
	}
	// Orthogonal → 0.
	if got := cosine([]float32{1, 0}, []float32{0, 1}); got != 0 {
		t.Errorf("cosine orthogonal = %v, want 0", got)
	}
	// Zero vector → 0 (no div-by-zero).
	if got := cosine([]float32{0, 0}, []float32{1, 1}); got != 0 {
		t.Errorf("cosine zero = %v, want 0", got)
	}
	// Opposite → -1.
	if got := cosine([]float32{1, 0}, []float32{-1, 0}); got > -0.999 {
		t.Errorf("cosine opposite = %v, want ~-1", got)
	}
}
