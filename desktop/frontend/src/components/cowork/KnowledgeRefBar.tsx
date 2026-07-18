// KnowledgeRefBar appears at the bottom of the graph canvas when selection mode
// is active and items are selected. Shows selected count and "use for" button.

import { X, ChevronRight } from "lucide-react";

export interface KnowledgeRefBarProps {
  selectedEntities: string[];
  selectedRelations: string[];
  onClear: () => void;
  onUseFor: () => void;
}

export function KnowledgeRefBar({
  selectedEntities,
  selectedRelations,
  onClear,
  onUseFor,
}: KnowledgeRefBarProps) {
  const total = selectedEntities.length + selectedRelations.length;
  if (total === 0) return null;

  return (
    <div className="rag-refbar">
      <div className="rag-refbar__info">
        <span className="rag-refbar__count">
          已选 {selectedEntities.length} 个实体 · {selectedRelations.length} 条关系
        </span>
        <button className="rag-refbar__clear" onClick={onClear}>
          <X size={12} />
          <span>清除</span>
        </button>
      </div>
      <div className="rag-refbar__tags">
        {selectedEntities.slice(0, 5).map((e) => (
          <span key={e} className="rag-refbar__tag rag-refbar__tag--entity">{e}</span>
        ))}
        {selectedEntities.length > 5 && (
          <span className="rag-refbar__tag rag-refbar__tag--more">+{selectedEntities.length - 5}</span>
        )}
        {selectedRelations.slice(0, 3).map((r) => (
          <span key={r} className="rag-refbar__tag rag-refbar__tag--relation">{r}</span>
        ))}
        {selectedRelations.length > 3 && (
          <span className="rag-refbar__tag rag-refbar__tag--more">+{selectedRelations.length - 3}</span>
        )}
      </div>
      <button className="rag-refbar__use-btn" onClick={onUseFor}>
        <span>用于</span>
        <ChevronRight size={14} />
      </button>
    </div>
  );
}
