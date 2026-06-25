package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zzycxz/momapeer/internal/tool"
)

// memoryQueryTool lets the model query the memory store with optional keyword
// search and time-point filtering. Unlike remember/forget it is read-only and
// does not mutate state.
type memoryQueryTool struct {
	store   Store
	service *SearchService
}

// NewMemoryQueryTool returns the `memory_query` tool. The service parameter is
// optional — if nil, keyword search falls back to store.List/ListAsOf.
func NewMemoryQueryTool(store Store, service *SearchService) tool.Tool {
	return memoryQueryTool{store: store, service: service}
}

func (memoryQueryTool) Name() string { return "memory_query" }

func (memoryQueryTool) Description() string {
	return "Search saved memories with optional keyword and time-point filtering. " +
		"Use this to answer questions like 'where did I live in March?' or 'what feedback has been saved?'. " +
		"Pass `query` for keyword search, `as_of` (YYYY-MM-DD) for a point-in-time query, or both. " +
		"Omit both to list all current active memories."
}

func (memoryQueryTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search keywords (optional). Omit to list all memories matching the time filter."},
			"as_of": {"type": "string", "description": "Point-in-time query, YYYY-MM-DD. Returns memories valid at that date. Omit for current active memories."}
		}
	}`)
}

func (t memoryQueryTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
		AsOf  string `json:"as_of"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	// Parse as_of date if given.
	var asOfTime time.Time
	if in.AsOf != "" {
		var err error
		asOfTime, err = time.Parse("2006-01-02", in.AsOf)
		if err != nil {
			return "", fmt.Errorf("invalid as_of date %q: use YYYY-MM-DD format", in.AsOf)
		}
	}

	// Case 1: keyword search with or without time filter.
	if in.Query != "" && t.service != nil {
		var results []SearchResult
		var err error
		if !asOfTime.IsZero() {
			results, err = t.service.SearchAsOf(in.Query, asOfTime)
		} else {
			results, err = t.service.Search(in.Query)
		}
		if err != nil {
			return "", err
		}
		return t.formatSearchResults(results, in.AsOf), nil
	}

	// Case 2: time-point query without keywords (or no FTS service).
	var memories []Memory
	if !asOfTime.IsZero() {
		memories = t.store.ListAsOf(asOfTime)
	} else {
		memories = t.store.List()
	}
	return t.formatMemoryList(memories, in.AsOf), nil
}

func (t memoryQueryTool) formatSearchResults(results []SearchResult, asOf string) string {
	if len(results) == 0 {
		if asOf != "" {
			return fmt.Sprintf("No memories found matching the query at %s.", asOf)
		}
		return "No memories found matching the query."
	}
	var out string
	if asOf != "" {
		out = fmt.Sprintf("Memories matching %q at %s (%d results):\n\n", results[0].Snippet, asOf, len(results))
	} else {
		out = fmt.Sprintf("Found %d memories:\n\n", len(results))
	}
	for _, r := range results {
		statusTag := ""
		if r.Status != "" && r.Status != "active" {
			statusTag = fmt.Sprintf(" [%s]", r.Status)
		}
		out += fmt.Sprintf("- **%s** (%s%s): %s\n", r.Name, r.Type, statusTag, r.Snippet)
	}
	return out
}

func (t memoryQueryTool) formatMemoryList(memories []Memory, asOf string) string {
	if len(memories) == 0 {
		if asOf != "" {
			return fmt.Sprintf("No active memories at %s.", asOf)
		}
		return "No active memories."
	}
	var out string
	if asOf != "" {
		out = fmt.Sprintf("Active memories at %s (%d):\n\n", asOf, len(memories))
	} else {
		out = fmt.Sprintf("Active memories (%d):\n\n", len(memories))
	}
	for _, m := range memories {
		validity := ""
		if m.ValidFrom != "" || m.ValidTo != "" {
			validity = fmt.Sprintf(" [%s → %s]", m.ValidFrom, m.ValidTo)
		}
		statusTag := ""
		if m.Status != "" && m.Status != "active" {
			statusTag = fmt.Sprintf(" [%s]", m.Status)
		}
		out += fmt.Sprintf("- **%s** (%s%s)%s: %s\n", m.Name, m.Type, statusTag, validity, oneLine(m.Description))
	}
	return out
}

func (memoryQueryTool) ReadOnly() bool { return true }
