// RagPanel is the coWork "知识库" panel, redesigned as a graph-first layout.
// The knowledge graph occupies the full center area. CoworkDock handles the
// navigation sidebar (collections, entities, files). This panel owns:
// - Empty state (import prompt)
// - GraphToolbar (top)
// - GraphCanvas (center, full screen)
// - KnowledgeRefBar (bottom, when selection mode is active)
// - GraphLegend (bottom-right overlay)

import { useCallback, useEffect, useState } from "react";
import { FolderPlus, Maximize2, MousePointer2, BoxSelect } from "lucide-react";

import { app, onFilesDropped, onRagChanged } from "../../lib/bridge";
import type { RagCollectionView } from "../../lib/types";
import { useToast } from "../../lib/toast";
import { useT } from "../../lib/i18n";
import { GraphCanvas } from "./GraphCanvas";
import { GraphToolbar, type SearchMode } from "./GraphToolbar";
import { GraphLegend } from "./GraphLegend";
import { KnowledgeRefBar } from "./KnowledgeRefBar";
import { SkillSelectModal } from "./SkillSelectModal";
import { ImportModal } from "./ImportModal";

export function RagPanel() {
  const { showToast } = useToast();
  const t = useT();
  const [showImportModal, setShowImportModal] = useState(false);

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

  // Auto-select the first collection when only one exists. Use the full path
  // (c.path) — the leaf name alone would create a duplicate top-level collection
  // on import instead of targeting the existing nested one.
  useEffect(() => {
    if (collections.length === 1 && !activeCollection) {
      setActiveCollection(collections[0].path || collections[0].name);
    }
  }, [collections, activeCollection]);

  // Stale-selection cleanup: if the active collection is deleted (no longer in
  // the list), reset to "" so downstream calls don't run against a dead path.
  useEffect(() => {
    if (activeCollection && collections.length > 0) {
      const stillExists = collections.some((c) => (c.path || c.name) === activeCollection);
      if (!stillExists) setActiveCollection("");
    }
  }, [collections, activeCollection]);


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

  /*
  const handleDetectCommunities = async () => {
    try {
      await app.RagDetectCommunities(activeCollection || "");
      showToast("社区检测中…完成后图谱自动刷新显示色环", "info");
    } catch (e) {
      showToast(String(e), "error");
    }
  };
  */

  // Node click: dispatch entity-click event with the node's own collection so
  // EntityDetail can find it even in "all collections" scope.
  const handleNodeClick = (name: string, entityCollection: string) => {
    window.dispatchEvent(new CustomEvent("rag:entity-click", { detail: { name, collection: entityCollection } }));
  };

  return (
    <div
      className="rag-panel"
      style={{ "--wails-drop-target": "drop" } as React.CSSProperties}
    >
      {/* Top toolbar: ALWAYS keep visible to preserve product layout and navigation */}
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
        onImport={() => setShowImportModal(true)}
        onExportObsidian={() => void handleExportObsidian()}
        hasData={hasData}
        summary={summary}
        summaryLoading={summaryLoading}
        onFetchSummary={() => void fetchSummary()}
      />

      {/* Graph canvas area: display GraphCanvas when data exists; display modern embedded Guide when empty */}
      <div className="rag-panel__graph" style={{ position: "relative", flex: 1, display: "flex", flexDirection: "column" }}>
        {hasData ? (
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
        ) : (
          <div
            className="rag-panel__empty-canvas-guide"
            style={{
              flex: 1,
              width: "100%",
              height: "100%",
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              justifyContent: "center",
              gap: 16,
              background: "radial-gradient(circle at center, rgba(59, 130, 246, 0.05) 0%, transparent 70%)",
            }}
          >
            <div
              style={{
                width: 64,
                height: 64,
                borderRadius: 20,
                background: "rgba(249, 115, 22, 0.1)",
                color: "#f97316",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                boxShadow: "0 8px 24px rgba(249, 115, 22, 0.12)",
                border: "1px solid rgba(249, 115, 22, 0.2)",
              }}
            >
              <FolderPlus size={32} />
            </div>
            <div style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 6, maxWidth: 480, textAlign: "center" }}>
              <h3 style={{ fontSize: 17, fontWeight: 600, color: "var(--fg)", margin: 0 }}>
                {activeCollection ? t("cowork.ragEmptyInCollection", { collection: activeCollection }) : t("cowork.ragHomeReady")}
              </h3>
              <p style={{ fontSize: 13, color: "var(--fg-dim)", margin: 0, lineHeight: 1.5 }}>
                {t("cowork.ragBuildDesc")}
              </p>
            </div>
            <div style={{ padding: "6px 14px", borderRadius: 20, background: "var(--bg-soft)", border: "1px solid var(--border-soft)", fontSize: 11.5, color: "var(--fg-faint)" }}>
              {supportedFormats.length > 0
                ? t("cowork.ragSupportFormats", { formats: supportedFormats.join(" / ") })
                : t("cowork.ragSupportFormatsDefault")}
            </div>
            <button
              type="button"
              className="btn btn--primary"
              onClick={() => setShowImportModal(true)}
              style={{
                marginTop: 6,
                padding: "9px 24px",
                borderRadius: 8,
                background: "var(--accent)",
                color: "#fff",
                border: "none",
                fontSize: 13,
                fontWeight: 600,
                cursor: "pointer",
                display: "inline-flex",
                alignItems: "center",
                gap: 8,
                boxShadow: "0 4px 12px rgba(249, 115, 22, 0.25)",
              }}
            >
              <FolderPlus size={16} />
              <span>{t("cowork.ragImportAssets")}</span>
            </button>
          </div>
        )}
      </div>

      {/* Legend overlay */}
      <GraphLegend hasCommunities={hasCommunities} />

      {/* Canvas bottom-right controls (Selection & Fit) */}
      <div style={{ position: "absolute", bottom: "24px", right: "24px", display: "flex", gap: "6px", background: "rgba(255, 255, 255, 0.8)", backdropFilter: "blur(12px)", padding: "4px", borderRadius: "8px", border: "1px solid var(--border-soft)", boxShadow: "0 2px 12px rgba(0,0,0,0.06)", pointerEvents: "auto", zIndex: 10 }}>
        <button
          className={`rag-toolbar__btn ${selectionMode ? "rag-toolbar__btn--active" : ""}`}
          onClick={() => setSelectionMode(!selectionMode)}
          title={selectionMode ? t("cowork.ragExitSelect") : t("cowork.ragEnterSelect")}
          style={{ padding: "6px 12px", borderRadius: "6px" }}
        >
          {selectionMode ? <MousePointer2 size={14} /> : <BoxSelect size={14} />}
          <span>{t("cowork.ragMultiSelect")}</span>
        </button>
        <div style={{ width: "1px", background: "var(--border-soft)", margin: "6px 2px" }} />
        <button
          className="rag-toolbar__btn"
          onClick={() => window.dispatchEvent(new CustomEvent("rag:fit-view"))}
          title={t("cowork.ragCenterView")}
          style={{ padding: "6px 12px", borderRadius: "6px" }}
        >
          <Maximize2 size={14} />
          <span>{t("cowork.ragCenter")}</span>
        </button>
      </div>

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

      <ImportModal
        isOpen={showImportModal}
        onClose={() => setShowImportModal(false)}
        collections={collections}
        defaultCollection={activeCollection}
        onSuccess={(col) => {
          setActiveCollection(col);
          void refresh();
        }}
      />
    </div>
  );
}
