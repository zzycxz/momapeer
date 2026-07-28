// GraphCanvas renders an interactive knowledge graph using React Flow.
// Obsidian-style star/radial layout: hub entities (high degree) at center,
// leaves radiate outward. Node colors by entity type (color-blind-safe palette).

import { memo, useCallback, useEffect, useRef, useState } from "react";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  type Node,
  type Edge,
  type NodeTypes,
  MarkerType,
  useReactFlow,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import { app, onRagChanged, onRagProgress } from "../../lib/bridge";
import { asArray } from "../../lib/array";
import { useT } from "../../lib/i18n";
import type { GraphDataView } from "../../lib/types";
import { colorFor as nodeColor, communityColor } from "./entityTypes";

// --- Custom node component ---------------------------------------------------

const EntityNode = memo(function EntityNode({ data }: {
  data: { label: string; type: string; description: string; relationCnt: number; highlighted: boolean; selected: boolean; community: number; collection: string };
}) {
  const degree = data.relationCnt || 0;
  const color = nodeColor(data.type);
  // Obsidian graph-view: a colored dot (sized by degree/hub-ness) + text label.
  const dotSize = Math.max(8, Math.min(22, 8 + degree * 0.7));
  const isHub = degree >= 5;
  const commColor = communityColor(data.community ?? -1);
  // Ring width scales with dot size so small dots aren't overwhelmed by the ring.
  const ringWidth = Math.max(1.5, dotSize * 0.2);
  return (
    <div
      className={`rag-gnode ${data.highlighted ? "rag-gnode--hl" : ""} ${data.selected ? "rag-gnode--sel" : ""} ${isHub ? "rag-gnode--hub" : ""}`}
    >
      <div
        className="rag-gnode__dot"
        style={{
          width: dotSize,
          height: dotSize,
          background: color,
          boxShadow: commColor !== "transparent" ? `0 0 0 ${ringWidth}px ${commColor}` : undefined,
        }}
      />
      <span className="rag-gnode__label" style={{ color: data.highlighted ? color : undefined }}>{data.label}</span>
    </div>
  );
});

const nodeTypes: NodeTypes = { entity: EntityNode };

// --- Star/radial layout (Obsidian graph style) ------------------------------
// BFS from the top hub outward: center node at origin, its direct neighbors
// form ring 1, their unvisited neighbors form ring 2, etc. Edges become
// visible spokes → the recognizable "star" shape. Deterministic (sorted by
// degree, angular offset by golden angle) so re-renders are stable.

