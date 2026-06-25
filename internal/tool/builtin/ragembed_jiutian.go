package builtin

import (
	"context"

	"github.com/zzycxz/momapeer/internal/jiutian"
)

// jiutianEmbedRequest calls the Jiutian embedding endpoint via the shared
// jiutian.APICall helper (handles API key, auth, HTTP). Request/response shapes
// follow the common embedding-API convention (OpenAI-style): input is a list of
// texts, data[].embedding is a list of float32 vectors. If the platform doesn't
// expose this path/model, APICall returns an HTTP error and the caller falls
// back to FTS5 — search still works.
func jiutianEmbedRequest(ctx context.Context, model string, texts []string) ([][]float32, error) {
	req := struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}{Model: model, Input: texts}
	var resp struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := jiutian.APICall(ctx, "POST", "/embeddings", req, &resp); err != nil {
		return nil, err
	}
	out := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		out[i] = d.Embedding
	}
	return out, nil
}
