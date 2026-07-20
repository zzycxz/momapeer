// CoworkDock is the cowork-mode right-side panel.
//
// Two modes, both rendered with the coding-mode workbench-dock tab strip
// classes (workbench-dock__tools / __tabs / __tab) so the styling reads as
// the same control as the coding-mode right dock:
//   - mode="default" (classic cowork dock): 3 tabs — 今日 / 邮件 / 文件.
//   - mode="rag" (knowledge base navigation): 4 tabs — 集合 / 实体 / 文件 / 提取.
//
// Sub-views (EntityDetail / DocPreview) replace the __body content with a
// "返回" affordance. RAG mode also listens to the rag:entity-click event so
// that clicks on the graph canvas (which emits the event with the node's own
// collection) open the entity detail directly in the dock.
//
// Backend methods (all wrapped with .catch fallbacks since the backend may
// not implement every one yet):
//   - ListCalendarEvents(since, before string) → []CalendarEventView
//   - ListScheduledTasks() → []TaskView
//   - ProbeMailAccount() → MailProbeResult
//   - InboxPreview(n) → []InboxItem     (fallback: [] if undefined)
//   - ListRagCollections() → []RagCollectionView
//   - GetSessionCollections() → []string
//   - SetSessionCollections([]string) → void
//   - ListRagTree(collection) → []RagNodeView
//   - RagStartExtract(collection, path)
//   - RagCancelExtract(jobId)
//   - RagRemovePath(collection, path)
//   - GetTopEntities(collection, n) → GraphDataView
//   - GetEntityDetail(collection, name) → EntityDetailView

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  CalendarDays,
  Circle,
  FileText,
  Folder,
  FolderOpen,
  Mail,
  Network as NetworkIcon,
  Plus,
  RefreshCw,
  Search,
  Trash2,
  Zap,
} from "lucide-react";

import { app, onCalendarChanged, onFilesDropped, onRagChanged, onSchedulerChanged } from "../../lib/bridge";
import type {
  CalendarEventView,
  GraphDataView,
  GraphNodeView,
  InboxItem,
  MailProbeResult,
  RagCollectionView,
  RagNodeView,
  TaskView,
} from "../../lib/types";
import { useT } from "../../lib/i18n";
import { useToast } from "../../lib/toast";
import { ENTITY_TYPES, ENTITY_TYPE_LABELS, colorFor } from "./entityTypes";
import { TemplateSelect } from "./TemplateSelect";
import { EntityDetail } from "./EntityDetail";
import { DocPreview } from "./DocPreview";
import { RagNode } from "./RagNode";

// Default-mode tabs: 今日 / 邮件 / 文件.
type DockTab = "today" | "mail" | "files";

// RAG-mode tabs: 集合 / 实体 / 文件 / 提取.
type RagTab = "collections" | "entities" | "files" | "extract";

export interface CoworkDockProps {
  cwd?: string;
  maximized: boolean;
  onClose: () => void;
  onToggleMaximized: () => void;
  mode?: "default" | "rag";
  onEntityClick?: (name: string) => void;
  onFileClick?: (path: string) => void;
}

// --- helpers ----------------------------------------------------------------

// todayRange returns {since, before} ISO-ish strings for the local today
// (00:00 → 23:59), matching ListCalendarEvents's "2006-01-02T15:04" format.
function todayRange(): { since: string; before: string } {
  const d = new Date();
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return { since: `${y}-${m}-${day}T00:00`, before: `${y}-${m}-${day}T23:59` };
}

// formatEventTime extracts "HH:MM" from "2006-01-02T15:04" (or "" on parse fail).
function formatEventTime(s: string): string {
  if (!s) return "";
  const m = s.match(/T(\d{2}:\d{2})/);
  return m ? m[1] : "";
}

// shortTime renders a nextRun timestamp as a compact "MM-DD HH:MM".
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

// fetchInbox safely reads the most recent unread mail. InboxPreview may not
// exist on older backends, so we fall back to an empty list (never throw).
async function fetchInbox(limit: number): Promise<InboxItem[]> {
  try {
    const fn = (app as unknown as { InboxPreview?: (n: number) => Promise<InboxItem[]> }).InboxPreview;
    if (typeof fn === "function") {
      return (await fn(limit)) ?? [];
    }
  } catch {
    /* fall through */
  }
  return [];
}

