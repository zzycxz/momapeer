import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
import { ShellExpandProvider, useShellExpand } from "./lib/shellExpand";
import {
  Activity,
  Command,
  Download,
  SquarePen,
  FileDown,
  FileImage,
  FileText,
  FileJson,
  GitBranch,
  History,
  MessageSquare,
  Settings as SettingsIcon,
  Pencil,
  Trash2,
  Brain,
  Cpu,
  Palette,
  SlidersHorizontal,
} from "lucide-react";
import { useToast } from "./lib/toast";
import { useConfirm } from "./lib/confirm";
import { asArray } from "./lib/array";
import { clearLegacyLangPref, normalizeLangPref, readLegacyLangPref, useI18n, useT, type Translator } from "./lib/i18n";
import { useController, type Item, type LiveStream } from "./lib/useController";
import { app, onEvent, onProjectTreeChanged, onSchedulerNotice } from "./lib/bridge";
import { onProfileChanged } from "./lib/bridge";
import { CoWorkLayout } from "./layouts/CoWorkLayout";
import { PreferencePanel } from "./components/cowork/PreferencePanel";
import { Transcript } from "./components/Transcript";
import { ExpertSessionView } from "./components/cowork/ExpertSessionView";
import { Composer } from "./components/Composer";
import { TodoPanel } from "./components/TodoPanel";
import { ApprovalModal } from "./components/ApprovalModal";
import { AskCard } from "./components/AskCard";
import { ClearContextCard } from "./components/ClearContextCard";
import { StatusBar } from "./components/StatusBar";
import { SidebarFooter } from "./components/SidebarFooter";
import { HistoryPanel } from "./components/HistoryPanel";
import { CommandPalette, type PaletteItem } from "./components/CommandPalette";
import { SettingsPanel } from "./components/SettingsPanel";
import { ShortcutsCheatsheet } from "./components/ShortcutsCheatsheet";
import { useGlobalShortcut } from "./lib/keyboardShortcuts";
import { UpdateBanner } from "./components/UpdateBanner";
import { ContextPanel } from "./components/ContextPanel";
import { WorkspacePanel } from "./components/WorkspacePanel";
import { Tooltip } from "./components/Tooltip";
import { StartupSplash, shouldShowStartupSplash } from "./components/StartupSplash";
import { OnboardingOverlay } from "./components/OnboardingOverlay";
import { AppChrome } from "./components/AppChrome";
import { ProjectTree } from "./components/ProjectTree";
import { CopyButton } from "./components/CopyButton";
import { parseTodos } from "./lib/tools";
import { shouldShowTodoPanel } from "./lib/todoVisibility";
import {
  modeFromAxes,
  normalizeMode,
  normalizeToolApprovalMode,
  type BotConnectionView,
  type BotSettingsView,
  type CollaborationMode,
  type ComposerInsertRequest,
  type Mode,
  type ProjectNode,
  type SessionMeta,
  type SettingsTab,
  type SettingsView,
  type TabMeta,
  type ToolApprovalMode,
  type CapabilitiesView,
} from "./lib/types";
import { Box, Server } from "lucide-react";
import {
  controllerCollaborationMode,
  displayedCollaborationMode,
  keepGoalDraftMode,
  metaSyncedCollaborationMode,
  tabListCollaborationMode,
} from "./lib/goalDraftMode";
import {
  restorableToolApprovalMode,
  toggleYoloToolApprovalMode,
  type RestorableToolApprovalMode,
} from "./lib/toolApprovalMode";
import { loadLayoutSize, saveLayoutSize } from "./lib/layoutPreferences";
import { hydrateDisplayMode } from "./lib/displayMode";
import { blobToBase64, renderSessionImageBlob, renderSessionPdfBlob } from "./lib/sessionExport";
import { sessionActivityTime } from "./lib/session";
import {
  applyTheme,
  clearLegacyThemePreference,
  getTheme,
  getThemeStyle,
  isThemeStyle,
  normalizeThemePreference,
  normalizeThemeStyleForTheme,
  readLegacyThemePreference,
  type Theme,
} from "./lib/theme";
import { applyTextSize, DEFAULT_TEXT_SIZE, getTextSize, nextTextSize } from "./lib/textSize";
import { useWindowStatePersistence } from "./lib/windowState";
import { availableWorkspacePanelWidth, resolveWorkspacePanelWidth, workspacePanelAriaMinWidth } from "./lib/workspaceLayout";

const SIDEBAR_COLLAPSED_KEY = "momapeer.sidebar.collapsed";
const SIDEBAR_DEFAULT_WIDTH = 264;
const SIDEBAR_MIN_WIDTH = 264;
const SIDEBAR_MAX_WIDTH = 300;
const SIDEBAR_VIEWPORT_RATIO = 0.18;
const CHAT_MIN_WIDTH = 400;
const CHAT_COMFORT_MIN_WIDTH = 560;
const WORKSPACE_RESIZER_WIDTH = 8;

function isThemeMode(value: string): value is Theme {
  return value === "auto" || value === "light" || value === "dark";
}
const RIGHT_DOCK_TREE_DEFAULT_WIDTH = 300;
const RIGHT_DOCK_TREE_MIN_WIDTH = 300;
const RIGHT_DOCK_TREE_MAX_WIDTH = 560;
const RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH = 660;
const RIGHT_DOCK_PREVIEW_MIN_WIDTH = 420;
const RIGHT_DOCK_MIN_RENDER_WIDTH = 280;
const RIGHT_DOCK_MAX_WIDTH = 860;

type RightDockMode = "context" | "files" | "changed";
type WorkspaceRevealRequest = { id: number; path: string };
type WorkspaceFileListRequest = { id: number; paths: string[] };
type WorkspaceChangeListEntry = { key: string; path: string; meta: string; time: string; detail: string };
type WorkspaceChangeListRequest = { id: number; changes: WorkspaceChangeListEntry[] };
const SHOW_CONTEXT_DOCK = true;
type HistoryScopeFilter = { scope: "global" | "project"; workspaceRoot: string };
type DesktopPlatform = "darwin" | "windows" | "linux";
type HistoryViewState =
  | { kind: "history"; source: "scope"; filter: HistoryScopeFilter; sessions: SessionMeta[] }
  | { kind: "history"; source: "all"; sessions: SessionMeta[] }
  | { kind: "trash"; sessions: SessionMeta[] };
type SidebarImPlatform = "feishu" | "lark" | "weixin";
type SidebarImStatus = "connected" | "disabled" | "pending" | "error" | "disconnected";
type SidebarImConnection = {
  id: string;
  platform: SidebarImPlatform;
  title: string;
  platformLabel: string;
  subtitle: string;
  status: SidebarImStatus;
  statusLabel: string;
  remoteId: string;
  sessionId: string;
  scope: "global" | "project";
  workspaceRoot: string;
};
type SidebarImTopicSource = {
  platform: SidebarImPlatform;
  label: string;
  title: string;
  remoteId: string;
  connectionId: string;
};
type SidebarImConnectionDetailProps = {
  connection: SidebarImConnection;
  onClose: () => void;
  onOpenSession: () => void;
  onOpenSettings: () => void;
};

function isSidebarImConnection(connection: BotConnectionView): boolean {
  return connection.provider === "feishu" || connection.provider === "weixin";
}

function sidebarImPlatform(connection: BotConnectionView): SidebarImPlatform {
  if (connection.provider === "weixin") return "weixin";
  return connection.domain === "lark" ? "lark" : "feishu";
}

function sidebarImPlatformLabel(platform: SidebarImPlatform, translate: Translator): string {
  if (platform === "lark") return "Lark";
  if (platform === "weixin") return translate("settings.botWeixin");
  return translate("settings.botFeishu");
}

function firstBotSessionMapping(connection: BotConnectionView): BotConnectionView["sessionMappings"][number] | null {
  return (
    connection.sessionMappings.find((mapping) => mapping.sessionId.trim()) ??
    connection.sessionMappings.find((mapping) => mapping.remoteId.trim()) ??
    null
  );
}

function botMappingScope(mapping: BotConnectionView["sessionMappings"][number] | null | undefined, connectionWorkspaceRoot: string): "global" | "project" {
  if (mapping?.scope === "project") return "project";
  if ((mapping?.workspaceRoot ?? "").trim()) return "project";
  return connectionWorkspaceRoot.trim() ? "project" : "global";
}

function botMappingWorkspaceRoot(
  mapping: BotConnectionView["sessionMappings"][number] | null | undefined,
  connectionWorkspaceRoot: string,
): string {
  const workspaceRoot = (mapping?.workspaceRoot ?? "").trim() || connectionWorkspaceRoot.trim();
  return botMappingScope(mapping, connectionWorkspaceRoot) === "project" ? workspaceRoot : "";
}

function compactRemoteId(value: string): string {
  const trimmed = value.trim();
  if (trimmed.length <= 28) return trimmed;
  return `${trimmed.slice(0, 12)}…${trimmed.slice(-8)}`;
}

function sidebarImStatus(connection: BotConnectionView, botEnabled: boolean): SidebarImStatus {
  if (!botEnabled || !connection.enabled) return "disabled";
  if (connection.status === "connected") return "connected";
  if (connection.status === "pending") return "pending";
  if (connection.status === "error") return "error";
  return "disconnected";
}

function sidebarImStatusLabel(status: SidebarImStatus, translate: Translator): string {
  switch (status) {
    case "connected":
      return translate("sidebar.imConnected");
    case "disabled":
      return translate("sidebar.imDisabled");
    case "pending":
      return translate("sidebar.imPending");
    case "error":
      return translate("sidebar.imError");
    default:
      return translate("sidebar.imDisconnected");
  }
}

function sidebarImConnectionsFromBot(bot: BotSettingsView | null | undefined, translate: Translator): SidebarImConnection[] {
  if (!bot?.connections?.length) return [];
  return bot.connections
    .filter(isSidebarImConnection)
    .map((connection) => {
      const platform = sidebarImPlatform(connection);
      const platformLabel = sidebarImPlatformLabel(platform, translate);
      const mapping = firstBotSessionMapping(connection);
      const remoteId = mapping?.remoteId.trim() ?? "";
      const sessionId = mapping?.sessionId.trim() ?? "";
      const scope = botMappingScope(mapping, connection.workspaceRoot);
      const workspaceRoot = botMappingWorkspaceRoot(mapping, connection.workspaceRoot);
      const status = sidebarImStatus(connection, bot.enabled);
      const title = connection.label.trim() || platformLabel;
      const subtitleParts = [
        remoteId ? compactRemoteId(remoteId) : platformLabel,
        connection.model.trim() || "",
        sidebarImStatusLabel(status, translate),
      ].filter(Boolean);
      return {
        id: connection.id,
        platform,
        title,
        platformLabel,
        subtitle: subtitleParts.join(" · "),
        status,
        statusLabel: sidebarImStatusLabel(status, translate),
        remoteId,
        sessionId,
        scope,
        workspaceRoot,
      };
    });
}

function mappedSessionTarget(sessionId: string): { kind: "path" | "topic"; value: string } | null {
  const trimmed = sessionId.trim();
  if (!trimmed) return null;
  const lower = trimmed.toLowerCase();
  if (lower.startsWith("path:")) {
    const value = trimmed.slice(5).trim();
    return value ? { kind: "path", value } : null;
  }
  if (lower.startsWith("topic:")) {
    const value = trimmed.slice(6).trim();
    return value ? { kind: "topic", value } : null;
  }
  if (trimmed.endsWith(".jsonl") || trimmed.includes("/") || trimmed.includes("\\") || trimmed.startsWith("~")) {
    return { kind: "path", value: trimmed };
  }
  return { kind: "topic", value: trimmed };
}

function sidebarImTopicSourcesFromBot(bot: BotSettingsView | null | undefined, translate: Translator): Record<string, SidebarImTopicSource> {
  if (!bot?.connections?.length) return {};
  const sources: Record<string, SidebarImTopicSource> = {};
  for (const connection of bot.connections) {
    if (!isSidebarImConnection(connection)) continue;
    const platform = sidebarImPlatform(connection);
    const label = sidebarImPlatformLabel(platform, translate);
    const title = connection.label.trim() || label;
    for (const mapping of asArray(connection.sessionMappings)) {
      const scope = botMappingScope(mapping, connection.workspaceRoot);
      if (scope !== "global") continue;
      const target = mappedSessionTarget(mapping.sessionId);
      if (!target || target.kind !== "topic") continue;
      if (sources[target.value]) continue;
      sources[target.value] = {
        platform,
        label,
        title,
        remoteId: mapping.remoteId.trim(),
        connectionId: connection.id,
      };
    }
  }
  return sources;
}

function sidebarImScopeLabel(connection: SidebarImConnection, translate: Translator): string {
  if (connection.scope === "project") return translate("botDetail.scopeProject", { name: connection.workspaceRoot || "Project" });
  return translate("botDetail.scopeGlobal");
}

function sidebarImSessionLabel(connection: SidebarImConnection, translate: Translator): string {
  const target = mappedSessionTarget(connection.sessionId);
  if (!target) return translate("botDetail.noSession");
  if (target.kind === "path") return target.value.split(/[\\/]/).pop() || target.value;
  return target.value;
}

function SidebarImConnectionDetail({ connection, onClose, onOpenSession, onOpenSettings }: SidebarImConnectionDetailProps) {
  const translate = useT();
  const target = mappedSessionTarget(connection.sessionId);
  return (
    <div className="bot-detail">
      <section className="bot-detail__summary">
        <div className="bot-detail__summary-main">
          <span>{translate("botDetail.subtitle")}</span>
          <h2>{connection.title}</h2>
          <div className="bot-detail__chips">
            <span>{connection.platformLabel}</span>
            <span>{connection.statusLabel}</span>
            <span>{sidebarImScopeLabel(connection, translate)}</span>
          </div>
        </div>
        <div className="bot-detail__summary-actions">
          <button type="button" className="btn btn--primary btn--small bot-detail__primary" disabled={!target} title={target ? undefined : translate("botDetail.openDisabled")} onClick={onOpenSession}>
            <MessageSquare size={14} />
            {translate("botDetail.openSession")}
          </button>
          <button type="button" className="btn btn--secondary btn--small" onClick={onOpenSettings}>
            <SettingsIcon size={14} />
            {translate("botDetail.manage")}
          </button>
          <button type="button" className="btn btn--secondary btn--small" onClick={onClose}>
            {translate("botDetail.close")}
          </button>
        </div>
      </section>

      <section className="bot-detail__panel bot-detail__panel--facts" aria-label={translate("botDetail.summary")}>
        <div className="bot-detail__section-head">
          <span>{translate("botDetail.summary")}</span>
        </div>
        <div className="bot-detail__facts">
          <div>
            <span>{translate("botDetail.remoteId")}</span>
            <code>{connection.remoteId || "—"}</code>
          </div>
          <div>
            <span>{translate("botDetail.localTopic")}</span>
            <strong>{sidebarImSessionLabel(connection, translate)}</strong>
          </div>
          <div>
            <span>{translate("botDetail.scope")}</span>
            <strong>{sidebarImScopeLabel(connection, translate)}</strong>
          </div>
        </div>
      </section>
    </div>
  );
}

