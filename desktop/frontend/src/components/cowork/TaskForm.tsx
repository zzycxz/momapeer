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
import type { RecentChatView, SchedulePreview, TaskInput, TaskView, TemplateView } from "../../lib/types";
import { useT } from "../../lib/i18n";

const DELIVERY_OPTIONS = [
  { value: "", key: "cowork.automationDeliveryStore" },
  { value: "notify", key: "cowork.automationDeliveryNotify" },
  { value: "im", key: "cowork.automationDeliveryIM" },
  { value: "email", key: "cowork.automationDeliveryEmail" },
  { value: "file", key: "cowork.automationDeliveryFile" },
] as const;

const IM_PLATFORMS = [
  { value: "feishu", label: "飞书 / Feishu" },
  { value: "qq", label: "QQ" },
  { value: "weixin", label: "微信 / WeChat" },
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
  const [outputAccount, setOutputAccount] = useState(initial?.outputAccount ?? "");
  const [outputDir, setOutputDir] = useState(initial?.outputDir ?? "");
  const [color, setColor] = useState(initial?.color ?? "#4488FF");
  const [location, setLocation] = useState(initial?.location ?? "");
  const [preview, setPreview] = useState<SchedulePreview | null>(null);
  const [saving, setSaving] = useState(false);
  // smartParsing tracks the on-demand LLM parse (🔍 智能解析). It's invoked only
  // by an explicit button click, never during typing — typing stays free
  // (regex-only PreviewSchedule). smartSrc marks the text the last smart parse
  // ran on so the button can hide after a successful parse until the text changes.
  const [smartParsing, setSmartParsing] = useState(false);
  const [smartSrc, setSmartSrc] = useState("");
  // IM-target picker state. recentChats is loaded once from the bot gateway so
  // the user can select a destination instead of hand-typing "feishu:oc_xxx".
  // imPlatform + imChatIndex drive a platform dropdown + chat dropdown; the
  // composed dest string is written back into outputDest on change.
  const [recentChats, setRecentChats] = useState<RecentChatView[]>([]);
  // emailAccounts is loaded from settings so the email mode can pick a sender.
  const [emailAccounts, setEmailAccounts] = useState<{name: string; default: boolean}[]>([]);
  // imPlatform defaults from the existing dest (parsed) or "feishu".
  const [imPlatform, setImPlatform] = useState<string>(() => {
    const d = initial?.outputDest ?? "";
    const idx = d.indexOf(":");
    return idx > 0 ? d.slice(0, idx) : "feishu";
  });
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

  // Load IM recent chats + email accounts once, for the IM/email pickers.
  // Failures are non-fatal (the pickers just show empty / fall back to manual).
  useEffect(() => {
    let cancelled = false;
    void app.ListRecentBotChats().then((cs) => { if (!cancelled) setRecentChats(cs); }).catch(() => {});
    void app.Settings().then((sv) => {
      if (cancelled) return;
      const accts = (sv.cowork?.emailAccounts ?? []).map((a) => ({ name: a.name, default: a.default }));
      setEmailAccounts(accts);
    }).catch(() => {});
    return () => { cancelled = true; };
  }, []);

  // composeImDest builds the dest string from platform + a recent chat. QQ group/
  // channel chats need the chatType segment so gw.Push routes to the right URL;
  // feishu/weixin route by chatID alone (2-segment dest).
  const composeImDest = (platform: string, chat: RecentChatView | undefined, manualDest: string) => {
    if (chat) {
      if (platform === "qq" && chat.chatType && chat.chatType !== "dm") {
        return `${platform}:${chat.chatType}:${chat.chatId}`;
      }
      return `${platform}:${chat.chatId}`;
    }
    return manualDest;
  };

  // Parse the current outputDest back into a selected chat index (for the
  // dropdown's controlled value) when it matches a known recent chat.
  const selectedChatIdx = (() => {
    if (outputMode !== "im") return -1;
    const filtered = recentChats.filter((c) => c.platform === imPlatform);
    return filtered.findIndex((c) => {
      const composed = composeImDest(imPlatform, c, "");
      return composed === outputDest || `${imPlatform}:${c.chatId}` === outputDest;
    });
  })();

  const applyTemplate = (tpl: TemplateView) => {
    setName(tpl.name);
    setExpression(tpl.expression);
    setPrompt(tpl.prompt);
    setOutputMode(tpl.outputMode);
  };

  // runSmartParse invokes the fast_task_model (迅捷任务模型) to resolve the
  // current expression into an absolute time. It's the ONLY path that calls the
  // LLM — triggered by an explicit button click, not by typing. On success the
  // model's resolved time replaces the preview; the composed "at YYYY-MM-DD HH:MM"
  // is also written back into the expression so saving stores the concrete time.
  const runSmartParse = async () => {
    const text = expression.trim();
    if (!text) return;
    setSmartParsing(true);
    try {
      const r = await app.SmartParseSchedule(text);
      setPreview(r);
      setSmartSrc(text);
      // If the model resolved a concrete time, adopt it as the stored expression
      // so the task is saved as an absolute one-shot rather than the raw phrase.
      if (r.kind === "oneshot" && r.expression) {
        setExpression(r.expression);
      }
    } catch {
      setPreview({ inputText: text, expression: "", absoluteTime: "", kind: "unknown", note: "智能解析调用失败" });
    } finally {
      setSmartParsing(false);
    }
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
    setSaving(true);
    try {
      await onSubmit({
        id: initial?.id ?? "",
        name: name.trim(),
        expression: expression.trim(),
        prompt: prompt.trim(),
        outputMode,
        outputDest: outputDest.trim(),
        outputAccount: outputAccount.trim(),
        outputDir: outputDir.trim(),
        color,
        location: location.trim(),
      });
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="cowork-taskform-overlay">
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

          {/* Color & Location */}
          <div className="cowork-taskform__section">
            <label className="cowork-taskform__label">
              <span className="cowork-taskform__labeltext">颜色与地点</span>
              <div style={{ display: "flex", gap: "8px" }}>
                <input
                  type="color"
                  className="cowork-taskform__input"
                  style={{ width: "40px", padding: "0" }}
                  value={color}
                  onChange={(e) => setColor(e.target.value)}
                />
                <input
                  className="cowork-taskform__input"
                  style={{ flex: 1 }}
                  value={location}
                  placeholder="地点 (如：会议室A)"
                  onChange={(e) => setLocation(e.target.value)}
                />
              </div>
            </label>
          </div>

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
                {/* Smart-parse button: shown only when the regex/expression path
                    failed (kind="unknown") AND the current text hasn't already
                    been smart-parsed. Clicking it is the sole trigger for the
                    迅捷任务模型 — typing never calls it. */}
                {preview.kind === "unknown" && expression.trim() && smartSrc !== expression.trim() && (
                  <button
                    type="button"
                    className="cowork-taskform__smart-btn"
                    onClick={() => void runSmartParse()}
                    disabled={smartParsing}
                    title="用迅捷任务模型解析复杂时间表达（如：下下周五下午3点）"
                  >
                    {smartParsing ? t("cowork.automationFormSmartParseRun") : t("cowork.automationFormSmartParse")}
                  </button>
                )}
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

            {/* IM: platform dropdown + recent-chat picker, with manual fallback. */}
            {outputMode === "im" && (
              <div className="cowork-taskform__im-picker">
                <div className="cowork-taskform__im-row">
                  <label className="cowork-taskform__label">
                    <span className="cowork-taskform__labeltext">{t("cowork.automationFormIMPlatform")}</span>
                    <select
                      className="cowork-taskform__input"
                      value={imPlatform}
                      onChange={(e) => {
                        setImPlatform(e.target.value);
                        setOutputDest(""); // reset dest when platform changes
                      }}
                    >
                      {IM_PLATFORMS.map((p) => (
                        <option key={p.value} value={p.value}>{p.label}</option>
                      ))}
                    </select>
                  </label>
                  <label className="cowork-taskform__label">
                    <span className="cowork-taskform__labeltext">{t("cowork.automationFormIMChat")}</span>
                    {(() => {
                      const filtered = recentChats.filter((c) => c.platform === imPlatform);
                      if (filtered.length === 0) {
                        return (
                          <span className="cowork-taskform__im-empty">{t("cowork.automationFormIMEmpty")}</span>
                        );
                      }
                      return (
                        <select
                          className="cowork-taskform__input"
                          value={selectedChatIdx >= 0 ? String(selectedChatIdx) : ""}
                          onChange={(e) => {
                            const i = Number(e.target.value);
                            const chat = filtered[i];
                            setOutputDest(composeImDest(imPlatform, chat, outputDest));
                          }}
                        >
                          <option value="">{t("cowork.automationFormIMSelect")}</option>
                          {filtered.map((c, i) => (
                            <option key={c.chatId + i} value={String(i)}>
                              {c.userName || c.chatId}
                              {c.chatType && c.chatType !== "dm" ? ` (${c.chatType})` : ""}
                              {" — "}{c.chatId.slice(0, 16)}{c.chatId.length > 16 ? "…" : ""}
                            </option>
                          ))}
                        </select>
                      );
                    })()}
                  </label>
                </div>
                <label className="cowork-taskform__label">
                  <span className="cowork-taskform__labeltext">{t("cowork.automationFormDest")}</span>
                  <input
                    className="cowork-taskform__input"
                    value={outputDest}
                    placeholder={t("cowork.automationFormDestHint")}
                    onChange={(e) => setOutputDest(e.target.value)}
                  />
                </label>
                <span className="cowork-taskform__im-hint">
                  {t("cowork.automationFormIMHint")}
                </span>
              </div>
            )}

            {/* Email: account dropdown (sender) + recipient dest. */}
            {outputMode === "email" && (
              <div className="cowork-taskform__email-picker">
                {emailAccounts.length > 0 && (
                  <label className="cowork-taskform__label">
                    <span className="cowork-taskform__labeltext">{t("cowork.automationFormEmailAccount")}</span>
                    <select
                      className="cowork-taskform__input"
                      value={outputAccount}
                      onChange={(e) => setOutputAccount(e.target.value)}
                    >
                      {emailAccounts.map((a) => (
                        <option key={a.name} value={a.name}>
                          {a.name}{a.default ? ` (${t("cowork.mailDefault")})` : ""}
                        </option>
                      ))}
                    </select>
                  </label>
                )}
                <label className="cowork-taskform__label">
                  <span className="cowork-taskform__labeltext">{t("cowork.automationFormDest")}</span>
                  <input
                    className="cowork-taskform__input"
                    value={outputDest}
                    placeholder={t("cowork.automationFormDestHint")}
                    onChange={(e) => setOutputDest(e.target.value)}
                  />
                </label>
              </div>
            )}

            {/* File: just the path. */}
            {outputMode === "file" && (
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

          {/* Output directory — soft isolation: concentrates the task's file
              artifacts (CSV/report/docs) into one folder instead of the shared
              workspace root. Optional; empty falls back to the active workspace. */}
          <div className="cowork-taskform__section">
            <label className="cowork-taskform__label">
              <span className="cowork-taskform__labeltext">{t("cowork.automationFormOutputDir")}</span>
              <input
                className="cowork-taskform__input"
                value={outputDir}
                placeholder={t("cowork.automationFormOutputDirHint")}
                onChange={(e) => setOutputDir(e.target.value)}
              />
            </label>
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
