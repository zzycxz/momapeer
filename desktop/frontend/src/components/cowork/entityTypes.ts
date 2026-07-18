// Shared entity-type metadata for the RAG knowledge graph. Previously the type
// list, labels, and colors were duplicated across GraphCanvas, GraphToolbar,
// GraphLegend, and TemplateSelect — and the colors collided (product and
// organization both resolved to the accent variable, technology/project/location
// all to the ok variable). This module is the single source of truth.
//
// Colors use a color-blind-safe palette (Okabe-Ito derived) with one distinct
// hue per type so nodes are distinguishable for users with deuteranopia/
// protanopia. Keep this list in sync with GraphCanvas.TYPE_COLORS.

export interface EntityTypeDef {
  key: string;
  label: string;
  color: string;
}

export const ENTITY_TYPES: readonly EntityTypeDef[] = [
  { key: "product", label: "产品", color: "#0072B2" },
  { key: "technology", label: "技术", color: "#009E73" },
  { key: "feature", label: "功能", color: "#56B4E9" },
  { key: "person", label: "人物", color: "#E69F00" },
  { key: "organization", label: "组织", color: "#D55E00" },
  { key: "project", label: "项目", color: "#F0E442" },
  { key: "concept", label: "概念", color: "#CC79A7" },
  { key: "event", label: "事件", color: "#CC4C02" },
  { key: "location", label: "地点", color: "#7B68EE" },
  { key: "topic", label: "主题", color: "#A0522D" },
] as const;

// ENTITY_TYPE_COLORS / ENTITY_TYPE_LABELS are keyed lookups derived from the
// single list above, for callers that need one or the other (e.g. GraphCanvas
// colors a node by type; TemplateSelect renders a label).
export const ENTITY_TYPE_COLORS: Record<string, string> = Object.fromEntries(
  ENTITY_TYPES.map((t) => [t.key, t.color]),
);
ENTITY_TYPE_COLORS.other = "#999999";

export const ENTITY_TYPE_LABELS: Record<string, string> = Object.fromEntries(
  ENTITY_TYPES.map((t) => [t.key, t.label]),
);
ENTITY_TYPE_LABELS.other = "其他";

/** colorFor returns the palette color for a type key (lowercased), defaulting to the neutral gray. */
export function colorFor(type: string): string {
  return ENTITY_TYPE_COLORS[type.toLowerCase()] ?? ENTITY_TYPE_COLORS.other;
}

/** labelFor returns the localized label for a type key, defaulting to the raw type. */
export function labelFor(type: string): string {
  return ENTITY_TYPE_LABELS[type.toLowerCase()] ?? type;
}

// --- Community color palette (Louvain community visualization) -------------
// 12 vivid colors chosen for visibility on both dark and light themes.
// Used by the graph to draw a colored ring around each node indicating its community.
const COMMUNITY_PALETTE = [
  "#ff6b6b", // red
  "#4ecdc4", // teal
  "#45b7d1", // sky blue
  "#ffa07a", // salmon
  "#dda0dd", // plum
  "#98d8c8", // mint
  "#f7dc6f", // gold
  "#bb8fce", // lavender
  "#85c1e9", // light blue
  "#f0b27a", // peach
  "#82e0aa", // light green
  "#f1948a", // coral
];

/** communityColor returns a color for a community ID. Negative IDs (unassigned) get transparent. */
export function communityColor(id: number): string {
  if (id < 0) return "transparent";
  return COMMUNITY_PALETTE[id % COMMUNITY_PALETTE.length];
}
