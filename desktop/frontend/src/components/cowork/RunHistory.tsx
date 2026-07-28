// RunHistory is a slide-in drawer showing recent run records for one task (or
// all tasks when taskName is empty). Loaded on open via app.ScheduledTaskHistory.
// The drawer is dismissable; clicking a row expands its truncated result.

import { useEffect, useState } from "react";
import { X } from "lucide-react";

import { app } from "../../lib/bridge";
import type { RunRecordView } from "../../lib/types";
import { useT, type Translator } from "../../lib/i18n";

export function RunHistory({
  taskID,
  taskName,
  onClose,
}: {
  taskID: string;
  taskName: string;
  onClose: () => void;
}) {
  const t = useT();
  const [records, setRecords] = useState<RunRecordView[] | null>(null);

  useEffect(() => {
    let cancelled = false;
    setRecords(null);
    void app.ScheduledTaskHistory(taskID).then((rs) => {
      if (!cancelled) setRecords(rs);
    });
    return () => {
      cancelled = true;
    };
  }, [taskID]);

  return (
    <div className="cowork-history-overlay" onClick={onClose}>
      <div className="cowork-history-drawer" onClick={(e) => e.stopPropagation()}>
        <header className="cowork-history-drawer__head">
          <h3>
            {t("cowork.automationHistory")}
            {taskName ? ` · ${taskName}` : ""}
          </h3>
          <button className="cowork-task-card__btn" onClick={onClose} title="close">
            <X size={16} />
          </button>
        </header>
        <div className="cowork-history-drawer__body">
          {records === null ? (
            <div className="cowork-history-drawer__empty">…</div>
          ) : records.length === 0 ? (
            <div className="cowork-history-drawer__empty">{t("cowork.automationHistoryEmpty")}</div>
          ) : (
            records.map((r, i) => <RunHistoryRow key={`${r.taskId}-${r.at}-${i}`} r={r} t={t} />)
          )}
        </div>
      </div>
    </div>
  );
}

function RunHistoryRow({ r, t }: { r: RunRecordView; t: Translator }) {
  const [open, setOpen] = useState(false);
  // Map the run status to a CSS modifier. deliver_failed / deliver_skipped are
  // delivery outcomes (the agent ran fine but IM/email/file push didn't reach
  // its destination) — rendered with their own colors so they stand out from a
  // pure run error.
  const statusCls = `cowork-history-row__status cowork-history-row__status--${r.status}`;
  const statusLabel = (() => {
    switch (r.status) {
      case "ok":
        return t("cowork.automationHistoryStatusOk");
      case "error":
        return t("cowork.automationHistoryStatusError");
      case "deliver_failed":
        return t("cowork.automationHistoryStatusDeliverFailed");
      case "deliver_skipped":
        return t("cowork.automationHistoryStatusDeliverSkipped");
      default:
        return t("cowork.automationHistoryStatusSkipped");
    }
  })();
  return (
    <div className="cowork-history-row" onClick={() => setOpen((v) => !v)}>
      <div className="cowork-history-row__head">
        <span className={statusCls}>{statusLabel}</span>
        <span className="cowork-history-row__at">{r.at}</span>
        <span className="cowork-history-row__name">{r.name}</span>
      </div>
      {open && r.result && <pre className="cowork-history-row__result">{r.result}</pre>}
    </div>
  );
}
