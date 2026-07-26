// GraphToolbar provides controls for the knowledge graph: collection selector,
// search, type filter, selection mode toggle, and export buttons.

import { useEffect, useRef, useState } from "react";
import { Download, FileText, Filter, Maximize2, Network, Search, ToggleLeft, ToggleRight, X } from "lucide-react";
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
  selectionMode: boolean;
  onToggleSelectionMode: () => void;
  onImport: () => void;
  onExportObsidian: () => void;
  onDetectCommunities: () => void;
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
  selectionMode,
  onToggleSelectionMode,
  onImport,
  onExportObsidian,
  onDetectCommunities,
}: GraphToolbarProps) {
  const [showFilter, setShowFilter] = useState(false);

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
          <option key={c.name} value={c.name}>
            {c.name} ({c.entities})
          </option>
        ))}
      </select>

      {/* Import button */}
      <button className="rag-toolbar__btn" onClick={onImport} title="导入文件">
        <FileText size={14} />
        <span>导入</span>
      </button>

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

      {/* Type filter */}
      <div className="rag-toolbar__filter-wrap">
        <button
          className={`rag-toolbar__btn ${filterTypes.length > 0 ? "rag-toolbar__btn--active" : ""}`}
          onClick={() => setShowFilter(!showFilter)}
          title="类型筛选"
        >
          <Filter size={14} />
          <span>筛选{filterTypes.length > 0 ? ` (${filterTypes.length})` : ""}</span>
        </button>
        {showFilter && (
          <div className="rag-toolbar__filter-dropdown">
            {ENTITY_TYPES.map((t) => (
              <label key={t.key} className="rag-toolbar__filter-item">
                <input
                  type="checkbox"
                  checked={filterTypes.includes(t.key)}
                  onChange={() => toggleType(t.key)}
                />
                <span>{t.label}</span>
              </label>
            ))}
            {filterTypes.length > 0 && (
              <button className="rag-toolbar__filter-clear" onClick={() => onFilterChange([])}>
                清除筛选
              </button>
            )}
          </div>
        )}
      </div>

      {/* Selection mode toggle */}
      <button
        className={`rag-toolbar__btn ${selectionMode ? "rag-toolbar__btn--active" : ""}`}
        onClick={onToggleSelectionMode}
        title={selectionMode ? "退出选择模式" : "进入选择模式"}
      >
        {selectionMode ? <ToggleRight size={14} /> : <ToggleLeft size={14} />}
        <span>选择模式</span>
      </button>

      {/* Detect communities (Louvain) */}
      <button className="rag-toolbar__btn" onClick={onDetectCommunities} title="检测社区（Louvain 聚类，自动给节点着色）">
        <Network size={14} />
        <span>社区</span>
      </button>

      {/* Fit view (re-center graph to show all nodes) */}
      <button
        className="rag-toolbar__btn"
        onClick={() => window.dispatchEvent(new CustomEvent("rag:fit-view"))}
        title="居中视图（缩放到全部节点）"
      >
        <Maximize2 size={14} />
      </button>

      {/* Export */}
      <button className="rag-toolbar__btn" onClick={onExportObsidian} title="导出 Obsidian">
        <Download size={14} />
      </button>
    </div>
  );
}
