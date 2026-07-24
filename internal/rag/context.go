package rag

import (
	"fmt"
	"strings"
	"sync"
)

// FormatKnowledgeRef formats selected entities and relations into a markdown
// document suitable for passing to a skill as context. The output is structured
// as: entity list (name, type, description) then relation list (source → type → target).
func FormatKnowledgeRef(store *Store, collection string, entityNames []string, relationKeys []string) string {
	var b strings.Builder
	b.WriteString("# 知识参考\n\n")

	// Entities section.
	if len(entityNames) > 0 {
		b.WriteString("## 实体\n\n")
		for _, name := range entityNames {
			ents, _ := store.SearchEntities(name, collection, 1)
			if len(ents) == 0 {
				continue
			}
			e := ents[0]
			typeStr := ""
			if e.Type != "" {
				typeStr = fmt.Sprintf(" (%s)", e.Type)
			}
			fmt.Fprintf(&b, "- **%s**%s", e.NameRaw, typeStr)
			if e.Description != "" {
				fmt.Fprintf(&b, ": %s", e.Description)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Relations section.
	if len(relationKeys) > 0 {
		b.WriteString("## 关系\n\n")
		for _, key := range relationKeys {
			// relationKeys format: "source→type→target"
			parts := strings.SplitN(key, "→", 3)
			if len(parts) != 3 {
				continue
			}
			src, typ, tgt := parts[0], parts[1], parts[2]
			// Look up relation for description.
			srcNorm := normalizeName(src)
			tgtNorm := normalizeName(tgt)
			rels, _ := store.RelationsOf(collection, srcNorm, true)
			desc := ""
			for _, r := range rels {
				if normalizeName(r.Source) == srcNorm && normalizeName(r.Target) == tgtNorm && r.Type == typ {
					desc = r.Description
					break
				}
			}
			fmt.Fprintf(&b, "- %s → %s → %s", src, typ, tgt)
			if desc != "" {
				fmt.Fprintf(&b, " (%s)", desc)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// RelationKey formats a relation as a unique key string for selection tracking.
func RelationKey(source, typ, target string) string {
	return fmt.Sprintf("%s→%s→%s", source, typ, target)
}

// SessionRAGContext tracks which collections are active for the current session.
// When non-empty, rag_search auto-scopes to these collections.
// Empty = search all collections (default behavior).
type SessionRAGContext struct {
	mu                sync.RWMutex
	activeCollections []string
}

// NewSessionRAGContext creates a new session context.
func NewSessionRAGContext() *SessionRAGContext {
	return &SessionRAGContext{}
}

// SetActiveCollections sets the active collections for this session.
// Pass nil or empty to search all.
func (c *SessionRAGContext) SetActiveCollections(collections []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeCollections = collections
}

// GetActiveCollections returns the currently active collections.
func (c *SessionRAGContext) GetActiveCollections() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, len(c.activeCollections))
	copy(out, c.activeCollections)
	return out
}

// ResolveCollection returns the effective collection scope for a search.
// If the caller specifies an explicit collection, it takes precedence.
// Otherwise, returns the session's active collections (may be empty = all).
func (c *SessionRAGContext) ResolveCollection(explicit string) string {
	if explicit != "" {
		return explicit
	}
	active := c.GetActiveCollections()
	if len(active) == 1 {
		return active[0]
	}
	// Multiple active collections or none: caller must handle iteration.
	return ""
}

// ActiveCollectionsOrAll returns the active collections, or a single empty
// string (meaning "all") when none are set.
func (c *SessionRAGContext) ActiveCollectionsOrAll() []string {
	active := c.GetActiveCollections()
	if len(active) == 0 {
		return []string{""} // empty string = all collections
	}
	return active
}