function activeTopicTurnsFromTree(tree: ProjectNode[], tab?: TabMeta): number | undefined {
  if (!tab?.topicId) return undefined;
  const targetScope = tab.scope === "global" ? "global" : "project";
  const walk = (nodes: ProjectNode[]): number | undefined => {
    for (const node of nodes) {
      if (!node) continue;
      if (node.kind === "topic" || node.kind === "global_topic") {
        const scope = node.kind === "global_topic" ? "global" : "project";
        if (
          scope === targetScope &&
          node.topicId === tab.topicId &&
          (scope === "global" || node.root === tab.workspaceRoot)
        ) {
          return node.turns;
        }
      }
      const found = walk(asArray(node.children));
      if (found !== undefined) return found;
    }
    return undefined;
  };
  return walk(tree);
}

function clampSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(width)));
}

function clampRightDockPreviewWidth(width: number): number {
  return Math.min(RIGHT_DOCK_MAX_WIDTH, Math.max(RIGHT_DOCK_PREVIEW_MIN_WIDTH, Math.round(width)));
}

function clampRightDockTreeWidth(width: number): number {
  return Math.min(RIGHT_DOCK_TREE_MAX_WIDTH, Math.max(RIGHT_DOCK_TREE_MIN_WIDTH, Math.round(width)));
}

function defaultSidebarWidth(): number {
  if (typeof window !== "undefined") {
    return clampSidebarWidth(window.innerWidth * SIDEBAR_VIEWPORT_RATIO);
  }
  return SIDEBAR_DEFAULT_WIDTH;
}

function defaultRightDockTreeWidth(): number {
  return RIGHT_DOCK_TREE_DEFAULT_WIDTH;
}

function loadSidebarCollapsed(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "1";
  } catch {
    return false;
  }
}

function saveSidebarCollapsed(collapsed: boolean): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, collapsed ? "1" : "0");
  } catch {
    /* ignore storage failures */
  }
}

function loadSidebarWidth(): number {
  return loadLayoutSize("sidebarWidthGraphite", defaultSidebarWidth(), clampSidebarWidth);
}

function saveSidebarWidth(width: number): void {
  saveLayoutSize("sidebarWidthGraphite", width, clampSidebarWidth);
}

function normalizeDesktopPlatform(value: string): DesktopPlatform {
  if (value === "darwin" || value === "windows") return value;
  return "linux";
}

function browserPlatformOverride(): DesktopPlatform | null {
  if (typeof window === "undefined" || window.runtime) return null;
  const value = new URLSearchParams(window.location.search).get("platform");
  if (value === "darwin" || value === "windows" || value === "linux") return value;
  return null;
}

function detectBrowserPlatform(): DesktopPlatform {
  const override = browserPlatformOverride();
  if (override) return override;
  if (typeof navigator === "undefined") return "linux";
  const marker = `${navigator.platform} ${navigator.userAgent}`;
  if (/Win/i.test(marker)) return "windows";
  if (/Mac/i.test(marker)) return "darwin";
  return "linux";
}

function loadRightDockTreeWidth(): number {
  return loadLayoutSize("rightDockTreeWidth", defaultRightDockTreeWidth(), clampRightDockTreeWidth);
}

function saveRightDockTreeWidth(width: number): void {
  saveLayoutSize("rightDockTreeWidth", width, clampRightDockTreeWidth);
}

function loadRightDockPreviewWidth(): number {
  return loadLayoutSize("rightDockPreviewWidth", RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH, clampRightDockPreviewWidth);
}

function saveRightDockPreviewWidth(width: number): void {
  saveLayoutSize("rightDockPreviewWidth", width, clampRightDockPreviewWidth);
}

function tabWorkspaceTitle(tab?: TabMeta): string {
  if (!tab) return "Global";
  if (tab.scope === "project") return tab.workspaceName || tab.workspaceRoot || "Project";
  if (tab.scope === "global") return tab.workspaceName || "Global";
  return tab.workspaceName || tab.workspaceRoot || "Global";
}

function topicTitle(tab?: TabMeta): string {
  if (!tab) return "Global";
  const workspaceTitle = tabWorkspaceTitle(tab);
  const topic = tab.topicTitle || (tab.scope === "global" ? workspaceTitle : "Untitled");
  return topic === workspaceTitle ? workspaceTitle : `${workspaceTitle} / ${topic}`;
}

function topicDisplayTitle(tab?: TabMeta): string {
  if (!tab) return "Global";
  return tab.topicTitle || (tab.scope === "global" ? tabWorkspaceTitle(tab) : "Untitled");
}

function sessionsForScope(sessions: SessionMeta[], filter: HistoryScopeFilter): SessionMeta[] {
  if (filter.scope === "project") {
    return sessions.filter((session) => session.scope === "project" && session.workspaceRoot === filter.workspaceRoot);
  }
  return sessions.filter((session) => (session.scope || "global") === "global");
}

function workspaceDisplayName(path?: string): string {
  if (!path) return "";
  const parts = path.split(/[/\\]/).filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : path;
}

function materializeLiveItems(items: Item[], live?: LiveStream): Item[] {
  if (!live) return items;
  return items.map((item) => {
    if (item.kind !== "assistant" || item.id !== live.id) return item;
    return { ...item, text: live.text, reasoning: live.reasoning, streaming: true };
  });
}

function fence(label: string, value: string): string {
  if (!value.trim()) return "";
  const fenceToken = value.includes("```") ? "````" : "```";
  return `${label}\n${fenceToken}\n${value.trim()}\n${fenceToken}`;
}

function sessionItemsToMarkdown(title: string, items: Item[], live?: LiveStream): string {
  const lines: string[] = [`# ${title.trim() || "momapeer session"}`, ""];
  for (const item of materializeLiveItems(items, live)) {
    switch (item.kind) {
      case "user":
        lines.push("## User", "", item.text.trim(), "");
        break;
      case "assistant":
        lines.push("## Assistant");
        if (item.reasoning.trim()) {
          lines.push("", "### Reasoning", "", item.reasoning.trim());
        }
        if (item.text.trim()) {
          lines.push("", item.text.trim());
        }
        lines.push("");
        break;
      case "tool":
        lines.push(`### Tool: ${item.name}`);
        if (item.args.trim()) lines.push("", fence("Args", item.args));
        if (item.output?.trim()) lines.push("", fence("Output", item.output));
        if (item.error?.trim()) lines.push("", fence("Error", item.error));
        lines.push("");
        break;
      case "phase":
        lines.push(`### Phase`, "", item.text.trim(), "");
        break;
      case "notice":
        lines.push(`### ${item.level === "warn" ? "Warning" : "Notice"}`, "", item.text.trim(), "");
        break;
      case "compaction":
        lines.push("### Context Compaction", "");
        if (item.pending) {
          lines.push("Compaction pending.");
        } else {
          lines.push(`Messages: ${item.messages}`);
          if (item.trigger) lines.push(`Trigger: ${item.trigger}`);
          if (item.summary.trim()) lines.push("", item.summary.trim());
        }
        lines.push("");
        break;
    }
  }
  return lines.join("\n").replace(/\n{3,}/g, "\n\n").trimEnd() + "\n";
}

function sessionItemsToJson(title: string, items: Item[], live?: LiveStream): string {
  return JSON.stringify(
    {
      title,
      exportedAt: new Date().toISOString(),
      items: materializeLiveItems(items, live),
    },
    null,
    2,
  );
}

