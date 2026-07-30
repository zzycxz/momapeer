// GraphToolbar provides controls for the knowledge graph: collection selector,
// search, type filter, selection mode toggle, and export buttons.

import { useEffect, useRef, useState } from "react";
import { Download, Filter, Search, X, Loader2, Sparkles, FolderPlus } from "lucide-react";
import type { RagCollectionView } from "../../lib/types";
import { ENTITY_TYPES } from "./entityTypes";

export type SearchMode = "keyword" | "semantic";

export interface GraphToolbarProps {
  collection: string;
  collections: RagCollectionView[];
  onCollectionChange: (name: string) => void;
  searchQuery: string;
  onSearchChange: (q: string) => void;
  searchMode: SearchMode;
  onSearchModeChange: (mode: SearchMode) => void;
  filterTypes: string[];
  onFilterChange: (types: string[]) => void;
  onImport: () => void;
  onExportObsidian: () => void;
  hasData: boolean;
  summary: any;
  summaryLoading: boolean;
  onFetchSummary: () => void;
}

export function GraphToolbar({
  collection,
  collections,
  onCollectionChange,
  searchQuery,
  onSearchChange,
  searchMode,
  onSearchModeChange,
  filterTypes,
  onFilterChange,
  onImport,
  onExportObsidian,
  hasData,
  summary,
  summaryLoading,
  onFetchSummary,
}: GraphToolbarProps) {
  const [showFilter, setShowFilter] = useState(false);
  const filterRef = useRef<HTMLDivElement>(null);

  const [showSummaryDropdown, setShowSummaryDropdown] = useState(false);
  const summaryRef = useRef<HTMLDivElement>(null);

  // Close dropdowns when clicking outside
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (filterRef.current && !filterRef.current.contains(e.target as Node)) {
        setShowFilter(false);
      }
      if (summaryRef.current && !summaryRef.current.contains(e.target as Node)) {
        setShowSummaryDropdown(false);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  // Debounce the search input: keep a responsive local draft while only
  // propagating to the parent (which triggers graph rebuild + backend calls)
  // after the user pauses typing for 300ms. Without this, every keystroke
  // rebuilds all nodes/edges and — in semantic mode — fires a backend request.
  const [draft, setDraft] = useState(searchQuery);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Keep the draft in sync when the parent resets the query (e.g. clear button).
  useEffect(() => { setDraft(searchQuery); }, [searchQuery]);
  const commitSearch = (value: string) => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => onSearchChange(value), 300);
  };

  const toggleType = (key: string) => {
    if (filterTypes.includes(key)) {
      onFilterChange(filterTypes.filter((t) => t !== key));
    } else {
      onFilterChange([...filterTypes, key]);
    }
  };

  return (
    <div className="rag-toolbar">
      {/* Collection selector */}
      <select
        className="rag-toolbar__select"
        value={collection}
        onChange={(e) => onCollectionChange(e.target.value)}
      >
        <option value="">全部</option>
        {collections.map((c) => (
          <option key={c.path || c.name} value={c.path || c.name}>
            {c.name} ({c.entities})
          </option>
        ))}
      </select>

      {/* Import button */}
      <button className="rag-toolbar__btn" onClick={onImport} title="导入文件">
        <FolderPlus size={14} />
        <span>导入</span>
      </button>

      {/* Summary button & dropdown */}
      {hasData && (
        <div className="rag-toolbar__filter-wrap" ref={summaryRef} style={{ display: "inline-flex", marginLeft: "4px" }}>
          <button
            className={`rag-toolbar__btn ${showSummaryDropdown ? "rag-toolbar__btn--active" : ""}`}
            onClick={() => {
              if (summary) {
                setShowSummaryDropdown(!showSummaryDropdown);
              } else {
                onFetchSummary();
              }
            }}
            title={summary ? "查看知识摘要" : "由大模型生成全局知识摘要与主题词"}
            style={summary ? { color: "var(--accent)", borderColor: "var(--accent)", background: showSummaryDropdown ? "rgba(249, 115, 22, 0.08)" : "transparent" } : {}}
            disabled={summaryLoading}
          >
            {summaryLoading ? <Loader2 size={14} className="spin" /> : <Sparkles size={14} />}
            <span>{summaryLoading ? "生成中..." : summary ? "知识摘要" : "生成摘要"}</span>
          </button>

          {showSummaryDropdown && summary && (
            <div className="rag-toolbar__filter-dropdown" style={{ display: "flex", flexDirection: "column", gap: "12px", backdropFilter: "blur(12px)", width: "340px", padding: "16px", left: 0, right: "auto", marginTop: "32px" }}>
              <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", paddingBottom: "8px", borderBottom: "1px solid var(--border-soft)" }}>
                <span style={{ fontSize: "13px", fontWeight: 600, color: "var(--fg)" }}>✨ 全局知识摘要</span>
                <button 
                  onClick={() => setShowSummaryDropdown(false)}
                  style={{ background: "transparent", border: "none", cursor: "pointer", color: "var(--fg-faint)", padding: "2px", display: "flex", alignItems: "center", justifyContent: "center" }}
                >
                  <X size={14} />
                </button>
              </div>
              <div style={{ fontSize: "13px", color: "var(--fg-dim)", lineHeight: "1.6", whiteSpace: "pre-wrap" }}>
                {summary.summary}
              </div>
              {summary.themes && (Array.isArray(summary.themes) ? summary.themes : [summary.themes]).length > 0 && (
                <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", marginTop: "4px" }}>
                  {(Array.isArray(summary.themes) ? summary.themes : [summary.themes]).map((t: string) => (
                    <span 
                      key={t} 
                      onClick={() => {
                        if (typeof onSearchChange === "function") onSearchChange(t);
                        setShowSummaryDropdown(false);
                      }}
                      style={{ padding: "4px 10px", background: "rgba(249, 115, 22, 0.1)", color: "var(--accent)", borderRadius: "12px", fontSize: "11.5px", cursor: "pointer", border: "1px solid rgba(249, 115, 22, 0.2)", transition: "all 0.2s ease" }}
                      title="点击搜索该主题"
                    >
                      {t}
                    </span>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* Search */}
      <div className="rag-toolbar__search">
        <Search size={14} />
        <input
          type="text"
          placeholder={searchMode === "semantic" ? "语义搜索..." : "搜索实体..."}
          value={draft}
          onChange={(e) => { setDraft(e.target.value); commitSearch(e.target.value); }}
        />
        <button
          className={`rag-toolbar__search-mode ${searchMode === "semantic" ? "rag-toolbar__search-mode--active" : ""}`}
          onClick={() => onSearchModeChange(searchMode === "keyword" ? "semantic" : "keyword")}
          title={searchMode === "keyword" ? "切换到语义搜索" : "切换到关键词搜索"}
        >
          {searchMode === "keyword" ? "词" : "义"}
        </button>
        {searchQuery && (
          <button className="rag-toolbar__clear" onClick={() => onSearchChange("")}>
            <X size={12} />
          </button>
        )}
      </div>

      {/* Filter Toggle Button & Dropdown Popover */}
      <div className="rag-toolbar__filter-wrap" ref={filterRef} style={{ display: "inline-flex", marginLeft: "8px" }}>
        <button
          type="button"
          className={`rag-toolbar__btn ${showFilter || filterTypes.length > 0 ? "rag-toolbar__btn--active" : ""}`}
          onClick={() => setShowFilter(!showFilter)}
          title="筛选图谱实体类型"
          style={filterTypes.length > 0 ? { color: "var(--accent)", borderColor: "var(--accent)", background: "rgba(249, 115, 22, 0.08)" } : {}}
        >
          <Filter size={14} />
          <span>筛选</span>
          {filterTypes.length > 0 && (
            <span style={{
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
              marginLeft: "4px",
              padding: "0 6px",
              borderRadius: "10px",
              background: "var(--accent)",
              color: "#fff",
              fontSize: "11px",
              fontWeight: 600,
              lineHeight: "16px",
            }}>
              {filterTypes.length}
            </span>
          )}
        </button>

        {showFilter && (
          <div className="rag-toolbar__filter-dropdown" style={{ display: "flex", flexDirection: "column", gap: "8px", backdropFilter: "blur(12px)" }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", paddingBottom: "6px", borderBottom: "1px solid var(--border-soft)" }}>
              <span style={{ fontSize: "12px", fontWeight: 600, color: "var(--fg)" }}>实体类型筛选</span>
              <span style={{ fontSize: "11px", color: "var(--fg-faint)" }}>
                {filterTypes.length > 0 ? `已选 ${filterTypes.length} 项` : "默认展示全部"}
              </span>
            </div>
            
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "4px", maxHeight: "280px", overflowY: "auto", paddingRight: "4px" }}>
              {ENTITY_TYPES.map((t) => {
                const active = filterTypes.length === 0 || filterTypes.includes(t.key);
                return (
                  <button
                    key={t.key}
                    type="button"
                    onClick={() => toggleType(t.key)}
                    className="rag-toolbar__filter-item"
                    style={{
                      border: `1px solid ${active ? t.color : "transparent"}`,
                      background: active ? `color-mix(in srgb, ${t.color} 15%, transparent)` : "transparent",
                      opacity: active ? 1 : 0.6,
                    }}
                  >
                    <span style={{ width: "8px", height: "8px", borderRadius: "50%", background: t.color, flexShrink: 0 }} />
                    <span style={{ flex: 1, textAlign: "left", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                      {t.label}
                    </span>
                  </button>
                );
              })}
            </div>

            {filterTypes.length > 0 && (
              <button
                type="button"
                className="rag-toolbar__filter-clear"
                onClick={() => onFilterChange([])}
              >
                重置清空筛选
              </button>
            )}
          </div>
        )}
      </div>

      {/* Export */}
      <button className="rag-toolbar__btn" onClick={onExportObsidian} title="导出 Obsidian">
        <Download size={14} />
      </button>
    </div>
  );
}
