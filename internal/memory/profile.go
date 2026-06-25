package memory

import (
	"context"
	"encoding/json"

	"github.com/zzycxz/momapeer/internal/tool"
)

// profileTool outputs a structured user profile grouped by category.
type profileTool struct{ store Store }

// NewProfileTool returns the `memory_profile` tool.
func NewProfileTool(store Store) tool.Tool { return profileTool{store: store} }

func (profileTool) Name() string { return "memory_profile" }

func (profileTool) Description() string {
	return "Output a structured user profile grouped by category (identity, style, belief, temporal, feedback). " +
		"Use when the user asks 'what do you know about me' or 'review my preferences'."
}

func (profileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`)
}

func (t profileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	out := t.store.ProfileBlock()
	if out == "" {
		return "No active memories recorded yet.", nil
	}
	return out, nil
}

func (profileTool) ReadOnly() bool { return true }
