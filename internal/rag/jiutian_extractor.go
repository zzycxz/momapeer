package rag

// jiutian_extractor.go is the default rag.Extractor implementation: it calls
// the 九天 (Jiutian/MoMA) /chat/completions endpoint with a JSON-output
// instruction and parses the structured entities+relations out of the response.
//
// This lives in the rag package (not internal/tool/builtin) so it's reusable by
// the Pipeline without a circular dep on the tool layer. It depends only on
// the shared jiutian HTTP helper.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// JiutianExtractorConfig configures the LLM-backed extractor.
type JiutianExtractorConfig struct {
	BaseURL  string // e.g. "https://jiutian.10086.cn/largemodel/moma/api/v3" ("" = default)
	APIKey   string // env var name to read the key from (e.g. "JIUTIAN_API_KEY"); if empty, reads JIUTIAN_API_KEY
	Model    string // chat model to use (e.g. the cowork main model)
	TwoStage bool   // extract entities then relations in two LLM calls (higher quality, 2× tokens); false = single combined call
}

// jiutianExtractor implements rag.Extractor via /chat/completions + JSON mode.
type jiutianExtractor struct {
	cfg     JiutianExtractorConfig
	baseURL string
	client  *http.Client
}

// NewJiutianExtractor builds the default extractor. The cfg passed from boot.go
// carries the model from [cowork] extract_model (or the main model when empty).
func NewJiutianExtractor(cfg JiutianExtractorConfig) Extractor {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = "https://jiutian.10086.cn/largemodel/moma/api/v3"
	}
	return &jiutianExtractor{
		cfg:     cfg,
		baseURL: base,
		client:  &http.Client{Timeout: 90 * time.Second},
	}
}

// Extract calls /chat/completions and parses entities + relations out of the
// assistant's JSON response. In two-stage mode (cfg.TwoStage) it runs two calls
// — entities first, then relations seeded with the extracted entities as
// {known_nodes} — which forces relation endpoints to be real entities and cuts
// hallucinated edges (mirrors HE graph.py:510). Two-stage costs ~2× tokens but
// markedly improves graph quality; single-stage is the cheaper fallback.
func (e *jiutianExtractor) Extract(ctx context.Context, chunk string) (ExtractResult, error) {
	if strings.TrimSpace(chunk) == "" {
		return ExtractResult{}, nil
	}
	if e.cfg.Model == "" {
		return ExtractResult{}, fmt.Errorf("extract model not configured (set [cowork] extract_model)")
	}
	apiKey := resolveKey(e.cfg.APIKey)
	if apiKey == "" {
		return ExtractResult{}, fmt.Errorf("LLM api key not set (JIUTIAN_API_KEY)")
	}

	text := truncateChunk(chunk, 6000)

	if !e.cfg.TwoStage {
		// Single combined call (original behavior).
		content, err := e.chatJSON(ctx, apiKey, fmt.Sprintf(ExtractionPrompt, text))
		if err != nil {
			return ExtractResult{}, err
		}
		res, err := ParseExtractJSON([]byte(content))
		if err != nil {
			return ExtractResult{}, fmt.Errorf("parse extract output: %w (content: %s)", err, truncateStr(content, 200))
		}
		return res, nil
	}

	// Two-stage: entities → relations seeded with the entity list.
	nodeContent, err := e.chatJSON(ctx, apiKey, fmt.Sprintf(NodeExtractionPrompt, text))
	if err != nil {
		return ExtractResult{}, fmt.Errorf("stage1 (nodes): %w", err)
	}
	res, err := parseNodesJSON([]byte(nodeContent))
	if err != nil {
		// Fall back to a single-stage parse so a stage-1 format glitch doesn't
		// waste the call entirely.
		res, err = ParseExtractJSON([]byte(nodeContent))
		if err != nil {
			return ExtractResult{}, fmt.Errorf("parse stage1 nodes: %w (content: %s)", err, truncateStr(nodeContent, 200))
		}
	}
	if len(res.Entities) == 0 {
		return res, nil // no entities → no relations to extract
	}
	knownNodes := formatKnownNodes(res.Entities)
	edgeContent, err := e.chatJSON(ctx, apiKey, fmt.Sprintf(EdgeExtractionPrompt, knownNodes, text))
	if err != nil {
		// Stage 2 failed: keep the entities we got, skip relations.
		return res, fmt.Errorf("stage2 (edges): %w", err)
	}
	rels, err := parseRelationsJSON([]byte(edgeContent))
	if err != nil {
		// Keep entities; relations just empty.
		return res, nil
	}
	res.Relations = rels
	return res, nil
}

