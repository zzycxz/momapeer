package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/zzycxz/momapeer/internal/rag"
)

// jiutianEmbedder is a rag.Embedder backed by the Jiutian (MoMA) platform's
// embedding API. It reuses the jiutian client's API key + base URL conventions.
// When the embedding endpoint or model isn't available, Embed returns an error
// and the caller (Store.Rerank) falls back to FTS5 ranking — so a missing/
// unsupported embedding capability degrades gracefully rather than breaking
// search.
//
// The endpoint path mirrors the OpenAI-style /embeddings convention; the actual
// Jiutian path may differ, in which case config.EmbeddingModel would be left
// empty and this embedder is never constructed (FTS5-only RAG). This keeps the
// hybrid path available where supported and invisible where not.
type jiutianEmbedder struct {
	model string
}

func (e jiutianEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	vecs, err := jiutianEmbed(ctx, e.model, texts)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	return vecs, nil
}

// jiutianEmbed calls the Jiutian embedding endpoint. Kept in a separate func so
// the embedder type stays focused on the rag.Embedder contract. The request/
// response shapes follow the common embedding-API convention.
func jiutianEmbed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	return jiutianEmbedRequest(ctx, model, texts)
}

// ResolveRAGEmbedder builds an embedder when [cowork] embedding_model is set,
// else returns nil (FTS5-only). Called from boot.go under the cowork profile.
func ResolveRAGEmbedder(model string) rag.Embedder {
	m := strings.TrimSpace(model)
	if m == "" {
		return nil
	}
	return jiutianEmbedder{model: m}
}
