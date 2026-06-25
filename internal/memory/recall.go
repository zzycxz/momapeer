package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zzycxz/momapeer/internal/tool"
)

// recallTool reactivates a dormant memory back to active status. When the model
// encounters a relevant dormant fact via memory_query, it can call this to bring
// it back into the hot layer (MEMORY.md index).
type recallTool struct{ store Store }

// NewRecallTool returns the `memory_recall` tool bound to store.
func NewRecallTool(store Store) tool.Tool { return recallTool{store: store} }

func (recallTool) Name() string { return "memory_recall" }

func (recallTool) Description() string {
	return "Reactivate a dormant memory fact back into the active index. " +
		"Use when memory_status shows dormant facts, or when the user mentions a past topic " +
		"that was dormant — e.g. a hobby or preference that is current once more. " +
		"The fact will reappear in the memory index and won't auto-decay again until its next inactivity period."
}

func (recallTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Slug of the dormant memory to reactivate, as shown in the search result."}
		},
		"required": ["name"]
	}`)
}

func (t recallTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if in.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	if err := t.store.Activate(in.Name); err != nil {
		return "", err
	}
	if q, ok := QueueFromContext(ctx); ok {
		q.QueueMemory("Reactivated dormant memory \"" + slug(in.Name) + "\" — it is now active again.")
	}
	return fmt.Sprintf("Reactivated memory %q — it is now active and will appear in the memory index.", in.Name), nil
}

func (recallTool) ReadOnly() bool { return false }
