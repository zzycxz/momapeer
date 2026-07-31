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

// exprToPickerValue converts a one-shot scheduler expression ("at 2026-07-31
// 09:50") back into the "YYYY-MM-DDTHH:MM" value an <input type="datetime-local">
// expects. The scheduler stores time with a SPACE separator (see parseAt in
// internal/scheduler/expr.go), but datetime-local uses a "T", so we swap it back.
// Returns "" for non-one-shot / unparseable expressions — the picker renders empty
// for recurring tasks (daily/every) and raw natural-language phrases.
function exprToPickerValue(expr: string): string {
  const e = expr.trim();
  if (!e.toLowerCase().startsWith("at ")) return "";
  const rest = e.slice(3).trim();
  // Accept "YYYY-MM-DD HH:MM" (the canonical stored form). Replace the space with
  // T to match datetime-local's expected value format.
  const m = rest.match(/^(\d{4}-\d{2}-\d{2})\s+(\d{2}:\d{2})/);
  if (!m) return "";
  return `${m[1]}T${m[2]}`;
}

// pickerValueToExpr is the inverse: the datetime-local value ("2026-07-31T09:50")
// becomes a stored scheduler expression ("at 2026-07-31 09:50") — T → space,
// prefixed with "at ". This matches what the backend itself produces
// (scheduler_app.go) and what SmartParseSchedule writes back.
function pickerValueToExpr(v: string): string {
  const trimmed = v.trim();
  if (!trimmed) return "";
  return "at " + trimmed.replace("T", " ");
}

// looksLikeEmailDirective reports whether a prompt reads like a "send an email"
// instruction (the user wants to send fixed content) rather than an AI task
// ("summarize today's email"). When output_mode=email AND this is true, the two
// delivery paths fight: the scheduler sends the AI's (likely error) output as
// the body, while the AI's own email_send call gets denied in headless mode. We
// detect this so the form can suggest switching to plain mode (write the body in
// the prompt, let the scheduler send it directly).
function looksLikeEmailDirective(prompt: string): boolean {
  const p = prompt.trim();
  if (!p) return false;
  const low = p.toLowerCase();
  // Email address present anywhere → almost certainly a send intent.
  if (/[^\s@]+@[^\s@]+\.[^\s@]+/.test(p)) return true;
  // Chinese send-mail phrasing.
  if (/发(一封)?(测试)?邮件|发送邮件|寄信|写信给/.test(p)) return true;
  // English send-mail phrasing.
  if (/\bsend\b.*\b(email|mail)\b|\b(email|mail)\b.*\bsend\b/.test(low)) return true;
  return false;
}

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
  // plain = "纯提醒" toggle. When on, the task surfaces its prompt verbatim at
  // fire time (toast/IM/email body) WITHOUT running the agent. Default OFF so a
  // task always runs the agent unless the user explicitly opts into plain mode —
  // this is the safe default (never silently drop an AI directive).
  const [plain, setPlain] = useState(initial?.plain ?? false);
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
        plain,
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
            {/* Two-column time input: natural-language phrase (left) + manual
                datetime picker (right). The picker is a derived view: its value
                prefers the stored `expression` (when the user already picked /
                it was written back as "at ..."), and falls back to the resolved
                preview.absoluteTime so a just-typed phrase like "9点50" — which
                hasn't been rewritten into "at ..." yet — still shows in the
                picker. Picking writes back via pickerValueToExpr. It's disabled
                for recurring expressions (daily/every) which have no single
                instant. */}
            <div className="cowork-taskform__time-row">
              <input
                className="cowork-taskform__input"
                style={{ flex: 1 }}
                value={expression}
                placeholder={t("cowork.automationFormExpressionHint")}
                onChange={(e) => setExpression(e.target.value)}
              />
              <input
                type="datetime-local"
                className="cowork-taskform__input cowork-taskform__picker"
                style={{ width: "200px" }}
                value={exprToPickerValue(expression) || (preview?.kind === "oneshot" && preview.absoluteTime ? preview.absoluteTime.replace(" ", "T") : "")}
                disabled={preview?.kind === "recurring"}
                title={preview?.kind === "recurring" ? t("cowork.automationFormDatetimeRecurring") : t("cowork.automationFormDatetimeHint")}
                onChange={(e) => setExpression(pickerValueToExpr(e.target.value))}
              />
            </div>
            {preview?.kind === "recurring" && (
              <span className="cowork-taskform__picker-hint">{t("cowork.automationFormDatetimeRecurring")}</span>
            )}
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

          {/* Action / 任务内容 */}
          <label className="cowork-taskform__label cowork-taskform__label--top">
            <span className="cowork-taskform__labeltext">{t("cowork.automationFormAction")}</span>
            <textarea
              className="cowork-taskform__textarea"
              value={prompt}
              rows={5}
              placeholder={t("cowork.automationFormActionHint")}
              onChange={(e) => setPrompt(e.target.value)}
            />
          </label>

          {/* 纯提醒开关：ON = 到点直接弹原文不调AI；OFF（默认）= 走完整 agent。
              这是显式用户意图，替代此前会误判的启发式（"周报"无动词会被误判为纯提醒）。
              默认 OFF 保证 AI 任务永远不会被静默吞掉。 */}
          <label className="cowork-taskform__plain-toggle">
            <input
              type="checkbox"
              checked={plain}
              onChange={(e) => setPlain(e.target.checked)}
            />
            <span>{t("cowork.automationFormPlain")}</span>
            <span className="cowork-taskform__plain-hint">
              {plain ? t("cowork.automationFormPlainOnHint") : t("cowork.automationFormPlainOffHint")}
            </span>
          </label>

          {/* 邮件任务防双发提示：output_mode=email 且 prompt 看起来像"发邮件给xxx"指令时，
              提醒用户改用纯提醒模式（任务内容写正文，让调度器直接发），避免 AI 调 email_send
              被 headless 权限拒绝、同时调度器又把 AI 的报错文字当正文发出去。 */}
          {outputMode === "email" && !plain && looksLikeEmailDirective(prompt) && (
            <div className="cowork-taskform__warn">
              {t("cowork.automationFormEmailDirectiveWarn")}
              <button
                type="button"
                className="cowork-taskform__warn-btn"
                onClick={() => setPlain(true)}
              >
                {t("cowork.automationFormEmailDirectiveFix")}
              </button>
            </div>
          )}

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
