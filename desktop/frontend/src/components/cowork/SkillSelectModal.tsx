// SkillSelectModal is a centered modal for selecting a skill to run with
// the selected knowledge references. Shows a preview of what will be passed.

import { useState } from "react";
import { X } from "lucide-react";

export interface SkillSelectModalProps {
  selectedEntities: string[];
  selectedRelations: string[];
  onConfirm: (skillName: string) => void | Promise<void>;
  onClose: () => void;
}

const SKILLS = [
  { name: "ppt-auto", label: "PPT 生成", description: "基于选中知识生成演示文稿" },
  { name: "document-auto", label: "Word 文档撰写", description: "基于选中知识撰写文档" },
];

export function SkillSelectModal({
  selectedEntities,
  selectedRelations,
  onConfirm,
  onClose,
}: SkillSelectModalProps) {
  const [selectedSkill, setSelectedSkill] = useState(SKILLS[0].name);
  const [running, setRunning] = useState(false);

  const handleConfirm = async () => {
    setRunning(true);
    try {
      await onConfirm(selectedSkill);
    } catch {
      // parent handles error display
    } finally {
      setRunning(false);
    }
  };

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal rag-skill-modal" onClick={(e) => e.stopPropagation()}>
        <div className="rag-skill-modal__header">
          <span className="rag-skill-modal__title">使用知识引用</span>
          <button className="rag-skill-modal__close" onClick={onClose}>
            <X size={16} />
          </button>
        </div>

        <div className="rag-skill-modal__body">
          <p className="rag-skill-modal__summary">
            将选中的 {selectedEntities.length} 个实体 · {selectedRelations.length} 条关系传递给：
          </p>

          {/* Skill selection */}
          <div className="rag-skill-modal__list">
            {SKILLS.map((s) => (
              <label key={s.name} className="rag-skill-modal__item">
                <input
                  type="radio"
                  name="skill"
                  value={s.name}
                  checked={selectedSkill === s.name}
                  onChange={() => setSelectedSkill(s.name)}
                />
                <div className="rag-skill-modal__item-info">
                  <span className="rag-skill-modal__item-label">{s.label}</span>
                  <span className="rag-skill-modal__item-desc">{s.description}</span>
                </div>
              </label>
            ))}
          </div>

          {/* Preview */}
          <div className="rag-skill-modal__preview">
            <div className="rag-skill-modal__preview-title">预览</div>
            <div className="rag-skill-modal__preview-content">
              <div className="rag-skill-modal__preview-section">
                <strong>实体:</strong>
                {selectedEntities.map((e) => (
                  <div key={e} className="rag-skill-modal__preview-item">- {e}</div>
                ))}
              </div>
              {selectedRelations.length > 0 && (
                <div className="rag-skill-modal__preview-section">
                  <strong>关系:</strong>
                  {selectedRelations.map((r) => (
                    <div key={r} className="rag-skill-modal__preview-item">- {r}</div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>

        <div className="modal__actions">
          <button className="btn" onClick={onClose}>取消</button>
          <button className="btn btn--primary" onClick={handleConfirm} disabled={running}>
            {running ? "运行中..." : "确认运行"}
          </button>
        </div>
      </div>
    </div>
  );
}
