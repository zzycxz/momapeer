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

// BaseURL is the Jiutian v3 API root.
const BaseURL = "https://jiutian.10086.cn/largemodel/moma/api/v3"

// Client is a shared HTTP client for all Jiutian API calls.
var Client = &http.Client{Timeout: 120 * time.Second}

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

func SetClient(c interface{}) {}
func SetBaseDomain(base string) {}
