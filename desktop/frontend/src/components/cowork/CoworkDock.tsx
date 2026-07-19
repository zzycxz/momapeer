// CoworkDock is the cowork-mode right-side panel: a tabbed overview of today's
// events, the user's mailbox, and the active workspace's files. It mirrors the
// coding-mode WorkspacePanel in spirit but presents cowork-flavored content
// (calendar events, scheduled tasks, unread mail) instead of git changes.
//
// Tabs:
//   - 今日 (today): today's calendar events + upcoming scheduled tasks + a
//     compact mail preview (unread count + latest subject).
//   - 邮件 (mail): mailbox connection status + unread message list.
//   - 文件 (files): a flat file tree of the active workspace (cwd), so the user
//     can browse/peek project files without leaving cowork mode.
//
// When mode === "rag" (the user opened the knowledge base panel), the dock
// instead shows a knowledge nav: collections, top entities, recent relations.
// The graph canvas itself lives in RagPanel; this nav complements it.
//
// The dock is always rendered when rightDockOpen; App.tsx controls visibility
// via the workspacePanelRenderable prop on AppChrome. dockOnClose / dockOnToggle
// are wired so the dock's toolbar matches the coding-mode workbench-dock.

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
  RagEntityBrief,
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
  upcoming: TaskView[];
  loading: boolean;
  error?: string;
}

interface MailData {
  probe: MailProbeResult | null;
  loading: boolean;
  error?: string;
}

interface FilesData {
  entries: { name: string; isDir: boolean }[];
  loading: boolean;
  error?: string;
}

interface RagNavData {
  collections: RagCollectionView[];
  entities: RagEntityBrief[];
  loading: boolean;
}

