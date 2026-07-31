// AutomationPanel is the coWork "自动化" panel: a live list of scheduled tasks
// with a header that exposes New Task + template quick-pick. It subscribes to
// the "scheduler:changed" event so cards refresh on any mutation (create / edit
// / delete / run / pause), and to "scheduler:notice" to surface fired task
// results as in-app toasts (works even when the user is on another panel).
//
// The panel owns three child components:
//   - TaskCard: one row, calls back for run/pause/edit/delete/history
//   - TaskForm: modal for create/edit
//   - RunHistory: slide-in drawer for per-task run records

import { useCallback, useEffect, useState } from "react";
import { Plus, LayoutTemplate } from "lucide-react";

import { app, onSchedulerChanged, onSchedulerNotice } from "../../lib/bridge";
import type { TaskInput, TaskView, TemplateView } from "../../lib/types";
import { useT } from "../../lib/i18n";
import { useToast } from "../../lib/toast";
import { useConfirm } from "../../lib/confirm";
import { TaskCard } from "./TaskCard";
import { TaskForm } from "./TaskForm";
import { RunHistory } from "./RunHistory";

export function AutomationPanel() {
  const t = useT();
  const { showToast } = useToast();
  const confirm = useConfirm();
  const [tasks, setTasks] = useState<TaskView[] | null>(null);
  const [templates, setTemplates] = useState<TemplateView[]>([]);
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<TaskView | null>(null);
  const [presetTemplate, setPresetTemplate] = useState<TemplateView | null>(null);
  const [historyTask, setHistoryTask] = useState<{ id: string; name: string } | null>(null);

  const refresh = useCallback(async () => {
    try {
      const list = await app.ListScheduledTasks();
      setTasks(list);
    } catch {
      setTasks([]);
    }
  }, []);

  // Initial load + template load.
  useEffect(() => {
    void refresh();
    void app.ScheduledTaskTemplates().then(setTemplates).catch(() => setTemplates([]));
  }, [refresh]);

  // Re-fetch on any backend mutation.
  useEffect(() => onSchedulerChanged(() => void refresh()), [refresh]);

  // Surface fired-task notices as toasts.
  useEffect(() => {
    return onSchedulerNotice((e) => {
      const head = e.name || t("cowork.automationNotifyTitle");
      const body = (e.result || "").slice(0, 120);
      showToast(`${head}: ${body}`, "info");
    });
  }, [showToast, t]);

  const openNew = () => {
    setEditing(null);
    setPresetTemplate(null);
    setFormOpen(true);
  };
  const openEdit = (task: TaskView) => {
    setEditing(task);
    setPresetTemplate(null);
    setFormOpen(true);
  };
  const openFromTemplate = (tpl: TemplateView) => {
    setEditing(null);
    setPresetTemplate(tpl);
    setFormOpen(true);
  };

  const handleSubmit = async (input: TaskInput) => {
    if (editing) {
      await app.UpdateScheduledTask({ ...input, id: editing.id });
    } else {
      await app.CreateScheduledTask(input);
    }
    setFormOpen(false);
    setEditing(null);
    setPresetTemplate(null);
    void refresh();
  };

  const handleDelete = async (task: TaskView) => {
    if (!(await confirm({ title: "删除任务", message: t("cowork.automationConfirmDelete").replace("{name}", task.name) }))) return;
    try {
      await app.DeleteScheduledTask(task.id);
    } catch (e) {
      showToast(String(e), "error");
    }
  };

  const handleRunNow = async (task: TaskView) => {
    showToast(t("cowork.automationRunning").replace("{name}", task.name), "info");
    try {
      const res = await app.RunScheduledTaskNow(task.id);
      showToast(t("cowork.automationRunDone").replace("{name}", task.name) + (res ? `: ${res.slice(0, 100)}` : ""), "info");
    } catch (e) {
      showToast(t("cowork.automationRunFailed").replace("{name}", task.name).replace("{err}", String(e)), "error");
    }
  };

  const handleTogglePause = async (task: TaskView) => {
    try {
      if (task.enabled) {
        await app.PauseScheduledTask(task.id);
      } else {
        await app.ResumeScheduledTask(task.id);
      }
    } catch (e) {
      showToast(String(e), "error");
    }
  };

  return (
    <div className="cowork-automation">
      <header className="cowork-main__header">
        <h2>{t("cowork.automation")}</h2>
        <button className="btn btn--primary btn--small" onClick={openNew}>
          <Plus size={14} />
          {t("cowork.automationNew")}
        </button>
      </header>

      <div className="cowork-automation__body">
        {templates.length > 0 && (
          <div className="cowork-automation__templates">
            <span className="cowork-automation__templates-label">
              <LayoutTemplate size={13} />
              {t("cowork.automationTemplate")}
            </span>
            {templates.map((tpl) => (
              <button
                key={tpl.id}
                className="cowork-automation__template"
                title={tpl.desc}
                onClick={() => openFromTemplate(tpl)}
              >
                {tpl.name}
              </button>
            ))}
          </div>
        )}

        {tasks === null ? (
          <div className="cowork-automation__loading">…</div>
        ) : tasks.length === 0 ? (
          <div className="cowork-automation__empty">{t("cowork.automationEmpty")}</div>
        ) : (
          <>
            <div className="cowork-automation__count">
              {t("cowork.automationCount").replace("{n}", String(tasks.length))}
            </div>
            <div className="cowork-automation__list">
              {tasks.map((task) => (
                <TaskCard
                  key={task.id}
                  task={task}
                  onRunNow={() => void handleRunNow(task)}
                  onTogglePause={() => void handleTogglePause(task)}
                  onEdit={() => openEdit(task)}
                  onDelete={() => void handleDelete(task)}
                  onShowHistory={() => setHistoryTask({ id: task.id, name: task.name })}
                />
              ))}
            </div>
          </>
        )}
      </div>

      {formOpen && (
        <TaskForm
          initial={editing}
          initialTemplate={presetTemplate}
          templates={templates}
          onSubmit={(input) => handleSubmit(input)}
          onCancel={() => {
            setFormOpen(false);
            setEditing(null);
            setPresetTemplate(null);
          }}
          onDelete={
            editing
              ? () => {
                  void confirm({ title: "删除任务", message: t("cowork.automationConfirmDelete").replace("{name}", editing.name) }).then((ok) => {
                    if (ok) {
                      void app.DeleteScheduledTask(editing.id).then(() => {
                        setFormOpen(false);
                        setEditing(null);
                        setPresetTemplate(null);
                      });
                    }
                  });
                }
              : undefined
          }
        />
      )}

      {historyTask && (
        <RunHistory
          taskID={historyTask.id}
          taskName={historyTask.name}
          onClose={() => setHistoryTask(null)}
        />
      )}
    </div>
  );
}
