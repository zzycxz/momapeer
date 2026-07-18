// GraphLegend shows the color legend for entity types and community rings
// in the knowledge graph. Entity types determine the dot color; community
// determines the outer ring color (Louvain detection).

import { ENTITY_TYPES, communityColor } from "./entityTypes";

export function GraphLegend({ hasCommunities = false }: { hasCommunities?: boolean }) {
  return (
    <div className="rag-legend">
      <div className="rag-legend__group">
        <span className="rag-legend__title">类型</span>
        {ENTITY_TYPES.map((item) => (
          <div key={item.key} className="rag-legend__item">
            <span className="rag-legend__dot" style={{ background: item.color }} />
            <span className="rag-legend__label">{item.label}</span>
          </div>
        ))}
      </div>
      {hasCommunities && (
        <div className="rag-legend__group">
          <span className="rag-legend__title">社区</span>
          <div className="rag-legend__item">
            <span
              className="rag-legend__ring"
              style={{ borderColor: communityColor(0) }}
            />
            <span className="rag-legend__label">节点外环 = 社区</span>
          </div>
        </div>
      )}
    </div>
  );
}
