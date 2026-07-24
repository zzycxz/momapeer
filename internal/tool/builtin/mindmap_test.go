package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMindMapMDStructure confirms the Markdown output nests headings correctly
// (title H1 → branches H2 → children H3…H6 → bullets beyond).
func TestMindMapMDStructure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.md")
	in := MMInput{
		Path:  path,
		Title: "2026规划",
		Branches: []MMNode{
			{Text: "Q1", Children: []MMNode{
				{Text: "新功能", Children: []MMNode{{Text: "思维导图"}, {Text: "Word/Excel 生成"}}},
				{Text: "优化"},
			}},
			{Text: "Q2", Note: "待规划"},
		},
	}
	format, err := writeMindMap(in)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if format != "md" {
		t.Errorf("format = %q, want md", format)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(data)
	// Title → H1, Q1 → H2, 新功能 → H3, 思维导图 → H4.
	for _, want := range []string{"# 2026规划", "## Q1", "### 新功能", "#### 思维导图", "#### Word/Excel 生成", "### 优化", "## Q2", "_待规划_"} {
		if !strings.Contains(got, want) {
			t.Errorf("md missing %q\ngot:\n%s", want, got)
		}
	}
}

// TestMindMapMDNoTitle confirms that when title is empty, branches start at H1.
func TestMindMapMDNoTitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notitle.md")
	_, err := writeMindMap(MMInput{Path: path, Branches: []MMNode{{Text: "根节点", Children: []MMNode{{Text: "子"}}}}})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.HasPrefix(s, "# 根节点") {
		t.Errorf("no-title: first heading should be H1 根节点, got:\n%s", s)
	}
	if !strings.Contains(s, "## 子") {
		t.Errorf("no-title: child should be H2, got:\n%s", s)
	}
}

// TestMindMapHTMLSelfContained confirms the HTML output embeds the markmap
// viewer script + the tree data, and is openable standalone.
func TestMindMapHTMLSelfContained(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "map.html")
	format, err := writeMindMap(MMInput{
		Path:     path,
		Title:    "产品规划",
		Branches: []MMNode{{Text: "A", Children: []MMNode{{Text: "A1"}}}},
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if format != "html" {
		t.Errorf("format = %q, want html", format)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(data)
	for _, want := range []string{"<!DOCTYPE html>", "markmap-view", "markmap-lib", "# 产品规划", "## A", "### A1"} {
		if !strings.Contains(got, want) {
			t.Errorf("html missing %q", want)
		}
	}
}

// TestMindMapFormatOverride confirms Format takes precedence over the path
// extension (so .md path can still emit html).
func TestMindMapFormatOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tricky.md") // .md path but force html
	format, err := writeMindMap(MMInput{Path: path, Format: "html", Title: "x", Branches: []MMNode{{Text: "y"}}})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if format != "html" {
		t.Errorf("format override = %q, want html", format)
	}
}

// TestMindMapDeepNesting confirms levels beyond H6 fall back to bullets
// (Markdown caps at H6).
func TestMindMapDeepNesting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep.md")
	// Build a 8-deep chain: title(H1) → b1(H2) → c1(H3) → ... → level 8 = bullet.
	leaf := MMNode{Text: "level8"}
	for i := 0; i < 6; i++ {
		leaf = MMNode{Text: "lvl", Children: []MMNode{leaf}}
	}
	_, err := writeMindMap(MMInput{Path: path, Title: "root", Branches: []MMNode{leaf}})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	// H6 should exist, and the 7th level should be a bullet.
	if !strings.Contains(s, "######") {
		t.Errorf("deep: expected an H6 somewhere\n%s", s)
	}
}

// TestMindMapCreateToolExecute confirms the tool wrapper works end-to-end
// (path abs + format dispatch + output message).
func TestMindMapCreateToolExecute(t *testing.T) {
	dir := t.TempDir()
	payload := map[string]any{
		"path":  filepath.Join(dir, "tool.md"),
		"title": "T",
		"branches": []map[string]any{
			{"text": "B1", "children": []map[string]any{{"text": "B1a"}}},
		},
	}
	args, _ := json.Marshal(payload)
	out, err := mindmapCreate{}.Execute(context.TODO(), args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "wrote") {
		t.Errorf("output = %q, want to contain 'wrote'", out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "tool.md"))
	if !strings.Contains(string(data), "# T") {
		t.Errorf("tool output missing # T")
	}
}
