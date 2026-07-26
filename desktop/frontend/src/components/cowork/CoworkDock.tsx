// CoworkDock is the cowork-mode right-side panel.
//
// Reverse-engineered 1:1 from the 7/15 release bundle. Two modes, both using
// the coding-mode workbench-dock tab strip classes (workbench-dock__tools /
// __tabs / __tab) so styling reads as the same control as the coding-mode right
// dock:
//   - mode="default" (Kp): 3 tabs — 今日 / 邮件 / 文件.
//   - mode="rag"     (Gp): 4 tabs — 集合 / 实体 / 文件 / 提取.
//
// In RAG mode, the body is replaced by EntityDetail / DocPreview when an entity
// or document is opened; rag:entity-click (emitted by the graph canvas) and
// rag:progress / rag:changed (emitted by the backend) are subscribed so the
// dock stays live.
//
// All backend methods are wrapped with .catch fallbacks since the backend may
// not implement every one yet.

import { useCallback, useEffect, useState } from "react";
import {
  CalendarClock,
  CalendarDays,
  CircleDashed,
  FileText,
  Folder,
  Mail,

  RefreshCw,
  Trash2,
  Zap,
} from "lucide-react";

import { app, onRagChanged, onRagProgress } from "../../lib/bridge";

// realApp mirrors bridge.ts's private helper: returns the Wails binding only
// when window.go.main.App is present (i.e. we are inside the desktop shell).
function realApp(): unknown | undefined {
  return typeof window !== "undefined"
    ? (window as unknown as { go?: { main?: { App?: unknown } } }).go?.main?.App
    : undefined;
}
import { useT } from "../../lib/i18n";
import type {
  CalendarEventView,
  MailProbeResult,
  RagCollectionView,
  RagNodeView,
  TaskView,
} from "../../lib/types";
import { WorkspacePanel } from "../WorkspacePanel";
import { EntityDetail } from "./EntityDetail";
import { DocPreview } from "./DocPreview";
import { TemplateSelect } from "./TemplateSelect";
import { RagNode } from "./RagNode";

// PALETTE (Bp) is the fallback color list for calendar events without an
// explicit color. The original bundle uses a small CSS-var palette; index 0 is
// the default returned by eventColor.
const PALETTE: string[] = [
  "var(--accent, #58a6ff)",
  "var(--ok, #3fb950)",
  "var(--warning, #d29922)",
  "var(--danger, #f85149)",
  "var(--info, #58a6ff)",
];

// InboxItem is the trimmed envelope returned by app.InboxPreview. types.ts
// declares preview as required, but the backend may omit it on some mails, so
// we mirror the runtime shape with an optional preview here.
interface InboxItem {
  from: string;
  date: string;
  subject: string;
  preview?: string;
}

// Window.runtime type for the wails EventsOn binding.
interface WailsRuntimeLike {
  EventsOn(name: string, cb: (...args: unknown[]) => void): () => void;
}

// ===========================================================================
// helpers
// ===========================================================================

// eventColor ($p) returns the event's own color or the first palette entry.
function eventColor(e: CalendarEventView): string {
  return e.color || PALETTE[0];
}

// formatEventTime (Hp) renders a Date as "HH:MM" (24h, locale-stripped), or ""
// when the input is not a valid date.
function formatEventTime(value: string): string {
  const d = new Date(value);
  if (isNaN(d.getTime())) return "";
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
}

// formatDateTime (Up) renders a Date as "M/D HH:MM" within the current year,
// or "YYYY/M/D HH:MM" otherwise. "" on parse failure.
function formatDateTime(value: string): string {
  const d = new Date(value);
  if (isNaN(d.getTime())) return "";
  const now = new Date();
  const sameYear = d.getFullYear() === now.getFullYear();
  const base = `${d.getMonth() + 1}/${d.getDate()} ${formatEventTime(value)}`;
  return sameYear ? base : `${d.getFullYear()}/${base}`;
}

