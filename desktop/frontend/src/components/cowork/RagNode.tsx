// RagNode renders one node (file or folder) of the RAG tree, recursively for
// folders. File nodes carry FTS5 + extraction status, a progress bar (when
// extracting), and action buttons (deep extract / cancel / remove). The ETA
// tooltip on the progress bar shows "已 X/Y 块 · 平均 Zs/块 · 预计还需 N分M秒".
//
// The node is presentational except for hover-triggered ETA probes — it calls
// back up to RagPanel for mutations so the network surface stays centralized.

import { useEffect, useState } from "react";
import { ChevronRight, File as FileIcon, Folder, FolderOpen, Info, Zap, Ban, Trash2, RefreshCw } from "lucide-react";

import type { RagETAView, RagNodeView } from "../../lib/types";
import { app } from "../../lib/bridge";
import { useT, type Translator } from "../../lib/i18n";
import { Tooltip } from "../Tooltip";

export function RagNode({
  node,
  depth,
  onStartExtract,
  onCancel,
  onRemove,
}: {
  node: RagNodeView;
  depth: number;
  onStartExtract: (node: RagNodeView) => void;
  onCancel: (node: RagNodeView) => void;
  onRemove: (node: RagNodeView) => void;
}) {
  const t = useT();
  const [expanded, setExpanded] = useState(depth < 1); // auto-expand first level
  const [eta, setEta] = useState<RagETAView | null>(null);

  const pad = 8 + depth * 16;
  const hasChildren = node.kind === "folder" && node.children && node.children.length > 0;
  const isExtracting = node.status === "extracting";

  // Poll ETA when extracting (throttled on hover). We fetch on a 3s interval
  // while the node is in the extracting state, so the tooltip stays fresh.
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
    <div className="rag-node">
      <div
        className={`rag-node__row ${node.kind === "folder" ? "rag-node__row--folder" : "rag-node__row--file"}`}
        style={{ paddingLeft: pad }}
      >
        {node.kind === "folder" ? (
          <>
            <button
              className="rag-node__chevron"
              onClick={() => setExpanded((v) => !v)}
              title={expanded ? "collapse" : "expand"}
            >
              <ChevronRight size={12} className={expanded ? "rag-node__chevron--open" : ""} />
            </button>
            {expanded ? <FolderOpen size={13} /> : <Folder size={13} />}
            <span className="rag-node__label">{node.label}</span>
          </>
        ) : (
          <>
            <span className="rag-node__chevron rag-node__chevron--placeholder" />
            <FileIcon size={13} />
            <span className="rag-node__label" title={node.path}>{node.label}</span>
            <StatusBadge node={node} t={t} />
            {isExtracting && (
              <div className="rag-node__progress">
                <progress
                  className="rag-progress-bar"
                  value={node.doneChunks}
                  max={node.totalChunks || 1}
                />
                <span className="rag-node__pct">
                  {node.totalChunks > 0 ? Math.round((node.doneChunks / node.totalChunks) * 100) : 0}%
                </span>
                <Tooltip label={etaLabel(eta, node, t)} side="top">
                  <Info size={13} className="rag-node__eta-icon" />
                </Tooltip>
              </div>
            )}
            <div className="rag-node__actions">
              {isExtracting ? (
                <button className="rag-node__btn" title={t("cowork.ragCancel")} onClick={() => onCancel(node)}>
                  <Ban size={14} />
                </button>
              ) : (
                <button
                  className="rag-node__btn rag-node__btn--accent"
                  title={node.status === "enriched" ? t("cowork.ragReExtract") : t("cowork.ragDeepExtract")}
                  onClick={() => onStartExtract(node)}
                >
                  {node.status === "error" ? <RefreshCw size={14} /> : <Zap size={14} />}
                </button>
              )}
              <button className="rag-node__btn rag-node__btn--danger" title={t("cowork.ragRemove")} onClick={() => onRemove(node)}>
                <Trash2 size={14} />
              </button>
            </div>
          </>
        )}
      </div>
      {hasChildren && expanded && (
        <div className="rag-node__children">
          {node.children!.map((child) => (
            <RagNode
              key={child.key}
              node={child}
              depth={depth + 1}
              onStartExtract={onStartExtract}
              onCancel={onCancel}
              onRemove={onRemove}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// StatusBadge renders the small colored chip next to a file: FTS5✓ / 抽取中 /
// 已抽取 N 实体 / 出错 / 已取消.
function StatusBadge({ node, t }: { node: RagNodeView; t: Translator }) {
  const cls = `rag-node__badge rag-node__badge--${node.status}`;
  switch (node.status) {
    case "extracting":
      return <span className={cls}>{t("cowork.ragStatusExtracting")}</span>;
    case "enriched":
      return <span className={cls}>{t("cowork.ragStatusEnriched")}</span>;
    case "error":
      return <span className={cls} title={node.errorMsg}>{t("cowork.ragStatusError")}</span>;
    case "cancelled":
      return <span className={cls}>{t("cowork.ragStatusCancelled")}</span>;
    default:
      return node.hasFts5 ? <span className={cls}>{t("cowork.ragStatusIndexed")}</span> : null;
  }
}

// etaLabel formats the hover tooltip: "已 X/Y 块 · 平均 Zs/块 · 预计还需 N分M秒".
function etaLabel(
  eta: RagETAView | null,
  node: RagNodeView,
  t: Translator,
): string {
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
