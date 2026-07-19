// TeamManager is the create/edit modal for an expert team: name, collaboration
// defaults, and a dynamic list of experts (name + model + perspective). The
// model field is a dropdown of available models from the user's configuration.

import { useEffect, useState } from "react";
import { X, Plus, Trash2 } from "lucide-react";

import { app } from "../../lib/bridge";
import type { ExpertView, ModelInfo, TeamView } from "../../lib/types";
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
  const [allowSearch, setAllowSearch] = useState(initial?.allowSearch ?? false);
  const [experts, setExperts] = useState<ExpertView[]>(
    initial?.experts ?? [
      { name: "", model: "", perspective: "" },
      { name: "", model: "", perspective: "" },
    ]
  );
  // Stable React keys per expert row (parallel to `experts`). Using the array
  // index as key makes React reuse DOM nodes by position, so deleting a middle
  // expert shifts every row below it and the text-cursor/focus jumps to the
  // wrong input. A stable per-row id avoids that while keeping index-based
  // update/remove logic intact.
  const [expertKeys, setExpertKeys] = useState<string[]>(() =>
    Array.from({ length: initial?.experts?.length ?? 2 }, () => crypto.randomUUID())
  );
  const [saving, setSaving] = useState(false);
  const [models, setModels] = useState<ModelInfo[]>([]);

  useEffect(() => {
    app.Models().then(setModels).catch(() => {});
  }, []);

  const updateExpert = (idx: number, field: keyof ExpertView, val: string) => {
    setExperts((prev) => prev.map((e, i) => (i === idx ? { ...e, [field]: val } : e)));
  };
  const addExpert = () => {
    setExperts((prev) => [...prev, { name: "", model: "", perspective: "" }]);
    setExpertKeys((prev) => [...prev, crypto.randomUUID()]);
  };
  const removeExpert = (idx: number) => {
    setExperts((prev) => prev.filter((_, i) => i !== idx));
    setExpertKeys((prev) => prev.filter((_, i) => i !== idx));
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave({
        id: initial?.id ?? "",
        name: name.trim(),
        experts: experts.filter((e) => e.name.trim()),
        defaultMode: mode,
        defaultRounds: rounds,
        allowSearch,
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
            <label className="cowork-expert__search-toggle">
              <input
                type="checkbox"
                checked={allowSearch}
                onChange={(e) => setAllowSearch(e.target.checked)}
              />
              <span className="cowork-expert__search-toggle-label">{t("cowork.expertAllowSearch")}</span>
            </label>
            <div className="cowork-expert__search-toggle-hint">{t("cowork.expertSearchHint")}</div>
          </div>

          <div className="cowork-taskform__section">
            <span className="cowork-taskform__labeltext">{t("cowork.expertMembers")}</span>
            {experts.map((ex, i) => (
              <div key={expertKeys[i] ?? i} className="cowork-expert__member-row">
                <input className="cowork-taskform__input cowork-expert__member-name" placeholder={t("cowork.expertMemberName")} value={ex.name} onChange={(e) => updateExpert(i, "name", e.target.value)} />
                <select
                  className="cowork-taskform__input cowork-expert__member-model"
                  value={ex.model}
                  onChange={(e) => updateExpert(i, "model", e.target.value)}
                >
                  <option value="">{t("cowork.expertDefaultModel")}</option>
                  {models.map((m) => (
                    <option key={m.ref} value={m.ref}>{m.provider}/{m.model}</option>
                  ))}
                </select>
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
