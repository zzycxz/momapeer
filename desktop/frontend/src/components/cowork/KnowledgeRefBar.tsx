// KnowledgeRefBar appears at the bottom of the graph canvas when selection mode
// is active and items are selected. Shows selected count and "use for" button.

import { X, ChevronRight, GripHorizontal } from "lucide-react";
import { useDraggable } from "../../hooks/useDraggable";
import { useT } from "../../lib/i18n";

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
  const { position, isDragging, handleMouseDown } = useDraggable();
  const t = useT();
  const total = selectedEntities.length + selectedRelations.length;
  if (total === 0) return null;

  return (
    <div 
      className="rag-refbar"
      style={{ 
        transform: `translate(calc(-50% + ${position.x}px), ${position.y}px)`,
        cursor: isDragging ? 'grabbing' : 'grab'
      }}
      onMouseDown={handleMouseDown}
      onTouchStart={handleMouseDown}
    >
      <div style={{ position: "absolute", top: "-10px", left: "50%", transform: "translateX(-50%)", color: "var(--fg-faint)", opacity: 0.5 }}>
        <GripHorizontal size={14} />
      </div>
      <div className="rag-refbar__info">
        <span className="rag-refbar__count">
          {t("cowork.refbarSelectedCount", { entities: selectedEntities.length, relations: selectedRelations.length })}
        </span>
        <button className="rag-refbar__clear" onClick={onClear}>
          <X size={12} />
          <span>{t("cowork.refbarClear")}</span>
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
        <span>{t("cowork.refbarUseFor")}</span>
        <ChevronRight size={14} />
      </button>
    </div>
  );
}
