package rag

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
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
	model := embedModelName(emb)

	// Resolve each candidate chunk's vector, preferring the on-disk cache so a
	// repeated search over the same corpus doesn't re-call the embedding API.
	// The query is always embedded fresh (never cached — it's unique per call).
	chunkVecs := make([][]float32, len(results))
	var missIdx []int
	for i, r := range results {
		h := chunkHash(r.Collection, r.Path, r.Chunk, r.Snippet)
		if v, ok := s.getEmbedding(h, model); ok {
			chunkVecs[i] = v
			continue
		}
		missIdx = append(missIdx, i)
	}
	// Embed the query + only the cache-missing chunks in one batch.
	texts := make([]string, 0, len(missIdx)+1)
	texts = append(texts, query)
	for _, i := range missIdx {
		texts = append(texts, results[i].Snippet)
	}
	vecs, err := emb.Embed(ctx, texts)
	if err != nil || len(vecs) != len(texts) {
		// Embedding failed — fall back to FTS5 ranking. Don't fail the whole search
		// over an embedding API hiccup; FTS5 results are still valid.
		return results
	}
	qVec := vecs[0]
	for j, i := range missIdx {
		v := vecs[1+j]
		chunkVecs[i] = v
		// Persist for next time. Best-effort: a write failure just means we
		// re-embed later, it never breaks the current search.
		h := chunkHash(results[i].Collection, results[i].Path, results[i].Chunk, results[i].Snippet)
		_ = s.putEmbedding(h, model, v)
	}
	type scored struct {
		r     Result
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
		cos := cosine(qVec, chunkVecs[i])
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

// chunkHash returns a stable content key for a chunk's embedding cache entry.
// It folds in the collection/path/chunk index AND the snippet body, so editing
// a document (changed body) naturally invalidates the cache on re-import, while
// an unchanged chunk queried repeatedly reuses its vector forever.
func chunkHash(collection, path string, chunk int, body string) string {
	h := sha256.New()
	h.Write([]byte(collection))
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	var idx [4]byte
	binary.LittleEndian.PutUint32(idx[:], uint32(chunk))
	h.Write(idx[:])
	h.Write([]byte{0})
	h.Write([]byte(body))
	return string(h.Sum(nil))
}

// embedModelName returns a stable per-model cache key. The default embedder has
// no model string exposed, so we fall back to a constant name; real models
// (e.g. the jiutian embedder) can expose one via an optional interface.
func embedModelName(emb Embedder) string {
	if m, ok := emb.(interface{ Model() string }); ok {
		if name := m.Model(); name != "" {
			return name
		}
	}
	return "default"
}

// getEmbedding reads a cached vector for (chunkHash, model). ok=false on miss
// OR when the store has no DB (e.g. an in-memory Store{} used in unit tests),
// so Rerank degrades gracefully to embedding everything fresh.
func (s *Store) getEmbedding(chunkHash, model string) ([]float32, bool) {
	if s == nil || s.db == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var blob []byte
	err := s.db.QueryRow(`SELECT vec FROM rag_embeddings WHERE chunk_hash = ? AND model = ?`, chunkHash, model).Scan(&blob)
	if err != nil {
		return nil, false
	}
	return decodeVec(blob), true
}

// putEmbedding persists a vector for (chunkHash, model), replacing any prior.
// No-op when the store has no DB.
func (s *Store) putEmbedding(chunkHash, model string, vec []float32) error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO rag_embeddings (chunk_hash, model, vec) VALUES (?, ?, ?)`,
		chunkHash, model, encodeVec(vec))
	return err
}

// encodeVec packs a float32 slice into a little-endian byte buffer.
func encodeVec(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(f))
	}
	return buf
}

// decodeVec reverses encodeVec.
func decodeVec(b []byte) []float32 {
	n := len(b) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

// ClearEmbeddings drops the whole embedding cache (e.g. on a collection clear,
// since stale entries for deleted chunks would otherwise linger forever).
func (s *Store) ClearEmbeddings() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM rag_embeddings`)
	return err
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
func chunkForEmbed(body string) string { //nolint:unused
	return strings.TrimSpace(body)
}
