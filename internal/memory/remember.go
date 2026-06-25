package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zzycxz/momapeer/internal/tool"
)

// rememberTool lets the model persist a durable fact to the auto-memory store.
// It is stateful (bound to one project's Store), so boot constructs it and adds
// it to the registry — the same pattern as the task tool — rather than
// self-registering as a stateless built-in. When detector is set, new facts
// that contradict existing ones automatically supersede the old record.
type rememberTool struct {
	store    Store
	detector ConflictDetector
}

// NewRememberTool returns the `remember` tool bound to store. A zero/disabled
// store yields a tool that reports the store is unavailable rather than silently
// dropping saves. detector is optional — pass nil to skip conflict detection.
func NewRememberTool(store Store, detector ConflictDetector) tool.Tool {
	return rememberTool{store: store, detector: detector}
}

func (rememberTool) Name() string { return "remember" }

func (rememberTool) Description() string {
	return "Save a durable fact to project memory so it survives across sessions. " +
		"Use for things worth remembering long-term: who the user is and their preferences (type \"user\"); " +
		"guidance on how to work, including the why (type \"feedback\"); ongoing goals or constraints not " +
		"derivable from the code (type \"project\"); or pointers to external resources (type \"reference\"). " +
		"For feedback/project, structure the body with a \"**Why:**\" line and a \"**How to apply:**\" line so the fact is actionable later; " +
		"link related memories inline with [[their-name]]. " +
		"Do NOT save what the repo already records (code structure, git history) or facts that only matter to the current conversation; " +
		"if asked to remember one of those, save instead the non-obvious point behind it. " +
		"Before saving, check the loaded memory index for an entry that already covers this — reuse that name to update it rather than create a near-duplicate, and use `forget` to drop one that is now wrong. " +
		"When a fact has a time boundary (e.g. user says \"3月在北京\"), set valid_from/valid_to in YYYY-MM-DD format. " +
		"The system will automatically supersede older conflicting records. " +
		"The saved index loads into context at the start of each session."
}

func (rememberTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Short kebab-case slug identifying the fact, e.g. \"prefers-tabs\". Reusing a name overwrites that memory — do that to update an existing fact. Omit to derive one from the description."},
			"title": {"type": "string", "description": "Short human-readable label shown in the memory index, e.g. \"Prefers tabs\". Omit to derive one from the name."},
			"description": {"type": "string", "description": "One-line hook shown in the index — the phrase a future session reads to decide whether to open this memory. Make it specific."},
			"type": {"type": "string", "enum": ["user", "feedback", "project", "reference"], "description": "Category of the fact."},
			"body": {"type": "string", "description": "The fact itself (Markdown). For feedback/project, include a \"**Why:**\" line and a \"**How to apply:**\" line; link related memories with [[their-name]]."},
			"valid_from": {"type": "string", "description": "When this fact becomes/became true, YYYY-MM-DD. E.g. user says '3月在北京' → '2026-03-01'. Omit for timeless facts."},
			"valid_to": {"type": "string", "description": "When this fact stops/stopped being true, YYYY-MM-DD. Empty = currently true. The system auto-sets this when a newer fact supersedes this one."},
			"ttl": {"type": "string", "description": "Auto-archive date, YYYY-MM-DD. The memory is automatically archived when this date passes. Use for time-bounded facts like weekly goals. Omit for durable facts."},
			"importance": {"type": "string", "enum": ["high", "medium", "low"], "description": "Decay resistance: 'high' = never auto-decays, 'medium' = standard decay (default), 'low' = decays twice as fast."},
			"category": {"type": "string", "enum": ["identity", "style", "belief", "temporal", "feedback"], "description": "Profile bucket for type=\"user\" facts, used by memory_profile to group the user's profile: identity (who they are: role, name, residence), style (work preferences, communication style), belief (technical opinions), temporal (time-sensitive attributes), feedback (guidance to you). Omit for non-user facts."},
			"tags": {"type": "array", "items": {"type": "string"}, "description": "Free-form labels for grouping/filtering, e.g. [\"backend\", \"go\"]. Optional."}
		},
		"required": ["description", "body"]
	}`)
}

func (t rememberTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Name        string   `json:"name"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Type        string   `json:"type"`
		Body        string   `json:"body"`
		ValidFrom   string   `json:"valid_from"`
		ValidTo     string   `json:"valid_to"`
		TTL         string   `json:"ttl"`
		Importance  string   `json:"importance"`
		Category    string   `json:"category"`
		Tags        []string `json:"tags"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if in.Description == "" || in.Body == "" {
		return "", fmt.Errorf("description and body are required")
	}
	name := in.Name
	if name == "" {
		name = in.Title // Save slugifies; the title (or, below, the description) makes a serviceable slug
	}
	if name == "" {
		name = in.Description
	}

	newMem := Memory{
		Name:        name,
		Title:       in.Title,
		Description: in.Description,
		Type:        NormalizeType(in.Type),
		Body:        in.Body,
		ValidFrom:   in.ValidFrom,
		ValidTo:     in.ValidTo,
		TTL:         in.TTL,
		Importance:  in.Importance,
		Category:    NormalizeCategory(in.Category),
		Tags:        in.Tags,
	}

	// Conflict detection via LLM. We scan ALL active memories of the same type,
	// not just the same-name one, so that a different-name contradiction is
	// caught — e.g. "住北京" (name=address) vs "住上海" (name=location) must not
	// both stay active. The detector itself short-circuits non-user/project
	// types (see conflict.go), so this loop is effectively bounded to the types
	// that carry mutable real-world facts.
	//
	// We stop at the first detected conflict: one new fact should obsolete at
	// most one old one in practice, and bounding the LLM calls keeps remember
	// latency predictable. Save() still handles same-name supersede inline, so
	// if the conflicting record shares the new name we skip it here to avoid
	// double-processing.
	if t.detector != nil {
		newName := slug(name)
		for _, old := range t.store.ListActiveByType(newMem.Type) {
			if old.Name == newName {
				continue // Save() handles same-name inline
			}
			if !t.detector.Detect(ctx, old, newMem) {
				continue
			}
			validTo := newMem.ValidFrom
			if validTo == "" {
				validTo = time.Now().UTC().Format("2006-01-02")
			}
			// Force-set SupersededBy so the chain can never break (plan 1.5).
			if err := t.store.Supersede(old.Name, validTo, newName); err == nil {
				newMem.Supersedes = old.Name
			}
			break // first conflict wins
		}
	}

	path, err := t.store.Save(newMem)
	if err != nil {
		return "", err
	}
	if q, ok := QueueFromContext(ctx); ok {
		q.QueueMemory("Saved memory \"" + slug(name) + "\": " + oneLine(in.Description))
	}
	return fmt.Sprintf("Saved memory to %s (it applies now and loads automatically in future sessions).", path), nil
}

func (rememberTool) ReadOnly() bool { return false }
