package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zzycxz/momapeer/internal/tool"
)

// recallTool lets the model look up facts it saved earlier. Saved (scattered)
// memories are NOT injected into the per-turn prompt (only the portrait layer
// is), so without this tool the archive is a write-only black hole: remember can
// store a fact but nothing can fetch it back. recall is the read side — a pure
// filesystem scan with zero LLM cost.
//
// Two modes:
//   - No name (or empty): list every visible saved memory as "name — first line",
//     so the model knows what it has recorded and can pick one to read in full.
//   - With a name: return that memory's full body.
//
// Visibility follows the active profile partition (global + current mode), same
// as the rest of the store, so dev never sees cowork facts and vice versa.
type recallTool struct{ store Store }

// NewRecallTool returns the `recall` tool bound to store.
func NewRecallTool(store Store) tool.Tool { return recallTool{store: store} }

func (recallTool) Name() string { return "recall" }

func (recallTool) Description() string {
	return "Look up a fact previously saved with `remember`. Saved facts are NOT loaded into context automatically (only the portrait is), so use this when you need something you remember recording — a preference, a decision, a constraint. " +
		"Call with no name (or empty) to list everything you have saved (name + one-line hook); call with a `name` to read one fact's full body. " +
		"This is a local file read — it costs nothing and never calls a model."
}

func (recallTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "The slug of a saved memory to read in full. Omit or leave empty to list all saved memories (name + first line)."}
		}
	}`)
}

func (t recallTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Name string `json:"name"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
	}
	name := strings.TrimSpace(in.Name)

	// Single-fact read: return the full body.
	if name != "" {
		path := t.store.Path(name)
		if path == "" {
			return "", fmt.Errorf("no memory named %q", name)
		}
		m, ok := loadMemory(path)
		if !ok {
			return "", fmt.Errorf("memory %q not found", name)
		}
		body := strings.TrimSpace(m.Body)
		if body == "" {
			return fmt.Sprintf("(memory %q is empty)", name), nil
		}
		return body, nil
	}

	// List mode: name + first line for every visible memory.
	facts := t.store.List()
	if len(facts) == 0 {
		return "No saved memories yet. Use `remember` to save a durable fact.", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d saved memor%s:\n", len(facts), pluralMem(len(facts)))
	for _, m := range facts {
		hook := oneLine(firstLine(m.Body))
		if hook == "" {
			hook = oneLine(m.Body)
		}
		fmt.Fprintf(&b, "- %s — %s\n", m.Name, hook)
	}
	return strings.TrimSpace(b.String()), nil
}

func (recallTool) ReadOnly() bool { return true }

// pluralMem returns "y" for one, "ies" otherwise — for the list header.
func pluralMem(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
