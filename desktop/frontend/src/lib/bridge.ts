// bridge is the single seam between the React app and the Go kernel. In the Wails
// shell it calls the bound App methods (window.go.main.App.*) and subscribes to
// the runtime event stream (window.runtime.EventsOn). In a plain browser (`pnpm
// dev` outside the shell) those globals are absent, so it falls back to a mock
// that streams a canned turn through the same contract — letting the whole UI be
// developed and laid out without rebuilding the Go side.

import type * as GeneratedApp from "../../wailsjs/go/main/App";

import { t } from "./i18n";
import { modeWithAutoApproveTools, modeWithPlan, normalizeCollaborationMode, normalizeMode, normalizeToolApprovalMode } from "./types";

import type {
  BotConnectionDiagnostic,
  BotInstallPollResult,
  BotInstallStartResult,
  BotSettingsView,
  CapabilitiesView,
  CheckpointMeta,
  CoWorkSettingsView,
  CommandInfo,
  ContextInfo,
  ContextPanelInfo,
  DirEntry,
  DroppedItem,
  DreamRunView,
  DreamStatusView,
  EffortInfo,
  FilePreview,
  HistoryMessage,
  JobView,
  MCPServerInput,
  MemoryView,
  Meta,
  ModelInfo,
  ProfileView,
  NetworkView,
  ProjectNode,
  ProfileInfo,
  ProviderView,
  QuestionAnswer,
  CollabEvent,
  RagCollectionView,
  RagETAView,
  RagImportResult,
  RagNodeView,
  RagProgressEvent,
  RagSearchHitView,
  GraphDataView,
  EntityDetailView,
  EntityPatch,
  DocPreviewView,
  RunRecordView,
  ServerView,
  SessionMeta,
  SettingsView,
  SchedulePreview,
  SkillRootView,
  SkillView,
  SlashArgsResult,
  TabMeta,
  CalendarEventView,
  CalendarEventInput,
  TaskInput,
  TaskView,
  TeamView,
  TemplateView,
  TopicMeta,
  UpdateInfo,
  UpdateProgress,
  WireEvent,
  WorkspaceChangesView,
  GitCommitView,
  GitCommitDetailView,
  WorkspaceView,
  HooksSettingsView,
  HookConfigView,
  MailProbeResult,
  InboxItem,
  RagExtractResultView,
  ExpertRunView,
  RecentChatView,
  BotDockStatusView,
} from "./types";

const GLOBAL_PROJECT_ORDER_KEY = "__global__";

// AppBindings is derived from the Wails-generated Go → TS method signatures, so
// the compiler catches drift between the Go binding surface and the frontend mock.
// Run `wails generate module` after adding/renaming a bound method on App, then
// `pnpm typecheck` to verify the mock still satisfies the contract.
//
// Types for the new native-feel bindings — kept inline since they are
// bridge-specific and only used in AppBindings / the dev mock.
interface NativeConfirmRequest {
  title: string;
  message: string;
  detail: string;
  confirmLabel: string;
  cancelLabel: string;
  destructive: boolean;
}

interface DesktopWindowState {
  width: number;
  height: number;
  x: number;
  y: number;
  maximised: boolean;
}

// AppBindings is the hand-written contract between the React app and the Go
// kernel. It uses local types (types.ts) so components don't import generated
// model classes. _CheckGeneratedBindings catches drift: when a Go method is
// added or renamed, the generated types shift, and a key present in GeneratedApp
// but missing from AppBindings causes a type error here. Fix: add the new method
// to AppBindings, then run `pnpm typecheck` to verify.
export interface AppBindings {
  Platform(): Promise<string>;
  Submit(input: string): Promise<void>;
  SubmitToTab(tabID: string, input: string): Promise<void>;
  SubmitDisplay(display: string, input: string): Promise<void>;
  SubmitDisplayToTab(tabID: string, display: string, input: string): Promise<void>;
  RunShell(command: string): Promise<void>;
  RunShellForTab(tabID: string, command: string): Promise<void>;
  Steer(text: string): Promise<void>;
  SteerForTab(tabID: string, text: string): Promise<void>;
  Cancel(): Promise<void>;
  CancelTab(tabID: string): Promise<void>;
  Pause(): Promise<void>;
  PauseTab(tabID: string): Promise<void>;
  ResumeTurn(): Promise<void>;
  ResumeTurnTab(tabID: string): Promise<void>;
  PausedTab(tabID: string): Promise<boolean>;
  Approve(id: string, allow: boolean, session: boolean, persist: boolean): Promise<void>;
  ApproveTab(tabID: string, id: string, allow: boolean, session: boolean, persist: boolean): Promise<void>;
  AnswerQuestion(id: string, answers: QuestionAnswer[]): Promise<void>;
  AnswerQuestionForTab(tabID: string, id: string, answers: QuestionAnswer[]): Promise<void>;
  ReplayPendingPrompts(): Promise<void>;
  SetFastTaskModel(ref: string): Promise<void>;
  // SetPlanMode/SetMode/SetGoal etc. (non-ForTab variants) are retained for
  // Wails binding parity (_CheckGenToApp) but not called by the frontend,
  // which uses the *ForTab variants exclusively. Their mock impls are omitted.
  SetPlanMode(on: boolean): Promise<void>;
  SetMode(mode: string): Promise<void>;
  SetModeForTab(tabID: string, mode: string): Promise<void>;
  SetAutoApproveTools(on: boolean): Promise<void>;
  SetCollaborationMode(mode: string): Promise<void>;
  SetCollaborationModeForTab(tabID: string, mode: string): Promise<void>;
  SetToolApprovalMode(mode: string): Promise<void>;
  SetToolApprovalModeForTab(tabID: string, mode: string): Promise<void>;
  SetRagScope(scope: string): Promise<void>;
  SetRagScopeForTab(tabID: string, scope: string): Promise<void>;
  SetGoal(goal: string): Promise<void>;
  SetGoalForTab(tabID: string, goal: string): Promise<void>;
  ClearGoal(): Promise<void>;
  ClearGoalForTab(tabID: string): Promise<void>;
  Compact(): Promise<void>;
  NewSession(): Promise<void>;
  ClearSession(): Promise<void>;
  History(): Promise<HistoryMessage[]>;
  HistoryForTab(tabID: string): Promise<HistoryMessage[]>;
  Checkpoints(): Promise<CheckpointMeta[]>;
  CheckpointsForTab(tabID: string): Promise<CheckpointMeta[]>;
  Rewind(turn: number, scope: string): Promise<void>;
  Fork(turn: number): Promise<TabMeta>;
  SummarizeFrom(turn: number): Promise<void>;
  SummarizeUpTo(turn: number): Promise<void>;
  ListSessions(): Promise<SessionMeta[]>;
  ListTrashedSessions(): Promise<SessionMeta[]>;
  ResumeSession(path: string): Promise<HistoryMessage[]>;
  ResumeSessionForTab(tabID: string, path: string): Promise<HistoryMessage[]>;
  PreviewSession(path: string): Promise<HistoryMessage[]>;
  DeleteSession(path: string): Promise<void>;
  RestoreSession(path: string): Promise<void>;
  PurgeTrashedSession(path: string): Promise<void>;
  RenameSession(path: string, title: string): Promise<void>;
  ListWorkspaces(): Promise<WorkspaceView[]>;
  PickWorkspace(): Promise<string>;
  PickImportFolder(): Promise<string>;
  PickImportFiles(): Promise<string[]>;
  SwitchWorkspace(path: string): Promise<string>;
  RemoveWorkspace(path: string): Promise<void>;
  ContextUsage(): Promise<ContextInfo>;
  ContextUsageForTab(tabID: string): Promise<ContextInfo>;
  Jobs(): Promise<JobView[]>;
  JobsForTab(tabID: string): Promise<JobView[]>;
  Meta(): Promise<Meta>;
  MetaForTab(tabID: string): Promise<Meta>;
  Commands(): Promise<CommandInfo[]>;
  Capabilities(): Promise<CapabilitiesView>;
  AddMCPServer(input: MCPServerInput): Promise<number>;
  UpdateMCPServer(name: string, input: MCPServerInput): Promise<void>;
  RemoveMCPServer(name: string): Promise<void>;
  ReconnectMCPServer(name: string): Promise<void>;
  ClearMCPServerAuthentication(name: string): Promise<void>;
  PickSkillFolder(): Promise<string>;
  PickDirectory(title: string): Promise<string>;
  AddSkillPath(path: string): Promise<void>;
  RemoveSkillPath(path: string): Promise<void>;
  RefreshSkills(): Promise<void>;
  SetSkillEnabled(name: string, enabled: boolean): Promise<void>;
  SetJiutianTool(name: string, enabled: boolean): Promise<void>;
  DreamStatus(): Promise<DreamStatusView>;
  SetDreamEnabled(enabled: boolean): Promise<void>;
  SetDreamIntervals(dreamDays: number, distillDays: number): Promise<void>;
  TriggerDream(): Promise<DreamRunView>;
  TriggerDistill(): Promise<DreamRunView>;
  SetMCPServerEnabled(name: string, enabled: boolean): Promise<void>;
  SetMCPServerTier(name: string, tier: string): Promise<void>;
  SlashArgs(input: string): Promise<SlashArgsResult>;
  ListDir(rel: string): Promise<DirEntry[]>;
  SearchFileRefs(query: string): Promise<DirEntry[]>;
  ReadFile(rel: string): Promise<FilePreview>;
  WorkspaceChanges(): Promise<WorkspaceChangesView>;
  GitBranches(): Promise<string[]>;
  GitCheckout(branch: string): Promise<void>;
  WorkspaceGitHistory(path: string): Promise<GitCommitView[]>;
  WorkspaceGitCommitDetail(hash: string, path: string): Promise<GitCommitDetailView>;
  OpenWorkspacePath(rel: string): Promise<void>;
  RevealWorkspacePath(rel: string): Promise<void>;
  RevealPath(path: string): Promise<void>;
  SavePastedImage(dataUrl: string): Promise<string>;
  SaveClipboardImage(): Promise<string>;
  SavePastedFile(name: string, dataUrl: string): Promise<string>;
  PickExportFile(defaultFilename: string, mimeType: string): Promise<string>;
  SaveExportFile(path: string, payload: string, base64Encoded: boolean): Promise<void>;
  AttachDropped(path: string): Promise<DroppedItem>;
  AttachmentDataURL(path: string): Promise<string>;
  Models(): Promise<ModelInfo[]>;
  SetModel(name: string): Promise<void>;
  ModelsForTab(tabID: string): Promise<ModelInfo[]>;
  SetModelForTab(tabID: string, name: string): Promise<void>;
  // Product profile (dev | cowork). SwitchProfile rebuilds the tab's controller
  // with the profile's model/prompt/skill/plugin bundle (see app.SwitchProfileForTab).
  Profile(): Promise<string>;
  ProfileForTab(tabID: string): Promise<string>;
  Profiles(): Promise<ProfileInfo[]>;
  SwitchProfile(name: string): Promise<void>;
  SwitchProfileForTab(tabID: string, name: string): Promise<void>;
  Effort(): Promise<EffortInfo>;
  SetEffort(level: string): Promise<void>;
  EffortForTab(tabID: string): Promise<EffortInfo>;
  SetEffortForTab(tabID: string, level: string): Promise<void>;
  Memory(): Promise<MemoryView>;
  MemoryHistory(): Promise<MemoryView>;
  Remember(scope: string, note: string): Promise<string>;
  Forget(name: string): Promise<void>;
  PromoteMemory(name: string): Promise<boolean>;
  RejectMemory(name: string): Promise<boolean>;
  SaveDoc(path: string, body: string): Promise<string>;
  PortraitProfile(): Promise<ProfileView>;
  Settings(): Promise<SettingsView>;
  SetDefaultModel(ref: string): Promise<void>;
  GetJiutianBaseDomain(): Promise<string>;
  SetJiutianBaseDomain(domain: string): Promise<void>;
  SetSubagentModel(ref: string): Promise<void>;
  SetSubagentEffort(level: string): Promise<void>;
  SetAutoPlan(mode: string): Promise<void>;
  SaveProvider(p: ProviderView): Promise<void>;
  AddOfficialProviderAccess(kind: string, key: string): Promise<void>;
  FetchProviderModels(p: ProviderView): Promise<string[]>;
  DeleteProvider(name: string): Promise<void>;
  RemoveProviderAccess(name: string): Promise<void>;
  SetProviderKey(apiKeyEnv: string, value: string): Promise<void>;
  ClearProviderKey(apiKeyEnv: string): Promise<void>;
  SetPermissionMode(mode: string): Promise<void>;
  AddPermissionRule(list: string, rule: string): Promise<void>;
  RemovePermissionRule(list: string, rule: string): Promise<void>;
  SetSandbox(bash: string, network: boolean, workspaceRoot: string, allowWrite: string[]): Promise<void>;
  SetNetwork(n: NetworkView): Promise<void>;
  SetBotSettings(b: BotSettingsView): Promise<void>;
  // coWork profile settings (browser/PPT/email/RAG). Secrets go to a managed
  // .env via SetCoWorkSettings; CheckCoworkBrowser powers the panel's detect
  // button; OpenPPTTemplateDir opens the templates folder so the user can add
  // JSON templates.
  SetCoWorkSettings(v: CoWorkSettingsView): Promise<void>;
  // ProbeMailAccount tests a saved mailbox's IMAP login by actually connecting.
  // An empty name probes the Default account; a non-empty name probes that
  // named account. Returns ok/error/unconfigured so the mail card can show a
  // green/red status dot after the user saves. Always resolves (a connection
  // failure comes back as status="error", not a rejection).
  ProbeMailAccount(name: string): Promise<MailProbeResult>;
  // InboxPreview reads the most recent messages (up to limit) from a mailbox
  // ("INBOX" unread-only, or "Sent" for sent mail), for the cowork dock's
  // "邮件" tab. Returns [] when no mailbox is configured or no mail.
  InboxPreview(mailbox: string, limit: number): Promise<InboxItem[]>;
  // Hooks settings (settings.json, global + project scopes). HooksSettings
  // returns the payload for the Hooks tab; Save/Trust write + gate project hooks.
  HooksSettings(scope: string): Promise<HooksSettingsView>;
  SaveHooksSettings(scope: string, hooks: HookConfigView[]): Promise<void>;
  SaveHooksSettingsForRoot(scope: string, projectRoot: string, hooks: HookConfigView[]): Promise<void>;
  TrustProjectHooks(): Promise<void>;
  TrustProjectHooksForRoot(projectRoot: string): Promise<void>;
  CheckCoworkBrowser(): Promise<string>;
  OpenPPTTemplateDir(): Promise<void>;
  SetBotSecret(envName: string, value: string): Promise<void>;
  ClearBotSecret(envName: string): Promise<void>;
  StartBotConnectionInstall(provider: string, domain: string): Promise<BotInstallStartResult>;
  PollBotConnectionInstall(installID: string): Promise<BotInstallPollResult>;
  DiagnoseBotConnection(id: string): Promise<BotConnectionDiagnostic>;
  TestBotConnection(id: string, target?: string): Promise<BotConnectionDiagnostic>;
  // ListRecentBotChats returns recently-seen IM chats for the task-form picker.
  ListRecentBotChats(): Promise<RecentChatView[]>;
  // BotDockStatus returns the lightweight bot status for the dock Today panel
  // (online, connected platforms, recent chat count). Replaces hardcoded text.
  BotDockStatus(): Promise<BotDockStatusView>;
  SetCloseBehavior(mode: string): Promise<void>;
  SetDisplayMode(mode: string): Promise<void>;
  SetDesktopLanguage(lang: string): Promise<void>;
  SetDesktopAppearance(theme: string, style: string): Promise<void>;
  SetDesktopCheckUpdates(enabled: boolean): Promise<void>;
  SetDesktopTelemetry(enabled: boolean): Promise<void>;
  SetExpandThinking(on: boolean): Promise<void>;
  MigrateDesktopPreferences(language: string, theme: string, style: string): Promise<void>;
  SetAgentParams(temperature: number, maxSteps: number, plannerMaxSteps: number, systemPrompt: string): Promise<void>;
  SetRPM(rpm: number): Promise<void>;
  SetTrayLocale(locale: "en" | "zh"): Promise<void>;
  // SetBypass is the legacy Wails name for YOLO/full-access tool auto-approval
  // (ask questions and plan approvals still wait; deny rules still apply).
  // Runtime-only.
  SetBypass(on: boolean): Promise<void>;
  Version(): Promise<string>;
  CheckUpdate(): Promise<UpdateInfo | null>;
  ApplyUpdate(): Promise<void>;
  OpenDownloadPage(): Promise<void>;
  NeedsOnboarding(): Promise<boolean>;
  ConnectKey(apiKey: string): Promise<void>;
  // Crash overlay "Send report" (desktop/crash_app.go): scrubs user paths, attaches
  // version/os/arch, POSTs to the collection endpoint. Only ever sent on user click.
  ReportCrash(kind: string, detail: string): Promise<void>;
  ListTabs(): Promise<TabMeta[]>;
  // profile ("dev"|"cowork"|"" for default) scopes the topic/tab to a product
  // profile; it comes from the active tab's profile in the frontend.
  OpenProjectTab(workspaceRoot: string, topicID: string, profile: string): Promise<TabMeta>;
  OpenProjectTab3(workspaceRoot: string, topicID: string, profile: string): Promise<TabMeta>;
  OpenGlobalTab(topicID: string, profile: string): Promise<TabMeta>;
  EnsureBlankTab(scope: string, workspaceRoot: string, profile: string): Promise<TabMeta>;
  OpenExpertSessionTab(teamId: string, teamName: string): Promise<TabMeta>;
  SetActiveTab(tabID: string): Promise<void>;
  ReorderTabs(tabIDs: string[]): Promise<void>;
  CloseTab(tabID: string): Promise<void>;
  ListProjectTree(profile: string): Promise<ProjectNode[]>;
  RenameProject(workspaceRoot: string, title: string): Promise<void>;
  SetProjectColor(workspaceRoot: string, color: string): Promise<void>;
  ReorderProjects(profile: string, workspaceRoots: string[]): Promise<void>;
  CreateTopic(scope: string, workspaceRoot: string, profile: string, title: string): Promise<TopicMeta>;
  RenameTopic(topicID: string, title: string): Promise<void>;
  DeleteTopic(topicID: string): Promise<void>;
  TrashTopic(topicID: string): Promise<void>;
  TrashExpertSession(teamID: string): Promise<void>;
  ContextPanel(tabID: string): Promise<ContextPanelInfo>;
  // New native-feel bindings (added with the desktop native-feel plan).
  ConfirmAction(req: NativeConfirmRequest): Promise<boolean>;
  SaveWindowState(state: DesktopWindowState): Promise<void>;
  // --- Scheduled tasks (coWork automation panel) ---------------------------
  // Backed by desktop/scheduler_app.go. The UI re-lists on the
  // "scheduler:changed" event (onSchedulerChanged) so cards stay live without
  // each component polling. "scheduler:notice" (onSchedulerNotice) carries a
  // fired task's {name, result} for an in-app toast.
  ListScheduledTasks(): Promise<TaskView[]>;
  CreateScheduledTask(input: TaskInput): Promise<TaskView>;
  UpdateScheduledTask(input: TaskInput): Promise<TaskView>;
  DeleteScheduledTask(id: string): Promise<void>;
  PauseScheduledTask(id: string): Promise<void>;
  ResumeScheduledTask(id: string): Promise<void>;
  RunScheduledTaskNow(id: string): Promise<string>;
  ScheduledTaskHistory(taskID: string): Promise<RunRecordView[]>;
  ScheduledTaskTemplates(): Promise<TemplateView[]>;
  PreviewSchedule(text: string): Promise<SchedulePreview>;
  // SmartParseSchedule is the on-demand LLM time parser (迅捷任务模型), called
  // ONLY when the user clicks the "🔍 智能解析" button — never during typing.
  // It resolves phrases the regex can't ("下下周五下午3点") into a one-shot time.
  SmartParseSchedule(text: string): Promise<SchedulePreview>;
  // --- Calendar (coWork calendar panel) ------------------------------------
  // Backed by desktop/calendar_app.go. The UI re-lists on the
  // "calendar:changed" event (onCalendarChanged).
  ListCalendarEvents(since: string, before: string): Promise<CalendarEventView[]>;
  ListScheduledTasksAsEvents(since: string, before: string): Promise<CalendarEventView[]>;
  CreateCalendarEvent(input: CalendarEventInput): Promise<CalendarEventView>;
  UpdateCalendarEvent(input: CalendarEventInput): Promise<CalendarEventView>;
  DeleteCalendarEvent(id: string): Promise<void>;
  SearchCalendarEvents(q: string, limit: number): Promise<CalendarEventView[]>;
  ExportCalendarEvents(path: string): Promise<string>;
  ImportCalendarEvents(path: string): Promise<string>;
  // ExportCalendarDialog / ImportCalendarDialog open a native file dialog, then
  // export/import. Return "" when the user cancels the dialog.
  ExportCalendarDialog(): Promise<string>;
  ImportCalendarDialog(): Promise<string>;
  GetChineseHolidays(year: number): Promise<CalendarEventView[]>;
  // --- RAG knowledge base (coWork RAG panel) -------------------------------
  // Backed by desktop/rag_app.go. The panel re-fetches the tree on the
  // "rag:changed" event (onRagChanged) and updates per-node progress bars on
  // "rag:progress" (onRagProgress).
  ListRagCollections(): Promise<RagCollectionView[]>;
  ListRagTree(collection: string): Promise<RagNodeView[]>;
  RagImportPaths(collection: string, paths: string[]): Promise<RagImportResult>;
  RagStartExtract(collection: string, template: string, mode: string): Promise<void>;
  RagExtractResult(collection: string): Promise<RagExtractResultView>;
  RagCancelExtract(jobId: string): Promise<void>;
  RagRemovePath(collection: string, path: string): Promise<void>;
  RagClear(collection: string): Promise<void>;
  RagCleanCollection(collection: string): Promise<void>;
  RagSearch(collection: string, query: string, topK: number): Promise<RagSearchHitView>;
  RagSemanticSearch(collection: string, query: string, topK: number): Promise<RagSearchHitView>;
  RagEmbedEntities(collection: string): Promise<void>;
  RagDetectCommunities(collection: string): Promise<void>;
  RagSummarize(collection: string): Promise<{ summary: string; themes: string[] }>;
  RagAsk(collection: string, question: string): Promise<string>;
  RagPreviewETA(jobId: string): Promise<RagETAView>;
  RagListTemplates(): Promise<string[]>;
  HEHealth(): Promise<{ running: boolean; ready: boolean; port: number }>;
  RagListHETemplates(): Promise<Array<{ name: string; displayName: string; description: string; category: string; available: boolean; templateType: string; entityFields: Array<{ name: string; description: string }>; relationFields: Array<{ name: string; description: string }> }>>;
  // Graph / Entity detail / Edit / Merge / KnowledgeRef / Obsidian
  GetGraphData(collection: string): Promise<GraphDataView>;
  GetTopEntities(collection: string, limit: number): Promise<GraphDataView>;
  GetGraphDataPaged(collection: string, offset: number, limit: number, types: string[]): Promise<GraphDataView>;
  GetEntityDetail(collection: string, name: string): Promise<EntityDetailView>;
  UpdateEntity(collection: string, name: string, patch: EntityPatch): Promise<void>;
  MergeEntities(collection: string, keepName: string, mergeNames: string[]): Promise<void>;
  RagFindMergeCandidates(collection: string): Promise<Array<{ keepName: string; mergeName: string; keepRaw: string; mergeRaw: string; score: number }>>;
  GetDocumentPreview(collection: string, docPath: string): Promise<DocPreviewView>;
  WriteKnowledgeRef(collection: string, entityNames: string[], relationKeys: string[]): Promise<string>;
  RunSkillWithKnowledge(skillName: string, refPath: string): Promise<void>;
  ExportObsidian(collection: string, outputDir: string): Promise<void>;
  SetSessionCollections(collections: string[]): Promise<void>;
  GetSessionCollections(): Promise<string[]>;
  RagFeedText(collection: string, label: string, text: string): Promise<void>;
  RagBatchImport(collection: string, paths: string[]): Promise<RagImportResult>;
  RagBatchExtract(collection: string): Promise<void>;
  // --- Expert team (multi-model collaboration) -----------------------------
  // Backed by desktop/experts_app.go. The panel subscribes to "experts:collab"
  // (onExpertsCollab) for streamed expert outputs and "experts:changed"
  // (onExpertsChanged) for team-list refresh.
  ListExpertTeams(): Promise<TeamView[]>;
  CreateExpertTeam(team: TeamView): Promise<TeamView>;
  UpdateExpertTeam(team: TeamView): Promise<TeamView>;
  DeleteExpertTeam(id: string): Promise<void>;
  RunExpertTeam(teamId: string, task: string, mode: string, rounds: number): Promise<string>;
  GetActiveExpertRun(teamId: string): Promise<ExpertRunView>;
  DeleteExpertCollab(tabId: string, ordinal: number): Promise<HistoryMessage[]>;
  StartScreenshotHotkey(): Promise<void>;
  StopScreenshotHotkey(): Promise<void>;
  StartEStopHotkey(): Promise<void>;
  StopEStopHotkey(): Promise<void>;
  RagCreateCollection(name: string): Promise<void>;
  RagDeleteCollection(name: string): Promise<void>;
  RagRenameCollection(oldName: string, newName: string): Promise<void>;
  SetDesktopMetrics(enabled: boolean): Promise<void>;
  SetPlannerModel(model: string): Promise<void>;
}