// chatJSON sends one user message and returns the assistant's content string
// (JSON fences stripped). Shared by both single- and two-stage paths.
func (e *jiutianExtractor) chatJSON(ctx context.Context, apiKey, userMsg string) (string, error) {
	reqBody := chatCompletionsRequest{
		Model: e.cfg.Model,
		Messages: []chatMessage{
			{Role: "user", Content: userMsg},
		},
		Temperature:    0,
		ResponseFormat: &responseFormat{Type: "json_object"},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("extract http: %w", err)
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("extract HTTP %d: %s", resp.StatusCode, truncateStr(string(respBytes), 300))
	}
	var cr chatCompletionsResponse
	if err := json.Unmarshal(respBytes, &cr); err != nil {
		return "", fmt.Errorf("parse chat response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return stripCodeFence(cr.Choices[0].Message.Content), nil
}

// formatKnownNodes renders the stage-1 entity list as the bullet list injected
// into the stage-2 prompt as {known_nodes} (HE graph.py:611 uses the same
// "- name\n- name" shape). Uses the raw display name so the model can match it
// verbatim against the text.
func formatKnownNodes(entities []Entity) string {
	if len(entities) == 0 {
		return "（本段未识别到具体实体）"
	}
	var b strings.Builder
	for _, e := range entities {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(e.NameRaw))
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// parseNodesJSON parses a stage-1 (entities-only) LLM response into an
// ExtractResult carrying just entities. Tolerates the canonical {entities:[...]}
// shape even if a relations field is also present (we ignore it).
func parseNodesJSON(b []byte) (ExtractResult, error) {
	var raw struct {
		Entities []struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return ExtractResult{}, fmt.Errorf("parse nodes json: %w", err)
	}
	res := ExtractResult{}
	for _, e := range raw.Entities {
		res.Entities = append(res.Entities, Entity{
			NameRaw:     e.Name,
			Type:        e.Type,
			Description: e.Description,
		})
	}
	return res, nil
}

// parseRelationsJSON parses a stage-2 (relations-only) LLM response.
func parseRelationsJSON(b []byte) ([]Relation, error) {
	var raw struct {
		Relations []struct {
			Source      string `json:"source"`
			Target      string `json:"target"`
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"relations"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("parse relations json: %w", err)
	}
	var out []Relation
	for _, r := range raw.Relations {
		out = append(out, Relation{
			Source:      r.Source,
			Target:      r.Target,
			Type:        r.Type,
			Description: r.Description,
		})
	}
	return out, nil
}

// resolveKey reads the API key from the named env var (default JIUTIAN_API_KEY).
func resolveKey(envName string) string {
	if envName == "" {
		envName = "JIUTIAN_API_KEY"
	}
	return strings.TrimSpace(os.Getenv(envName))
}

func truncateChunk(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// stripCodeFence removes ```json ... ``` wrappers that some models add despite
// the json_object instruction.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	for _, fence := range []string{"```json", "```JSON", "```"} {
		if strings.HasPrefix(s, fence) {
			s = strings.TrimPrefix(s, fence)
			break
		}
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}

// --- request/response shapes (OpenAI-compatible /chat/completions) ----------

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"` // "json_object" | "text"
}

type chatCompletionsRequest struct {
	Model          string           `json:"model"`
	Messages       []chatMessage    `json:"messages"`
	Temperature    float64          `json:"temperature"`
	ResponseFormat *responseFormat  `json:"response_format,omitempty"`
}

type chatCompletionsResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}
