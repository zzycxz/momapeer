// CalendarTaskPanel merges calendar events and scheduled tasks into one panel.
// Left: calendar grid (month/week/list). Right: task list + templates.
// Subscribes to "calendar:changed", "scheduler:changed", "scheduler:notice",
// and "calendar:reminder" events.

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Plus, ChevronLeft, ChevronRight, Calendar as CalendarIcon, Search,
  List as ListIcon, Grid3X3, Columns3, Download, Upload,
  PlayCircle, Pause, Play, Pencil, Trash2, History as HistoryIcon,
  LayoutTemplate,
} from "lucide-react";

import {
  app, onCalendarChanged, onSchedulerChanged, onSchedulerNotice,
} from "../../lib/bridge";
import type {
  CalendarEventView, CalendarEventInput,
  TaskView, TaskInput, TemplateView,
} from "../../lib/types";
import { TaskForm } from "./TaskForm";
import { EventEditForm } from "./EventEditForm";
import { RunHistory } from "./RunHistory";
import { useToast } from "../../lib/toast";
import { useConfirm } from "../../lib/confirm";

const WEEKDAYS = ["一", "二", "三", "四", "五", "六", "日"];
const HOURS = Array.from({ length: 14 }, (_, i) => i + 8);
const COLORS = ["#FF4444", "#4488FF", "#44BB44", "#FF8800", "#AA44FF", "#FF44AA", "#44CCCC", "#888888"];

