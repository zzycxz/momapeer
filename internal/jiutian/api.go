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

// APICall is a shared helper for calling Jiutian platform APIs.
// It handles API key lookup, HTTP request creation, auth header, response
// status checking, and JSON response parsing. The result is unmarshaled
// into `out` (must be a pointer).
func APICall(ctx context.Context, method, path string, payload any, out any) error {
	apiKey := os.Getenv("JIUTIAN_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("JIUTIAN_API_KEY not set")
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
