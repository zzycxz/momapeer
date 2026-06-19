package skillstore

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zzycxz/momapeer/internal/netclient"
)

// Client talks to a ClawHub-compatible registry API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient creates a store client for the given base URL (e.g.
// "https://clawhub.ai/api/v1"). It reuses the momapeer proxy settings.
func NewClient(baseURL string) *Client {
	c, _ := netclient.NewHTTPClient(netclient.ProxySpec{}, netclient.TransportOptions{
		DialTimeout:         15 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	})
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: c,
	}
}

// ListSkills fetches the skill listing with pagination and sorting.
func (c *Client) ListSkills(ctx context.Context, opts ListOptions) (*ListResult, error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	if opts.Sort != "" {
		params.Set("sort", opts.Sort)
	}
	if opts.NonSuspiciousOnly {
		params.Set("nonSuspiciousOnly", "true")
	}
	var result ListResult
	if err := c.get(ctx, "/skills", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SearchSkills searches for skills matching the query.
func (c *Client) SearchSkills(ctx context.Context, query string, opts SearchOptions) (*SearchResult, error) {
	params := url.Values{}
	params.Set("q", query)
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.NonSuspiciousOnly {
		params.Set("nonSuspiciousOnly", "true")
	}
	var result SearchResult
	if err := c.get(ctx, "/search", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetSkillDetail fetches full detail for a single skill.
func (c *Client) GetSkillDetail(ctx context.Context, slug string) (*SkillDetail, error) {
	var result SkillDetail
	if err := c.get(ctx, "/skills/"+url.PathEscape(slug), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DownloadSkill downloads a skill ZIP and extracts its files. Returns a map of
// relative path → file content. The caller should confirm the ZIP contains a
// SKILL.md before installing.
func (c *Client) DownloadSkill(ctx context.Context, slug, version string) (map[string][]byte, error) {
	params := url.Values{}
	params.Set("slug", slug)
	if version != "" {
		params.Set("version", version)
	}
	reqURL := c.BaseURL + "/download?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", slug, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return extractZIP(data)
}

// get performs a GET request and JSON-decodes the response body.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	reqURL := c.BaseURL + path
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// extractZIP reads a ZIP archive from data and returns path→content.
func extractZIP(data []byte) (map[string][]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	files := map[string][]byte{}
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}
		// Strip the top-level directory from the zip path (common in GitHub exports).
		name := f.Name
		if i := strings.Index(name, "/"); i >= 0 && i < len(name)-1 {
			name = name[i+1:]
		}
		if name != "" {
			files[name] = content
		}
	}
	return files, nil
}