// colorForEvent picks a color for an event. User-set color wins; otherwise we
// auto-classify by tags/source so the calendar reads at a glance: holidays=red,
// work=blue, personal=green, agent-created=purple, etc. Falls back to a stable
// hash of the title so the same event always gets the same color.
function colorForEvent(e: CalendarEventView): string {
  if (e.color) return e.color;
  // Tag-based auto-coloring (first matching tag wins).
  const tags = (e.tags ?? []).map(t => t.toLowerCase());
  const source = (e.source ?? "").toLowerCase();
  if (tags.includes("节假日") || tags.includes("holiday")) return "#FF4444"; // red
  if (tags.includes("工作") || tags.includes("work") || tags.includes("例会")) return "#4488FF"; // blue
  if (tags.includes("个人") || tags.includes("personal")) return "#44BB44"; // green
  if (source === "agent") return "#AA44FF"; // purple
  if (source === "email") return "#FF8800"; // orange
  // Stable hash → pick from the palette so identical titles stay consistent.
  let h = 0;
  for (let i = 0; i < e.title.length; i++) h = (h * 31 + e.title.charCodeAt(i)) | 0;
  return COLORS[Math.abs(h) % COLORS.length];
}
function isSameDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}
function isToday(d: Date): boolean { return isSameDay(d, new Date()); }
function buildGrid(year: number, month: number): (Date | null)[] {
  const first = new Date(year, month, 1);
  const last = new Date(year, month + 1, 0);
  const startDay = first.getDay();
  const offset = startDay === 0 ? 6 : startDay - 1;
  const grid: (Date | null)[] = [];
  for (let i = 0; i < offset; i++) grid.push(null);
  for (let d = 1; d <= last.getDate(); d++) grid.push(new Date(year, month, d));
  while (grid.length % 7 !== 0) grid.push(null);
  while (grid.length < 42) grid.push(null);
  return grid;
}
function getWeekDays(ref: Date): Date[] {
  const day = ref.getDay();
  const monday = new Date(ref);
  monday.setDate(ref.getDate() - (day === 0 ? 6 : day - 1));
  return Array.from({ length: 7 }, (_, i) => { const d = new Date(monday); d.setDate(monday.getDate() + i); return d; });
}
function eventsForDay(events: CalendarEventView[] | null | undefined, day: Date): CalendarEventView[] {
  return (events ?? []).filter((e) => isSameDay(new Date(e.start), day));
}
function formatTime(dateStr: string): string {
  return new Date(dateStr).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
function formatDate(dateStr: string): string {
  const d = new Date(dateStr); return `${d.getMonth() + 1}月${d.getDate()}日`;
}

type ViewMode = "month" | "week" | "list";
type TaskFilter = "all" | "manual" | "calendar";
type CreateMode = "event" | "task" | "template";

export function CalendarTaskPanel() {
  const { showToast } = useToast();
  const confirm = useConfirm();

  // Calendar state
  const [events, setEvents] = useState<CalendarEventView[]>([]);
  const [viewDate, setViewDate] = useState(() => new Date(new Date().getFullYear(), new Date().getMonth(), 1));
  const [viewMode, setViewMode] = useState<ViewMode>("month");
  const [selectedDay, setSelectedDay] = useState<Date | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<CalendarEventView[]>([]);
  const [holidays, setHolidays] = useState<CalendarEventView[]>([]);

  // Task state
  const [tasks, setTasks] = useState<TaskView[] | null>(null);
  const [templates, setTemplates] = useState<TemplateView[]>([]);
  const [taskFilter, setTaskFilter] = useState<TaskFilter>("all");

  // Form state
  const [taskFormOpen, setTaskFormOpen] = useState(false);
  const [editingTask, setEditingTask] = useState<TaskView | null>(null);
  const [presetTemplate, setPresetTemplate] = useState<TemplateView | null>(null);
  const [historyTask, setHistoryTask] = useState<{ id: string; name: string } | null>(null);
  const [createMode, setCreateMode] = useState<CreateMode | null | undefined>(undefined);
  // Pure calendar event (no associated task) form state.
  const [eventFormOpen, setEventFormOpen] = useState(false);
  const [editingEvent, setEditingEvent] = useState<CalendarEventView | null>(null);

  // --- Data loading ---
  const refreshEvents = useCallback(async () => {
    const y = viewDate.getFullYear(), m = viewDate.getMonth();
    const since = `${y}-${String(m + 1).padStart(2, "0")}-01`;
    const before = new Date(y, m + 2, 1);
    const beforeStr = `${before.getFullYear()}-${String(before.getMonth() + 1).padStart(2, "0")}-01`;
    try { setEvents(await app.ListScheduledTasksAsEvents(since, beforeStr)); } catch { setEvents([]); }
  }, [viewDate]);

  const refreshTasks = useCallback(async () => {
    try { setTasks(await app.ListScheduledTasks()); } catch { setTasks([]); }
  }, []);

  useEffect(() => { void refreshEvents(); }, [refreshEvents]);
  useEffect(() => { void refreshTasks(); void app.ScheduledTaskTemplates().then(setTemplates).catch(() => setTemplates([])); }, []);
  useEffect(() => onCalendarChanged(() => void refreshEvents()), [refreshEvents]);
  useEffect(() => onSchedulerChanged(() => void refreshTasks()), [refreshTasks]);
  useEffect(() => onSchedulerNotice((e) => showToast(`${e.name}: ${(e.result || "").slice(0, 100)}`, "info")), [showToast]);
  useEffect(() => {
    if (typeof window !== "undefined" && window.runtime) {
      return window.runtime.EventsOn("calendar:reminder", (...args: unknown[]) => {
        const d = (args?.[0] ?? {}) as { title?: string; body?: string };
        showToast(`${d.title || "提醒"}: ${d.body || ""}`, "info");
      });
    }
  }, [showToast]);
  useEffect(() => { app.GetChineseHolidays(viewDate.getFullYear()).then(setHolidays).catch(() => setHolidays([])); }, [viewDate.getFullYear()]);

  const grid = useMemo(() => buildGrid(viewDate.getFullYear(), viewDate.getMonth()), [viewDate]);
  const weekDays = useMemo(() => getWeekDays(selectedDay || new Date()), [selectedDay]);
  const todayEvents = selectedDay ? eventsForDay(events, selectedDay) : eventsForDay(events, new Date());
  const filteredTasks = useMemo(() => {
    if (!tasks) return [];
    return tasks.filter((tk) => taskFilter === "all" || tk.source === taskFilter);
  }, [tasks, taskFilter]);

  // --- Navigation ---
  const prev = () => { if (viewMode === "week") { const d = new Date(selectedDay || new Date()); d.setDate(d.getDate() - 7); setSelectedDay(d); } else setViewDate(new Date(viewDate.getFullYear(), viewDate.getMonth() - 1, 1)); };
  const next = () => { if (viewMode === "week") { const d = new Date(selectedDay || new Date()); d.setDate(d.getDate() + 7); setSelectedDay(d); } else setViewDate(new Date(viewDate.getFullYear(), viewDate.getMonth() + 1, 1)); };
  const goToday = () => { const n = new Date(); setViewDate(new Date(n.getFullYear(), n.getMonth(), 1)); setSelectedDay(n); };
  const headerTitle = viewMode === "month" ? `${viewDate.getFullYear()}年${viewDate.getMonth() + 1}月`
    : viewMode === "week" ? `${weekDays[0].getMonth() + 1}月${weekDays[0].getDate()}日 - ${weekDays[6].getMonth() + 1}月${weekDays[6].getDate()}日`
      : `${viewDate.getFullYear()}年${viewDate.getMonth() + 1}月`;

  // --- Calendar CRUD ---
  const handleSearch = async () => { if (!searchQuery.trim()) { setSearchResults([]); return; } setSearchResults(await app.SearchCalendarEvents(searchQuery, 20)); };

  // --- Task CRUD ---
  const handleCreateTask = async (input: TaskInput) => { await app.CreateScheduledTask(input); setTaskFormOpen(false); setEditingTask(null); setPresetTemplate(null); };
  const handleUpdateTask = async (input: TaskInput) => { if (editingTask) await app.UpdateScheduledTask({ ...input, id: editingTask.id }); setTaskFormOpen(false); setEditingTask(null); };
  const handleDeleteTask = async (task: TaskView) => { if (!(await confirm({ title: "删除任务", message: `确定删除任务"${task.name}"？` }))) return; try { await app.DeleteScheduledTask(task.id); } catch (e) { showToast(String(e), "error"); } };
  const handleRunNow = async (task: TaskView) => { showToast(`执行中: ${task.name}`, "info"); try { const r = await app.RunScheduledTaskNow(task.id); showToast(`完成: ${task.name}${r ? `: ${r.slice(0, 100)}` : ""}`, "info"); } catch (e) { showToast(`失败: ${task.name} - ${e}`, "error"); } };
  const handleTogglePause = async (task: TaskView) => { try { if (task.enabled) await app.PauseScheduledTask(task.id); else await app.ResumeScheduledTask(task.id); } catch (e) { showToast(String(e), "error"); } };

  // --- Pure calendar event CRUD (events with no associated task) ---
  // onEventClick routes a clicked calendar event: task-bound events open the
  // task editor; otherwise (agent/ICS/imported events with no taskId) the
  // EventEditForm is shown so they can be edited/deleted too.
  const onEventClick = (e: CalendarEventView) => {
    const task = (tasks || []).find(t => t.id === e.taskId);
    if (task) { setEditingTask(task); setTaskFormOpen(true); }
    else { setEditingEvent(e); setEventFormOpen(true); }
  };
  const handleCreateEvent = async (input: CalendarEventInput) => { await app.CreateCalendarEvent(input); setEventFormOpen(false); setEditingEvent(null); void refreshEvents(); };
  const handleUpdateEvent = async (input: CalendarEventInput) => { if (editingEvent) await app.UpdateCalendarEvent({ ...input, id: editingEvent.id }); setEventFormOpen(false); setEditingEvent(null); void refreshEvents(); };
  const handleDeleteEvent = async () => { if (!editingEvent) return; if (!(await confirm({ title: "删除日程", message: `确定删除日程"${editingEvent.title}"？` }))) return; try { await app.DeleteCalendarEvent(editingEvent.id); setEventFormOpen(false); setEditingEvent(null); void refreshEvents(); } catch (e) { showToast(String(e), "error"); } };

  // --- Create menu ---
  const openCreateMenu = () => setCreateMode(null);
  const closeCreateMenu = () => setCreateMode(undefined);
  const chooseCreateTask = () => { setCreateMode(null); setEditingTask(null); setPresetTemplate(null); setTaskFormOpen(true); };
  const chooseTemplate = (tpl: TemplateView) => { setCreateMode(null); setEditingTask(null); setPresetTemplate(tpl); setTaskFormOpen(true); };
  // ICS export/import via native file dialog.
  const handleExport = async () => {
    try {
      const r = await app.ExportCalendarDialog();
      if (r) showToast(r, "info");
    } catch (e) { showToast(`导出失败：${e}`, "error"); }
  };
  const handleImport = async () => {
    try {
      const r = await app.ImportCalendarDialog();
      if (r) { showToast(r, "info"); void refreshEvents(); }
    } catch (e) { showToast(`导入失败：${e}`, "error"); }
  };

  return (
    <div className="cowork-calendar-task">
      {/* Header */}
      <header className="cowork-main__header">
        <div className="cowork-calendar-task__nav">
          <button className="btn btn--icon" onClick={prev}><ChevronLeft size={16} /></button>
          <span className="cowork-calendar-task__title">{headerTitle}</span>
          <button className="btn btn--icon" onClick={next}><ChevronRight size={16} /></button>
          <button className="btn btn--small" onClick={goToday}>今天</button>
        </div>
        <div className="cowork-calendar-task__actions">
          <div className="cowork-calendar-task__view-switcher">
            <button className={`btn btn--icon ${viewMode === "month" ? "btn--active" : ""}`} onClick={() => setViewMode("month")} title="月"><Grid3X3 size={14} /></button>
            <button className={`btn btn--icon ${viewMode === "week" ? "btn--active" : ""}`} onClick={() => setViewMode("week")} title="周"><Columns3 size={14} /></button>
            <button className={`btn btn--icon ${viewMode === "list" ? "btn--active" : ""}`} onClick={() => setViewMode("list")} title="列表"><ListIcon size={14} /></button>
          </div>
          <div className="cowork-calendar-task__search">
            <Search size={14} />
            <input type="text" placeholder="搜索..." value={searchQuery} onChange={(e) => setSearchQuery(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") void handleSearch(); }} />
          </div>
          <button className="btn btn--icon" title="导出日历 (.ics)" onClick={() => void handleExport()}><Download size={14} /></button>
          <button className="btn btn--icon" title="导入日历 (.ics)" onClick={() => void handleImport()}><Upload size={14} /></button>
          <div className="cowork-calendar-task__create-wrap">
            <button className="btn btn--primary btn--small" onClick={openCreateMenu}><Plus size={14} /> 新建</button>
            {createMode === null && (
              <div className="cowork-calendar-task__create-menu" onClick={closeCreateMenu}>
                <div className="cowork-calendar-task__create-item" onClick={chooseCreateTask}>⏰ 新建任务</div>
                <div className="cowork-calendar-task__create-item" onClick={() => { setCreateMode(null); setEditingEvent(null); setEventFormOpen(true); }}>📅 新建日程</div>
                <div className="cowork-calendar-task__create-divider" />
                <div className="cowork-calendar-task__create-label">从模板建</div>
                {templates.map((tpl) => (
                  <div key={tpl.id} className="cowork-calendar-task__create-item cowork-calendar-task__create-tpl" onClick={() => chooseTemplate(tpl)}>
                    <LayoutTemplate size={12} /> {tpl.name}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </header>

      {/* Search results dropdown */}
      {searchResults.length > 0 && (
        <div className="cowork-calendar-task__search-results">
          <div className="cowork-calendar-task__search-header"><span>{searchResults.length} 个结果</span><button className="btn btn--text" onClick={() => setSearchResults([])}>清除</button></div>
          {searchResults.map((e) => (
            <div key={e.id} className="cowork-calendar-task__search-item" onClick={() => {
              onEventClick(e);
              setSearchResults([]);
            }}>
              <span className="cowork-calendar-task__dot" style={{ background: colorForEvent(e) }} /><span>{e.title}</span>
              <span className="cowork-calendar-task__search-time">{formatDate(e.start)}</span>
            </div>
          ))}
        </div>
      )}

      {/* Body: calendar + sidebar */}
      <div className="cowork-calendar-task__body">
        {/* Left: Calendar */}
        <div className="cowork-calendar-task__main">
          {viewMode === "month" && (
            <div className="cowork-calendar-task__grid">
              {WEEKDAYS.map((wd) => <div key={wd} className="cowork-calendar-task__weekday">{wd}</div>)}
              {grid.map((day, i) => {
                if (!day) return <div key={i} className="cowork-calendar-task__day cowork-calendar-task__day--empty" />;
                const dayEvents = eventsForDay(events, day);
                const dayHolidays = eventsForDay(holidays, day);
                const isHol = dayHolidays.length > 0;
                const today = isToday(day);
                const selected = selectedDay && isSameDay(day, selectedDay);
                return (
                  <div key={i} className={`cowork-calendar-task__day ${today ? "cowork-calendar-task__day--today" : ""} ${selected ? "cowork-calendar-task__day--selected" : ""} ${isHol ? "cowork-calendar-task__day--holiday" : ""}`} onClick={() => setSelectedDay(day)}>
                    <span className="cowork-calendar-task__day-num">{day.getDate()}</span>
                    {isHol && <span className="cowork-calendar-task__holiday-tag">{dayHolidays[0].title}</span>}
                    <div className="cowork-calendar-task__day-events">
                      {dayEvents.slice(0, 2).map((e) => (
                        <div key={e.id} className="cowork-calendar-task__event-dot" style={{ background: colorForEvent(e) }} title={e.title} onClick={(ev) => {
                          ev.stopPropagation();
                          onEventClick(e);
                        }}>
                          <span className="cowork-calendar-task__event-title">{e.title}</span>
                        </div>
                      ))}
                      {dayEvents.length > 2 && <span className="cowork-calendar-task__more">+{dayEvents.length - 2}</span>}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
          {viewMode === "week" && (
            <div className="cowork-calendar-task__week">
              <div className="cowork-calendar-task__week-header">
                <div className="cowork-calendar-task__week-time-col" />
                {weekDays.map((d, i) => (
                  <div key={i} className={`cowork-calendar-task__week-day-header ${isToday(d) ? "cowork-calendar-task__week-day-header--today" : ""}`}>
                    <span className="cowork-calendar-task__week-day-name">{WEEKDAYS[i]}</span>
                    <span className="cowork-calendar-task__week-day-num">{d.getDate()}</span>
                  </div>
                ))}
              </div>
              <div className="cowork-calendar-task__week-body">
                {HOURS.map((hour) => (
                  <div key={hour} className="cowork-calendar-task__week-row">
                    <div className="cowork-calendar-task__week-time-col">{`${hour}:00`}</div>
                    {weekDays.map((d, di) => {
                      const hourEvents = eventsForDay(events, d).filter((e) => !e.allDay && new Date(e.start).getHours() === hour);
                      return (
                        <div key={di} className="cowork-calendar-task__week-cell">
                          {hourEvents.map((e) => (
                            <div key={e.id} className="cowork-calendar-task__week-event" style={{ background: colorForEvent(e) }} onClick={() => {
                              onEventClick(e);
                            }}>
                              <span className="cowork-calendar-task__event-title">{e.title}</span>
                            </div>
                          ))}
                        </div>
                      );
                    })}
                  </div>
                ))}
              </div>
            </div>
          )}
          {viewMode === "list" && (
            <div className="cowork-calendar-task__list">
              {(() => {
                const sorted = [...(events ?? [])].sort((a, b) => new Date(a.start).getTime() - new Date(b.start).getTime());
                const groups = new Map<string, CalendarEventView[]>();
                for (const e of sorted) { const k = new Date(e.start).toLocaleDateString(); if (!groups.has(k)) groups.set(k, []); groups.get(k)!.push(e); }
                if (groups.size === 0) return <div className="cowork-calendar-task__empty-day">暂无日程</div>;
                return Array.from(groups.entries()).map(([date, evts]) => (
                  <div key={date} className="cowork-calendar-task__list-group">
                    <div className="cowork-calendar-task__list-date">{date}</div>
                    {evts.map((e) => (
                      <div key={e.id} className="cowork-calendar-task__list-item" onClick={() => {
                        onEventClick(e);
                      }}>
                        <span className="cowork-calendar-task__dot" style={{ background: colorForEvent(e) }} />
                        <span className="cowork-calendar-task__list-time">{e.allDay ? "全天" : formatTime(e.start)}</span>
                        <span className="cowork-calendar-task__list-title">{e.title}</span>
                        {e.outputMode === "im" && <span className="cowork-calendar-task__push-badge" title={`提醒推送 IM：${e.outputDest}`}>💬</span>}
                        {e.outputMode === "email" && <span className="cowork-calendar-task__push-badge" title={`提醒推送邮件：${e.outputDest}`}>✉️</span>}
                        {e.location && <span className="cowork-calendar-task__list-loc">📍 {e.location}</span>}
                      </div>
                    ))}
                  </div>
                ));
              })()}
            </div>
          )}
        </div>

        {/* Right sidebar: today + tasks */}
        <div className="cowork-calendar-task__sidebar">
          {/* Today's events */}
          <div className="cowork-calendar-task__sidebar-section">
            <h3><CalendarIcon size={14} />{selectedDay ? `${selectedDay.getMonth() + 1}月${selectedDay.getDate()}日` : "今日"}日程</h3>
            {todayEvents.length === 0 ? (
              <div className="cowork-calendar-task__empty-day">暂无日程</div>
            ) : (
              todayEvents.map((e) => (
                <div key={e.id} className="cowork-calendar-task__event-card" onClick={() => {
                  onEventClick(e);
                }}>
                  <div className="cowork-calendar-task__event-head">
                    <span className="cowork-calendar-task__dot" style={{ background: colorForEvent(e) }} />
                    <span className="cowork-calendar-task__event-name">{e.title}</span>
                  </div>
                  <div className="cowork-calendar-task__event-time">{e.allDay ? "全天" : `${formatTime(e.start)} - ${formatTime(e.end)}`}</div>
                  {e.recurrence && <span className="cowork-calendar-task__badge">🔁</span>}
                  {e.taskId && <span className="cowork-calendar-task__badge cowork-calendar-task__badge--task">⚡</span>}
                </div>
              ))
            )}
          </div>

          {/* Task filters */}
          <div className="cowork-calendar-task__sidebar-section">
            <div className="cowork-calendar-task__task-filters">
              <button className={`cowork-calendar-task__filter ${taskFilter === "all" ? "cowork-calendar-task__filter--active" : ""}`} onClick={() => setTaskFilter("all")}>全部</button>
              <button className={`cowork-calendar-task__filter ${taskFilter === "manual" ? "cowork-calendar-task__filter--active" : ""}`} onClick={() => setTaskFilter("manual")}>⏰ 手动</button>
              <button className={`cowork-calendar-task__filter ${taskFilter === "calendar" ? "cowork-calendar-task__filter--active" : ""}`} onClick={() => setTaskFilter("calendar")}>📅 日历</button>
            </div>
          </div>

          {/* Task list */}
          <div className="cowork-calendar-task__sidebar-section cowork-calendar-task__task-list">
            {tasks === null ? (
              <div className="cowork-calendar-task__loading">…</div>
            ) : filteredTasks.length === 0 ? (
              <div className="cowork-calendar-task__empty-day">暂无任务</div>
            ) : (
              filteredTasks.map((task) => (
                <div key={task.id} className={`cowork-calendar-task__task-card ${!task.enabled ? "cowork-calendar-task__task-card--paused" : ""}`}>
                  <div className="cowork-calendar-task__task-head">
                    <span className={`cowork-calendar-task__task-dot ${task.enabled ? "cowork-calendar-task__task-dot--on" : ""}`} />
                    <span className="cowork-calendar-task__task-name">{task.name}</span>
                    {task.source === "calendar" && <span className="cowork-calendar-task__badge cowork-calendar-task__badge--calendar">📅</span>}
                    {task.oneShot && <span className="cowork-calendar-task__badge">一次</span>}
                  </div>
                  <div className="cowork-calendar-task__task-meta">
                    {task.humanSchedule} {task.lastRun && `· 上次: ${task.lastRun.slice(5)}`}
                  </div>
                  <div className="cowork-calendar-task__task-actions">
                    <button className="cowork-calendar-task__btn" title="立即执行" onClick={() => void handleRunNow(task)}><PlayCircle size={13} /></button>
                    <button className="cowork-calendar-task__btn" title={task.enabled ? "暂停" : "恢复"} onClick={() => void handleTogglePause(task)}>{task.enabled ? <Pause size={13} /> : <Play size={13} />}</button>
                    <button className="cowork-calendar-task__btn" title="编辑" onClick={() => { setEditingTask(task); setTaskFormOpen(true); }}><Pencil size={13} /></button>
                    <button className="cowork-calendar-task__btn" title="删除" onClick={() => void handleDeleteTask(task)}><Trash2 size={13} /></button>
                    <button className="cowork-calendar-task__btn" title="历史" onClick={() => setHistoryTask({ id: task.id, name: task.name })}><HistoryIcon size={13} /></button>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      </div>

      {/* Task form modal */}
      {taskFormOpen && (
        <TaskForm
          initial={editingTask}
          initialTemplate={presetTemplate}
          templates={templates}
          onSubmit={(input) => editingTask ? handleUpdateTask(input) : handleCreateTask(input)}
          onCancel={() => { setTaskFormOpen(false); setEditingTask(null); setPresetTemplate(null); }}
          onDelete={editingTask ? () => { void confirm({ title: "删除任务", message: `确定删除"${editingTask.name}"？` }).then((ok) => { if (ok) { void app.DeleteScheduledTask(editingTask.id).then(() => { setTaskFormOpen(false); setEditingTask(null); }); } }); } : undefined}
        />
      )}

      {/* Pure calendar event form modal */}
      {eventFormOpen && (
        <EventEditForm
          initial={editingEvent}
          onSubmit={editingEvent ? handleUpdateEvent : handleCreateEvent}
          onDelete={editingEvent ? handleDeleteEvent : undefined}
          onCancel={() => { setEventFormOpen(false); setEditingEvent(null); }}
        />
      )}

      {/* Run history drawer */}
      {historyTask && (
        <RunHistory taskID={historyTask.id} taskName={historyTask.name} onClose={() => setHistoryTask(null)} />
      )}
    </div>
  );
}