// ===========================================================================
// CoworkDock — top-level component
// ===========================================================================

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
  const isRag = mode === "rag";

  // Default mode: active tab among {today, mail, files}.
  const [tab, setTab] = useState<DockTab>("today");
  // RAG mode: active tab among {collections, entities, files, extract}.
  const [ragTab, setRagTab] = useState<RagTab>("collections");

  // --- today/mail shared data ---
  const [events, setEvents] = useState<CalendarEventView[]>([]);
  const [tasks, setTasks] = useState<TaskView[]>([]);
  const [mailProbe, setMailProbe] = useState<MailProbeResult | null>(null);
  const [inbox, setInbox] = useState<InboxItem[]>([]);
  const [todayLoading, setTodayLoading] = useState(true);
  const [mailLoading, setMailLoading] = useState(true);

  // --- RAG data ---
  const [collections, setCollections] = useState<RagCollectionView[]>([]);
  const [collectionsLoading, setCollectionsLoading] = useState(true);
  const [collectionsError, setCollectionsError] = useState<string | null>(null);
  const [activeCollection, setActiveCollection] = useState<string>("");
  const [sessionCollections, setSessionCollections] = useState<string[]>([]);

  // Sub-view navigation (entity detail / doc preview) — replaces body.
  const [entityName, setEntityName] = useState<string>("");
  const [docPath, setDocPath] = useState<string>("");

  // --- data refreshers ---
  const refreshToday = useCallback(async () => {
    setTodayLoading(true);
    try {
      const { since, before } = todayRange();
      const [evs, tks, probe, inb] = await Promise.all([
        app.ListCalendarEvents(since, before).catch(() => [] as CalendarEventView[]),
        app.ListScheduledTasks().catch(() => [] as TaskView[]),
        app.ProbeMailAccount().catch(() => ({ ok: false, status: "error", message: "" } as MailProbeResult)),
        fetchInbox(50),
      ]);
      // Filter events to today only (server may return a wider window).
      const todayStr = new Date().toISOString().slice(0, 10);
      const todays = (evs ?? []).filter((e) => (e.start || "").slice(0, 10) === todayStr);
      todays.sort((a, b) => (a.start || "").localeCompare(b.start || ""));
      setEvents(todays);
      setTasks(tks ?? []);
      setMailProbe(probe);
      setInbox(inb);
    } catch {
      setEvents([]);
      setTasks([]);
      setMailProbe(null);
      setInbox([]);
    } finally {
      setTodayLoading(false);
    }
  }, []);

  const refreshMail = useCallback(async () => {
    setMailLoading(true);
    try {
      const [probe, inb] = await Promise.all([
        app.ProbeMailAccount().catch(() => ({ ok: false, status: "error", message: "" } as MailProbeResult)),
        fetchInbox(30),
      ]);
      setMailProbe(probe);
      setInbox(inb);
    } catch {
      setMailProbe(null);
      setInbox([]);
    } finally {
      setMailLoading(false);
    }
  }, []);

  const refreshCollections = useCallback(async () => {
    setCollectionsLoading(true);
    setCollectionsError(null);
    try {
      const [cols, sess] = await Promise.all([
        app.ListRagCollections().catch(() => [] as RagCollectionView[]),
        (app as unknown as { GetSessionCollections?: () => Promise<string[]> })
          .GetSessionCollections?.().catch(() => [] as string[]) ?? Promise.resolve([] as string[]),
      ]);
      setCollections(cols ?? []);
      setSessionCollections(sess ?? []);
    } catch (e) {
      setCollections([]);
      setSessionCollections([]);
      setCollectionsError(String(e));
    } finally {
      setCollectionsLoading(false);
    }
  }, []);

  // Default to the first collection once loaded, if none is active.
  useEffect(() => {
    if (!activeCollection && collections.length > 0) {
      setActiveCollection(collections[0].name);
    }
  }, [collections, activeCollection]);

  // Initial load + live refresh subscriptions.
  useEffect(() => {
    if (isRag) {
      void refreshCollections();
      return onRagChanged(() => void refreshCollections());
    }
    void refreshToday();
    const off1 = onCalendarChanged(() => void refreshToday());
    const off2 = onSchedulerChanged(() => void refreshToday());
    return () => {
      off1();
      off2();
    };
  }, [isRag, refreshToday, refreshCollections]);

  // rag:entity-click → open entity detail in dock (graph click → dock).
  useEffect(() => {
    if (!isRag) return;
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<{ name?: string; collection?: string }>).detail || {};
      if (detail.name) {
        if (detail.collection) setActiveCollection(detail.collection);
        setEntityName(detail.name);
        setRagTab("entities");
      }
    };
    window.addEventListener("rag:entity-click", handler);
    return () => window.removeEventListener("rag:entity-click", handler);
  }, [isRag]);

  // Reset sub-views when switching tabs (back to the tab list).
  useEffect(() => {
    setEntityName("");
    setDocPath("");
  }, [ragTab, tab]);

  // Shared maximized/close affordances in the trailing slot of the tab strip.
  const trailingButtons = (
    <>
      <button
        className="workbench-dock__tab"
        type="button"
        onClick={onToggleMaximized}
        title={maximized ? "还原" : "最大化"}
        aria-label={maximized ? "还原" : "最大化"}
      >
        {maximized ? "❐" : "▢"}
      </button>
      <button
        className="workbench-dock__tab"
        type="button"
        onClick={onClose}
        title="关闭"
        aria-label="关闭"
      >
        ✕
      </button>
    </>
  );

  // --- RAG mode render ---
  if (isRag) {
    return (
      <aside className="cowork-dock" aria-label={t("coworkDock.label") || "知识库导航"}>
        <div className="workbench-dock__tools">
          <div className="workbench-dock__tabs" role="tablist">
            <button
              className={`workbench-dock__tab${ragTab === "collections" ? " workbench-dock__tab--active" : ""}`}
              type="button"
              onClick={() => setRagTab("collections")}
              title="集合"
            >
              <Folder size={13} />
              <span className="workbench-dock__tab-label">集合</span>
            </button>
            <button
              className={`workbench-dock__tab${ragTab === "entities" ? " workbench-dock__tab--active" : ""}`}
              type="button"
              onClick={() => setRagTab("entities")}
              title="实体"
            >
              <NetworkIcon size={13} />
              <span className="workbench-dock__tab-label">实体</span>
            </button>
            <button
              className={`workbench-dock__tab${ragTab === "files" ? " workbench-dock__tab--active" : ""}`}
              type="button"
              onClick={() => setRagTab("files")}
              title="文件"
            >
              <FileText size={13} />
              <span className="workbench-dock__tab-label">文件</span>
            </button>
            <button
              className={`workbench-dock__tab${ragTab === "extract" ? " workbench-dock__tab--active" : ""}`}
              type="button"
              onClick={() => setRagTab("extract")}
              title="深度提取"
            >
              <Zap size={13} />
              <span className="workbench-dock__tab-label">提取</span>
            </button>
          </div>
          {trailingButtons}
        </div>

        <div className="cowork-dock__body">
          {entityName ? (
            <EntityDetail
              collection={activeCollection}
              entityName={entityName}
              onBack={() => setEntityName("")}
              onHighlightInGraph={(name) => onEntityClick?.(name)}
              onNavigatePeer={(name) => setEntityName(name)}
            />
          ) : docPath ? (
            <DocPreview
              collection={activeCollection}
              docPath={docPath}
              onBack={() => setDocPath("")}
            />
          ) : ragTab === "extract" ? (
            <TemplateSelect collection={activeCollection} onBack={() => setRagTab("entities")} />
          ) : ragTab === "collections" ? (
            <CollectionsView
              collections={collections}
              loading={collectionsLoading}
              error={collectionsError}
              activeCollection={activeCollection}
              sessionCollections={sessionCollections}
              onSelect={(name) => {
                setActiveCollection(name);
                window.dispatchEvent(
                  new CustomEvent("rag:active-collection", { detail: { collection: name } }),
                );
              }}
              onToggleSession={(name, on) => {
                const next = on
                  ? Array.from(new Set([...sessionCollections, name]))
                  : sessionCollections.filter((x) => x !== name);
                setSessionCollections(next);
                void (app as unknown as { SetSessionCollections?: (c: string[]) => Promise<void> })
                  .SetSessionCollections?.(next).catch(() => {});
              }}
              onRefresh={() => void refreshCollections()}
              onEnterEntities={(name) => {
                setActiveCollection(name);
                setRagTab("entities");
              }}
            />
          ) : ragTab === "entities" ? (
            <EntitiesView
              collection={activeCollection}
              sessionCollections={sessionCollections}
              collections={collections}
              onOpenEntity={(name) => setEntityName(name)}
              onEntityClick={onEntityClick}
            />
          ) : (
            <FilesView
              collection={activeCollection}
              onOpenFile={(path) => setDocPath(path)}
              onFileClick={onFileClick}
            />
          )}
        </div>
      </aside>
    );
  }

  // --- default mode render (today / mail / files) ---
  return (
    <aside className="cowork-dock" aria-label={t("coworkDock.label") || "办公概览"}>
      <div className="workbench-dock__tools">
        <div className="workbench-dock__tabs" role="tablist">
          <button
            className={`workbench-dock__tab${tab === "today" ? " workbench-dock__tab--active" : ""}`}
            type="button"
            onClick={() => {
              setTab("today");
              void refreshToday();
            }}
            title={t("coworkDock.today") || "今日"}
          >
            <CalendarDays size={13} />
            <span className="workbench-dock__tab-label">{t("coworkDock.today") || "今日"}</span>
          </button>
          <button
            className={`workbench-dock__tab${tab === "mail" ? " workbench-dock__tab--active" : ""}`}
            type="button"
            onClick={() => {
              setTab("mail");
              void refreshMail();
            }}
            title={t("coworkDock.mail") || "邮件"}
          >
            <Mail size={13} />
            <span className="workbench-dock__tab-label">{t("coworkDock.mail") || "邮件"}</span>
          </button>
          <button
            className={`workbench-dock__tab${tab === "files" ? " workbench-dock__tab--active" : ""}`}
            type="button"
            onClick={() => setTab("files")}
            title={t("coworkDock.files") || "文件"}
          >
            <FileText size={13} />
            <span className="workbench-dock__tab-label">{t("coworkDock.files") || "文件"}</span>
          </button>
        </div>
        {trailingButtons}
      </div>

      <div className="cowork-dock__body">
        {tab === "today" ? (
          <TodayView
            events={events}
            tasks={tasks}
            mailProbe={mailProbe}
            unreadCount={inbox.length}
            loading={todayLoading}
          />
        ) : tab === "mail" ? (
          <MailView probe={mailProbe} inbox={inbox} loading={mailLoading} onRefresh={() => void refreshMail()} />
        ) : (
          <FilesViewDefault cwd={cwd} onFileClick={onFileClick} />
        )}
      </div>
    </aside>
  );
}

