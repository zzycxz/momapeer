package rag

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ExportToObsidian exports a collection's knowledge graph as an Obsidian vault.
// Each entity becomes a markdown file with YAML front matter, wikilinks for
// relations, backlinks, and source citations. A MOC (_目录.md) is generated.
func ExportToObsidian(store *Store, collection, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	entities, err := store.SearchEntities("", collection, 10000)
	if err != nil {
		return fmt.Errorf("list entities: %w", err)
	}
	// Build name→NameRaw lookup for wikilink targets.
	nameToRaw := make(map[string]string, len(entities))
	for _, e := range entities {
		nameToRaw[e.Name] = e.NameRaw
	}
	// Build backlinks map in a separate pass (before generating any markdown).
	backlinks := make(map[string][]string)
	for _, e := range entities {
		rels, _ := store.RelationsOf(collection, e.Name, true)
		for _, r := range rels {
			peerName := r.Target
			if r.Source != e.Name {
				peerName = r.Source
			}
			// Dedup: don't add the same source twice.
			found := false
			for _, existing := range backlinks[peerName] {
				if existing == e.NameRaw {
					found = true
					break
				}
			}
			if !found {
				backlinks[peerName] = append(backlinks[peerName], e.NameRaw)
			}
		}
	}
	// Generate markdown files with filename collision dedup.
	usedNames := make(map[string]bool)
	for _, e := range entities {
		md := generateEntityMarkdown(store, collection, e, nameToRaw, backlinks)
		fname := sanitizeFilename(e.NameRaw) + ".md"
		base := strings.TrimSuffix(fname, ".md")
		for i := 2; usedNames[fname]; i++ {
			fname = fmt.Sprintf("%s_%d.md", base, i)
		}
		usedNames[fname] = true
		if err := os.WriteFile(filepath.Join(outputDir, fname), md, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", fname, err)
		}
	}
	// Generate MOC file.
	moc := generateMOC(entities)
	if err := os.WriteFile(filepath.Join(outputDir, "_目录.md"), moc, 0o644); err != nil {
		return fmt.Errorf("write MOC: %w", err)
	}
	return nil
}

func generateEntityMarkdown(store *Store, collection string, e Entity, nameToRaw map[string]string, backlinks map[string][]string) []byte {
	var b strings.Builder

	// YAML front matter.
	b.WriteString("---\n")
	fmt.Fprintf(&b, "type: %s\n", e.Type)
	if len(e.Sources) > 0 {
		b.WriteString("sources:\n")
		for _, s := range e.Sources {
			fmt.Fprintf(&b, "  - %s#%d\n", s.Path, s.Chunk)
		}
	}
	b.WriteString("---\n\n")

	// Title.
	fmt.Fprintf(&b, "# %s\n\n", e.NameRaw)

	// Description.
	if e.Description != "" {
		b.WriteString(e.Description)
		b.WriteString("\n\n")
	}

	// Relations.
	rels, _ := store.RelationsOf(collection, e.Name, true)
	if len(rels) > 0 {
		b.WriteString("## 关系\n\n")
		for _, r := range rels {
			peerName := r.Target
			if r.Source != e.Name {
				peerName = r.Source
			}
			peerRaw := nameToRaw[peerName]
			if peerRaw == "" {
				peerRaw = peerName
			}
			fmt.Fprintf(&b, "- %s → [[%s]]\n", r.Type, peerRaw)
		}
		b.WriteString("\n")
	}

	// Backlinks.
	if bl := backlinks[e.Name]; len(bl) > 0 {
		b.WriteString("## 被引用\n\n")
		for _, src := range bl {
			fmt.Fprintf(&b, "- [[%s]]\n", src)
		}
		b.WriteString("\n")
	}

	// Sources.
	if len(e.Sources) > 0 {
		b.WriteString("## 来源\n\n")
		for _, s := range e.Sources {
			fmt.Fprintf(&b, "> %s (chunk %d)\n", s.Path, s.Chunk)
		}
	}

	return []byte(b.String())
}

// generateMOC creates a Map of Content file grouping entities by type.
func generateMOC(entities []Entity) []byte {
	byType := make(map[string][]Entity)
	for _, e := range entities {
		typ := e.Type
		if typ == "" {
			typ = "未分类"
		}
		byType[typ] = append(byType[typ], e)
	}

	var b strings.Builder
	b.WriteString("# 知识目录\n\n")
	for typ, ents := range byType {
		fmt.Fprintf(&b, "## %s\n\n", typ)
		for _, e := range ents {
			fmt.Fprintf(&b, "- [[%s]]", e.NameRaw)
			if e.Description != "" {
				desc := e.Description
				if len(desc) > 60 {
					desc = desc[:60] + "…"
				}
				fmt.Fprintf(&b, " — %s", desc)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// sanitizeFilename removes characters that are invalid in file names.
func sanitizeFilename(s string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	return replacer.Replace(s)
}

// ReadFileForPreview reads a text file for document preview. Returns the body and extension.
// Delegates to readDoc which handles markitdown + Go fallback for all formats.
func ReadFileForPreview(path string) (string, string, error) {
	return readDoc(path)
}

// SplitForPreview splits a document body into chunks for preview highlighting.
// Uses the same chunking strategy as chunkDoc (paragraph split for md/txt,
// fixed windows for code).
func SplitForPreview(body string, path string) []string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	return chunkDoc(body, ext)
}

// chunkForPreview is a simple paragraph-based splitter for preview.
func chunkForPreview(body string, maxRunes int) []string { //nolint:unused
	if maxRunes <= 0 {
		maxRunes = 1200
	}
	paras := strings.Split(body, "\n\n")
	var chunks []string
	var buf strings.Builder
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if utf8.RuneCountInString(buf.String())+utf8.RuneCountInString(p)+2 > maxRunes && buf.Len() > 0 {
			chunks = append(chunks, buf.String())
			buf.Reset()
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(p)
	}
	if buf.Len() > 0 {
		chunks = append(chunks, buf.String())
	}
	return chunks
}
