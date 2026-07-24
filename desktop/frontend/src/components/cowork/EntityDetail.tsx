// EntityDetail shows the full detail of an entity in the CoworkDock panel.
// Displays type, description, relations (with strength), sources, community,
// and provides edit/merge actions. Relation items are clickable to jump to
// the peer entity's detail.

import { useEffect, useState } from "react";
import { ArrowLeft, Edit3, MapPin } from "lucide-react";

import { app } from "../../lib/bridge";
import type { EntityDetailView, EntityRelationView } from "../../lib/types";
import { EntityEditModal } from "./EntityEditModal";
import { labelFor, communityColor } from "./entityTypes";

export interface EntityDetailProps {
  collection: string;
  entityName: string;
  onBack: () => void;
  onHighlightInGraph: (name: string) => void;
  onNavigatePeer?: (name: string) => void;
}

export function EntityDetail({ collection, entityName, onBack, onHighlightInGraph, onNavigatePeer }: EntityDetailProps) {
  const [entity, setEntity] = useState<EntityDetailView | null>(null);
  const [loading, setLoading] = useState(false);
  const [showEdit, setShowEdit] = useState(false);

  useEffect(() => {
    if (!entityName) return;
    setLoading(true);
    app.GetEntityDetail(collection, entityName).then((d) => {
      setEntity(d);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, [collection, entityName]);

  const handleSave = () => {
    app.GetEntityDetail(collection, entityName).then(setEntity).catch(() => {});
  };

  if (loading) {
    return <div className="rag-detail__loading">加载中...</div>;
  }
  if (!entity) {
    return <div className="rag-detail__empty">实体未找到</div>;
  }

  // Group relations by direction.
  const relations = entity.relations ?? [];
  const outRels = relations.filter((r) => r.direction === "out");
  const inRels = relations.filter((r) => r.direction === "in");

  const handleRelClick = (peer: string) => {
    onHighlightInGraph(peer);
    if (onNavigatePeer) onNavigatePeer(peer);
  };

  return (
    <div className="rag-detail">
      {/* Header */}
      <div className="rag-detail__header">
        <button className="rag-detail__back" onClick={onBack}>
          <ArrowLeft size={14} />
        </button>
        <div className="rag-detail__title">{entity.nameRaw}</div>
        {entity.type && <span className="rag-detail__type-badge">{labelFor(entity.type)}</span>}
      </div>

      {/* Meta badges */}
      <div className="rag-detail__meta">
        <span className="rag-detail__meta-item">关联 {entity.relationCnt ?? 0}</span>
        {(entity.community ?? 0) >= 0 && (
          <span
            className="rag-detail__community-badge"
            style={{ borderColor: communityColor(entity.community ?? 0) }}
          >
            社区 {entity.community ?? 0}
          </span>
        )}
      </div>

      {/* Description */}
      {entity.description && (
        <div className="rag-detail__desc">{entity.description}</div>
      )}

      {/* Relations */}
      {outRels.length > 0 && (
        <div className="rag-detail__section">
          <div className="rag-detail__section-title">→ 关联 ({outRels.length})</div>
          {outRels.map((r, i) => (
            <RelationItem key={`out-${i}`} rel={r} onClick={() => handleRelClick(r.peer)} />
          ))}
        </div>
      )}
      {inRels.length > 0 && (
        <div className="rag-detail__section">
          <div className="rag-detail__section-title">← 被引用 ({inRels.length})</div>
          {inRels.map((r, i) => (
            <RelationItem key={`in-${i}`} rel={r} onClick={() => handleRelClick(r.peer)} />
          ))}
        </div>
      )}

      {/* Sources */}
      {(entity.sources ?? []).length > 0 && (
        <div className="rag-detail__section">
          <div className="rag-detail__section-title">来源 ({(entity.sources ?? []).length})</div>
          {(entity.sources ?? []).map((s, i) => (
            <div key={i} className="rag-detail__source">
              <span className="rag-detail__source-path">{s.path.split(/[/\\]/).pop()}</span>
              <span className="rag-detail__source-chunk">#{s.chunk}</span>
            </div>
          ))}
        </div>
      )}

      {/* Actions */}
      <div className="rag-detail__actions">
        <button className="rag-detail__action-btn" onClick={() => setShowEdit(true)}>
          <Edit3 size={14} />
          <span>编辑</span>
        </button>
        <button className="rag-detail__action-btn" onClick={() => onHighlightInGraph(entity.name)}>
          <MapPin size={14} />
          <span>在图谱高亮</span>
        </button>
      </div>

      {/* Edit modal */}
      {showEdit && (
        <EntityEditModal
          collection={collection}
          entity={entity}
          onClose={() => setShowEdit(false)}
          onSave={handleSave}
        />
      )}
    </div>
  );
}

function RelationItem({ rel, onClick }: { rel: EntityRelationView; onClick: () => void }) {
  const strength = rel.strength || 5;
  const isStrong = strength >= 7;
  return (
    <div className="rag-detail__rel" onClick={onClick}>
      <div className="rag-detail__rel-row">
        <span className="rag-detail__rel-type">{rel.type}</span>
        <span className="rag-detail__rel-peer">{rel.peer}</span>
        <span className={`rag-detail__rel-strength ${isStrong ? "rag-detail__rel-strength--high" : ""}`}>
          {Math.round(strength)}
        </span>
      </div>
      {rel.description && <span className="rag-detail__rel-desc">{rel.description}</span>}
    </div>
  );
}
