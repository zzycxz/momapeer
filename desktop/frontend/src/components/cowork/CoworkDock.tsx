// CoworkDock is the cowork-mode right-side panel: a tabbed overview of today's
// events, the user's mailbox status, and the active workspace's files. It
// mirrors the coding-mode workbench-dock in structure but presents cowork-
// flavored content (calendar events, scheduled tasks, mail probe).
//
// Tabs:
//   - 今日 (today): today's calendar events + upcoming scheduled tasks.
//   - 邮件 (mail): mailbox connection status (ProbeMailAccount: ok/error/
//     unconfigured). The lightweight probe is all we show here — full mail
//     reading lives in the dedicated mail tools.
//   - 文件 (files): a flat list of the active workspace's top-level entries
//     (ListDir ""), so the user can browse/peek project files without leaving
//     cowork mode.
//
// When mode === "rag" (the user opened the knowledge base panel), the dock
// shows a knowledge nav: collections list. The graph canvas itself lives in
// RagPanel; this nav complements it.
//
// Go methods used (all verified to exist):
//   - ListCalendarEvents(since, before string) → []CalendarEventView
//   - ListScheduledTasks() → []TaskView
//   - ProbeMailAccount() → MailProbeResult {OK, Status, Message}
//   - ListDir(rel string) → []DirEntry {Name, IsDir}
//   - ListRagCollections() → []RagCollectionView

import { useCallback, useEffect, useState } from "react";
import {
  CalendarDays,
  Inbox,
  Mail,
  RefreshCw,
  Folder,
  FileText,
  Network as NetworkIcon,
  Circle,
} from "lucide-react";

import { app, onCalendarChanged, onSchedulerChanged, onRagChanged } from "../../lib/bridge";
import type {
  CalendarEventView,
  TaskView,
  MailProbeResult,
  RagCollectionView,
  DirEntry,
} from "../../lib/types";
import { useT } from "../../lib/i18n";

type DockTab = "today" | "mail" | "files";

export interface CoworkDockProps {
  cwd?: string;
  maximized: boolean;
  onClose: () => void;
  onToggleMaximized: () => void;
  mode?: "default" | "rag";
  onEntityClick?: (name: string) => void;
  onFileClick?: (path: string) => void;
}

interface TodayData {
  events: CalendarEventView[];
  tasks: TaskView[];
  loading: boolean;
}

interface FilesData {
  entries: DirEntry[];
  loading: boolean;
}

interface RagNavData {
  collections: RagCollectionView[];
  loading: boolean;
}

// todayRange returns {since, before} ISO-ish strings for the local today
// (00:00 → 23:59), matching ListCalendarEvents's "2006-01-02T15:04" format.
function todayRange(): { since: string; before: string } {
  const d = new Date();
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return { since: `${y}-${m}-${day}T00:00`, before: `${y}-${m}-${day}T23:59` };
}