// ===========================================================================
// CoworkDock (qp) — top-level switch between default and RAG modes
// ===========================================================================

export interface CoworkDockProps {
  cwd?: string;
  maximized: boolean;
  onClose: () => void;
  onToggleMaximized: () => void;
  mode?: "default" | "rag";
  onEntityClick?: (name: string) => void;
  onFileClick?: (path: string) => void;
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
  return mode === "rag" ? (
    <RagDock onEntityClick={onEntityClick} onFileClick={onFileClick} />
  ) : (
    <DefaultDock
      cwd={cwd}
      maximized={maximized}
      onClose={onClose}
      onToggleMaximized={onToggleMaximized}
    />
  );
}

// ===========================================================================
// DefaultDock (Kp) — 今日 / 邮件 / 文件
// ===========================================================================

type DefaultTab = "today" | "mail" | "files";

function DefaultDock({
  cwd,
  maximized,
  onClose,
  onToggleMaximized,
}: {
  cwd?: string;
  maximized: boolean;
  onClose: () => void;
  onToggleMaximized: () => void;
}) {
  const t = useT();
  const [tab, setTab] = useState<DefaultTab>("today");

  return (
    <aside className="cowork-dock" aria-label={t("coworkDock.label") || "办公概览"}>
      <div className="workbench-dock__tools">
        <div className="workbench-dock__tabs" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={tab === "today"}
            className={"workbench-dock__tab" + (tab === "today" ? " workbench-dock__tab--active" : "")}
            onClick={() => setTab("today")}
            title={t("coworkDock.today") || "今日"}
          >
            <CalendarDays size={13} />
            <span className="workbench-dock__tab-label">{t("coworkDock.today") || "今日"}</span>
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "mail"}
            className={"workbench-dock__tab" + (tab === "mail" ? " workbench-dock__tab--active" : "")}
            onClick={() => setTab("mail")}
            title={t("coworkDock.mail") || "邮件"}
          >
            <Mail size={13} />
            <span className="workbench-dock__tab-label">{t("coworkDock.mail") || "邮件"}</span>
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "files"}
            className={"workbench-dock__tab" + (tab === "files" ? " workbench-dock__tab--active" : "")}
            onClick={() => setTab("files")}
            title={t("coworkDock.files") || "文件"}
          >
            <FileText size={13} />
            <span className="workbench-dock__tab-label">{t("coworkDock.files") || "文件"}</span>
          </button>
        </div>
      </div>

      <div className="cowork-dock__body">
        {tab === "today" && <TodayView />}
        {tab === "mail" && <MailView />}
        {tab === "files" &&
          (cwd ? (
            <WorkspacePanel
              open
              cwd={cwd}
              maximized={maximized}
              onClose={onClose}
              onToggleMaximized={onToggleMaximized}
              showViewTabs={false}
              initialViewMode="files"
            />
          ) : (
            <div className="cowork-dock__empty-state">
              <FileText size={22} />
              <p>{t("coworkDock.noWorkspace") || "当前会话未关联工作区文件夹"}</p>
              <p className="cowork-dock__empty-hint">
                {t("coworkDock.noWorkspaceHint") || "新建会话时选择一个项目文件夹即可在此浏览文件"}
              </p>
            </div>
          ))}
      </div>
    </aside>
  );
}

// ===========================================================================
// TodayView (Wp) — 今日日程 / 邮箱 / 自动化
// ===========================================================================