// ===========================================================================
// TodayView — 今日日程 / 邮箱 / 自动化
// ===========================================================================

function TodayView({
  events,
  tasks,
  mailProbe,
  unreadCount,
  loading,
}: {
  events: CalendarEventView[];
  tasks: TaskView[];
  mailProbe: MailProbeResult | null;
  unreadCount: number;
  loading: boolean;
}) {
  const t = useT();
  if (loading) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }

  // Upcoming tasks: enabled + nextRun in the future, first 3.
  const now = Date.now();
  const upcoming = tasks
    .filter((tk) => tk.enabled && tk.nextRun && new Date(tk.nextRun).getTime() > now)
    .sort((a, b) => (a.nextRun || "").localeCompare(b.nextRun || ""))
    .slice(0, 3);

  const mailOk = mailProbe?.ok || mailProbe?.status === "ok";
  const hasAny = events.length > 0 || upcoming.length > 0 || mailProbe;

  if (!hasAny) {
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
      {events.length > 0 && (
        <div className="cowork-today__section">
          <h3 className="cowork-today__heading">
            <CalendarDays size={12} />
            {t("coworkDock.todayEvents") || "今日日程"}
          </h3>
          <ul className="cowork-today__list">
            {events.slice(0, 8).map((e, i) => (
              <li key={e.id ?? i} className="cowork-today__row" title={e.title}>
                <span className="cowork-today__time">{formatEventTime(e.start)}</span>
                <span className="cowork-today__dot" />
                <span className="cowork-today__text">{e.title}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {mailProbe && (
        <div className="cowork-today__section">
          <h3 className="cowork-today__heading">
            <Mail size={12} />
            {t("coworkDock.mailbox") || "邮箱"}
          </h3>
          <div className="cowork-today__mailrow">
            <span
              className="cowork-today__dot"
              style={{
                background: mailOk ? "var(--ok, #3fb950)" : "var(--danger, #f85149)",
                border: 0,
              }}
            />
            <span className="cowork-today__mailtext">
              {mailOk
                ? unreadCount > 0
                  ? t("coworkDock.unreadN").replace("{n}", String(unreadCount)) || `${unreadCount} 封未读`
                  : t("coworkDock.noUnread") || "无未读"
                : t("coworkDock.mailError") || "连接失败"}
            </span>
          </div>
        </div>
      )}

      {upcoming.length > 0 && (
        <div className="cowork-today__section">
          <h3 className="cowork-today__heading">
            <Circle size={12} />
            {t("coworkDock.upcoming") || "自动化"}
          </h3>
          <ul className="cowork-today__list">
            {upcoming.map((task, i) => (
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

// ===========================================================================
// MailView — 邮件列表 + probe status
// ===========================================================================

function MailView({
  probe,
  inbox,
  loading,
  onRefresh,
}: {
  probe: MailProbeResult | null;
  inbox: InboxItem[];
  loading: boolean;
  onRefresh: () => void;
}) {
  const t = useT();
  const [openIdx, setOpenIdx] = useState<number | null>(null);

  if (loading) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }

  const ok = probe?.ok || probe?.status === "ok";
  if (!probe || probe.status === "unconfigured") {
    return (
      <div className="cowork-dock__empty-state">
        <Mail size={22} />
        <p>{t("coworkDock.mailUnconfigured") || "未配置"}</p>
        <span className="cowork-dock__empty-hint">
          {t("coworkDock.mailConfigureHint") || "请在「设置」-「办公」中配置邮箱"}
        </span>
        <button className="workbench-dock__tab" type="button" onClick={onRefresh} title="重试">
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

      {!ok && probe.message && (
        <div className="cowork-today__section">
          <p className="cowork-dock__empty-hint">{probe.message}</p>
        </div>
      )}

      {ok && inbox.length === 0 ? (
        <div className="cowork-dock__empty-state">
          <Mail size={22} />
          <p>{t("coworkDock.noUnreadMail") || "没有未读邮件"}</p>
        </div>
      ) : (
        <div className="cowork-mailtab__list">
          {inbox.map((m, i) => {
            const open = openIdx === i;
            return (
              <div
                key={i}
                className={`cowork-mailtab__item${open ? " cowork-mailtab__item--open" : ""}`}
                onClick={() => setOpenIdx(open ? null : i)}
              >
                <div className="cowork-mailtab__item-head">
                  <span className="cowork-mailtab__from">{m.from || "(unknown)"}</span>
                  <span className="cowork-mailtab__date">{shortTime(m.date) || m.date}</span>
                </div>
                <div className="cowork-mailtab__subject">{m.subject || "(no subject)"}</div>
                {open && m.preview && <div className="cowork-mailtab__preview">{m.preview}</div>}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

// ===========================================================================
// FilesViewDefault — default-mode "文件" tab: reuse WorkspacePanel
// ===========================================================================

function FilesViewDefault({
  cwd,
  onFileClick,
}: {
  cwd?: string;
  onFileClick?: (path: string) => void;
}) {
  const t = useT();
  // Lazily import WorkspacePanel to avoid a static circular-import risk at
  // module load (WorkspacePanel pulls in many coding-mode dependencies).
  const [Panel, setPanel] = useState<React.ComponentType<Record<string, unknown>> | null>(null);
  useEffect(() => {
    let mounted = true;
    void import("../WorkspacePanel").then((mod) => {
      if (mounted) setPanel(() => mod.WorkspacePanel as React.ComponentType<Record<string, unknown>>);
    }).catch(() => { /* leave Panel null → empty state */ });
    return () => { mounted = false; };
  }, []);

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
  if (!Panel) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }
  return (
    <Panel
      open
      cwd={cwd}
      maximized
      onClose={() => onFileClick?.("")}
      onToggleMaximized={() => {}}
      showViewTabs={false}
      initialViewMode="files"
    />
  );
}

// ===========================================================================
// CollectionsView — RAG "集合" tab
// ===========================================================================

function CollectionsView({
  collections,
  loading,
  error,
  activeCollection,
  sessionCollections,
  onSelect,
  onToggleSession,
  onRefresh,
  onEnterEntities,
}: {
  collections: RagCollectionView[];
  loading: boolean;
  error: string | null;
  activeCollection: string;
  sessionCollections: string[];
  onSelect: (name: string) => void;
  onToggleSession: (name: string, on: boolean) => void;
  onRefresh: () => void;
  onEnterEntities: (name: string) => void;
}) {
  const t = useT();
  const { showToast } = useToast();

  if (loading) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }
  if (error) {
    return (
      <div className="cowork-dock__empty-state">
        <NetworkIcon size={22} />
        <p>{t("coworkDock.loadFailed") || "加载失败"}</p>
        <span className="cowork-dock__empty-hint">{error}</span>
        <button className="workbench-dock__tab" type="button" onClick={onRefresh} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
    );
  }
  if (collections.length === 0) {
    return (
      <div className="cowork-dock__empty-state">
        <NetworkIcon size={22} />
        <p>{t("cowork.ragComingSoon") || "知识库为空"}</p>
        <span className="cowork-dock__empty-hint">
          {t("coworkDock.collectionsEmptyHint") || "导入文件后此处显示集合导航"}
        </span>
        <button className="workbench-dock__tab" type="button" onClick={onRefresh} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
    );
  }

  const selectAll = () => collections.forEach((c) => onToggleSession(c.name, true));
  const selectNone = () => collections.forEach((c) => onToggleSession(c.name, false));

  const handleCreate = async () => {
    const name = window.prompt(t("coworkDock.createCollectionPrompt") || "输入新集合名称：", "");
    if (!name) return;
    try {
      const picked = await app.PickWorkspace().catch(() => "");
      if (!picked) return;
      const res = await app.RagImportPaths(name, [picked]);
      showToast(res.message || `集合 ${name} 已创建`, "info");
      onRefresh();
    } catch (e) {
      showToast(`${t("coworkDock.createFailed") || "创建失败"}：${String(e)}`, "error");
    }
  };

  const handleDelete = async (name: string) => {
    if (!window.confirm(t("coworkDock.confirmDelete") || `确认清理集合「${name}」的知识？文档不会被删除。`)) return;
    try {
      await app.RagCleanCollection(name);
      showToast(`已清理 ${name}`, "info");
      onRefresh();
    } catch (e) {
      showToast(`${t("coworkDock.deleteFailed") || "清理失败"}：${String(e)}`, "error");
    }
  };

  return (
    <div className="rag-dock__collections">
      <div className="rag-dock__collection-header">
        <span className="rag-dock__collection-title">激活集合</span>
        <div className="rag-dock__collection-actions">
          <button className="rag-dock__collection-action" type="button" onClick={selectAll}>全选</button>
          <button className="rag-dock__collection-action" type="button" onClick={selectNone}>全不选</button>
          <button className="rag-dock__collection-action" type="button" onClick={() => void handleCreate()} title="新建集合">
            <Plus size={11} />
          </button>
          <button className="rag-dock__collection-action" type="button" onClick={onRefresh} title="刷新">
            <RefreshCw size={11} />
          </button>
        </div>
      </div>

      {collections.map((c) => {
        const active = c.name === activeCollection;
        const checked = sessionCollections.includes(c.name);
        return (
          <label
            key={c.name}
            className={`rag-dock__collection-item${active ? " rag-dock__collection-item--active" : ""}`}
          >
            <input
              type="checkbox"
              checked={checked}
              onChange={(e) => {
                onToggleSession(c.name, e.target.checked);
                onSelect(c.name);
              }}
            />
            <span
              className="rag-dock__collection-name"
              onClick={() => onEnterEntities(c.name)}
              style={{ cursor: "pointer" }}
            >
              {active ? <FolderOpen size={12} style={{ marginRight: 4, verticalAlign: "-1px" }} /> : null}
              {c.name}
            </span>
            <span className="rag-dock__collection-stats">
              {c.entities} 实体 · {c.documents} 文档
            </span>
            <button
              type="button"
              className="rag-dock__collection-action"
              title={t("cowork.ragRemove") || "清理"}
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                void handleDelete(c.name);
              }}
            >
              <Trash2 size={11} />
            </button>
          </label>
        );
      })}
    </div>
  );
}

// ===========================================================================
// EntitiesView — RAG "实体" tab
// ===========================================================================

function EntitiesView({
  collection,
  collections,
  sessionCollections,
  onOpenEntity,
  onEntityClick,
}: {
  collection: string;
  collections: RagCollectionView[];
  sessionCollections: string[];
  onOpenEntity: (name: string) => void;
  onEntityClick?: (name: string) => void;
}) {
  const t = useT();
  const [data, setData] = useState<GraphDataView | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [filterTypes, setFilterTypes] = useState<string[]>([]);

  // Scope: union of the active collection (if any) and all session-checked
  // collections. The graph API takes a single collection, so we query each
  // and merge. If neither is set we fall back to the first collection.
  const scopedNames = useMemo(() => {
    const names = new Set<string>();
    if (collection) names.add(collection);
    for (const n of sessionCollections) names.add(n);
    return Array.from(names);
  }, [collection, sessionCollections]);

  const refresh = useCallback(async () => {
    const targets = scopedNames.length > 0 ? scopedNames : collections.map((c) => c.name);
    if (targets.length === 0) {
      setData(null);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const results = await Promise.all(
        targets.map((n) => app.GetTopEntities(n, 200).catch(() => null)),
      );
      const merged: GraphDataView = { nodes: [], edges: [] };
      for (const r of results) {
        if (!r) continue;
        merged.nodes.push(...(r.nodes ?? []));
        merged.edges.push(...(r.edges ?? []));
      }
      // De-duplicate nodes by id (entity may appear in multiple collections).
      const seen = new Set<string>();
      merged.nodes = merged.nodes.filter((n) => {
        if (seen.has(n.id)) return false;
        seen.add(n.id);
        return true;
      });
      setData(merged);
    } catch (e) {
      setData(null);
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }, [scopedNames, collections]);

  useEffect(() => {
    void refresh();
    return onRagChanged(() => void refresh());
  }, [refresh]);

  const filtered = useMemo(() => {
    const nodes = data?.nodes ?? [];
    const q = query.trim().toLowerCase();
    return nodes.filter((n) => {
      if (filterTypes.length > 0 && !filterTypes.includes(n.type)) return false;
      if (q && !(`${n.label} ${n.description}`.toLowerCase().includes(q))) return false;
      return true;
    });
  }, [data, query, filterTypes]);

  if (scopedNames.length === 0 && collections.length === 0) {
    return (
      <div className="cowork-dock__empty-state">
        <NetworkIcon size={22} />
        <p>{t("coworkDock.selectCollectionFirst") || "请先选择一个集合"}</p>
        <span className="cowork-dock__empty-hint">
          {t("coworkDock.selectCollectionHint") || "在「集合」tab 中选择或创建一个集合"}
        </span>
      </div>
    );
  }
  if (loading) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }
  if (error) {
    return (
      <div className="cowork-dock__empty-state">
        <NetworkIcon size={22} />
        <p>{t("coworkDock.loadFailed") || "加载失败"}</p>
        <span className="cowork-dock__empty-hint">{error}</span>
        <button className="workbench-dock__tab" type="button" onClick={() => void refresh()} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
    );
  }

  const toggleType = (key: string) =>
    setFilterTypes((cur) => (cur.includes(key) ? cur.filter((x) => x !== key) : [...cur, key]));

  return (
    <div className="rag-dock__entities">
      <div className="cowork-rag__search">
        <Search size={13} className="cowork-rag__search-icon" />
        <input
          className="cowork-rag__search-input"
          type="text"
          placeholder={t("coworkDock.searchEntities") || "搜索实体…"}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <button className="cowork-mailtab__refresh" type="button" onClick={() => void refresh()} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>

      <div className="cowork-rag__entity" style={{ flexWrap: "wrap", gap: 4 }}>
        {ENTITY_TYPES.map((def) => {
          const on = filterTypes.includes(def.key);
          return (
            <button
              key={def.key}
              type="button"
              className={`workbench-dock__tab${on ? " workbench-dock__tab--active" : ""}`}
              onClick={() => toggleType(def.key)}
              style={{
                fontSize: 10.5,
                padding: "1px 6px",
                height: 18,
                color: on ? def.color : undefined,
                borderColor: on ? def.color : undefined,
              }}
              title={def.label}
            >
              {def.label}
            </button>
          );
        })}
        {filterTypes.length > 0 && (
          <button
            type="button"
            className="workbench-dock__tab"
            onClick={() => setFilterTypes([])}
            style={{ fontSize: 10.5, padding: "1px 6px", height: 18 }}
          >
            ✕
          </button>
        )}
      </div>

      {filtered.length === 0 ? (
        <div className="cowork-dock__empty-state">
          <NetworkIcon size={22} />
          <p>{(data?.nodes ?? []).length === 0 ? (t("coworkDock.noEntities") || "暂无实体") : (t("coworkDock.noMatch") || "无匹配实体")}</p>
          <span className="cowork-dock__empty-hint">
            {(data?.nodes ?? []).length === 0
              ? (t("coworkDock.runExtractHint") || "在「提取」tab 中运行深度提取以生成实体")
              : ""}
          </span>
        </div>
      ) : (
        <ul className="cowork-today__list">
          {filtered.map((node) => (
            <EntityRow
              key={node.id}
              node={node}
              onOpen={() => onOpenEntity(node.id)}
              onClick={() => onEntityClick?.(node.id)}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function EntityRow({
  node,
  onOpen,
  onClick,
}: {
  node: GraphNodeView;
  onOpen: () => void;
  onClick?: () => void;
}) {
  const label = ENTITY_TYPE_LABELS[node.type] ?? node.type;
  const color = colorFor(node.type);
  return (
    <li
      className="cowork-today__row cowork-rag__entity"
      style={{ cursor: "pointer", alignItems: "center" }}
      title={node.description || node.label}
      onClick={() => {
        onClick?.();
        onOpen();
      }}
    >
      <span
        style={{
          flex: "0 0 auto",
          width: 8,
          height: 8,
          borderRadius: "50%",
          background: color,
          display: "inline-block",
        }}
      />
      <span
        className="cowork-rag__entity-name"
        style={{ flex: "1 1 auto", minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}
      >
        {node.label}
      </span>
      <span className="cowork-dock__empty-hint" style={{ margin: 0, fontSize: 10.5 }}>
        {label}
        {node.relationCnt > 0 ? ` · ${node.relationCnt}` : ""}
      </span>
    </li>
  );
}

// ===========================================================================
// FilesView — RAG "文件" tab: recursive RagNode tree
// ===========================================================================

function FilesView({
  collection,
  onOpenFile,
  onFileClick,
}: {
  collection: string;
  onOpenFile: (path: string) => void;
  onFileClick?: (path: string) => void;
}) {
  const t = useT();
  const { showToast } = useToast();
  const [tree, setTree] = useState<RagNodeView[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!collection) return;
    setLoading(true);
    setError(null);
    try {
      const nodes = await app.ListRagTree(collection).catch(() => [] as RagNodeView[]);
      setTree(nodes ?? []);
    } catch (e) {
      setTree([]);
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }, [collection]);

  useEffect(() => {
    void refresh();
    return onRagChanged(() => void refresh());
  }, [refresh]);

  // Drag-and-drop import.
  useEffect(() => {
    if (!collection) return;
    return onFilesDropped((paths) => {
      if (!paths || paths.length === 0) return;
      void app
        .RagImportPaths(collection, paths)
        .then((res) => {
          showToast(res.message, "info");
          void refresh();
        })
        .catch((e) => showToast(String(e), "error"));
    });
  }, [collection, refresh, showToast]);

  const handleStartExtract = useCallback(
    async (node: RagNodeView) => {
      try {
        await app.RagStartExtract(collection, node.path).catch(() => {});
        void refresh();
      } catch (e) {
        showToast(String(e), "error");
      }
    },
    [collection, refresh, showToast],
  );

  const handleCancel = useCallback(
    async (node: RagNodeView) => {
      if (!node.jobId) return;
      try {
        await app.RagCancelExtract(node.jobId).catch(() => {});
        void refresh();
      } catch (e) {
        showToast(String(e), "error");
      }
    },
    [refresh, showToast],
  );

  const handleRemove = useCallback(
    async (node: RagNodeView) => {
      try {
        await app.RagRemovePath(collection, node.path).catch(() => {});
        void refresh();
      } catch (e) {
        showToast(String(e), "error");
      }
    },
    [collection, refresh, showToast],
  );

  const handleFileClick = useCallback(
    (node: RagNodeView) => {
      const p = node.path || node.relPath || node.label;
      onFileClick?.(p);
      onOpenFile(p);
    },
    [onFileClick, onOpenFile],
  );

  if (!collection) {
    return (
      <div className="cowork-dock__empty-state">
        <FileText size={22} />
        <p>{t("coworkDock.selectCollectionFirst") || "请先选择一个集合"}</p>
        <span className="cowork-dock__empty-hint">
          {t("coworkDock.selectCollectionHint") || "在「集合」tab 中选择或创建一个集合"}
        </span>
      </div>
    );
  }
  if (loading) {
    return <div className="cowork-dock__loading">{t("common.loading") || "加载中…"}</div>;
  }
  if (error) {
    return (
      <div className="cowork-dock__empty-state">
        <FileText size={22} />
        <p>{t("coworkDock.loadFailed") || "加载失败"}</p>
        <span className="cowork-dock__empty-hint">{error}</span>
        <button className="workbench-dock__tab" type="button" onClick={() => void refresh()} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
    );
  }

  if (tree.length === 0) {
    return (
      <div
        className="cowork-dock__empty-state"
        style={{ "--wails-drop-target": "drop" } as React.CSSProperties}
      >
        <FolderOpen size={22} />
        <p>{t("coworkDock.dragFilesHint") || "拖入文件以导入"}</p>
        <span className="cowork-dock__empty-hint">
          {t("coworkDock.dragFilesHint2") || "支持 md / docx / pdf / xlsx / csv / 代码 等格式"}
        </span>
        <button className="workbench-dock__tab" type="button" onClick={() => void refresh()} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
    );
  }

  return (
    <div
      className="rag-dock__files cowork-rag__tree"
      style={{ "--wails-drop-target": "drop" } as React.CSSProperties}
    >
      <div className="cowork-mailtab__head">
        <span className="cowork-mailtab__status">{t("coworkDock.files") || "文件"}</span>
        <button className="cowork-mailtab__refresh" type="button" onClick={() => void refresh()} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
      <div className="rag-dock__file-tree">
        {tree.map((node) => (
          <RagNode
            key={node.key || node.path || node.label}
            node={node}
            depth={0}
            onStartExtract={handleStartExtract}
            onCancel={handleCancel}
            onRemove={handleRemove}
            onFileClick={handleFileClick}
          />
        ))}
      </div>
    </div>
  );
}

