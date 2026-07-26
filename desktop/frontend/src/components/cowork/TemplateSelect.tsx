// TemplateSelect provides the deep extraction UI: template selection, silent/
// immediate extraction modes, progress tracking, and result display. Shown in
// CoworkDock when the user clicks "深度提取".

import { useEffect, useRef, useState } from "react";
import { Zap, Clock, RefreshCw, ArrowRight, Eye, Trash2 } from "lucide-react";

import { app } from "../../lib/bridge";
import { asArray } from "../../lib/array";
import { useToast } from "../../lib/toast";
import type { RagExtractResultView, RagEntityBrief, RagNodeView } from "../../lib/types";
import { ENTITY_TYPE_LABELS, ENTITY_TYPE_COLORS } from "./entityTypes";

interface TemplateField {
  name: string;
  description: string;
}

interface Template {
  name: string;
  displayName: string;
  description: string;
  category: string;
  available: boolean;
  templateType: string;
  entityFields: TemplateField[];
  relationFields: TemplateField[];
}

interface ExtractJob {
  id: string;
  collection: string;
  path: string;
  template: string;
  status: string;
  progress: number;
  error: string;
  entities: number;
  relations: number;
}

export interface TemplateSelectProps {
  collection: string;
  onBack: () => void;
  onViewGraph?: () => void;
}

const MAX_POLL_TICKS = 150; // 150 × 2s = 5min max

