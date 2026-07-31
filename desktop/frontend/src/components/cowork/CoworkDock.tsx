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
  ChevronDown,
  ChevronRight,
  CornerDownRight,
  FileText,
  Folder,
  FolderPlus,
  Mail,

  RefreshCw,
  Search,
  Trash2,
  X,
  Zap,
  Bot,
  Sparkles,
} from "lucide-react";

import { app, onRagChanged, onRagProgress } from "../../lib/bridge";
import { useToast } from "../../lib/toast";
import { CustomSelect } from "./CustomSelect";

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
  BotDockStatusView,
} from "../../lib/types";
import { WorkspacePanel } from "../WorkspacePanel";
import { EntityDetail } from "./EntityDetail";
import { DocPreview } from "./DocPreview";
import { TemplateSelect } from "./TemplateSelect";
import { RagNode } from "./RagNode";
import { ImportModal } from "./ImportModal";
import { ConfirmModal } from "../ConfirmModal";
import { useConfirm } from "../../lib/confirm";

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
  to: string;
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
              onAddToChat={(text: string) => window.dispatchEvent(new CustomEvent("cowork:insert-text", { detail: text }))}
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
  const [holidays, setHolidays] = useState<CalendarEventView[]>([]);
  const [botStatus, setBotStatus] = useState<BotDockStatusView | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(() => {
    setLoading(true);
    const now = new Date();
    const since = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(
      now.getDate(),
    ).padStart(2, "0")}`;
    const next = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1);
    const before = `${next.getFullYear()}-${String(next.getMonth() + 1).padStart(2, "0")}-${String(
      next.getDate(),
    ).padStart(2, "0")}`;

    // 1. 优先快加载：日程、任务、节假日、bot状态（本地数据，几乎 0 延迟）
    Promise.all([
      (app as unknown as { ListCalendarEvents: (s: string, b: string) => Promise<CalendarEventView[]> })
        .ListCalendarEvents(since, before)
        .catch(() => [] as CalendarEventView[]),
      (app as unknown as { ListScheduledTasks: () => Promise<TaskView[]> })
        .ListScheduledTasks()
        .catch(() => [] as TaskView[]),
      (app as unknown as { GetChineseHolidays?: (year: number) => Promise<CalendarEventView[]> })
        .GetChineseHolidays?.(now.getFullYear())
        .catch(() => [] as CalendarEventView[]) ?? Promise.resolve([] as CalendarEventView[]),
      (app as unknown as { BotDockStatus?: () => Promise<BotDockStatusView> })
        .BotDockStatus?.()
        .catch(() => null as BotDockStatusView | null) ?? Promise.resolve(null as BotDockStatusView | null),
    ]).then(([evs, tks, hols, bs]) => {
      setEvents(evs);
      setTasks(tks);
      setHolidays(hols);
      setBotStatus(bs);
    });

    // 2. 异步慢加载：邮件探针与 50 封邮件列表（远程请求）
    Promise.all([
      (app as unknown as { ProbeMailAccount: (name: string) => Promise<MailProbeResult> })
        .ProbeMailAccount("")
        .catch(() => ({ ok: false, status: "error", message: "Wails调用异常" } as MailProbeResult)),
      (app as unknown as { InboxPreview?: (mailbox: string, n: number) => Promise<InboxItem[]> })
        .InboxPreview?.("INBOX", 10)
        .catch(() => [] as InboxItem[]) ?? Promise.resolve([] as InboxItem[]),
    ]).then(([mb, inb]) => {
      setProbe(mb);
      setInbox(inb);
      setLoading(false); // 慢查询完成后关闭 loading 状态
    });
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // 移除整页阻塞 loading，改为局部静默刷新，防止切换页卡卡顿

  const now = new Date();
  // todayItems merges today's calendar events AND today's enabled scheduled
  // tasks into a single unified list, so the user sees "everything happening
  // today" in one place instead of two disconnected sections. Each item carries
  // a `kind` so we can visually distinguish events (📅) from auto-tasks (⚡).
  // Events use .start ("YYYY-MM-DDTHH:MM"), tasks use .nextRun ("YYYY-MM-DD HH:MM",
  // space-separated) — both parse fine via new Date().
  type TodayItem = {
    id: string;
    title: string;
    time: Date;
    allDay: boolean;
    location: string;
    color: string;
    outputMode: string;
    kind: "event" | "task";
  };
  const isToday = (d: Date) =>
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  const todayItems: TodayItem[] = [
    ...(events ?? [])
      .filter((e) => isToday(new Date(e.start)))
      .map((e) => ({
        id: e.id,
        title: e.title,
        time: new Date(e.start),
        allDay: e.allDay,
        location: e.location,
        color: eventColor(e),
        outputMode: e.outputMode,
        kind: "event" as const,
      })),
    ...(tasks ?? [])
      .filter((tk) => tk.enabled && tk.nextRun && isToday(new Date(tk.nextRun)))
      .map((tk) => ({
        id: tk.id,
        title: tk.name,
        time: new Date(tk.nextRun),
        allDay: false,
        location: tk.location ?? "",
        color: tk.color || "#8b949e",
        outputMode: tk.outputMode,
        kind: "task" as const,
      })),
  ].sort((a, b) => a.time.getTime() - b.time.getTime());

  // Holiday hint: is today a holiday? If not, find the next upcoming one so
  // the user sees "距 XX节还有 N 天" in the briefing.
  const todayStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
  const todayHoliday = holidays.find((h) => {
    const hs = new Date(h.start);
    const he = new Date(h.end);
    return (now >= hs && now < he) || h.start === todayStr;
  });
  const nextHoliday = !todayHoliday
    ? holidays
        .filter((h) => new Date(h.start) > now)
        .sort((a, b) => a.start.localeCompare(b.start))[0]
    : undefined;
  const daysToNext = nextHoliday
    ? Math.ceil((new Date(nextHoliday.start).getTime() - Date.now()) / 86400000)
    : 0;

  const mailOk = probe?.status === "ok";
  const unreadCount = inbox?.length ?? 0;

  return (
    <div className="cowork-today" style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      <div style={{ flex: 1, overflowY: "auto", paddingBottom: "16px" }}>
        {/* 1. 顶部简报卡片 */}
        <div className="cowork-today__briefing">
          <div className="cowork-today__briefing-head" style={{ display: "flex", alignItems: "center", justifyContent: "flex-start" }}>
            <Sparkles size={14} style={{ color: "#f26522", marginRight: "4px" }} />
            <span style={{ fontWeight: 600 }}>今日日程与任务</span>
          </div>
          <div className="cowork-today__briefing-body" style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
            <div>1. <CalendarClock size={13} style={{ color: "#8b949e", margin: "0 2px", verticalAlign: "middle" }} />今日安排核心日程 {todayItems.length} 项。</div>
            <div>2. <Mail size={13} style={{ color: "#58a6ff", margin: "0 2px", verticalAlign: "middle" }} />待处理未读件 {unreadCount} 封。</div>
            <div>3. ☕ 当前时段暂无紧迫议程。</div>
            <div>4. 🤖 请点击下方按钮，获取昨日邮件工作总结。</div>
            {todayHoliday && (
              <div style={{ color: "#f85149", fontWeight: 600 }}>🎉 今天是「{todayHoliday.title}」假期，祝您节日愉快！</div>
            )}
            {!todayHoliday && nextHoliday && daysToNext <= 14 && (
              <div style={{ color: "#f0883e" }}>📅 距「{nextHoliday.title}」还有 {daysToNext} 天。</div>
            )}
          </div>
          <div className="cowork-today__briefing-actions">
            <button 
              className="rag-toolbar__btn" 
              style={{ background: "#f26522", color: "#fff", border: "none" }}
              onClick={() => {
                const prompt = `请生成今日行政决策早报。在早报开头，请务必直接列出以下现状信息：\n1. 今日安排核心日程 ${todayItems.length} 项。\n2. 待处理未读件 ${unreadCount} 封。\n3. 当前时段的紧迫议程。\n4. 昨日邮件的重要内容。\n\n接下来，请调用邮箱等工具分析上述内容，并向我简炼总结：今日还需要做的事情，以及昨天已经进行或遗留的重要事项。`;
                window.dispatchEvent(new CustomEvent("cowork:insert-text", { detail: prompt }));
              }}
            >
              <Sparkles size={13} /> 生成深度决策指引
            </button>
          </div>
        </div>

      <section className="cowork-today__section" style={{ marginTop: "20px", paddingBottom: "12px" }}>
        <h4 className="cowork-today__heading">
          <CalendarClock size={13} />
          {t("coworkDock.todayTodo") || "今日待办"}
          <span className="cowork-today__heading-count">{todayItems.length}</span>
        </h4>
        {todayItems.length === 0 ? (
          <div className="cowork-today__empty">{t("coworkDock.noTodo") || "今日暂无待办"}</div>
        ) : (
          <ul className="cowork-today__list">
            {todayItems.map((it) => {
              // Past items (time already passed) render dimmed so the user can
              // tell at a glance what's left today vs. what's behind them.
              const past = it.time.getTime() < Date.now();
              return (
                <li
                  key={`${it.kind}-${it.id}`}
                  className="cowork-today__row"
                  style={{ opacity: past ? 0.55 : 1 }}
                >
                  <span className="cowork-today__time">{it.allDay ? "全天" : formatEventTime(it.time.toISOString())}</span>
                  <span className="cowork-today__dot" style={{ background: it.color }} />
                  <span className="cowork-today__text" title={it.title + (it.location ? ` @ ${it.location}` : "")}>
                    {it.title}
                  </span>
                  {/* kind badge: 📅 event vs ⚡ task, so the two underlying systems
                      are still distinguishable inside the unified list. */}
                  <span className="cowork-today__kind" title={it.kind === "event" ? "日历事件" : "定时任务"}>
                    {it.kind === "event" ? "📅" : "⚡"}
                  </span>
                  {/* output-mode hint when the item pushes to IM/email */}
                  {it.outputMode === "im" && <span className="cowork-today__out" title="推送到 IM">💬</span>}
                  {it.outputMode === "email" && <span className="cowork-today__out" title="推送到邮件">✉️</span>}
                </li>
              );
            })}
          </ul>
        )}
      </section>

      </div> {/* End of scrollable area */}

      {/* 4. 底部的统合状态控制卡片 (固定在底部) */}
      <div style={{ 
        margin: "0 16px 16px", 
        background: "var(--bg-elev)", 
        border: "1px solid var(--border-soft)", 
        borderRadius: "12px", 
        boxShadow: "0 2px 8px rgba(0,0,0,0.02)",
        overflow: "hidden",
        flexShrink: 0
      }}>
        {/* 上半区：状态列表 (纵向堆叠，解决横向空间不足) */}
        <div style={{ display: "flex", flexDirection: "column" }}>
          {/* 邮箱行 */}
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "10px 16px", borderBottom: "1px solid var(--border-soft)" }}>
            <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
              <Mail size={14} style={{ color: "var(--fg-dim)" }} />
              <span style={{ fontSize: "13px", fontWeight: 500 }}>邮箱</span>
              <span className={"mail-status-dot mail-status-dot--" + (loading ? "warning" : (mailOk ? "ok" : "error"))} style={{ marginLeft: "4px" }} />
              <span style={{ fontSize: "12px", color: "var(--fg-dim)", maxWidth: "80px", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                {loading ? "同步中..." : (mailOk ? "已连接" : (probe?.message || "连接失败"))}
              </span>
            </div>
            <div>
              {unreadCount > 0 ? (
                <span style={{ color: "#e84e3c", fontWeight: 600, fontSize: "12px" }}>{unreadCount}封未读</span>
              ) : (
                <span style={{ color: "var(--fg-faint)", fontSize: "12px" }}>0封未读</span>
              )}
            </div>
          </div>

          {/* IM bot 行 — real status from BotDockStatus (not hardcoded). Click
              the row to jump to Settings → Bots for connection details. */}
          <div
            style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "10px 16px", cursor: "pointer" }}
            onClick={() => window.dispatchEvent(new CustomEvent("app:open-settings-tab", { detail: "bots" }))}
            title="点击查看 IM bot 连接详情"
          >
            <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
              <Bot size={14} style={{ color: "var(--fg-dim)" }} />
              <span style={{ fontSize: "13px", fontWeight: 500 }}>IM bot</span>
              <span className={"mail-status-dot mail-status-dot--" + (loading ? "warning" : (botStatus?.online ? "ok" : "idle"))} style={{ marginLeft: "4px" }} />
              <span style={{ fontSize: "12px", color: "var(--fg-dim)" }}>
                {loading ? "同步中..." : (botStatus?.online
                  ? (botStatus.platforms.length > 0 ? `在线（${botStatus.platforms.join("、")}）` : "在线")
                  : "未启动")}
              </span>
            </div>
            <div>
              <span style={{ color: "var(--fg-faint)", fontSize: "12px" }}>
                {botStatus?.online && botStatus.recentCount > 0
                  ? `${botStatus.recentCount} 个近期会话`
                  : "暂无消息"}
                <span style={{ marginLeft: "4px", opacity: 0.6 }}>›</span>
              </span>
            </div>
          </div>
        </div>

        {/* 下半区：刷新操作条 */}
        <div 
          className="cowork-today__refresh-btn"
          style={{ 
            display: "flex", 
            justifyContent: "center", 
            alignItems: "center", 
            gap: "6px",
            padding: "8px", 
            background: "rgba(0,0,0,0.02)", 
            borderTop: "1px solid var(--border-soft)",
            color: "var(--fg-dim)",
            fontSize: "12px",
            cursor: "pointer",
            transition: "all 0.2s"
          }}
          onClick={refresh}
        >
          <RefreshCw size={12} className={loading ? "spin" : ""} />
          刷新状态
        </div>
      </div>
    </div>
  );
}

// ===========================================================================
// MailView (Vp) — 邮件列表 + probe 状态
// ===========================================================================

function MailView() {
  const t = useT();
  const [probe, setProbe] = useState<MailProbeResult | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [openKey, setOpenKey] = useState<string | null>(null);
  // folder: "inbox" (unread INBOX) or "sent" (Sent folder). Drives which mailbox
  // InboxPreview reads and the tab label.
  const [folder, setFolder] = useState<"inbox" | "sent">("inbox");

  // Cache both folders so switching inbox/sent is instant (no re-fetch).
  const [inboxData, setInboxData] = useState<InboxItem[]>([]);
  const [sentData, setSentData] = useState<InboxItem[]>([]);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      // Fetch both folders at once (probe + inbox 30 + sent 10). This runs on
      // mount and on explicit refresh-button click — NOT on folder switch, so
      // switching tabs is instant.
      const [mb, inb, sent] = await Promise.all([
        (app as unknown as { ProbeMailAccount: (name: string) => Promise<MailProbeResult> })
          .ProbeMailAccount("")
          .catch(() => ({ ok: false, status: "error", message: "Wails调用异常" } as MailProbeResult)),
        (app as unknown as { InboxPreview?: (mailbox: string, n: number) => Promise<InboxItem[]> })
          .InboxPreview?.("INBOX", 30) ??
          Promise.resolve([] as InboxItem[]),
        (app as unknown as { InboxPreview?: (mailbox: string, n: number) => Promise<InboxItem[]> })
          .InboxPreview?.("Sent", 10) ??
          Promise.resolve([] as InboxItem[]),
      ]);
      setProbe(mb);
      setInboxData(inb);
      setSentData(sent);
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setLoading(false);
    }
  }, []);

  // Load once on mount only (not on folder switch).
  useEffect(() => {
    refresh();
  }, [refresh]);

  // The displayed list comes from the cached folder data — instant switch.
  const inbox = folder === "sent" ? sentData : inboxData;

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
          onClick={() => refresh()}
          title={t("common.refresh") || "刷新"}
        >
          <RefreshCw size={13} className={loading ? "spin" : ""} />
        </button>
      </div>

      {/* Inbox / Sent folder switch. Only show when mail is configured. */}
      {!mailUnconfigured && !error && (
        <div className="cowork-mailtab__folders">
          <button
            className={"cowork-mailtab__folder" + (folder === "inbox" ? " cowork-mailtab__folder--active" : "")}
            onClick={() => setFolder("inbox")}
          >
            {t("coworkDock.mailInbox") || "收件箱"}
          </button>
          <button
            className={"cowork-mailtab__folder" + (folder === "sent" ? " cowork-mailtab__folder--active" : "")}
            onClick={() => setFolder("sent")}
          >
            {t("coworkDock.mailSent") || "发件箱"}
          </button>
        </div>
      )}

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
                    {/* Inbox shows sender (from); Sent shows recipient (to). */}
                    <span className="cowork-mailtab__from" title={folder === "sent" ? m.to : m.from}>
                      {folder === "sent" ? (m.to || "（未知收件人）") : m.from}
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

// 递归过滤 RagNodeView 树结构，若子树有匹配项则保留并展示父级文件夹
function filterRagTree(nodes: RagNodeView[], q: string): RagNodeView[] {
  if (!q || !q.trim()) return nodes;
  const lower = q.trim().toLowerCase();
  const res: RagNodeView[] = [];
  for (const node of nodes) {
    const selfMatch = node.label.toLowerCase().includes(lower) || (node.path && node.path.toLowerCase().includes(lower));
    const kidMatch = node.children ? filterRagTree(node.children, q) : undefined;
    if (selfMatch || (kidMatch && kidMatch.length > 0)) {
      res.push({
        ...node,
        children: kidMatch && kidMatch.length > 0 ? kidMatch : node.children,
      });
    }
  }
  return res;
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
  const [, setActiveCollections] = useState<string[] | null>(null);
  const [tree, setTree] = useState<RagNodeView[]>([]);
  const [entityName, setEntityName] = useState<string | null>(null);
  const [entityCollection, setEntityCollection] = useState<string | null>(null);
  const [docPath, setDocPath] = useState<string | null>(null);
  const [docCollection, setDocCollection] = useState("");
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [newCollectionName, setNewCollectionName] = useState("");
  const [newCollectionParent, setNewCollectionParent] = useState("");
  const { showToast } = useToast();
  const confirm = useConfirm();
  const [expandedCats, setExpandedCats] = useState<Set<string>>(new Set());
  const [catSearch, setCatSearch] = useState("");
  const [fileSearch, setFileSearch] = useState("");
  const [showImportModal, setShowImportModal] = useState(false);
  const [importTargetCol, setImportTargetCol] = useState("");
  const [allExpanded, setAllExpanded] = useState(true);
  // Pending delete-collection confirmation (null = closed). Replaces the native
  // window.confirm() with an app-styled modal.
  const [deleteTarget, setDeleteTarget] = useState<{ name: string; path: string } | null>(null);

  // 当在分类查询框中打字时，自动将含匹配分支的父目录加至展开集合，让层级在视觉上一目了然
  useEffect(() => {
    if (!catSearch.trim()) return;
    const q = catSearch.trim().toLowerCase();
    setExpandedCats((prev) => {
      const next = new Set(prev);
      for (const c of collections) {
        if (c.name.toLowerCase().includes(q) || (c.path && c.path.toLowerCase().includes(q))) {
          if (c.parent) next.add(c.parent);
        }
      }
      return next;
    });
  }, [catSearch, collections]);

  // Auto-expand root nodes only upon initial tree load, respecting user's toggle state thereafter.
  useEffect(() => {
    if (collections.length === 0) return;
    setExpandedCats((prev) => {
      if (prev.size > 0) return prev; // Do not override user's manual collapse/expand actions!
      const next = new Set<string>();
      const allPaths = new Set(collections.map((c) => c.path));
      for (const c of collections) {
        if (collections.some((child) => child.parent === c.path)) {
          next.add(c.path);
        }
        if (c.parent && !allPaths.has(c.parent)) {
          next.add(c.parent);
        }
      }
      return next;
    });
  }, [collections]);

  const toggleCat = (path: string) => {
    setExpandedCats((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  };

  // Import files directly into a specific collection from the dock via modern modal selection.
  const handleImportToCollection = (collectionName: string) => {
    setImportTargetCol(collectionName);
    setShowImportModal(true);
  };

  const refreshCollections = useCallback(() => {
    (app as unknown as { ListRagCollections: () => Promise<RagCollectionView[]> })
      .ListRagCollections()
      .then(setCollections)
      .catch(() => {});
  }, []);

  // Quick extract a single collection (incremental) without switching to the extract tab.
  const handleQuickExtract = async (collectionName: string) => {
    try {
      await app.RagStartExtract(collectionName, "general/graph", "incremental");
      showToast(`正在提取「${collectionName}」…`, "info");
    } catch (e) {
      const msg = String(e);
      if (msg.includes("已提取完成")) {
        showToast(`「${collectionName}」已全部提取完成`, "info");
      } else {
        showToast(`提取失败：${msg}`, "error");
      }
    }
  };

  // Drag-and-drop import into the dock — drops onto the collections area.
  const [dragOver, setDragOver] = useState(false);
  const handleDrop = async (e: React.DragEvent) => {
    e.preventDefault();
    setDragOver(false);
    // Wails delivers drops to the window-level onFilesDropped handler in RagPanel.
    // Here we just provide the visual drag highlight; the actual import is handled
    // by the existing bridge. The activeCollection at drop time determines target.
  };

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
          <div
            className={`rag-dock__collections ${dragOver ? "rag-dock__collections--drag" : ""}`}
            onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
            onDragLeave={() => setDragOver(false)}
            onDrop={(e) => void handleDrop(e)}
          >
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

            {/* 实时分类检索筛选框 (Live Category Filter Bar) - 像素级对齐 .mem-search 规范，置于所有树条目最上方 */}
            <div style={{ padding: "6px 8px 6px", borderBottom: "1px solid var(--border-soft)" }}>
              <label className="mem-search" style={{ height: 28, borderRadius: 6, background: "var(--bg-elev)" }}>
                <Search size={13} style={{ color: "var(--fg-dim)", flex: "0 0 auto" }} />
                <input
                  value={catSearch}
                  onChange={(e) => setCatSearch(e.target.value)}
                  placeholder="检索分类名称或层级..."
                  style={{ fontSize: "11.5px" }}
                />
                {catSearch && (
                  <button
                    type="button"
                    onClick={() => setCatSearch("")}
                    style={{ border: "none", background: "transparent", cursor: "pointer", color: "var(--fg-dim)", padding: 0, display: "inline-flex" }}
                  >
                    <X size={12} />
                  </button>
                )}
              </label>
            </div>

            {/* "全部" — search across all collections */}
            <div
              className={`rag-dock__collection-row ${activeCollection === "" ? "rag-dock__collection-row--active" : ""}`}
              onClick={() => {
                setActiveCollection("");
                setActiveCollections(null);
                window.dispatchEvent(new CustomEvent("rag:collection-selected", { detail: { collection: "" } }));
                (app as unknown as { SetSessionCollections: (c: string[]) => Promise<void> })
                  .SetSessionCollections([])
                  .catch(() => {});
              }}
              style={{ cursor: "pointer", paddingLeft: "6px", gap: "4px" }}
            >
              <button
                className="rag-dock__collection-chevron"
                onClick={(e) => { e.stopPropagation(); setAllExpanded(!allExpanded); }}
                style={{ border: "none", background: "transparent", cursor: "pointer", color: "var(--fg-faint)", display: "flex", alignItems: "center", justifyContent: "center", width: "12px", padding: 0 }}
              >
                {allExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
              </button>
              <Folder size={13} className="rag-dock__collection-icon" style={{ color: "var(--accent)" }} />
              <span className="rag-dock__collection-label" style={{ fontWeight: 600 }}>全部</span>
              <span className="rag-dock__collection-count">
                {collections.reduce((s, c) => s + c.documents, 0)} 文档
              </span>
            </div>

            {/* Build tree: root collections + their children */}
            {allExpanded && (() => {
              const qCat = catSearch.trim().toLowerCase();
              const filteredCols = !qCat ? collections : collections.filter((c) => {
                if (c.name.toLowerCase().includes(qCat) || (c.path && c.path.toLowerCase().includes(qCat))) return true;
                if (collections.some((child) => (child.name.toLowerCase().includes(qCat) || (child.path && child.path.toLowerCase().includes(qCat))) && (child.path.startsWith(c.path + "/") || child.parent === c.path))) {
                  return true;
                }
                return false;
              });

              if (filteredCols.length === 0 && qCat) {
                return (
                  <div style={{ padding: "24px 12px", textAlign: "center", color: "var(--fg-faint)", fontSize: "12px" }}>
                    未检索到匹配「{catSearch}」的分类
                  </div>
                );
              }

              // Build a true tree from paths
              interface TreeNode {
                name: string;
                path: string;
                documents: number;
                entities: number;
                children: Map<string, TreeNode>;
                isVirtual: boolean;
              }
              const tree = new Map<string, TreeNode>();
              
              filteredCols.forEach(c => {
                const parts = (c.path || c.name).split("/");
                let currPath = "";
                let currLevel = tree;
                parts.forEach((p, i) => {
                  currPath = currPath ? currPath + "/" + p : p;
                  if (!currLevel.has(p)) {
                    currLevel.set(p, {
                      name: p,
                      path: currPath,
                      documents: 0,
                      entities: 0,
                      children: new Map(),
                      isVirtual: true
                    });
                  }
                  const node = currLevel.get(p)!;
                  if (i === parts.length - 1) {
                    node.isVirtual = false;
                    node.documents = c.documents || 0;
                    node.entities = c.entities || 0;
                  }
                  currLevel = node.children;
                });
              });

              // Recursive render function
              const renderNode = (node: TreeNode, depth: number) => {
                const hasKids = node.children.size > 0;
                const expanded = expandedCats.has(node.path);
                const isActive = activeCollection === node.path;
                
                return (
                  <div key={node.path}>
                    <div
                      className={`rag-dock__collection-row ${isActive ? "rag-dock__collection-row--active" : ""}`}
                      onClick={() => {
                        if (hasKids && node.isVirtual) {
                           toggleCat(node.path);
                           return;
                        }
                        setActiveCollection(node.path);
                        window.dispatchEvent(new CustomEvent("rag:collection-selected", { detail: { collection: node.path } }));
                      }}
                      style={{ cursor: "pointer", paddingLeft: `${22 + depth * 14}px`, gap: "4px" }}
                      title={`${node.documents} 文档 · ${node.entities} 实体`}
                    >
                      {hasKids ? (
                        <button
                          className="rag-dock__collection-chevron"
                          onClick={(e) => { e.stopPropagation(); toggleCat(node.path); }}
                          style={{ border: "none", background: "transparent", cursor: "pointer", color: "var(--fg-faint)", display: "flex", alignItems: "center", justifyContent: "center", width: "12px", padding: 0 }}
                        >
                          {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                        </button>
                      ) : (
                        depth > 0 ? (
                          <span style={{ width: "12px", display: "flex", justifyContent: "center", alignItems: "center" }}>
                            <CornerDownRight size={10} style={{ color: "var(--fg-faint)", opacity: 0.6, marginTop: "-2px" }} />
                          </span>
                        ) : (
                          <span style={{ width: "12px" }} />
                        )
                      )}
                      <Folder size={13} className="rag-dock__collection-icon" style={{ opacity: node.isVirtual ? 0.6 : 1, color: depth > 0 ? "var(--fg-dim)" : "var(--fg)" }} />
                      <span className="rag-dock__collection-label" style={{ fontWeight: depth === 0 ? 500 : 400, color: depth > 0 ? "var(--fg-dim)" : "var(--fg)" }}>{node.name}</span>
                      <span className="rag-dock__collection-count">{node.documents > 0 ? node.documents : ""}</span>
                      
                      {!node.isVirtual && (
                        <>
                          <button className="rag-dock__collection-action-btn" title="导入" onClick={(e) => { e.stopPropagation(); void handleImportToCollection(node.path); }}>
                            <FolderPlus size={12} />
                          </button>
                          <button className="rag-dock__collection-action-btn" title="提取" onClick={(e) => { e.stopPropagation(); void handleQuickExtract(node.path); }}>
                            <Zap size={12} />
                          </button>
                          <button className="rag-dock__collection-delete" title="删除" onClick={(e) => {
                            e.stopPropagation();
                            setDeleteTarget({ name: node.name, path: node.path });
                          }}>
                            <Trash2 size={12} />
                          </button>
                        </>
                      )}
                    </div>
                    {expanded && hasKids && Array.from(node.children.values()).map(child => renderNode(child, depth + 1))}
                  </div>
                );
              };

              return Array.from(tree.values()).map(root => renderNode(root, 0));
            })()}

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
            collections={collections}
            onCollectionChange={(name) => {
              setActiveCollection(name);
              window.dispatchEvent(new CustomEvent("rag:collection-selected", { detail: { collection: name } }));
            }}
            onBack={() => setTab("files")}
          />
        )}

        {/* === 文件 tab === */}
        {tab === "files" && (
          <div className="rag-dock__files">
            {/* 简洁干练的分类切换级联菜单 (Select Dropdown) */}
            <div style={{ padding: "8px 12px", borderBottom: "1px solid var(--border-soft)" }}>
              <CustomSelect
                value={activeCollection}
                onChange={(val) => {
                  setActiveCollection(val);
                  window.dispatchEvent(new CustomEvent("rag:collection-selected", { detail: { collection: val } }));
                }}
                icon={<Folder size={14} style={{ color: "var(--accent)" }} />}
                options={[
                  {
                    value: "",
                    label: `全部文档 (${collections.reduce((acc, c) => acc + (c.documents || 0), 0)})`,
                    icon: <Folder size={13} style={{ color: "var(--accent)" }} />,
                  },
                  ...collections.map((c) => ({
                    value: c.path || c.name,
                    label: c.name,
                    subtitle: c.documents > 0 ? `${c.documents} 篇` : undefined,
                    indent: !!c.parent,
                    icon: <Folder size={13} />,
                  })),
                ]}
              />
            </div>

            {/* 实时文件检索过滤框 (Live File Search Bar) - 像素级对齐 .mem-search 规范 */}
            <div style={{ padding: "6px 8px 6px", borderBottom: "1px solid var(--border-soft)" }}>
              <label className="mem-search" style={{ height: 28, borderRadius: 6, background: "var(--bg-elev)" }}>
                <Search size={13} style={{ color: "var(--fg-dim)", flex: "0 0 auto" }} />
                <input
                  value={fileSearch}
                  onChange={(e) => setFileSearch(e.target.value)}
                  placeholder="检索当前列表文件或文档..."
                  style={{ fontSize: "11.5px" }}
                />
                {fileSearch && (
                  <button
                    type="button"
                    onClick={() => setFileSearch("")}
                    style={{ border: "none", background: "transparent", cursor: "pointer", color: "var(--fg-dim)", padding: 0, display: "inline-flex" }}
                  >
                    <X size={12} />
                  </button>
                )}
              </label>
            </div>

            {/* 当处于搜索过滤下，若未命中任何项则给出优雅空状态 */}
            {(() => {
              const displayTree = filterRagTree(tree, fileSearch);
              if (displayTree.length === 0 && tree.length > 0 && fileSearch.trim()) {
                return (
                  <div style={{ padding: "24px 12px", textAlign: "center", color: "var(--fg-faint)", fontSize: "12px" }}>
                    未检索到匹配「{fileSearch}」的文档
                  </div>
                );
              }
              return null;
            })()}

            {/* 文件列表 */}
            {tree.length === 0 ? (
              activeCollection ? (
                <div className="cowork-dock__empty-state">
                  <FileText size={22} />
                  <p>此分类暂无文件</p>
                  <button
                    className="btn btn--small btn--primary"
                    onClick={() => void handleImportToCollection(activeCollection)}
                  >
                    <FolderPlus size={14} />
                    <span>导入文件</span>
                  </button>
                </div>
              ) : (
                <div className="cowork-dock__empty-state">
                  <FileText size={22} />
                  <p>暂无文件</p>
                </div>
              )
            ) : (
              <div className="rag-dock__file-tree">
                    {filterRagTree(tree, fileSearch).map((node) => (
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
                          if (!n.path) return;
                          void confirm({ title: "删除提取的知识", message: `确定删除该文件已提取的知识？\n${n.path}\n\n文档本身不会被删除，可重新提取。` }).then((ok) => {
                            if (!ok) return;
                            (app as unknown as { RagRemovePath: (c: string, p: string) => Promise<void> })
                              .RagRemovePath(activeCollection, n.path)
                              .then(() => refreshTree())
                              .catch(() => refreshTree());
                          });
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

      <ImportModal
        isOpen={showImportModal}
        onClose={() => setShowImportModal(false)}
        collections={collections}
        defaultCollection={importTargetCol}
        onSuccess={() => {
          refreshCollections();
        }}
      />
      {deleteTarget && (
        <ConfirmModal
          title={`删除"${deleteTarget.name}"`}
          message="该分类及其全部文档、已抽取的知识图谱将被永久删除，此操作不可撤销。"
          onConfirm={() => {
            const path = deleteTarget.path;
            (app as unknown as { RagDeleteCollection: (n: string) => Promise<void> })
              .RagDeleteCollection(path)
              .then(() => refreshCollections())
              .catch(() => {});
          }}
          onClose={() => setDeleteTarget(null)}
        />
      )}
    </aside>
  );
}

// onRagProgress is imported above for API parity with bridge.ts. The dock
// subscribes via window.runtime.EventsOn directly (matching the bundle), but
// we keep the symbol referenced so tree-shaking does not drop the import and
// future migrations to the bridge helper are a one-line swap.
void onRagProgress;