export function CoworkDock({
  cwd,
  maximized,
  onClose,
  onToggleMaximized,
  mode = "default",
  onEntityClick,
  onFileClick,
}: CoworkDockProps) {
  const t = useT();
  const [tab, setTab] = useState<DockTab>("today");

  // --- today data ---
  const [today, setToday] = useState<TodayData>({ events: [], upcoming: [], loading: true });
  const refreshToday = useCallback(async () => {
    setToday((s) => ({ ...s, loading: true, error: undefined }));
    try {
      const [events, upcoming] = await Promise.all([
        (app as unknown as { ListTodayEvents?: () => Promise<CalendarEventView[]> }).ListTodayEvents?.() ??
          Promise.resolve([] as CalendarEventView[]),
        (app as unknown as { ListUpcomingScheduledTasks?: () => Promise<TaskView[]> }).ListUpcomingScheduledTasks?.() ??
          Promise.resolve([] as TaskView[]),
      ]);
      setToday({ events: events ?? [], upcoming: upcoming ?? [], loading: false });
    } catch (e) {
      setToday((s) => ({ ...s, loading: false, error: String(e) }));
    }
  }, []);

  // --- mail data ---
  const [mail, setMail] = useState<MailData>({ probe: null, loading: true });
  const refreshMail = useCallback(async () => {
    setMail({ probe: null, loading: true });
    try {
      const probe = await (app as unknown as { ProbeMail?: () => Promise<MailProbeResult> }).ProbeMail?.();
      setMail({ probe: probe ?? null, loading: false });
    } catch (e) {
      setMail({ probe: null, loading: false, error: String(e) });
    }
  }, []);

  // --- files data ---
  const [files, setFiles] = useState<FilesData>({ entries: [], loading: true });
  const refreshFiles = useCallback(async () => {
    if (!cwd) {
      setFiles({ entries: [], loading: false });
      return;
    }
    setFiles({ entries: [], loading: true });
    try {
      const entries = await (app as unknown as {
        ListWorkspaceDir?: (dir: string) => Promise<{ name: string; isDir: boolean }[]>;
      }).ListWorkspaceDir?.(cwd);
      setFiles({ entries: entries ?? [], loading: false });
    } catch (e) {
      setFiles({ entries: [], loading: false, error: String(e) });
    }
  }, [cwd]);

  // --- rag nav data ---
  const [ragNav, setRagNav] = useState<RagNavData>({ collections: [], entities: [], loading: true });
  const refreshRagNav = useCallback(async () => {
    setRagNav({ collections: [], entities: [], loading: true });
    try {
      const [cols, ents] = await Promise.all([
        (app as unknown as { ListRagCollections?: () => Promise<RagCollectionView[]> }).ListRagCollections?.() ??
          Promise.resolve([] as RagCollectionView[]),
        (app as unknown as { ListRagTopEntities?: () => Promise<RagEntityBrief[]> }).ListRagTopEntities?.() ??
          Promise.resolve([] as RagEntityBrief[]),
      ]);
      setRagNav({ collections: cols ?? [], entities: ents ?? [], loading: false });
    } catch (e) {
      setRagNav({ collections: [], entities: [], loading: false });
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

  // When cwd changes, refresh files.
  useEffect(() => {
    if (mode !== "rag") void refreshFiles();
  }, [cwd, mode, refreshFiles]);

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
        <button
          className="cowork-dock__tab"
          type="button"
          onClick={onClose}
          title="关闭"
          aria-label="关闭"
        >
          ✕
        </button>
      </div>

      <div className="cowork-dock__body">
        {mode === "rag" ? (
          <RagNavView data={ragNav} onEntityClick={onEntityClick} />
        ) : tab === "today" ? (
          <TodayView data={today} />
        ) : tab === "mail" ? (
          <MailView data={mail} onRefresh={() => void refreshMail()} />
        ) : (
          <FilesView data={files} cwd={cwd} onRefresh={() => void refreshFiles()} onFileClick={onFileClick} />
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
  const hasUpcoming = data.upcoming.length > 0;
  if (!hasEvents && !hasUpcoming) {
    return (
      <div className="cowork-dock__empty-state">
        <CalendarDays size={22} />
        <p>{t("coworkDock.noEvents") || "今日暂无日程"}</p>
        <span className="cowork-dock__empty-hint">{t("coworkDock.noUpcoming") || "暂无待触发任务"}</span>
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
                <span className="cowork-today__time">{formatEventTime(e)}</span>
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
            {data.upcoming.slice(0, 5).map((task, i) => (
              <li key={task.id ?? i} className="cowork-today__row" title={task.name}>
                <span className="cowork-today__time">{task.nextRun ?? ""}</span>
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

function formatEventTime(e: CalendarEventView): string {
  if (e.startMs && typeof e.startMs === "number") {
    const d = new Date(e.startMs);
    return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  }
  return e.startTime ?? "";
}

// --- 邮件 view: mailbox connection status ------------------------------

function MailView({ data, onRefresh }: { data: MailData; onRefresh: () => void }) {
  const t = useT();
  if (data.loading) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }
  const probe = data.probe;
  if (!probe || !probe.connected) {
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
        <span className="cowork-mailtab__status">{t("coworkDock.mailConnected") || "已连接"}</span>
        <button className="cowork-mailtab__refresh" type="button" onClick={onRefresh} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
      <div className="cowork-today__section">
        <h3 className="cowork-today__heading">
          <Inbox size={12} />
          {probe.unread && probe.unread > 0
            ? t("coworkDock.unreadN", { n: probe.unread }) || `${probe.unread} 封未读`
            : t("coworkDock.noUnread") || "无未读"}
        </h3>
      </div>
      {probe.recent && probe.recent.length > 0 ? (
        <ul className="cowork-mailtab__list">
          {probe.recent.slice(0, 10).map((m, i) => (
            <li key={i} className={`cowork-mailtab__item ${m.unread ? "" : "cowork-mailtab__item--open"}`}>
              <div className="cowork-mailtab__item-head">
                <span className="cowork-mailtab__from">{m.from}</span>
                <span className="cowork-mailtab__date">{m.date}</span>
              </div>
              <div className="cowork-mailtab__subject">{m.subject}</div>
              {m.preview && <div className="cowork-mailtab__preview">{m.preview}</div>}
            </li>
          ))}
        </ul>
      ) : (
        <div className="cowork-dock__empty-hint">{t("coworkDock.noUnreadMail") || "没有未读邮件"}</div>
      )}
    </div>
  );
}

// --- 文件 view: workspace file list -----------------------------------

function FilesView({
  data,
  cwd,
  onRefresh,
  onFileClick,
}: {
  data: FilesData;
  cwd?: string;
  onRefresh: () => void;
  onFileClick?: (path: string) => void;
}) {
  const t = useT();
  if (!cwd) {
    return (
      <div className="cowork-dock__empty-state">
        <Folder size={22} />
        <p>{t("coworkDock.noWorkspace") || "当前会话未关联工作区文件夹"}</p>
        <span className="cowork-dock__empty-hint">
          {t("coworkDock.noWorkspaceHint") || "新建会话时选择一个项目文件夹即可在此浏览文件"}
        </span>
      </div>
    );
  }
  if (data.loading) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }
  if (data.entries.length === 0) {
    return (
      <div className="cowork-dock__empty-state">
        <Folder size={22} />
        <p>空文件夹</p>
        <button className="cowork-dock__tab" type="button" onClick={onRefresh} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
    );
  }
  return (
    <div className="cowork-rag__tree">
      <div className="cowork-mailtab__head">
        <span className="cowork-mailtab__status">{cwd}</span>
        <button className="cowork-mailtab__refresh" type="button" onClick={onRefresh} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
      <ul className="cowork-today__list">
        {data.entries.slice(0, 100).map((e, i) => (
          <li
            key={i}
            className="cowork-today__row"
            onClick={() => !e.isDir && onFileClick?.(`${cwd}/${e.name}`)}
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

function RagNavView({
  data,
  onEntityClick,
}: {
  data: RagNavData;
  onEntityClick?: (name: string) => void;
}) {
  const t = useT();
  if (data.loading) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }
  if (data.collections.length === 0 && data.entities.length === 0) {
    return (
      <div className="cowork-dock__empty-state">
        <NetworkIcon size={22} />
        <p>{t("cowork.ragComingSoon") || "知识库为空"}</p>
        <span className="cowork-dock__empty-hint">导入文件后此处显示实体导航</span>
      </div>
    );
  }
  return (
    <div className="cowork-rag__body">
      {data.collections.length > 0 && (
        <div className="cowork-dock__group">
          <h3 className="cowork-today__heading">
            <Folder size={12} />
            知识库集合
          </h3>
          <ul className="cowork-today__list">
            {data.collections.slice(0, 15).map((c, i) => (
              <li key={c.id ?? i} className="cowork-today__row" title={c.name}>
                <Folder size={13} />
                <span className="cowork-today__text">{c.name}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {data.entities.length > 0 && (
        <div className="cowork-dock__group">
          <h3 className="cowork-today__heading">
            <NetworkIcon size={12} />
            热门实体
          </h3>
          <ul className="cowork-today__list">
            {data.entities.slice(0, 20).map((e, i) => (
              <li
                key={i}
                className="cowork-today__row"
                onClick={() => onEntityClick?.(e.name)}
                style={{ cursor: "pointer" }}
                title={e.desc || e.name}
              >
                <Circle size={10} />
                <span className="cowork-today__text">{e.name}</span>
                {e.desc && <span className="cowork-today__time">{e.desc.slice(0, 30)}</span>}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