// Compile-time drift check. Exclude<A, B> extracts keys in A that are missing
// from B. If that set is non-empty, AssertNever<non-never> fails with
// "Type 'X' does not satisfy the constraint 'never'".
// _CheckGenToApp errors mean a generated Go method has no TS counterpart.
// These compare method *names* only; full signature checking isn't possible here
// because local types (types.ts) use plain interfaces while generated types
// (models.ts) use classes with a convertValues prototype method. The structural
// mismatch would produce false positives. Method-arity and parameter-order drift
// are caught at the call sites by tsc when components invoke app.<method>(...).
type AssertNever<T extends never> = T;
export type _CheckGenToApp = AssertNever<Exclude<keyof typeof GeneratedApp, keyof AppBindings>>;

interface WailsRuntime {
  EventsOn(name: string, cb: (...data: unknown[]) => void): () => void;
  BrowserOpenURL(url: string): void;
  // Native OS file drop (desktop only); useDropTarget gates delivery to elements
  // carrying the --wails-drop-target CSS property. Absent in the browser dev mock.
  OnFileDrop?(cb: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean): void;
  OnFileDropOff?(): void;
}

declare global {
  interface Window {
    runtime?: WailsRuntime;
    go?: { main?: { App?: AppBindings } };
  }
}

// Must match desktop/app.go's eventChannel constant.
const EVENT_CHANNEL = "agent:event";

// Resolve the Wails binding at CALL time, not module-load time: in dev the Wails
// runtime can inject window.go AFTER this module first evaluates, so snapshotting
// once would pin the browser mock for the whole session (and show fake data — the
// dev mock's model list leaking into the real app was exactly this bug).
function realApp(): AppBindings | undefined {
  return typeof window !== "undefined" ? window.go?.main?.App : undefined;
}

let mockSingleton: AppBindings | null = null;
function getMock(): AppBindings {
  if (!mockSingleton) mockSingleton = makeMockApp();
  return mockSingleton;
}

// onEvent subscribes to the agent's typed event stream; returns an unsubscribe.
export function onEvent(cb: (e: WireEvent) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn(EVENT_CHANNEL, (payload) => cb(payload as WireEvent));
  }
  return mockSubscribe(cb);
}

// onUpdaterProgress subscribes to the auto-updater's progress events (a separate
// channel from the agent stream); returns an unsubscribe. Must match the event
// name emitted in desktop/updater_app.go.
export function onUpdaterProgress(cb: (p: UpdateProgress) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("updater:progress", (p) => cb(p as UpdateProgress));
  }
  updaterListeners.add(cb);
  return () => {
    updaterListeners.delete(cb);
  };
}

// onFilesDropped subscribes to native OS file drops landing on the composer (the
// --wails-drop-target element); the callback gets the dropped files' absolute
// paths. No-op in the browser dev mock, where the runtime is absent.
export function onFilesDropped(cb: (paths: string[]) => void): () => void {
  const rt = typeof window !== "undefined" ? window.runtime : undefined;
  if (!rt?.OnFileDrop) return () => {};

  // Wails' internal ResolveFilePaths throws when a non-file object (e.g. the
  // window icon) is dragged onto the webview. The error is uncaught and crashes
  // the app. Intercept it here so only real file drops reach the callback.
  const suppressNonFileDragError = (e: ErrorEvent) => {
    if (e.message?.includes("additional File object is not a file on the disk")) {
      e.preventDefault();
    }
  };
  const suppressNonFileDragRejection = (e: PromiseRejectionEvent) => {
    const msg = e.reason?.message ?? String(e.reason);
    if (msg.includes("additional File object is not a file on the disk")) {
      e.preventDefault();
    }
  };
  window.addEventListener("error", suppressNonFileDragError);
  window.addEventListener("unhandledrejection", suppressNonFileDragRejection);

  rt.OnFileDrop((_x, _y, paths) => {
    if (Array.isArray(paths) && paths.length > 0) cb(paths);
  }, true);
  return () => {
    rt.OnFileDropOff?.();
    window.removeEventListener("error", suppressNonFileDragError);
    window.removeEventListener("unhandledrejection", suppressNonFileDragRejection);
  };
}

// onReady subscribes to the agent:ready event fired when boot.Build completes.
// The frontend re-fetches Meta/Context/History when this lands.
export function onReady(cb: () => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("agent:ready", () => cb());
  }
  // Mock fallback: fire once on subscribe (as before) AND on subsequent mock
  // agent:ready emissions (e.g. after a mock SwitchProfile), so the dev shell
  // reloads session data the same way the real app does.
  cb();
  const wrapped = () => cb();
  (mockEventListeners["agent:ready"] ??= []).push(wrapped);
  return () => {
    const arr = mockEventListeners["agent:ready"] ?? [];
    mockEventListeners["agent:ready"] = arr.filter((x) => x !== wrapped);
  };
}

// Mock-mode event bus: when running without the Go backend (npm run dev),
// onProjectTreeChanged/onProfileChanged register here and the mock App methods
// emit through emitMockEvent. This lets the dev shell exercise profile-switch
// UI flows (sidebar refresh, message clear) that otherwise only fire via Wails.
const mockEventListeners: Record<string, Array<(payload: unknown) => void>> = {};
export function emitMockEvent(name: string, payload?: unknown): void {
  (mockEventListeners[name] ?? []).forEach((cb) => cb(payload));
}

export function onProjectTreeChanged(cb: () => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("project-tree:changed", () => cb());
  }
  // Mock fallback so the dev shell refreshes the sidebar on profile switch.
  (mockEventListeners["project-tree:changed"] ??= []).push(cb);
  return () => {
    const arr = mockEventListeners["project-tree:changed"] ?? [];
    mockEventListeners["project-tree:changed"] = arr.filter((x) => x !== cb);
  };
}

// onProfileChanged fires when a tab's product profile (dev/cowork) changes after
// a SwitchProfile rebuild. The payload carries {tabId, profile}; cb receives it
// so the layout can swap for the affected tab only.
export function onProfileChanged(cb: (e: { tabId: string; profile: string }) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("profile:changed", (...data: unknown[]) => {
      const e = (data?.[0] ?? {}) as { tabId?: string; profile?: string };
      cb({ tabId: e.tabId ?? "", profile: e.profile ?? "dev" });
    });
  }
  // Mock fallback so the dev shell clears messages + refreshes the sidebar on
  // profile switch (matching the real Wails event-driven flow).
  const wrapped = (payload: unknown) => {
    const e = (payload ?? {}) as { tabId?: string; profile?: string };
    cb({ tabId: e.tabId ?? "", profile: e.profile ?? "dev" });
  };
  (mockEventListeners["profile:changed"] ??= []).push(wrapped);
  return () => {
    const arr = mockEventListeners["profile:changed"] ?? [];
    mockEventListeners["profile:changed"] = arr.filter((x) => x !== wrapped);
  };
}

// onSchedulerChanged fires when the scheduled-task list mutates (create/update/
// delete/run). Payload-free — the automation panel re-lists on this event to
// keep cards live without polling.
export function onSchedulerChanged(cb: () => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("scheduler:changed", () => cb());
  }
  return () => {};
}

// onSchedulerNotice fires when a task with OutputMode="notify" runs (in-app
// desktop toast). Payload: {name, result}. The toast layer subscribes once at
// app root so notices surface even when the user isn't on the automation tab.
export function onSchedulerNotice(cb: (e: { name: string; result: string }) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("scheduler:notice", (...data: unknown[]) => {
      const e = (data?.[0] ?? {}) as { name?: string; result?: string };
      cb({ name: e.name ?? "", result: e.result ?? "" });
    });
  }
  return () => {};
}

// onCalendarChanged fires when the calendar event list mutates (create/update/
// delete). Payload-free — the calendar panel re-lists on this event.
export function onCalendarChanged(cb: () => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("calendar:changed", () => cb());
  }
  return () => {};
}

// onRagChanged fires when the RAG tree/collections mutate (import/remove/status
// change). Payload-free — the panel re-fetches the tree.
export function onRagChanged(cb: () => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("rag:changed", () => cb());
  }
  return () => {};
}

// onRagProgress fires on each chunk extraction completion. Payload is a
// RagProgressEvent; the panel updates the matching tree node's progress bar.
export function onRagProgress(cb: (e: RagProgressEvent) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("rag:progress", (...data: unknown[]) => {
      const e = (data?.[0] ?? {}) as Partial<RagProgressEvent>;
      cb({
        jobId: e.jobId ?? "",
        collection: e.collection ?? "",
        path: e.path ?? "",
        status: e.status ?? "",
        doneChunks: e.doneChunks ?? 0,
        totalChunks: e.totalChunks ?? 0,
        avgLatencyMs: e.avgLatencyMs ?? 0,
        message: e.message ?? "",
      });
    });
  }
  return () => {};
}

// onRagRunSkill fires when the user selects a skill from the knowledge-ref panel.
// Payload: { skill, arguments, refPath }. The chat should invoke the skill.
export function onRagRunSkill(cb: (e: { skill: string; arguments: string; refPath: string }) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("rag:run-skill", (...data: unknown[]) => {
      const e = (data?.[0] ?? {}) as Record<string, unknown>;
      cb({
        skill: String(e.skill ?? ""),
        arguments: String(e.arguments ?? ""),
        refPath: String(e.refPath ?? ""),
      });
    });
  }
  return () => {};
}

// onExpertsCollab fires during an expert-team run (expert chunks, synthesis,
// completion). Payload is a CollabEvent; the panel appends text deltas.
export function onExpertsCollab(cb: (e: CollabEvent) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("experts:collab", (...data: unknown[]) => {
      const e = (data?.[0] ?? {}) as Partial<CollabEvent>;
      cb({
        runId: e.runId ?? "",
        teamId: e.teamId ?? "",
        teamName: e.teamName ?? "",
        phase: e.phase ?? "",
        expertIdx: e.expertIdx ?? 0,
        expertName: e.expertName ?? "",
        round: e.round ?? 0,
        text: e.text ?? "",
        message: e.message ?? "",
        mode: e.mode ?? "",
      });
    });
  }
  return () => {};
}

// onExpertsChanged fires when the team list mutates. Payload-free — re-list.
export function onExpertsChanged(cb: () => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("experts:changed", () => cb());
  }
  return () => {};
}


// outside the shell), so a late-injected window.go is picked up transparently.
export const app: AppBindings = new Proxy({} as AppBindings, {
  get(_t, prop) {
    const target = realApp() ?? getMock();
    const v = (target as unknown as Record<string, unknown>)[String(prop)];
    return typeof v === "function" ? (v as (...a: unknown[]) => unknown).bind(target) : v;
  },
});

// openExternal opens a URL in the system browser (so links in rendered markdown
// don't navigate the webview away from the app). Falls back to window.open in the
// browser dev mock.
export function openExternal(url: string): void {
  if (typeof window !== "undefined" && window.runtime?.BrowserOpenURL) {
    window.runtime.BrowserOpenURL(url);
  } else if (typeof window !== "undefined") {
    window.open(url, "_blank", "noopener");
  }
}

// --- browser dev mock --------------------------------------------------------

const listeners = new Set<(e: WireEvent) => void>();
let mockScopedTabId: string | undefined;

function mockSubscribe(cb: (e: WireEvent) => void): () => void {
  listeners.add(cb);
  return () => {
    listeners.delete(cb);
  };
}

function emit(e: WireEvent) {
  const event = mockScopedTabId && !e.tabId ? { ...e, tabId: mockScopedTabId } : e;
  listeners.forEach((l) => l(event));
}

async function withMockTabScope<T>(tabId: string, fn: () => Promise<T>): Promise<T> {
  const previous = mockScopedTabId;
  mockScopedTabId = tabId || previous;
  try {
    return await fn();
  } finally {
    mockScopedTabId = previous;
  }
}

// Updater progress has its own listener set so the browser dev mock's ApplyUpdate
// can stream a fake download through onUpdaterProgress.
const updaterListeners = new Set<(p: UpdateProgress) => void>();

function emitUpdater(p: UpdateProgress) {
  updaterListeners.forEach((l) => l(p));
}

