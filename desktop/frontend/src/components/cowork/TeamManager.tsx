// TeamManager is the create/edit modal for an expert team: name, collaboration
// defaults, and a dynamic list of experts (name + model + perspective). The
// model field is a free-text "provider/model" ref (matching the agent's model
// picker) — we keep it simple rather than embedding the full ModelSwitcher.

import { useState } from "react";
import { X, Plus, Trash2 } from "lucide-react";

import type { ExpertView, TeamView } from "../../lib/types";
import { useT } from "../../lib/i18n";

export function TeamManager({
  initial,
  onSave,
  onCancel,
}: {
  initial: TeamView | null;
  onSave: (team: TeamView) => Promise<void>;
  onCancel: () => void;
}) {
  const t = useT();
  const [name, setName] = useState(initial?.name ?? "");
  const [mode, setMode] = useState(initial?.defaultMode ?? "debate");
  const [rounds, setRounds] = useState(initial?.defaultRounds ?? 2);
  const [experts, setExperts] = useState<ExpertView[]>(
    initial?.experts ?? [
      { name: "", model: "", perspective: "" },
      { name: "", model: "", perspective: "" },
    ]
  );
  const [saving, setSaving] = useState(false);

  const updateExpert = (idx: number, field: keyof ExpertView, val: string) => {
    setExperts((prev) => prev.map((e, i) => (i === idx ? { ...e, [field]: val } : e)));
  };
  const addExpert = () => setExperts((prev) => [...prev, { name: "", model: "", perspective: "" }]);
  const removeExpert = (idx: number) => setExperts((prev) => prev.filter((_, i) => i !== idx));

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave({
        id: initial?.id ?? "",
        name: name.trim(),
        experts: experts.filter((e) => e.name.trim()),
        defaultMode: mode,
        defaultRounds: rounds,
      });
    } finally {
      setSaving(false);
    }
  };

  const valid = name.trim() && experts.some((e) => e.name.trim());

  return (
    <div className="cowork-taskform-overlay" onClick={onCancel}>
      <div className="cowork-taskform" onClick={(e) => e.stopPropagation()}>
        <header className="cowork-taskform__head">
          <h3>{initial ? t("cowork.expertEdit") : t("cowork.expertNew")}</h3>
          <button className="cowork-task-card__btn" onClick={onCancel}><X size={16} /></button>
        </header>

        <div className="cowork-taskform__body">
          <label className="cowork-taskform__label">
            <span className="cowork-taskform__labeltext">{t("cowork.expertName")}</span>
            <input className="cowork-taskform__input" value={name} onChange={(e) => setName(e.target.value)} placeholder={t("cowork.expertName")} />
          </label>

          <div className="cowork-taskform__section">
            <span className="cowork-taskform__labeltext">{t("cowork.expertMode")}</span>
            <div className="cowork-taskform__delivery">
              <button type="button" className={`cowork-taskform__delivery-opt ${mode === "debate" ? "cowork-taskform__delivery-opt--active" : ""}`} onClick={() => setMode("debate")}>
                {t("cowork.expertModeDebate")}
              </button>
              <button type="button" className={`cowork-taskform__delivery-opt ${mode === "parallel" ? "cowork-taskform__delivery-opt--active" : ""}`} onClick={() => setMode("parallel")}>
                {t("cowork.expertModeParallel")}
              </button>
              <button type="button" className={`cowork-taskform__delivery-opt ${mode === "pipeline" ? "cowork-taskform__delivery-opt--active" : ""}`} onClick={() => setMode("pipeline")}>
                {t("cowork.expertModePipeline")}
              </button>
            </div>
            {mode === "debate" && (
              <label className="cowork-taskform__label cowork-expert__rounds-input">
                <span className="cowork-taskform__labeltext">{t("cowork.expertRounds")}</span>
                <input type="number" min={1} max={5} value={rounds} onChange={(e) => setRounds(Math.max(1, Math.min(5, Number(e.target.value) || 2)))} />
              </label>
            )}
          </div>

          <div className="cowork-taskform__section">
            <span className="cowork-taskform__labeltext">{t("cowork.expertMembers")}</span>
            {experts.map((ex, i) => (
              <div key={i} className="cowork-expert__member-row">
                <input className="cowork-taskform__input cowork-expert__member-name" placeholder={t("cowork.expertMemberName")} value={ex.name} onChange={(e) => updateExpert(i, "name", e.target.value)} />
                <input className="cowork-taskform__input cowork-expert__member-model" placeholder={t("cowork.expertModel") + " (provider/model)"} value={ex.model} onChange={(e) => updateExpert(i, "model", e.target.value)} />
                <input className="cowork-taskform__input cowork-expert__member-perspective" placeholder={t("cowork.expertPerspective")} value={ex.perspective} onChange={(e) => updateExpert(i, "perspective", e.target.value)} />
                {experts.length > 1 && (
                  <button className="cowork-task-card__btn cowork-task-card__btn--danger" onClick={() => removeExpert(i)}><Trash2 size={14} /></button>
                )}
              </div>
            ))}
            <button type="button" className="cowork-taskform__quick" onClick={addExpert}>
              <Plus size={13} /> {t("cowork.expertAddMember")}
            </button>
          </div>
        </div>

        <footer className="cowork-taskform__foot">
          <div className="cowork-taskform__foot-right">
            <button className="btn btn--small" onClick={onCancel} disabled={saving}>{t("cowork.expertCancel")}</button>
            <button className="btn btn--primary btn--small" onClick={() => void handleSave()} disabled={saving || !valid}>{t("cowork.expertSave")}</button>
          </div>
        </footer>
      </div>
    </div>
  );
}
