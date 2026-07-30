// TaskCard renders one scheduled-task row in the automation panel. It shows the
// human-friendly schedule (with absolute next-fire), delivery mode, run count,
// last result (collapsed), and the action buttons: Run Now / Pause-Resume /
// Edit / Delete / History.
//
// The card is presentational — all mutations call back up to AutomationPanel,
// which talks to the bridge. This keeps the network surface in one place and
// lets the parent show toasts on completion.

import { useState } from "react";
import { Clock, Pause, Play, Pencil, Trash2, PlayCircle, History as HistoryIcon, AlertTriangle } from "lucide-react";

import type { TaskView } from "../../lib/types";
import { useT, type Translator } from "../../lib/i18n";

const DELIVERY_ICON: Record<string, string> = {
  "": "📝",
  im: "💬",
  email: "✉️",
  notify: "🔔",
  file: "💾",
};

export function TaskCard({
  task,
  onRunNow,
  onTogglePause,
  onEdit,
  onDelete,
  onShowHistory,
}: {
  task: TaskView;
  onRunNow: () => void;
  onTogglePause: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onShowHistory: () => void;
}) {
  const t = useT();
  const [showResult, setShowResult] = useState(false);
  const delivery = task.outputMode;

  return (
    <div className={`cowork-task-card ${task.enabled ? "" : "cowork-task-card--paused"}`}>
      <div className="cowork-task-card__head">
        <div className="cowork-task-card__title">
          <span className={`cowork-task-card__dot ${task.enabled ? "cowork-task-card__dot--on" : ""}`} />
          <span className="cowork-task-card__name">{task.name}</span>
          {task.source === "calendar" && <span className="cowork-task-card__badge cowork-task-card__badge--calendar">📅 日历</span>}
          {task.oneShot && <span className="cowork-task-card__badge">{t("cowork.automationOneShot")}</span>}
          {!task.enabled && <span className="cowork-task-card__badge cowork-task-card__badge--muted">{t("cowork.automationPaused")}</span>}
        </div>
        <div className="cowork-task-card__actions">
          <button
            className="cowork-task-card__btn"
            title={t("cowork.automationRunNow")}
            onClick={onRunNow}
          >
            <PlayCircle size={15} />
          </button>
          <button
            className="cowork-task-card__btn"
            title={task.enabled ? t("cowork.automationPause") : t("cowork.automationResume")}
            onClick={onTogglePause}
          >
            {task.enabled ? <Pause size={15} /> : <Play size={15} />}
          </button>
          <button
            className="cowork-task-card__btn"
            title={t("cowork.automationHistory")}
            onClick={onShowHistory}
          >
            <HistoryIcon size={15} />
          </button>
          <button
            className="cowork-task-card__btn"
            title={t("cowork.automationEdit")}
            onClick={onEdit}
          >
            <Pencil size={15} />
          </button>
          <button
            className="cowork-task-card__btn cowork-task-card__btn--danger"
            title={t("cowork.automationDelete")}
            onClick={onDelete}
          >
            <Trash2 size={15} />
          </button>
        </div>
      </div>

      <div className="cowork-task-card__meta">
        <span className="cowork-task-card__schedule">
          <Clock size={13} />
          {task.humanSchedule || task.expression}
        </span>
        {task.nextRun && (
          <span className="cowork-task-card__nextrun">
            {t("cowork.automationNextRun")}: {task.nextRun}
            <span className="cowork-task-card__countdown">{relativeCountdown(task.nextRun)}</span>
          </span>
        )}
      </div>

      <div className="cowork-task-card__footer">
        <span className="cowork-task-card__delivery">
          {DELIVERY_ICON[delivery] ?? "📝"} {t("cowork.automationDelivery")}: {deliveryLabel(t, delivery)}
        </span>
        <span className="cowork-task-card__runs">{t("cowork.automationRuns").replace("{n}", String(task.runCount))}</span>
        {task.lastRun && (
          <span
            className="cowork-task-card__lastresult"
            onClick={() => setShowResult((v) => !v)}
            title={task.lastResult}
          >
            {t("cowork.automationLastRun")}: {task.lastRun}
          </span>
        )}
      </div>
      {/* Delivery-failure banner: the agent ran but IM/email/file push didn't
          reach its destination. Without this the user would believe a silent
          drop was a success. Hidden when delivery succeeded or wasn't
          configured (store-only mode). */}
      {task.lastDeliverErr && (
        <div
          className="cowork-task-card__deliver-fail"
          title={task.lastDeliverAt ? `${task.lastDeliverAt}: ${task.lastDeliverErr}` : task.lastDeliverErr}
        >
          <AlertTriangle size={13} />
          <span>{t("cowork.automationDeliveryFailBanner")}：</span>
          <span className="cowork-task-card__deliver-fail-reason">{task.lastDeliverErr}</span>
        </div>
      )}
      {showResult && task.lastResult && (
        <pre className="cowork-task-card__result">{task.lastResult}</pre>
      )}
    </div>
  );
}

// relativeCountdown turns a "2006-01-02 15:04" nextRun into a compact relative
// hint like "3小时后" / "2天后" / "已过期". Computed once at render; not a live
// ticking timer (good enough for a card that refreshes on data reload).
function relativeCountdown(nextRun: string): string {
  const t = new Date(nextRun.replace(/-/g, "/")).getTime();
  if (isNaN(t)) return "";
  const diff = t - Date.now();
  if (diff < 0) return "（已过期）";
  const mins = Math.floor(diff / 60000);
  if (mins < 60) return `（${mins}分钟后）`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `（${hours}小时后）`;
  const days = Math.floor(hours / 24);
  return `（${days}天后）`;
}

function deliveryLabel(t: Translator, mode: string): string {
  switch (mode) {
    case "im":
      return t("cowork.automationDeliveryIM");
    case "email":
      return t("cowork.automationDeliveryEmail");
    case "notify":
      return t("cowork.automationDeliveryNotify");
    case "file":
      return t("cowork.automationDeliveryFile");
    default:
      return t("cowork.automationDeliveryStore");
  }
}