export function CoworkDock({
  cwd: _cwd,
  maximized,
  onClose,
  onToggleMaximized,
  mode = "default",
  onEntityClick: _onEntityClick,
  onFileClick,
}: CoworkDockProps) {
  const t = useT();
  const [tab, setTab] = useState<DockTab>("today");

  // --- today data ---
  const [today, setToday] = useState<TodayData>({ events: [], tasks: [], loading: true });
  const refreshToday = useCallback(async () => {
    setToday((s) => ({ ...s, loading: true }));
    try {
      const { since, before } = todayRange();
      const [events, tasks] = await Promise.all([
        app.ListCalendarEvents(since, before).catch(() => [] as CalendarEventView[]),
        app.ListScheduledTasks().catch(() => [] as TaskView[]),
      ]);
      // Upcoming = enabled tasks with a future nextRun.
      const upcoming = (tasks ?? []).filter(
        (tk) => tk.enabled && tk.nextRun && new Date(tk.nextRun).getTime() > Date.now() - 86400000,
      );
      setToday({ events: events ?? [], tasks: upcoming, loading: false });
    } catch {
      setToday({ events: [], tasks: [], loading: false });
    }
  }, []);

  // --- mail data ---
  const [mail, setMail] = useState<{ probe: MailProbeResult | null; loading: boolean }>({
    probe: null,
    loading: true,
  });
  const refreshMail = useCallback(async () => {
    setMail({ probe: null, loading: true });
    try {
      const probe = await app.ProbeMailAccount().catch(() => null);
      setMail({ probe, loading: false });
    } catch {
      setMail({ probe: null, loading: false });
    }
  }, []);

  // --- files data ---
  const [files, setFiles] = useState<FilesData>({ entries: [], loading: true });
  const refreshFiles = useCallback(async () => {
    setFiles({ entries: [], loading: true });
    try {
      const entries = await app.ListDir("").catch(() => [] as DirEntry[]);
      setFiles({ entries: entries ?? [], loading: false });
    } catch {
      setFiles({ entries: [], loading: false });
    }
  }, []);

  // --- rag nav data ---
  const [ragNav, setRagNav] = useState<RagNavData>({ collections: [], loading: true });
  const refreshRagNav = useCallback(async () => {
    setRagNav({ collections: [], loading: true });
    try {
      const cols = await app.ListRagCollections().catch(() => [] as RagCollectionView[]);
      setRagNav({ collections: cols ?? [], loading: false });
    } catch {
      setRagNav({ collections: [], loading: false });
    }
  }, []);

  // Initial load + live refresh subscriptions.
  useEffect(() => {
    if (mode === "rag") {
      void refreshRagNav();
      return onRagChanged(() => void refreshRagNav());
    }
    void refreshToday();
    void refreshMail();
    void refreshFiles();
    const unsub1 = onCalendarChanged(() => void refreshToday());
    const unsub2 = onSchedulerChanged(() => void refreshToday());
    return () => {
      unsub1();
      unsub2();
    };
  }, [mode, refreshToday, refreshMail, refreshFiles, refreshRagNav]);

  return (
    <aside className="cowork-dock" aria-label={t("coworkDock.label") || "办公概览"}>
      <div className="cowork-dock__tools">
        {mode === "rag" ? (
          <div className="cowork-dock__tabs">
            <button className="cowork-dock__tab cowork-dock__tab--active" type="button">
              <NetworkIcon size={13} />
              <span className="cowork-dock__tab-label">{t("cowork.knowledge") || "知识库"}</span>
            </button>
          </div>
        ) : (
          <div className="cowork-dock__tabs">
            <button
              className={`cowork-dock__tab ${tab === "today" ? "cowork-dock__tab--active" : ""}`}
              type="button"
              onClick={() => {
                setTab("today");
                void refreshToday();
              }}
              title={t("coworkDock.today") || "今日"}
            >
              <CalendarDays size={13} />
              <span className="cowork-dock__tab-label">{t("coworkDock.today") || "今日"}</span>
            </button>
            <button
              className={`cowork-dock__tab ${tab === "mail" ? "cowork-dock__tab--active" : ""}`}
              type="button"
              onClick={() => {
                setTab("mail");
                void refreshMail();
              }}
              title={t("coworkDock.mail") || "邮件"}
            >
              <Mail size={13} />
              <span className="cowork-dock__tab-label">{t("coworkDock.mail") || "邮件"}</span>
            </button>
            <button
              className={`cowork-dock__tab ${tab === "files" ? "cowork-dock__tab--active" : ""}`}
              type="button"
              onClick={() => {
                setTab("files");
                void refreshFiles();
              }}
              title={t("coworkDock.files") || "文件"}
            >
              <Inbox size={13} />
              <span className="cowork-dock__tab-label">{t("coworkDock.files") || "文件"}</span>
            </button>
          </div>
        )}
        <button
          className="cowork-dock__tab"
          type="button"
          onClick={onToggleMaximized}
          title={maximized ? "还原" : "最大化"}
          aria-label={maximized ? "还原" : "最大化"}
        >
          {maximized ? "❐" : "▢"}
        </button>
        <button className="cowork-dock__tab" type="button" onClick={onClose} title="关闭" aria-label="关闭">
          ✕
        </button>
      </div>

      <div className="cowork-dock__body">
        {mode === "rag" ? (
          <RagNavView data={ragNav} />
        ) : tab === "today" ? (
          <TodayView data={today} />
        ) : tab === "mail" ? (
          <MailView data={mail} onRefresh={() => void refreshMail()} />
        ) : (
          <FilesView data={files} onRefresh={() => void refreshFiles()} onFileClick={onFileClick} />
        )}
      </div>
    </aside>
  );
}

// --- 今日 view: today's calendar events + upcoming scheduled tasks --------