function delay(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

function baseName(path: string): string {
  return path.replace(/[/\\]+$/, "").split(/[/\\]/).filter(Boolean).pop() ?? path;
}

function browserPlatformOverride(): "darwin" | "windows" | "linux" | "" {
  if (typeof window === "undefined" || window.runtime) return "";
  const value = new URLSearchParams(window.location.search).get("platform");
  return value === "darwin" || value === "windows" || value === "linux" ? value : "";
}

function mockScenario(): "demo" | "fresh" | "running" {
  if (typeof window === "undefined") return "demo";
  const value = new URLSearchParams(window.location.search).get("mock")?.trim().toLowerCase();
  if (value === "fresh" || value === "empty" || value === "first-run") return "fresh";
  if (value === "running" || value === "busy" || value === "streaming") return "running";
  return "demo";
}

function makeMockApp(): AppBindings {
  const scenario = mockScenario();
  const freshMock = scenario === "fresh";
  const runningMock = scenario === "running";
  let cancelled = false;
  let pendingAskPreview = false;
  let pendingApprovalPreview = false;
  const globalWorkspaceRoot = "~/Library/Application Support/momapeer/global-workspace";
  let cwd = freshMock ? globalWorkspaceRoot : "~/projects/joyquant-db"; // mutable so PickWorkspace is visible in dev
  let workspaces = freshMock ? [] : ["~/projects/joyquant-db", "~/projects/joyquant-sys", "~/projects/momapeer", "~/projects/blade"];
  let mockEffort = "auto";
  // In-memory RAG state for browser dev. Seeded with one file mid-extraction so
  // the panel shows a live progress bar + ETA outside the Wails shell.
  let mockRagDocs = 3;
  let mockRagEntities = 12;
  let mockRagTree: RagNodeView[] = freshMock ? [] : [
    {
      key: "/mock/doc.md", label: "会议纪要.md", kind: "file", path: "/mock/doc.md", relPath: "会议纪要.md",
      isDir: false, collection: "default", status: "extracting", hasFts5: true,
      jobId: "rag_job_mock_demo", doneChunks: 3, totalChunks: 10, entityCount: 0, errorMsg: "",
    },
  ];
  // simulateRagProgress advances a mock node's doneChunks every ~1.5s until done.
  // jobId is kept in the signature for parity with the real backend's events.
  const simulateRagProgress = (_jobId: string, node: RagNodeView) => {
    const h = setInterval(() => {
      if (node.doneChunks >= node.totalChunks) {
        node.status = "enriched"; node.entityCount = Math.floor(Math.random() * 8) + 2;
        mockRagEntities += node.entityCount;
        clearInterval(h);
        return;
      }
      node.doneChunks++;
    }, 1500);
  };
  // In-memory scheduled-task store for browser dev. Seeded with one sample so
  // the automation panel isn't empty outside the Wails shell.
  let mockSchedulerTasks: TaskView[] = freshMock ? [] : [
    {
      id: "sched_mock_demo",
      name: "日报提醒",
      expression: "daily 18:00 Mon-Fri",
      prompt: "请整理今日工作日报，按三段式汇总。",
      profile: "cowork",
      enabled: true,
      oneShot: false,
      lastRun: "2026-06-21 18:00",
      nextRun: "2026-06-22 18:00",
      runCount: 12,
      lastResult: "日报已生成",
      outputMode: "notify",
      outputDest: "",
      outputDir: "",
      humanSchedule: "工作日 18:00",
      source: "manual",
      calendarEventId: "",
      outputAccount: "",
      plain: false,
      lastDeliverErr: "",
      lastDeliverAt: "",
    },
  ];
  const cloneTask = (t: TaskView): TaskView => JSON.parse(JSON.stringify(t)) as TaskView;
  const day = 86_400_000;
  const t0 = Date.now();
  // Mutable so MCP add/remove/retry are observable in browser dev.
  let capServers: ServerView[] = [
    {
      name: "codegraph",
      transport: "stdio",
      status: "disabled",
      builtIn: true,
      configured: true,
      autoStart: false,
      tier: "background",
      tools: 0,
      prompts: 0,
      resources: 0,
      toolList: [
        { name: "search", description: "Search symbols, files, and text in the workspace." },
        { name: "context", description: "Fetch surrounding source context for a symbol or file." },
        { name: "trace", description: "Follow callers and callees across the code graph." },
        { name: "node", description: "Inspect a specific graph node." },
      ],
    },
    { name: "github", transport: "stdio", status: "connected", configured: true, autoStart: true, tier: "background", command: "npx", args: ["-y", "@modelcontextprotocol/server-github"], tools: 12, prompts: 2, resources: 0 },
    {
      name: "linear",
      transport: "http",
      status: "initializing",
      configured: true,
      autoStart: true,
      tier: "background",
      url: "https://mcp.linear.app/mcp",
      authStatus: "possible",
      authUrl: "https://mcp.linear.app/mcp",
      tools: 8,
      prompts: 0,
      resources: 0,
      toolList: [
        { name: "list_issues", description: "List and filter Linear issues." },
        { name: "get_issue", description: "Fetch a Linear issue by id or key." },
        { name: "create_issue", description: "Create a Linear issue." },
        { name: "update_issue", description: "Update status, assignee, priority, or labels." },
        { name: "list_projects", description: "List Linear projects." },
        { name: "get_project", description: "Fetch project details." },
        { name: "list_teams", description: "List Linear teams." },
        { name: "search", description: "Search Linear workspace objects." },
      ],
    },
    { name: "figma", transport: "http", status: "failed", configured: true, autoStart: true, tier: "background", url: "https://mcp.figma.com/mcp", authStatus: "required", authUrl: "https://mcp.figma.com/mcp", tools: 0, prompts: 0, resources: 0, error: "connect: 401 unauthorized" },
  ];
  const capSkills: SkillView[] = [
    { name: "explore", description: "Investigate the codebase in an isolated subagent", scope: "builtin", runAs: "subagent", enabled: true },
    { name: "review", description: "Review the staged diff", scope: "project", runAs: "inline", enabled: false },
    { name: "init", description: "Scaffold a project memory doc (momapeer.md) for this repo", scope: "builtin", runAs: "inline", enabled: true },
  ];
  let capSkillRoots: SkillRootView[] = [
    { dir: "~/projects/momapeer/.momapeer/skills", scope: "project", priority: 1, status: "missing", configured: false, removable: true, skills: 0 },
    {
      dir: "~/my-skills",
      scope: "custom",
      priority: 5,
      status: "ok",
      configured: true,
      removable: true,
      skills: 1,
      skillItems: [{ name: "review", description: "Review the staged diff", scope: "custom", runAs: "inline" }],
    },
    {
      dir: "~/.momapeer/skills",
      scope: "global",
      priority: 6,
      status: "ok",
      configured: false,
      removable: true,
      skills: 2,
      skillItems: [
        { name: "explore", description: "Investigate the codebase in an isolated subagent", scope: "global", runAs: "subagent" },
        { name: "init", description: "Scaffold a project memory doc (momapeer.md) for this repo", scope: "global", runAs: "inline" },
      ],
    },
  ];
  const mockSwitchWorkspace = async (path: string) => {
    cwd = path || "~";
    workspaces = [cwd, ...workspaces.filter((p) => p !== cwd)].slice(0, 12);
    if (!mockProjectTree.some((node) => node.kind === "project" && node.root === cwd)) {
      mockProjectTree.unshift({
        key: `project_${cwd}`,
        kind: "project",
        label: baseName(cwd),
        root: cwd,
        children: [],
      });
    }
    return cwd;
  };
  // Mutable so delete/rename are observable in browser dev.
  const sessions: SessionMeta[] = [
    { path: "/mock/sessions/a.jsonl", preview: "fix the login bug in auth.go", turns: 12, createdAt: t0 - 2 * day, lastActivityAt: t0 - 3_600_000, modTime: t0 - 3_600_000, current: true, open: true },
    { path: "/mock/sessions/b.jsonl", preview: "refactor the payment module", turns: 5, createdAt: t0 - 3 * day, lastActivityAt: t0 - 6 * 3_600_000, modTime: t0 - 6 * 3_600_000, current: false, open: true },
    { path: "/mock/sessions/c.jsonl", preview: "write the README and badges", turns: 8, createdAt: t0 - 4 * day, lastActivityAt: t0 - day - 3_600_000, modTime: t0 - day - 3_600_000, current: false, open: false },
    { path: "/mock/sessions/d.jsonl", preview: "explain the plugin host design", turns: 3, createdAt: t0 - 5 * day, lastActivityAt: t0 - 4 * day, modTime: t0 - 4 * day, current: false, open: false },
  ];
  const trashedSessions: SessionMeta[] = [
    {
      path: "/mock/sessions/.trash/trash-dev-standard.jsonl",
      title: t("mock.trashDevStandardTitle"),
      preview: t("mock.trashDevStandardPreview"),
      turns: 4,
      createdAt: t0 - 8 * day,
      lastActivityAt: t0 - 7 * day,
      modTime: t0 - 7 * day,
      deletedAt: t0 - 20 * 60_000,
      current: false,
      open: false,
      scope: "project",
      workspaceRoot: "~/projects/joyquant-db",
      topicId: "topic_dev_standard",
      topicTitle: t("mock.trashDevStandardTitle"),
    },
    {
      path: "/mock/sessions/.trash/trash-p3a-review.jsonl",
      title: t("mock.trashP3aTitle"),
      preview: t("mock.trashP3aPreview"),
      turns: 7,
      createdAt: t0 - 6 * day,
      lastActivityAt: t0 - 5 * day,
      modTime: t0 - 5 * day,
      deletedAt: t0 - 2 * 3_600_000,
      current: false,
      open: false,
      scope: "project",
      workspaceRoot: "~/projects/joyquant-sys",
      topicId: "topic_p3a_pd",
      topicTitle: t("mock.trashP3aTitle"),
    },
    {
      path: "/mock/sessions/.trash/trash-global-product.jsonl",
      title: t("mock.trashGlobalProductTitle"),
      preview: t("mock.trashGlobalProductPreview"),
      turns: 2,
      createdAt: t0 - 4 * day,
      lastActivityAt: t0 - 3 * day,
      modTime: t0 - 3 * day,
      deletedAt: t0 - day,
      current: false,
      open: false,
      scope: "global",
      topicId: "topic_product",
      topicTitle: t("mock.trashGlobalProductTitle"),
    },
  ];
  if (freshMock) {
    sessions.splice(0);
    trashedSessions.splice(0);
  }
  // Mutable dream/distill status so the Memory panel's self-evolution section is
  // interactive in browser dev mode (no backend).
  const dreamMock: DreamStatusView = {
    enabled: true,
    dreamInterval: 7,
    distillInterval: 30,
    dreamInFlight: false,
    distillInFlight: false,
    history: [],
  };
  // Mutable settings so the Settings panel's edits are observable in browser dev.
  // hookSettings holds the per-scope mock hooks payload (global + project).
  const hookEvents = ["Startup", "PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop", "PostLLMCall", "SessionStart", "SessionEnd", "SubagentStop", "Notification", "PreCompact"];
  const hookSettings: Record<string, HooksSettingsView> = {
    global: {
      scope: "global",
      path: "~/.momapeer/settings.json",
      projectRoot: "",
      trusted: true,
      events: hookEvents,
      hooks: [],
    },
    project: {
      scope: "project",
      path: "./.momapeer/settings.json",
      projectRoot: "/mock/project",
      trusted: false,
      events: hookEvents,
      hooks: [],
    },
  };
  const settings: SettingsView = {
    defaultModel: "moma",
    fastTaskModel: "moma/qwen/qwen3.6-35b",
    subagentModel: "",
    subagentEffort: "",
    autoPlan: "off",
    providers: [
      { name: "moma", builtIn: true, added: false, kind: "openai", baseUrl: "https://jiutian.10086.cn/largemodel/moma/api/v3", modelsUrl: "", models: ["jiutian/jiutian-lan-236b", "jiutian/jiutian-lan-35b", "jiutian/jiutian-lan-thinking", "jiutian/jiutian-da-35b", "qwen/qwen3.6-35b", "qwen/qwen3.6-27b", "qwen/qwen3.5-397b-a17b", "deepseek/deepseek-v4-flash", "z.ai/glm-5.1", "z.ai/glm-5.2", "minimax/minimax-m2.7", "minimax/minimax-m2.5", "moonshotai/kimi-k2.6", "moonshotai/kimi-k2.5-thinking"], default: "minimax/minimax-m2.7", apiKeyEnv: "JIUTIAN_API_KEY", keySet: true, contextWindow: 200_000, reasoningProtocol: "", supportedEfforts: [], defaultEffort: "" },
    ],
    officialProviders: [
      { name: "moma", builtIn: true, added: false, kind: "openai", baseUrl: "https://jiutian.10086.cn/largemodel/moma/api/v3", modelsUrl: "", models: ["jiutian/jiutian-lan-236b", "jiutian/jiutian-lan-35b", "jiutian/jiutian-lan-thinking", "jiutian/jiutian-da-35b", "qwen/qwen3.6-35b", "qwen/qwen3.6-27b", "qwen/qwen3.5-397b-a17b", "deepseek/deepseek-v4-flash", "z.ai/glm-5.1", "z.ai/glm-5.2", "minimax/minimax-m2.7", "minimax/minimax-m2.5", "moonshotai/kimi-k2.6", "moonshotai/kimi-k2.5-thinking"], default: "minimax/minimax-m2.7", apiKeyEnv: "JIUTIAN_API_KEY", keySet: true, contextWindow: 200_000, reasoningProtocol: "", supportedEfforts: [], defaultEffort: "" },
    ],
    permissions: { mode: "ask", allow: ["read_file"], ask: [], deny: ["Bash(rm:*)"] },
    sandbox: { bash: "enforce", network: true, workspaceRoot: "", allowWrite: [] },
    network: {
      proxyMode: "auto",
      proxyUrl: "",
      noProxy: "",
      proxy: { type: "socks5", server: "127.0.0.1", port: 7890, username: "", password: "" },
    },
    agent: { temperature: 0.2, maxSteps: 0, plannerMaxSteps: 0, systemPrompt: "You are momapeer, a coding agent.", rpm: 60 },
    cowork: {
      browserPath: "",
      embeddingModel: "",
      ragEnabled: null,
      pptActiveTemplate: "",
      pptTemplates: [],
      pptTemplateDir: "",
      pptMode: "fast",
      smtp: { host: "", port: 0, from: "", username: "", passwordEnv: "COWORK_SMTP_PASSWORD", useTLS: false },
      imap: { host: "", port: 0, username: "", passwordEnv: "COWORK_IMAP_PASSWORD" },
      smtpPassword: "",
      imapPassword: "",
      smtpPasswordSet: false,
      imapPasswordSet: false,
      detectedBrowser: "",
      screenshotEnabled: false,
      screenshotHotkey: "Ctrl+Shift+Alt+W",
      screenshotPrompt: "",
      screenshotVlmModel: "qwen/qwen3.6-27b",
      estopHotkey: "Ctrl+Shift+Pause",
      emailAccounts: [],
      allowHeadlessEmail: false,
    },
    bot: {
      enabled: !freshMock,
      model: "",
      maxSteps: 25,
      debounceMs: 1500,
      allowlist: {
        enabled: true,
        allowAll: false,
        mode: "open",
        qqUsers: [],
        feishuUsers: [],
        weixinUsers: [],
        qqGroups: [],
        feishuGroups: [],
        weixinGroups: [],
      },
      qq: { enabled: false, appId: "", appSecretEnv: "QQ_BOT_APP_SECRET", secretSet: false },
      feishu: {
        enabled: false,
        domain: "feishu",
        appId: "",
        appSecretEnv: "FEISHU_BOT_APP_SECRET",
        secretSet: false,
        verificationToken: "",
        mode: "webhook",
        webhookPort: 8080,
        requireMention: true,
      },
      weixin: {
        enabled: false,
        accountId: "default",
        tokenEnv: "WEIXIN_BOT_TOKEN",
        tokenSet: false,
        apiBase: "https://ilinkai.weixin.qq.com",
      },
      connections: freshMock ? [] : [
        {
          id: "mock-lark-kun",
          provider: "feishu",
          domain: "lark",
          label: "kun",
          enabled: true,
          status: "connected",
          model: "",
          workspaceRoot: "",
          credential: {
            appId: "cli_mock_lark",
            appSecretEnv: "FEISHU_BOT_APP_SECRET",
            accountId: "",
            tokenEnv: "",
            secretSet: true,
          },
          sessionMappings: [
            {
              remoteId: "ou_3a2bdd60640aaa95518186677b1f6d8c",
              sessionId: "topic:topic_product",
              scope: "global",
              workspaceRoot: "",
              updatedAt: new Date(Date.now() - 4 * 60_000).toISOString(),
            },
          ],
          lastError: "",
          createdAt: new Date(Date.now() - 86_400_000).toISOString(),
          updatedAt: new Date(Date.now() - 4 * 60_000).toISOString(),
        },
        {
          id: "mock-weixin-kun",
          provider: "weixin",
          domain: "weixin",
          label: "kun",
          enabled: true,
          status: "connected",
          model: "",
          workspaceRoot: "",
          credential: {
            appId: "",
            appSecretEnv: "",
            accountId: "default",
            tokenEnv: "WEIXIN_BOT_TOKEN",
            secretSet: true,
          },
          sessionMappings: [
            {
              remoteId: "wxid_kun_auto",
              sessionId: "topic:topic_ai",
              scope: "global",
              workspaceRoot: "",
              updatedAt: new Date(Date.now() - 12 * 60_000).toISOString(),
            },
          ],
          lastError: "",
          createdAt: new Date(Date.now() - 86_400_000).toISOString(),
          updatedAt: new Date(Date.now() - 12 * 60_000).toISOString(),
        },
      ],
    },
    webSearch: {
      braveKeySet: false,
      exaKeySet: false,
      linkupKeySet: false,
    },
    jiutian: { imageUnderstand: true, imageGenerate: true, videoUnderstand: false },
    desktopLanguage: "",
    desktopTheme: "light",
    desktopThemeStyle: "graphite",
    closeBehavior: "background",
    displayMode: "minimal",
    checkUpdates: true,
    telemetry: true,
    expandThinking: false,
    configPath: "~/projects/momapeer/momapeer.toml",
    providerKinds: ["openai"],
    autoApproveTools: false,
    bypass: false,
  };
  settings.providers = settings.providers.map((provider) =>
    provider.apiKeyEnv === "JIUTIAN_API_KEY" ? { ...provider, keySet: !freshMock } : provider,
  );
  if (freshMock) {
    settings.configPath = "~/.config/momapeer/config.toml";
  }
  const mockNow = Date.now();
  const mockProjectTree: ProjectNode[] = freshMock ? [] : [
    {
      key: "project_~/projects/joyquant-db",
      kind: "project",
      label: t("mock.projectJoyquantDb"),
      root: "~/projects/joyquant-db",
      projectColor: "blue",
      children: [
        { key: "topic_dev_standard", kind: "topic", label: `● ${t("mock.topicDevStandard")}`, root: "~/projects/joyquant-db", topicId: "topic_dev_standard", projectColor: "blue", turns: 18, lastActivityAt: mockNow - 8 * 60_000, open: true, running: runningMock },
        { key: "topic_db_maint", kind: "topic", label: t("mock.topicDbMaint"), root: "~/projects/joyquant-db", topicId: "topic_db_maint", projectColor: "blue", turns: 7, lastActivityAt: mockNow - 2 * 60 * 60_000 },
        { key: "topic_env", kind: "topic", label: t("mock.topicEnv"), root: "~/projects/joyquant-db", topicId: "topic_env", projectColor: "blue", turns: 3, lastActivityAt: mockNow - 26 * 60 * 60_000 },
      ],
    },
    {
      key: "project_~/projects/joyquant-sys",
      kind: "project",
      label: t("mock.projectJoyquantSys"),
      root: "~/projects/joyquant-sys",
      projectColor: "purple",
      children: [
        { key: "topic_p3b_pd", kind: "topic", label: `● ${t("mock.topicP3b")}`, root: "~/projects/joyquant-sys", topicId: "topic_p3b_pd", projectColor: "purple", turns: 11, lastActivityAt: mockNow - 3 * 24 * 60 * 60_000, status: runningMock ? "streaming" : undefined },
        { key: "topic_p3a_pd", kind: "topic", label: t("mock.topicP3a"), root: "~/projects/joyquant-sys", topicId: "topic_p3a_pd", projectColor: "purple", turns: 9, lastActivityAt: mockNow - 4 * 24 * 60 * 60_000, status: runningMock ? "thinking" : undefined },
        { key: "topic_hotfix", kind: "topic", label: t("mock.topicHotfix"), root: "~/projects/joyquant-sys", topicId: "topic_hotfix", projectColor: "purple", turns: 4, lastActivityAt: mockNow - 5 * 24 * 60 * 60_000, status: runningMock ? "thinking" : undefined },
        { key: "topic_sys_coord", kind: "topic", label: t("mock.topicSysCoord"), root: "~/projects/joyquant-sys", topicId: "topic_sys_coord", projectColor: "purple", turns: 14, lastActivityAt: mockNow - 6 * 24 * 60 * 60_000, status: runningMock ? "waiting_confirmation" : undefined },
        { key: "topic_sys_standard", kind: "topic", label: t("mock.topicSysStandard"), root: "~/projects/joyquant-sys", topicId: "topic_sys_standard", projectColor: "purple", turns: 6, lastActivityAt: mockNow - 7 * 24 * 60 * 60_000, status: "paused" },
        { key: "topic_sys_exception", kind: "topic", label: t("mock.topicSysException"), root: "~/projects/joyquant-sys", topicId: "topic_sys_exception", projectColor: "purple", turns: 2, lastActivityAt: mockNow - 8 * 24 * 60 * 60_000, status: "error" },
      ],
    },
    {
      key: "global_folder",
      kind: "global_folder",
      label: "Global",
      root: globalWorkspaceRoot,
      children: [
        { key: "global_topic_product", kind: "global_topic", label: t("mock.topicProduct"), topicId: "topic_product", turns: 5, lastActivityAt: mockNow - 8 * 24 * 60 * 60_000 },
        { key: "global_topic_ai", kind: "global_topic", label: t("mock.topicAi"), topicId: "topic_ai", turns: 8, lastActivityAt: mockNow - 10 * 24 * 60 * 60_000 },
        { key: "global_topic_lab", kind: "global_topic", label: t("mock.topicLab"), topicId: "topic_lab", turns: 2, lastActivityAt: mockNow - 12 * 24 * 60 * 60_000 },
      ],
    },
  ];
  const ensureMockGlobalFolder = (): ProjectNode => {
    let node = mockProjectTree.find((item) => item.kind === "global_folder");
    if (!node) {
      node = {
        key: "global_folder",
        kind: "global_folder",
        label: "Global",
        root: globalWorkspaceRoot,
        children: [],
      };
      mockProjectTree.push(node);
    }
    return node;
  };
  const cloneProjectTree = () => {
    if (mockProjectTree.length === 0) ensureMockGlobalFolder();
    return JSON.parse(JSON.stringify(mockProjectTree)) as ProjectNode[];
  };
  const projectChildren = (node: ProjectNode): ProjectNode[] => Array.isArray(node.children) ? node.children : [];
  const findMockTopic = (topicId: string): ProjectNode | null => {
    for (const parent of mockProjectTree) {
      const found = projectChildren(parent).find((child) => child.topicId === topicId);
      if (found) return found;
    }
    return null;
  };
  const deleteMockTopic = (topicId: string) => {
    for (const parent of mockProjectTree) {
      parent.children = projectChildren(parent).filter((child) => child.topicId !== topicId);
    }
  };
  const topicLabel = (topicId: string, fallback: string) => (findMockTopic(topicId)?.label || fallback).replace(/^●\s*/, "");
  const mockTopicStatus = (topicId: string) => findMockTopic(topicId)?.status ?? "";
  const mockTopicIsRunning = (topicId: string) => {
    const status = mockTopicStatus(topicId);
    return status === "streaming" || status === "thinking" || status === "waiting_confirmation";
  };
  const mockTopicRunsInScenario = (topicId: string) => runningMock && mockTopicIsRunning(topicId);
  const mockTopicHistory = (topicId: string): HistoryMessage[] => {
    switch (topicId) {
      case "topic_product":
        return [
          {
            role: "user",
            content: [
              "[[momapeer-im]]",
              "provider=lark",
              "label=Feishu / Lark",
              "sender=ou_3a2bdd60640aaa95518186677b1f6d8c",
              "chat=p2p 会话",
              "[[/momapeer-im]]",
              "你可以做什么",
            ].join("\n"),
          },
          {
            role: "assistant",
            content: "这是 Global 范围下的 IM 会话。我可以先处理不依赖项目文件的问答、计划和信息整理；需要进入项目时，再由桌面端显式绑定或迁移到项目话题。",
          },
        ];
      case "topic_ai":
        return [
          {
            role: "user",
            content: [
              "[[momapeer-im]]",
              "provider=weixin",
              "label=微信",
              "sender=wxid_kun_auto",
              "chat=单聊",
              "[[/momapeer-im]]",
              "帮我整理一下今天要做的事",
            ].join("\n"),
          },
          {
            role: "assistant",
            content: "可以。我会先在 Global 范围里整理任务清单；如果某条任务需要读取项目文件，再切到你授权的项目话题处理。",
          },
        ];
      case "topic_dev_standard":
        return [
          {
            role: "user",
            content: [
              "[[momapeer-im]]",
              "provider=lark",
              "label=Feishu / Lark",
              "sender=ou_3a2bdd60640aaa95518186677b1f6d8c",
              "chat=p2p 会话",
              "[[/momapeer-im]]",
              "你可以做什么",
            ].join("\n"),
          },
          {
            role: "assistant",
            content: "我可以在桌面端帮你处理代码编写、文件操作、项目分析和问题定位。来自 IM 的请求会进入同一条聊天时间线，桌面端继续承载模型调用、工具执行和上下文管理。",
          },
        ];
      case "topic_p3b_pd":
        return [
          { role: "user", content: "把 p3b P&D 的范围和风险重新整理成可执行计划。" },
          { role: "phase", content: "分析需求范围" },
        ];
      case "topic_p3a_pd":
        return [
          { role: "user", content: "复盘 p3a 的技术方案，先不要写文件，先说明你的判断。" },
        ];
      case "topic_hotfix":
        return [
          { role: "user", content: "检查 post-p3-hotfix 的回归风险，重点看最近的 shell 输出和 git 改动。" },
          { role: "assistant", content: "", reasoning: "我先定位最近一次 hotfix 的上下文，然后用只读命令检查状态；左侧保持“思考中”，工具细节在这里展开。" },
        ];
      case "topic_sys_coord":
        return [
          { role: "user", content: "准备执行 joyquant-sys 的同步脚本，但需要我确认后再运行。" },
          { role: "assistant", content: "", reasoning: "这个动作会运行脚本并可能刷新本地缓存，所以需要先等用户确认。" },
        ];
      case "topic_sys_standard":
        return [
          { role: "user", content: "继续制定 SYS 项目开发规范，先停在当前检查点。" },
          { role: "assistant", content: "已暂停在规范整理阶段。当前保留了目录约定、分支策略和待确认的发布检查项；继续时可以从这里恢复。" },
          { role: "notice", level: "info", content: "会话已暂停：未继续执行命令，等待用户恢复或切换任务。" },
        ];
      case "topic_sys_exception":
        return [
          { role: "user", content: "演练异常处理流程，看看失败时界面怎么提示。" },
          { role: "assistant", content: "我尝试校验恢复脚本时遇到异常，已停止继续执行。" },
          { role: "notice", level: "warn", content: "运行异常：恢复脚本缺少必要环境变量 JOYQUANT_SYS_TOKEN。请补齐配置后重试。" },
        ];
      default:
        return [];
    }
  };
  const mockRuntimeInjected = new Set<string>();
  const queueMockTopicRuntime = (tab: TabMeta) => {
    if (!runningMock) return;
    const status = mockTopicStatus(tab.topicId);
    if (status !== "streaming" && status !== "thinking" && status !== "waiting_confirmation") return;
    const key = `${tab.id}:${tab.topicId}:${status}`;
    if (mockRuntimeInjected.has(key)) return;
    mockRuntimeInjected.add(key);
    window.setTimeout(() => {
      void withMockTabScope(tab.id, async () => {
        emitMockTurnStarted();
        await delay(120);
        if (tab.topicId === "topic_p3b_pd") {
          const text = "我会先把范围拆成三层：目标、依赖、风险。当前已经确认 p3b 的交付边界，接下来补充每个模块的验收口径...";
          for (const ch of text) {
            emit({ kind: "text", text: ch });
            await delay(5);
          }
          return;
        }
        if (tab.topicId === "topic_p3a_pd") {
          emit({ kind: "reasoning", text: "我正在对比 p3a 和 p3b 的差异：先看约束，再看变更风险，最后判断是否需要拆成独立任务。\n\n" });
          await delay(220);
          emit({ kind: "reasoning", text: "当前倾向：先保留 p3a 的兼容路径，不急于删除旧逻辑。" });
          return;
        }
        if (tab.topicId === "topic_hotfix") {
          const id = "mock-hotfix-shell";
          emit({ kind: "tool_dispatch", tool: { id, name: "bash", args: JSON.stringify({ command: "git status --short && npm test" }), readOnly: true } });
          await delay(180);
          emit({ kind: "tool_progress", tool: { id, name: "bash", readOnly: true, output: "$ git status --short\n M internal/sys/runner.go\n\n$ npm test\nrunning targeted regression tests...\n" } });
          return;
        }
        if (tab.topicId === "topic_sys_coord") {
          pendingApprovalPreview = true;
          emit({ kind: "reasoning", text: "我已经准备好执行同步脚本，但这个操作会影响本地 workspace，需要用户确认。" });
          await delay(160);
          emit({
            kind: "approval_request",
            approval: {
              id: "mock-sys-confirm",
              tool: "bash",
              subject: "npm run sync:joyquant-sys\n\n该命令会同步 SYS 项目配置并刷新本地缓存。",
            },
          });
        }
      });
    }, 180);
  };
  const setMockActiveTab = (tabId: string) => {
    mockTabs = mockTabs.map((tab) => ({ ...tab, active: tab.id === tabId }));
  };
  const currentMockTurnTabId = () => mockScopedTabId || mockTabs.find((tab) => tab.active)?.id;
  const setMockTabRunning = (tabId: string | undefined, running: boolean) => {
    if (!tabId) return;
    mockTabs = mockTabs.map((tab) => (tab.id === tabId ? { ...tab, running } : tab));
  };
  const emitMockTurnStarted = () => {
    setMockTabRunning(currentMockTurnTabId(), true);
    emit({ kind: "turn_started" });
  };
  const emitMockTurnDone = () => {
    setMockTabRunning(currentMockTurnTabId(), false);
    emit({ kind: "turn_done" });
  };
  let mockTabs: TabMeta[] = freshMock ? [
    {
      id: "tab_global",
      scope: "global",
      workspaceRoot: globalWorkspaceRoot,
      workspaceName: "Global",
      topicId: "",
      topicTitle: "Global",
      label: "Qwen3.6-35B",
      ready: true,
      running: false,
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      active: true,
      cwd: globalWorkspaceRoot,
    },
  ] : [
    {
      id: "tab_joyquant_db",
      scope: "project",
      workspaceRoot: "~/projects/joyquant-db",
      workspaceName: "joyquant-db",
      topicId: "topic_dev_standard",
      topicTitle: t("mock.trashDevStandardTitle"),
      projectColor: "blue",
      label: "Qwen3.6-35B",
      ready: true,
      running: false,
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      active: true,
      cwd: "~/projects/joyquant-db",
    },
    {
      id: "tab_joyquant_sys",
      scope: "project",
      workspaceRoot: "~/projects/joyquant-sys",
      workspaceName: "joyquant-sys",
      topicId: "topic_p3b_pd",
      topicTitle: "p3b P&D",
      projectColor: "purple",
      label: "Qwen3.6-35B",
      ready: true,
      running: runningMock && mockTopicIsRunning("topic_p3b_pd"),
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      active: false,
      cwd: "~/projects/joyquant-sys",
    },
    {
      id: "tab_global",
      scope: "global",
      workspaceRoot: "",
      workspaceName: "Global",
      topicId: "topic_global",
      topicTitle: "Global",
      label: "Qwen3.6-35B",
      ready: true,
      running: false,
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      active: false,
      cwd: "~/projects/joyquant-db",
    },
  ];
  const mockModelCatalog = [
    { ref: "qwen/qwen3.6-35b", provider: "moma", model: "qwen3.6-35b" },
    { ref: "moonshotai/kimi-k2.6", provider: "moma", model: "kimi-k2.6" },
    { ref: "minimax/minimax-m2.7", provider: "moma", model: "minimax-m2.7" },
  ];
  const defaultMockModelRef = mockModelCatalog[0].ref;
  const mockModelRef = (name: string): string => {
    const trimmed = name.trim();
    if (!trimmed || trimmed === "Qwen3.6-35B") return defaultMockModelRef;
    const exact = mockModelCatalog.find((model) => model.ref === trimmed);
    if (exact) return exact.ref;
    const byModel = mockModelCatalog.find((model) => model.model === trimmed);
    return byModel?.ref ?? trimmed;
  };
  const mockModelLabel = (ref: string): string => mockModelCatalog.find((model) => model.ref === mockModelRef(ref))?.model ?? ref.split("/").pop() ?? ref;
  const mockTabModelRef = (tab?: TabMeta): string => mockModelRef(tab?.label ?? "");
  const setMockTabModel = (tabID: string | undefined, name: string) => {
    const ref = mockModelRef(name);
    const label = mockModelLabel(ref);
    let applied = false;
    mockTabs = mockTabs.map((tab) => {
      const match = tabID ? tab.id === tabID : tab.active;
      if (!match) return tab;
      applied = true;
      return { ...tab, label };
    });
    if (!applied && mockTabs.length > 0) {
      mockTabs = mockTabs.map((tab, index) => (index === 0 ? { ...tab, label } : tab));
    }
  };
  // Profile mock helpers: profile lives on TabMeta.profile (absent = dev). The
  // mock emits a profile:changed event so the dev-shell layout swap can be
  // exercised without the Go backend.
  const mockProfileOf = (tab?: TabMeta): string => (tab?.profile ?? "dev").toLowerCase() || "dev";
  const setMockTabProfile = (tabID: string | undefined, name: string) => {
    const profile = (name || "dev").toLowerCase();
    let affectedId: string | undefined;
    mockTabs = mockTabs.map((tab) => {
      const match = tabID ? tab.id === tabID : tab.active;
      if (!match) return tab;
      affectedId = tab.id;
      return { ...tab, profile };
    });
    if (affectedId) {
      // Emit through the mock event bus so the dev shell's profile:changed /
      // project-tree:changed handlers fire — mirroring the real Wails flow
      // (backend emits both after a SwitchProfileForTab rebuild).
      emitMockEvent("profile:changed", { tabId: affectedId, profile });
      emitMockEvent("project-tree:changed");
      emitMockEvent("agent:ready", { tabId: affectedId });
    }
  };
  return {
    async Platform() {
      const override = browserPlatformOverride();
      if (override) return override;
      // Mirror the OS the browser dev mock runs on.
      const ua = typeof navigator !== "undefined" ? navigator.userAgent : "";
      if (/Win/i.test(ua)) return "windows";
      if (/Mac/i.test(ua)) return "darwin";
      return "linux";
    },
        async Submit(input) {
          cancelled = false;
      emitMockTurnStarted();
      const trimmedInput = input.trim().toLowerCase();
      const goalMatch = /^\/goal(?:\s+([\s\S]*))?$/.exec(input.trim());
      if (goalMatch) {
        const arg = (goalMatch[1] ?? "").trim();
        const lowered = arg.toLowerCase();
        const active = mockTabs.find((tab) => tab.active);
        if (!arg || lowered === "status") {
          emit({ kind: "notice", level: "info", text: active?.goal ? `goal: ${active.goal}` : "goal: none" });
          emitMockTurnDone();
          return;
        }
        if (["clear", "off", "stop", "done"].includes(lowered)) {
          mockTabs = mockTabs.map((tab) => (tab.active ? { ...tab, goal: "", goalStatus: "stopped", collaborationMode: "normal" } : tab));
          emit({ kind: "notice", level: "info", text: "goal cleared" });
          emitMockTurnDone();
          return;
        }
        mockTabs = mockTabs.map((tab) => (tab.active ? { ...tab, goal: arg, goalStatus: "running", collaborationMode: "goal" } : tab));
        emit({ kind: "notice", level: "info", text: `goal set: ${arg}` });
        await delay(350);
        if (cancelled) return;
        const reply = `Autonomous goal run started for: **${arg}**\n\nMock run completed.\n\n[goal:complete]`;
        emit({ kind: "message", text: reply });
        mockTabs = mockTabs.map((tab) => (tab.active ? { ...tab, goal: "", goalStatus: "complete", collaborationMode: "normal" } : tab));
        emit({ kind: "notice", level: "info", text: "goal complete" });
        emitMockTurnDone();
        return;
      }
      if (trimmedInput === "/approve-preview" || trimmedInput === "approve preview" || trimmedInput === "approve预览") {
        pendingApprovalPreview = true;
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "approval_request",
          approval: {
            id: "mock-approval-preview",
            tool: "bash",
            subject: t("mock.approvalSubject"),
          },
        });
        return;
      }
      if (
        trimmedInput === "/plan-approve-preview" ||
        trimmedInput === "plan approve preview" ||
        trimmedInput === "plan approve预览"
      ) {
        pendingApprovalPreview = true;
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "approval_request",
          approval: {
            id: "mock-plan-approval-preview",
            tool: "exit_plan_mode",
            subject: "",
          },
        });
        return;
      }
      if (trimmedInput === "/ask-preview" || trimmedInput === "ask preview" || trimmedInput === "ask预览") {
        pendingAskPreview = true;
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "ask_request",
          ask: {
            id: "mock-ask-preview",
            questions: [
              {
                id: "q1",
                header: t("mock.askQ1Header"),
                prompt: t("mock.askQ1Prompt"),
                options: [
                  { label: t("mock.askQ1Opt1Label"), description: t("mock.askQ1Opt1Desc") },
                  { label: t("mock.askQ1Opt2Label"), description: t("mock.askQ1Opt2Desc") },
                  { label: t("mock.askQ1Opt3Label"), description: t("mock.askQ1Opt3Desc") },
                ],
              },
              {
                id: "q2",
                header: t("mock.askQ2Header"),
                prompt: t("mock.askQ2Prompt"),
                options: [
                  { label: t("mock.askQ2Opt1Label"), description: t("mock.askQ2Opt1Desc") },
                  { label: t("mock.askQ2Opt2Label"), description: t("mock.askQ2Opt2Desc") },
                  { label: t("mock.askQ2Opt3Label"), description: t("mock.askQ2Opt3Desc") },
                ],
              },
            ],
          },
        });
        return;
      }
      if (trimmedInput === "/todo-preview" || trimmedInput === "todo preview" || trimmedInput === "todo预览") {
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "tool_dispatch",
          tool: {
            id: "mock-todo-preview",
            name: "todo_write",
            args: JSON.stringify({
              todos: [
                { content: t("mock.todo1"), status: "completed" },
                { content: t("mock.todo2"), activeForm: t("mock.todo2ActiveForm"), status: "in_progress" },
                { content: t("mock.todo3"), status: "pending" },
              ],
            }),
            readOnly: false,
          },
        });
        await delay(150);
        emit({
          kind: "tool_result",
          tool: {
            id: "mock-todo-preview",
            name: "todo_write",
            args: JSON.stringify({
              todos: [
                { content: t("mock.todo1"), status: "completed" },
                { content: t("mock.todo2"), activeForm: t("mock.todo2ActiveForm"), status: "in_progress" },
                { content: t("mock.todo3"), status: "pending" },
              ],
            }),
            output: "todo list updated",
            readOnly: false,
            durationMs: 150,
          },
        });
        emitMockTurnDone();
        return;
      }
      if (trimmedInput === "/process-preview" || trimmedInput === "process preview" || trimmedInput === "过程预览") {
        await delay(200);
        if (cancelled) return;
        emit({ kind: "phase", text: "Preparing context" });
        await delay(120);
        emit({ kind: "notice", level: "info", text: "Loaded project instructions from AGENTS.md." });
        await delay(120);
        emit({ kind: "notice", level: "warn", text: "Network access is enabled; external results may change over time." });
        await delay(120);
        emit({ kind: "compaction_started", compaction: { trigger: "manual" } });
        await delay(320);
        emit({
          kind: "compaction_done",
          compaction: {
            trigger: "manual",
            messages: 6,
            summary: "Preserved the active task, relevant files, and UI decisions while trimming earlier exploratory context.",
          },
        });
        emit({ kind: "message", text: "Process card preview complete." });
        emitMockTurnDone();
        return;
      }
      // Simulate the server's pre-first-token latency so the deferred user bubble
      // and the "un-send on Esc before any reply" path are observable in browser
      // dev. Bail if cancelled during the wait — nothing was streamed yet.
      await delay(700);
      if (cancelled) return;
      const reply =
        `You said: **${input}**\n\n` +
        "This is the browser dev mock — the real reply comes from the kernel " +
        "inside the Wails shell. Here's a fenced block to exercise the editor seam:\n\n" +
        "```go\nfunc main() {\n    println(\"hello from the mock\")\n}\n```\n";
      for (const ch of reply) {
        if (cancelled) break;
        emit({ kind: "text", text: ch });
        await delay(6);
      }
      emit({ kind: "message", text: reply });
      emit({
        kind: "tool_dispatch",
        tool: {
          id: "t1",
          name: "edit_file",
          args: '{"path":"main.go","old_string":"println(\\"hi\\")","new_string":"println(\\"hello\\")"}',
          readOnly: false,
        },
      });
      await delay(350);
      emit({
        kind: "tool_result",
        tool: { id: "t1", name: "edit_file", output: "edited main.go", readOnly: false, durationMs: 350 },
      });
      emit({
        kind: "usage",
        usage: {
          promptTokens: 1280,
          completionTokens: 64,
          totalTokens: 1344,
          cacheHitTokens: 0,
          cacheMissTokens: 0,
          sessionCacheHitTokens: 0,
          sessionCacheMissTokens: 0,
        },
      });
          emitMockTurnDone();
        },
        async SubmitToTab(_tabID, input) {
          await withMockTabScope(_tabID, () => this.Submit(input));
        },
        async SubmitDisplay(_display, input) {
          await this.Submit(input);
        },
        async SubmitDisplayToTab(_tabID, display, input) {
          await withMockTabScope(_tabID, () => this.SubmitDisplay(display, input));
        },
        async RunShell(command) {
          cancelled = false;
          emitMockTurnStarted();
          await delay(100);
          if (cancelled) return;
          const id = `shell-${command.slice(0, 32)}`;
          emit({ kind: "tool_dispatch", tool: { id, name: "bash", args: JSON.stringify({ command }), readOnly: false } });
          await delay(200);
          if (cancelled) return;
          emit({ kind: "tool_progress", tool: { id, name: "bash", output: `$ ${command}\n(mock output)\n`, readOnly: false } });
          await delay(100);
          if (cancelled) return;
          emit({ kind: "tool_result", tool: { id, name: "bash", output: `$ ${command}\n(mock output)\n`, readOnly: false, durationMs: 300 } });
          emitMockTurnDone();
        },
        async RunShellForTab(_tabID, command) {
          await withMockTabScope(_tabID, () => this.RunShell(command));
        },
        async Steer(_text) {
          // Mock: emit a steer event as confirmation in the transcript.
          emit({ kind: "steer", text: _text });
        },
        async SteerForTab(_tabID, _text) {
          await this.Steer(_text);
        },
        async Cancel() {
          cancelled = true;
          emitMockTurnDone();
        },
        async CancelTab(_tabID) {
          await withMockTabScope(_tabID, () => this.Cancel());
        },
        async Pause() {
          // Mock: surface a pause notice so the UI flow is testable without a backend.
          emit({ kind: "notice", level: "info", text: "（预览）已暂停" });
        },
        async PauseTab(_tabID) {
          await withMockTabScope(_tabID, () => this.Pause());
        },
        async ResumeTurn() {
          emit({ kind: "notice", level: "info", text: "（预览）已恢复" });
        },
        async ResumeTurnTab(_tabID) {
          await withMockTabScope(_tabID, () => this.ResumeTurn());
        },
        async PausedTab(_tabID) {
          return false;
        },
        async Approve(_id, allow, session, persist) {
          if (!pendingApprovalPreview) return;
          pendingApprovalPreview = false;
          const suffix = persist ? "grant saved" : session ? "grant active this session" : "allowed once";
          emit({
            kind: "message",
            text: `approval preview answered: ${allow ? suffix : "denied"}`,
          });
          emitMockTurnDone();
        },
        async ApproveTab(_tabID, id, allow, session, persist) {
          await withMockTabScope(_tabID, () => this.Approve(id, allow, session, persist));
        },
        async AnswerQuestion(_id, answers) {
      if (!pendingAskPreview) return;
      pendingAskPreview = false;
      const summary = answers
        .map((answer) => `${answer.questionId}: ${(answer.selected ?? []).join(", ") || "(no answer)"}`)
        .join("\n");
      emit({ kind: "message", text: `ask preview answered:\n\n${summary}` });
          emitMockTurnDone();
        },
        async AnswerQuestionForTab(_tabID, id, answers) {
          await withMockTabScope(_tabID, () => this.AnswerQuestion(id, answers));
        },
        async ReplayPendingPrompts() {},
        async ConfirmAction(req) {
          void req;
          return false;
        },
        // SetPlanMode is retained for binding parity but unused by the frontend
        // (use SetModeForTab / SetCollaborationModeForTab instead). Stub so the
        // mock satisfies the interface contract.
        async SetPlanMode() {},
        async SetMode(mode) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetModeForTab(active.id, mode);
        },
        async SetModeForTab(tabID, mode) {
          const nextMode = normalizeMode(mode);
          mockTabs = mockTabs.map((tab) =>
            tab.id === tabID
              ? {
                  ...tab,
                  mode: nextMode,
                  collaborationMode: normalizeCollaborationMode(undefined, tab.goal, nextMode),
                  toolApprovalMode: normalizeToolApprovalMode(undefined, nextMode),
                }
              : tab,
          );
        },
        async SetCollaborationMode(mode) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetCollaborationModeForTab(active.id, mode);
        },
        async SetCollaborationModeForTab(tabID, mode) {
          const next = normalizeCollaborationMode(mode);
          mockTabs = mockTabs.map((tab) => {
            if (tab.id !== tabID) return tab;
            const toolMode = normalizeToolApprovalMode(tab.toolApprovalMode, normalizeMode(tab.mode));
            return {
              ...tab,
              collaborationMode: next,
              goal: next === "normal" || next === "plan" ? "" : tab.goal,
              mode: modeWithPlan(modeWithAutoApproveTools(normalizeMode(tab.mode), toolMode === "yolo"), next === "plan"),
            };
          });
        },
        async SetToolApprovalMode(mode) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetToolApprovalModeForTab(active.id, mode);
        },
        async SetToolApprovalModeForTab(tabID, mode) {
          const next = normalizeToolApprovalMode(mode);
          settings.autoApproveTools = next === "yolo";
          settings.bypass = next === "yolo";
          mockTabs = mockTabs.map((tab) =>
            tab.id === tabID
              ? {
                  ...tab,
                  toolApprovalMode: next,
                  mode: modeWithAutoApproveTools(normalizeMode(tab.mode), next === "yolo"),
                }
              : tab,
          );
        },
        async SetRagScope(scope) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetRagScopeForTab(active.id, scope);
        },
        async SetRagScopeForTab(tabID, scope) {
          mockTabs = mockTabs.map((tab) =>
            tab.id === tabID ? { ...tab, ragScope: scope.trim() } : tab,
          );
        },
        async SetGoal(goal) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetGoalForTab(active.id, goal);
        },
        async SetGoalForTab(tabID, goal) {
          const nextGoal = goal.trim();
          mockTabs = mockTabs.map((tab) =>
            tab.id === tabID
              ? {
                  ...tab,
                  goal: nextGoal,
                  goalStatus: nextGoal ? "running" : "stopped",
                  collaborationMode: nextGoal ? "goal" : "normal",
                  mode: modeWithPlan(normalizeMode(tab.mode), false),
                }
              : tab,
          );
        },
        async ClearGoal() {
          await this.SetGoal("");
        },
        async ClearGoalForTab(tabID) {
          await this.SetGoalForTab(tabID, "");
        },
        async Compact() {},
        async NewSession() {},
        async ClearSession() {},
    async Checkpoints() {
      return [
        { turn: 0, prompt: "你好呀", files: ["src/App.tsx"], time: Date.now() - 30_000, canCode: true, canConversation: true },
      ];
    },
    async CheckpointsForTab() {
      return this.Checkpoints();
    },
    async Rewind() {},
    async Fork() {
      const active = mockTabs.find((tab) => tab.active) ?? mockTabs[0];
      const tab: TabMeta = {
        ...active,
        id: "tab_fork_" + Date.now(),
        topicId: "topic_fork_" + Date.now(),
        topicTitle: `${active.topicTitle || t("rewind.fork")} · fork`,
        active: true,
        running: false,
      };
      mockTabs = [...mockTabs.map((item) => ({ ...item, active: false })), tab];
      return { ...tab };
    },
    async SummarizeFrom() {},
    async SummarizeUpTo() {},
        async History() {
          return [];
        },
        async HistoryForTab(tabID?: string) {
          const tab = mockTabs.find((item) => item.id === tabID) ?? mockTabs.find((item) => item.active);
          if (tab?.topicId) {
            queueMockTopicRuntime(tab);
            return mockTopicHistory(tab.topicId);
          }
          return this.History();
        },
    async ListSessions() {
      return sessions.map((s) => ({ ...s }));
    },
    async ListTrashedSessions() {
      return trashedSessions.map((s) => ({ ...s }));
    },
    async ResumeSession(path: string) {
      sessions.forEach((s) => {
        s.current = s.path === path;
        s.open = s.open || s.path === path;
      });
      return [
        { role: "user", content: `(mock) resumed ${path}` },
        { role: "assistant", content: "This is a mock resumed transcript — the real one comes from the kernel." },
      ];
    },
    async ResumeSessionForTab(_tabID: string, path: string) {
      return this.ResumeSession(path);
    },
    async PreviewSession(path: string) {
      const s = sessions.find((x) => x.path === path) ?? trashedSessions.find((x) => x.path === path);
      return [
        { role: "user", content: s?.preview || `(mock) preview ${path}` },
        { role: "phase", content: "Preparing read-only preview" },
        {
          role: "assistant",
          content: "This is a read-only mock preview. The active conversation is unchanged.",
          reasoning: "Preview reads the saved session without resuming it.",
        },
        { role: "notice", level: "info", content: "Preview mode keeps the active conversation untouched." },
        { role: "compaction", content: "", trigger: "manual", messages: 3, summary: "Mock preview preserved the latest task, tool result, and answer summary." },
      ];
    },
    async DeleteSession(path: string) {
      const i = sessions.findIndex((s) => s.path === path);
      if (i >= 0) {
        const [s] = sessions.splice(i, 1);
        trashedSessions.unshift({
          ...s,
          current: false,
          open: false,
          path: s.path.replace("/mock/sessions/", "/mock/sessions/.trash/"),
          deletedAt: Date.now(),
        });
      }
    },
    async RestoreSession(path: string) {
      const i = trashedSessions.findIndex((s) => s.path === path);
      if (i >= 0) {
        const [s] = trashedSessions.splice(i, 1);
        sessions.unshift({
          ...s,
          path: s.path.replace("/mock/sessions/.trash/", "/mock/sessions/"),
          deletedAt: undefined,
        });
      }
    },
    async PurgeTrashedSession(path: string) {
      const i = trashedSessions.findIndex((s) => s.path === path);
      if (i >= 0) trashedSessions.splice(i, 1);
    },
    async RenameSession(path: string, title: string) {
      const s = sessions.find((x) => x.path === path);
      if (s) s.title = title.trim() || undefined;
    },
    async ListWorkspaces() {
      return mockProjectTree
        .filter((node) => node.kind === "project" && node.root)
        .map((node) => ({
          path: node.root!,
          name: node.label || baseName(node.root!),
          current: node.root === cwd,
        }));
    },
    async PickWorkspace() {
      // Browser dev has no native dialog; simulate picking a folder and re-root so
      // the topbar folder chip visibly changes.
      return mockSwitchWorkspace(cwd.endsWith("another-project") ? "~/projects/momapeer" : "~/projects/another-project");
    },
    async PickImportFolder() {
      return "~/Documents/my-import-folder";
    },
    async PickImportFiles() {
      return ["~/Documents/sample-report.pdf", "~/Documents/summary-data.xlsx"];
    },
    async SwitchWorkspace(path: string) {
      return mockSwitchWorkspace(path);
    },
    async RemoveWorkspace(path: string) {
      workspaces = workspaces.filter((p) => p !== path);
      const index = mockProjectTree.findIndex((node) => node.root === path);
      if (index >= 0) mockProjectTree.splice(index, 1);
    },
        async ContextUsage() {
          return { used: 42124, window: 128000, sessionTokens: 34479, compactRatio: 0.8 };
        },
        async ContextUsageForTab() {
          return this.ContextUsage();
        },
        async Jobs() {
          return []; // browser dev mock has no background jobs
        },
        async JobsForTab() {
          return this.Jobs();
        },
        async Meta() {
          const active = mockTabs.find((tab) => tab.active) ?? mockTabs[0];
          const toolApprovalMode = normalizeToolApprovalMode(active?.toolApprovalMode, active ? normalizeMode(active.mode) : "normal", settings.autoApproveTools);
          const autoApproveTools = toolApprovalMode === "yolo";
          return {
            label: active?.label ?? "Qwen3.6-35B",
            ready: active?.ready ?? true,
            eventChannel: EVENT_CHANNEL,
            cwd: active?.cwd || cwd,
            autoApproveTools,
            bypass: autoApproveTools,
            toolApprovalMode,
            goal: active?.goal ?? "",
            goalStatus: active?.goalStatus ?? (active?.goal ? "running" : "stopped"),
          };
        },
        async MetaForTab(tabID) {
          const tab = mockTabs.find((item) => item.id === tabID) ?? mockTabs.find((item) => item.active) ?? mockTabs[0];
          const toolApprovalMode = normalizeToolApprovalMode(tab?.toolApprovalMode, tab ? normalizeMode(tab.mode) : "normal", settings.autoApproveTools);
          const autoApproveTools = toolApprovalMode === "yolo";
          return {
            label: tab?.label ?? "Qwen3.6-35B",
            ready: tab?.ready ?? true,
            eventChannel: EVENT_CHANNEL,
            cwd: tab?.cwd || cwd,
            autoApproveTools,
            bypass: autoApproveTools,
            toolApprovalMode,
            goal: tab?.goal ?? "",
            goalStatus: tab?.goalStatus ?? (tab?.goal ? "running" : "stopped"),
          };
        },
    async Commands() {
      return [
        { name: "new", description: "start new session; save transcript", kind: "builtin" as const },
        { name: "clear", description: "discard current context", kind: "builtin" as const },
        { name: "compact", description: "Summarize older history to free up context", kind: "builtin" as const },
        { name: "model", description: "Switch model", kind: "builtin" as const },
        { name: "effort", description: "Set reasoning effort", kind: "builtin" as const },
        { name: "skill", description: "List skills", kind: "builtin" as const },
        { name: "explore", description: "Investigate the codebase in an isolated subagent", kind: "skill" as const },
        { name: "review", description: "Review the staged diff", hint: "[focus]", kind: "custom" as const },
      ];
    },
    async Capabilities() {
      return {
        servers: capServers.map((s) => ({ ...s })),
        skills: capSkills.map((s) => ({ ...s })),
        skillRoots: capSkillRoots.map((s) => ({ ...s })),
      };
    },
    async AddMCPServer(input: MCPServerInput) {
      const tools = input.transport === "stdio" ? 3 : 5;
      capServers.push({
        name: input.name,
        transport: input.transport,
        status: "connected",
        configured: true,
        autoStart: true,
        tier: "background",
        command: input.command,
        args: input.args,
        url: input.url,
        tools,
        prompts: 0,
        resources: 0,
        toolList: Array.from({ length: tools }, (_, i) => ({
          name: `${input.name}_tool_${i + 1}`,
          description: `Mock tool ${i + 1} exposed by ${input.name}.`,
        })),
      });
      return tools;
    },
    async UpdateMCPServer(name: string, input: MCPServerInput) {
      capServers = capServers.map((s) => {
        if (s.name !== name) return s;
        const connected = s.status === "connected" || s.status === "failed" || s.tier !== "lazy";
        const nextStatus = s.status === "disabled" ? "disabled" : connected ? "connected" : "deferred";
        const nextTools = nextStatus === "connected" ? s.tools || (input.transport === "stdio" ? 3 : 5) : 0;
        return {
          ...s,
          transport: input.transport,
          status: nextStatus,
          command: input.transport === "stdio" ? input.command : "",
          args: input.transport === "stdio" ? input.args : [],
          url: input.transport === "stdio" ? "" : input.url,
          envKeys: input.env ? Object.keys(input.env).sort() : s.envKeys,
          tools: nextTools,
          error: undefined,
          authStatus: nextStatus !== "connected" && input.transport !== "stdio" ? "possible" : undefined,
          authUrl: nextStatus !== "connected" && input.transport !== "stdio" ? input.url : undefined,
        };
      });
    },
    async RemoveMCPServer(name: string) {
      capServers = capServers.filter((s) => s.name !== name);
    },
    async ReconnectMCPServer(name: string) {
      capServers = capServers.map((s) =>
        s.name === name
          ? { ...s, status: "initializing", error: undefined, authStatus: undefined, authUrl: undefined }
          : s,
      );
      await new Promise((r) => setTimeout(r, 400));
      capServers = capServers.map((s) =>
        s.name === name ? { ...s, status: "connected", tools: s.tools || 4 } : s,
      );
    },
    async ClearMCPServerAuthentication(name: string) {
      capServers = capServers.map((s) =>
        s.name === name
          ? {
              ...s,
              status: s.tier === "background" || s.tier === "eager" ? "initializing" : "deferred",
              tools: 0,
              error: undefined,
              authStatus: s.transport !== "stdio" ? "possible" : undefined,
              authUrl: s.transport !== "stdio" ? s.url : undefined,
              authConfigured: undefined,
            }
          : s,
      );
    },
    async PickSkillFolder() {
      return "~/my-skills";
    },
    async PickDirectory(_title: string) {
      return "~/selected-folder";
    },
    async AddSkillPath(path: string) {
      const dir = path.trim() || "~/my-skills";
      if (!capSkillRoots.some((r) => r.scope === "custom" && r.dir === dir)) {
        capSkillRoots.push({
          dir,
          scope: "custom",
          priority: capSkillRoots.length + 1,
          status: "ok",
          configured: true,
          removable: true,
          skills: 1,
          skillItems: [{ name: "local-dev", description: "Local custom development workflow", scope: "custom", runAs: "inline" }],
        });
      }
      if (!capSkills.some((s) => s.name === "local-dev")) {
        capSkills.push({ name: "local-dev", description: "Local custom development workflow", scope: "custom", runAs: "inline", enabled: true });
      }
    },
    async RemoveSkillPath(path: string) {
      capSkillRoots = capSkillRoots.filter((r) => r.dir !== path);
      if (!capSkillRoots.some((r) => r.scope === "custom")) {
        const idx = capSkills.findIndex((s) => s.name === "local-dev");
        if (idx >= 0) capSkills.splice(idx, 1);
      }
    },
    async RefreshSkills() {},
    async SetSkillEnabled(name: string, enabled: boolean) {
      const skill = capSkills.find((s) => s.name === name);
      if (skill) skill.enabled = enabled;
    },
    async SetJiutianTool(name: string, enabled: boolean) {
      const jiutian = settings.jiutian ?? { imageUnderstand: true, imageGenerate: true, videoUnderstand: false };
      if (name === "image_understand") jiutian.imageUnderstand = enabled;
      if (name === "image_generate") jiutian.imageGenerate = enabled;
      if (name === "video_understand") jiutian.videoUnderstand = enabled;
      settings.jiutian = jiutian;
    },
    async DreamStatus(): Promise<DreamStatusView> {
      return {
        enabled: dreamMock.enabled,
        dreamInterval: dreamMock.dreamInterval,
        distillInterval: dreamMock.distillInterval,
        dreamInFlight: dreamMock.dreamInFlight,
        distillInFlight: dreamMock.distillInFlight,
        lastDream: dreamMock.lastDream,
        lastDistill: dreamMock.lastDistill,
        history: dreamMock.history,
      };
    },
    async SetDreamEnabled(enabled: boolean) {
      dreamMock.enabled = enabled;
    },
    async SetDreamIntervals(dreamDays: number, distillDays: number) {
      dreamMock.dreamInterval = dreamDays;
      dreamMock.distillInterval = distillDays;
    },
    async TriggerDream(): Promise<DreamRunView> {
      const run: DreamRunView = {
        kind: "dream",
        trigger: "manual",
        startedAt: new Date().toISOString(),
        duration: "2s",
        status: "ok",
      };
      dreamMock.lastDream = run;
      dreamMock.history = [run, ...dreamMock.history].slice(0, 20);
      return run;
    },
    async TriggerDistill(): Promise<DreamRunView> {
      const run: DreamRunView = {
        kind: "distill",
        trigger: "manual",
        startedAt: new Date().toISOString(),
        duration: "3s",
        status: "ok",
      };
      dreamMock.lastDistill = run;
      dreamMock.history = [run, ...dreamMock.history].slice(0, 20);
      return run;
    },
    async SetMCPServerEnabled(name: string, enabled: boolean) {
      capServers = capServers.map((s) =>
        s.name === name
          ? {
              ...s,
              status: enabled ? "connected" : "disabled",
              autoStart: s.builtIn ? enabled : s.autoStart,
              tools: enabled ? s.tools || 4 : 0,
              error: undefined,
              authStatus: !enabled && s.transport !== "stdio" ? "possible" : undefined,
              authUrl: !enabled && s.transport !== "stdio" ? s.url : undefined,
            }
          : s,
      );
    },
    async SetMCPServerTier(name: string, tier: string) {
      capServers = capServers.map((s) => {
        if (s.name !== name) return s;
        if (tier === "lazy") return { ...s, tier, autoStart: true };
        const tools = s.tools || (s.transport === "stdio" ? 3 : 5);
        return { ...s, tier, autoStart: true, status: "connected", tools, error: undefined, authStatus: undefined, authUrl: undefined };
      });
    },
    async SlashArgs(input: string) {
      // Mirror a slice of the real arg hints so the menu is exercisable in browser dev.
      const from = input.lastIndexOf(" ") + 1;
      const cur = input.slice(from);
      const cmd = input.slice(0, input.indexOf(" ") < 0 ? input.length : input.indexOf(" "));
      const subs: Record<string, { label: string; insert: string; hint: string; descend?: boolean }[]> = {
        "/skill": [
          { label: "list", insert: "list", hint: "list skills" },
          { label: "show", insert: "show ", hint: "show a skill's body", descend: true },
          { label: "enable", insert: "enable ", hint: "enable a disabled skill", descend: true },
          { label: "disable", insert: "disable ", hint: "disable an enabled skill", descend: true },
          { label: "new", insert: "new ", hint: "scaffold a new skill" },
          { label: "paths", insert: "paths", hint: "show discovery paths" },
        ],
        "/hooks": [
          { label: "list", insert: "list", hint: "list active hooks" },
          { label: "trust", insert: "trust", hint: "trust this project's hooks" },
        ],
        "/model": [
          { label: "qwen/qwen3.6-35b", insert: "qwen/qwen3.6-35b", hint: "current" },
          { label: "minimax/minimax-m2.7", insert: "minimax/minimax-m2.7", hint: "" },
          { label: "moonshotai/kimi-k2.6", insert: "moonshotai/kimi-k2.6", hint: "" },
        ],
        "/effort": [
          { label: "auto", insert: "auto", hint: "use the model default" },
          { label: "high", insert: "high", hint: "deeper reasoning" },
          { label: "max", insert: "max", hint: "maximum reasoning" },
        ],
      };
      const items = (subs[cmd] ?? [])
        .filter((it) => it.label.toLowerCase().startsWith(cur.toLowerCase()))
        .map((it) => ({ label: it.label, insert: it.insert, hint: it.hint, descend: it.descend ?? false }));
      return { items, from };
    },
    async ListDir(rel: string) {
      // A tiny fake tree so the @ menu is navigable in browser dev.
      if (rel === "" || rel === "./") {
        return [
          { name: "internal", isDir: true },
          { name: "desktop", isDir: true },
          { name: "README.md", isDir: false },
          { name: "go.mod", isDir: false },
        ];
      }
      if (rel === "internal/") {
        return [
          { name: "control", isDir: true },
          { name: "boot", isDir: true },
          { name: "event.go", isDir: false },
        ];
      }
      return [{ name: "file.go", isDir: false }];
    },
    async SearchFileRefs(query: string) {
      const q = query.toLowerCase();
      return ["desktop/frontend/src/lib/bridge.ts", "frontend/wailsjs/runtime/runtime.js", "internal/control/refs.go"]
        .filter((path) => path.split("/").pop()?.toLowerCase().includes(q))
        .map((name) => ({ name, isDir: false }));
    },
    async ReadFile(rel: string) {
      const samples: Record<string, string> = {
        "README.md": "# momapeer\n\nBrowser-dev workspace preview.\n\n- Chat in the center\n- Browse files on the right\n- Keep sessions on the left\n",
        "go.mod": "module momapeer\n\ngo 1.23\n",
        "desktop/file.go": "package desktop\n\nfunc main() {\n\tprintln(\"workspace preview\")\n}\n",
        "internal/event.go": "package internal\n\n// mock file used by the browser dev seam\n",
      };
      return {
        path: rel,
        body: samples[rel] ?? `// ${rel}\n\nMock file body from browser dev.`,
        size: samples[rel]?.length ?? 42,
        truncated: false,
        binary: false,
      };
    },
    async WorkspaceChanges() {
      return {
        gitAvailable: true,
        gitBranch: "main",
        files: [
          {
            path: "desktop/frontend/src/components/WorkspacePanel.tsx",
            sources: ["session", "git"],
            gitStatus: "M",
            turns: [0, 2],
            latestPrompt: "Mock session edited the workspace panel.",
            latestTime: Date.now() - 60_000,
          },
          { path: "README.md", sources: ["git"], gitStatus: "??" },
          { path: "internal/control/controller.go", sources: ["session"], turns: [1], latestTime: Date.now() - 120_000 },
        ],
      };
    },
    async GitBranches() {
      return ["main", "dev", "feature/branch-switcher"];
    },
    async GitCheckout(_branch: string) {
      console.info("mock GitCheckout", _branch);
    },
    async WorkspaceGitHistory(path: string) {
      return [
        { hash: "abcdef123456", author: "Mock Author", date: new Date().toISOString(), message: "Mock commit message for " + path },
      ];
    },
    async WorkspaceGitCommitDetail(_hash: string, path: string) {
      if (path) {
        return { diff: "--- a/mock\n+++ b/mock\n@@ -1,1 +1,1 @@\n-mock\n+mock diff" };
      }
      return { files: ["mock_file_1.ts", "mock_file_2.ts"] };
    },
    async OpenWorkspacePath(rel: string) {
      console.info("mock OpenWorkspacePath", rel);
    },
    async RevealWorkspacePath(rel: string) {
      console.info("mock RevealWorkspacePath", rel);
    },
    async RevealPath(path: string) {
      console.info("mock RevealPath", path);
    },
    async SavePastedImage(_dataUrl: string) {
      return ".momapeer/attachments/mock.png";
    },
    async SaveClipboardImage() {
      return ".momapeer/attachments/mock-clipboard.png";
    },
    async SavePastedFile(name: string, _dataUrl: string) {
      return `.momapeer/attachments/mock-${name}`;
    },
    async PickExportFile(defaultFilename: string, _mimeType: string) {
      return defaultFilename;
    },
    async SaveExportFile(path: string, payload: string, base64Encoded: boolean) {
      const a = document.createElement("a");
      let url = "";
      if (base64Encoded) {
        url = `data:application/octet-stream;base64,${payload}`;
      } else {
        url = URL.createObjectURL(new Blob([payload], { type: "text/plain;charset=utf-8" }));
      }
      a.href = url;
      a.download = path;
      document.body.appendChild(a);
      a.click();
      a.remove();
      if (!base64Encoded) URL.revokeObjectURL(url);
    },
    async AttachDropped(path: string) {
      const name = path.split(/[/\\]/).filter(Boolean).pop() ?? path;
      return { kind: "attachment" as const, path: `.momapeer/attachments/mock-${name}` };
    },
    async AttachmentDataURL(_path: string) {
      return "data:image/png;base64,iVBORw0KGgo=";
    },
        async Models() {
          const active = mockTabs.find((tab) => tab.active) ?? mockTabs[0];
          const current = mockTabModelRef(active);
          return mockModelCatalog.map((model) => ({ ...model, current: model.ref === current }));
        },
        async ModelsForTab(tabID) {
          const tab = mockTabs.find((item) => item.id === tabID) ?? mockTabs.find((item) => item.active) ?? mockTabs[0];
          const current = mockTabModelRef(tab);
          return mockModelCatalog.map((model) => ({ ...model, current: model.ref === current }));
        },
        async SetModel(name) {
          setMockTabModel(undefined, name);
        },
        async SetModelForTab(tabID, name) {
          setMockTabModel(tabID, name);
        },
        async Profile() {
          const active = mockTabs.find((tab) => tab.active) ?? mockTabs[0];
          return mockProfileOf(active);
        },
        async ProfileForTab(tabID) {
          const tab = mockTabs.find((item) => item.id === tabID) ?? mockTabs.find((item) => item.active) ?? mockTabs[0];
          return mockProfileOf(tab);
        },
        async Profiles() {
          return [
            { name: "dev", displayName: "编码", workspaceType: "code" },
            { name: "cowork", displayName: "办公", workspaceType: "document" },
          ];
        },
        async SwitchProfile(name) {
          setMockTabProfile(undefined, name);
        },
        async SwitchProfileForTab(tabID, name) {
          setMockTabProfile(tabID, name);
        },
        async Effort() {
          return { supported: true, current: mockEffort, default: "high", levels: ["auto", "high", "max"] };
        },
        async EffortForTab() {
          return this.Effort();
        },
        async SetEffort(level: string) {
          mockEffort = level || "auto";
        },
        async SetEffortForTab(_tabID, level) {
          await this.SetEffort(level);
        },
    async Memory() {
      return {
        available: true,
        storeDir: "~/.config/momapeer/projects/-mock/memory",
        docs: [
          {
            path: "momapeer.md",
            scope: "project",
            body: "# momapeer project memory\n\nMock doc shown in the browser dev seam.\n\n## Notes\n\n- prefers concise replies",
          },
          {
            path: "~/.config/momapeer/momapeer.md",
            scope: "user",
            body: t("mock.memoryBody"),
          },
        ],
        facts: [
          {
            name: "prefers-tabs",
            description: "User prefers tabs",
            type: "user",
            body: "Indent with tabs.",
            status: "active",
            category: "style",
            tags: ["indent", "code-style"],
            validFrom: "2026-01-15",
            createdAt: "2026-01-15T09:30:00Z",
            updatedAt: "2026-01-15T09:30:00Z",
          },
          {
            name: "lives-in-shanghai",
            description: "User currently lives in Shanghai (moved from Beijing)",
            type: "user",
            body: "User moved to Shanghai in May 2026. Previously lived in Beijing.",
            status: "active",
            category: "temporal",
            tags: ["location"],
            validFrom: "2026-05-01",
            supersededBy: "",
            createdAt: "2026-05-01T08:00:00Z",
            updatedAt: "2026-05-01T08:00:00Z",
          },
        ],
        scopes: [
          { scope: "user", path: "~/.config/momapeer/momapeer.md" },
          { scope: "project", path: "momapeer.md" },
          { scope: "local", path: "momapeer.local.md" },
        ],
      };
    },
    async MemoryHistory() {
      // Dev-seam mock: shows the full version chain including a superseded
      // record (the Beijing address that the Shanghai move replaced).
      return {
        available: true,
        storeDir: "~/.config/momapeer/projects/-mock/memory",
        docs: [],
        facts: [
          {
            name: "lives-in-shanghai",
            description: "User currently lives in Shanghai (moved from Beijing)",
            type: "user",
            body: "User moved to Shanghai in May 2026. Previously lived in Beijing.",
            status: "active",
            category: "temporal",
            validFrom: "2026-05-01",
            createdAt: "2026-05-01T08:00:00Z",
          },
          {
            name: "lives-in-beijing",
            description: "User lived in Beijing",
            type: "user",
            body: "User lives in Beijing.",
            status: "superseded",
            category: "temporal",
            validFrom: "2026-03-01",
            validTo: "2026-04-30",
            supersededBy: "lives-in-shanghai",
            createdAt: "2026-03-01T10:00:00Z",
          },
          {
            name: "prefers-tabs",
            description: "User prefers tabs",
            type: "user",
            body: "Indent with tabs.",
            status: "active",
            category: "style",
            validFrom: "2026-01-15",
            createdAt: "2026-01-15T09:30:00Z",
          },
        ],
        scopes: [],
      };
    },
    async Remember(scope: string, note: string) {
      emit({ kind: "notice", level: "info", text: `remembered → ${scope}` });
      return `${scope} momapeer.md (mock): ${note}`;
    },
    async Forget(name: string) {
      emit({ kind: "notice", level: "info", text: `forgot → ${name}` });
    },
    async PromoteMemory(name: string) {
      emit({ kind: "notice", level: "info", text: `promoted → ${name}` });
      return true;
    },
    async RejectMemory(name: string) {
      emit({ kind: "notice", level: "info", text: `rejected → ${name}` });
      return true;
    },
    async SaveDoc(path: string, _body: string) {
      emit({ kind: "notice", level: "info", text: `saved → ${path}` });
      return path;
    },
    async PortraitProfile() {
      return { path: "", content: "" };
    },
    async Settings() {
      return JSON.parse(JSON.stringify(settings)) as SettingsView;
    },
    async SetDefaultModel(ref: string) {
      settings.defaultModel = ref;
    },
    async GetJiutianBaseDomain(): Promise<string> {
      return "https://jiutian.10086.cn";
    },
    async SetJiutianBaseDomain(_domain: string) {
      // mock: no-op
    },
    async SetFastTaskModel(ref: string) {
      settings.fastTaskModel = ref;
    },
    async SetSubagentModel(ref: string) {
      settings.subagentModel = ref;
    },
    async SetSubagentEffort(level: string) {
      settings.subagentEffort = level;
    },
    async SetAutoPlan(mode: string) {
      settings.autoPlan = mode;
    },
    async SaveProvider(p: ProviderView) {
      p.added = true;
      const i = settings.providers.findIndex((x) => x.name === p.name);
      if (i >= 0) settings.providers[i] = p;
      else settings.providers.push(p);
    },
    async AddOfficialProviderAccess(kind: string, key: string) {
      const templates: Record<string, ProviderView> = {
        moma: { name: "moma", builtIn: true, added: true, kind: "openai", baseUrl: "https://jiutian.10086.cn/largemodel/moma/api/v3", modelsUrl: "", models: ["jiutian/jiutian-lan-236b", "jiutian/jiutian-lan-35b", "jiutian/jiutian-lan-thinking", "jiutian/jiutian-da-35b", "qwen/qwen3.6-35b", "qwen/qwen3.6-27b", "qwen/qwen3.5-397b-a17b", "deepseek/deepseek-v4-flash", "z.ai/glm-5.1", "z.ai/glm-5.2", "minimax/minimax-m2.7", "minimax/minimax-m2.5", "moonshotai/kimi-k2.6", "moonshotai/kimi-k2.5-thinking"], default: "minimax/minimax-m2.7", apiKeyEnv: "JIUTIAN_API_KEY", keySet: !!key.trim(), contextWindow: 200_000, reasoningProtocol: "", supportedEfforts: [], defaultEffort: "" },
      };
      const next = templates[kind] ?? templates.moma;
      const i = settings.providers.findIndex((x) => x.name === next.name);
      if (i >= 0) settings.providers[i] = { ...settings.providers[i], ...next, keySet: next.keySet || settings.providers[i].keySet };
      else settings.providers.push(next);
    },
    async FetchProviderModels(p: ProviderView) {
      if (!p.baseUrl.trim()) throw new Error(t("settings.fetchModelsMissingBaseUrl"));
      if (!p.apiKeyEnv.trim()) throw new Error(t("settings.fetchModelsMissingKeyEnv"));
      await delay(350);
      if (p.baseUrl.includes("jiutian")) return ["jiutian/jiutian-lan-236b", "jiutian/jiutian-lan-35b", "jiutian/jiutian-lan-thinking", "jiutian/jiutian-da-35b"];
      return ["qwen/qwen3.6-35b"];
    },
    async DeleteProvider(name: string) {
      settings.providers = settings.providers.filter((p) => p.name !== name);
    },
    async RemoveProviderAccess(name: string) {
      const p = settings.providers.find((x) => x.name === name);
      if (p?.builtIn) p.added = false;
      else settings.providers = settings.providers.filter((x) => x.name !== name);
    },
    async SetProviderKey(apiKeyEnv: string, _value: string) {
      settings.providers.forEach((p) => {
        if (p.apiKeyEnv === apiKeyEnv) p.keySet = true;
      });
    },
    async ClearProviderKey(apiKeyEnv: string) {
      settings.providers.forEach((p) => {
        if (p.apiKeyEnv === apiKeyEnv) p.keySet = false;
      });
    },
    async SetPermissionMode(mode: string) {
      settings.permissions.mode = mode;
    },
    async AddPermissionRule(list: string, rule: string) {
      const k = list as "allow" | "ask" | "deny";
      if (settings.permissions[k] && !settings.permissions[k].includes(rule)) settings.permissions[k].push(rule);
    },
    async RemovePermissionRule(list: string, rule: string) {
      const k = list as "allow" | "ask" | "deny";
      settings.permissions[k] = settings.permissions[k].filter((r) => r !== rule);
    },
        async SetSandbox(bash: string, network: boolean, workspaceRoot: string, allowWrite: string[]) {
          settings.sandbox = { bash, network, workspaceRoot, allowWrite };
        },
        async SetNetwork(n: NetworkView) {
          settings.network = n;
        },
        async SetBotSettings(b: BotSettingsView) {
          settings.bot = JSON.parse(JSON.stringify(b)) as BotSettingsView;
        },
        async SetBotSecret(envName: string, _value: string) {
          const name = envName.trim();
          if (settings.bot.qq.appSecretEnv === name) settings.bot.qq.secretSet = true;
          if (settings.bot.feishu.appSecretEnv === name) settings.bot.feishu.secretSet = true;
          if (settings.bot.weixin.tokenEnv === name) settings.bot.weixin.tokenSet = true;
          settings.bot.connections = settings.bot.connections.map((connection) => ({
            ...connection,
            credential: connection.credential.appSecretEnv === name || connection.credential.tokenEnv === name
              ? { ...connection.credential, secretSet: true }
              : connection.credential,
          }));
        },
        async ClearBotSecret(envName: string) {
          const name = envName.trim();
          if (settings.bot.qq.appSecretEnv === name) settings.bot.qq.secretSet = false;
          if (settings.bot.feishu.appSecretEnv === name) settings.bot.feishu.secretSet = false;
          if (settings.bot.weixin.tokenEnv === name) settings.bot.weixin.tokenSet = false;
          settings.bot.connections = settings.bot.connections.map((connection) => ({
            ...connection,
            credential: connection.credential.appSecretEnv === name || connection.credential.tokenEnv === name
              ? { ...connection.credential, secretSet: false }
              : connection.credential,
          }));
        },
        async StartBotConnectionInstall(provider: string, domain: string) {
          const normalizedProvider = provider === "weixin" ? "weixin" : "feishu";
          const normalizedDomain = normalizedProvider === "weixin" ? "weixin" : domain === "lark" ? "lark" : "feishu";
          return {
            ok: true,
            provider: normalizedProvider,
            domain: normalizedDomain,
            installId: `mock-${normalizedProvider}-${normalizedDomain}`,
            url: "https://example.com/momapeer-bot-qr",
            deviceCode: "MOCKDEVICE",
            userCode: normalizedProvider === "weixin" ? "" : "MOCK-CODE",
            interval: 3,
            expireIn: 300,
            message: "",
          };
        },
        async PollBotConnectionInstall(installID: string) {
          const isWeixin = installID.includes("weixin");
          const domain = installID.includes("lark") ? "lark" : isWeixin ? "weixin" : "feishu";
          const provider = isWeixin ? "weixin" : "feishu";
          const connection = {
            id: `${provider}-${domain}`,
            provider,
            domain,
            label: domain === "lark" ? "Lark" : domain === "weixin" ? "微信" : "飞书",
            enabled: true,
            status: "connected",
            model: "",
            workspaceRoot: "",
            credential: {
              appId: provider === "feishu" ? "cli_mock" : "",
              appSecretEnv: provider === "feishu" ? (domain === "lark" ? "LARK_BOT_APP_SECRET" : "FEISHU_BOT_APP_SECRET") : "",
              accountId: provider === "weixin" ? "mock-account" : "",
              tokenEnv: provider === "weixin" ? "WEIXIN_BOT_TOKEN" : "",
              secretSet: true,
            },
            sessionMappings: [],
            lastError: "",
            createdAt: new Date().toISOString(),
            updatedAt: new Date().toISOString(),
          };
          settings.bot.connections = [...settings.bot.connections.filter((c) => c.id !== connection.id), connection];
          return { done: true, connection, status: "connected", message: "connected", error: "" };
        },
        async DiagnoseBotConnection(id: string) {
          const connection = settings.bot.connections.find((c) => c.id === id);
          return connection
            ? { id, label: connection.label, status: connection.enabled ? "ok" : "disabled", message: connection.enabled ? "连接配置已保存。" : "连接已保存但未启用。", messageId: "" }
            : { id, label: "", status: "missing", message: "未找到连接。", messageId: "" };
        },
        async TestBotConnection(id: string, target?: string) {
          const diag = await this.DiagnoseBotConnection(id);
          if (target?.trim()) return { ...diag, message: `Mock test sent to ${target.trim()}`, messageId: "mock-message-id" };
          return diag;
        },
        async ListRecentBotChats() {
          return [];
        },
        async BotDockStatus() {
          return { online: false, platforms: [], recentCount: 0 } as BotDockStatusView;
        },
        async SetCloseBehavior(mode: string) {
          settings.closeBehavior = mode === "quit" ? "quit" : "background";
        },
        async SetDisplayMode(mode: string) {
          settings.displayMode = mode;
        },
        async SetDesktopLanguage(lang: string) {
          settings.desktopLanguage = lang === "en" || lang === "zh" ? lang : "";
        },
        async SetDesktopAppearance(theme: string, style: string) {
          settings.desktopTheme = theme === "auto" || theme === "light" ? theme : "dark";
          settings.desktopThemeStyle = style;
        },
        async SetDesktopCheckUpdates(enabled: boolean) {
          settings.checkUpdates = enabled;
        },
        async SetDesktopTelemetry(enabled: boolean) {
          settings.telemetry = enabled;
        },
        async SetExpandThinking(on: boolean) {
          settings.expandThinking = on;
        },
        async MigrateDesktopPreferences(language: string, theme: string, style: string) {
          if (!settings.desktopLanguage) settings.desktopLanguage = language === "en" || language === "zh" ? language : "";
          if (!settings.desktopTheme && !settings.desktopThemeStyle) {
            settings.desktopTheme = theme === "auto" || theme === "light" ? theme : "dark";
            settings.desktopThemeStyle = style;
          }
        },
    async SetAgentParams(temperature: number, maxSteps: number, plannerMaxSteps: number, systemPrompt: string) {
      settings.agent = { temperature, maxSteps, plannerMaxSteps, systemPrompt, rpm: settings.agent?.rpm ?? 0 };
    },
    async SetRPM(rpm: number) {
      if (settings.agent) settings.agent.rpm = rpm;
    },
    async SetTrayLocale(_locale: "en" | "zh") {},
    async SetAutoApproveTools(on: boolean) {
      await this.SetToolApprovalMode(on ? "yolo" : "ask");
    },
    async SetBypass(on: boolean) {
      await this.SetAutoApproveTools(on);
    },
    async Version() {
      return "v1.0.0 (browser dev)";
    },
    async CheckUpdate() {
      // Keep the default browser preview focused on the primary product surface.
      // ApplyUpdate remains mocked for explicit updater-flow tests.
      return {
        available: false,
        current: "v1.0.0",
        latest: "v1.0.0",
        notes: "",
        canSelfUpdate: false,
        downloadUrl: "",
        assetSize: 0,
      };
    },
    async ApplyUpdate() {
      const total = 12_345_678;
      for (let r = 0; r <= total; r += 1_800_000) {
        emitUpdater({ phase: "downloading", received: Math.min(r, total), total });
        await delay(120);
      }
      emitUpdater({ phase: "verifying", received: total, total });
      await delay(500);
      emitUpdater({ phase: "applying", received: total, total });
      await delay(500);
      emitUpdater({ phase: "done", received: total, total });
      // The real shell relaunches here; the mock just stops.
    },
    async OpenDownloadPage() {
      if (typeof window !== "undefined") {
        window.open("https://github.com/zzycxz/momapeer/releases/latest", "_blank", "noopener");
      }
    },
    // Dev seam: drives the overlay flow in the browser until ConnectKey sets the
    // key. Matches ConnectKey on apiKeyEnv so the two stay in sync.
    async NeedsOnboarding() {
      return !settings.providers.find((p) => p.apiKeyEnv === "JIUTIAN_API_KEY")?.keySet;
    },
    async ConnectKey(apiKey: string) {
      if (!apiKey.trim()) throw new Error("key is required");
      settings.providers.forEach((p) => {
        if (p.apiKeyEnv === "JIUTIAN_API_KEY") p.keySet = true;
      });
      await delay(300);
    },
    async ReportCrash() {
      await delay(300);
    },
    // Tab management mocks.
    async ListTabs() {
      return mockTabs.map((tab) => ({ ...tab }));
    },
    async OpenProjectTab(workspaceRoot: string, _topicID: string, profile?: string) {
      const existing = mockTabs.find((tab) => tab.scope === "project" && tab.workspaceRoot === workspaceRoot && tab.topicId === _topicID);
      if (existing) {
        const active = { ...existing, active: true, running: mockTopicRunsInScenario(_topicID) };
        mockTabs = mockTabs.map((tab) => (tab.id === existing.id ? active : { ...tab, active: false }));
        return { ...active };
      }
      const tab: TabMeta = {
        id: "tab_" + Date.now(),
        scope: "project",
        workspaceRoot,
        workspaceName: workspaceRoot.split("/").filter(Boolean).pop() ?? workspaceRoot,
        topicId: _topicID,
        topicTitle: topicLabel(_topicID, t("mock.newSession")),
        projectColor: mockProjectTree.find((node) => node.root === workspaceRoot)?.projectColor,
        label: "qwen/qwen3.6-35b",
        ready: true,
        running: mockTopicRunsInScenario(_topicID),
        mode: "normal",
        collaborationMode: "normal",
        toolApprovalMode: "ask",
        active: true,
        cwd: workspaceRoot,
        profile: (profile || "dev").toLowerCase(),
      };
      mockTabs = [...mockTabs.map((item) => ({ ...item, active: false })), tab];
      return { ...tab };
    },
    async OpenProjectTab3(workspaceRoot: string, topicID: string, profile: string) {
      return this.OpenProjectTab(workspaceRoot, topicID, profile);
    },
    async OpenGlobalTab(_topicID: string, profile?: string) {
      const existing = mockTabs.find((tab) => tab.scope === "global" && tab.topicId === _topicID);
      if (existing) {
        setMockActiveTab(existing.id);
        return { ...existing, active: true };
      }
      const tab: TabMeta = {
        id: "tab_" + Date.now(),
        scope: "global",
        workspaceRoot: "",
        workspaceName: "Global",
        topicId: _topicID,
        topicTitle: topicLabel(_topicID, "Global"),
        label: "qwen/qwen3.6-35b",
        ready: true,
        running: false,
        mode: "normal",
        collaborationMode: "normal",
        toolApprovalMode: "ask",
        active: true,
        cwd: "",
        profile: (profile || "dev").toLowerCase(),
      };
      mockTabs = [...mockTabs.map((item) => ({ ...item, active: false })), tab];
      return { ...tab };
    },
    async EnsureBlankTab(scope: string, workspaceRoot: string, profile: string) {
      const targetScope = scope === "project" && workspaceRoot ? "project" : "global";
      const targetRoot = targetScope === "project" ? workspaceRoot : "";
      const targetProfile = (profile || "dev").toLowerCase();
      const existing = mockTabs.find((tab) =>
        tab.scope === targetScope &&
        (tab.profile ?? "dev").toLowerCase() === targetProfile &&
        (targetScope === "global" || tab.workspaceRoot === targetRoot) &&
        !tab.running
      );
      if (existing) {
        setMockActiveTab(existing.id);
        return { ...existing, active: true };
      }
      const topic = await this.CreateTopic(targetScope, targetRoot, targetProfile, "");
      return targetScope === "global" ? this.OpenGlobalTab(topic.id, targetProfile) : this.OpenProjectTab(targetRoot, topic.id, targetProfile);
    },
    async OpenExpertSessionTab(teamId: string, teamName: string) {
      const existing = mockTabs.find((t) => t.expertSession?.teamId === teamId);
      if (existing) { setMockActiveTab(existing.id); return { ...existing, active: true }; }
      const meta: TabMeta = {
        id: `expert_${Date.now()}`, tabType: "session", scope: "expert",
        workspaceRoot: "", workspaceName: "", topicId: "", topicTitle: teamName,
        label: "", ready: true, running: false, mode: "normal", active: true, cwd: "",
        profile: "cowork", expertSession: { teamId, teamName },
      };
      mockTabs.push(meta);
      setMockActiveTab(meta.id);
      return meta;
    },
    async SetActiveTab(_tabID: string) {
      setMockActiveTab(_tabID);
      const tab = mockTabs.find((item) => item.id === _tabID);
      if (tab) queueMockTopicRuntime(tab);
    },
    async ReorderTabs(_tabIDs: string[]) {
      const byId = new Map(mockTabs.map((tab) => [tab.id, tab]));
      const ordered = _tabIDs.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
      if (ordered.length === mockTabs.length) mockTabs = ordered;
    },
    async CloseTab(_tabID: string) {
      if (mockTabs.length <= 1) return;
      const wasActive = mockTabs.some((tab) => tab.id === _tabID && tab.active);
      mockTabs = mockTabs.filter((tab) => tab.id !== _tabID);
      if (wasActive && mockTabs.length > 0 && !mockTabs.some((tab) => tab.active)) {
        mockTabs[mockTabs.length - 1] = { ...mockTabs[mockTabs.length - 1], active: true };
      }
    },
    async ListProjectTree(_profile?: string) {
      return cloneProjectTree();
    },
    async RenameProject(workspaceRoot: string, title: string) {
      const node = workspaceRoot
        ? mockProjectTree.find((item) => item.root === workspaceRoot)
        : mockProjectTree.find((item) => item.kind === "global_folder");
      if (node) node.label = title.trim() || (node.kind === "global_folder" ? "Global" : node.label);
    },
    async SetProjectColor(workspaceRoot: string, color: string) {
      const node = workspaceRoot
        ? mockProjectTree.find((item) => item.root === workspaceRoot)
        : mockProjectTree.find((item) => item.kind === "global_folder");
      if (!node) return;
      node.projectColor = color || undefined;
      for (const child of projectChildren(node)) child.projectColor = node.projectColor;
      mockTabs = mockTabs.map((tab) =>
        (workspaceRoot ? tab.workspaceRoot === workspaceRoot : tab.scope === "global")
          ? { ...tab, projectColor: node.projectColor }
          : tab,
      );
    },
    async ReorderProjects(_profile: string, workspaceRoots: string[]) {
      const projects = mockProjectTree.filter((node) => node.kind === "project");
      const globals = mockProjectTree.filter((node) => node.kind === "global_folder");
      if (!workspaceRoots.includes(GLOBAL_PROJECT_ORDER_KEY)) {
        if (workspaceRoots.length !== projects.length) return;
        const byRoot = new Map(projects.map((node) => [node.root, node]));
        const ordered = workspaceRoots.map((root) => byRoot.get(root)).filter((node): node is ProjectNode => Boolean(node));
        if (ordered.length !== projects.length) return;
        mockProjectTree.splice(0, mockProjectTree.length, ...globals, ...ordered);
        return;
      }
      const byKey = new Map<string, ProjectNode>();
      for (const node of projects) {
        if (node.root) byKey.set(node.root, node);
      }
      for (const node of globals) byKey.set(GLOBAL_PROJECT_ORDER_KEY, node);
      const seen = new Set<string>();
      const ordered: ProjectNode[] = [];
      for (const key of workspaceRoots) {
        if (seen.has(key)) return;
        const node = byKey.get(key);
        if (!node) return;
        seen.add(key);
        ordered.push(node);
      }
      if (ordered.length !== projects.length + globals.length) return;
      mockProjectTree.splice(0, mockProjectTree.length, ...ordered);
    },
    async CreateTopic(_scope: string, _workspaceRoot: string, _profile: string, title: string) {
      const now = Date.now();
      const id = "topic_" + now;
      const topicTitle = title.trim() || t("mock.newSession");
      const parent = _scope === "global"
        ? ensureMockGlobalFolder()
        : mockProjectTree.find((node) => node.root === _workspaceRoot);
      if (parent) {
        const global = parent.kind === "global_folder";
        parent.children = [{
          key: parent.kind === "global_folder" ? "global_topic_" + id : "topic_" + id,
          kind: global ? "global_topic" : "topic",
          label: topicTitle,
          root: parent.root,
          topicId: id,
          projectColor: parent.projectColor,
          createdAt: now,
        }, ...projectChildren(parent)];
      }
      return { id, title: topicTitle, createdAt: now };
    },
    async RenameTopic(topicID: string, title: string) {
      const topic = findMockTopic(topicID);
      const nextTitle = title.trim();
      if (!topic || !nextTitle) return;
      const activePrefix = topic.label?.startsWith("● ") ? "● " : "";
      topic.label = `${activePrefix}${nextTitle}`;
      mockTabs = mockTabs.map((tab) =>
        tab.topicId === topicID ? { ...tab, topicTitle: nextTitle } : tab,
      );
    },
    async DeleteTopic(topicID: string) {
      deleteMockTopic(topicID);
    },
    async TrashTopic(topicID: string) {
      deleteMockTopic(topicID);
    },
    async TrashExpertSession(_teamID: string) {
      // no-op in mock — no real session files to trash
    },
    async SaveWindowState(_state) {
      // no-op in browser dev — no real window geometry to persist
    },
    // --- Scheduled tasks mock (browser dev only) -----------------------------
    // A small in-memory store seeded with one sample task so the automation
    // panel looks alive outside the Wails shell. The real backend persists to
    // JSON; here we just keep the array.
    async ListScheduledTasks(): Promise<TaskView[]> {
      return mockSchedulerTasks.map(cloneTask);
    },
    async CreateScheduledTask(input: TaskInput): Promise<TaskView> {
      const view: TaskView = {
        id: `sched_mock_${Date.now()}`,
        name: input.name || "未命名任务",
        expression: input.expression,
        prompt: input.prompt,
        profile: "cowork",
        enabled: true,
        oneShot: input.expression.toLowerCase().startsWith("at "),
        lastRun: "",
        nextRun: input.expression.toLowerCase().startsWith("at ") ? input.expression.slice(3) : "明天 09:00",
        runCount: 0,
        lastResult: "",
        outputMode: input.outputMode ?? "",
        outputDest: input.outputDest ?? "",
        outputAccount: input.outputAccount ?? "",
        outputDir: input.outputDir ?? "",
        plain: input.plain ?? false,
        lastDeliverErr: "",
        lastDeliverAt: "",
        humanSchedule: input.expression,
        source: "manual",
        calendarEventId: "",
      };
      mockSchedulerTasks.unshift(view);
      return cloneTask(view);
    },
    async UpdateScheduledTask(input: TaskInput): Promise<TaskView> {
      const idx = mockSchedulerTasks.findIndex((t) => t.id === input.id);
      if (idx < 0) throw new Error("task not found");
      mockSchedulerTasks[idx] = {
        ...mockSchedulerTasks[idx],
        name: input.name,
        expression: input.expression,
        prompt: input.prompt,
        outputMode: input.outputMode ?? "",
        outputDest: input.outputDest ?? "",
        outputAccount: input.outputAccount ?? "",
        outputDir: input.outputDir ?? "",
        plain: input.plain ?? false,
        lastDeliverErr: "",
        lastDeliverAt: "",
        humanSchedule: input.expression,
      };
      return cloneTask(mockSchedulerTasks[idx]);
    },
    async DeleteScheduledTask(id: string): Promise<void> {
      mockSchedulerTasks = mockSchedulerTasks.filter((t) => t.id !== id);
    },
    async PauseScheduledTask(id: string): Promise<void> {
      const t = mockSchedulerTasks.find((t) => t.id === id);
      if (t) { t.enabled = false; t.nextRun = ""; }
    },
    async ResumeScheduledTask(id: string): Promise<void> {
      const t = mockSchedulerTasks.find((t) => t.id === id);
      if (t) { t.enabled = true; t.nextRun = "稍后"; }
    },
    async RunScheduledTaskNow(id: string): Promise<string> {
      const t = mockSchedulerTasks.find((t) => t.id === id);
      if (!t) throw new Error("task not found");
      t.runCount++;
      t.lastRun = new Date().toISOString().slice(0, 16).replace("T", " ");
      t.lastResult = "（mock）已运行";
      return t.lastResult;
    },
    async ScheduledTaskHistory(_taskID: string): Promise<RunRecordView[]> {
      return [
        { taskId: _taskID || "demo", name: "日报提醒", at: "2026-06-21 18:00", status: "ok", result: "日报已生成并发送", outputMode: "notify" },
      ];
    },
    async ScheduledTaskTemplates(): Promise<TemplateView[]> {
      return [
        { id: "daily_report_reminder", name: "日报提醒", category: "reminder", desc: "每个工作日下班前提醒整理日报", expression: "daily 18:00 Mon-Fri", prompt: "请整理今日工作日报，按三段式汇总。", outputMode: "notify", outputHint: "", oneShot: false },
        { id: "weekly_report_reminder", name: "周报提醒", category: "reminder", desc: "每周五提醒提交周报到邮箱", expression: "daily 17:00 Fri", prompt: "生成本周工作周报。", outputMode: "email", outputHint: "填写收件人邮箱", oneShot: false },
        { id: "meeting_reminder", name: "会议提醒", category: "reminder", desc: "一次性会议开始前提醒", expression: "at 2026-06-24 14:45", prompt: "15分钟后有会议，请准备材料。", outputMode: "notify", outputHint: "", oneShot: true },
        { id: "data_scrape", name: "定时数据抓取", category: "data", desc: "每天早上抓取数据存为 CSV", expression: "daily 09:00", prompt: "抓取昨日关键业务数据并保存为 CSV。", outputMode: "file", outputHint: "填写保存路径", oneShot: false },
        { id: "system_check", name: "系统巡检", category: "ops", desc: "每小时检查系统状态，异常告警", expression: "every 1h", prompt: "检查磁盘/内存/进程，异常时告警。", outputMode: "im", outputHint: "填写飞书会话标识", oneShot: false },
      ];
    },
    async PreviewSchedule(text: string): Promise<SchedulePreview> {
      const low = (text || "").trim().toLowerCase();
      if (!low) return { inputText: text, expression: "", absoluteTime: "", kind: "unknown", note: "输入时间或计划" };
      if (/^(后天|明天|大后天|下周|今天|周|星期)/.test(text) || /点|：|:/.test(text)) {
        return { inputText: text, expression: "at 2026-06-24 15:00", absoluteTime: "2026-06-24 15:00", kind: "oneshot", note: "一次性任务（mock 预览）" };
      }
      if (low.startsWith("at ") || low.startsWith("in ") || low.startsWith("daily") || low.startsWith("every") || low === "hourly") {
        return { inputText: text, expression: text, absoluteTime: "", kind: "recurring", note: "下次：稍后（mock）" };
      }
      return { inputText: text, expression: "", absoluteTime: "", kind: "unknown", note: "无法识别（mock）" };
    },
    async SmartParseSchedule(text: string): Promise<SchedulePreview> {
      // Mock: pretend the model resolved it to a near-future time.
      const now = new Date();
      now.setDate(now.getDate() + 7);
      const ts = `${now.getFullYear()}-${String(now.getMonth()+1).padStart(2,"0")}-${String(now.getDate()).padStart(2,"0")} 15:00`;
      return { inputText: text, expression: "at " + ts, absoluteTime: ts, kind: "oneshot", note: "一次性任务（智能解析 mock）" };
    },
    // --- Calendar mock (browser dev only) ------------------------------------
    async ListCalendarEvents(_since: string, _before: string): Promise<CalendarEventView[]> {
      const now = new Date();
      const y = now.getFullYear();
      const m = now.getMonth();
      const d = now.getDate();
      return [
        { id: "evt_mock_1", title: "周会", description: "讨论本周进展", location: "会议室A", start: `${y}-${String(m+1).padStart(2,"0")}-${String(d).padStart(2,"0")}T10:00`, end: `${y}-${String(m+1).padStart(2,"0")}-${String(d).padStart(2,"0")}T11:00`, allDay: false, timezone: "Asia/Shanghai", color: "#FF4444", status: "confirmed", source: "manual", recurrence: "FREQ=WEEKLY;BYDAY=MO", recurrenceEnd: "", reminders: [15], taskId: "", tags: ["工作", "例会"], createdAt: "2026-07-01 10:00", outputMode: "", outputDest: "", outputAccount: "" },
        { id: "evt_mock_2", title: "代码review", description: "", location: "线上", start: `${y}-${String(m+1).padStart(2,"0")}-${String(d).padStart(2,"0")}T14:00`, end: `${y}-${String(m+1).padStart(2,"0")}-${String(d).padStart(2,"0")}T15:00`, allDay: false, timezone: "Asia/Shanghai", color: "#4488FF", status: "confirmed", source: "manual", recurrence: "", recurrenceEnd: "", reminders: [5], taskId: "", tags: ["工作"], createdAt: "2026-07-01 10:00", outputMode: "", outputDest: "", outputAccount: "" },
      ];
    },
    async ListScheduledTasksAsEvents(_since: string, _before: string): Promise<CalendarEventView[]> {
      return [];
    },
    async CreateCalendarEvent(input: CalendarEventInput): Promise<CalendarEventView> {
      return { ...input, outputMode: input.outputMode ?? "", outputDest: input.outputDest ?? "", outputAccount: input.outputAccount ?? "", id: `evt_mock_${Date.now()}`, status: "confirmed", source: "manual", taskId: "", createdAt: new Date().toISOString().slice(0,16).replace("T"," ") };
    },
    async UpdateCalendarEvent(input: CalendarEventInput): Promise<CalendarEventView> {
      return { ...input, outputMode: input.outputMode ?? "", outputDest: input.outputDest ?? "", outputAccount: input.outputAccount ?? "", status: "confirmed", source: "manual", taskId: "", createdAt: "2026-07-01 10:00" };
    },
    async DeleteCalendarEvent(_id: string): Promise<void> {},
    async SearchCalendarEvents(_q: string, _limit: number): Promise<CalendarEventView[]> {
      return [];
    },
    async ExportCalendarEvents(_path: string): Promise<string> {
      return "exported 0 events (mock)";
    },
    async ImportCalendarEvents(_path: string): Promise<string> {
      return "imported 0 events (mock)";
    },
    async ExportCalendarDialog(): Promise<string> {
      return "exported 0 events (mock)";
    },
    async ImportCalendarDialog(): Promise<string> {
      return "imported 0 events (mock)";
    },
    async GetChineseHolidays(_year: number): Promise<CalendarEventView[]> {
      return [];
    },
    // --- RAG mock (browser dev only) ------------------------------------------
    // In-memory tree seeded with one sample collection + a file mid-extraction
    // so the panel shows a progress bar outside the Wails shell.
    async ListRagCollections(): Promise<RagCollectionView[]> {
      return [
        { id: "default", name: "default", path: "default", parent: "", documents: mockRagDocs, chunks: mockRagDocs * 4, entities: mockRagEntities },
      ];
    },
    async ListRagTree(_collection: string): Promise<RagNodeView[]> {
      return mockRagTree;
    },
    async RagImportPaths(_collection: string, paths: string[]): Promise<RagImportResult> {
      const jobIds: string[] = [];
      let files = 0;
      for (const p of paths) {
        files++;
        const jid = `rag_job_mock_${Date.now()}_${files}`;
        jobIds.push(jid);
        const node: RagNodeView = {
          key: p, label: p.split(/[\\/]/).pop() || p, kind: "file", path: p, relPath: p,
          isDir: false, collection: _collection || "default", status: "extracting",
          hasFts5: true, jobId: jid, doneChunks: 0, totalChunks: 8, entityCount: 0, errorMsg: "",
        };
        mockRagTree.push(node);
        // Simulate progress for browser dev.
        simulateRagProgress(jid, node);
      }
      mockRagDocs += files;
      return { jobIds, files, ftsChunks: files * 4, message: `mock：已导入 ${files} 个文件` };
    },
    async RagStartExtract(_collection: string, _template: string, _mode: string): Promise<void> {
      const node = mockRagTree.find((n) => n.path === _template);
      if (node) { node.status = "extracting"; node.doneChunks = 0; node.totalChunks = node.totalChunks || 8; simulateRagProgress(node.jobId, node); }
    },
    async RagExtractResult(_collection: string): Promise<RagExtractResultView> {
      return {
        entityCount: 5,
        relationCount: 3,
        topEntities: [
          { name: "mock_entity", nameRaw: "Mock Entity", type: "concept", description: "A mock entity", relationCount: 2 },
        ],
        topRelations: [
          { source: "mock_entity", target: "mock_entity2", type: "related", description: "mock relation" },
        ],
        jobCount: 1,
        doneCount: 1,
        hasData: true,
      };
    },
    async RagCancelExtract(jobId: string): Promise<void> {
      for (const n of mockRagTree) { if (n.jobId === jobId) { n.status = "cancelled"; } }
    },
    async RagRemovePath(_collection: string, path: string): Promise<void> {
      mockRagTree = mockRagTree.filter((n) => n.path !== path);
    },
    async RagClear(_collection: string): Promise<void> {
      mockRagTree = [];
      mockRagDocs = 0;
      mockRagEntities = 0;
    },
    async RagCleanCollection(_collection: string): Promise<void> {
      // mock: no-op
    },
    async RagSearch(_collection: string, query: string, _topK: number): Promise<RagSearchHitView> {
      return {
        entities: [{ name: query + "（示例实体）", type: "person", description: "mock 命中" }],
        relations: [],
        snippets: [{ collection: "default", path: "/mock/doc.md", chunk: 0, snippet: `…包含「${query}」的片段…`, score: 0.9 }],
      };
    },
    async RagSemanticSearch(_collection: string, query: string, _topK: number): Promise<RagSearchHitView> {
      return {
        entities: [{ name: query + "（语义匹配）", type: "concept", description: "mock 语义命中" }],
        relations: [],
        snippets: [],
      };
    },
    async RagEmbedEntities(_collection: string): Promise<void> {
      // mock: no-op
    },
    async RagDetectCommunities(_collection: string): Promise<void> {
      // mock: no-op
    },
    async RagSummarize(_collection: string): Promise<{ summary: string; themes: string[] }> {
      return { summary: "这是一份示例摘要，展示了知识库的主要内容。", themes: ["示例主题1", "示例主题2"] };
    },
    async RagAsk(_collection: string, _question: string): Promise<string> {
      return "这是来自知识库的示例回答。";
    },
    async RagPreviewETA(jobId: string): Promise<RagETAView> {
      const n = mockRagTree.find((x) => x.jobId === jobId);
      const remaining = n ? Math.max(0, n.totalChunks - n.doneChunks) : 0;
      return { jobId, doneChunks: n?.doneChunks ?? 0, totalChunks: n?.totalChunks ?? 0, avgLatencyMs: 2800, etaSeconds: remaining * 3 };
    },
    async RagListTemplates(): Promise<string[]> {
      return [".txt", ".md", ".csv", ".json", ".html", ".py", ".go", ".js", ".ts", ".yaml"];
    },
    async HEHealth(): Promise<{ running: boolean; ready: boolean; port: number }> {
      return { running: false, ready: false, port: 0 };
    },
    async RagListHETemplates() {
      return [] as Array<{ name: string; displayName: string; description: string; category: string; available: boolean; templateType: string; entityFields: Array<{ name: string; description: string }>; relationFields: Array<{ name: string; description: string }> }>;
    },
    async GetGraphData(_collection: string): Promise<GraphDataView> {
      return { nodes: [], edges: [] };
    },
    async GetTopEntities(_collection: string, _limit: number): Promise<GraphDataView> {
      return { nodes: [], edges: [] };
    },
    async GetGraphDataPaged(_collection: string, _offset: number, _limit: number, _types: string[]): Promise<GraphDataView> {
      return { nodes: [], edges: [] };
    },
    async GetEntityDetail(_collection: string, _name: string): Promise<EntityDetailView> {
      return { name: "", nameRaw: "", type: "", description: "", sources: [], relations: [], community: -1, relationCnt: 0 };
    },
    async UpdateEntity(_collection: string, _name: string, _patch: EntityPatch): Promise<void> {},
    async MergeEntities(_collection: string, _keepName: string, _mergeNames: string[]): Promise<void> {},
    async RagFindMergeCandidates(_collection: string): Promise<Array<{ keepName: string; mergeName: string; keepRaw: string; mergeRaw: string; score: number }>> {
      return [];
    },
    async GetDocumentPreview(_collection: string, _docPath: string): Promise<DocPreviewView> {
      return { path: "", content: "", chunks: [] };
    },
    async WriteKnowledgeRef(_collection: string, _entityNames: string[], _relationKeys: string[]): Promise<string> {
      return "/tmp/mock_knowledge_ref.md";
    },
    async RunSkillWithKnowledge(_skillName: string, _refPath: string): Promise<void> {},
    async ExportObsidian(_collection: string, _outputDir: string): Promise<void> {},
    async SetSessionCollections(_collections: string[]): Promise<void> {},
    async GetSessionCollections(): Promise<string[]> { return []; },
    async RagFeedText(_collection: string, _label: string, _text: string): Promise<void> {},
    async RagBatchImport(_collection: string, _paths: string[]): Promise<RagImportResult> {
      return { jobIds: [], files: 0, ftsChunks: 0, message: "mock" };
    },
    async RagBatchExtract(_collection: string): Promise<void> {},
    // --- Expert team mock (browser dev only) -------------------------------
    async ListExpertTeams(): Promise<TeamView[]> {
      return [
        { id: "t1", name: "方案评审团", defaultMode: "debate", defaultRounds: 2, allowSearch: false, experts: [
          { name: "批判者", model: "", perspective: "从风险角度批判性审视" },
          { name: "建设者", model: "", perspective: "从改进落地角度给建议" },
        ]},
      ];
    },
    async CreateExpertTeam(team: TeamView): Promise<TeamView> {
      return { ...team, id: `team_mock_${Date.now()}` };
    },
    async UpdateExpertTeam(team: TeamView): Promise<TeamView> { return team; },
    async DeleteExpertTeam(_id: string): Promise<void> {},
    async RunExpertTeam(_teamId: string, _task: string, _mode: string, _rounds: number): Promise<string> {
      const runId = `run_mock_${Date.now()}`;
      // In browser dev there's no runtime.EventsOn, so the mock can't stream
      // CollabEvents. Real runs stream via onExpertsCollab; here we just return
      // a runId so the panel's "start" handler doesn't crash.
      return runId;
    },
    async GetActiveExpertRun(_teamId: string): Promise<ExpertRunView> {
      // No in-flight run in the browser dev shell.
      return {};
    },
    async DeleteExpertCollab(_tabId: string, _ordinal: number): Promise<HistoryMessage[]> {
      return [];
    },
    async StartScreenshotHotkey() {},
    async StopScreenshotHotkey() {},
    async StartEStopHotkey() {},
    async StopEStopHotkey() {},
    async SetCoWorkSettings(v: any) { settings.cowork = { ...v, detectedBrowser: settings.cowork.detectedBrowser }; },
    async ProbeMailAccount(_name: string) { return { ok: true, status: "unconfigured", message: "" } as MailProbeResult; },
    async InboxPreview(_mailbox: string, _limit: number) { return [] as InboxItem[]; },
    async HooksSettings(scope: string) {
      const key = scope === "project" ? "project" : "global";
      return JSON.parse(JSON.stringify(hookSettings[key])) as HooksSettingsView;
    },
    async SaveHooksSettings(scope: string, hooks: HookConfigView[]) {
      const key = scope === "project" ? "project" : "global";
      hookSettings[key].hooks = JSON.parse(JSON.stringify(hooks)) as HookConfigView[];
    },
    async SaveHooksSettingsForRoot(scope: string, _projectRoot: string, hooks: HookConfigView[]) {
      const key = scope === "project" ? "project" : "global";
      hookSettings[key].hooks = JSON.parse(JSON.stringify(hooks)) as HookConfigView[];
    },
    async TrustProjectHooks() {
      hookSettings.project.trusted = true;
    },
    async TrustProjectHooksForRoot(projectRoot: string) {
      if (projectRoot && projectRoot === hookSettings.project.projectRoot) {
        hookSettings.project.trusted = true;
      }
    },
    async CheckCoworkBrowser() { return "Chrome"; },
    async OpenPPTTemplateDir() {},
    async ContextPanel(_tabID: string) {
      const now = Date.now();
      return {
        usedTokens: 42124,
        windowTokens: 128000,
        promptTokens: 22134,
        completionTokens: 12345,
        totalTokens: 34479,
        reasoningTokens: 7521,
        cacheHitTokens: 0,
        cacheMissTokens: 0,
        requestCount: 6,
        elapsedMs: 33 * 60 * 1000,
        mock: true,
        readFiles: [
          { path: "README.md", turn: 2, time: now - 34 * 60 * 1000 },
          { path: "go.mod", turn: 3, time: now - 30 * 60 * 1000 },
          { path: "desktop/file.go", turn: 5, time: now - 13 * 60 * 1000, offset: 0, limit: 180 },
          { path: "internal/event.go", turn: 6, time: now - 4 * 60 * 1000, offset: 120, limit: 80, truncated: true },
        ],
        changedFiles: [
          { path: t("mock.changedFile1Path"), sources: ["session"], gitStatus: "modified", turns: [5, 6], latestPrompt: t("mock.changedFile1Prompt"), latestTime: now - 2 * 60 * 1000 },
          { path: t("mock.changedFile2Path"), sources: ["session"], gitStatus: "added", turns: [6], latestPrompt: t("mock.changedFile2Prompt"), latestTime: now - 60 * 1000 },
        ],
      };
    },
    async RagCreateCollection(_name: string) {},
    async RagDeleteCollection(_name: string) {},
    async RagRenameCollection(_oldName: string, _newName: string) {},
    async SetDesktopMetrics(_enabled: boolean) {},
    async SetPlannerModel(_model: string) {},
  };
}