export function TemplateSelect({ collection, onBack, onViewGraph }: TemplateSelectProps) {
  const { showToast } = useToast();
  const [templates, setTemplates] = useState<Template[]>([]);
  const [heReady, setHeReady] = useState<boolean | null>(null);
  const [selectedTemplate, setSelectedTemplate] = useState("general/graph");
  const [jobs, setJobs] = useState<ExtractJob[]>([]);
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<RagExtractResultView | null>(null);
  const [showResult, setShowResult] = useState(false);
  const [, setDocCount] = useState<number>(-1); // -1 = loading; setter used in effect
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Load templates, HE health, and document count on mount.
  useEffect(() => {
    app.HEHealth().then((h) => setHeReady(h.ready)).catch(() => setHeReady(false));
    app.RagListHETemplates().then((ts) => {
      if (ts.length > 0) setTemplates(ts);
    }).catch(() => {});
    app.RagExtractResult(collection).then((r) => {
      if (r.hasData) setResult(r);
    }).catch(() => {});
    // Check if the collection has any documents (flatten tree to find all file nodes).
    app.ListRagTree(collection).then((tree) => {
      const flatAll = (nodes: RagNodeView[]): RagNodeView[] => {
        const out: RagNodeView[] = [];
        for (const n of nodes) { out.push(n); if (n.children) out.push(...flatAll(n.children)); }
        return out;
      };
      const count = flatAll(tree).filter((n) => n.kind === "file").length;
      setDocCount(count);
    }).catch(() => setDocCount(0));
  }, [collection]);

  // Clean up polling on unmount.
  useEffect(() => {
    return () => {
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = null;
      }
    };
  }, []);

  const stopPolling = () => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  };

  // Cancel the current extraction: stop polling and re-enable the UI.
  // Note: this only resets the front-end state — the backend worker continues
  // processing remaining chunks in the background (by design, no orphaned jobs).
  const handleCancel = () => {
    stopPolling();
    setLoading(false);
    showToast("已停止监听进度，提取仍在后台继续", "info");
  };

  const handleSilentExtract = async () => {
    setLoading(true);
    try {
      await app.RagStartExtract(collection, selectedTemplate, "silent");
      onBack();
    } catch (e) {
      const msg = String(e);
      if (msg.includes("no documents")) {
        showToast("当前集合暂无文档，请先导入文件", "error");
      } else {
        showToast(`启动提取失败：${msg}`, "error");
      }
    } finally {
      setLoading(false);
    }
  };

  const handleFullExtract = async () => {
    if (!window.confirm("确定重新提取全部文档？已有实体和关系将被清空。")) return;
    setLoading(true);
    setJobs([]);
    setShowResult(false);
    setResult(null);
    try {
      await app.RagStartExtract(collection, selectedTemplate, "full");
      stopPolling();
      let ticks = 0;
      pollRef.current = setInterval(() => {
        ticks++;
        Promise.all([
          app.ListRagTree(collection),
          app.RagExtractResult(collection),
        ]).then(([tree, extractResult]) => {
          const flat = flatNodes(tree);
          const mapped: ExtractJob[] = flat
            .filter((n) => n.status === "extracting" || n.status === "queued" || n.status === "error" || n.status === "enriched")
            .map((n) => ({
              id: n.jobId || n.key,
              collection,
              path: n.path || n.label,
              template: selectedTemplate,
              status: n.status === "error" ? "failed" : n.status === "enriched" ? "done" : n.status,
              progress: n.totalChunks > 0 ? Math.round((n.doneChunks / n.totalChunks) * 100) : 0,
              error: n.errorMsg ?? "",
              entities: n.entityCount ?? 0,
              relations: 0,
            }));
          setJobs(mapped);
          setResult(extractResult);
          const fileNodes = flat.filter((n) => n.kind === "file");
          const allDone = fileNodes.length === 0 || fileNodes.every(
            (n) => n.status !== "extracting" && n.status !== "queued" && n.status !== "pending"
          );
          if (allDone || ticks >= MAX_POLL_TICKS) {
            stopPolling();
            setLoading(false);
            const errorCount = mapped.filter((j) => j.status === "failed").length;
            if (errorCount > 0) {
              showToast(`提取完成，但有 ${errorCount} 个文件失败`, "warn");
            }
            if (extractResult.hasData) {
              setShowResult(true);
            }
          }
        }).catch(() => {
          if (ticks >= MAX_POLL_TICKS) {
            stopPolling();
            setLoading(false);
          }
        });
      }, 2000);
    } catch (e) {
      setLoading(false);
      showToast(`启动提取失败：${e}`, "error");
    }
  };

  // Helper: recursively flatten the nested tree into a flat list of all nodes.
  const flatNodes = (nodes: RagNodeView[]): RagNodeView[] => {
    const result: RagNodeView[] = [];
    for (const n of nodes) {
      result.push(n);
      if (n.children && n.children.length > 0) {
        result.push(...flatNodes(n.children));
      }
    }
    return result;
  };

  const handleImmediateExtract = async () => {
    setLoading(true);
    setJobs([]);
    setShowResult(false);
    setResult(null);

    try {
      await app.RagStartExtract(collection, selectedTemplate, "incremental");
      // Poll for progress with timeout guard.
      stopPolling();
      let ticks = 0;
      pollRef.current = setInterval(() => {
        ticks++;
        // Fetch both tree progress and extraction result.
        Promise.all([
          app.ListRagTree(collection),
          app.RagExtractResult(collection),
        ]).then(([tree, extractResult]) => {
          // Flatten nested tree so we can find file-level status regardless of folder depth.
          const flat = flatNodes(tree);
          // Map tree nodes to job-like progress objects.
          const mapped: ExtractJob[] = flat
            .filter((n) => n.status === "extracting" || n.status === "queued" || n.status === "error" || n.status === "enriched")
            .map((n) => ({
              id: n.jobId || n.key,
              collection,
              path: n.path || n.label,
              template: selectedTemplate,
              status: n.status === "error" ? "failed" : n.status === "enriched" ? "done" : n.status,
              progress: n.totalChunks > 0 ? Math.round((n.doneChunks / n.totalChunks) * 100) : 0,
              error: n.errorMsg ?? "",
              entities: n.entityCount ?? 0,
              relations: 0,
            }));
          setJobs(mapped);
          setResult(extractResult);

          const fileNodes = flat.filter((n) => n.kind === "file");
          // A file is "settled" when it's done, enriched, or errored (not still
          // extracting or queued). allDone = every file has settled.
          const allDone = fileNodes.length === 0 || fileNodes.every(
            (n) => n.status !== "extracting" && n.status !== "queued" && n.status !== "pending"
          );
          if (allDone || ticks >= MAX_POLL_TICKS) {
            stopPolling();
            setLoading(false);
            // Show error summary if some chunks failed.
            const errorCount = mapped.filter((j) => j.status === "failed").length;
            if (errorCount > 0) {
              showToast(`提取完成，但有 ${errorCount} 个文件失败（可能是 API 限流或内容不合规）`, "warn");
            }
            if (extractResult.hasData) {
              setShowResult(true);
            }
          }
        }).catch(() => {
          if (ticks >= MAX_POLL_TICKS) {
            stopPolling();
            setLoading(false);
          }
        });
      }, 2000);
    } catch (e) {
      setLoading(false);
      const msg = String(e);
      if (msg.includes("no documents")) {
        showToast("当前集合暂无文档，请先导入文件", "error");
      } else {
        showToast(`启动提取失败：${msg}`, "error");
      }
    }
  };


  // Result panel: show after extraction completes.
  if (showResult && result) {
    return <ExtractionResult result={result} onBack={onBack} onViewGraph={onViewGraph} onReExtract={() => { setShowResult(false); setResult(null); }} />;
  }

  return (
    <div className="rag-template">
      {/* Header */}
      <div className="rag-template__header">
        <button className="rag-template__back" onClick={onBack}>←</button>
        <span className="rag-template__title">深度提取</span>
      </div>

      {/* Collection */}
      <div className="rag-template__section">
        <span className="rag-template__label">集合</span>
        <span className="rag-template__value">{collection || "全部"}</span>
      </div>

      {/* HE service status */}
      {heReady !== null && (
        <div className="rag-template__section">
          <span className="rag-template__label">提取引擎</span>
          <span className={`rag-template__status ${heReady ? "rag-template__status--ok" : "rag-template__status--info"}`}>
            {heReady
              ? "Hyper-Extract 就绪（模板抽取可用）"
              : "内置九天提取引擎（Hyper-Extract 模板未启用）"}
          </span>
        </div>
      )}

      {/* Template selection */}
      <div className="rag-template__section">
        <span className="rag-template__label">提取模板</span>
        <div className="rag-template__list">
          {templates.length > 0 ? templates.map((t) => {
            const entityFields = asArray(t.entityFields);
            const relationFields = asArray(t.relationFields);
            return (
            <div
              key={t.name}
              className={`rag-template__item ${selectedTemplate === t.name ? "rag-template__item--selected" : ""}`}
              onClick={() => setSelectedTemplate(t.name)}
            >
              <div className="rag-template__item-head">
                <span className="rag-template__item-name">{t.displayName}</span>
                {t.templateType && <span className="rag-template__item-type">{t.templateType}</span>}
              </div>
              <span className="rag-template__item-desc">{t.description}</span>
              {(entityFields.length > 0 || relationFields.length > 0) && (
                <div className="rag-template__item-fields">
                  {entityFields.length > 0 && (
                    <span className="rag-template__field-group">
                      <span className="rag-template__field-label">实体</span>
                      {entityFields.map((f) => (
                        <span key={f.name} className="rag-template__field-chip" title={f.description}>{f.name}</span>
                      ))}
                    </span>
                  )}
                  {relationFields.length > 0 && (
                    <span className="rag-template__field-group">
                      <span className="rag-template__field-label">关系</span>
                      {relationFields.map((f) => (
                        <span key={f.name} className="rag-template__field-chip" title={f.description}>{f.name}</span>
                      ))}
                    </span>
                  )}
                </div>
              )}
            </div>
            );
          }) : (
            <div className="rag-template__empty">暂无可用模板</div>
          )}
        </div>
      </div>

      {/* Action buttons */}
      <div className="rag-template__actions">
        {loading ? (
          // While extracting: show a cancel/unlock button so the user isn't stuck
          <button
            className="btn rag-template__btn"
            onClick={handleCancel}
            style={{ color: "var(--fg-dim)", borderColor: "var(--border)" }}
          >
            <RefreshCw size={14} />
            <span>停止监听</span>
          </button>
        ) : (
          <>
            <button
              className="btn btn--primary rag-template__btn"
              onClick={() => void handleImmediateExtract()}
              disabled={loading}
            >
              <Zap size={14} />
              <span>增量理解</span>
            </button>
            <button
              className="btn rag-template__btn"
              onClick={() => void handleFullExtract()}
              disabled={loading}
              title="清空已有实体，重新提取全部文档"
            >
              <RefreshCw size={14} />
              <span>重新理解</span>
            </button>
            <button
              className="btn rag-template__btn"
              onClick={() => void handleSilentExtract()}
              disabled={loading}
              title="后台静默提取，不等待"
            >
              <Clock size={14} />
              <span>静默理解</span>
            </button>
          </>
        )}
      </div>

      {/* Live stats during extraction */}
      {loading && result && (
        <div className="rag-template__live-stats">
          <span className="rag-template__live-stat">
            实体 <strong>{result.entityCount}</strong>
          </span>
          <span className="rag-template__live-stat">
            关系 <strong>{result.relationCount}</strong>
          </span>
        </div>
      )}

      {/* Progress */}
      {jobs.length > 0 && (
        <div className="rag-template__progress">
          <span className="rag-template__progress-title">提取进度</span>
          {jobs.map((j) => (
            <div key={j.id} className={`rag-template__job rag-template__job--${j.status}`}>
              <span className="rag-template__job-path">{j.path.split(/[/\\]/).pop()}</span>
              <span className="rag-template__job-status">
                {j.status === "done" ? `✓ ${j.entities}实体` :
                 j.status === "running" || j.status === "extracting" ? `... ${j.progress}%` :
                 j.status === "failed" ? `! ${j.error}` :
                 j.status}
              </span>
              {j.status === "failed" && (
                <button className="rag-template__retry" onClick={() => void handleImmediateExtract()}>
                  <RefreshCw size={12} />
                </button>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Existing result hint */}
      {!loading && result && result.hasData && !showResult && (
        <div className="rag-template__hint">
          <span>已有 {result.entityCount} 个实体、{result.relationCount} 条关系</span>
          <button className="btn btn--link" onClick={() => setShowResult(true)}>
            <Eye size={13} /> 查看结果
          </button>
          <button className="btn btn--link btn--danger-link" onClick={async () => {
            if (window.confirm("确认清理所有提取的知识？文档不会被删除，可重新提取。")) {
              await app.RagCleanCollection(collection);
              setResult(null);
            }
          }}>
            <Trash2 size={13} /> 清理知识
          </button>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
//  ExtractionResult — post-extraction summary panel
// ---------------------------------------------------------------------------

function ExtractionResult({ result, onBack, onViewGraph, onReExtract }: {
  result: RagExtractResultView;
  onBack: () => void;
  onViewGraph?: () => void;
  onReExtract: () => void;
}) {
  // Group entities by type for distribution bar.
  const typeGroups = new Map<string, number>();
  for (const e of result.topEntities) {
    typeGroups.set(e.type, (typeGroups.get(e.type) ?? 0) + 1);
  }
  const totalEntities = result.entityCount || 1; // avoid /0

  return (
    <div className="rag-result">
      {/* Header */}
      <div className="rag-result__header">
        <button className="rag-template__back" onClick={onBack}>←</button>
        <span className="rag-result__title">提取结果</span>
      </div>

      {/* Summary */}
      <div className="rag-result__summary">
        <div className="rag-result__stat">
          <span className="rag-result__stat-num">{result.entityCount}</span>
          <span className="rag-result__stat-label">实体</span>
        </div>
        <div className="rag-result__stat">
          <span className="rag-result__stat-num">{result.relationCount}</span>
          <span className="rag-result__stat-label">关系</span>
        </div>
        <div className="rag-result__stat">
          <span className="rag-result__stat-num">{result.doneCount}/{result.jobCount}</span>
          <span className="rag-result__stat-label">文件</span>
        </div>
      </div>

      {/* Entity type distribution */}
      {typeGroups.size > 0 && (
        <div className="rag-result__section">
          <div className="rag-result__section-title">实体类型分布</div>
          <div className="rag-result__distribution">
            {Array.from(typeGroups.entries()).map(([type, count]) => (
              <div key={type} className="rag-result__dist-row">
                <span className="rag-result__dist-label">{ENTITY_TYPE_LABELS[type] ?? type}</span>
                <div className="rag-result__dist-bar">
                  <div
                    className="rag-result__dist-fill"
                    style={{
                      width: `${(count / totalEntities) * 100}%`,
                      background: ENTITY_TYPE_COLORS[type] ?? "#95A5A6",
                    }}
                  />
                </div>
                <span className="rag-result__dist-count">{count}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Top entities */}
      {result.topEntities.length > 0 && (
        <div className="rag-result__section">
          <div className="rag-result__section-title">高频实体</div>
          <div className="rag-result__entity-list">
            {result.topEntities.slice(0, 10).map((e) => (
              <EntityCard key={e.name} entity={e} />
            ))}
          </div>
        </div>
      )}

      {/* Top relations */}
      {result.topRelations.length > 0 && (
        <div className="rag-result__section">
          <div className="rag-result__section-title">关系示例</div>
          <div className="rag-result__relation-list">
            {result.topRelations.slice(0, 5).map((r, i) => (
              <div key={i} className="rag-result__relation">
                <span className="rag-result__rel-node">{r.source}</span>
                <span className="rag-result__rel-type">{r.type}</span>
                <ArrowRight size={12} />
                <span className="rag-result__rel-node">{r.target}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Actions */}
      <div className="rag-result__actions">
        {onViewGraph && (
          <button className="btn btn--primary" onClick={onViewGraph}>
            <Eye size={14} />
            <span>查看图谱</span>
          </button>
        )}
        <button className="btn" onClick={onReExtract}>
          <RefreshCw size={14} />
          <span>换模板重提取</span>
        </button>
      </div>
    </div>
  );
}

function EntityCard({ entity }: { entity: RagEntityBrief }) {
  const label = ENTITY_TYPE_LABELS[entity.type] ?? entity.type;
  const color = ENTITY_TYPE_COLORS[entity.type] ?? "#95A5A6";
  return (
    <div className="rag-result__entity">
      <span className="rag-result__entity-type" style={{ background: color }}>{label}</span>
      <div className="rag-result__entity-info">
        <span className="rag-result__entity-name">{entity.nameRaw || entity.name}</span>
        {entity.description && (
          <span className="rag-result__entity-desc">{entity.description.slice(0, 60)}{entity.description.length > 60 ? "…" : ""}</span>
        )}
      </div>
      {entity.relationCount > 0 && (
        <span className="rag-result__entity-rels">{entity.relationCount} 关系</span>
      )}
    </div>
  );
}
