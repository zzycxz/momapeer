// TaskForm is the create/edit surface for a scheduled task, structured as
// Trigger → Action → Delivery (mirrors the WorkBuddy "添加自动化任务" layout).
//
// Trigger: a text input that accepts both natural-language phrases ("后天下午3点")
//   and scheduler expressions ("daily 09:00", "every 1h"). A live preview calls
//   app.PreviewSchedule on each change and shows the resolved absolute time, so
//   the user always sees the concrete instant the task will fire. Four quick
//   buttons (Daily / Weekdays / Hourly / One-shot) insert common templates.
// Action: a textarea pre-filled from the chosen template (or empty); the user
//   customizes the prompt.
// Delivery: a single-select of {store / im / email / notify / file}. Email/IM/
//   file reveal a destination field with a hint.
//
// On Save, the form calls onSubmit with the assembled TaskInput; the parent
// decides create-vs-update by whether input.id is set.

import { useEffect, useState } from "react";
import { X } from "lucide-react";

import { app } from "../../lib/bridge";
import type { SchedulePreview, TaskInput, TaskView, TemplateView } from "../../lib/types";
import { useT } from "../../lib/i18n";

const DELIVERY_OPTIONS = [
  { value: "", key: "cowork.automationDeliveryStore" },
  { value: "notify", key: "cowork.automationDeliveryNotify" },
  { value: "im", key: "cowork.automationDeliveryIM" },
  { value: "email", key: "cowork.automationDeliveryEmail" },
  { value: "file", key: "cowork.automationDeliveryFile" },
] as const;

