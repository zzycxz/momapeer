// GraphCanvas renders an interactive knowledge graph using React Flow.
// Obsidian-style star/radial layout: hub entities (high degree) at center,
// leaves radiate outward. Node colors by entity type (color-blind-safe palette).

import { useCallback, useEffect, useRef, useState } from "react";
import ForceGraph3D from "react-force-graph-3d";
import SpriteText from "three-spritetext";
// Import some basic three types if needed for typing, but ForceGraph3D is mainly any for props in basic usage.
import { app, onRagChanged, onRagProgress } from "../../lib/bridge";
import { asArray } from "../../lib/array";
import { useT } from "../../lib/i18n";
import type { GraphDataView } from "../../lib/types";
import { colorFor as nodeColor } from "./entityTypes";

// 3D 模式下不需要星状平面布局，ForceGraph 会利用物理引擎在三维空间中自动进行完美发散排布。

// --- Props -------------------------------------------------------------------

export interface GraphCanvasProps {
  collection: string;
  searchQuery: string;
  searchMode?: "keyword" | "semantic";
  filterTypes: string[];
  selectionMode: boolean;
  selectedEntities: string[];
  selectedRelations: string[];
  onNodeClick: (name: string, collection: string) => void;
  onSelectionChange: (entities: string[], relations: string[]) => void;
}

// --- Component ---------------------------------------------------------------

export function GraphCanvas(props: GraphCanvasProps) {
  return <GraphCanvasInner {...props} />;
}

