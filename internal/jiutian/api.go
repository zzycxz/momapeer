// Package jiutian provides shared helpers for calling China Mobile's Jiutian
// (MoMA) platform APIs. Both the tool layer (image_understand, video_understand)
// and the provider layer (vision image description) import this package to avoid
// duplicating the HTTP call pattern.
package jiutian

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

// defaultBaseURL is the Jiutian v3 API root. Kept as a const so the original
// BaseURL value is preserved for reference; BaseURL (a var) is what APICall
// actually uses and is overridable via SetBaseDomain.
const defaultBaseURL = "https://jiutian.10086.cn/largemodel/moma/api/v3"

// BaseURL is the Jiutian v3 API root. It's a var (not a const) so a private
// deployment or proxy can override it at boot via SetBaseDomain.
var BaseURL = defaultBaseURL

// Client is a shared HTTP client for all Jiutian API calls.
var Client = &http.Client{Timeout: 120 * time.Second}

// SetClient replaces the shared HTTP client. boot.go calls this so Jiutian API
// calls go through the same configured client (proxy, timeouts) as the rest of
// the app; without it APICall uses the default 120s client and may fail with
// EOF in proxy-only environments.
func SetClient(c *http.Client) {
	if c != nil {
		Client = c
	}
}

// SetBaseDomain overrides the Jiutian API base URL. Pass the full base
// (e.g. "https://jiutian.example.cn/largemodel/moma/api/v3"); an empty value
// resets to the default. boot.go derives the value from config so a private
// deployment or proxy can redirect Jiutian calls without code changes.
func SetBaseDomain(base string) {
	base = strings.TrimSpace(base)
	if base == "" {
		BaseURL = defaultBaseURL
		return
	}
	BaseURL = base
}

// BudgetAcquirer gates a request through the global RPM limiter. It's the
// subset of *provider.RequestBudget this package needs, exposed as an
// interface to avoid a jiutian→provider import cycle (provider/openai imports
// jiutian). boot.go injects the real *provider.RequestBudget at startup; it
// satisfies this interface via RequestBudget.Acquire.
type BudgetAcquirer interface {
	Acquire(ctx context.Context, key string, priority bool) error
}

var (
	// budget, when set, gates all Jiutian LLM-class calls (image/text,
	// video/text, images/generations, embeddings) through the global RPM limiter
	// so tools and RAG share the user's per-minute quota. nil = no limiting.
	budget BudgetAcquirer
	// budgetKey is the budget bucket key (baseURL + resolved API key) shared by
	// all direct Jiutian calls, so they draw from one RPM quota.
	budgetKey string
)

// SetBudget installs the global RPM limiter for all Jiutian platform LLM calls.
// boot calls this after building globalBudget; pass nil to disable. key should
// be a stable string identifying the Jiutian API key + base URL (the same key
// form boot uses elsewhere) so all direct Jiutian calls share one quota.
func SetBudget(b BudgetAcquirer, key string) {
	budget = b
	budgetKey = key
}

// isLLMPath reports whether path targets an LLM-class Jiutian endpoint that
// counts against the platform's RPM (and thus should be gated by the budget).
// File-storage paths (/fs/*) are excluded — they are not LLM calls.
func isLLMPath(path string) bool {
	switch path {
	case "/image/text", "/video/text", "/images/generations", "/embeddings", "/chat/completions":
		return true
	}
	return false
}

// APICall is a shared helper for calling Jiutian platform APIs.
// It handles API key lookup, HTTP request creation, auth header, response
// status checking, and JSON response parsing. The result is unmarshaled
// into `out` (must be a pointer).
func APICall(ctx context.Context, method, path string, payload any, out any) error {
	apiKey := os.Getenv("JIUTIAN_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("JIUTIAN_API_KEY not set")
	}

	// Gate LLM-class calls (image/video/embeddings/generations) through the
	// global RPM limiter so multimodal tools, RAG embedding, and the VLM
	// fallback share the user's per-minute quota with the main conversation.
	// Background priority (false) so they don't starve interactive requests
	// when reserve_main is configured. File-storage paths (/fs/*) skip this.
	if budget != nil && isLLMPath(path) {
		if err := budget.Acquire(ctx, budgetKey, false); err != nil {
			return fmt.Errorf("jiutian %s rate-limited: %w", path, err)
		}
	}

	var reqBody *bytes.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(body)
	} else {
		reqBody = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, BaseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := Client.Do(req)
	if err != nil {
		return fmt.Errorf("jiutian %s: %w", path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		errMsg := Truncate(string(respBody), 300)
		return fmt.Errorf("jiutian %s HTTP %d: %s", path, resp.StatusCode, errMsg)
	}

	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}
	}
	return nil
}

// Truncate shortens s to n bytes, snapping to the last space to avoid
// cutting mid-word. Returns s unchanged if it's already short enough.
func Truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > n/2 && cut < len(s) && s[cut] != ' ' {
		cut--
	}
	if cut <= n/2 {
		cut = n
	}
	return s[:cut] + "..."
}
