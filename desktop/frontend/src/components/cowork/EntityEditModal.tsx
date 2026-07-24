// EntityEditModal is a centered modal for editing an entity's fields and
// optionally merging duplicate entities. Uses the existing modal CSS patterns.

import { useEffect, useState } from "react";
import { X } from "lucide-react";

import { app } from "../../lib/bridge";
import { useToast } from "../../lib/toast";
import type { EntityDetailView } from "../../lib/types";

const TYPE_OPTIONS = [
  "", "product", "technology", "feature", "person", "organization", "project", "concept", "event", "location", "topic", "other",
];

export interface EntityEditModalProps {
  collection: string;
  entity: EntityDetailView;
  onClose: () => void;
  onSave: () => void;
}

export function EntityEditModal({ collection, entity, onClose, onSave }: EntityEditModalProps) {
  const { showToast } = useToast();
  const [nameRaw, setNameRaw] = useState(entity.nameRaw);
  const [typ, setTyp] = useState(entity.type);
  const [desc, setDesc] = useState(entity.description);
  const [saving, setSaving] = useState(false);

  // Merge state.
  const [showMerge, setShowMerge] = useState(false);
  const [mergeCandidates, setMergeCandidates] = useState<Array<{ name: string; raw?: string; score?: number }>>([]);
  const [mergeSelected, setMergeSelected] = useState<string[]>([]);
  const [mergeLoading, setMergeLoading] = useState(false);

  // Find merge candidates: prefer semantic similarity (embeddings), fall back
  // to FTS5 keyword search when embeddings aren't available (HE offline).
  useEffect(() => {
    if (!showMerge) return;
    setMergeLoading(true);
    // Try semantic merge candidates first (more accurate for aliases).
    app.RagFindMergeCandidates(collection).then((candidates) => {
      // Filter to pairs involving THIS entity.
      const related = candidates
        .filter((c) => c.keepName === entity.name || c.mergeName === entity.name)
        .map((c) => {
          if (c.keepName === entity.name) {
            return { name: c.mergeName, raw: c.mergeRaw, score: c.score };
          }
          return { name: c.keepName, raw: c.keepRaw, score: c.score };
        });
      if (related.length > 0) {
        setMergeCandidates(related);
        setMergeLoading(false);
        return;
      }
      // Fallback: FTS5 keyword search for similar names.
      return app.RagSearch(collection, entity.nameRaw, 20).then((hits) => {
        const kwCandidates = hits.entities
          .filter((e) => e.name !== entity.name)
          .map((e) => ({ name: e.name }));
        setMergeCandidates(kwCandidates);
        setMergeLoading(false);
      });
    }).catch(() => {
      // Final fallback: FTS5.
      app.RagSearch(collection, entity.nameRaw, 20).then((hits) => {
        const kwCandidates = hits.entities
          .filter((e) => e.name !== entity.name)
          .map((e) => ({ name: e.name }));
        setMergeCandidates(kwCandidates);
      }).catch(() => {}).finally(() => setMergeLoading(false));
    });
  }, [showMerge, collection, entity.name, entity.nameRaw]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await app.UpdateEntity(collection, entity.name, { nameRaw, type: typ, description: desc });
      onSave();
      onClose();
    } catch (e) {
      showToast(`保存失败：${e}`, "error");
    } finally {
      setSaving(false);
    }
  };

  const handleMerge = async () => {
    if (mergeSelected.length === 0) return;
    setSaving(true);
    try {
      // Also save current edits.
      await app.UpdateEntity(collection, entity.name, { nameRaw, type: typ, description: desc });
      await app.MergeEntities(collection, entity.name, mergeSelected);
      onSave();
      onClose();
    } catch (e) {
      showToast(`合并失败：${e}`, "error");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal rag-edit-modal" onClick={(e) => e.stopPropagation()}>
        <div className="rag-edit-modal__header">
          <span className="rag-edit-modal__title">编辑实体</span>
          <button className="rag-edit-modal__close" onClick={onClose}>
            <X size={16} />
          </button>
        </div>

        <div className="rag-edit-modal__body">
          {/* Name */}
          <label className="rag-edit-modal__field">
            <span className="rag-edit-modal__label">名称</span>
            <input
              type="text"
              value={nameRaw}
              onChange={(e) => setNameRaw(e.target.value)}
              className="rag-edit-modal__input"
            />
          </label>

          {/* Type */}
          <label className="rag-edit-modal__field">
            <span className="rag-edit-modal__label">类型</span>
            <select
              value={typ}
              onChange={(e) => setTyp(e.target.value)}
              className="rag-edit-modal__select"
            >
              {TYPE_OPTIONS.map((t) => (
                <option key={t} value={t}>{t || "未分类"}</option>
              ))}
            </select>
          </label>

          {/* Description */}
          <label className="rag-edit-modal__field">
            <span className="rag-edit-modal__label">描述</span>
            <textarea
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              className="rag-edit-modal__textarea"
              rows={4}
            />
          </label>

          {/* Merge section */}
          <div className="rag-edit-modal__merge-section">
            <button
              className="rag-edit-modal__merge-toggle"
              onClick={() => setShowMerge(!showMerge)}
            >
              合并重复实体...
            </button>

            {showMerge && (
              <div className="rag-edit-modal__merge-body">
                <p className="rag-edit-modal__merge-hint">
                  勾选要合并到 "{entity.nameRaw}" 的实体，关系自动迁移
                </p>
                {mergeLoading ? (
                  <p className="rag-edit-modal__merge-empty">正在分析相似实体...</p>
                ) : mergeCandidates.length === 0 ? (
                  <p className="rag-edit-modal__merge-empty">未找到相似实体</p>
                ) : (
                  <div className="rag-edit-modal__merge-list">
                    {mergeCandidates.map((c) => (
                      <label key={c.name} className="rag-edit-modal__merge-item">
                        <input
                          type="checkbox"
                          checked={mergeSelected.includes(c.name)}
                          onChange={(e) => {
                            if (e.target.checked) {
                              setMergeSelected([...mergeSelected, c.name]);
                            } else {
                              setMergeSelected(mergeSelected.filter((s) => s !== c.name));
                            }
                          }}
                        />
                        <span>{c.raw || c.name}</span>
                        {c.score !== undefined && (
                          <span className={`rag-edit-modal__merge-score ${c.score >= 0.95 ? "rag-edit-modal__merge-score--high" : "rag-edit-modal__merge-score--mid"}`}>
                            {Math.min(100, Math.round(c.score * 100))}%
                          </span>
                        )}
                      </label>
                    ))}
                  </div>
                )}
                {mergeSelected.length > 0 && (
                  <p className="rag-edit-modal__merge-preview">
                    合并后将迁移 {mergeSelected.length} 个实体的关系
                  </p>
                )}
              </div>
            )}
          </div>
        </div>

        <div className="modal__actions">
          <button className="btn" onClick={onClose}>取消</button>
          {showMerge && mergeSelected.length > 0 ? (
            <button className="btn btn--primary" onClick={handleMerge} disabled={saving}>
              {saving ? "合并中..." : "合并并保存"}
            </button>
          ) : (
            <button className="btn btn--primary" onClick={handleSave} disabled={saving}>
              {saving ? "保存中..." : "保存"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
