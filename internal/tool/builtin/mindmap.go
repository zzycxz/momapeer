package builtin

// mindmap.go generates mind maps from a structured tree description. Output is
// either a Markdown file (nested headings/lists — readable in any editor, and
// renderable by markmap/Obsidian) or a self-contained HTML file (embeds the
// markmap-view library via CDN so double-clicking opens an interactive SVG
// mind map in a browser with zero setup).
//
// This is the same "JSON describes content, code generates the file" pattern as
// doc_write (docx) and xlsx_write (xlsx): the agent emits a tree of branches,
// this builder compiles it to a portable format. Pure stdlib — no Go deps.

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

// MMNode is one node in the mind-map tree.
type MMNode struct {
	Text     string   `json:"text"`           // node label (required)
	Children []MMNode `json:"children"`       // sub-branches
	Note     string   `json:"note,omitempty"` // optional side note (md: italic; html: tooltip)
}

// MMInput is the mindmap_create payload.
type MMInput struct {
	Path     string   `json:"path"`     // output path; .md or .html decides format
	Title    string   `json:"title"`    // root node label
	Branches []MMNode `json:"branches"` // top-level branches off the root
	Format   string   `json:"format"`   // "md" | "html" (default: infer from path ext)
}

// writeMindMap compiles MMInput to a .md or .html file. Format is taken from
// MMInput.Format when set, else inferred from the path extension (default md).
func writeMindMap(in MMInput) (string, error) {
	if err := os.MkdirAll(filepath.Dir(in.Path), 0o755); err != nil {
		return "", err
	}
	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format == "" {
		format = strings.TrimPrefix(filepath.Ext(in.Path), ".")
	}
	if format == "html" || format == "htm" {
		return writeMindMapHTML(in)
	}
	return writeMindMapMD(in)
}

// writeMindMapMD renders the tree as Markdown: the title is H1, branches nest
// as H2/H3/H4 (capped at H6), and any level beyond H6 falls back to a bulleted
// list. This is the markmap-friendly format.
func writeMindMapMD(in MMInput) (string, error) {
	var b strings.Builder
	// Optional title as H1. When no title, the first branch becomes H1.
	root := strings.TrimSpace(in.Title)
	if root != "" {
		b.WriteString("# " + root + "\n\n")
	}
	startLevel := 2
	if root == "" {
		startLevel = 1
	}
	for _, node := range in.Branches {
		writeMDNode(&b, node, startLevel)
	}
	out := strings.TrimRight(b.String(), "\n") + "\n"
	if err := os.WriteFile(in.Path, []byte(out), 0o644); err != nil {
		return "", err
	}
	return "md", nil
}

// writeMDNode recurses, emitting headings up to H6 then bullets.
func writeMDNode(b *strings.Builder, n MMNode, level int) {
	if strings.TrimSpace(n.Text) == "" {
		return
	}
	if level <= 6 {
		b.WriteString(strings.Repeat("#", level) + " " + n.Text + "\n")
	} else {
		b.WriteString(strings.Repeat("  ", level-7) + "- " + n.Text + "\n")
	}
	if note := strings.TrimSpace(n.Note); note != "" {
		b.WriteString(strings.Repeat("  ", max(0, level-2)) + "_" + note + "_\n")
	}
	for _, c := range n.Children {
		writeMDNode(b, c, level+1)
	}
	b.WriteString("\n")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// writeMindMapHTML renders a self-contained HTML file that loads markmap-view
// from a CDN and renders the tree as an interactive SVG mind map. The Markdown
// form of the tree is embedded as a JSON string consumed by the viewer script,
// so no separate .md file is needed.
func writeMindMapHTML(in MMInput) (string, error) {
	// Build the Markdown representation the viewer consumes (title + branches).
	var md strings.Builder
	if t := strings.TrimSpace(in.Title); t != "" {
		md.WriteString("# " + t + "\n")
	}
	for _, node := range in.Branches {
		writeMDNode(&md, node, 2)
	}
	mdJSONBytes, err := json.Marshal(md.String())
	if err != nil {
		return "", err
	}
	mdJSON := string(mdJSONBytes)
	title := html.EscapeString(strings.TrimSpace(in.Title))
	if title == "" {
		title = "Mind Map"
	}
	htmlDoc := `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>` + title + `</title>
<style>
  html, body { margin: 0; height: 100%; background: #fff; font-family: -apple-system, "Microsoft YaHei", sans-serif; }
  #mindmap { width: 100vw; height: 100vh; }
</style>
<script src="https://cdn.jsdelivr.net/npm/d3@7"></script>
<script src="https://cdn.jsdelivr.net/npm/markmap-view"></script>
<script src="https://cdn.jsdelivr.net/npm/markmap-lib@0.18"></script>
</head>
<body>
<svg id="mindmap"></svg>
<script>
  const MD = ` + mdJSON + `;
  (async () => {
    const { Transformer } = window.markmap;
    const transformer = new Transformer();
    const { root } = transformer.transform(MD);
    const { Markmap } = window.markmap;
    Markmap.create('#mindmap', { maxWidth: 320, duration: 300 }, root);
  })();
</script>
</body>
</html>`
	if err := os.WriteFile(in.Path, []byte(htmlDoc), 0o644); err != nil {
		return "", err
	}
	return "html", nil
}

// describeMindMapOutput renders the success message for the tool.
func describeMindMapOutput(path, format string) string {
	if format == "html" {
		return fmt.Sprintf("wrote %s (self-contained HTML mind map — double-click to view)", path)
	}
	return fmt.Sprintf("wrote %s (Markdown mind map — open with markmap/Obsidian, or any editor as an outline)", path)
}

var _ = html.EscapeString // keep html import used (title escaping)

// --- mindmap_create tool ----------------------------------------------------

type mindmapCreate struct{}

func (mindmapCreate) Name() string { return "mindmap_create" }

func (mindmapCreate) Description() string {
	return "Generate a mind map file from a tree of branches. Output format by extension: .md (Markdown nested headings — readable in any editor, renderable by markmap/Obsidian) or .html (self-contained interactive SVG mind map, embeds markmap-view via CDN, double-click to open in a browser with zero setup). Use for: turning an outline/topic/doc into a visual mind map. The tree is title → branches → children (nested to any depth)."
}

func (mindmapCreate) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"Output path (.md or .html decides format)"},
  "title":{"type":"string","description":"Root node label"},
  "branches":{"type":"array","description":"Top-level branches off the root","items":{"$ref":"#/$defs/node"}},
  "format":{"type":"string","description":"\"md\" | \"html\" (default: infer from path extension)"},
  "note":{"type":"string","description":"Optional root note (ignored in md)"}
},
"required":["path","title","branches"],
"$defs":{
  "node":{"type":"object","properties":{
    "text":{"type":"string"},
    "children":{"type":"array","items":{"$ref":"#/$defs/node"}},
    "note":{"type":"string"}
  },"required":["text"]}
}
}`)
}

func (mindmapCreate) ReadOnly() bool { return false }

func (mindmapCreate) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in MMInput
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(in.Path)
	if err != nil {
		return "", err
	}
	in.Path = abs
	format, err := writeMindMap(in)
	if err != nil {
		return "", err
	}
	return describeMindMapOutput(abs, format), nil
}