function TodayView() {
  const t = useT();
  const [events, setEvents] = useState<CalendarEventView[] | null>(null);
  const [tasks, setTasks] = useState<TaskView[] | null>(null);
  const [probe, setProbe] = useState<MailProbeResult | null>(null);
  const [inbox, setInbox] = useState<InboxItem[] | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    const now = new Date();
    const since = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(
      now.getDate(),
    ).padStart(2, "0")}`;
    const next = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1);
    const before = `${next.getFullYear()}-${String(next.getMonth() + 1).padStart(2, "0")}-${String(
      next.getDate(),
    ).padStart(2, "0")}`;
    const [evs, tks, mb, inb] = await Promise.all([
      (app as unknown as { ListCalendarEvents: (s: string, b: string) => Promise<CalendarEventView[]> })
        .ListCalendarEvents(since, before)
        .catch(() => [] as CalendarEventView[]),
      (app as unknown as { ListScheduledTasks: () => Promise<TaskView[]> })
        .ListScheduledTasks()
        .catch(() => [] as TaskView[]),
      (app as unknown as { ProbeMailAccount: () => Promise<MailProbeResult> })
        .ProbeMailAccount()
        .catch(() => ({ ok: false, status: "error", message: "" } as MailProbeResult)),
      (app as unknown as { InboxPreview?: (n: number) => Promise<InboxItem[]> })
        .InboxPreview?.(50)
        .catch(() => [] as InboxItem[]) ?? Promise.resolve([] as InboxItem[]),
    ]);
    setEvents(evs);
    setTasks(tks);
    setProbe(mb);
    setInbox(inb);
    setLoading(false);
  }, []);

  useEffect(() => {
    refresh();
    const h = window.setInterval(() => {
      refresh();
    }, 60000);
    return () => window.clearInterval(h);
  }, [refresh]);

  if (loading) {
    return <div className="cowork-dock__loading">…</div>;
  }

  const now = new Date();
  const todaysEvents = (events ?? []).filter((e) => {
    const start = new Date(e.start);
    return (
      start.getFullYear() === now.getFullYear() &&
      start.getMonth() === now.getMonth() &&
      start.getDate() === now.getDate()
    );
  }).sort((a, b) => a.start.localeCompare(b.start));

  const nowMs = Date.now();
  const upcoming = (tasks ?? [])
    .filter((tk) => tk.enabled && tk.nextRun)
    .map((tk) => ({ tk, ts: new Date(tk.nextRun).getTime() }))
    .filter((x) => !isNaN(x.ts) && x.ts >= nowMs)
    .sort((a, b) => a.ts - b.ts)
    .slice(0, 3);

  const mailOk = probe?.status === "ok";
  const mailUnconfigured = probe?.status === "unconfigured" || !probe;
  const unreadCount = inbox?.length ?? 0;

  return (
    <div className="cowork-today">
      <section className="cowork-today__section">
        <h4 className="cowork-today__heading">
          <CalendarClock size={13} />
          {t("coworkDock.todayEvents") || "今日日程"}
        </h4>
        {todaysEvents.length === 0 ? (
          <div className="cowork-today__empty">{t("coworkDock.noEvents") || "今日暂无日程"}</div>
        ) : (
          <ul className="cowork-today__list">
            {todaysEvents.map((e) => (
              <li key={e.id} className="cowork-today__row">
                <span className="cowork-today__time">{e.allDay ? "全天" : formatEventTime(e.start)}</span>
                <span className="cowork-today__dot" style={{ background: eventColor(e) }} />
                <span className="cowork-today__text" title={e.title}>
                  {e.title}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="cowork-today__section">
        <h4 className="cowork-today__heading">
          <Mail size={13} />
          {t("coworkDock.mailbox") || "邮箱"}
        </h4>
        {mailUnconfigured ? (
          <div className="cowork-today__hint">
            {t("coworkDock.mailConfigureHint") || "请在「设置」-「办公」中配置邮箱"}
          </div>
        ) : (
          <div className="cowork-today__mailrow">
            <span className={"mail-status-dot mail-status-dot--" + (mailOk ? "ok" : "error")} />
            <span className="cowork-today__mailtext">
              {mailOk
                ? t("coworkDock.mailConnected") || "已连接"
                : probe?.message || t("coworkDock.mailError") || "连接失败"}
            </span>
            {mailOk && (
              <span className="cowork-today__badge">
                {unreadCount > 0
                  ? t("coworkDock.unreadN").replace("{n}", String(unreadCount)) || `${unreadCount} 封未读`
                  : t("coworkDock.noUnread") || "无未读"}
              </span>
            )}
          </div>
        )}
      </section>

      <section className="cowork-today__section">
        <h4 className="cowork-today__heading">
          <CircleDashed size={13} />
          {t("coworkDock.upcoming") || "自动化"}
        </h4>
        {upcoming.length === 0 ? (
          <div className="cowork-today__empty">{t("coworkDock.noUpcoming") || "暂无定时任务"}</div>
        ) : (
          <ul className="cowork-today__list">
            {upcoming.map(({ tk, ts }) => (
              <li key={tk.id} className="cowork-today__row">
                <span className="cowork-today__time">{formatDateTime(new Date(ts).toISOString())}</span>
                <span className="cowork-today__dot cowork-today__dot--plain" />
                <span className="cowork-today__text" title={tk.name}>
                  {tk.name}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

// ===========================================================================
// MailView (Vp) — 邮件列表 + probe 状态
// ===========================================================================

function MailView() {
  const t = useT();
  const [inbox, setInbox] = useState<InboxItem[] | null>(null);
  const [probe, setProbe] = useState<MailProbeResult | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [openKey, setOpenKey] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [mb, inb] = await Promise.all([
        (app as unknown as { ProbeMailAccount: () => Promise<MailProbeResult> })
          .ProbeMailAccount()
          .catch(() => ({ ok: false, status: "error", message: "" } as MailProbeResult)),
        (app as unknown as { InboxPreview?: (n: number) => Promise<InboxItem[]> }).InboxPreview?.(30) ??
          Promise.resolve([] as InboxItem[]),
      ]);
      setProbe(mb);
      setInbox(inb);
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const h = window.setInterval(() => {
      refresh();
    }, 120000);
    return () => window.clearInterval(h);
  }, [refresh]);

  const mailOk = probe?.status === "ok";
  const mailUnconfigured = probe?.status === "unconfigured" || !probe;

  return (
    <div className="cowork-mailtab">
      <div className="cowork-mailtab__head">
        <div className="cowork-mailtab__status">
          <span
            className={"mail-status-dot mail-status-dot--" + (mailOk ? "ok" : mailUnconfigured ? "idle" : "error")}
          />
          <span>
            {mailUnconfigured
              ? t("coworkDock.mailUnconfigured") || "未配置邮箱"
              : mailOk
                ? t("coworkDock.mailConnected") || "已连接"
                : probe?.message || t("coworkDock.mailError") || "连接失败"}
          </span>
        </div>
        <button
          className="cowork-mailtab__refresh"
          onClick={() => {
            refresh();
          }}
          title={t("common.refresh") || "刷新"}
        >
          <RefreshCw size={13} />
        </button>
      </div>

      {loading ? (
        <div className="cowork-dock__loading">…</div>
      ) : error ? (
        <div className="cowork-today__empty">{error}</div>
      ) : mailUnconfigured ? (
        <div className="cowork-dock__empty-state">
          <Mail size={22} />
          <p>{t("coworkDock.mailUnconfigured") || "未配置邮箱"}</p>
          <p className="cowork-dock__empty-hint">
            {t("coworkDock.mailConfigureHint") || "请在「设置」-「办公」中配置邮箱"}
          </p>
        </div>
      ) : mailOk ? (
        inbox && inbox.length !== 0 ? (
          <ul className="cowork-mailtab__list">
            {inbox.map((m, i) => {
              const key = `${m.date}-${i}`;
              const open = openKey === key;
              return (
                <li
                  key={key}
                  className={"cowork-mailtab__item" + (open ? " cowork-mailtab__item--open" : "")}
                  onClick={() => setOpenKey(open ? null : key)}
                >
                  <div className="cowork-mailtab__item-head">
                    <span className="cowork-mailtab__from" title={m.from}>
                      {m.from}
                    </span>
                    <span className="cowork-mailtab__date">{formatDateTime(m.date)}</span>
                  </div>
                  <div className="cowork-mailtab__subject" title={m.subject}>
                    {m.subject || "（无主题）"}
                  </div>
                  {open && m.preview && <div className="cowork-mailtab__preview">{m.preview}</div>}
                </li>
              );
            })}
          </ul>
        ) : (
          <div className="cowork-today__empty">{t("coworkDock.noUnreadMail") || "没有未读邮件"}</div>
        )
      ) : (
        <div className="cowork-today__empty">{probe?.message || t("coworkDock.mailError") || "连接失败"}</div>
      )}
    </div>
  );
}

// ===========================================================================
// RagDock (Gp) — 分类 / 文件（精简为 2 tab）
// ===========================================================================

type RagTab = "collections" | "files" | "extract";

function RagDock({
  onEntityClick,
  onFileClick,
}: {
  onEntityClick?: (name: string) => void;
  onFileClick?: (path: string) => void;
}) {
  const [tab, setTab] = useState<RagTab>("collections");
  const [collections, setCollections] = useState<RagCollectionView[]>([]);
  const [activeCollection, setActiveCollection] = useState("");
  // activeCollections: null = "all selected", string[] = explicit subset.
  const [activeCollections, setActiveCollections] = useState<string[] | null>(null);
  const [tree, setTree] = useState<RagNodeView[]>([]);
  const [entityName, setEntityName] = useState<string | null>(null);
  const [entityCollection, setEntityCollection] = useState<string | null>(null);
  const [docPath, setDocPath] = useState<string | null>(null);
  const [docCollection, setDocCollection] = useState("");
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [newCollectionName, setNewCollectionName] = useState("");
  const [newCollectionParent, setNewCollectionParent] = useState("");

  // Initial load: collections + session-scoped collections.
  useEffect(() => {
    (app as unknown as { ListRagCollections: () => Promise<RagCollectionView[]> })
      .ListRagCollections()
      .then(setCollections)
      .catch(() => {});
    (app as unknown as { GetSessionCollections: () => Promise<string[]> })
      .GetSessionCollections()
      .then((e) => {
        setActiveCollections(e.length === 0 ? null : e);
      })
      .catch(() => {});
  }, []);

  const refreshTree = useCallback(() => {
    (app as unknown as { ListRagTree: (c: string) => Promise<RagNodeView[]> })
      .ListRagTree(activeCollection)
      .then(setTree)
      .catch(() => {});
  }, [activeCollection]);

  // Re-fetch tree when active collection changes.
  useEffect(() => {
    (app as unknown as { ListRagTree: (c: string) => Promise<RagNodeView[]> })
      .ListRagTree(activeCollection)
      .then(setTree)
      .catch(() => {});
  }, [activeCollection]);

  // rag:progress + rag:changed subscriptions.
  useEffect(() => {
    const offProgress =
      realApp() &&
      typeof window !== "undefined" &&
      (window as unknown as { runtime?: WailsRuntimeLike }).runtime
        ? (window as unknown as { runtime: WailsRuntimeLike }).runtime.EventsOn("rag:progress", (...args) => {
            const payload = (args?.[0] ?? {}) as Record<string, unknown>;
            // The bundle destructures these but only uses them to trigger a
            // tree refresh, so we just call refreshTree() here.
            void payload;
            refreshTree();
          })
        : () => {};
    const offChanged = onRagChanged(() => {
      refreshTree();
      (app as unknown as { ListRagCollections: () => Promise<RagCollectionView[]> })
        .ListRagCollections()
        .then(setCollections)
        .catch(() => {});
    });
    return () => {
      offProgress();
      offChanged();
    };
  }, [refreshTree]);

  // rag:entity-click → open entity detail (graph click → dock).
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<{ name?: string; collection?: string }>).detail;
      if (detail?.name) {
        setEntityName(detail.name);
        if (detail.collection) setEntityCollection(detail.collection);
        setDocPath(null);
      }
    };
    window.addEventListener("rag:entity-click", handler);
    return () => window.removeEventListener("rag:entity-click", handler);
  }, []);

  // --- sub-view: entity detail --------------------------------------------
  if (entityName) {
    return (
      <aside className="cowork-dock">
        <EntityDetail
          collection={entityCollection || activeCollection}
          entityName={entityName}
          onBack={() => setEntityName(null)}
          onHighlightInGraph={(name) => {
            setEntityName(name);
            onEntityClick?.(name);
          }}
          onNavigatePeer={(name) => setEntityName(name)}
        />
      </aside>
    );
  }

  // --- sub-view: doc preview ----------------------------------------------
  if (docPath) {
    return (
      <aside className="cowork-dock">
        <DocPreview collection={docCollection} docPath={docPath} onBack={() => setDocPath(null)} />
      </aside>
    );
  }

  // --- main tabbed body (2 tabs: 分类 + 文件) -----------------------------
  return (
    <aside className="cowork-dock" aria-label="知识库导航">
      <div className="workbench-dock__tools">
        <div className="workbench-dock__tabs" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={tab === "collections"}
            className={"workbench-dock__tab" + (tab === "collections" ? " workbench-dock__tab--active" : "")}
            onClick={() => setTab("collections")}
            title="分类"
          >
            <Folder size={13} />
            <span className="workbench-dock__tab-label">分类</span>
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "files"}
            className={"workbench-dock__tab" + (tab === "files" ? " workbench-dock__tab--active" : "")}
            onClick={() => setTab("files")}
            title="文件"
          >
            <FileText size={13} />
            <span className="workbench-dock__tab-label">文件</span>
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "extract"}
            className={"workbench-dock__tab" + (tab === "extract" ? " workbench-dock__tab--active" : "")}
            onClick={() => setTab("extract")}
            title="深度提取"
          >
            <Zap size={13} />
            <span className="workbench-dock__tab-label">提取</span>
          </button>
        </div>
      </div>

      <div className="cowork-dock__body">
        {/* === 分类 tab === */}
        {tab === "collections" && (
          <div className="rag-dock__collections">
            <div className="rag-dock__collection-header">
              <span className="rag-dock__collection-title">激活集合</span>
              <div className="rag-dock__collection-actions">
                <button
                  className="rag-dock__collection-action"
                  onClick={() => {
                    setActiveCollections(null);
                    setActiveCollection("");
                    (app as unknown as { SetSessionCollections: (c: string[]) => Promise<void> })
                      .SetSessionCollections([])
                      .catch(() => {});
                  }}
                >
                  全选
                </button>
                <button
                  className="rag-dock__collection-action"
                  title="新建分类"
                  onClick={() => setShowCreateModal(true)}
                >
                  +
                </button>
              </div>
            </div>

            {/* "全部" pseudo-collection */}
            <label className="rag-dock__collection-item">
              <input
                type="checkbox"
                checked={activeCollections === null}
                onChange={() => {
                  setActiveCollections(null);
                  setActiveCollection("");
                  (app as unknown as { SetSessionCollections: (c: string[]) => Promise<void> })
                    .SetSessionCollections([])
                    .catch(() => {});
                }}
              />
              <span className="rag-dock__collection-name" style={{ fontWeight: 500 }}>全部</span>
            </label>

            {collections.map((c) => {
              const checked = activeCollections === null || activeCollections.includes(c.name);
              const isActive = activeCollection === c.name;
              return (
                <label
                  key={c.path || c.name}
                  className={`rag-dock__collection-item ${isActive ? "rag-dock__collection-item--active" : ""}`}
                  style={{ paddingLeft: c.parent ? "24px" : undefined }}
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={(ev) => {
                      let next: string[] | null;
                      if (activeCollections === null) {
                        next = ev.target.checked
                          ? null
                          : collections.map((x) => x.name).filter((n) => n !== c.name);
                      } else {
                        next = ev.target.checked
                          ? [...activeCollections, c.name]
                          : activeCollections.filter((n) => n !== c.name);
                      }
                      setActiveCollections(next);
                      (app as unknown as { SetSessionCollections: (c: string[]) => Promise<void> })
                        .SetSessionCollections(next ?? [])
                        .catch(() => {});
                    }}
                  />
                  <span
                    className="rag-dock__collection-name"
                    onClick={() => {
                      setActiveCollection(c.name);
                      setTab("files");
                    }}
                    style={{ cursor: "pointer" }}
                    title={`${c.documents} 文档 · ${c.entities} 实体`}
                  >
                    {c.parent ? "└ " : "📁 "}
                    {c.name}
                  </span>
                  <button
                    className="rag-dock__collection-delete"
                    title={`删除分类及全部文档`}
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      if (window.confirm(`删除分类"${c.name}"及其全部 ${c.documents} 个文档和 ${c.entities} 个实体？\n\n此操作不可撤销。`)) {
                        (app as unknown as { RagDeleteCollection: (n: string) => Promise<void> })
                          .RagDeleteCollection(c.name)
                          .then(() => {
                            (app as unknown as { ListRagCollections: () => Promise<RagCollectionView[]> })
                              .ListRagCollections()
                              .then(setCollections)
                              .catch(() => {});
                          })
                          .catch(() => {});
                      }
                    }}
                  >
                    <Trash2 size={12} />
                  </button>
                  <span className="rag-dock__collection-stats">
                    {c.documents > 0 ? `${c.documents}` : ""}
                  </span>
                </label>
              );
            })}

            {collections.length === 0 && (
              <div className="cowork-dock__empty-state">
                <Folder size={22} />
                <p>暂无集合</p>
              </div>
            )}
          </div>
        )}

        {/* === 提取 tab === */}
        {tab === "extract" && (
          <TemplateSelect
            collection={activeCollection}
            onBack={() => setTab("files")}
          />
        )}

        {/* === 文件 tab === */}
        {tab === "files" && (
          <div className="rag-dock__files">
            {/* 文件列表 */}
            {tree.length === 0 ? (
                  <div className="cowork-dock__empty-state">
                    <FileText size={22} />
                    <p>暂无文件</p>
                  </div>
                ) : (
                  <div className="rag-dock__file-tree">
                    {tree.map((node) => (
                      <RagNode
                        key={node.key}
                        node={node}
                        depth={0}
                        onStartExtract={(n) => {
                          if (n.path) {
                            (app as unknown as { RagStartExtract: (c: string, t: string, m: string) => Promise<void> })
                              .RagStartExtract(activeCollection, n.path, "incremental")
                              .then(() => refreshTree())
                              .catch(() => refreshTree());
                          }
                        }}
                        onCancel={(n) => {
                          if (n.jobId) {
                            (app as unknown as { RagCancelExtract: (j: string) => Promise<void> })
                              .RagCancelExtract(n.jobId)
                              .then(() => refreshTree())
                              .catch(() => refreshTree());
                          }
                        }}
                        onRemove={(n) => {
                          if (n.path && window.confirm(`确定删除该文件已提取的知识？\n${n.path}\n\n文档本身不会被删除，可重新提取。`)) {
                            (app as unknown as { RagRemovePath: (c: string, p: string) => Promise<void> })
                              .RagRemovePath(activeCollection, n.path)
                              .then(() => refreshTree())
                              .catch(() => refreshTree());
                          }
                        }}
                        onFileClick={(n) => {
                          setDocCollection(n.collection);
                          setDocPath(n.path);
                          setEntityName(null);
                          onFileClick?.(n.path);
                        }}
                        selectedPath={docPath ?? undefined}
                      />
                    ))}
                  </div>
                )}
          </div>
        )}
      </div>

      {/* 新建分类弹窗 */}
      {showCreateModal && (
        <div className="rag-create-overlay" onClick={() => setShowCreateModal(false)}>
          <div className="rag-create-modal" onClick={(e) => e.stopPropagation()}>
            <div className="rag-create-modal__head">
              <h3 className="rag-create-modal__title">新建分类</h3>
              <button className="rag-create-modal__close" onClick={() => setShowCreateModal(false)}>✕</button>
            </div>
            <div className="rag-create-modal__body">
              <div className="rag-create-modal__section">
                <label className="rag-create-modal__label">选择模板或自定义</label>
                <div className="rag-create-modal__templates">
                  {["工作", "学习", "个人", "项目"].map((tpl) => (
                    <button
                      key={tpl}
                      className={`rag-create-modal__template ${newCollectionParent === tpl ? "rag-create-modal__template--selected" : ""}`}
                      onClick={() => setNewCollectionParent(tpl)}
                    >
                      📁 {tpl}
                    </button>
                  ))}
                  <button
                    className={`rag-create-modal__template ${newCollectionParent === "" ? "rag-create-modal__template--selected" : ""}`}
                    onClick={() => setNewCollectionParent("")}
                  >
                    ✏️ 自定义
                  </button>
                </div>
              </div>
              {newCollectionParent && (
                <div className="rag-create-modal__section">
                  <label className="rag-create-modal__label">父分类</label>
                  <div className="rag-create-modal__parent">{newCollectionParent}/</div>
                </div>
              )}
              <div className="rag-create-modal__section">
                <label className="rag-create-modal__label">分类名称</label>
                <input
                  className="rag-create-modal__input"
                  placeholder={newCollectionParent ? "如：领导材料" : "如：工作 或 工作/领导材料"}
                  value={newCollectionName}
                  onChange={(e) => setNewCollectionName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      const name = newCollectionName.trim();
                      if (name) {
                        const full = newCollectionParent ? `${newCollectionParent}/${name}` : name;
                        (app as unknown as { RagCreateCollection: (n: string) => Promise<void> })
                          .RagCreateCollection(full)
                          .then(() => {
                            setShowCreateModal(false);
                            setNewCollectionName("");
                            setNewCollectionParent("");
                            (app as unknown as { ListRagCollections: () => Promise<RagCollectionView[]> })
                              .ListRagCollections()
                              .then(setCollections)
                              .catch(() => {});
                          })
                          .catch(() => {});
                      }
                    }
                  }}
                  autoFocus
                />
              </div>
              <div className="rag-create-modal__preview">
                {newCollectionParent && newCollectionName
                  ? `${newCollectionParent}/${newCollectionName}`
                  : newCollectionName || "（请输入名称）"}
              </div>
            </div>
            <div className="rag-create-modal__foot">
              <button className="btn btn--small" onClick={() => setShowCreateModal(false)}>取消</button>
              <button
                className="btn btn--primary btn--small"
                disabled={!newCollectionName.trim()}
                onClick={() => {
                  const name = newCollectionName.trim();
                  if (!name) return;
                  const full = newCollectionParent ? `${newCollectionParent}/${name}` : name;
                  (app as unknown as { RagCreateCollection: (n: string) => Promise<void> })
                    .RagCreateCollection(full)
                    .then(() => {
                      setShowCreateModal(false);
                      setNewCollectionName("");
                      setNewCollectionParent("");
                      (app as unknown as { ListRagCollections: () => Promise<RagCollectionView[]> })
                        .ListRagCollections()
                        .then(setCollections)
                        .catch(() => {});
                    })
                    .catch(() => {});
                }}
              >
                创建
              </button>
            </div>
          </div>
        </div>
      )}
    </aside>
  );
}

// onRagProgress is imported above for API parity with bridge.ts. The dock
// subscribes via window.runtime.EventsOn directly (matching the bundle), but
// we keep the symbol referenced so tree-shaking does not drop the import and
// future migrations to the bridge helper are a one-line swap.
void onRagProgress;
