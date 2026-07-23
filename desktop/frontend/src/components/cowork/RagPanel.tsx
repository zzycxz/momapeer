// RagPanel is the coWork "知识库" panel, redesigned as a graph-first layout.
// The knowledge graph occupies the full center area. CoworkDock handles the
// navigation sidebar (collections, entities, files). This panel owns:
// - Empty state (import prompt)
// - GraphToolbar (top)
// - GraphCanvas (center, full screen)
// - KnowledgeRefBar (bottom, when selection mode is active)
// - GraphLegend (bottom-right overlay)

import { useCallback, useEffect, useState } from "react";
import { FolderPlus } from "lucide-react";

import { app, onFilesDropped, onRagChanged } from "../../lib/bridge";
import type { RagCollectionView } from "../../lib/types";
import { asArray } from "../../lib/array";
import { useToast } from "../../lib/toast";
import { useT } from "../../lib/i18n";
import { GraphCanvas } from "./GraphCanvas";
import { GraphToolbar, type SearchMode } from "./GraphToolbar";
import { GraphLegend } from "./GraphLegend";
import { KnowledgeRefBar } from "./KnowledgeRefBar";
import { SkillSelectModal } from "./SkillSelectModal";

export function RagPanel() {
  const { showToast } = useToast();
  const t = useT();

  // Data state.
  const [collections, setCollections] = useState<RagCollectionView[]>([]);
  const [activeCollection, setActiveCollection] = useState("");
  const [hasData, setHasData] = useState(false);

  // UI state.
  const [searchQuery, setSearchQuery] = useState("");
  const [searchMode, setSearchMode] = useState<SearchMode>("keyword");
  const [filterTypes, setFilterTypes] = useState<string[]>([]);
  const [selectionMode, setSelectionMode] = useState(false);
  const [selectedEntities, setSelectedEntities] = useState<string[]>([]);
  const [selectedRelations, setSelectedRelations] = useState<string[]>([]);
  const [showSkillModal, setShowSkillModal] = useState(false);
  const [summary, setSummary] = useState<{ summary: string; themes: string[] } | null>(null);
  const [summaryLoading, setSummaryLoading] = useState(false);
  // Supported file formats, fetched from the backend so the empty-state hint
  // always matches what rag actually accepts (previously hardcoded & stale).
  const [supportedFormats, setSupportedFormats] = useState<string[]>([]);
  const [hasCommunities, setHasCommunities] = useState(false);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [newCollectionName, setNewCollectionName] = useState("");
  const [newCollectionParent, setNewCollectionParent] = useState("");

  useEffect(() => {
    app.RagListTemplates().then(setSupportedFormats).catch(() => setSupportedFormats([]));
  }, []);

  // Check if the active collection has communities assigned. Used to toggle
  // the community legend section. Debounced to avoid request storms during
  // extraction (which fires many rag:changed events).
  useEffect(() => {
    const check = () => {
      app.GetTopEntities(activeCollection || "", 5).then((data) => {
        setHasCommunities(data.nodes.some((n) => n.community >= 0));
      }).catch(() => {});
    };
    check();
    let timer: ReturnType<typeof setTimeout> | null = null;
    const off = onRagChanged(() => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(check, 1000);
    });
    return () => { off(); if (timer) clearTimeout(timer); };
  }, [activeCollection]);

  // Refresh collections.
  const refresh = useCallback(async () => {
    try {
      const cols = await app.ListRagCollections();
      setCollections(cols);
      setHasData(cols.length > 0 && cols.some((c) => c.documents > 0));
    } catch {
      setCollections([]);
      setHasData(false);
    }
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => onRagChanged(() => void refresh()), [refresh]);

  // Fetch summary when collection changes and has data.
  const fetchSummary = useCallback(async () => {
    if (!activeCollection) { setSummary(null); return; }
    setSummaryLoading(true);
    try {
      const s = await app.RagSummarize(activeCollection);
      setSummary(s.summary ? s : null);
    } catch {
      setSummary(null);
    } finally {
      setSummaryLoading(false);
    }
  }, [activeCollection]);

  // Auto-select the first collection when only one exists.
  useEffect(() => {
    if (collections.length === 1 && !activeCollection) {
      setActiveCollection(collections[0].name);
    }
  }, [collections, activeCollection]);

  // Import handler.
  const handleImport = async () => {
    try {
      const path = await app.PickWorkspace();
      if (!path) return;
      const res = await app.RagImportPaths(activeCollection || "default", [path]);
      showToast(res.message, "info");
      void refresh();
    } catch (e) {
      showToast(String(e), "error");
    }
  };

  // Drag-and-drop import.
  useEffect(() => {
    return onFilesDropped((paths) => {
      if (paths.length === 0) return;
      void app.RagImportPaths(activeCollection || "default", paths).then((res) => {
        showToast(res.message, "info");
        void refresh();
      }).catch((e) => showToast(String(e), "error"));
    });
  }, [activeCollection, refresh, showToast]);

  // Selection mode: clear when toggling off.
  useEffect(() => {
    if (!selectionMode) {
      setSelectedEntities([]);
      setSelectedRelations([]);
    }
  }, [selectionMode]);

  // Knowledge reference: write temp file and invoke skill.
  const handleSkillConfirm = async (skillName: string) => {
    try {
      const refPath = await app.WriteKnowledgeRef(activeCollection || "default", selectedEntities, selectedRelations);
      await app.RunSkillWithKnowledge(skillName, refPath);
      showToast(t("cowork.ragImportStarted", { skill: skillName }), "info");
      setShowSkillModal(false);
      setSelectionMode(false);
    } catch (e) {
      showToast(String(e), "error");
    }
  };

  // Export Obsidian.
  const handleExportObsidian = async () => {
    try {
      const outDir = await app.PickWorkspace();
      if (!outDir) return;
      await app.ExportObsidian(activeCollection || "default", outDir);
      showToast(t("cowork.ragObsidianExported"), "info");
    } catch (e) {
      showToast(String(e), "error");
    }
  };

  // Create collection.
  const handleCreateCollection = async () => {
    const parent = newCollectionParent.trim();
    const name = newCollectionName.trim();
    if (!name) return;
    const full = parent ? `${parent}/${name}` : name;
    try {
      await app.RagCreateCollection(full);
      showToast(`分类"${full}"已创建`, "info");
      setShowCreateModal(false);
      setNewCollectionName("");
      setNewCollectionParent("");
      void refresh();
    } catch (e) {
      showToast(String(e), "error");
    }
  };

  // Template presets for new collections.
  const collectionTemplates = [
    { label: "工作", value: "工作" },
    { label: "学习", value: "学习" },
    { label: "个人", value: "个人" },
    { label: "项目", value: "项目" },
  ];

  const handleDetectCommunities = async () => {
    try {
      await app.RagDetectCommunities(activeCollection || "");
      showToast("社区检测中…完成后图谱自动刷新显示色环", "info");
    } catch (e) {
      showToast(String(e), "error");
    }
  };

  // Node click: dispatch entity-click event with the node's own collection so
  // EntityDetail can find it even in "all collections" scope.
  const handleNodeClick = (name: string, entityCollection: string) => {
    window.dispatchEvent(new CustomEvent("rag:entity-click", { detail: { name, collection: entityCollection } }));
  };

  // Empty state.
  if (!hasData) {
    return (
      <div
        className="rag-panel rag-panel--empty"
        style={{ "--wails-drop-target": "drop" } as React.CSSProperties}
        role="region"
        aria-label={t("cowork.ragDropRegion")}
      >
        <div className="rag-panel__empty-content">
          <div className="rag-panel__empty-text">{t("cowork.ragDropToStart")}</div>
          <div className="rag-panel__empty-hint">
            {supportedFormats.length > 0
              ? `支持 ${supportedFormats.join(" / ")}`
              : "支持 md / docx / pdf / xlsx / csv / 代码 等格式"}
          </div>
          <button className="btn btn--primary" onClick={() => void handleImport()}>
            <FolderPlus size={14} />
            <span>导入文件</span>
          </button>
        </div>
      </div>
    );
  }

  return (
    <div
      className="rag-panel rag-panel--split"
      style={{ "--wails-drop-target": "drop" } as React.CSSProperties}
    >
      {/* Left sidebar: collection tree (Obsidian-style) */}
      <aside className="rag-tree">
        <div className="rag-tree__header">
          <span className="rag-tree__title">分类</span>
          <button
            className="rag-tree__new-btn"
            title="新建分类"
            onClick={() => setShowCreateModal(true)}
          >
            +
          </button>
        </div>
        <div className="rag-tree__list">
          <div
            className={`rag-tree__item ${activeCollection === "" ? "rag-tree__item--active" : ""}`}
            onClick={() => setActiveCollection("")}
          >
            全部
          </div>
          {collections.map((c) => (
            <div
              key={c.id || c.name}
              className={`rag-tree__item ${activeCollection === c.name ? "rag-tree__item--active" : ""}`}
              onClick={() => setActiveCollection(c.name)}
              onContextMenu={(e) => {
                e.preventDefault();
                const action = window.confirm(`删除分类"${c.name}"及其所有文档？\n（确认删除，取消不操作）`);
                if (action) {
                  app.RagDeleteCollection(c.name).then(() => void refresh()).catch((err) => showToast(String(err), "error"));
                }
              }}
              title={`右键删除 · ${c.documents} 文档 · ${c.entities} 实体`}
              style={{ paddingLeft: c.parent ? "24px" : "12px" }}
            >
              {c.parent ? "└ " : "📁 "}
              {c.name}
              <span className="rag-tree__count">{c.documents > 0 ? c.documents : ""}</span>
            </div>
          ))}
        </div>
      </aside>

      {/* Right: toolbar + graph + overlays */}
      <div className="rag-panel__main">
        {/* Top toolbar */}
        <GraphToolbar
          collection={activeCollection}
          collections={collections}
          onCollectionChange={setActiveCollection}
          searchQuery={searchQuery}
          onSearchChange={setSearchQuery}
          searchMode={searchMode}
          onSearchModeChange={setSearchMode}
          filterTypes={filterTypes}
          onFilterChange={setFilterTypes}
          selectionMode={selectionMode}
          onToggleSelectionMode={() => setSelectionMode(!selectionMode)}
          onImport={() => void handleImport()}
          onExportObsidian={() => void handleExportObsidian()}
          onDetectCommunities={() => void handleDetectCommunities()}
        />

        {/* Knowledge summary card */}
        {summary && (
          <div className="rag-summary">
            <div className="rag-summary__text">{summary.summary}</div>
            <div className="rag-summary__themes">
              {asArray(summary.themes).map((t) => (
                <span key={t} className="rag-summary__theme" onClick={() => setSearchQuery(t)}>{t}</span>
              ))}
            </div>
          </div>
        )}
        {!summary && !summaryLoading && activeCollection && hasData && (
          <div className="rag-summary rag-summary--prompt">
            <button className="btn btn--link" onClick={() => void fetchSummary()}>
              生成知识摘要
            </button>
          </div>
        )}

        {/* Graph canvas */}
        <div className="rag-panel__graph">
          <GraphCanvas
            collection={activeCollection}
            searchQuery={searchQuery}
            searchMode={searchMode}
            filterTypes={filterTypes}
            selectionMode={selectionMode}
            selectedEntities={selectedEntities}
            selectedRelations={selectedRelations}
            onNodeClick={handleNodeClick}
            onSelectionChange={(ents, rels) => {
              setSelectedEntities(ents);
              setSelectedRelations(rels);
            }}
          />
        </div>

        {/* Legend overlay */}
        <GraphLegend hasCommunities={hasCommunities} />

        {/* Knowledge reference bar (selection mode) */}
        {selectionMode && (
          <KnowledgeRefBar
            selectedEntities={selectedEntities}
            selectedRelations={selectedRelations}
            onClear={() => {
              setSelectedEntities([]);
              setSelectedRelations([]);
            }}
            onUseFor={() => setShowSkillModal(true)}
          />
        )}

        {/* Skill selection modal */}
        {showSkillModal && (
          <SkillSelectModal
            selectedEntities={selectedEntities}
            selectedRelations={selectedRelations}
            onConfirm={(skill) => handleSkillConfirm(skill)}
            onClose={() => setShowSkillModal(false)}
          />
        )}
      </div>

      {/* Create collection modal */}
      {showCreateModal && (
        <div className="rag-create-overlay" onClick={() => setShowCreateModal(false)}>
          <div className="rag-create-modal" onClick={(e) => e.stopPropagation()}>
            <div className="rag-create-modal__head">
              <h3 className="rag-create-modal__title">新建分类</h3>
              <button className="rag-create-modal__close" onClick={() => setShowCreateModal(false)}>✕</button>
            </div>
            <div className="rag-create-modal__body">
              {/* Template quick picks */}
              <div className="rag-create-modal__section">
                <label className="rag-create-modal__label">选择模板或自定义</label>
                <div className="rag-create-modal__templates">
                  {collectionTemplates.map((tpl) => (
                    <button
                      key={tpl.value}
                      className={`rag-create-modal__template ${
                        newCollectionParent === tpl.value ? "rag-create-modal__template--selected" : ""
                      }`}
                      onClick={() => setNewCollectionParent(tpl.value)}
                    >
                      📁 {tpl.label}
                    </button>
                  ))}
                  <button
                    className={`rag-create-modal__template ${
                      newCollectionParent === "" ? "rag-create-modal__template--selected" : ""
                    }`}
                    onClick={() => setNewCollectionParent("")}
                  >
                    ✏️ 自定义
                  </button>
                </div>
              </div>

              {/* Parent display (read-only, from template pick) */}
              {newCollectionParent && (
                <div className="rag-create-modal__section">
                  <label className="rag-create-modal__label">父分类</label>
                  <div className="rag-create-modal__parent">{newCollectionParent}/</div>
                </div>
              )}

              {/* Name input */}
              <div className="rag-create-modal__section">
                <label className="rag-create-modal__label">分类名称</label>
                <input
                  className="rag-create-modal__input"
                  placeholder={newCollectionParent ? "如：领导材料" : "如：工作 或 工作/领导材料"}
                  value={newCollectionName}
                  onChange={(e) => setNewCollectionName(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter") void handleCreateCollection(); }}
                  autoFocus
                />
              </div>

              {/* Preview full path */}
              <div className="rag-create-modal__preview">
                {newCollectionParent && newCollectionName
                  ? `${newCollectionParent}/${newCollectionName}`
                  : newCollectionName || "（请输入名称）"}
              </div>
            </div>
            <div className="rag-create-modal__foot">
              <button className="btn btn--small" onClick={() => setShowCreateModal(false)}>取消</button>
              <button
                className="btn btn--primary btn--small"
                disabled={!newCollectionName.trim()}
                onClick={() => void handleCreateCollection()}
              >
                创建
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