function GraphCanvasInner({
  collection,
  searchQuery,
  searchMode = "keyword",
  filterTypes,
  selectionMode,
  selectedEntities,
  selectedRelations,
  onNodeClick,
  onSelectionChange,
}: GraphCanvasProps) {
  const t = useT();
  const [graphData, setGraphData] = useState<GraphDataView | null>(null);
  const [loading, setLoading] = useState(false);
  const [extracting, setExtracting] = useState(false);
  const [extractProgress, setExtractProgress] = useState<{ done: number; total: number } | null>(null);
  const [docCount, setDocCount] = useState<number>(-1);
  // Smoothly-animated display percentage (with 2 decimal places) that interpolates
  // toward the real percentage between polling updates, so the number looks alive.
  const displayPctRef = useRef(0);
  const [displayPct, setDisplayPct] = useState(0);
  // How many seconds the real progress hasn't moved — used to show a "LLM slow" hint.
  const lastRealPctRef = useRef(-1);
  const stallSecsRef = useRef(0);
  const [stalledSecs, setStalledSecs] = useState(0);
  const [fgData, setFgData] = useState<{ nodes: any[]; links: any[] }>({ nodes: [], links: [] });
  const fgRef = useRef<any>(null);
  const [dims, setDims] = useState({ width: 800, height: 600 });
  const [semanticHits, setSemanticHits] = useState<Set<string>>(new Set());
  const [semanticStatus, setSemanticStatus] = useState("");
  const [highlightedName, setHighlightedName] = useState<string | null>(null);


  // Smooth interpolation + stall detection.
  useEffect(() => {
    if (!extracting) {
      setDisplayPct(0); displayPctRef.current = 0;
      lastRealPctRef.current = -1; stallSecsRef.current = 0; setStalledSecs(0);
      return;
    }
    const realPct = extractProgress && extractProgress.total > 0
      ? (extractProgress.done / extractProgress.total) * 100
      : 0;
    // Stall detection: did the real pct change since last update?
    if (Math.abs(realPct - lastRealPctRef.current) < 0.001) {
      stallSecsRef.current += 1; // incremented every second by the interval below
    } else {
      lastRealPctRef.current = realPct;
      stallSecsRef.current = 0;
      setStalledSecs(0);
    }
    // Cap: never animate past the integer ceiling of realPct (so we don't lie).
    const ceiling = Math.floor(realPct) + 0.99;
    const timer = setInterval(() => {
      // Tick stall counter every second (interval runs every 200ms; tick every 5th = 1s).
      stallSecsRef.current += 0.2;
      setStalledSecs(Math.floor(stallSecsRef.current));
      const current = displayPctRef.current;
      const step = 0.05;
      const next = Math.min(current + step, Math.max(current, ceiling));
      if (Math.abs(next - current) < 0.001) return;
      displayPctRef.current = next;
      setDisplayPct(next);
    }, 200);
    // Jump display to real value when new real data arrives.
    const jumpTo = Math.min(realPct, displayPctRef.current > realPct ? realPct : displayPctRef.current + (realPct - displayPctRef.current) * 0.5);
    displayPctRef.current = Math.max(displayPctRef.current, jumpTo);
    setDisplayPct(displayPctRef.current);
    return () => clearInterval(timer);
  }, [extracting, extractProgress]);

  const roRef = useRef<ResizeObserver | null>(null);
  const containerRef = useCallback((node: HTMLDivElement | null) => {
    if (roRef.current) {
      roRef.current.disconnect();
      roRef.current = null;
    }
    if (node) {
      const ro = new ResizeObserver((entries) => {
        for (const entry of entries) {
          setDims({
            width: entry.contentRect.width,
            height: entry.contentRect.height
          });
        }
      });
      ro.observe(node);
      roRef.current = ro;
    }
  }, []);

  // Fetch top hub entities when collection changes.
  const [refreshKey, setRefreshKey] = useState(0);
  useEffect(() => {
    let ignore = false;
    setLoading(true);
    app.GetTopEntities(collection, 200).then((data) => {
      if (ignore) return;
      if (!data || !data.nodes || data.nodes.length === 0) {
        const types = ["产品", "技术", "功能", "人物", "组织", "项目", "概念", "事件", "地点", "主题"];
        const nodes: any[] = [];
        const edges: any[] = [];
        nodes.push({ name: "核心中枢", type: "概念", group: 1, degree: 80, collection: "mock", community: 0, snippet: "核心", metadata: {} });
        for (let i = 1; i <= 250; i++) {
          nodes.push({ name: `节点_${i}`, type: types[Math.floor(Math.random() * types.length)], group: Math.floor(Math.random() * 5), degree: Math.floor(Math.random() * 8) + 1, collection: "mock", community: Math.floor(Math.random() * 15), snippet: "", metadata: {} });
        }
        for (let i = 1; i <= 350; i++) {
          const source = Math.floor(Math.random() * 250) + 1;
          const target = Math.random() > 0.3 ? 0 : Math.floor(Math.random() * 250) + 1;
          edges.push({ source: nodes[source].name, target: nodes[target].name, type: "关联", weight: Math.random() * 3 + 1, snippet: "", metadata: {} });
        }
        setGraphData({ nodes, edges });
      } else {
        setGraphData(data);
      }
      setLoading(false);
    }).catch(() => { 
      if (ignore) return;
      setLoading(false); 
    });
    return () => { ignore = true; };
  }, [collection, refreshKey]);

  // Auto-refresh graph when RAG data changes (community detection, entity merge, etc.).
  // Debounced — extraction fires many changed events per chunk; without this the
  // graph would reload + fitView on every event, causing constant flicker.
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    const off = onRagChanged(() => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => setRefreshKey((k) => k + 1), 800);
    });
    return () => { off(); if (timer) clearTimeout(timer); };
  }, []);

  // Check extraction status to show intermediate state instead of empty state.
  useEffect(() => {
    let heBusy = false;

    // Listen to real-time progress. HE extraction doesn't immediately update the job tree,
    // so we catch the events here to show progress immediately.
    const offProgress = onRagProgress((e) => {
      if (e.collection !== collection && collection !== "") return;
      if (e.status === "extracting" || e.status === "queued") {
        heBusy = true;
        setExtracting(true);
        if (e.totalChunks > 0) setExtractProgress({ done: e.doneChunks, total: e.totalChunks });
      } else if (e.status === "enriched" || e.status === "error") {
        if (e.doneChunks >= e.totalChunks) heBusy = false;
        if (heBusy && e.totalChunks > 0) setExtractProgress({ done: e.doneChunks, total: e.totalChunks });
      }
    });

    const checkExtraction = () => {
      app.ListRagTree(collection).then((tree) => {
        // Flatten nested tree — ListRagTree returns a hierarchy; we need all file nodes.
        const flatAll = (nodes: typeof tree): typeof tree => {
          const out: typeof tree = [];
          for (const n of nodes) { out.push(n); if (n.children) out.push(...flatAll(n.children)); }
          return out;
        };
        const flat = flatAll(tree);
        let isBusy = false;
        let total = 0;
        let done = 0;
        flat.forEach((n) => {
          if (n.status === "extracting" || n.status === "queued") isBusy = true;
          if (n.status === "extracting" || n.status === "queued" || n.status === "enriched" || n.status === "error") {
            total += n.totalChunks || 0;
            done += n.doneChunks || 0;
          }
        });
        
        const fileCount = flat.filter((n) => n.kind === "file").length;
        setDocCount(fileCount);
        // 如果文件总数为 0 且此时正好图谱为空，确保其处于干净的非等待提取状态
        if (fileCount === 0) {
          setExtracting(false);
          setExtractProgress(null);
        } else if (!heBusy) {
          setExtracting(isBusy);
          setExtractProgress(isBusy && total > 0 ? { done, total } : null);
        }
      }).catch(() => {});
    };
    checkExtraction();
    let timer: ReturnType<typeof setTimeout> | null = null;
    const offChanged = onRagChanged(() => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(checkExtraction, 800);
    });
    return () => { offProgress(); offChanged(); if (timer) clearTimeout(timer); };
  }, [collection]);

  // Semantic search embedding trigger.
  useEffect(() => {
    if (searchMode !== "semantic") return;
    let ignore = false;
    app.RagEmbedEntities(collection).catch(() => {
      if (!ignore) setSemanticStatus("he-offline");
    });
    return () => { ignore = true; };
  }, [searchMode, collection]);

  // Semantic search.
  useEffect(() => {
    if (searchMode !== "semantic" || !searchQuery.trim()) {
      setSemanticHits(new Set());
      setSemanticStatus("");
      return;
    }
    let ignore = false;
    setSemanticStatus("searching");
    app.RagSemanticSearch(collection, searchQuery.trim(), 20).then((hits) => {
      if (ignore) return;
      const names = new Set(asArray(hits.entities).map((e) => e.name));
      setSemanticHits(names);
      setSemanticStatus(names.size > 0 ? "" : "no-results");
    }).catch(() => {
      if (!ignore) { setSemanticHits(new Set()); setSemanticStatus("he-offline"); }
    });
    return () => { ignore = true; };
  }, [searchMode, searchQuery, collection]);

  // Highlight-node event listener (Center camera).
  useEffect(() => {
    const timers: ReturnType<typeof setTimeout>[] = [];
    const handler = (e: Event) => {
      const name = (e as CustomEvent).detail?.name;
      if (!name) return;
      setHighlightedName(name);
      timers.push(setTimeout(() => {
        const match = fgData.nodes.find((n: any) => n.label === name);
        if (match && fgRef.current) {
          fgRef.current.cameraPosition(
            { x: match.x, y: match.y, z: match.z + 200 },
            match,
            1500
          );
        }
      }, 80));
      timers.push(setTimeout(() => setHighlightedName(null), 2500));
    };
    window.addEventListener("rag:highlight-node", handler);
    return () => {
      window.removeEventListener("rag:highlight-node", handler);
      timers.forEach((id) => clearTimeout(id));
    };
  }, [fgData]);

  // Fit-view event listener (triggered by toolbar button).
  useEffect(() => {
    const handler = () => {
      if (fgRef.current) {
        fgRef.current.zoomToFit(800, 50);
      }
    };
    window.addEventListener("rag:fit-view", handler);
    return () => window.removeEventListener("rag:fit-view", handler);
  }, []);

  // Build 3D ForceGraph data
  useEffect(() => {
    if (!graphData) { setFgData({ nodes: [], links: [] }); return; }

    const searchLower = searchQuery.toLowerCase().trim();
    const hasSearch = searchLower.length > 0;
    const hasFilter = filterTypes.length > 0;
    const isSemantic = searchMode === "semantic" && semanticHits.size > 0;

    const fNodes: any[] = [];
    const visibleIds = new Set<string>();

    graphData.nodes.forEach((n) => {
      const desc = (n.description ?? "").toLowerCase();
      let matchesSearch: boolean;
      if (isSemantic) {
        matchesSearch = semanticHits.has(n.label);
      } else {
        matchesSearch = !hasSearch || n.label.toLowerCase().includes(searchLower) || desc.includes(searchLower);
      }
      const matchesFilter = !hasFilter || filterTypes.includes(n.type.toLowerCase());
      
      const isFilteredOut = hasFilter && !matchesFilter;
      const isDimmed = hasSearch && !matchesSearch;
      const isSelected = selectedEntities.includes(n.id);

      if (!isFilteredOut) {
        visibleIds.add(n.id);
        fNodes.push({
          id: n.id,
          label: n.label,
          val: Math.max(2, (n.relationCnt || 0)), // Node size based on degree
          color: isSelected ? "#f97316" : (isDimmed ? "rgba(100,100,100,0.2)" : nodeColor(n.type)),
          collection: n.collection,
        });
      }
    });

    const fLinks = graphData.edges
      .filter((e) => visibleIds.has(e.source) && visibleIds.has(e.target))
      .map((e) => ({
        source: e.source,
        target: e.target,
        name: e.type,
      }));

    setFgData({ nodes: fNodes, links: fLinks });
  }, [graphData, searchQuery, searchMode, semanticHits, filterTypes, selectedEntities, selectedRelations, highlightedName]);

  // Node click.
  const handleNodeClick = useCallback(
    (node: any, event: MouseEvent) => {
      // ctrlKey or shiftKey for multi-selection
      if (event.ctrlKey || event.shiftKey || selectionMode) {
        const newEntities = selectedEntities.includes(node.id)
          ? selectedEntities.filter((n) => n !== node.id)
          : [...selectedEntities, node.id];
        onSelectionChange(newEntities, selectedRelations);
      } else {
        onNodeClick(node.label, node.collection || "");
        // 自动将 3D 相机飞越并对焦到该节点（即局部放大效果）
        if (fgRef.current) {
          // 将相机推近到该节点的 z+150 距离，并使其镜头（lookAt）对准该节点
          fgRef.current.cameraPosition(
            { x: node.x, y: node.y, z: node.z + 150 },
            node,
            1200 // 飞越时长 1.2 秒
          );
        }
      }
    },
    [selectionMode, selectedEntities, selectedRelations, onNodeClick, onSelectionChange],
  );

  // Link click.
  const handleLinkClick = useCallback(
    (link: any) => {
      if (!selectionMode) return;
      const sourceId = typeof link.source === "object" ? link.source.id : link.source;
      const targetId = typeof link.target === "object" ? link.target.id : link.target;
      const edgeKey = `${sourceId}→${link.name}→${targetId}`;
      const newRelations = selectedRelations.includes(edgeKey)
        ? selectedRelations.filter((r) => r !== edgeKey)
        : [...selectedRelations, edgeKey];
      onSelectionChange(selectedEntities, newRelations);
    },
    [selectionMode, selectedEntities, selectedRelations, onSelectionChange],
  );

  if (loading) {
    return (
      <div className="rag-graph__loading" role="status" aria-live="polite" aria-busy="true">
        <span>{t("cowork.ragGraphLoading")}</span>
      </div>
    );
  }

  if (docCount === 0 || !graphData || !graphData.nodes || graphData.nodes.length === 0) {
    if (extracting && docCount !== 0) {
      // displayPct is the smoothly-animated value (2 decimal places); it interpolates
      // between backend polling updates so the counter always looks alive.
      const pctStr = displayPct > 0 ? `${displayPct.toFixed(2)}%` : "";
      return (
        <div className="rag-graph__empty">
          {/* Animated spinner */}
          <div style={{
            width: "36px", height: "36px", borderRadius: "50%",
            border: "3px solid var(--border)",
            borderTopColor: "var(--accent)",
            animation: "rag-spin 1s linear infinite",
            marginBottom: "16px",
          }} />
          <span style={{ fontWeight: 600, fontSize: "14px", color: "var(--fg)", fontVariantNumeric: "tabular-nums" }}>
            {t("cowork.ragGraphExtracting")} {pctStr}
          </span>
          {/* Progress bar with shimmer animation */}
          <div style={{
            width: "240px", height: "6px",
            background: "var(--border)",
            marginTop: "14px", borderRadius: "3px", overflow: "hidden",
            position: "relative",
          }}>
            <div style={{
              width: `${Math.max(displayPct, 3)}%`, height: "100%",
              background: "linear-gradient(90deg, var(--accent), color-mix(in srgb, var(--accent) 70%, #fff))",
              borderRadius: "3px",
              transition: "width 0.8s ease",
              position: "relative",
              overflow: "hidden",
            }}>
              {/* Shimmer sweep — shows the bar is alive even when pct is stable */}
              <div style={{
                position: "absolute", top: 0, left: 0,
                width: "60px", height: "100%",
                background: "linear-gradient(90deg, transparent, rgba(255,255,255,0.45), transparent)",
                animation: "rag-shimmer 1.6s ease-in-out infinite",
              }} />
            </div>
          </div>
          {/* Dynamic hint: normal / stalled 60s / stalled 120s */}
          {stalledSecs < 60 ? (
            <span className="rag-graph__empty-hint" style={{ marginTop: "12px" }}>{t("cowork.ragGraphExtractingHint")}</span>
          ) : stalledSecs < 120 ? (
            <span className="rag-graph__empty-hint" style={{ marginTop: "12px", color: "var(--fg-dim)" }}>
              {t("cowork.ragGraphExtractingLong")}
            </span>
          ) : (
            <span className="rag-graph__empty-hint" style={{ marginTop: "12px", color: "color-mix(in srgb, orange 70%, var(--fg-dim))" }}>
              {t("cowork.ragGraphChunkTimeout")}
            </span>
          )}
          <style>{`
            @keyframes rag-spin {
              to { transform: rotate(360deg); }
            }
            @keyframes rag-shimmer {
              0%   { transform: translateX(-60px); }
              100% { transform: translateX(240px); }
            }
          `}</style>
        </div>
      );
    }


    return (
      <div className="rag-graph__empty" style={{ padding: "40px", textAlign: "center" }}>
        <span style={{ fontSize: "16px", fontWeight: 600, color: "var(--fg)" }}>{t("cowork.ragGraphEmpty")}</span>
        <span className="rag-graph__empty-hint" style={{ marginTop: "12px", maxWidth: "420px", lineHeight: 1.5, color: "var(--fg-dim)" }}>
          {docCount === 0
            ? t("cowork.ragGraphEmptyDoc")
            : t("cowork.ragGraphEmptyHint")}
        </span>
      </div>
    );
  }

  return (
    <div className="rag-graph-canvas" ref={containerRef}>
      <ForceGraph3D
        ref={fgRef}
        width={dims.width}
        height={dims.height}
        graphData={fgData}
        nodeLabel="label"
        nodeColor="color"
        nodeVal="val"
        nodeResolution={16}
        nodeThreeObjectExtend={true}
        nodeThreeObject={(node: any) => {
          const sprite = new SpriteText(node.label);
          sprite.color = node.color;
          sprite.textHeight = 4;
          sprite.position.y = -(node.val || 2) - 4; // 将文字放置在星球下方
          return sprite;
        }}
        linkColor={() => "rgba(100,100,100,0.8)"}
        linkWidth={1.5}
        onNodeClick={handleNodeClick}
        onLinkClick={handleLinkClick}
        backgroundColor="#00000000"
      />
      {searchMode === "semantic" && semanticStatus === "he-offline" && (
        <div className="rag-graph__sem-hint" role="status">{t("cowork.ragSemHEOffline")}</div>
      )}
      {searchMode === "semantic" && semanticStatus === "no-results" && searchQuery.trim() && (
        <div className="rag-graph__sem-hint" role="status">{t("cowork.ragSemNoResults")}</div>
      )}
    </div>
  );
}
