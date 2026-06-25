package rag

import (
	"context"
	"math"
	"sort"
	"strings"
)

// Embedder produces a vector for a text string, enabling semantic reranking of
// FTS5 hits. Implementations call an embedding API (Jiutian/other). When nil,
// rag_search uses FTS5 alone. The interface is minimal so any provider can
// implement it; the store never assumes a dimension or model.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// rerankWithEmbeddings upgrades FTS5 results using semantic similarity: it
// embeds the query + each hit's chunk, recomputes scores as a blend of FTS5 BM25
// and cosine similarity, and re-sorts. If the embedder is nil or fails, it
// returns the original FTS5 ranking unchanged (graceful degradation — FTS5 is
// always a valid baseline).
//
// The blend weight (ftsWeight) favors FTS5 for exact-term recall while letting
// semantics surface topically-related hits that don't share exact words. This
// matches the typical hybrid-RAG finding: BM25 for precision, embeddings for
// recall, blend for both.
func (s *Store) Rerank(ctx context.Context, query string, results []Result, emb Embedder, ftsWeight float64) []Result {
	if emb == nil || len(results) == 0 {
		return results
	}
	if ftsWeight <= 0 {
		ftsWeight = 0.5
	}
	// Embed the query once, and all candidate chunks in one batch.
	texts := make([]string, 0, len(results)+1)
	texts = append(texts, query)
	for _, r := range results {
		texts = append(texts, r.Snippet)
	}
	vecs, err := emb.Embed(ctx, texts)
	if err != nil || len(vecs) != len(texts) {
		// Embedding failed — fall back to FTS5 ranking. Don't fail the whole search
		// over an embedding API hiccup; FTS5 results are still valid.
		return results
	}
	qVec := vecs[0]
	type scored struct {
		r    Result
		blend float64
	}
	out := make([]scored, len(results))
	maxBM25 := 0.0
	for _, r := range results {
		if r.Score > maxBM25 {
			maxBM25 = r.Score
		}
	}
	for i, r := range results {
		cos := cosine(qVec, vecs[i+1])
		// Normalize BM25 to [0,1] so it blends on the same scale as cosine.
		normBM25 := 0.0
		if maxBM25 > 0 {
			normBM25 = r.Score / maxBM25
		}
		blend := ftsWeight*normBM25 + (1-ftsWeight)*cos
		out[i] = scored{r: r, blend: blend}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].blend > out[j].blend })
	reranked := make([]Result, len(out))
	for i, o := range out {
		o.r.Score = o.blend
		reranked[i] = o.r
	}
	return reranked
}

// cosine computes the cosine similarity between two float32 vectors. Returns 0
// for zero vectors (avoids div-by-zero).
func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		af, bf := float64(a[i]), float64(b[i])
		dot += af * bf
		na += af * af
		nb += bf * bf
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// chunkForEmbed returns the text to embed for a chunk — currently the body, but
// kept as a helper so future metadata-augmented embeddings (title + body) stay
// localized. Unused for now; reserved for when rag_import stores embeddings.
func chunkForEmbed(body string) string {
	return strings.TrimSpace(body)
}
