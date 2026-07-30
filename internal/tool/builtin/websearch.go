package builtin

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

	"github.com/zzycxz/momapeer/internal/netclient"
	"github.com/zzycxz/momapeer/internal/tool"
)

func init() { tool.RegisterBuiltin(webSearch{}) }

// webSearchErrorMaxRead caps how much of a non-200 error response body we read
// into the returned error. A misbehaving/abusive search backend could stream
// a huge body; without a cap that unbounded read bloats memory. (The success
// path parses structured JSON, so it's not affected.)
const webSearchErrorMaxRead = 8 << 10 // 8 KiB

type webSearch struct {
	proxySpec netclient.ProxySpec
}

const webSearchTimeout = 15 * time.Second

func (webSearch) Name() string { return "web_search" }

func (webSearch) Description() string {
	return "Perform a web search to find current information, news, or reference material. It automatically uses available search engines (Brave, Exa, Linkup) in a fallback chain. Returns a formatted markdown list of search results including titles, URLs, and snippets."
}

func (webSearch) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "query":{"type":"string","description":"The search query string."}
},
"required":["query"]
}`)
}

func (webSearch) ReadOnly() bool { return true }

func (ws webSearch) proxyURLFor(req *http.Request) (string, error) {
	return netclient.ProxyURLFor(ws.proxySpec, req)
}

type searchResultItem struct {
	Title   string
	URL     string
	Snippet string
}

func (ws webSearch) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.Query) == "" {
		return "", fmt.Errorf("query is required")
	}

	client := ssrfGuardedClient(ws.proxyURLFor)

	var lastErr error
	var results []searchResultItem
	var engineUsed string

	// 1. Try Brave Search
	if key := os.Getenv("BRAVE_API_KEY"); key == "" {
		key = os.Getenv("BRAVE_SEARCH_API_KEY")
		if key != "" {
			results, lastErr = searchBrave(ctx, client, key, p.Query)
			if lastErr == nil {
				engineUsed = "Brave Search"
				goto Render
			}
		}
	} else if key != "" {
		results, lastErr = searchBrave(ctx, client, key, p.Query)
		if lastErr == nil {
			engineUsed = "Brave Search"
			goto Render
		}
	}

	// 2. Try Exa
	if key := os.Getenv("EXA_API_KEY"); key != "" {
		results, lastErr = searchExa(ctx, client, key, p.Query)
		if lastErr == nil {
			engineUsed = "Exa"
			goto Render
		}
	}

	// 3. Try Linkup
	if key := os.Getenv("LINKUP_API_KEY"); key != "" {
		results, lastErr = searchLinkup(ctx, client, key, p.Query)
		if lastErr == nil {
			engineUsed = "Linkup"
			goto Render
		}
	}

	if lastErr != nil {
		return "", fmt.Errorf("all configured search engines failed. Last error: %w", lastErr)
	}
	return "", fmt.Errorf("no search engine API keys configured. Please set BRAVE_API_KEY, EXA_API_KEY, or LINKUP_API_KEY")

Render:
	if len(results) == 0 {
		return fmt.Sprintf("No results found for %q using %s.", p.Query, engineUsed), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Search results for %q (via %s):\n\n", p.Query, engineUsed)
	for i, r := range results {
		fmt.Fprintf(&sb, "### %d. [%s](%s)\n", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&sb, "> %s\n", strings.ReplaceAll(strings.TrimSpace(r.Snippet), "\n", "\n> "))
		}
		sb.WriteString("\n")
	}
	// Wrap as untrusted content so the model treats result titles/snippets
	// (which are attacker-controllable web page content) as data, not
	// instructions — same defense as web_fetch and rag_search.
	return WrapUntrusted("web", sb.String()), nil
}

func searchBrave(ctx context.Context, client *http.Client, key, query string) ([]searchResultItem, error) {
	reqCtx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", "https://api.search.brave.com/res/v1/web/search", nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Add("q", query)
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", key)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, webSearchErrorMaxRead))
		return nil, fmt.Errorf("brave search returned status %d: %s", resp.StatusCode, string(body))
	}

	var data struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var out []searchResultItem
	for _, r := range data.Web.Results {
		out = append(out, searchResultItem{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Description,
		})
	}
	return out, nil
}

func searchExa(ctx context.Context, client *http.Client, key, query string) ([]searchResultItem, error) {
	reqCtx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()

	payload := map[string]any{
		"query": query,
		"type":  "auto",
		"contents": map[string]any{
			"highlights": true,
		},
	}
	bodyData, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(reqCtx, "POST", "https://api.exa.ai/search", bytes.NewReader(bodyData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, webSearchErrorMaxRead))
		return nil, fmt.Errorf("exa search returned status %d: %s", resp.StatusCode, string(body))
	}

	var data struct {
		Results []struct {
			Title      string   `json:"title"`
			URL        string   `json:"url"`
			Text       string   `json:"text"`
			Highlights []string `json:"highlights"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var out []searchResultItem
	for _, r := range data.Results {
		snippet := r.Text
		if len(r.Highlights) > 0 {
			snippet = strings.Join(r.Highlights, " ... ")
		}
		if len(snippet) > 800 {
			snippet = snippet[:800] + "..."
		}
		out = append(out, searchResultItem{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: snippet,
		})
	}
	return out, nil
}

func searchLinkup(ctx context.Context, client *http.Client, key, query string) ([]searchResultItem, error) {
	reqCtx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()

	payload := map[string]any{
		"q":          query,
		"depth":      "standard",
		"outputType": "searchResults",
	}
	bodyData, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(reqCtx, "POST", "https://api.linkup.so/v1/search", bytes.NewReader(bodyData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, webSearchErrorMaxRead))
		return nil, fmt.Errorf("linkup search returned status %d: %s", resp.StatusCode, string(body))
	}

	var data struct {
		Results []struct {
			Name    string `json:"name"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var out []searchResultItem
	for _, r := range data.Results {
		snippet := r.Content
		if len(snippet) > 800 {
			snippet = snippet[:800] + "..."
		}
		out = append(out, searchResultItem{
			Title:   r.Name,
			URL:     r.URL,
			Snippet: snippet,
		})
	}
	return out, nil
}
