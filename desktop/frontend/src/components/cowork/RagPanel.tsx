// RagPanel is the coWork "资料库" panel: a file/folder tree of imported
// documents with per-file FTS5 + extraction status, deep-extract controls, and
// an embedded search bar. It subscribes to rag:changed (tree refresh) and
// rag:progress (live chunk progress) so the UI stays current without polling.
//
// Two-layer RAG by design:
//   - FTS5 (instant): every imported file is searchable the moment it lands.
//   - Deep extract (explicit): user clicks ⚡ to turn a file into a structured
//     entity/relation graph in the background, with a progress bar + ETA.
//
// Drag-and-drop is supported: drop files/folders onto the panel to import them.

import { useCallback, useEffect, useState } from "react";
import { FolderPlus, FilePlus, Search } from "lucide-react";

import { app, onFilesDropped, onRagChanged, onRagProgress } from "../../lib/bridge";
import type { RagCollectionView, RagNodeView, RagProgressEvent, RagSearchHitView } from "../../lib/types";
import { useT } from "../../lib/i18n";
import { useToast } from "../../lib/toast";
import { RagNode } from "./RagNode";

export function RagPanel() {
  const t = useT();
  const { showToast } = useToast();
  const [collections, setCollections] = useState<RagCollectionView[] | null>(null);
  const [activeCollection, setActiveCollection] = useState<string>("");
  const [tree, setTree] = useState<RagNodeView[] | null>(null);
  // Live progress overlays keyed by jobId — merged into the tree on render so
  // we don't refetch the whole tree on every chunk completion.
  const [progressMap, setProgressMap] = useState<Record<string, RagProgressEvent>>({});
  const [searchQuery, setSearchQuery] = useState("");
  const [searchHits, setSearchHits] = useState<RagSearchHitView | null>(null);
  const [searching, setSearching] = useState(false);

  // Refresh tree + collections.
  const refresh = useCallback(async () => {
    try {
      const [cols, nodes] = await Promise.all([
        app.ListRagCollections(),
        app.ListRagTree(activeCollection),
      ]);
      setCollections(cols);
      setTree(nodes);
    } catch {
      setCollections([]);
      setTree([]);
    }
  }, [activeCollection]);

  useEffect(() => { void refresh(); }, [refresh]);

  // Re-fetch on any backend mutation (import/remove/status change).
  useEffect(() => onRagChanged(() => void refresh()), [refresh]);

  // Merge live progress into the tree without refetching. We keep a map of
  // jobId → latest event and apply it at render time so a 50-file folder
  // doesn't cause 50 full tree refetches.
  useEffect(() => {
    return onRagProgress((ev) => {
      setProgressMap((prev) => ({ ...prev, [ev.jobId]: ev }));
    });
  }, []);

  // Debounced search.
  useEffect(() => {
    if (!searchQuery.trim()) {
      setSearchHits(null);
      return;
    }
    setSearching(true);
    const h = setTimeout(() => {
      void app.RagSearch(activeCollection, searchQuery, 5).then((hits) => {
        setSearchHits(hits);
        setSearching(false);
      }).catch(() => setSearching(false));
    }, 300);
    return () => clearTimeout(h);
  }, [searchQuery, activeCollection]);

  const handleImportFolder = async () => {
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

  const handleImportFile = async () => {
    // No multi-file picker binding; reuse dropped-files UX via a single folder
    // pick, or show a hint. For now route to folder pick (most common case).
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

  // Drag-and-drop import: files dropped on the panel are imported directly.
  useEffect(() => {
    return onFilesDropped((paths) => {
      if (paths.length === 0) return;
      void app.RagImportPaths(activeCollection || "default", paths).then((res) => {
        showToast(res.message, "info");
        void refresh();
      }).catch((e) => showToast(String(e), "error"));
    });
  }, [activeCollection, refresh, showToast]);

  const onStartExtract = async (node: RagNodeView) => {
    try {
      await app.RagStartExtract(node.collection || activeCollection || "default", node.path);
      showToast(`深度提取已开始：${node.label}`, "info");
    } catch (e) {
      showToast(String(e), "error");
    }
  };
  const onCancel = async (node: RagNodeView) => {
    if (!node.jobId) return;
    try { await app.RagCancelExtract(node.jobId); } catch (e) { showToast(String(e), "error"); }
  };
  const onRemove = async (node: RagNodeView) => {
    if (!window.confirm(`${t("cowork.ragRemove")}: ${node.label}?`)) return;
    try {
      await app.RagRemovePath(node.collection || activeCollection || "default", node.path);
      void refresh();
    } catch (e) { showToast(String(e), "error"); }
  };

  // Apply live progress overlays to the tree (deep merge by jobId).
  const treeWithProgress = tree ? applyProgress(tree, progressMap) : null;
  const totalEntities = collections?.reduce((s, c) => s + c.entities, 0) ?? 0;

  return (
    <div
      className="cowork-rag"
      // Wails only delivers drops to elements carrying this custom property.
      style={{ "--wails-drop-target": "drop" } as React.CSSProperties}
    >
      <header className="cowork-main__header">
        <h2>{t("cowork.ragCollection")}</h2>
        <div className="cowork-rag__header-actions">
          <button className="btn btn--small" onClick={() => void handleImportFile()} title={t("cowork.ragImportFile")}>
            <FilePlus size={14} />
            {t("cowork.ragImportFile")}
          </button>
          <button className="btn btn--primary btn--small" onClick={() => void handleImportFolder()} title={t("cowork.ragImportFolder")}>
            <FolderPlus size={14} />
            {t("cowork.ragImportFolder")}
          </button>
        </div>
      </header>

      <div className="cowork-rag__body">
        {/* Collection dropdown + stats */}
        <div className="cowork-rag__meta">
          {collections && collections.length > 0 && (
            <select
              className="cowork-rag__select"
              value={activeCollection}
              onChange={(e) => setActiveCollection(e.target.value)}
            >
              <option value="">{t("cowork.ragCollection")}（全部）</option>
              {collections.map((c) => (
                <option key={c.name} value={c.name}>
                  {c.name} · {t("cowork.ragDocs").replace("{n}", String(c.documents))} · {t("cowork.ragEntities").replace("{n}", String(c.entities))}
                </option>
              ))}
            </select>
          )}
          {totalEntities > 0 && (
            <span className="cowork-rag__stat">{t("cowork.ragEntities").replace("{n}", String(totalEntities))}</span>
          )}
        </div>

        {/* Embedded search bar */}
        <div className="cowork-rag__search">
          <Search size={13} className="cowork-rag__search-icon" />
          <input
            className="cowork-rag__search-input"
            placeholder={t("cowork.ragSearchPlaceholder")}
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />
          {searching && <span className="cowork-rag__search-spinner">…</span>}
        </div>
        {searchHits && (searchHits.entities.length > 0 || searchHits.snippets.length > 0) && (
          <div className="cowork-rag__hits">
            {searchHits.entities.length > 0 && (
              <div className="cowork-rag__hits-layer">
                <div className="cowork-rag__hits-label">{t("cowork.ragLayerEntities")}（{searchHits.entities.length}）</div>
                {searchHits.entities.map((e, i) => (
                  <div key={i} className="cowork-rag__entity">
                    <span className="rag-node__badge rag-node__badge--enriched">{e.type}</span>
                    <span className="cowork-rag__entity-name">{e.name}</span>
                    {e.description && <span className="cowork-rag__entity-desc">· {e.description}</span>}
                  </div>
                ))}
              </div>
            )}
            {searchHits.snippets.length > 0 && (
              <div className="cowork-rag__hits-layer">
                <div className="cowork-rag__hits-label">{t("cowork.ragLayerSnippets")}（{searchHits.snippets.length}）</div>
                {searchHits.snippets.map((s, i) => (
                  <div key={i} className="cowork-rag__snippet">
                    <span className="cowork-rag__snippet-path">{s.path}</span>
                    <span className="cowork-rag__snippet-body">{s.snippet}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* File tree */}
        {treeWithProgress === null ? (
          <div className="cowork-rag__loading">…</div>
        ) : treeWithProgress.length === 0 ? (
          <div className="cowork-rag__empty">
            <div>{t("cowork.ragEmpty")}</div>
            <div className="cowork-rag__drop-hint">{t("cowork.ragDropHint")}</div>
          </div>
        ) : (
          <div className="cowork-rag__tree">
            {treeWithProgress.map((node) => (
              <RagNode
                key={node.key}
                node={node}
                depth={0}
                onStartExtract={(n) => void onStartExtract(n)}
                onCancel={(n) => void onCancel(n)}
                onRemove={(n) => void onRemove(n)}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// applyProgress deep-merges live progress events into the tree by jobId. Each
// event carries the latest doneChunks/totalChunks/status for a job, so we walk
// the tree and update any matching file node.
function applyProgress(nodes: RagNodeView[], progress: Record<string, RagProgressEvent>): RagNodeView[] {
  if (Object.keys(progress).length === 0) return nodes;
  return nodes.map((n) => {
    const ev = n.jobId ? progress[n.jobId] : undefined;
    const updated: RagNodeView = ev
      ? {
          ...n,
          doneChunks: ev.doneChunks,
          totalChunks: ev.totalChunks,
          status: mapEventStatus(ev.status),
        }
      : { ...n };
    if (updated.children) {
      updated.children = applyProgress(updated.children, progress);
    }
    return updated;
  });
}

// mapEventStatus translates the job status from a ProgressEvent into the UI's
// node-status vocabulary.
function mapEventStatus(s: string): string {
  switch (s) {
    case "done": return "enriched";
    case "error": return "error";
    case "cancelled": return "cancelled";
    case "extracting":
    case "pending": return "extracting";
    default: return s;
  }
}
