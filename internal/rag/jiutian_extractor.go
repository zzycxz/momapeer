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
	BaseURL string // e.g. "https://jiutian.10086.cn/largemodel/moma/api/v3" ("" = default)
	APIKey  string // env var name to read the key from (e.g. "JIUTIAN_API_KEY"); if empty, reads JIUTIAN_API_KEY
	Model   string // chat model to use (e.g. the cowork main model)
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

// Extract calls /chat/completions with the chunk as the user message and parses
// the assistant's JSON response into entities + relations.
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

	userMsg := fmt.Sprintf(ExtractionPrompt, truncateChunk(chunk, 6000))
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
		return ExtractResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ExtractResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("extract http: %w", err)
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return ExtractResult{}, fmt.Errorf("extract HTTP %d: %s", resp.StatusCode, truncateStr(string(respBytes), 300))
	}

	var cr chatCompletionsResponse
	if err := json.Unmarshal(respBytes, &cr); err != nil {
		return ExtractResult{}, fmt.Errorf("parse chat response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return ExtractResult{}, fmt.Errorf("no choices in response")
	}
	content := cr.Choices[0].Message.Content
	// Some models wrap JSON in ```json fences — strip them.
	content = stripCodeFence(content)
	res, err := ParseExtractJSON([]byte(content))
	if err != nil {
		return ExtractResult{}, fmt.Errorf("parse extract output: %w (content: %s)", err, truncateStr(content, 200))
	}
	return res, nil
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