function TodayView({ data }: { data: TodayData }) {
  const t = useT();
  if (data.loading) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }
  const hasEvents = data.events.length > 0;
  const hasUpcoming = data.tasks.length > 0;
  if (!hasEvents && !hasUpcoming) {
    return (
      <div className="cowork-dock__empty-state">
        <CalendarDays size={22} />
        <p>{t("coworkDock.noEvents") || "今日暂无日程"}</p>
        <span className="cowork-dock__empty-hint">
          {t("coworkDock.noUpcoming") || "暂无待触发任务"}
        </span>
      </div>
    );
  }
  return (
    <div className="cowork-today">
      {hasEvents && (
        <div className="cowork-today__section">
          <h3 className="cowork-today__heading">
            <CalendarDays size={12} />
            {t("coworkDock.todayEvents") || "今日日程"}
          </h3>
          <ul className="cowork-today__list">
            {data.events.slice(0, 8).map((e, i) => (
              <li key={e.id ?? i} className="cowork-today__row" title={e.title}>
                <span className="cowork-today__time">{formatEventTime(e.start)}</span>
                <span className="cowork-today__dot" />
                <span className="cowork-today__text">{e.title}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {hasUpcoming && (
        <div className="cowork-today__section">
          <h3 className="cowork-today__heading">
            <Circle size={12} />
            {t("coworkDock.upcoming") || "即将触发"}
          </h3>
          <ul className="cowork-today__list">
            {data.tasks.slice(0, 5).map((task, i) => (
              <li key={task.id ?? i} className="cowork-today__row" title={task.name}>
                <span className="cowork-today__time">{shortTime(task.nextRun)}</span>
                <span className="cowork-today__dot cowork-today__dot--plain" />
                <span className="cowork-today__text">{task.name}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

// formatEventTime extracts "HH:MM" from "2006-01-02T15:04" (or "" on parse fail).
function formatEventTime(s: string): string {
  if (!s) return "";
  const m = s.match(/T(\d{2}:\d{2})/);
  return m ? m[1] : "";
}

// shortTime renders a nextRun timestamp as a compact "MM-DD HH:MM" for the
// upcoming list. Falls back to the raw string when it can't be parsed.
function shortTime(s: string): string {
  if (!s) return "";
  const d = new Date(s);
  if (isNaN(d.getTime())) return s;
  const mm = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const mi = String(d.getMinutes()).padStart(2, "0");
  return `${mm}-${dd} ${hh}:${mi}`;
}

// --- 邮件 view: mailbox probe status --------------------------------

function MailView({ data, onRefresh }: { data: { probe: MailProbeResult | null; loading: boolean }; onRefresh: () => void }) {
  const t = useT();
  if (data.loading) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }
  const probe = data.probe;
  const ok = probe?.ok || probe?.status === "ok";
  if (!probe || probe.status === "unconfigured") {
    return (
      <div className="cowork-dock__empty-state">
        <Mail size={22} />
        <p>{t("coworkDock.mailUnconfigured") || "未配置"}</p>
        <span className="cowork-dock__empty-hint">
          {t("coworkDock.mailConfigureHint") || "请在「设置」-「办公」中配置邮箱"}
        </span>
        <button className="cowork-dock__tab" type="button" onClick={onRefresh} title="重试">
          <RefreshCw size={12} />
        </button>
      </div>
    );
  }
  return (
    <div className="cowork-mailtab">
      <div className="cowork-mailtab__head">
        <span className="cowork-mailtab__status">
          {ok ? (t("coworkDock.mailConnected") || "已连接") : (t("coworkDock.mailError") || "连接失败")}
        </span>
        <button className="cowork-mailtab__refresh" type="button" onClick={onRefresh} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
      {probe.message && (
        <div className="cowork-today__section">
          <p className="cowork-dock__empty-hint">{probe.message}</p>
        </div>
      )}
      {!ok && (
        <div className="cowork-dock__empty-hint">
          {t("coworkDock.mailConfigureHint") || "请在「设置」-「办公」中检查邮箱配置"}
        </div>
      )}
    </div>
  );
}

// --- 文件 view: workspace top-level entries --------------------------

function FilesView({
  data,
  onRefresh,
  onFileClick,
}: {
  data: FilesData;
  onRefresh: () => void;
  onFileClick?: (path: string) => void;
}) {
  const t = useT();
  if (data.loading) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }
  if (data.entries.length === 0) {
    return (
      <div className="cowork-dock__empty-state">
        <Folder size={22} />
        <p>空工作区</p>
        <button className="cowork-dock__tab" type="button" onClick={onRefresh} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
    );
  }
  return (
    <div className="cowork-rag__tree">
      <div className="cowork-mailtab__head">
        <span className="cowork-mailtab__status">工作区文件</span>
        <button className="cowork-mailtab__refresh" type="button" onClick={onRefresh} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
      <ul className="cowork-today__list">
        {data.entries.slice(0, 100).map((e, i) => (
          <li
            key={i}
            className="cowork-today__row"
            onClick={() => !e.isDir && onFileClick?.(e.name)}
            style={{ cursor: e.isDir ? "default" : "pointer" }}
            title={e.name}
          >
            {e.isDir ? <Folder size={13} /> : <FileText size={13} />}
            <span className="cowork-today__text">{e.name}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// --- RAG knowledge nav (mode="rag") ----------------------------------

function RagNavView({ data }: { data: RagNavData }) {
  const t = useT();
  if (data.loading) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }
  if (data.collections.length === 0) {
    return (
      <div className="cowork-dock__empty-state">
        <NetworkIcon size={22} />
        <p>{t("cowork.ragComingSoon") || "知识库为空"}</p>
        <span className="cowork-dock__empty-hint">导入文件后此处显示集合导航</span>
      </div>
    );
  }
  return (
    <div className="cowork-rag__body">
      <div className="cowork-dock__group">
        <h3 className="cowork-today__heading">
          <Folder size={12} />
          知识库集合
        </h3>
        <ul className="cowork-today__list">
          {data.collections.slice(0, 20).map((c, i) => (
            <li key={c.id ?? i} className="cowork-today__row" title={c.name}>
              <Folder size={13} />
              <span className="cowork-today__text">{c.name}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
