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

  useEffect(() => {
    app.RagListTemplates().then(setSupportedFormats).catch(() => setSupportedFormats([]));
  }, []);

  // Listen for collection selection from CoworkDock (right panel).
  // This keeps the central panel's activeCollection in sync with the dock.
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (detail && typeof detail.collection === "string") {
        setActiveCollection(detail.collection);
      }
    };
    window.addEventListener("rag:collection-selected", handler);
    return () => window.removeEventListener("rag:collection-selected", handler);
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
      const path = await app.PickImportFolder();
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
      className="rag-panel"
      style={{ "--wails-drop-target": "drop" } as React.CSSProperties}
    >
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
  );
}