function starLayout(nodes: Node[], edges: Edge[]): Node[] {
  const visible = nodes.filter((n) => !n.hidden);
  if (visible.length === 0) return nodes;

  // Build adjacency from edges (undirected for layout purposes).
  const adj = new Map<string, Set<string>>();
  for (const e of edges) {
    if (!adj.has(e.source)) adj.set(e.source, new Set());
    if (!adj.has(e.target)) adj.set(e.target, new Set());
    adj.get(e.source)!.add(e.target);
    adj.get(e.target)!.add(e.source);
  }

  // Sort by degree desc to pick the hub as BFS root.
  const byDegree = [...visible].sort(
    (a, b) => Number(b.data?.relationCnt ?? 0) - Number(a.data?.relationCnt ?? 0),
  );

  // ring[nodeId] = depth from center.
  const ring = new Map<string, number>();
  // angular slot within the ring.
  const angle = new Map<string, number>();
  // Radius per ring — grows so outer rings have room.
  const ringRadius = [0, 220, 420, 600, 760];

  // BFS in layers, assigning golden-angle angular offsets within each ring.
  const visited = new Set<string>();
  let queue: string[] = [byDegree[0].id];
  visited.add(byDegree[0].id);
  ring.set(byDegree[0].id, 0);
  angle.set(byDegree[0].id, 0);

  while (queue.length > 0) {
    const next: string[] = [];
    for (const id of queue) {
      const neighbors = [...(adj.get(id) ?? [])]
        .filter((n) => !visited.has(n))
        // Stable ordering by degree for deterministic placement.
        .sort((a, b) => {
          const na = byDegree.findIndex((n) => n.id === a);
          const nb = byDegree.findIndex((n) => n.id === b);
          return na - nb;
        });
      for (const nb of neighbors) {
        if (visited.has(nb)) continue;
        visited.add(nb);
        const r = (ring.get(id) ?? 0) + 1;
        ring.set(nb, Math.min(r, ringRadius.length - 1));
        next.push(nb);
      }
    }
    // Assign angular offsets within this BFS frontier using golden angle.
    next.forEach((id, i) => {
      angle.set(id, i * 2.39996323);
    });
    queue = next;
  }

  // Any leftover nodes (disconnected) go to the outermost ring.
  let leftoverSlot = 0;
  for (const n of visible) {
    if (!visited.has(n.id)) {
      ring.set(n.id, ringRadius.length - 1);
      angle.set(n.id, leftoverSlot++ * 2.39996323);
    }
  }

  return nodes.map((n) => {
    if (n.hidden) return n;
    const r = ringRadius[ring.get(n.id) ?? 2] ?? 600;
    const a = angle.get(n.id) ?? 0;
    return {
      ...n,
      position: { x: Math.cos(a) * r, y: Math.sin(a) * r },
    };
  });
}

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
  return (
    <ReactFlowProvider>
      <GraphCanvasInner {...props} />
    </ReactFlowProvider>
  );
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
  const [nodes, setNodes, onNodesChange] = useNodesState([] as Node[]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([] as Edge[]);
  const [semanticHits, setSemanticHits] = useState<Set<string>>(new Set());
  const [semanticStatus, setSemanticStatus] = useState("");
  const [highlightedName, setHighlightedName] = useState<string | null>(null);
  const { fitView, setCenter } = useReactFlow();
  const layoutDone = useRef(false);
  const nodesRef = useRef<Node[]>([]);
  useEffect(() => { nodesRef.current = nodes; }, [nodes]);

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

  // Fetch top hub entities when collection changes.
  const [refreshKey, setRefreshKey] = useState(0);
  useEffect(() => {
    let ignore = false;
    setLoading(true);
    layoutDone.current = false;
    app.GetTopEntities(collection, 200).then((data) => {
      if (ignore) return;
      setGraphData(data);
      setLoading(false);
    }).catch(() => { if (!ignore) setLoading(false); });
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

  // Highlight-node event listener.
  useEffect(() => {
    const handler = (e: Event) => {
      const name = (e as CustomEvent).detail?.name;
      if (!name) return;
      setHighlightedName(name);
      setTimeout(() => {
        const match = nodesRef.current.find((n) => n.data?.label === name);
        if (match) {
          setCenter(match.position.x + 60, match.position.y + 30, { zoom: 1.3, duration: 400 });
        }
      }, 80);
      setTimeout(() => setHighlightedName(null), 2500);
    };
    window.addEventListener("rag:highlight-node", handler);
    return () => window.removeEventListener("rag:highlight-node", handler);
  }, [setCenter]);

  // Fit-view event listener (triggered by toolbar button).
  useEffect(() => {
    const handler = () => fitView({ padding: 0.15, duration: 300 });
    window.addEventListener("rag:fit-view", handler);
    return () => window.removeEventListener("rag:fit-view", handler);
  }, [fitView]);

  // Build nodes + edges + layout in one effect.
  useEffect(() => {
    if (!graphData) { setNodes([]); setEdges([]); return; }

    const searchLower = searchQuery.toLowerCase().trim();
    const hasSearch = searchLower.length > 0;
    const hasFilter = filterTypes.length > 0;
    const isSemantic = searchMode === "semantic" && semanticHits.size > 0;

    // Build nodes.
    const flowNodes: Node[] = graphData.nodes.map((n) => {
      const desc = (n.description ?? "").toLowerCase();
      let matchesSearch: boolean;
      if (isSemantic) {
        matchesSearch = semanticHits.has(n.label);
      } else {
        matchesSearch = !hasSearch || n.label.toLowerCase().includes(searchLower) || desc.includes(searchLower);
      }
      const matchesFilter = !hasFilter || filterTypes.includes(n.type.toLowerCase());
      // Type filter hides nodes completely; search dims non-matches instead of
      // hiding them so the user can still see surrounding context.
      const isFilteredOut = hasFilter && !matchesFilter;
      const isDimmed = hasSearch && !matchesSearch;
      const isSelected = selectedEntities.includes(n.id);
      const isPinned = highlightedName !== null && n.label === highlightedName;

      return {
        id: n.id,
        type: "entity",
        position: { x: 0, y: 0 },
        data: {
          label: n.label,
          type: n.type,
          description: n.description,
          relationCnt: n.relationCnt,
          collection: n.collection,
          community: n.community,
          highlighted: (hasSearch && matchesSearch) || isPinned,
          selected: isSelected,
        },
        hidden: isFilteredOut,
        style: { opacity: isDimmed ? 0.15 : 1 },
      };
    });

    // Build edges — only between visible nodes in the current page.
    const visibleIds = new Set(flowNodes.filter((n) => !n.hidden).map((n) => n.id));
    // Track which nodes matched the search for edge opacity differentiation.
    const matchedIds = new Set<string>();
    if (hasSearch || isSemantic) {
      flowNodes.forEach((n) => {
        if (!n.hidden && n.style?.opacity === 1) matchedIds.add(n.id);
      });
    }
    const flowEdges: Edge[] = graphData.edges
      .filter((e) => visibleIds.has(e.source) && visibleIds.has(e.target))
      .map((e) => {
        const edgeKey = `${e.source}→${e.type}→${e.target}`;
        const isSelected = selectedRelations.includes(edgeKey);
        // Width: combine co-occurrence weight + semantic strength.
        const w = e.weight || 1;
        const s = e.strength || 5;
        const baseWidth = 1.0 + Math.min(w - 1, 4) * 0.4 + Math.max(0, s - 5) * 0.3;
        // Strong edges (strength>=7) use accent-tinted color; weak ones stay dim.
        const isStrong = s >= 7;
        // During search: edges between matched nodes are bright, others are dim.
        const bothMatched = matchedIds.size > 0 && matchedIds.has(e.source) && matchedIds.has(e.target);
        const edgeOpacity = hasSearch ? (bothMatched ? (isStrong ? 0.8 : 0.5) : 0.08) : (isStrong ? 0.75 : 0.4);
        const edgeColor = isSelected
          ? "var(--accent)"
          : isStrong
            ? "color-mix(in srgb, var(--accent) 40%, var(--fg-dim))"
            : "color-mix(in srgb, var(--fg-dim) 45%, transparent)";
        return {
          id: `edge-${e.source}-${e.type}-${e.target}`,
          source: e.source,
          target: e.target,
          type: "default",
          data: { type: e.type, description: e.description },
          animated: isSelected,
          style: {
            stroke: edgeColor,
            strokeWidth: isSelected ? Math.max(2.5, baseWidth) : baseWidth,
            opacity: edgeOpacity,
          },
          markerEnd: { type: MarkerType.ArrowClosed, color: edgeColor, width: 16, height: 16 },
        };
      });

    // Apply star/radial layout (BFS from top hub outward).
    const positioned = starLayout(flowNodes, flowEdges);
    setNodes(positioned);
    setEdges(flowEdges);

    // Fit view after layout settles.
    if (!layoutDone.current) {
      requestAnimationFrame(() => {
        setTimeout(() => fitView({ padding: 0.15, duration: 200 }), 50);
      });
      layoutDone.current = true;
    }
  }, [graphData, searchQuery, searchMode, semanticHits, filterTypes, selectedEntities, selectedRelations, highlightedName, setNodes, setEdges, fitView]);

  // Node click.
  const handleNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      if (selectionMode) {
        const newEntities = selectedEntities.includes(node.id)
          ? selectedEntities.filter((n) => n !== node.id)
          : [...selectedEntities, node.id];
        onSelectionChange(newEntities, selectedRelations);
      } else {
        onNodeClick(String(node.data?.label ?? node.id), String(node.data?.collection ?? ""));
      }
    },
    [selectionMode, selectedEntities, selectedRelations, onNodeClick, onSelectionChange],
  );

  // Edge click.
  const handleEdgeClick = useCallback(
    (_: React.MouseEvent, edge: Edge) => {
      if (!selectionMode || !edge.data) return;
      const edgeKey = `${edge.source}→${edge.data.type}→${edge.target}`;
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
              ⏳ 当前文档较长，正在提取中…
            </span>
          ) : (
            <span className="rag-graph__empty-hint" style={{ marginTop: "12px", color: "color-mix(in srgb, orange 70%, var(--fg-dim))" }}>
              ⚠️ 当前块响应超时中，完成后将自动继续下一块
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
            ? "当前集合中暂无文件文档，请先通过左侧【导入】添加对应格式文件后，点击【深度提取】即可自研构建知识图谱。" 
            : t("cowork.ragGraphEmptyHint")}
        </span>
      </div>
    );
  }

  return (
    <div className="rag-graph-canvas">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={handleNodeClick}
        onEdgeClick={handleEdgeClick}
        nodeTypes={nodeTypes}
        fitView
        minZoom={0.05}
        maxZoom={3}
        proOptions={{ hideAttribution: true }}
      >
        <Background color="var(--border-soft)" gap={24} size={1} />
        <Controls showInteractive={false} style={{ background: "var(--bg-elev)", borderColor: "var(--border)" }} />
        <MiniMap
          nodeColor={(n: Node) => nodeColor(String(n.data?.type ?? ""))}
          style={{ background: "var(--bg-elev)", border: "1px solid var(--border)" }}
          maskColor="rgba(0,0,0,0.3)"
        />
      </ReactFlow>
      {searchMode === "semantic" && semanticStatus === "he-offline" && (
        <div className="rag-graph__sem-hint" role="status">{t("cowork.ragSemHEOffline")}</div>
      )}
      {searchMode === "semantic" && semanticStatus === "no-results" && searchQuery.trim() && (
        <div className="rag-graph__sem-hint" role="status">{t("cowork.ragSemNoResults")}</div>
      )}
    </div>
  );
}