export function TaskForm({
  initial,
  initialTemplate,
  templates,
  onSubmit,
  onCancel,
  onDelete,
}: {
  initial: TaskView | null;
  initialTemplate: TemplateView | null;
  templates: TemplateView[];
  onSubmit: (input: TaskInput) => Promise<void>;
  onCancel: () => void;
  onDelete?: () => void;
}) {
  const t = useT();
  const isEdit = !!initial;
  // Seed from the editing task, else from a header-picked template, else empty.
  const seedTpl = !initial && initialTemplate ? initialTemplate : null;
  const [name, setName] = useState(initial?.name ?? seedTpl?.name ?? "");
  const [expression, setExpression] = useState(initial?.expression ?? seedTpl?.expression ?? "");
  const [prompt, setPrompt] = useState(initial?.prompt ?? seedTpl?.prompt ?? "");
  const [outputMode, setOutputMode] = useState(initial?.outputMode ?? seedTpl?.outputMode ?? "notify");
  const [outputDest, setOutputDest] = useState(initial?.outputDest ?? "");
  const [preview, setPreview] = useState<SchedulePreview | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>("");

  // Live preview of the expression as the user types. Debounced via a microtask
  // gate so we don't spam the bridge on every keystroke.
  useEffect(() => {
    if (!expression.trim()) {
      setPreview(null);
      return;
    }
    const handle = setTimeout(() => {
      void app.PreviewSchedule(expression).then(setPreview).catch(() => setPreview(null));
    }, 200);
    return () => clearTimeout(handle);
  }, [expression]);

  const applyTemplate = (tpl: TemplateView) => {
    setName(tpl.name);
    setExpression(tpl.expression);
    setPrompt(tpl.prompt);
    setOutputMode(tpl.outputMode);
  };

  const quickFill = (kind: "daily" | "weekday" | "hourly" | "oneshot") => {
    switch (kind) {
      case "daily":
        setExpression("daily 09:00");
        break;
      case "weekday":
        setExpression("daily 09:00 Mon-Fri");
        break;
      case "hourly":
        setExpression("every 1h");
        break;
      case "oneshot":
        setExpression("in 1h");
        break;
    }
  };

  const save = async () => {
    setError("");
    if (!name.trim()) {
      setError(t("cowork.automationFormName"));
      return;
    }
    if (!expression.trim()) {
      setError(t("cowork.automationFormExpression"));
      return;
    }
    if (!prompt.trim()) {
      setError(t("cowork.automationFormPrompt"));
      return;
    }
    setSaving(true);
    try {
      await onSubmit({
        id: initial?.id ?? "",
        name: name.trim(),
        expression: expression.trim(),
        prompt: prompt.trim(),
        outputMode,
        outputDest: outputDest.trim(),
      });
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="cowork-taskform-overlay" onClick={onCancel}>
      <div className="cowork-taskform" onClick={(e) => e.stopPropagation()}>
        <header className="cowork-taskform__head">
          <h3>{isEdit ? t("cowork.automationFormTitleEdit") : t("cowork.automationFormTitleNew")}</h3>
          <button className="cowork-task-card__btn" onClick={onCancel} title="close">
            <X size={16} />
          </button>
        </header>

        <div className="cowork-taskform__body">
          {/* Name */}
          <label className="cowork-taskform__label">
            <span className="cowork-taskform__labeltext">{t("cowork.automationFormName")}</span>
            <input
              className="cowork-taskform__input"
              value={name}
              placeholder={t("cowork.automationFormNameHint")}
              onChange={(e) => setName(e.target.value)}
            />
          </label>

          {/* Templates */}
          {!isEdit && templates.length > 0 && (
            <div className="cowork-taskform__section">
              <span className="cowork-taskform__labeltext">{t("cowork.automationTemplatePick")}</span>
              <div className="cowork-taskform__templates">
                {templates.map((tpl) => (
                  <button
                    key={tpl.id}
                    type="button"
                    className="cowork-taskform__template"
                    title={tpl.desc}
                    onClick={() => applyTemplate(tpl)}
                  >
                    {tpl.name}
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Trigger */}
          <div className="cowork-taskform__section">
            <span className="cowork-taskform__labeltext">{t("cowork.automationFormTrigger")}</span>
            <input
              className="cowork-taskform__input"
              value={expression}
              placeholder={t("cowork.automationFormExpressionHint")}
              onChange={(e) => setExpression(e.target.value)}
            />
            <div className="cowork-taskform__quicks">
              <button type="button" className="cowork-taskform__quick" onClick={() => quickFill("daily")}>
                {t("cowork.automationFormQuickDaily")}
              </button>
              <button type="button" className="cowork-taskform__quick" onClick={() => quickFill("weekday")}>
                {t("cowork.automationFormQuickWeekday")}
              </button>
              <button type="button" className="cowork-taskform__quick" onClick={() => quickFill("hourly")}>
                {t("cowork.automationFormQuickHourly")}
              </button>
              <button type="button" className="cowork-taskform__quick" onClick={() => quickFill("oneshot")}>
                {t("cowork.automationFormQuickOneShot")}
              </button>
            </div>
            {preview && (
              <div className={`cowork-taskform__preview cowork-taskform__preview--${preview.kind}`}>
                <span className="cowork-taskform__preview-label">{t("cowork.automationFormPreview")}:</span>
                {preview.absoluteTime && (
                  <span className="cowork-taskform__preview-time">→ {preview.absoluteTime}</span>
                )}
                {preview.note && <span className="cowork-taskform__preview-note">{preview.note}</span>}
              </div>
            )}
          </div>

          {/* Action */}
          <label className="cowork-taskform__label cowork-taskform__label--top">
            <span className="cowork-taskform__labeltext">{t("cowork.automationFormAction")}</span>
            <textarea
              className="cowork-taskform__textarea"
              value={prompt}
              rows={5}
              onChange={(e) => setPrompt(e.target.value)}
            />
          </label>

          {/* Delivery */}
          <div className="cowork-taskform__section">
            <span className="cowork-taskform__labeltext">{t("cowork.automationFormDelivery")}</span>
            <div className="cowork-taskform__delivery">
              {DELIVERY_OPTIONS.map((opt) => (
                <button
                  key={opt.value || "store"}
                  type="button"
                  className={`cowork-taskform__delivery-opt ${outputMode === opt.value ? "cowork-taskform__delivery-opt--active" : ""}`}
                  onClick={() => setOutputMode(opt.value)}
                >
                  {t(opt.key)}
                </button>
              ))}
            </div>
            {(outputMode === "email" || outputMode === "im" || outputMode === "file") && (
              <label className="cowork-taskform__label">
                <span className="cowork-taskform__labeltext">{t("cowork.automationFormDest")}</span>
                <input
                  className="cowork-taskform__input"
                  value={outputDest}
                  placeholder={t("cowork.automationFormDestHint")}
                  onChange={(e) => setOutputDest(e.target.value)}
                />
              </label>
            )}
          </div>

          {error && <div className="cowork-taskform__error">{error}</div>}
        </div>

        <footer className="cowork-taskform__foot">
          {isEdit && onDelete && (
            <button
              type="button"
              className="btn btn--danger btn--small"
              onClick={onDelete}
              disabled={saving}
            >
              {t("cowork.automationFormDelete")}
            </button>
          )}
          <div className="cowork-taskform__foot-right">
            <button type="button" className="btn btn--small" onClick={onCancel} disabled={saving}>
              {t("cowork.automationFormCancel")}
            </button>
            <button
              type="button"
              className="btn btn--primary btn--small"
              onClick={() => void save()}
              disabled={saving}
            >
              {saving ? t("cowork.automationRunning") : t("cowork.automationFormSave")}
            </button>
          </div>
        </footer>
      </div>
    </div>
  );
}
