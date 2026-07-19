// RagNode renders one node (file or folder) of the RAG tree, recursively for
// folders. Styled to match the coding-mode workspace tree (workspace-tree__row):
// clean rows, shared --tree-row-* variables, chevron + icon + name. The
// RAG-specific action buttons (extract / cancel / remove) appear on hover so
// the resting row is as quiet as the coding tree, but stay one click away.
//
// Folder rows expand/collapse via the chevron; file rows open the document
// preview via onFileClick (passing the node's own collection so preview works
// even when the dock is in "all collections" scope).

import { useEffect, useState } from "react";
import { ChevronDown, ChevronRight, FileText, Folder, FolderOpen, Info, Zap, Ban, Trash2, RefreshCw } from "lucide-react";

import type { RagETAView, RagNodeView } from "../../lib/types";
import { app } from "../../lib/bridge";
import { useT, type Translator } from "../../lib/i18n";
import { Tooltip } from "../Tooltip";
import { fileIconColor } from "./fileTypeColors";

export function RagNode({
  node,
  depth,
  onStartExtract,
  onCancel,
  onRemove,
  onFileClick,
  selectedPath,
}: {
  node: RagNodeView;
  depth: number;
  onStartExtract: (node: RagNodeView) => void;
  onCancel: (node: RagNodeView) => void;
  onRemove: (node: RagNodeView) => void;
  onFileClick?: (node: RagNodeView) => void;
  selectedPath?: string;
}) {
  const t = useT();
  const [expanded, setExpanded] = useState(depth < 1); // auto-expand first level
  const [eta, setEta] = useState<RagETAView | null>(null);

  const active = !!selectedPath && node.path === selectedPath;
  const hasChildren = node.kind === "folder" && node.children && node.children.length > 0;
  const isExtracting = node.status === "extracting";
  const iconColor = node.kind === "file" ? fileIconColor(node.label) : undefined;

  // Poll ETA when extracting so the tooltip stays fresh.
  useEffect(() => {
    if (!isExtracting || !node.jobId) {
      setEta(null);
      return;
    }
    let cancelled = false;
    const fetchETA = () => {
      void app.RagPreviewETA(node.jobId).then((e) => { if (!cancelled) setEta(e); }).catch(() => {});
    };
    fetchETA();
    const h = setInterval(fetchETA, 3000);
    return () => { cancelled = true; clearInterval(h); };
  }, [isExtracting, node.jobId]);

  return (
    <div className={`ragft-node${node.status === "enriched" ? " ragft-node--enriched" : ""}${node.status === "error" ? " ragft-node--error" : ""}`}>
      <div
        className={`ragft-row${active ? " ragft-row--active" : ""}`}
        style={{ paddingLeft: 8 + depth * 14 }}
      >
        {/* Folder: chevron toggles expand. File: no chevron (placeholder keeps alignment). */}
        {node.kind === "folder" ? (
          <button
            type="button"
            className="ragft-chev-btn"
            onClick={() => setExpanded((v) => !v)}
            title={expanded ? "collapse" : "expand"}
          >
            {expanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
          </button>
        ) : (
          <span className="ragft-chev" />
        )}
        {/* Icon + label. Folder label is static; file label opens preview. */}
        {node.kind === "folder" ? (
          expanded ? <FolderOpen size={14} className="ragft-icon ragft-icon--dir" /> : <Folder size={14} className="ragft-icon ragft-icon--dir" />
        ) : (
          <FileText size={14} className="ragft-icon" style={iconColor ? { color: iconColor } : undefined} />
        )}
        <span
          className="ragft-name"
          title={node.kind === "file" ? statusTitle(node, t) : node.label}
          style={node.kind === "file" && onFileClick ? { cursor: "pointer" } : undefined}
          onClick={node.kind === "file" && onFileClick ? () => onFileClick(node) : undefined}
        >
          {node.label}
        </span>

        {/* File-only extras: status text (tiny, faded), inline progress while
            extracting, action buttons on hover. */}
        {node.kind === "file" && (
          <>
            {/* Status text: a tiny faded label at the right edge — always visible
                but quiet (10px, --fg-faint). Shows entity count for enriched,
                error hint for failures; empty when there's nothing to say so the
                resting row stays clean like the coding tree. */}
            {!isExtracting && statusText(node, t) && (
              <span className={`ragft-status ragft-status--${node.status}`} title={statusTitle(node, t)}>
                {statusText(node, t)}
              </span>
            )}
            {isExtracting ? (
              <span className="ragft-progress">
                <progress className="ragft-progress-bar" value={node.doneChunks} max={node.totalChunks || 1} />
                <span className="ragft-pct">{node.totalChunks > 0 ? Math.round((node.doneChunks / node.totalChunks) * 100) : 0}%</span>
                <Tooltip label={etaLabel(eta, node, t)} side="top">
                  <Info size={12} className="ragft-eta-icon" />
                </Tooltip>
              </span>
            ) : null}
            <div className="ragft-actions">
              {isExtracting ? (
                <button type="button" className="ragft-btn" title={t("cowork.ragCancel")} onClick={() => onCancel(node)}>
                  <Ban size={13} />
                </button>
              ) : (
                <button
                  type="button"
                  className="ragft-btn ragft-btn--accent"
                  title={node.status === "enriched" ? t("cowork.ragReExtract") : t("cowork.ragDeepExtract")}
                  onClick={() => onStartExtract(node)}
                >
                  {node.status === "error" ? <RefreshCw size={13} /> : <Zap size={13} />}
                </button>
              )}
              <button type="button" className="ragft-btn ragft-btn--danger" title={t("cowork.ragRemove")} onClick={() => onRemove(node)}>
                <Trash2 size={13} />
              </button>
            </div>
          </>
        )}
      </div>
      {hasChildren && expanded && (
        <div className="ragft-children">
          {node.children!.map((child) => (
            <RagNode
              key={child.key}
              node={child}
              depth={depth + 1}
              onStartExtract={onStartExtract}
              onCancel={onCancel}
              onRemove={onRemove}
              onFileClick={onFileClick}
              selectedPath={selectedPath}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// statusTitle gives the file label hover tooltip a short status line.
function statusTitle(node: RagNodeView, t: Translator): string {
  switch (node.status) {
    case "enriched":
      return t("cowork.ragStatusEnriched") + (node.entityCount > 0 ? ` · ${node.entityCount}` : "");
    case "extracting":
      return t("cowork.ragStatusExtracting");
    case "error":
      return t("cowork.ragStatusError") + (node.errorMsg ? `: ${node.errorMsg}` : "");
    case "cancelled":
      return t("cowork.ragStatusCancelled");
    default:
      return node.hasFts5 ? t("cowork.ragStatusIndexed") : node.label;
  }
}

// statusText returns a compact always-visible label for the right edge of a
// file row: the entity count for enriched files, a short error tag for
// failures, etc. Returns "" when there's nothing meaningful (keeps the row
// clean for plain indexed/empty files). Truncated so it never wraps.
function statusText(node: RagNodeView, t: Translator): string {
  switch (node.status) {
    case "enriched":
      return node.entityCount > 0 ? `${node.entityCount}` : t("cowork.ragStatusEnriched");
    case "extracting":
      return ""; // progress bar handles this state
    case "error":
      return t("cowork.ragStatusError");
    case "cancelled":
      return t("cowork.ragStatusCancelled");
    default:
      return node.hasFts5 ? "✓" : "";
  }
}

// etaLabel formats the hover tooltip: "已 X/Y 块 · 平均 Zs/块 · 预计还需 N分M秒".
function etaLabel(eta: RagETAView | null, node: RagNodeView, t: Translator): string {
  const done = eta?.doneChunks ?? node.doneChunks;
  const total = eta?.totalChunks ?? node.totalChunks;
  const avgMs = eta?.avgLatencyMs ?? 0;
  const etaSec = eta?.etaSeconds ?? 0;
  const avg = avgMs > 0 ? (avgMs / 1000).toFixed(1) + "s" : "—";
  const etaStr =
    etaSec <= 0 ? t("cowork.ragETASoon") : etaSec < 60
      ? t("cowork.ragSeconds").replace("{n}", String(Math.max(1, Math.round(etaSec))))
      : t("cowork.ragMinutes").replace("{n}", String(Math.floor(etaSec / 60))).replace("{s}", String(Math.round(etaSec % 60)));
  return t("cowork.ragETAHint")
    .replace("{done}", String(done))
    .replace("{total}", String(total))
    .replace("{avg}", avg)
    .replace("{eta}", etaStr);
}
