// cowork-search-filter.test.ts
// Verifies real-time in-memory search and filtering for CoworkDock collections (tree structure)
// and file assets, ensuring case-insensitive substring matching and auto-expansion of parent nodes.

import { describe, expect, it } from "vitest";

interface CollectionNode {
  name: string;
  path: string;
  parent: string;
  documents?: number;
}

interface FileNode {
  key: string;
  label: string;
  path: string;
  kind: "file" | "dir";
}

// 1. Core Logic for Collection Tree Search & Auto-expand Path Calculation
function filterCollections(
  collections: CollectionNode[],
  query: string
): { filtered: CollectionNode[]; autoExpandPaths: Set<string> } {
  if (!query || !query.trim()) {
    return { filtered: collections, autoExpandPaths: new Set() };
  }
  const q = query.trim().toLowerCase();
  const autoExpandPaths = new Set<string>();
  const matchedSet = new Set<string>();

  // Find all matching nodes
  for (const c of collections) {
    if (c.name.toLowerCase().includes(q) || c.path.toLowerCase().includes(q)) {
      matchedSet.add(c.path);
      // If this is a child node, we must auto-expand its parent so it's visible in the tree
      if (c.parent) {
        autoExpandPaths.add(c.parent);
      }
    }
  }

  // Also include parent nodes if any of their children matched
  const filtered = collections.filter((c) => {
    if (matchedSet.has(c.path)) return true;
    // Check if this node is a parent of any matched node
    for (const matchedPath of matchedSet) {
      if (matchedPath.startsWith(c.path + "/") || matchedPath === c.parent) {
        autoExpandPaths.add(c.path);
        return true;
      }
    }
    return false;
  });

  return { filtered, autoExpandPaths };
}

// 2. Core Logic for File Asset List Filtering
function filterFiles(files: FileNode[], query: string): FileNode[] {
  if (!query || !query.trim()) return files;
  const q = query.trim().toLowerCase();
  return files.filter(
    (f) => f.label.toLowerCase().includes(q) || f.path.toLowerCase().includes(q)
  );
}

describe("Cowork Search & Filter Suite", () => {
  const mockCollections: CollectionNode[] = [
    { name: "个人", path: "个人", parent: "", documents: 2 },
    { name: "学习材料", path: "个人/学习材料", parent: "个人", documents: 5 },
    { name: "工作", path: "工作", parent: "", documents: 0 },
    { name: "报表", path: "工作/报表", parent: "工作", documents: 10 },
    { name: "日报", path: "工作/日报", parent: "工作", documents: 3 },
    { name: "2026年管理办法", path: "工作/2026年管理办法", parent: "工作", documents: 1 },
  ];

  const mockFiles: FileNode[] = [
    { key: "1", label: "2026年Q1财务营收报表.pdf", path: "工作/报表/2026年Q1财务营收报表.pdf", kind: "file" },
    { key: "2", label: "全公司管理约束体系.docx", path: "工作/2026年管理办法/全公司管理约束体系.docx", kind: "file" },
    { key: "3", label: "个人技术架构笔记.txt", path: "个人/学习材料/个人技术架构笔记.txt", kind: "file" },
    { key: "4", label: "DeepSeek协同开发指南.md", path: "个人/学习材料/DeepSeek协同开发指南.md", kind: "file" },
  ];

  it("should handle empty query by returning all nodes without forcing expansions", () => {
    const res = filterCollections(mockCollections, "");
    expect(res.filtered.length).toBe(6);
    expect(res.autoExpandPaths.size).toBe(0);
  });

  it("should automatically pull in parents and flag autoExpandPaths when child matches query", () => {
    const res = filterCollections(mockCollections, "日报");
    expect(res.filtered.length).toBe(2);
    expect(res.filtered.some(c => c.name === "日报")).toBe(true);
    expect(res.filtered.some(c => c.name === "工作")).toBe(true);
    expect(res.autoExpandPaths.has("工作")).toBe(true);
  });

  it("should perform case-insensitive search on file list", () => {
    const res = filterFiles(mockFiles, "deepseek");
    expect(res.length).toBe(1);
    expect(res[0].label).toBe("DeepSeek协同开发指南.md");
  });

  it("should filter exact keyword on file labels", () => {
    const res = filterFiles(mockFiles, "报表");
    expect(res.length).toBe(1);
    expect(res[0].label).toBe("2026年Q1财务营收报表.pdf");
  });

  it("should gracefully return empty results when no matches exist", () => {
    const resCol = filterCollections(mockCollections, "完全不存在的分类的关键字");
    const resFile = filterFiles(mockFiles, "NON_EXISTENT_FILE_12345");
    expect(resCol.filtered.length).toBe(0);
    expect(resFile.length).toBe(0);
  });
});