function safeFilename(name: string): string {
  const cleaned = name.trim().replace(/[\\/:*?"<>|]+/g, "-").replace(/\s+/g, " ").slice(0, 80);
  return cleaned || "momapeer-session";
}

export default function App() {
  // profileRef mirrors the active tab's product profile ("dev"|"cowork"). It is
  // kept in sync by the coworkActive effect below and read via the getter passed
  // to useController so the controller's OpenProjectTab/OpenGlobalTab calls scope
  // the topic to the active profile without stale-closure issues.
  const profileRef = useRef<"dev" | "cowork">("dev");
  const {
    state,
    activeTabId,
    send,
    runShell,
    steer,
    notice,
    cancel,
    pauseToggle,
    approve,
    answerQuestion,
    setCollaborationMode: setControllerCollaborationMode,
    setToolApprovalMode: setControllerToolApprovalMode,
    setGoal: setControllerGoal,
    clearGoal: clearControllerGoal,
    setRagScope: setControllerRagScope,
    clearSession,
    listSessions,
    listTrashedSessions,
    resumeSession,
    previewSession,
    deleteSession,
    restoreSession,
    purgeTrashedSession,
    renameSession,
    refreshMeta,
    pickWorkspace,
    switchWorkspace,
    rewind,
    setModel,
    setEffort,
    switchTab,
    openProjectTab,
    openGlobalTab,
    closeTab,
    reorderTabs,
    syncActiveTab,
    ensureBlankTab,
  } = useController(() => profileRef.current);
  const { locale, setPref: setLocalePref } = useI18n();
  const t = useT();
  const [modesByTab, setModesByTab] = useState<Record<string, Mode>>({});
  const [collaborationModesByTab, setCollaborationModesByTab] = useState<Record<string, CollaborationMode>>({});
  const [toolApprovalModesByTab, setToolApprovalModesByTab] = useState<Record<string, ToolApprovalMode>>({});
  const yoloRestoreToolApprovalModesRef = useRef<Record<string, RestorableToolApprovalMode>>({});
  const [goalsByTab, setGoalsByTab] = useState<Record<string, string>>({});
  const [goalDraftModesByTab, setGoalDraftModesByTab] = useState<Record<string, boolean>>({});
  const [tabMetas, setTabMetas] = useState<TabMeta[]>([]);
  const [tabOrderIds, setTabOrderIds] = useState<string[]>([]);
  const [tabRevealSignal, setTabRevealSignal] = useState(0);
  // coworkActive tracks whether the ACTIVE tab is in the coWork product profile.
  // Driven by app.Profile() on active-tab change + the "profile:changed" event
  // emitted after a SwitchProfile rebuild. When true the standard three-pane body
  // is hidden (via the app--cowork class) and a CoWorkLayout is rendered instead.
  const [coworkActive, setCoworkActive] = useState(false);
  const [startupSplashVisible, setStartupSplashVisible] = useState<boolean>(() => shouldShowStartupSplash());
  // null until the mount probe resolves; true shows the overlay. Probed once —
  // clearing the key mid-session is the Settings panel's job, not the gate's.
  const [needsOnboarding, setNeedsOnboarding] = useState<boolean | null>(null);
  const [settingsTarget, setSettingsTarget] = useState<SettingsTab | null>(null);
  const [settingsPayload, setSettingsPayload] = useState<string | null>(null);
  const [startupUpdateChecksEnabled, setStartupUpdateChecksEnabled] = useState<boolean | null>(null);
  const [histView, setHistView] = useState<HistoryViewState | null>(null);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const [preferenceOpen, setPreferenceOpen] = useState(false);
  const [paletteSessions, setPaletteSessions] = useState<SessionMeta[]>([]);
  const [paletteCapabilities, setPaletteCapabilities] = useState<CapabilitiesView | null>(null);
  const { showToast } = useToast();
  const confirm = useConfirm();
  const [sidebarImConnections, setSidebarImConnections] = useState<SidebarImConnection[]>([]);
  const [imTopicSources, setImTopicSources] = useState<Record<string, SidebarImTopicSource>>({});
  const [sidebarImDetailConnectionId, setSidebarImDetailConnectionId] = useState("");
  const [sidebarCollapsed, setSidebarCollapsed] = useState(loadSidebarCollapsed);
  const [sidebarWidth, setSidebarWidth] = useState(loadSidebarWidth);
  const [sidebarResizing, setSidebarResizing] = useState(false);
  const [viewportWidth, setViewportWidth] = useState(() => (typeof window === "undefined" ? 1440 : window.innerWidth));
  const [workspacePanelOpen, setWorkspacePanelOpen] = useState(true);
  // Cowork dock has its OWN open state, separate from the coding-mode
  // workspacePanelOpen. Rationale: the cowork overview dock (今日/邮件/文件)
  // is the centerpiece of office mode — it should default open and NOT be
  // closed when the user shrinks the coding-mode workspace panel. Sharing one
  // state leaked coding-mode preferences into cowork (a user who collapsed the
  // dev dock would arrive at cowork with no right column at all).
  const [coworkDockOpen, setCoworkDockOpen] = useState(true);
  const [rightDockTreeWidth, setRightDockTreeWidth] = useState(loadRightDockTreeWidth);
  const [rightDockPreviewWidth, setRightDockPreviewWidth] = useState(loadRightDockPreviewWidth);
  const [workspacePreviewActive, setWorkspacePreviewActive] = useState(false);
  // Bump dockRefreshKey after each turn so WorkspacePanel/ContextPanel re-fetch
  // workspace changes, git history, and session metadata after AI tool writes.
  useEffect(() => {
    const unsub = onEvent((e) => {
      if (e.kind === "turn_done") {
        setDockRefreshKey((v) => v + 1);
      }
    });
    return unsub;
  }, []);

  const [workspacePanelResizing, setWorkspacePanelResizing] = useState(false);
  const [workspacePanelMaximized, setWorkspacePanelMaximized] = useState(false);
  const [rightDockMode, setRightDockMode] = useState<RightDockMode>("context");
  const [workspaceRevealRequest, setWorkspaceRevealRequest] = useState<WorkspaceRevealRequest | null>(null);
  const [workspaceChangeRevealRequest, setWorkspaceChangeRevealRequest] = useState<WorkspaceRevealRequest | null>(null);
  const [workspaceFileListRequest, setWorkspaceFileListRequest] = useState<WorkspaceFileListRequest | null>(null);
  const [workspaceChangeListRequest, setWorkspaceChangeListRequest] = useState<WorkspaceChangeListRequest | null>(null);
  const [dockRefreshKey, setDockRefreshKey] = useState(0);
  const [projectRevision, setProjectRevision] = useState(0);
  const [activeTopicTurns, setActiveTopicTurns] = useState<number | undefined>(undefined);
  const [composerInsertRequest, setComposerInsertRequest] = useState<ComposerInsertRequest | null>(null);
  const [transientOverlayDismissSignal, setTransientOverlayDismissSignal] = useState(0);
  const [desktopPlatform, setDesktopPlatform] = useState<DesktopPlatform>(detectBrowserPlatform);
  const [expandThinking, setExpandThinking] = useState(false);
  const [renamingTopicId, setRenamingTopicId] = useState<string | null>(null);
  const [topicTitleDraft, setTopicTitleDraft] = useState("");
  const [topicExportOpen, setTopicExportOpen] = useState(false);
  const [sidebarTogglePressed, setSidebarTogglePressed] = useState(false);
  const [workspaceTogglePressed, setWorkspaceTogglePressed] = useState(false);
  const [clearContextPending, setClearContextPending] = useState(false);
  const topicRenameSkipCommitRef = useRef(false);
  const topicRenameCommitHandledRef = useRef(false);
  const appRef = useRef<HTMLDivElement>(null);
  const sidebarTogglePressTimerRef = useRef<number | null>(null);
  const workspaceTogglePressTimerRef = useRef<number | null>(null);
  const sidebarImRefreshRef = useRef({ last: 0, inFlight: false });

  // Persist window geometry across launches.
  useWindowStatePersistence();

  useEffect(() => {
  }, []);

  const closeTransientOverlays = useCallback(() => {
    setTransientOverlayDismissSignal((signal) => signal + 1);
  }, []);

  const reloadSidebarImConnections = useCallback(async () => {
    const settings = await app.Settings();
    setSidebarImConnections(sidebarImConnectionsFromBot(settings.bot, t));
    setImTopicSources(sidebarImTopicSourcesFromBot(settings.bot, t));
  }, [t]);

  const quietlyRefreshSidebarImConnections = useCallback(() => {
    const now = Date.now();
    if (sidebarImRefreshRef.current.inFlight || now - sidebarImRefreshRef.current.last < 1000) return;
    sidebarImRefreshRef.current.inFlight = true;
    sidebarImRefreshRef.current.last = now;
    void reloadSidebarImConnections()
      .catch((e) => console.warn("bot sidebar refresh failed", e))
      .finally(() => {
        sidebarImRefreshRef.current.inFlight = false;
      });
  }, [reloadSidebarImConnections]);

  const openBotSettings = useCallback(() => {
    closeTransientOverlays();
    setSidebarImDetailConnectionId("");
    setSettingsTarget("bots");
  }, [closeTransientOverlays]);

  const pulseSidebarToggle = useCallback(() => {
    if (typeof window === "undefined") return;
    if (sidebarTogglePressTimerRef.current !== null) {
      window.clearTimeout(sidebarTogglePressTimerRef.current);
    }
    setSidebarTogglePressed(true);
    sidebarTogglePressTimerRef.current = window.setTimeout(() => {
      sidebarTogglePressTimerRef.current = null;
      setSidebarTogglePressed(false);
    }, 260);
  }, []);

  const pulseWorkspaceToggle = useCallback(() => {
    if (typeof window === "undefined") return;
    if (workspaceTogglePressTimerRef.current !== null) {
      window.clearTimeout(workspaceTogglePressTimerRef.current);
    }
    setWorkspaceTogglePressed(true);
    workspaceTogglePressTimerRef.current = window.setTimeout(() => {
      workspaceTogglePressTimerRef.current = null;
      setWorkspaceTogglePressed(false);
    }, 260);
  }, []);

  const anchorAppScrollToChat = useCallback(() => {
    if (typeof window === "undefined") return;
    const el = appRef.current;
    if (!el) return;
    const pin = () => {
      el.scrollLeft = 0;
    };
    pin();
    window.requestAnimationFrame(pin);
    window.setTimeout(pin, 300);
  }, []);

  useEffect(() => {
    return () => {
      if (sidebarTogglePressTimerRef.current !== null) {
        window.clearTimeout(sidebarTogglePressTimerRef.current);
      }
      if (workspaceTogglePressTimerRef.current !== null) {
        window.clearTimeout(workspaceTogglePressTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    const override = browserPlatformOverride();
    if (override) {
      setDesktopPlatform(override);
      return () => {
        cancelled = true;
      };
    }
    void app.Platform()
      .then((value) => {
        if (!cancelled) setDesktopPlatform(normalizeDesktopPlatform(value));
      })
      .catch((e) => {
        console.warn("platform probe failed", e);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const applyDesktopPreferences = useCallback(
    (settings: Pick<SettingsView, "desktopTheme" | "desktopThemeStyle" | "desktopLanguage" | "checkUpdates">) => {
      const nextTheme = normalizeThemePreference(settings.desktopTheme);
      const nextStyle = normalizeThemeStyleForTheme(settings.desktopThemeStyle, nextTheme);
      applyTheme(nextTheme, nextStyle, { persist: false });
      setLocalePref(normalizeLangPref(settings.desktopLanguage));
      setStartupUpdateChecksEnabled(settings.checkUpdates !== false);
    },
    [setLocalePref],
  );

  useEffect(() => {
    let cancelled = false;
    const syncDesktopPreferences = async () => {
      const legacyLanguage = readLegacyLangPref();
      const legacyTheme = readLegacyThemePreference();
      if (legacyLanguage || legacyTheme.hasValue) {
        await app.MigrateDesktopPreferences(legacyLanguage, legacyTheme.theme, legacyTheme.style);
        clearLegacyLangPref();
        clearLegacyThemePreference();
      }
      const settings = await app.Settings();
      if (cancelled) return;
      applyDesktopPreferences(settings);
      setExpandThinking(settings.expandThinking);
      hydrateDisplayMode(settings.displayMode);
      setSidebarImConnections(sidebarImConnectionsFromBot(settings.bot, t));
      setImTopicSources(sidebarImTopicSourcesFromBot(settings.bot, t));
    };
    void syncDesktopPreferences().catch((e) => {
      console.warn("desktop preferences sync failed", e);
      setStartupUpdateChecksEnabled(true);
    });
    return () => {
      cancelled = true;
    };
  }, [applyDesktopPreferences, t]);

  useEffect(() => {
    setSidebarImDetailConnectionId((current) => {
      if (!current) return "";
      return sidebarImConnections.some((connection) => connection.id === current) ? current : "";
    });
  }, [sidebarImConnections]);

  // Open settings when the native menu item (CmdOrCtrl+,) is activated (Wails
  // backend event), OR when another frontend component (e.g. the dock's IM bot
  // row) dispatches a DOM CustomEvent to open a specific settings tab.
  useEffect(() => {
    // Backend event (keyboard shortcut) — always opens "general".
    if (typeof window !== "undefined" && window.runtime) {
      window.runtime.EventsOn("app:open-settings", () => {
        closeTransientOverlays();
        setSettingsTarget("general");
      });
    }
    // Frontend DOM event (e.g. dock → settings → bots). detail carries the tab.
    const validTabs = ["general", "models", "bots", "cowork", "mcp", "skills", "memory", "permissions", "sandbox", "network", "hooks", "appearance", "updates"];
    const handler = (e: Event) => {
      closeTransientOverlays();
      const tab = (e as CustomEvent<string>).detail;
      setSettingsTarget((validTabs.includes(tab) ? tab : "general") as SettingsTab);
    };
    window.addEventListener("app:open-settings-tab", handler as EventListener);
    return () => window.removeEventListener("app:open-settings-tab", handler as EventListener);
  }, [closeTransientOverlays]);

  // Screenshot hotkey recognition results — surface as a toast so the user sees
  // the VLM output even when MoMAPeer isn't focused.
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("screenshot:notice", (...data: unknown[]) => {
      const e = (data?.[0] ?? {}) as { message?: string };
      if (e.message) showToast(e.message, "info");
    });
  }, [showToast]);

  // Emergency-stop hotkey confirmation — the global Ctrl+Shift+Pause fired and
  // cancelled the in-flight turn. Surface a prominent (error-level = red) toast
  // so the user gets immediate visible confirmation the stop landed, even when
  // MoMAPeer is in the background (the event is emitted from the backend).
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("estop:fired", (...data: unknown[]) => {
      const e = (data?.[0] ?? {}) as { message?: string };
      showToast(e.message ?? "已紧急停止 AI 操作", "error");
    });
  }, [showToast]);

  // Global scheduler notice: when a scheduled task with output_mode="notify"
  // fires, the backend emits "scheduler:notice". This listener is registered at
  // the app root (not inside AutomationPanel/CalendarTaskPanel) so the toast
  // surfaces regardless of which panel the user is currently viewing —
  // previously the notice was only visible while the automation/calendar panel
  // was mounted, and switching to experts/rag/chat silently swallowed it.
  useEffect(() => {
    return onSchedulerNotice((e) => {
      showToast(`${e.name}: ${(e.result || "").slice(0, 100)}`, "info");
    });
  }, [showToast]);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const onResize = () => setViewportWidth(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  // Per-tab pending plan revision text. Keyed by tab id so a revision drafted
  // in one tab is never sent to another (previously a single global value that
  // could leak across tabs when the active tab changed).
  const [pendingPlanRevisionsByTab, setPendingPlanRevisionsByTab] = useState<Record<string, string>>({});
  const [footerHeight, setFooterHeight] = useState(0);
  const footerHeightRef = useRef(0);
  const footerRef = useRef<HTMLElement>(null);
  const runningRef = useRef(state.running);
  const rightDockDetailActive = rightDockMode !== "context" && workspacePreviewActive;
  const preferredWorkspacePanelWidth = rightDockDetailActive ? rightDockPreviewWidth : rightDockTreeWidth;
  const workspacePanelMinWidth = rightDockDetailActive ? RIGHT_DOCK_PREVIEW_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH;
  const chatReservedWidth = workspacePanelOpen && !workspacePanelMaximized ? CHAT_COMFORT_MIN_WIDTH : CHAT_MIN_WIDTH;
  const workspacePanelAvailableWidth = availableWorkspacePanelWidth({
    viewportWidth,
    sidebarCollapsed,
    sidebarWidth,
    chatMinWidth: chatReservedWidth,
    resizerWidth: WORKSPACE_RESIZER_WIDTH,
  });

  const resolvedWorkspacePanelWidth = resolveWorkspacePanelWidth({
    open: workspacePanelOpen,
    maximized: workspacePanelMaximized,
    preferredWidth: preferredWorkspacePanelWidth,
    minWidth: workspacePanelMinWidth,
    availableWidth: workspacePanelAvailableWidth,
  });

  const workspacePanelRenderable =
    workspacePanelOpen && (workspacePanelMaximized || resolvedWorkspacePanelWidth >= RIGHT_DOCK_MIN_RENDER_WIDTH);
  const workspacePanelGridOpen = workspacePanelRenderable && !workspacePanelMaximized;
  const workspacePanelRenderWidth = workspacePanelMaximized ? preferredWorkspacePanelWidth : resolvedWorkspacePanelWidth;
  // The cowork dock reuses the same width resolution (so resize/maximize behave
  // consistently) but pairs it with the cowork-specific open state. So a coding-
  // mode panel close doesn't hide the cowork overview.
  const coworkDockRenderable =
    coworkDockOpen && (workspacePanelMaximized || resolvedWorkspacePanelWidth >= RIGHT_DOCK_MIN_RENDER_WIDTH);
  const activeTab = useMemo(
    () => tabMetas.find((tab) => tab.id === activeTabId) ?? tabMetas.find((tab) => tab.active),
    [activeTabId, tabMetas],
  );
  const sidebarImDetailConnection = useMemo(
    () => sidebarImConnections.find((connection) => connection.id === sidebarImDetailConnectionId) ?? null,
    [sidebarImConnections, sidebarImDetailConnectionId],
  );
  useEffect(() => {
    let cancelled = false;
    if (!activeTab?.topicId) {
      setActiveTopicTurns(undefined);
      return () => {
        cancelled = true;
      };
    }
    void app.ListProjectTree(profileRef.current)
      .then((tree) => {
        if (!cancelled) setActiveTopicTurns(activeTopicTurnsFromTree(asArray(tree), activeTab));
      })
      .catch(() => {
        if (!cancelled) setActiveTopicTurns(undefined);
      });
    return () => {
      cancelled = true;
    };
  }, [activeTab?.scope, activeTab?.topicId, activeTab?.workspaceRoot, projectRevision]);
  const sessionTurns = useMemo(() => {
    const visibleUserTurns = state.items.reduce((count, item) => (item.kind === "user" ? count + 1 : count), 0);
    const currentTabTurns = Math.max(state.checkpoints.length, visibleUserTurns);
    return currentTabTurns > 0 ? currentTabTurns : activeTopicTurns ?? 0;
  }, [activeTopicTurns, state.checkpoints.length, state.items]);
  const startupSplashHold = state.meta?.ready !== true && !state.meta?.startupErr;
  const legacyMode = activeTabId ? modesByTab[activeTabId] ?? "normal" : "normal";
  const goal = activeTabId ? goalsByTab[activeTabId] ?? state.meta?.goal ?? activeTab?.goal ?? "" : "";
  const goalDraftMode = activeTabId ? Boolean(goalDraftModesByTab[activeTabId]) : false;
  const collaborationMode = activeTabId
    ? displayedCollaborationMode({
        goalDraftMode,
        localMode: collaborationModesByTab[activeTabId],
        metaGoal: state.meta?.goal,
        tabMode: activeTab?.collaborationMode,
        goal,
        legacyMode,
      })
    : "normal";
  const toolApprovalMode = activeTabId
    ? toolApprovalModesByTab[activeTabId] ?? normalizeToolApprovalMode(state.meta?.toolApprovalMode ?? activeTab?.toolApprovalMode, legacyMode, state.meta?.autoApproveTools ?? state.meta?.bypass)
    : "ask";
  // Knowledge-base auto-injection scope, per-tab ("" = 不使用 / off, default).
  // Sourced directly from backend meta since the selection is persisted there;
  // refreshMetaForTab re-syncs after a change.
  const ragScope = state.meta?.ragScope ?? "";
  const controllerReady = state.meta?.ready === true;
  const setMode = useCallback(
    (next: Mode | ((prev: Mode) => Mode)) => {
      if (!activeTabId) return;
      setModesByTab((current) => {
        const prev = current[activeTabId] ?? "normal";
        const value = typeof next === "function" ? next(prev) : next;
        if (value === prev) return current;
        return { ...current, [activeTabId]: value };
      });
    },
    [activeTabId],
  );
  const setGoalDraftModeForTab = useCallback((tabId: string, enabled: boolean) => {
    setGoalDraftModesByTab((current) => {
      if (Boolean(current[tabId]) === enabled) return current;
      if (enabled) return { ...current, [tabId]: true };
      const next = { ...current };
      delete next[tabId];
      return next;
    });
  }, []);
  const topicbarEditing = Boolean(activeTab?.topicId && activeTab.topicId === renamingTopicId);
  const visibleTabId = activeTabId;
  const visibleTabs = useMemo(() => {
    const byId = new Map(tabMetas.map((tab) => [tab.id, tab]));
    const ordered = tabOrderIds.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
    const missing = tabMetas.filter((tab) => !tabOrderIds.includes(tab.id));
    return [...ordered, ...missing].map((tab) => ({
      ...tab,
      running: tab.id === visibleTabId ? tab.running || state.running : tab.running,
      mode: modesByTab[tab.id] ?? normalizeMode(tab.mode),
      collaborationMode: tabListCollaborationMode({
        goalDraftMode: Boolean(goalDraftModesByTab[tab.id]),
        localMode: collaborationModesByTab[tab.id],
        tabMode: tab.collaborationMode,
        tabGoal: goalsByTab[tab.id] ?? tab.goal,
        legacyMode: normalizeMode(tab.mode),
      }),
      toolApprovalMode: toolApprovalModesByTab[tab.id] ?? normalizeToolApprovalMode(tab.toolApprovalMode, normalizeMode(tab.mode), tab.toolApprovalMode === "yolo"),
      goal: goalsByTab[tab.id] ?? tab.goal ?? "",
      active: tab.id === visibleTabId,
    }));
  }, [collaborationModesByTab, goalDraftModesByTab, goalsByTab, modesByTab, state.running, tabMetas, tabOrderIds, toolApprovalModesByTab, visibleTabId]);

  useEffect(() => {
    const ids = tabMetas.map((tab) => tab.id);
    setTabOrderIds((current) => {
      const next = current.filter((id) => ids.includes(id));
      for (const id of ids) {
        if (!next.includes(id)) next.push(id);
      }
      return next.join("\u0000") === current.join("\u0000") ? current : next;
    });
  }, [tabMetas]);

  useEffect(() => {
    const ids = new Set(tabMetas.map((tab) => tab.id));
    for (const id of Object.keys(yoloRestoreToolApprovalModesRef.current)) {
      if (!ids.has(id)) delete yoloRestoreToolApprovalModesRef.current[id];
    }
    setGoalDraftModesByTab((current) => {
      let changed = false;
      const next: Record<string, boolean> = {};
      for (const tab of tabMetas) {
        if (keepGoalDraftMode(Boolean(current[tab.id]), tab.goal)) {
          next[tab.id] = true;
        } else if (current[tab.id]) {
          changed = true;
        }
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
    setModesByTab((current) => {
      let changed = false;
      const next: Record<string, Mode> = {};
      for (const tab of tabMetas) {
        const mode = normalizeMode(tab.mode);
        next[tab.id] = mode;
        if (current[tab.id] !== mode) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
    setCollaborationModesByTab((current) => {
      let changed = false;
      const next: Record<string, CollaborationMode> = {};
      for (const tab of tabMetas) {
        const value = tabListCollaborationMode({
          goalDraftMode: keepGoalDraftMode(Boolean(goalDraftModesByTab[tab.id]), tab.goal),
          tabMode: tab.collaborationMode,
          tabGoal: tab.goal,
          legacyMode: normalizeMode(tab.mode),
        });
        next[tab.id] = value;
        if (current[tab.id] !== value) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
    setToolApprovalModesByTab((current) => {
      let changed = false;
      const next: Record<string, ToolApprovalMode> = {};
      for (const tab of tabMetas) {
        const value = normalizeToolApprovalMode(tab.toolApprovalMode, normalizeMode(tab.mode));
        next[tab.id] = value;
        if (current[tab.id] !== value) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
    setGoalsByTab((current) => {
      let changed = false;
      const next: Record<string, string> = {};
      for (const tab of tabMetas) {
        const value = tab.goal ?? "";
        next[tab.id] = value;
        if (current[tab.id] !== value) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
  }, [goalDraftModesByTab, tabMetas]);

  useEffect(() => {
    if (!renamingTopicId || activeTab?.topicId === renamingTopicId) return;
    topicRenameSkipCommitRef.current = false;
    topicRenameCommitHandledRef.current = false;
    setRenamingTopicId(null);
    setTopicTitleDraft("");
  }, [activeTab?.topicId, renamingTopicId]);

  useEffect(() => {
    if (!activeTabId || !state.meta) return;
    const nextGoal = state.meta.goalStatus === "running" ? state.meta.goal ?? "" : "";
    if (nextGoal) setGoalDraftModeForTab(activeTabId, false);
    setGoalsByTab((current) => (current[activeTabId] === nextGoal ? current : { ...current, [activeTabId]: nextGoal }));
    setCollaborationModesByTab((current) => {
      const nextMode = metaSyncedCollaborationMode({ nextGoal, goalDraftMode, legacyMode });
      return current[activeTabId] === nextMode ? current : { ...current, [activeTabId]: nextMode };
    });
  }, [activeTabId, goalDraftMode, legacyMode, setGoalDraftModeForTab, state.meta]);

  useEffect(() => {
    void app.SetTrayLocale(locale).catch(() => {});
  }, [locale]);

  const applyCollaborationMode = useCallback(
    (m: CollaborationMode) => {
      if (!activeTabId) return;
      if (m === "goal") {
        setGoalDraftModeForTab(activeTabId, true);
        setCollaborationModesByTab((current) => (current[activeTabId] === "goal" ? current : { ...current, [activeTabId]: "goal" }));
        setMode(modeFromAxes(false, toolApprovalMode === "yolo"));
        void setControllerCollaborationMode("normal");
        return;
      }
      setGoalDraftModeForTab(activeTabId, false);
      setCollaborationModesByTab((current) => (current[activeTabId] === m ? current : { ...current, [activeTabId]: m }));
      if (m === "normal" || m === "plan") {
        setGoalsByTab((current) => (current[activeTabId] ? { ...current, [activeTabId]: "" } : current));
      }
      setMode(modeFromAxes(m === "plan", toolApprovalMode === "yolo"));
      void setControllerCollaborationMode(m);
    },
    [activeTabId, setControllerCollaborationMode, setGoalDraftModeForTab, setMode, toolApprovalMode],
  );
  const applyToolApprovalMode = useCallback(
    (m: ToolApprovalMode) => {
      if (!activeTabId) return;
      if (m === "yolo") {
        if (toolApprovalMode !== "yolo") {
          yoloRestoreToolApprovalModesRef.current[activeTabId] = restorableToolApprovalMode(toolApprovalMode);
        }
      } else {
        yoloRestoreToolApprovalModesRef.current[activeTabId] = restorableToolApprovalMode(m);
      }
      setToolApprovalModesByTab((current) => (current[activeTabId] === m ? current : { ...current, [activeTabId]: m }));
      setMode(modeFromAxes(collaborationMode === "plan", m === "yolo"));
      void setControllerToolApprovalMode(m);
    },
    [activeTabId, collaborationMode, setControllerToolApprovalMode, setMode, toolApprovalMode],
  );
  const toggleYoloApprovalMode = useCallback(() => {
    if (!activeTabId) return;
    const next = toggleYoloToolApprovalMode(
      toolApprovalMode,
      yoloRestoreToolApprovalModesRef.current[activeTabId],
    );
    if (next.restore) {
      yoloRestoreToolApprovalModesRef.current[activeTabId] = next.restore;
    }
    applyToolApprovalMode(next.mode);
  }, [activeTabId, applyToolApprovalMode, toolApprovalMode]);
  const applyGoal = useCallback(
    (nextGoal: string) => {
      if (!activeTabId) return;
      const trimmed = nextGoal.trim();
      setGoalDraftModeForTab(activeTabId, false);
      setGoalsByTab((current) => (current[activeTabId] === trimmed ? current : { ...current, [activeTabId]: trimmed }));
      setCollaborationModesByTab((current) => {
        const nextMode = trimmed ? "goal" : "normal";
        return current[activeTabId] === nextMode ? current : { ...current, [activeTabId]: nextMode };
      });
      setMode(modeFromAxes(false, toolApprovalMode === "yolo"));
      void (trimmed ? setControllerGoal(trimmed) : clearControllerGoal());
    },
    [activeTabId, clearControllerGoal, setControllerGoal, setGoalDraftModeForTab, setMode, toolApprovalMode],
  );
  // Shift+Tab toggles only the collaboration axis; Ctrl/Cmd+Y toggles YOLO on the
  // tool-permission axis while preserving the Ask/Auto base mode.
  const cycleMode = useCallback(() => {
    applyCollaborationMode(collaborationMode === "plan" ? "normal" : "plan");
  }, [applyCollaborationMode, collaborationMode]);

  // Switching models rebuilds the controller, which starts in normal mode — so
  // re-apply the current mode, or the pill would say plan/YOLO while the fresh
  // controller silently uses normal gating.
  const switchModel = useCallback(
    async (name: string) => {
      await setModel(name);
      await setControllerCollaborationMode(controllerCollaborationMode({ collaborationMode, goal }));
      await setControllerToolApprovalMode(toolApprovalMode);
      if (goal.trim()) await setControllerGoal(goal);
    },
    [collaborationMode, goal, setControllerCollaborationMode, setControllerGoal, setControllerToolApprovalMode, setModel, toolApprovalMode],
  );

  // switchProfile rebuilds the active tab's controller with a product profile
  // bundle (dev/cowork). The Go side carries conversation history across the
  // rebuild (see app.SwitchProfileForTab); we optimistically flip coworkActive so
  // the layout swaps without waiting for the profile:changed event, and revert on
  // error. The composer/mode/goal re-application is unnecessary here — the rebuild
  // preserves them via applyTabModeToController on the Go side.
  // switchProfile implements VIEW-SWITCH semantics: toggling dev/cowork does NOT
  // convert the current tab to the other profile. Instead it finds an existing
  // tab of the target profile (preferring the same scope/workspaceRoot) and
  // activates it, or creates a fresh blank tab in the target profile if none
  // exists. The current tab stays as-is in its own profile — so a dev tab is
  // never lost or relabeled when you peek at cowork, and switching back is just
  // clicking the dev tab again. The sidebar follows the active tab's profile
  // automatically (see the active-tab-sync effect below).
  const switchProfile = useCallback(
    async (name: string) => {
      const targetProfile = name.toLowerCase() === "cowork" ? "cowork" : "dev";
      // Close any open modal/overlay (History panel, transient popovers) — their
      // cached content belongs to the outgoing profile's view and would be stale.
      closeTransientOverlays();
      setHistView(null);
      // Prefer a tab matching the active tab's scope/workspaceRoot so the view
      // switch lands in the same project; fall back to any tab of that profile.
      const curScope = activeTab?.scope === "project" ? "project" : "global";
      const curRoot = activeTab?.scope === "project" ? (activeTab.workspaceRoot ?? "") : "";
      const match = tabMetas.find(
        (t) => (t.profile ?? "dev").toLowerCase() === targetProfile &&
          t.scope === curScope &&
          (curScope === "global" || t.workspaceRoot === curRoot),
      );
      const anyMatch = match ?? tabMetas.find(
        (t) => (t.profile ?? "dev").toLowerCase() === targetProfile,
      );
      try {
        if (anyMatch) {
          // Activate the existing target-profile tab. Tab activation drives
          // coworkActive/profileRef/sidebar via the active-tab-sync effect, so
          // no manual layout flip is needed.
          await switchTab(anyMatch.id);
        } else {
          // No tab of the target profile exists yet — create a blank one. The
          // explicit profile arg ensures it boots in the target mode regardless
          // of the current tab's profile.
          await ensureBlankTab(curScope, curRoot, targetProfile);
        }
        // Optimistically flip the layout so the view swaps immediately, without
        // waiting for the backend's profile:changed event to round-trip. The
        // active-tab-sync effect re-confirms (or corrects) coworkActive once the
        // new tab's profile field arrives; this just removes the dead time. In
        // the browser dev mock (no Go backend) this flip is what makes the
        // CoWorkLayout actually render, since the event never fires.
        const nextCowork = targetProfile === "cowork";
        setCoworkActive(nextCowork);
        profileRef.current = nextCowork ? "cowork" : "dev";
        setProjectRevision((v) => v + 1);
      } catch (err) {
        console.error("[switchProfile] failed:", err);
        if (String(err).includes("finish or cancel the current turn") || String(err).includes("turn")) {
          notice("请先停止当前运行的任务（如分析截图等），再切换模式！", "warn");
        } else {
          notice("切换模式失败: " + String(err), "warn");
        }
      }
    },
    [activeTab, tabMetas, switchTab, ensureBlankTab, closeTransientOverlays, notice],
  );

  // Startup and workspace/model rebuilds create a fresh controller in normal
  // mode. Re-apply the UI mode once the controller is ready, including the case
  // where the user picked YOLO while boot was still loading and the legacy
  // SetBypass binding was a harmless no-op.
  useEffect(() => {
    if (!controllerReady) return;
    void setControllerCollaborationMode(controllerCollaborationMode({ collaborationMode, goal }));
    void setControllerToolApprovalMode(toolApprovalMode);
    if (goal.trim()) void setControllerGoal(goal);
  }, [collaborationMode, controllerReady, goal, setControllerCollaborationMode, setControllerGoal, setControllerToolApprovalMode, toolApprovalMode]);

  // The live task list pinned above the composer comes from the most recent
  // successful top-level todo_write result; failed or still-running attempts do
  // not advance the canonical panel state. It stays visible through the final
  // all-completed update, and can be dismissed by the user (the ✕). A dismissal
  // is keyed to that list's id, so a fresh accepted todo_write brings the panel
  // back.
  const todoEntry = useMemo(() => {
    for (let i = state.items.length - 1; i >= 0; i--) {
      const it = state.items[i];
      if (it.kind === "tool" && it.name === "todo_write" && !it.parentId && it.status === "done" && !it.error) {
        return { item: it, index: i };
      }
    }
    return null;
  }, [state.items]);
  const todoItem = todoEntry?.item ?? null;
  const todos = useMemo(() => (todoItem ? parseTodos(todoItem.args) : []), [todoItem]);
  const [dismissedTodo, setDismissedTodo] = useState<string | null>(null);
  const showTodos = shouldShowTodoPanel(todoItem?.id, dismissedTodo, todos);

  const sessionTitle = topicTitle(activeTab);
  const sessionHasContent = state.items.length > 0 || Boolean(state.live?.text || state.live?.reasoning);
  const getSessionMarkdown = useCallback(
    () => sessionItemsToMarkdown(sessionTitle, state.items, state.live),
    [sessionTitle, state.items, state.live],
  );
  const getSessionJson = useCallback(
    () => sessionItemsToJson(sessionTitle, state.items, state.live),
    [sessionTitle, state.items, state.live],
  );

  useEffect(() => {
    if (!topicExportOpen) return;
    const onDown = (event: MouseEvent) => {
      const target = event.target as Element | null;
      if (!target?.closest(".topicbar__export")) setTopicExportOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [topicExportOpen]);

  const exportSession = useCallback(
    async (format: "markdown" | "json" | "pdf" | "image") => {
      const base = safeFilename(sessionTitle);
      setTopicExportOpen(false);
      try {
        if (format === "json") {
          const path = await app.PickExportFile(`${base}.json`, "application/json");
          if (path) await app.SaveExportFile(path, getSessionJson(), false);
        } else if (format === "pdf") {
          const path = await app.PickExportFile(`${base}.pdf`, "application/pdf");
          if (!path) return;
          const blob = await renderSessionPdfBlob(getSessionMarkdown(), sessionTitle);
          await app.SaveExportFile(path, await blobToBase64(blob), true);
        } else if (format === "image") {
          const path = await app.PickExportFile(`${base}.png`, "image/png");
          if (!path) return;
          const blob = await renderSessionImageBlob(getSessionMarkdown());
          await app.SaveExportFile(path, await blobToBase64(blob), true);
        } else {
          const path = await app.PickExportFile(`${base}.md`, "text/markdown");
          if (path) await app.SaveExportFile(path, getSessionMarkdown(), false);
        }
      } catch (err) {
        console.error("Failed to export session", err);
      }
    },
    [getSessionJson, getSessionMarkdown, sessionTitle],
  );

  // Drain the active tab's pending plan revision once it stops running. Keyed
  // per-tab so a revision drafted in tab A can't leak into tab B's send.
  useEffect(() => {
    if (!activeTabId || state.running) return;
    const text = pendingPlanRevisionsByTab[activeTabId];
    if (!text) return;
    setPendingPlanRevisionsByTab((current) => {
      if (!(activeTabId in current)) return current;
      const next = { ...current };
      delete next[activeTabId];
      return next;
    });
    send(text);
  }, [activeTabId, pendingPlanRevisionsByTab, send, state.running]);

  useEffect(() => {
    setClearContextPending(false);
  }, [activeTabId]);

  const cancelClearContext = useCallback(() => {
    setClearContextPending(false);
  }, []);

  const confirmClearContext = useCallback(async () => {
    setClearContextPending(false);
    try {
      await clearSession();
      notice(t("clearContext.done"));
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      notice(msg || t("clearContext.failed"), "warn");
    }
  }, [clearSession, notice, t]);

  // Keep runningRef in sync so handleSend sees the latest running value
  // even inside a stale closure.
  useEffect(() => {
    runningRef.current = state.running;
  }, [state.running]);

  // handleSend intercepts slash commands that need a desktop-native action before
  // they reach the backend: "/model <ref>" rebuilds on that model, "/memory"
  // opens Settings, and "/clear" shows an in-app confirmation card. Everything else — skills (/init, …),
  // custom commands, bare /model and the other read-only management verbs
  // (/skill, /hooks, /mcp) — goes straight to Submit, which the controller
  // resolves (a turn, or a listing Notice).
  const handleSend = useCallback(
    async (displayText: string, submitText = displayText) => {
      const trimmed = displayText.trim();
      // "!<cmd>" runs a shell command directly, bypassing the model.
      if (trimmed.startsWith("!")) {
        const cmd = trimmed.slice(1).trim();
        if (!cmd) {
          notice("usage: !<command>  (e.g. !ls -la)");
          return;
        }
        runShell(cmd);
        return;
      }
      const model = /^\/model\s+(\S+)$/.exec(trimmed);
      if (model) {
        void switchModel(model[1]);
        return;
      }
      if (trimmed === "/memory") {
        closeTransientOverlays();
        setSettingsTarget("memory");
        return;
      }
      if (trimmed === "/clear") {
        setClearContextPending(true);
        return;
      }
      const goalCommand = /^\/goal(?:\s+(.*))?$/.exec(trimmed);
      if (goalCommand) {
        const arg = (goalCommand[1] ?? "").trim();
        if (arg && !["status", "clear", "off", "stop", "done"].includes(arg.toLowerCase())) {
          applyGoal(arg);
        } else if (["clear", "off", "stop", "done"].includes(arg.toLowerCase())) {
          applyGoal("");
        }
        send(trimmed, submitText.trim());
        return;
      }
      if (collaborationMode === "goal" && !goal.trim()) {
        applyGoal(trimmed);
        send(trimmed, `/goal ${submitText.trim()}`);
        return;
      }
      const theme = /^\/theme(?:\s+(\S+))?$/.exec(trimmed);
      if (theme) {
        const arg = theme[1]?.toLowerCase();
        if (!arg) {
          const cur = getTheme();
          notice(t("settings.themeCurrent", { theme: cur, style: getThemeStyle(cur) }));
          return;
        }
        if (isThemeMode(arg)) {
          const next = arg;
          const style = getThemeStyle(next);
          await app.SetDesktopAppearance(next, style);
          applyTheme(next, style);
          notice(t("settings.themeChanged", { theme: next, style }));
          return;
        }
        if (isThemeStyle(arg)) {
          const cur = getTheme();
          await app.SetDesktopAppearance(cur, arg);
          applyTheme(cur, arg);
          notice(t("settings.themeChanged", { theme: cur, style: arg }));
          return;
        }
        notice(t("settings.themeUnknown", { name: arg }), "warn");
        return;
      }
      if (runningRef.current) { steer(submitText.trim()); return; }
      await setControllerCollaborationMode(controllerCollaborationMode({ collaborationMode, goal }));
      await setControllerToolApprovalMode(toolApprovalMode);
      if (goal.trim()) await setControllerGoal(goal);
      send(trimmed, submitText.trim());
    },
    [applyGoal, closeTransientOverlays, collaborationMode, goal, send, runShell, notice, setControllerCollaborationMode, setControllerGoal, setControllerToolApprovalMode, steer, switchModel, t, toolApprovalMode],
  );

  const refreshTabMetas = useCallback(async (): Promise<TabMeta[]> => {
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    setTabMetas(tabs);
    return tabs;
  }, []);

  const blankSessionTarget = useCallback(() => {
    const activeWorkspaceRoot = activeTab?.scope === "project" ? activeTab.workspaceRoot || "" : "";
    const scope = activeWorkspaceRoot ? "project" : "global";
    return { scope, workspaceRoot: activeWorkspaceRoot };
  }, [activeTab?.scope, activeTab?.workspaceRoot]);

  const openBlankSession = useCallback(async (scope: string, workspaceRoot: string) => {
    window.dispatchEvent(new CustomEvent("cowork:reset-panel"));
    await ensureBlankTab(scope, scope === "project" ? workspaceRoot : "", profileRef.current);
    setProjectRevision((value) => value + 1);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [ensureBlankTab, refreshTabMetas]);

  useEffect(() => {
    void refreshTabMetas();
    const id = window.setInterval(() => void refreshTabMetas(), 2000);
    return () => window.clearInterval(id);
  }, [refreshTabMetas]);

  useEffect(() => {
    return onProjectTreeChanged(() => {
      setProjectRevision((value) => value + 1);
      void refreshTabMetas();
    });
  }, [refreshTabMetas]);

  // Refresh tab metas + sync active tab when cowork:reset-panel fires —
  // ExpertPanel dispatches it after RunExpertTeam (which opens an expert-session
  // tab and sets it active on the backend). Without this, the frontend's
  // activeTabId lags behind the backend, so the new expert tab appears in the
  // TabBar but isn't selected — the user still sees the old tab.
  useEffect(() => {
    const handler = () => {
      void refreshTabMetas();
      void syncActiveTab(true);
    };
    window.addEventListener("cowork:reset-panel", handler);
    return () => window.removeEventListener("cowork:reset-panel", handler);
  }, [refreshTabMetas, syncActiveTab]);

  // Sync coworkActive from the active tab's profile. Refetch on active-tab change
  // (each tab remembers its own profile) and on the profile:changed event (fired
  // after a SwitchProfile rebuild). The TabMeta.profile field is the per-tab
  // source of truth from refreshTabMetas; app.Profile() is the live backend read.
  useEffect(() => {
    const active = tabMetas.find((m) => m.id === activeTabId);
    if (active?.profile) {
      const isCowork = active.profile.toLowerCase() === "cowork";
      setCoworkActive(isCowork);
      profileRef.current = isCowork ? "cowork" : "dev";
      return;
    }
    // No tab meta yet (startup) — ask the backend directly.
    let cancelled = false;
    app
      .Profile()
      .then((name) => {
        const isCowork = (name ?? "").toLowerCase() === "cowork";
        if (!cancelled) {
          setCoworkActive(isCowork);
          profileRef.current = isCowork ? "cowork" : "dev";
        }
      })
      .catch(() => {
        /* dev mock / backend not ready */
      });
    return () => {
      cancelled = true;
    };
  }, [activeTabId, tabMetas]);

  useEffect(() => {
    return onProfileChanged((e) => {
      const isActive = e.tabId === activeTabId || !e.tabId;
      // Only adopt the change if it concerns the active tab — a background tab's
      // profile switch should not flip the visible layout.
      setCoworkActive((prev) => {
        if (e.profile.toLowerCase() === "cowork") return true;
        return prev && isActive ? false : prev;
      });
      // Keep the profile getter (handed to useController) live so subsequent
      // OpenProjectTab/OpenGlobalTab calls scope to the new profile.
      profileRef.current = e.profile.toLowerCase() === "cowork" ? "cowork" : "dev";
      // A profile switch rebuilds the active tab's controller, so the backend
      // re-emits agent:ready. The ready handler reloads with reset=false, which
      // leaves the previous profile's messages on screen when the new history is
      // empty. Explicitly reset+reload here so the chat list reflects the new
      // (possibly empty) session. Only the active tab's switch matters.
      if (isActive) void syncActiveTab(true);
      // Close any open History panel — its cached session list belongs to the
      // previous profile and would show the wrong conversations until reopened.
      setHistView(null);
    });
  }, [activeTabId, syncActiveTab]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const needs = await app.NeedsOnboarding();
        if (!cancelled) setNeedsOnboarding(needs);
      } catch {
        // Bridge unavailable (browser dev seam) — skip the gate; a real key
        // failure still surfaces via the topbar startupError banner.
        if (!cancelled) setNeedsOnboarding(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const el = footerRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    let frame = 0;
    const update = () => {
      if (frame) window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        const next = Math.round(el.getBoundingClientRect().height);
        if (Math.abs(footerHeightRef.current - next) < 2) return;
        footerHeightRef.current = next;
        setFooterHeight(next);
      });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, []);

  const toggleSidebar = useCallback(() => {
    closeTransientOverlays();
    pulseSidebarToggle();
    anchorAppScrollToChat();
    const nextCollapsed = !sidebarCollapsed;
    setSidebarCollapsed(nextCollapsed);
    saveSidebarCollapsed(nextCollapsed);
  }, [anchorAppScrollToChat, closeTransientOverlays, pulseSidebarToggle, sidebarCollapsed]);

  const setExpandedSidebarWidth = useCallback((width: number) => {
    closeTransientOverlays();
    const next = clampSidebarWidth(width);
    setSidebarWidth(next);
    saveSidebarWidth(next);
  }, [closeTransientOverlays]);

  const startSidebarResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (sidebarCollapsed) return;
      event.preventDefault();
      closeTransientOverlays();
      setSidebarResizing(true);
      let nextWidth = sidebarWidth;
      const onMove = (moveEvent: PointerEvent) => {
        nextWidth = clampSidebarWidth(moveEvent.clientX);
        setSidebarWidth(nextWidth);
      };
      const onDone = () => {
        setSidebarWidth(nextWidth);
        saveSidebarWidth(nextWidth);
        setSidebarResizing(false);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [closeTransientOverlays, sidebarCollapsed, sidebarWidth],
  );

  const resizeSidebarWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (sidebarCollapsed) return;
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setExpandedSidebarWidth(sidebarWidth + (event.key === "ArrowRight" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setExpandedSidebarWidth(SIDEBAR_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setExpandedSidebarWidth(SIDEBAR_MAX_WIDTH);
      }
    },
    [setExpandedSidebarWidth, sidebarCollapsed, sidebarWidth],
  );

  const setSavedWorkspacePanelWidth = useCallback(
    (width: number) => {
      closeTransientOverlays();
      if (rightDockDetailActive) {
        const next = clampRightDockPreviewWidth(width);
        setRightDockPreviewWidth(next);
        saveRightDockPreviewWidth(next);
        return;
      }
      const next = clampRightDockTreeWidth(width);
      setRightDockTreeWidth(next);
      saveRightDockTreeWidth(next);
    },
    [closeTransientOverlays, rightDockDetailActive],
  );

  const ensureWorkspacePanelWidth = useCallback(
    (width: number) => {
      closeTransientOverlays();
      if (rightDockMode === "context") return;
      const next = clampRightDockPreviewWidth(width);
      setRightDockPreviewWidth(next);
      saveRightDockPreviewWidth(next);
    },
    [closeTransientOverlays, rightDockMode],
  );

  const startWorkspacePanelResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (!workspacePanelOpen) return;
      event.preventDefault();
      closeTransientOverlays();
      setWorkspacePanelResizing(true);
      const startX = event.clientX;
      const startDockWidth = workspacePanelRenderWidth;
      let nextDockWidth = startDockWidth;
      const onMove = (moveEvent: PointerEvent) => {
        const delta = moveEvent.clientX - startX;
        nextDockWidth = startDockWidth - delta;
        if (rightDockDetailActive) {
          setRightDockPreviewWidth(clampRightDockPreviewWidth(nextDockWidth));
        } else {
          setRightDockTreeWidth(clampRightDockTreeWidth(nextDockWidth));
        }
      };
      const onDone = () => {
        setSavedWorkspacePanelWidth(nextDockWidth);
        setWorkspacePanelResizing(false);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [closeTransientOverlays, rightDockDetailActive, setSavedWorkspacePanelWidth, workspacePanelOpen, workspacePanelRenderWidth],
  );

  const resizeWorkspacePanelWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(workspacePanelRenderWidth + (event.key === "ArrowLeft" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(rightDockDetailActive ? RIGHT_DOCK_PREVIEW_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(rightDockDetailActive ? RIGHT_DOCK_MAX_WIDTH : RIGHT_DOCK_TREE_MAX_WIDTH);
      }
    },
    [rightDockDetailActive, setSavedWorkspacePanelWidth, workspacePanelRenderWidth],
  );

  const openWorkspacePanel = useCallback(
    (mode: RightDockMode = rightDockMode) => {
      closeTransientOverlays();
      if (mode === "context" || mode !== rightDockMode) {
        setWorkspacePreviewActive(false);
      }
      setRightDockMode(mode);
      let nextMaximized = workspacePanelMaximized;
      if (mode === "context") {
        nextMaximized = false;
        setWorkspacePanelMaximized(false);
      } else {
        // Keep file/change views docked; the rendered dock width is clamped to
        // the viewport so opening it reflows instead of forcing maximize.
        nextMaximized = false;
        setWorkspacePanelMaximized(false);
      }
      if (workspacePanelOpen && workspacePanelMaximized === nextMaximized) {
        return;
      }
      setWorkspacePanelOpen(true);
    },
    [closeTransientOverlays, rightDockMode, workspacePanelMaximized, workspacePanelOpen],
  );

  const closeWorkspacePanel = useCallback(() => {
    closeTransientOverlays();
    setCoworkDockOpen(false);
    if (!workspacePanelOpen) {
      return;
    }
    setWorkspacePanelMaximized(false);
    setWorkspacePanelOpen(false);
  }, [closeTransientOverlays, workspacePanelOpen]);

  const toggleWorkspacePanel = useCallback(() => {
    pulseWorkspaceToggle();
    // In cowork mode toggle the cowork-specific dock state, not the shared
    // coding-mode workspacePanelOpen — see coworkDockOpen comment above.
    if (coworkActive) {
      setCoworkDockOpen((open) => !open);
      return;
    }
    if (workspacePanelRenderable) {
      closeWorkspacePanel();
      return;
    }
    openWorkspacePanel("context");
  }, [closeWorkspacePanel, coworkActive, openWorkspacePanel, pulseWorkspaceToggle, workspacePanelRenderable]);

  const openRightDockMode = useCallback(
    (mode: RightDockMode) => {
      setWorkspaceRevealRequest(null);
      setWorkspaceChangeRevealRequest(null);
      setWorkspaceFileListRequest(null);
      setWorkspaceChangeListRequest(null);
      openWorkspacePanel(mode);
    },
    [openWorkspacePanel],
  );

  const openRightDockFile = useCallback(
    (path: string) => {
      const nextPath = path.trim();
      if (!nextPath) return;
      setWorkspaceFileListRequest(null);
      setWorkspaceChangeListRequest(null);
      setWorkspaceChangeRevealRequest(null);
      setWorkspaceRevealRequest((current) => ({ id: (current?.id ?? 0) + 1, path: nextPath }));
      openWorkspacePanel("files");
    },
    [openWorkspacePanel],
  );

  const openRightDockFileList = useCallback(
    (paths: string[]) => {
      const normalized = Array.from(new Set(paths.map((path) => path.trim()).filter(Boolean)));
      setWorkspaceRevealRequest(null);
      setWorkspaceChangeRevealRequest(null);
      setWorkspaceChangeListRequest(null);
      setWorkspaceFileListRequest((current) =>
        normalized.length > 0
          ? { id: (current?.id ?? 0) + 1, paths: normalized }
          : null,
      );
      openWorkspacePanel("files");
    },
    [openWorkspacePanel],
  );

  const openRightDockChangeFile = useCallback(
    (path: string) => {
      const nextPath = path.trim();
      if (!nextPath) return;
      setWorkspaceRevealRequest(null);
      setWorkspaceFileListRequest(null);
      setWorkspaceChangeListRequest(null);
      setWorkspaceChangeRevealRequest((current) => ({ id: (current?.id ?? 0) + 1, path: nextPath }));
      openWorkspacePanel("changed");
    },
    [openWorkspacePanel],
  );

  const openRightDockChangeList = useCallback(
    (changes: WorkspaceChangeListEntry[]) => {
      const seen = new Set<string>();
      const normalized = changes
        .map((change) => ({ ...change, path: change.path.trim() }))
        .filter((change) => {
          if (!change.path || seen.has(change.path)) return false;
          seen.add(change.path);
          return true;
      });
      setWorkspaceRevealRequest(null);
      setWorkspaceChangeRevealRequest(null);
      setWorkspaceFileListRequest(null);
      setWorkspaceChangeListRequest((current) =>
        normalized.length > 0
          ? { id: (current?.id ?? 0) + 1, changes: normalized }
          : null,
      );
      openWorkspacePanel("changed");
    },
    [openWorkspacePanel],
  );

  const handleWorkspacePreviewModeChange = useCallback(
    (active: boolean) => {
      if (workspacePreviewActive === active) return;
      closeTransientOverlays();
      setWorkspacePreviewActive(active);
    },
    [closeTransientOverlays, workspacePreviewActive],
  );

  const layoutStyle = useMemo(
    () =>
      ({
        "--sidebar-expanded-width": `${sidebarWidth}px`,
        "--chat-min-width": `${chatReservedWidth}px`,
        "--workspace-width": `${workspacePanelRenderWidth}px`,
        "--workspace-resizer-width": `${WORKSPACE_RESIZER_WIDTH}px`,
      }) as CSSProperties,
    [chatReservedWidth, sidebarWidth, workspacePanelRenderWidth],
  );

  const setWorkspacePanel = useCallback((open: boolean) => {
    if (open) {
      openWorkspacePanel();
    } else {
      closeWorkspacePanel();
    }
  }, [closeWorkspacePanel, openWorkspacePanel]);

  const addWorkspaceTextToComposer = useCallback((text: string) => {
    setComposerInsertRequest({ id: Date.now(), text });
  }, []);

  useEffect(() => {
    const handleCoworkInsert = (e: Event) => {
      const text = (e as CustomEvent<string>).detail;
      if (text) addWorkspaceTextToComposer(text);
    };
    window.addEventListener("cowork:insert-text", handleCoworkInsert as EventListener);
    return () => window.removeEventListener("cowork:insert-text", handleCoworkInsert as EventListener);
  }, [addWorkspaceTextToComposer]);

  const handleTabChange = useCallback(async (id: string) => {
    closeTransientOverlays();
    await switchTab(id);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [closeTransientOverlays, refreshTabMetas, switchTab]);

  const handleTabClose = useCallback(async (id: string) => {
    closeTransientOverlays();
    // Purge all per-tab mode maps so a closed tab leaves no residue. The
    // tabMetas sync effect (App.tsx ~L1031) would eventually reconcile, but
    // deleting up-front avoids stale state window during async refresh.
    const dropKey = <T extends Record<string, unknown>>(current: T): T => {
      if (!(id in current)) return current;
      const next = { ...current };
      delete next[id];
      return next;
    };
    setModesByTab(dropKey);
    setCollaborationModesByTab(dropKey);
    setToolApprovalModesByTab(dropKey);
    setGoalsByTab(dropKey);
    setGoalDraftModesByTab(dropKey);
    setPendingPlanRevisionsByTab(dropKey);
    delete yoloRestoreToolApprovalModesRef.current[id];
    setTabMetas((current) => {
      if (current.length <= 1) return current;
      const closingIndex = current.findIndex((tab) => tab.id === id);
      if (closingIndex < 0) return current;
      const closingTab = current[closingIndex];
      const remaining = current.filter((tab) => tab.id !== id);
      if (!closingTab.active && closingTab.id !== activeTabId) return remaining;
      const nextIndex = Math.min(closingIndex, remaining.length - 1);
      const nextActiveId = remaining[nextIndex]?.id;
      return remaining.map((tab) => ({ ...tab, active: tab.id === nextActiveId }));
    });
    await closeTab(id);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [activeTabId, closeTab, closeTransientOverlays, refreshTabMetas, yoloRestoreToolApprovalModesRef]);

  const handleTabsClose = useCallback(async (ids: string[], nextActiveTabId?: string) => {
    closeTransientOverlays();
    const currentIds = tabMetas.map((tab) => tab.id);
    const targets = ids.filter((id, index) => currentIds.includes(id) && ids.indexOf(id) === index);
    if (targets.length === 0) return;
    for (const id of targets) {
      await closeTab(id);
    }
    if (nextActiveTabId && currentIds.includes(nextActiveTabId)) {
      await switchTab(nextActiveTabId);
    }
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [closeTab, closeTransientOverlays, refreshTabMetas, switchTab, tabMetas]);

  const handleTabsReorder = useCallback(async (ids: string[]) => {
    setTabOrderIds(ids);
    setTabMetas((current) => {
      const byId = new Map(current.map((tab) => [tab.id, tab]));
      const ordered = ids.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
      return ordered.length === current.length ? ordered : current;
    });
    await reorderTabs(ids);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [refreshTabMetas, reorderTabs]);

  const handleNewTab = useCallback(async () => {
    closeTransientOverlays();
    setSidebarImDetailConnectionId("");
    const target = blankSessionTarget();
    await openBlankSession(target.scope, target.workspaceRoot);
  }, [blankSessionTarget, closeTransientOverlays, openBlankSession]);

  const handleMessageAction = useCallback(async (turn: number, scope: string) => {
    await rewind(turn, scope);
    if (scope === "fork") {
      await refreshTabMetas();
      setProjectRevision((value) => value + 1);
      setTabRevealSignal((signal) => signal + 1);
      return;
    }
    if (scope === "code" || scope === "both") {
      setDockRefreshKey((value) => value + 1);
      setProjectRevision((value) => value + 1);
    }
  }, [refreshTabMetas, rewind]);

  // runExpertFromSession triggers a new collaboration run on the team whose
  // expert-session tab is active. RunExpertTeam opens/activates the expert tab
  // and streams live; the result appends to the same session (multi-turn).
  // Search-cost confirmation is tracked per-team (a Set in a ref) so the user
  // is warned ONCE per team per session — not nagged on every follow-up.
  const searchCostConfirmedRef = useRef<Set<string>>(new Set());
  const runExpertFromSession = useCallback(async (teamId: string, task: string, mode: string, rounds: number) => {
    try {
      const teams = await app.ListExpertTeams().catch(() => []);
      const team = teams.find((tm) => tm.id === teamId);
      if (team?.allowSearch && !searchCostConfirmedRef.current.has(teamId)) {
        if (!(await confirm({ title: "搜索确认", message: t("cowork.expertSearchCostConfirm"), danger: false }))) return;
        searchCostConfirmedRef.current.add(teamId);
      }
      await app.RunExpertTeam(teamId, task, mode, rounds);
    } catch { /* ExpertSessionView shows error via stream events */ }
  }, [t]);

  const handleOpenTopic = useCallback(async (scope: string, workspaceRoot: string, topicId: string) => {
    window.dispatchEvent(new CustomEvent("cowork:reset-panel"));
    closeTransientOverlays();
    setSidebarImDetailConnectionId("");
    try {
      if (scope === "global") {
        await openGlobalTab(topicId);
      } else {
        await openProjectTab(workspaceRoot, topicId);
      }
    } catch (err) {
      // A cross-profile topicId (stale sidebar briefly showing the previous
      // profile's tree right after a switch) is rejected by the backend with a
      // "does not belong to the X profile" error. Refresh the sidebar so the
      // stale entry is gone, and surface a localized notice instead of the raw
      // English backend message.
      const msg = String(err);
      if (msg.includes("does not belong")) {
        setProjectRevision((v) => v + 1);
        await refreshTabMetas();
        showToast(t("cowork.topicNotInProfile"), "warn");
        return;
      }
      throw err;
    }
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [closeTransientOverlays, openGlobalTab, openProjectTab, refreshTabMetas, showToast, t]);

  const openSidebarImConnectionSession = useCallback(async (connection: SidebarImConnection) => {
    const target = mappedSessionTarget(connection.sessionId);
    if (!target) {
      showToast(t("sidebar.imWaiting", { name: connection.title }));
      return;
    }
    setSidebarImDetailConnectionId("");
    try {
      if (target.kind === "path") {
        const tab = await ensureBlankTab(connection.scope, connection.scope === "project" ? connection.workspaceRoot : "", profileRef.current);
        await resumeSession(target.value, tab.id);
      } else if (connection.scope === "project") {
        await openProjectTab(connection.workspaceRoot, target.value);
      } else {
        await openGlobalTab(target.value);
      }
      await refreshTabMetas();
      setProjectRevision((value) => value + 1);
      setTabRevealSignal((signal) => signal + 1);
    } catch (err) {
      console.warn("bot sidebar open failed", err);
      showToast(t("sidebar.imOpenFailed", { name: connection.title }));
    }
  }, [ensureBlankTab, openGlobalTab, openProjectTab, refreshTabMetas, resumeSession, showToast, t]);

  const showSidebarImDetail = useCallback(() => {
    closeTransientOverlays();
    if (sidebarImConnections.length === 0) {
      openBotSettings();
      return;
    }
    quietlyRefreshSidebarImConnections();
    setSidebarImDetailConnectionId(sidebarImConnections[0].id);
  }, [closeTransientOverlays, openBotSettings, quietlyRefreshSidebarImConnections, sidebarImConnections]);

  // History drawer: project menus can open a scoped saved-session list. Idle row
  // clicks resume; running row clicks only preview through PreviewSession.
  const openProjectHistory = useCallback(async (scope: "global" | "project", workspaceRoot: string) => {
    closeTransientOverlays();
    const filter = { scope, workspaceRoot };
    setHistView({ kind: "history", source: "scope", filter, sessions: sessionsForScope(await listSessions(), filter) });
  }, [closeTransientOverlays, listSessions]);
  const openAllHistory = useCallback(async () => {
    closeTransientOverlays();
    setHistView({ kind: "history", source: "all", sessions: await listSessions() });
  }, [closeTransientOverlays, listSessions]);
  const openTrash = useCallback(async () => {
    closeTransientOverlays();
    setHistView({ kind: "trash", sessions: await listTrashedSessions() });
  }, [closeTransientOverlays, listTrashedSessions]);
  const closeHistory = useCallback(() => {
    closeTransientOverlays();
    setHistView(null);
  }, [closeTransientOverlays]);

  const onResumeSession = useCallback(
    async (session: SessionMeta) => {
      window.dispatchEvent(new CustomEvent("cowork:reset-panel"));
      if (state.running) return;
      const scope = session.scope || (session.workspaceRoot ? "project" : "global");
      try {
        let targetTab: TabMeta;
        if (scope === "project" && session.workspaceRoot && session.topicId) {
          targetTab = await openProjectTab(session.workspaceRoot, session.topicId);
        } else if (scope === "global" && session.topicId) {
          targetTab = await openGlobalTab(session.topicId);
        } else {
          throw new Error(scope === "global" && !session.topicId
            ? t("history.failedOpenSession")
            : (session.topicId ? "Missing workspaceRoot" : t("history.failedOpenSession")));
        }
        setHistView(null);
        await resumeSession(session.path, targetTab.id);
        await refreshTabMetas();
        setTabRevealSignal((signal) => signal + 1);
      } catch (err: any) {
        setHistView(null);
        if (scope === "project" && session.workspaceRoot) {
          const name = workspaceDisplayName(session.workspaceRoot);
          showToast(t("history.failedOpenProject", { name, path: session.workspaceRoot }));
        } else {
          showToast(err?.message || String(err));
        }
      }
    },
    [openGlobalTab, openProjectTab, refreshTabMetas, state.running, resumeSession, t, showToast],
  );

  // Command palette: ⌘K / Ctrl+K opens a fuzzy navigator over commands and
  // recent sessions. Sessions are snapshotted on open so the list is stable
  // while the palette is up.
  const openPalette = useCallback(async () => {
    closeTransientOverlays();
    setPaletteOpen(true);
    setPaletteSessions(await listSessions().catch(() => []));
    setPaletteCapabilities(await app.Capabilities().catch(() => null));
  }, [closeTransientOverlays, listSessions]);
  // Global keyboard shortcuts — all wired through the centralized
  // keyboardShortcuts registry so they're self-documenting (Shift+? cheatsheet)
  // and share platform-aware matching, editable-target filtering, and a single
  // capture-phase listener per action. Replaces the old hand-rolled
  // ShellHotkeys / TextSizeHotkeys / CommandPalette Ctrl+K listeners.
  useGlobalShortcut("commandPalette.open", () => { if (!paletteOpen) void openPalette(); }, [paletteOpen, openPalette]);
  useGlobalShortcut("shortcuts.show", () => setShortcutsOpen(true));
  useGlobalShortcut("settings.open", () => setSettingsTarget("general"));
  useGlobalShortcut("app.newSession", () => { void handleNewTab(); });
  const shellExpand = useShellExpand();
  useGlobalShortcut("shell.toggle", () => shellExpand?.toggleLast(), [shellExpand]);
  useGlobalShortcut("textSize.increase", () => applyTextSize(nextTextSize(getTextSize(), 1)));
  useGlobalShortcut("textSize.decrease", () => applyTextSize(nextTextSize(getTextSize(), -1)));
  useGlobalShortcut("textSize.reset", () => applyTextSize(DEFAULT_TEXT_SIZE));
  // Pause/Resume the in-flight turn (graceful — finishes current step, freezes
  // with state preserved). Only fires while a turn is running; pauseToggle is a
  // no-op otherwise. Bound after pauseToggle is defined so the latest callback
  // is used.
  useGlobalShortcut("turn.pauseToggle", () => pauseToggle(), [pauseToggle]);
  const paletteItems = useMemo<PaletteItem[]>(() => {
    const cmds: PaletteItem[] = [
      { id: "cmd-new", group: t("palette.group.commands"), title: t("palette.cmd.newSession"), icon: <SquarePen size={15} />, compact: true, keywords: ["new", "新建"], run: () => void handleNewTab() },
      { id: "cmd-history", group: t("palette.group.commands"), title: t("palette.cmd.history"), icon: <History size={15} />, compact: true, keywords: ["history", "历史"], run: () => void openAllHistory() },
      { id: "cmd-trash", group: t("palette.group.commands"), title: t("palette.cmd.trash"), icon: <Trash2 size={15} />, compact: true, keywords: ["trash", "回收站"], run: () => void openTrash() },
      { id: "cmd-settings", group: t("palette.group.commands"), title: t("palette.cmd.settings"), icon: <SettingsIcon size={15} />, compact: true, keywords: ["settings", "设置"], run: () => setSettingsTarget("general") },
      { id: "cmd-appearance", group: t("palette.group.commands"), title: t("palette.cmd.appearance"), icon: <Palette size={15} />, compact: true, keywords: ["theme", "appearance", "外观", "主题"], run: () => setSettingsTarget("appearance") },
      { id: "cmd-memory", group: t("palette.group.commands"), title: t("palette.cmd.memory"), icon: <Brain size={15} />, compact: true, keywords: ["memory", "记忆"], run: () => setSettingsTarget("memory") },
      { id: "cmd-models", group: t("palette.group.commands"), title: t("palette.cmd.models"), icon: <Cpu size={15} />, compact: true, keywords: ["model", "模型"], run: () => setSettingsTarget("models") },
    ];
    const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
    const dayLabel = (ms: number) => {
      const days = Math.round((startOfDay(new Date()) - startOfDay(new Date(ms))) / 86_400_000);
      if (days <= 0) return t("history.today");
      if (days === 1) return t("history.yesterday");
      return new Date(ms).toLocaleDateString();
    };
    const sessionItems: PaletteItem[] = paletteSessions.slice(0, 12).map((s) => ({
      id: `sess-${s.path}`,
      group: t("palette.group.sessions"),
      title: s.title?.trim() || s.preview || t("history.emptySession"),
      hint: s.workspaceRoot || undefined,
      meta: dayLabel(sessionActivityTime(s)),
      badge: t(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns }),
      run: () => void onResumeSession(s),
    }));

    const skillItems: PaletteItem[] = (paletteCapabilities?.skills || []).map((sk) => ({
      id: `skill-${sk.name}`,
      group: t("settings.tab.skills"),
      title: sk.name,
      hint: sk.description,
      icon: <Box size={15} />,
      keywords: [sk.name, sk.runAs, sk.scope],
      run: () => {
        setSettingsTarget("skills");
        setSettingsPayload(sk.name);
      },
    }));

    const mcpItems: PaletteItem[] = (paletteCapabilities?.servers || []).map((srv) => ({
      id: `mcp-${srv.name}`,
      group: t("settings.tab.mcp"),
      title: srv.name,
      hint: srv.command,
      icon: <Server size={15} />,
      keywords: [srv.name, srv.transport],
      run: () => {
        setSettingsTarget("mcp");
        setSettingsPayload(srv.name);
      },
    }));

    return [...cmds, ...skillItems, ...mcpItems, ...sessionItems];
  }, [t, paletteSessions, paletteCapabilities, handleNewTab, openAllHistory, openTrash, onResumeSession]);
  // Delete / rename act on disk, then re-fetch so the panel reflects the change.
  const onDeleteSession = useCallback(
    async (path: string) => {
      if (state.running) return;
      await deleteSession(path);
      const sessions = await listSessions();
      setHistView((cur) =>
        cur === null
          ? null
          : cur.kind === "history"
            ? { ...cur, sessions: cur.source === "scope" ? sessionsForScope(sessions, cur.filter) : sessions }
            : cur,
      );
    },
    [state.running, deleteSession, listSessions],
  );
  const onRenameSession = useCallback(
    async (path: string, title: string) => {
      if (state.running) return;
      await renameSession(path, title);
      const sessions = await listSessions();
      setHistView((cur) =>
        cur === null
          ? null
          : cur.kind === "history"
            ? { ...cur, sessions: cur.source === "scope" ? sessionsForScope(sessions, cur.filter) : sessions }
            : cur,
      );
    },
    [state.running, renameSession, listSessions],
  );
  const onRestoreTrashedSession = useCallback(
    async (path: string) => {
      await restoreSession(path);
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [restoreSession, listTrashedSessions],
  );
  const onPurgeTrashedSession = useCallback(
    async (path: string) => {
      await purgeTrashedSession(path);
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [purgeTrashedSession, listTrashedSessions],
  );
  const onPurgeAllTrashedSessions = useCallback(
    async (paths: string[]) => {
      const uniquePaths = Array.from(new Set(paths));
      for (const path of uniquePaths) {
        await purgeTrashedSession(path);
      }
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [purgeTrashedSession, listTrashedSessions],
  );

  // Workspace: open the folder chooser and switch projects. The hook resets the
  // transcript and refreshes meta on a pick. A cancel is a no-op.
  const switchFolder = useCallback(async (path?: string) => {
    const picked = path === undefined ? await pickWorkspace() : await switchWorkspace(path);
    if (picked) {
      setProjectRevision((value) => value + 1);
      await refreshTabMetas();
    }
    return picked;
  }, [pickWorkspace, switchWorkspace, refreshTabMetas]);

  const refreshProjectsAndTabs = useCallback(async () => {
    setProjectRevision((value) => value + 1);
    const tabs = await refreshTabMetas();
    if (activeTabId && !tabs.some((tab) => tab.id === activeTabId)) {
      await syncActiveTab(true);
    }
  }, [activeTabId, refreshTabMetas, syncActiveTab]);

  const renameTopic = useCallback(async (topicId: string, title: string) => {
    const nextTitle = title.trim();
    if (!topicId || !nextTitle) return;
    await app.RenameTopic(topicId, nextTitle);
    await refreshProjectsAndTabs();
  }, [refreshProjectsAndTabs]);

  const startActiveTopicRename = useCallback(() => {
    if (!activeTab?.topicId) return;
    topicRenameSkipCommitRef.current = false;
    topicRenameCommitHandledRef.current = false;
    setRenamingTopicId(activeTab.topicId);
    setTopicTitleDraft(activeTab.topicTitle || "");
  }, [activeTab?.topicId, activeTab?.topicTitle]);

  const cancelActiveTopicRename = useCallback(() => {
    topicRenameSkipCommitRef.current = true;
    topicRenameCommitHandledRef.current = true;
    setRenamingTopicId(null);
    setTopicTitleDraft("");
  }, []);

  const commitActiveTopicRename = useCallback(async () => {
    if (topicRenameSkipCommitRef.current) {
      topicRenameSkipCommitRef.current = false;
      topicRenameCommitHandledRef.current = false;
      setRenamingTopicId(null);
      return;
    }
    if (topicRenameCommitHandledRef.current) return;
    topicRenameCommitHandledRef.current = true;
    const topicId = renamingTopicId;
    setRenamingTopicId(null);
    if (!topicId) return;
    const nextTitle = topicTitleDraft.trim();
    if (!nextTitle) return;
    try {
      await renameTopic(topicId, nextTitle);
    } catch {
      /* keep the app usable if a stale topic cannot be renamed */
    }
  }, [renameTopic, renamingTopicId, topicTitleDraft]);

  const sidebarExpandBlocked = false;
  const sidebarToggleTitle = sidebarCollapsed
      ? t("sidebar.expand")
      : t("sidebar.collapse");

  const browserPreviewChrome = typeof window !== "undefined" && !window.runtime;
  const workspacePanelResetWidth = rightDockDetailActive
    ? RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH
    : defaultRightDockTreeWidth();
  const workspacePanelResizeMinWidth = workspacePanelAriaMinWidth(workspacePanelMinWidth, workspacePanelRenderWidth);
  const workspacePanelMaxWidth = rightDockDetailActive ? RIGHT_DOCK_MAX_WIDTH : RIGHT_DOCK_TREE_MAX_WIDTH;
  const topicbarTitle = sidebarImDetailConnection ? t("botDetail.title", { name: sidebarImDetailConnection.title }) : topicDisplayTitle(activeTab);
  const topicbarWorkspaceLabel = sidebarImDetailConnection ? t("botDetail.subtitle") : activeTab ? tabWorkspaceTitle(activeTab) : "";
  const topicbarWorkspacePath = activeTab?.scope === "project" ? activeTab.workspaceRoot || state.meta?.cwd : "";
  const topicbarImSource = activeTab?.scope === "global" && activeTab.topicId ? imTopicSources[activeTab.topicId] : undefined;
  const topicbarImSourceLabel = sidebarImDetailConnection
    ? sidebarImDetailConnection.platformLabel
    : topicbarImSource ? t("msg.fromIm", { source: topicbarImSource.label }) : "";
  const topicbarImSourcePlatform = sidebarImDetailConnection?.platform ?? topicbarImSource?.platform;
  const topicbarSubtitleVisible = Boolean(topicbarWorkspaceLabel || topicbarImSourceLabel);
  const topicbarSubtitleTitle = sidebarImDetailConnection
    ? [topicbarWorkspaceLabel, topicbarImSourceLabel, sidebarImScopeLabel(sidebarImDetailConnection, t)].filter(Boolean).join(" · ")
    : [topicbarWorkspacePath || topicbarWorkspaceLabel, topicbarImSourceLabel].filter(Boolean).join(" · ");
  const sidebarImConnectedCount = sidebarImConnections.filter((connection) => connection.status === "connected").length;
  const sidebarImOnline = sidebarImConnectedCount > 0;

  // sessionActions is the Copy + Export control block. It is shared by two
  // render sites: the dev-mode topicbar (App.tsx below) and the CoWork header
  // (passed to CoWorkLayout as a prop). Both are in this render scope, so all
  // dependencies (getSessionMarkdown, exportSession, topicExportOpen,
  // sessionHasContent) close over the same state. Defining it once keeps the
  // two surfaces in sync and fixes the bug where Copy/Export were unreachable
  // in CoWork mode (the topicbar lives in .chat-pane, which is display:none
  // under .app--cowork). The .topicbar__export class is preserved so the
  // outside-click handler (App.tsx ~1313) keeps closing the dropdown.
  const sessionActions = (
    <>
      <CopyButton
        getText={getSessionMarkdown}
        label={t("topicBar.copyAll")}
        showLabel={false}
        className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
      />
      <div className={`topicbar__export${topicExportOpen ? " topicbar__export--open" : ""}`}>
        <Tooltip label={t("topicBar.export")}>
          <button
            className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
            type="button"
            disabled={!sessionHasContent}
            aria-label={t("topicBar.export")}
            aria-haspopup="menu"
            aria-expanded={topicExportOpen}
            onClick={() => setTopicExportOpen((open) => !open)}
          >
            <Download size={14} />
          </button>
        </Tooltip>
        {topicExportOpen && (
          <div className="topicbar__export-menu" role="menu">
            <button type="button" role="menuitem" onClick={() => void exportSession("markdown")}>
              <FileText size={13} />
              <span>{t("topicBar.exportMarkdown")}</span>
            </button>
            <button type="button" role="menuitem" onClick={() => void exportSession("json")}>
              <FileJson size={13} />
              <span>{t("topicBar.exportJson")}</span>
            </button>
            <button type="button" role="menuitem" onClick={() => void exportSession("pdf")}>
              <FileDown size={13} />
              <span>{t("topicBar.exportPdf")}</span>
            </button>
            <button type="button" role="menuitem" onClick={() => void exportSession("image")}>
              <FileImage size={13} />
              <span>{t("topicBar.exportImage")}</span>
            </button>
          </div>
        )}
      </div>
    </>
  );

  const headerNode = !preferenceOpen && (
    <header className="topicbar">
      <div className="topicbar__identity">
        <div className="topicbar__title-row">
          {topicbarEditing ? (
            <div className="topicbar__title-edit">
              <input
                autoFocus
                className="topicbar__title-input"
                value={topicTitleDraft}
                onChange={(event) => setTopicTitleDraft(event.target.value)}
                onKeyDown={(event: KeyboardEvent<HTMLInputElement>) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    void commitActiveTopicRename();
                  }
                  if (event.key === "Escape") {
                    event.preventDefault();
                    cancelActiveTopicRename();
                  }
                }}
                onBlur={() => void commitActiveTopicRename()}
              />
            </div>
          ) : (
            <h1 title={sidebarImDetailConnection ? topicbarTitle : topicTitle(activeTab)}>{topicbarTitle}</h1>
          )}
          <Tooltip label={t("topicBar.renameSession")}>
            <button
              className="topicbar__icon-btn"
              type="button"
              disabled={Boolean(sidebarImDetailConnection) || !activeTab?.topicId || topicbarEditing}
              onClick={startActiveTopicRename}
              aria-label={t("topicBar.renameSession")}
            >
              <Pencil size={14} />
            </button>
          </Tooltip>
        </div>
        {topicbarSubtitleVisible && (
          <div className="topicbar__subtitle" title={topicbarSubtitleTitle}>
            {topicbarWorkspaceLabel && <span>{topicbarWorkspaceLabel}</span>}
            {topicbarImSourcePlatform && (
              <span className={`topicbar__source-chip topicbar__source-chip--${topicbarImSourcePlatform}`}>
                {topicbarImSourceLabel}
              </span>
            )}
          </div>
        )}
      </div>
      <div className="topicbar__spacer" />
      <div className="topicbar__actions">
        {sessionActions}
        <Tooltip label={t("workspace.changedTab")}>
          <button
            className="topicbar__action-btn topicbar__action-btn--label"
            type="button"
            aria-label={t("workspace.changedTab")}
            aria-pressed={workspacePanelRenderable && rightDockMode === "changed"}
            onClick={() => openRightDockMode("changed")}
          >
            <GitBranch size={14} />
            <span>{t("workspace.changedTab")}</span>
          </button>
        </Tooltip>
        <Tooltip label={t("topicBar.command")}>
          <button
            className="topicbar__action-btn topicbar__action-btn--label topicbar__action-btn--accent"
            type="button"
            aria-label={t("topicBar.command")}
            onClick={() => void openPalette()}
          >
            <Command size={14} />
            <span>{t("topicBar.command")}</span>
          </button>
        </Tooltip>
      </div>
    </header>
  );

  const mainNode = (
    <main className="main">
      {preferenceOpen && (
        <PreferencePanel 
          title={t("preference.title") || "编码偏好"}
          onClose={() => setPreferenceOpen(false)}
        />
      )}
      {sidebarImDetailConnection ? (
        <SidebarImConnectionDetail
          connection={sidebarImDetailConnection}
          onClose={() => setSidebarImDetailConnectionId("")}
          onOpenSettings={openBotSettings}
          onOpenSession={() => void openSidebarImConnectionSession(sidebarImDetailConnection)}
        />
      ) : state.meta?.ready === false && !state.meta?.startupErr ? (
        <div className="loading-screen">
          <div className="loading-screen__spinner" />
          <span className="loading-screen__text">{t("common.loading")}</span>
        </div>
      ) : state.meta?.expertSession ? (
        <ExpertSessionView
          key={state.meta.expertSession.teamId}
          items={state.items}
          teamName={state.meta.expertSession.teamName}
          teamId={state.meta.expertSession.teamId}
          running={state.running}
          onSend={(task, mode, rounds) => void runExpertFromSession(state.meta!.expertSession!.teamId, task, mode, rounds)}
        />
      ) : (
        <Transcript
          items={state.items}
          live={state.live}
          footerHeight={footerHeight}
          onPrompt={send}
          onRewind={handleMessageAction}
          checkpoints={state.checkpoints}
          actionPending={state.messageAction != null}
          rewindDisabled={state.running || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
          defaultExpandThinking={expandThinking}
          profile={coworkActive ? "cowork" : "dev"}
          onInsert={addWorkspaceTextToComposer}
        />
      )}
    </main>
  );

  // Expert-session tabs render their own composer inside ExpertSessionView, so
  // the normal chat Composer (footerNode) is suppressed for them to avoid a
  // double-composer.
  const footerNode = !sidebarImDetailConnection && !state.meta?.expertSession ? (
    <footer className="footer" ref={footerRef}>
      {showTodos && <TodoPanel todos={todos} onDismiss={() => setDismissedTodo(todoItem!.id)} />}
      {state.approval && (
        <ApprovalModal
          approval={state.approval}
          onAnswer={(allow, session, persist) => {
            // Approving an exit_plan_mode plan leaves plan mode; sync the
            // tab-local indicator and persisted safe mode immediately.
            if (state.approval!.tool === "exit_plan_mode" && allow) applyCollaborationMode("normal");
            approve(state.approval!.id, allow, session, persist);
          }}
          onRevisePlan={(text) => {
            if (activeTabId) {
              setPendingPlanRevisionsByTab((current) => ({ ...current, [activeTabId]: text }));
            }
            approve(state.approval!.id, false, false, false);
          }}
          onExitPlan={() => {
            applyCollaborationMode("normal");
            approve(state.approval!.id, false, false, false);
          }}
        />
      )}
      {state.ask && (
        <AskCard
          ask={state.ask}
          onAnswer={answerQuestion}
          onDismiss={() => answerQuestion(state.ask!.id, [])}
        />
      )}
      {clearContextPending && (
        <ClearContextCard
          onCancel={cancelClearContext}
          onConfirm={() => {
            void confirmClearContext();
          }}
        />
      )}
      <Composer
        key={`composer-${coworkActive ? "cowork" : "dev"}`}
        running={state.running}
        paused={state.paused}
        collaborationMode={collaborationMode}
        showKnowledge={coworkActive}
        toolApprovalMode={toolApprovalMode}
        goal={goal}
        cwd={state.meta?.cwd}
        modelLabel={state.meta?.label ?? t("status.connecting")}
        tabId={activeTabId}
        effort={state.effort}
        onSend={handleSend}
        onCancel={cancel}
        onPauseToggle={pauseToggle}
        onCycleMode={cycleMode}
        onSetCollaborationMode={applyCollaborationMode}
        onSetToolApprovalMode={applyToolApprovalMode}
        onToggleYoloApprovalMode={toggleYoloApprovalMode}
        onClearGoal={() => applyGoal("")}
        onSwitchModel={switchModel}
        onSetEffort={setEffort}
        ragScope={ragScope}
        onPickRagScope={setControllerRagScope}
        insertRequest={composerInsertRequest}
        onInsertComplete={() => setComposerInsertRequest(null)}
        disabled={state.meta?.ready === false || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
        decisionPending={state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
        ready={state.meta?.ready === true}
        turnStartAt={state.turnStartAt}
        turnTokens={state.turnTokens}
        retry={state.retry}
        transientDismissSignal={transientOverlayDismissSignal}
      />

    </footer>
  ) : null;

  const projectTreeNode = (
    <ProjectTree
      activeScope={activeTab?.scope}
      activeWorkspaceRoot={activeTab?.workspaceRoot}
      activeTopicId={activeTab?.topicId}
      profile={coworkActive ? "cowork" : "dev"}
      imTopicSources={imTopicSources}
      onOpenTopic={handleOpenTopic}
      onOpenExpertSession={async (teamId, teamName) => {
        try {
          const meta = await app.OpenExpertSessionTab(teamId, teamName);
          // Switch to task center so the main area shows the expert tab's
          // ExpertSessionView (not the sidebar ExpertPanel).
          window.dispatchEvent(new CustomEvent("cowork:reset-panel"));
          await refreshTabMetas();
          await syncActiveTab(true);
          if (meta?.id) {
            setTimeout(() => { void refreshTabMetas(); void syncActiveTab(true); }, 1000);
          }
        } catch { /* ignore */ }
      }}
      onOpenProjectHistory={openProjectHistory}
      onCreateTopic={(scope, workspaceRoot) => openBlankSession(scope, scope === "project" ? workspaceRoot : "")}
      onTopicsChanged={refreshProjectsAndTabs}
      onRenameTopic={renameTopic}
      refreshSignal={projectRevision}
      onAddProject={async () => {
        await switchFolder();
      }}
    />
  );

  return (
    <ShellExpandProvider>
    <div ref={appRef} className={["app", `app--${desktopPlatform}`, browserPreviewChrome ? "app--browser-preview" : "", coworkActive ? "app--cowork" : ""].filter(Boolean).join(" ")}>
      <div
        className={[
          "layout",
          sidebarCollapsed ? "layout--sidebar-collapsed" : "",
          sidebarResizing ? "layout--resizing layout--sidebar-resizing" : "",
          workspacePanelGridOpen ? "layout--workspace-open" : "",
          workspacePanelOpen && workspacePanelMaximized ? "layout--workspace-maximized" : "",
          workspacePanelResizing ? "layout--resizing layout--workspace-resizing" : "",
        ]
          .filter(Boolean)
          .join(" ")}
        style={layoutStyle}
      >
        {coworkActive && (
          <CoWorkLayout
            headerNode={headerNode}
            mainNode={mainNode}
            footerNode={footerNode}
            projectTreeNode={projectTreeNode}
            rightDockOpen={coworkDockRenderable}
            sidebarCollapsed={sidebarCollapsed}
            onNewSession={() => void handleNewTab()}
            dockCwd={state.meta?.cwd}
            dockMaximized={workspacePanelMaximized}
            dockOnClose={() => closeWorkspacePanel()}
            dockOnToggleMaximized={() => {
              closeTransientOverlays();
              setWorkspacePanelMaximized((value) => !value);
            }}
            dockWidth={workspacePanelRenderWidth}
            dockMinWidth={workspacePanelResizeMinWidth}
            dockMaxAriaWidth={Math.max(workspacePanelMaxWidth, workspacePanelRenderWidth)}
            onDockResizeStart={startWorkspacePanelResize}
            onDockResizeKey={resizeWorkspacePanelWithKeyboard}
            onDockResetWidth={() => setSavedWorkspacePanelWidth(workspacePanelResetWidth)}
            sidebarWidth={sidebarWidth}
            sidebarMinWidth={SIDEBAR_MIN_WIDTH}
            sidebarMaxWidth={SIDEBAR_MAX_WIDTH}
            onSidebarResizeStart={startSidebarResize}
            onSidebarResizeKey={resizeSidebarWithKeyboard}
            onSidebarResetWidth={() => setExpandedSidebarWidth(defaultSidebarWidth())}
          />
        )}
        <AppChrome
          platform={desktopPlatform}
          browserPreviewChrome={browserPreviewChrome}
          tabs={visibleTabs}
          activeTabId={visibleTabId}
          revealActiveSignal={tabRevealSignal}
          commandCompact={workspacePanelGridOpen}
          sidebarTogglePressed={sidebarTogglePressed}
          sidebarExpandBlocked={sidebarExpandBlocked}
          sidebarCollapsed={sidebarCollapsed}
          sidebarToggleTitle={sidebarToggleTitle}
          workspacePanelMaximized={workspacePanelMaximized}
          workspacePanelRenderable={coworkActive ? coworkDockRenderable : workspacePanelRenderable}
          workspaceTogglePressed={workspaceTogglePressed}
          workspacePanelLabel={(coworkActive ? coworkDockRenderable : workspacePanelRenderable) ? t("rightDock.collapse") : t("rightDock.expand")}
          onToggleSidebar={toggleSidebar}
          onToggleWorkspacePanel={toggleWorkspacePanel}
          onTabChange={(id) => void handleTabChange(id)}
          onTabClose={(id) => void handleTabClose(id)}
          onTabsClose={(ids, nextActiveTabId) => void handleTabsClose(ids, nextActiveTabId)}
          onTabsReorder={(ids) => void handleTabsReorder(ids)}
          onNewTab={() => void handleNewTab()}
          onOpenPalette={() => void openPalette()}
          profile={coworkActive ? "cowork" : "dev"}
          onSwitchProfile={(name) => void switchProfile(name).catch(() => { /* revert handled in switchProfile */ })}
        />

          <aside 
            className={`sidebar${sidebarCollapsed ? " sidebar--collapsed" : ""}`} 
            aria-label={t("sidebar.navigation")}
            style={{ paddingBottom: '8px' }}
          >
          <button
            className="sidebar__new"
            onClick={() => {
              void handleNewTab();
            }}
          >
            <SquarePen size={18} />
            <span>{t("topbar.newSession")}</span>
          </button>

          <section className="sidebar__section sidebar__section--projects">
            {projectTreeNode}
          </section>

          <section className="cowork-sidebar__group" style={{ marginBottom: '0px', marginTop: 'auto' }}>
            <button
              className={`cowork-sidebar__item ${preferenceOpen ? "cowork-sidebar__item--active" : ""}`}
              onClick={() => {
                closeTransientOverlays();
                setPreferenceOpen(true);
              }}
            >
              <SlidersHorizontal size={14} />
              <span>{t("preference.title") || "编码偏好"}</span>
            </button>
          </section>

        </aside>
        <button
          className="sidebar-resizer"
          type="button"
          role="separator"
          aria-orientation="vertical"
          aria-label={t("sidebar.resize")}
          aria-valuemin={SIDEBAR_MIN_WIDTH}
          aria-valuemax={SIDEBAR_MAX_WIDTH}
          aria-valuenow={sidebarWidth}
          onPointerDown={startSidebarResize}
          onKeyDown={resizeSidebarWithKeyboard}
          onDoubleClick={() => setExpandedSidebarWidth(defaultSidebarWidth())}
        />

        <section className="chat-pane">
          <>
          {!coworkActive && headerNode}

          {state.meta?.startupErr && (
            <div className="banner banner--error">{t("topbar.startupError", { msg: state.meta.startupErr })}</div>
          )}

          <UpdateBanner enabled={startupUpdateChecksEnabled === true} />

          {!coworkActive && (
            <>
              {mainNode}
              {!preferenceOpen && footerNode}
            </>
          )}
          </>
        </section>

        {workspacePanelGridOpen && (
          <button
            className="workspace-panel-resizer"
            type="button"
            role="separator"
            aria-orientation="vertical"
            aria-label={t("rightDock.resize")}
            aria-valuemin={workspacePanelResizeMinWidth}
            aria-valuemax={Math.max(workspacePanelMaxWidth, workspacePanelRenderWidth)}
            aria-valuenow={workspacePanelRenderWidth}
            onPointerDown={startWorkspacePanelResize}
            onKeyDown={resizeWorkspacePanelWithKeyboard}
            onDoubleClick={() => setSavedWorkspacePanelWidth(workspacePanelResetWidth)}
          />
        )}

        {workspacePanelRenderable && (
          <aside
            className={[
              "workbench-dock",
              `workbench-dock--${rightDockMode}`,
            ].join(" ")}
            aria-label={t("rightDock.workbench")}
          >
            <div className="workbench-dock__tools">
              <div className="workbench-dock__tabs" role="tablist" aria-label={t("rightDock.views")}>
                {SHOW_CONTEXT_DOCK && (
                  <button
                    type="button"
                    role="tab"
                    aria-selected={rightDockMode === "context"}
                    className={`workbench-dock__tab${rightDockMode === "context" ? " workbench-dock__tab--active" : ""}`}
                    onClick={() => openRightDockMode("context")}
                  >
                    <Activity size={13} />
                    <span className="workbench-dock__tab-label">{t("rightDock.overview")}</span>
                  </button>
                )}
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "files"}
                  className={`workbench-dock__tab${rightDockMode === "files" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("files")}
                >
                  <FileText size={13} />
                  <span className="workbench-dock__tab-label">{t("workspace.filesTab")}</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "changed"}
                  className={`workbench-dock__tab${rightDockMode === "changed" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("changed")}
                >
                  <GitBranch size={13} />
                  <span className="workbench-dock__tab-label">{t("workspace.changedTab")}</span>
                </button>
              </div>
            </div>
            <div className="workbench-dock__body">
              {rightDockMode === "context" ? (
                <ContextPanel
                  tabId={activeTabId}
                  context={state.context}
                  usage={state.usage}
                  sessionTokens={state.sessionTokens}
                  refreshKey={dockRefreshKey}
                  onOpenWorkspaceMode={openRightDockMode}
                  onOpenWorkspaceFile={openRightDockFile}
                  onOpenWorkspaceFileList={openRightDockFileList}
                  onOpenWorkspaceChangeList={openRightDockChangeList}
                  onOpenWorkspaceChangeFile={openRightDockChangeFile}
                />
              ) : (
                <WorkspacePanel
                  open={workspacePanelRenderable}
                  cwd={state.meta?.cwd}
                  maximized={workspacePanelMaximized}
                  panelWidth={workspacePanelRenderWidth}
                  onClose={() => setWorkspacePanel(false)}
                  onToggleMaximized={() => {
                    closeTransientOverlays();
                    setWorkspacePanelMaximized((value) => !value);
                  }}
                  onPreviewModeChange={handleWorkspacePreviewModeChange}
                  onAddToChat={addWorkspaceTextToComposer}
                  onRequestPanelWidth={ensureWorkspacePanelWidth}
                  refreshKey={dockRefreshKey}
                  initialViewMode={rightDockMode === "changed" ? "changed" : "files"}
                  revealPathRequest={workspaceRevealRequest}
                  changeRevealRequest={workspaceChangeRevealRequest}
                  fileListRequest={workspaceFileListRequest}
                  changeListRequest={workspaceChangeListRequest}
                  showViewTabs={false}
                />
              )}
            </div>
          </aside>
        )}
      </div>

      <div className="global-bottom-bar">
        <SidebarFooter
          imConnectionCount={sidebarImConnections.length}
          imOnline={sidebarImOnline}
          tooltipDisabled={false}
          onOpenIm={() => void showSidebarImDetail()}
          onOpenHistory={() => void openAllHistory()}
          onOpenTrash={() => void openTrash()}
          onOpenSettings={() => {
            closeTransientOverlays();
            setSettingsTarget("general");
          }}
        />
        <StatusBar
          context={state.context}
          usage={state.usage}
          jobs={state.jobs}
          running={state.running}
          collaborationMode={collaborationMode}
          toolApprovalMode={toolApprovalMode}
          sessionTurns={sessionTurns}
          sessionTokens={state.sessionTokens}
          turnTokens={state.turnTotalTokens}
          modelLabel={state.meta?.label}
        />
      </div>

      {histView !== null && (
        <HistoryPanel
          kind={histView.kind}
          sessions={histView.sessions}
          running={state.running}
          onResume={onResumeSession}
          onPreview={previewSession}
          onDelete={onDeleteSession}
          onRename={onRenameSession}
          onRestore={onRestoreTrashedSession}
          onPurge={onPurgeTrashedSession}
          onPurgeAll={onPurgeAllTrashedSessions}
          onClose={closeHistory}
        />
      )}

      {settingsTarget !== null && (
        <SettingsPanel
          initialTab={settingsTarget}
          initialPayload={settingsPayload ?? undefined}
          onClose={() => {
            setSettingsTarget(null);
            setSettingsPayload(null);
          }}
          onChanged={() => {
            void refreshMeta();
            void reloadSidebarImConnections().catch((e) => console.warn("bot sidebar refresh failed", e));
            void app.Settings()
              .then(applyDesktopPreferences)
              .catch((e) => console.warn("desktop preferences refresh failed", e));
          }}
        />
      )}

      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        items={paletteItems}
        placeholder={t("palette.placeholder")}
        emptyText={t("palette.empty")}
      />

      <ShortcutsCheatsheet
        open={shortcutsOpen}
        platform={desktopPlatform}
        onClose={() => setShortcutsOpen(false)}
        t={t}
      />

      {startupSplashVisible && (
        <StartupSplash hold={startupSplashHold} onDone={() => setStartupSplashVisible(false)} />
      )}

      {needsOnboarding && <OnboardingOverlay onComplete={() => setNeedsOnboarding(false)} />}
    </div>
    </ShellExpandProvider>
  );
}
