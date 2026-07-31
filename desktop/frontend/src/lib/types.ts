// Wire contract — mirrors desktop/wire.go (itself mirroring internal/serve/wire.go).
// One event channel carries every kind; `kind` discriminates the payload.

export type EventKind =
  | "turn_started"
  | "reasoning"
  | "text"
  | "message"
  | "tool_dispatch"
  | "tool_result"
  | "tool_progress"
  | "usage"
  | "notice"
  | "phase"
  | "approval_request"
  | "ask_request"
  | "turn_done"
  | "compaction_started"
  | "compaction_done"
  | "retrying"
  | "steer"
  | "paused"
  | "resumed"
  | "expert_collab";

export interface WireCompaction {
  trigger?: string; // "auto" | "manual"
  messages?: number; // done: how many messages were folded into the summary
  summary?: string; // done: the briefing (empty on an aborted pass)
  archive?: string; // done: archive path, if any
}

// WireCollab is the payload of an "expert_collab" event: a finished expert-team
// collaboration appended to the transcript. Carries the full record (per-round
// expert answers + synthesis) so the frontend renders the folded card without an
// extra fetch. Mirrors desktop/wire.go wireCollab.
export interface WireCollabAnswer {
  expertName: string;
  text: string;
}
export interface WireCollab {
  runId: string;
  teamId: string;
  teamName: string;
  task: string;
  mode: string; // "parallel" | "debate" | "pipeline"
  rounds: WireCollabAnswer[][];
  synthesis: string;
  createdAt: number; // unix ms
}

export interface WireProfile {
  model?: string;
  effort?: string;
}

export interface WireTool {
  id?: string;
  name: string;
  args?: string;
  output?: string;
  err?: string;
  readOnly: boolean;
  truncated?: boolean;
  durationMs?: number;
  partial?: boolean; // an early dispatch (name only) — a full one with args follows
  parentId?: string; // set on a sub-agent's calls — the parent `task` call's id
  profile?: WireProfile; // subagent model/effort resolved for this call
  attachments?: WireAttachment[]; // files the tool produced (e.g. generated images)
}

export interface WireAttachment {
  path: string; // repo-relative, under .momapeer/attachments/
  kind: string; // "image"
}

export interface WireUsage {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  reasoningTokens?: number;
  // Session-cumulative cache tokens — the status bar shows the aggregate
  // hit-rate (Σhit/Σ(hit+miss)), steadier than the single-turn cacheHitTokens.
  // MoMA currently does not report these fields, so they remain 0.
  sessionCacheHitTokens: number;
  sessionCacheMissTokens: number;
}

export interface WireApproval {
  id: string;
  tool: string;
  subject: string;
}

export interface WireAskOption {
  label: string;
  description?: string;
}

export interface WireAskQuestion {
  id: string;
  header?: string;
  prompt: string;
  options: WireAskOption[];
  multi?: boolean;
}

export interface WireAsk {
  id: string;
  questions: WireAskQuestion[];
}

// QuestionAnswer is the reply for one question, sent back via AnswerQuestion.
export interface QuestionAnswer {
  questionId: string;
  selected: string[];
}

export interface WireEvent {
  kind: EventKind;
  text?: string;
  reasoning?: string;
  level?: "info" | "warn";
  tool?: WireTool;
  usage?: WireUsage;
  approval?: WireApproval;
  ask?: WireAsk;
  compaction?: WireCompaction;
  collab?: WireCollab;
  err?: string;
  retryAttempt?: number;
  retryMax?: number;
  // Tab routing: set by the Go-side tabEventSink so multi-tab frontends
  // route each event to the correct per-tab reducer.
  tabId?: string;
  sessionHitTokens?: number;
  sessionMissTokens?: number;
}

// Tab management types (desktop/tabs.go).
export interface TabMeta {
  id: string;
  tabType?: "session" | "file";
  scope: string;
  workspaceRoot: string;
  workspaceName: string;
  topicId: string;
  topicTitle: string;
  filePath?: string;
  projectColor?: string;
  label: string;
  ready: boolean;
  running: boolean;
  mode: Mode;
  collaborationMode?: CollaborationMode;
  toolApprovalMode?: ToolApprovalMode;
  // Knowledge-base collection to auto-inject ("ragScope"); "" = 不使用 (default).
  ragScope?: string;
  goal?: string;
  goalStatus?: GoalStatus;
  startupErr?: string;
  active: boolean;
  cwd: string;
  // Product profile ("dev" | "cowork"); absent = dev. Drives layout selection.
  profile?: string;
  // Set when this tab is an independent expert-team collaboration session.
  // The frontend renders ExpertSessionView (group-chat) instead of the normal
  // transcript when present.
  expertSession?: ExpertSessionMeta;
}

// ExpertSessionMeta carries the team context for an expert-session tab.
export interface ExpertSessionMeta {
  teamId: string;
  teamName: string;
}

export interface ProjectNode {
  key: string;
  kind: "project" | "topic" | "global_folder" | "global_topic" | "expert_folder" | "expert_topic";
  label: string;
  root?: string;
  topicId?: string;
  projectColor?: string;
  turns?: number;
  createdAt?: number;
  lastActivityAt?: number;
  open?: boolean;
  running?: boolean;
  status?: ProjectTopicStatus;
  children?: ProjectNode[];
  expertTeamId?: string;
  expertTeamName?: string;
}

export type ProjectTopicStatus = "thinking" | "streaming" | "waiting_confirmation" | "paused" | "error";

export interface TopicMeta {
  id: string;
  title: string;
  createdAt: number;
}

export interface ContextPanelInfo {
  usedTokens: number;
  windowTokens: number;
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  reasoningTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  requestCount?: number;
  elapsedMs?: number;
  mock?: boolean;
  readFiles: ReadFileRecord[];
  changedFiles: ChangedFileInfo[];
}

export interface ReadFileRecord {
  path: string;
  turn: number;
  time: number;
  offset?: number;
  limit?: number;
  truncated?: boolean;
}

export interface ChangedFileInfo {
  path: string;
  oldPath?: string;
  sources: string[];
  gitStatus?: string;
  turns: number[];
  latestPrompt?: string;
  latestTime?: number;
}

// Bound-method payloads (desktop/app.go).
export interface HistoryMessage {
  role: string;
  content: string;
  reasoning?: string;
  level?: "info" | "warn";
  toolCalls?: HistoryToolCall[];
  toolCallId?: string;
  toolName?: string;
  pending?: boolean;
  trigger?: string;
  messages?: number;
  summary?: string;
  archive?: string;
}

export interface HistoryToolCall {
  id: string;
  name: string;
  arguments: string;
}

// CheckpointMeta is one rewind point (a user turn) for the rewind UI.
export interface CheckpointMeta {
  turn: number;
  prompt: string;
  files: string[];
  time: number; // unix ms
  canCode?: boolean;
  canConversation?: boolean;
}

// SessionMeta is one saved session for the history panel.
export interface SessionMeta {
  path: string;
  preview: string;
  title?: string; // user-chosen name; falls back to preview when empty
  turns: number;
  createdAt: number; // unix milliseconds
  lastActivityAt: number; // unix milliseconds
  modTime: number; // compatibility alias for lastActivityAt
  deletedAt?: number; // unix milliseconds, present for trashed sessions
  current: boolean;
  open: boolean;
  scope?: string;       // "project" | "global"; empty for legacy → treated as "global"
  workspaceRoot?: string;
  topicId?: string;
  topicTitle?: string;
  profile?: string;
}

// SessionReference is a session selected via @ past:chats for context injection.
export interface SessionReference {
  path: string;
  title: string;
  preview?: string;
  turns?: number;
  createdAt?: number;
  lastActivityAt?: number;
}

export interface WorkspaceView {
  path: string;
  name: string;
  current: boolean;
}

export interface ContextInfo {
  used: number;
  window: number;
  sessionTokens: number;
  compactRatio?: number;
}

export interface Meta {
  label: string;
  ready: boolean;
  startupErr?: string;
  eventChannel: string;
  cwd: string;
  autoApproveTools?: boolean;
  bypass?: boolean; // legacy JSON key for YOLO/full-access tool auto-approval
  toolApprovalMode?: ToolApprovalMode;
  // Knowledge-base collection to auto-inject ("ragScope"); "" = 不使用 (default).
  ragScope?: string;
  goal?: string;
  goalStatus?: GoalStatus;
  expertSession?: ExpertSessionMeta;
}

export type CollaborationMode = "normal" | "plan" | "goal";
export type ToolApprovalMode = "ask" | "auto" | "yolo";
export type GoalStatus = "running" | "complete" | "blocked" | "stopped";

export function normalizeCollaborationMode(mode?: string, goal?: string, legacyMode?: Mode): CollaborationMode {
  if (mode === "plan" || mode === "goal" || mode === "normal") return mode;
  if (legacyMode && modeHasPlan(legacyMode)) return "plan";
  if ((goal ?? "").trim()) return "goal";
  return "normal";
}

export function normalizeToolApprovalMode(mode?: string, legacyMode?: Mode, legacyAutoApproveTools?: boolean): ToolApprovalMode {
  if (mode === "auto" || mode === "yolo" || mode === "ask") return mode;
  if (legacyAutoApproveTools || (legacyMode && modeHasAutoApproveTools(legacyMode))) return "yolo";
  return "ask";
}

// Mode is the compatibility string for two independent composer axes:
// plan (read-only/user-plan gate) and yolo/full access (tool auto-approval).
export type Mode = "normal" | "plan" | "yolo" | "plan-yolo";

export function normalizeMode(mode?: string): Mode {
  if (mode === "plan" || mode === "yolo" || mode === "plan-yolo" || mode === "yolo-plan") {
    return mode === "yolo-plan" ? "plan-yolo" : mode;
  }
  return "normal";
}

export function modeHasPlan(mode: Mode): boolean {
  return mode === "plan" || mode === "plan-yolo";
}

export function modeHasAutoApproveTools(mode: Mode): boolean {
  return mode === "yolo" || mode === "plan-yolo";
}

export function modeFromAxes(plan: boolean, autoApproveTools: boolean): Mode {
  if (plan && autoApproveTools) return "plan-yolo";
  if (plan) return "plan";
  if (autoApproveTools) return "yolo";
  return "normal";
}

export function modeWithPlan(mode: Mode, plan: boolean): Mode {
  return modeFromAxes(plan, modeHasAutoApproveTools(mode));
}

export function modeWithAutoApproveTools(mode: Mode, autoApproveTools: boolean): Mode {
  return modeFromAxes(modeHasPlan(mode), autoApproveTools);
}

export interface CommandInfo {
  name: string; // without the leading slash
  description: string;
  hint?: string;
  kind: "builtin" | "custom" | "mcp" | "skill";
}

export interface DirEntry {
  name: string;
  isDir: boolean;
}

export interface DroppedItem {
  kind: "workspace" | "attachment";
  path: string;
  isDir?: boolean;
  previewUrl?: string;
}

export interface FilePreview {
  path: string;
  body: string;
  size: number;
  truncated: boolean;
  binary: boolean;
  kind?: "image" | "pdf";
  mime?: string;
  url?: string;
  err?: string;
}

export interface WorkspaceChangeView {
  path: string;
  oldPath?: string;
  sources: string[];
  gitStatus?: string;
  turns?: number[];
  latestPrompt?: string;
  latestTime?: number;
}

export interface WorkspaceChangesView {
  files: WorkspaceChangeView[];
  gitAvailable: boolean;
  gitErr?: string;
  gitBranch?: string;
}

export interface GitCommitView {
  hash: string;
  author: string;
  date: string;
  message: string;
}

export interface GitCommitDetailView {
  diff?: string;
  files?: string[];
}

export interface ComposerInsertRequest {
  id: number;
  text: string;
}

// MCP & Skills drawer (desktop/app.go Capabilities) — the GUI counterpart to
// /mcp + /skill: connected/failed servers and discoverable skills.
export interface ServerView {
  name: string;
  transport: string;
  status: "connected" | "deferred" | "failed" | "initializing" | "disabled";
  builtIn?: boolean;
  configured?: boolean;
  autoStart: boolean;
  tier?: "lazy" | "background" | "eager" | string;
  command?: string;
  args?: string[];
  url?: string;
  envKeys?: string[];
  tools: number;
  prompts: number;
  resources: number;
  error?: string;
  toolList?: MCPToolView[];
  authStatus?: "none" | "possible" | "required" | string;
  authUrl?: string;
  authConfigured?: boolean;
}
export interface MCPToolView {
  name: string;
  description: string;
}
export interface SkillView {
  name: string;
  description: string;
  scope: string;
  runAs: string;
  enabled: boolean;
  /** In effect under the current product profile. A profile whitelist can hide a
   * skill the user left enabled — enabled=true, active=false. */
  active?: boolean;
}
export interface SkillRootSkillView {
  name: string;
  description: string;
  scope: string;
  runAs: string;
}
export interface SkillRootView {
  dir: string;
  scope: string;
  priority: number;
  status: string;
  configured: boolean;
  removable: boolean;
  skills: number;
  skillItems?: SkillRootSkillView[];
  warning?: string;
}
export interface CapabilitiesView {
  servers: ServerView[];
  skills: SkillView[];
  skillRoots: SkillRootView[];
  jiutianTools?: JiutianToolView[];
}
export interface JiutianToolView {
  name: string;
  description: string;
  enabled: boolean;
}
export interface MCPServerInput {
  name: string;
  transport: string; // stdio | http | sse
  command: string;
  args: string[];
  url: string;
  env?: Record<string, string> | null;
}

export interface ModelInfo {
  ref: string; // "provider/model" — pass to SetModel
  provider: string;
  model: string;
  current: boolean;
}

export interface EffortInfo {
  supported: boolean;
  current: string; // "auto" | "low" | "medium" | "high" | "xhigh" | "max"
  default: string;
  levels: string[];
}

// Product profile entry returned by Profiles() — drives the profile picker in
// the chrome. WorkspaceType is a frontend hint ("code" | "document") that
// selects the layout; the backend ignores it.
export interface ProfileInfo {
  name: string; // "dev" | "cowork" | …
  displayName: string;
  workspaceType?: string;
}

// --- Scheduled tasks (coWork automation panel) ------------------------------
// Mirror desktop/scheduler_app.go view structs. Time fields are pre-formatted
// "YYYY-MM-DD HH:MM" strings (empty when absent) so the UI renders directly
// without a date library.

// One task row in the automation panel. humanSchedule is a friendly Chinese
// rendering of expression (e.g. "工作日 09:00"); the UI may show both.
export interface TaskView {
  id: string;
  name: string;
  expression: string;
  prompt: string;
  profile: string;
  enabled: boolean;
  oneShot: boolean;
  lastRun: string;
  nextRun: string;
  runCount: number;
  lastResult: string;
  outputMode: string; // "" | "im" | "email" | "notify" | "file"
  outputDest: string;
  outputAccount: string; // named mailbox for "email" mode; "" = default
  outputDir: string;
  color?: string;
  location?: string;
  plain: boolean;        // 纯提醒：到点直接弹原文，不调 AI
  // Last delivery outcome. lastDeliverErr is empty when the last run delivered
  // successfully (or had no delivery configured); non-empty means the agent ran
  // but IM/email/file push failed — surfaced in the card so the user notices.
  lastDeliverErr: string;
  lastDeliverAt: string;
  humanSchedule: string;
  source: string;        // "manual" | "calendar"
  calendarEventId: string;
}

// Create/update payload from the UI. Empty id on create.
export interface TaskInput {
  id: string;
  name: string;
  expression: string;
  prompt: string;
  outputMode?: string;
  outputDest?: string;
  outputAccount?: string;
  outputDir?: string;
  color?: string;
  location?: string;
  plain?: boolean;       // 纯提醒：到点直接弹原文，不调 AI
}

// One run-history record (newest first when listed).
export interface RunRecordView {
  taskId: string;
  name: string;
  at: string;
  status: string; // "ok" | "error" | "skipped"
  result: string;
  outputMode: string;
}

// Predefined recipe in the "模板" menu.
export interface TemplateView {
  id: string;
  name: string;
  category: string; // "reminder" | "data" | "ops"
  desc: string;
  expression: string;
  prompt: string;
  outputMode: string;
  outputHint: string;
  oneShot: boolean;
}

// Live preview of an expression input. Kind is "oneshot" | "recurring" |
// "unknown"; absoluteTime is set for one-shot (the resolved instant) and empty
// for recurring (which has no single fire time).
export interface SchedulePreview {
  inputText: string;
  expression: string;
  absoluteTime: string;
  kind: string;
  note: string;
}

// --- Calendar (coWork calendar panel) ----------------------------------------
// Mirror desktop/calendar_app.go view structs.

export interface CalendarEventView {
  id: string;
  title: string;
  description: string;
  location: string;
  start: string;    // "2006-01-02T15:04"
  end: string;
  allDay: boolean;
  timezone: string;
  color: string;
  status: string;   // confirmed / cancelled / tentative
  source: string;   // manual / email / agent
  recurrence: string;
  recurrenceEnd: string;
  reminders: number[];
  taskId: string;
  tags: string[];
  // Reminder push routing (mirrors Go). Empty outputMode = toast only.
  outputMode: string;
  outputDest: string;
  outputAccount: string;
  createdAt: string;
}

export interface CalendarEventInput {
  id: string;
  title: string;
  description: string;
  location: string;
  start: string;
  end: string;
  allDay: boolean;
  timezone: string;
  color: string;
  recurrence: string;
  recurrenceEnd: string;
  reminders: number[];
  tags: string[];
  outputMode?: string;
  outputDest?: string;
  outputAccount?: string;
}

// --- RAG knowledge base (coWork RAG panel) ----------------------------------
// Mirror desktop/rag_app.go view structs. The tree is folder/file recursive;
// file nodes carry FTS5 + extraction status + progress.

// One node in the RAG file/folder tree.
export interface RagNodeView {
  key: string;
  label: string;
  kind: string; // "folder" | "file"
  path: string;
  relPath: string;
  isDir: boolean;
  collection: string;
  status: string; // "indexed" | "extracting" | "enriched" | "error" | "cancelled"
  hasFts5: boolean;
  jobId: string;
  doneChunks: number;
  totalChunks: number;
  entityCount: number;
  errorMsg: string;
  children?: RagNodeView[];
}

// One collection summary (for the dropdown).
export interface RagCollectionView {
  id: string;       // = path (full path, e.g. "工作/领导材料")
  name: string;     // display name (last path segment)
  path: string;     // full path
  parent: string;   // parent path (empty for root)
  documents: number;
  chunks: number;
  entities: number;
}

// Import result (immediate feedback: FTS5 ready, extraction queued).
export interface RagImportResult {
  jobIds: string[];
  files: number;
  ftsChunks: number;
  message: string;
}

// Extraction result summary.
export interface RagExtractResultView {
  entityCount: number;
  relationCount: number;
  topEntities: RagEntityBrief[];
  topRelations: RagRelationBrief[];
  jobCount: number;
  doneCount: number;
  hasData: boolean;
}

export interface RagEntityBrief {
  name: string;
  nameRaw: string;
  type: string;
  description: string;
  relationCount: number;
}

export interface RagRelationBrief {
  source: string;
  target: string;
  type: string;
  description: string;
}

// Combined search hits (entities/relations + FTS5 snippets).
export interface RagSearchHitView {
  entities: RagEntityView[];
  relations: RagRelView[];
  snippets: RagSnippetView[];
}
export interface RagEntityView {
  name: string;
  type: string;
  description: string;
}
export interface RagRelView {
  source: string;
  target: string;
  type: string;
  description: string;
}
export interface RagSnippetView {
  collection: string;
  path: string;
  chunk: number;
  snippet: string;
  score: number;
}

// On-demand ETA probe (for hover tooltip).
export interface RagETAView {
  jobId: string;
  doneChunks: number;
  totalChunks: number;
  avgLatencyMs: number;
  etaSeconds: number;
}

// Progress event payload from the pipeline (rag:progress).
export interface RagProgressEvent {
  jobId: string;
  collection: string;
  path: string;
  status: string;
  doneChunks: number;
  totalChunks: number;
  avgLatencyMs: number;
  message: string;
}

// --- Graph visualization types (mirrors desktop/rag_app.go) ------------------

export interface GraphNodeView {
  id: string;
  label: string;
  type: string;
  description: string;
  sources: Array<{ path: string; chunk: number }>;
  relationCnt: number;
  collection: string;
  community: number;
}

export interface GraphEdgeView {
  source: string;
  target: string;
  type: string;
  description: string;
  weight: number;
  strength: number;
}

export interface GraphDataView {
  nodes: GraphNodeView[];
  edges: GraphEdgeView[];
}

export interface EntityRelationView {
  direction: string; // "out" | "in"
  peer: string;
  type: string;
  description: string;
  weight: number;
  strength: number;
}

export interface EntityDetailView {
  name: string;
  nameRaw: string;
  type: string;
  description: string;
  sources: Array<{ path: string; chunk: number }>;
  relations: EntityRelationView[];
  community: number;
  relationCnt: number;
}

export interface EntityPatch {
  nameRaw: string;
  type: string;
  description: string;
}

export interface ChunkHighlight {
  index: number;
  start: number;
  end: number;
  content: string;
}

export interface DocPreviewView {
  path: string;
  content: string;
  chunks: ChunkHighlight[];
}

// --- Expert team (multi-model collaboration) --------------------------------
// Mirror desktop/experts_app.go view structs.

export interface ExpertView {
  name: string;
  model: string;       // "provider/model" ref, "" = use default
  perspective: string; // role instruction
}

export interface TeamView {
  id: string;
  name: string;
  experts: ExpertView[];
  defaultMode: string;     // "parallel" | "debate" | "pipeline"
  defaultRounds: number;   // debate rounds
  allowSearch: boolean;    // true = each expert runs a web_search mini-agent (slower, accurate for real-time data)
}

// CollabEvent is one streamed event during an expert-team run.
export interface CollabEvent {
  runId: string;
  teamId: string;
  teamName: string;
  phase: string; // "expert_start" | "expert_chunk" | "expert_done" | "synthesis" | "run_done" | "error"
  expertIdx: number;
  expertName: string;
  round: number;
  text: string;   // expert_chunk: delta; synthesis: delta
  message: string;
  mode: string;
}

// ExpertRunView is the queryable status of an in-flight expert-team run, used
// by ExpertPanel/ExpertSessionView to recover after the CoWorkLayout is torn
// down (tab/profile switch) mid-run. Mirrors desktop/expert_runs.go ExpertRunView.
export interface ExpertRunView {
  runId?: string;
  teamId?: string;
  teamName?: string;
  task?: string;
  mode?: string;
  status?: string;  // "" | "running" | "done" | "error"
  startedAt?: number;  // unix ms
  err?: string;
  // Cached live-stream messages accumulated on the backend while the tab was
  // hidden. When the user switches back, the frontend restores these so the
  // experts' progress that happened during the switch isn't lost.
  messages?: StreamMessageWire[];
}

// StreamMessageWire mirrors the Go StreamMessageWire type — the JSON form of
// one expert's (or synthesis's) accumulated text in the live stream.
export interface StreamMessageWire {
  kind: string;       // "expert" | "synthesis"
  expertName: string;
  round: number;
  text: string;
  streaming: boolean;
}

// Slash sub-command / argument completion (desktop/app.go SlashArgs). Mirrors the
// CLI's arg hints so the composer can suggest e.g. /skill → list/show/new/paths.
export interface SlashArgItem {
  label: string;
  insert: string; // token to place at the current position
  hint: string;
  descend: boolean; // re-open the menu one level deeper after accepting
}
export interface SlashArgsResult {
  items: SlashArgItem[];
  from: number; // byte offset where the current token begins
}

// Memory panel payloads (desktop/app.go MemoryView).
export interface MemoryDoc {
  path: string;
  scope: string; // "user" | "ancestor" | "project" | "local"
  body: string;
}

export interface MemoryFact {
  name: string;
  body: string;
  type: string; // "user" | "feedback" | "project" | "reference" (panel tag)
  // v0.4: title/description were removed from the store struct; the index label
  // is now derived from the body's first line. Kept optional here so older
  // payloads and the timeline UI don't type-error — they just read as empty.
  title?: string;
  description?: string;
  // Bitemporal fields (v0.3.0). No longer populated by the slimmed store (the
  // bitemporal model was removed in v0.4); retained optional so the timeline
  // panel compiles. They will be undefined on new facts.
  validFrom?: string;
  validTo?: string;
  status?: string;
  category?: string;
  tags?: string[];
  supersededBy?: string;
  createdAt?: string; // RFC3339, system write time
  updatedAt?: string;
}

export interface MemoryScope {
  scope: string; // "user" | "project" | "local"
  path: string;
}

// ProfileView is the active mode's portrait (cowork.md under cowork, dev.md
// under dev). The workspace preference panel reads it on open and writes it
// back via SaveDoc (the path is whitelisted in the backend).
export interface ProfileView {
  path: string;
  content: string;
}

export interface MemoryView {
  docs: MemoryDoc[];
  facts: MemoryFact[];
  scopes: MemoryScope[];
  storeDir: string;
  available: boolean;
}

// Dream / Distill self-evolution payloads (desktop/app.go DreamStatusView).
export interface DreamRunView {
  kind: string; // "dream" | "distill"
  trigger: string; // "auto" | "manual"
  startedAt: string; // RFC3339
  duration?: string;
  status: string; // "ok" | "error" | "timeout"
  error?: string;
}

export interface DreamStatusView {
  enabled: boolean;
  dreamInterval: number;
  distillInterval: number;
  dreamInFlight: boolean;
  distillInFlight: boolean;
  lastDream?: DreamRunView;
  lastDistill?: DreamRunView;
  history: DreamRunView[];
}

// SettingsTab is the top-level navigation item in the Settings Centre modal.
export type SettingsTab = "general" | "models" | "providers" | "bots" | "cowork" | "preference" | "mcp" | "skills" | "memory" | "permissions" | "sandbox" | "network" | "hooks" | "appearance" | "updates";

// Settings panel payloads (desktop/settings_app.go).
export interface ProviderView {
  name: string;
  builtIn: boolean;
  added: boolean;
  kind: string;
  baseUrl: string;
  models: string[];
  modelsUrl: string; // optional override for model discovery; empty derives from baseUrl
  default: string;
  apiKeyEnv: string;
  keySet: boolean; // the env var currently resolves to a value
  contextWindow: number;
  reasoningProtocol: string; // auto|moma|MoMA|openai|none; empty = auto/model registry
  supportedEfforts: string[]; // custom /effort levels; empty = use built-in Kind/BaseURL default
  defaultEffort: string; // /effort level when user picks "auto" or unset; "" = supportedEfforts[0]
}

// JobView is one running background job (desktop/app.go Jobs) for the status bar.
export interface JobView {
  id: string;
  kind: string; // "bash" | "task"
  label: string;
  status: string; // "running"
  startedAt: number; // unix milliseconds
}

export interface PermissionsView {
  mode: string; // "ask" | "allow" | "deny"
  allow: string[];
  ask: string[];
  deny: string[];
}

export interface SandboxView {
  bash: string; // "enforce" | "off"
  network: boolean;
  workspaceRoot: string;
  allowWrite: string[];
}

export interface NetworkProxyView {
  type: string;
  server: string;
  port: number;
  username: string;
  password: string;
}

export interface NetworkView {
  proxyMode: string; // "auto" | "custom" | "off" (backend may still return legacy "env")
  proxyUrl: string;
  noProxy: string;
  proxy: NetworkProxyView;
}

export interface AgentView {
  temperature: number;
  maxSteps: number;
  /** @deprecated retained for Wails binding parity; the backend ignores it. */
  plannerMaxSteps?: number;
  systemPrompt: string;
  rpm: number; // max requests/minute; 0 = unlimited
}

export interface BotAllowlistView {
  enabled: boolean;
  allowAll: boolean;
  mode: string; // "open" | "review"
  qqUsers: string[];
  feishuUsers: string[];
  weixinUsers: string[];
  qqGroups: string[];
  feishuGroups: string[];
  weixinGroups: string[];
}

export interface QQBotView {
  enabled: boolean;
  appId: string;
  appSecretEnv: string;
  secretSet: boolean;
}

export interface FeishuBotView {
  enabled: boolean;
  domain: string;
  appId: string;
  appSecretEnv: string;
  secretSet: boolean;
  verificationToken: string;
  mode: string;
  webhookPort: number;
  requireMention: boolean;
}

export interface WeixinBotView {
  enabled: boolean;
  accountId: string;
  tokenEnv: string;
  tokenSet: boolean;
  apiBase: string;
}

export interface BotConnectionCredentialView {
  appId: string;
  appSecretEnv: string;
  accountId: string;
  tokenEnv: string;
  secretSet: boolean;
}

export interface BotConnectionSessionMappingView {
  remoteId: string;
  sessionId: string;
  scope: "global" | "project" | string;
  workspaceRoot: string;
  updatedAt: string;
}

export interface BotConnectionView {
  id: string;
  provider: "qq" | "feishu" | "weixin" | string;
  domain: "qq" | "feishu" | "lark" | "weixin" | string;
  label: string;
  enabled: boolean;
  status: "disconnected" | "pending" | "connected" | "error" | string;
  model: string;
  workspaceRoot: string;
  credential: BotConnectionCredentialView;
  sessionMappings: BotConnectionSessionMappingView[];
  lastError: string;
  createdAt: string;
  updatedAt: string;
}

// HookConfigView mirrors the Go HookConfigView (one hook, flat — carries its
// event). command is required; match is a regex for PreToolUse/PostToolUse only.
export interface HookConfigView {
  event: string;
  match?: string;
  command: string;
  description?: string;
  timeout?: number;
  cwd?: string;
}

// HooksSettingsView mirrors the Go HooksSettingsView — the hooks panel payload.
// scope is "global" | "project"; path is the settings.json being edited;
// projectRoot/trusted apply to the project scope (project hooks load only when
// trusted); events is the valid event list (drives the JSON editor validation).
export interface HooksSettingsView {
  scope: string;
  path: string;
  projectRoot: string;
  trusted: boolean;
  hooks: HookConfigView[];
  events: string[];
}

export interface BotSettingsView {
  enabled: boolean;
  model: string;
  maxSteps: number;
  debounceMs: number;
  allowlist: BotAllowlistView;
  qq: QQBotView;
  feishu: FeishuBotView;
  weixin: WeixinBotView;
  connections: BotConnectionView[];
}

// CoWorkSettingsView mirrors the Go CoWorkSettingsView. Secrets (SMTP/IMAP
// passwords) are presented as plain fields here; they're persisted to a
// momapeer-managed .env (not config.toml). detectedBrowser is a read-only
// diagnostic from CheckCoworkBrowser. pptTemplates/pptActiveTemplate drive the
// PPT-template dropdown; pptTemplateDir is where the user drops JSON templates.
export interface CoWorkSettingsView {
  browserPath: string;
  embeddingModel: string;
  // PPT template selection. pptTemplates lists available templates (id+name)
  // read from the templates dir; pptActiveTemplate is the selected id ("=" none).
  pptActiveTemplate: string;
  pptTemplates: PPTTemplateView[];
  pptTemplateDir: string;
  pptMode: string;
  smtp: SMTPSettings;
  imap: IMAPSettings;
  smtpPassword: string;
  imapPassword: string;
  // True when an encrypted secret is stored for the password_env — the panel
  // shows "已设置" without ever holding the value (the field above is write-only).
  smtpPasswordSet: boolean;
  imapPasswordSet: boolean;
  detectedBrowser: string;
  // Screenshot hotkey → VLM feature (off by default; user opts in).
  screenshotEnabled: boolean;
  screenshotHotkey: string;
  screenshotVlmModel: string;
  // Emergency-stop hotkey for desktop automation (always on by default; set
  // "off" to disable). Cancels the in-flight turn globally.
  estopHotkey: string;
  // Multi-mailbox list. When non-empty it's the source of truth on save (full
  // overwrite); the legacy smtp/imap single-pair fields mirror the Default
  // account for backward compat.
  emailAccounts: EmailAccountView[];
  // When true, email_send is added to permissions.Allow so scheduled tasks can
  // send email in headless mode (no interactive user to approve).
  allowHeadlessEmail: boolean;
}

// EmailAccountView is one mailbox in the multi-account list. password is
// write-only (a freshly-typed value on save; the stored secret is never echoed
// back). passwordSet reports whether a secret is stored.
export interface EmailAccountView {
  name: string;
  default: boolean;
  smtp: SMTPSettings;
  imap: IMAPSettings;
  password: string;
  passwordSet: boolean;
}

// PPTTemplateView is a trimmed PPT template (id + name) for the settings
// dropdown. The full template (master file + theme + layout coordinates) is
// loaded by the backend and injected into the ppt-auto skill context.
export interface PPTTemplateView {
  id: string;
  name: string;
}

export interface SMTPSettings {
  host: string;
  port: number;
  from: string;
  username: string;
  passwordEnv: string;
  useTLS: boolean;
  encryptionMode?: string; // "tls" | "starttls" | "none"; empty → migrate from useTLS
}

export interface IMAPSettings {
  host: string;
  port: number;
  username: string;
  passwordEnv: string;
}

export interface WebSearchView {
  braveKeySet: boolean;
  exaKeySet: boolean;
  linkupKeySet: boolean;
}

export interface BotInstallStartResult {
  ok: boolean;
  provider: string;
  domain: string;
  installId: string;
  url: string;
  deviceCode: string;
  userCode: string;
  interval: number;
  expireIn: number;
  message: string;
}

export interface BotInstallPollResult {
  done: boolean;
  connection: BotConnectionView;
  status: string;
  message: string;
  error: string;
}

// RecentChatView is one recently-seen IM chat, offered as a push destination in
// the task form's IM-target picker. userName is the best available display name
// (private chat = user name; group may be empty when the platform doesn't
// expose group names).
export interface RecentChatView {
  platform: string;
  chatType: string;
  chatId: string;
  userName: string;
  lastSeen: number;
}

// BotDockStatusView is the lightweight bot status shown in the dock Today panel:
// online (gateway running), connected platforms, and recent chat count.
export interface BotDockStatusView {
  online: boolean;
  platforms: string[];
  recentCount: number;
}

export interface BotConnectionDiagnostic {
  id: string;
  label: string;
  status: string;
  message: string;
  messageId: string;
}

// MailProbeResult is the outcome of a mailbox IMAP connection probe, returned
// by ProbeMailAccount so the mail card can show a green/red status dot after
// the user saves the mailbox config. status: "ok" | "error" | "unconfigured".
export interface MailProbeResult {
  ok: boolean;
  status: string;
  message: string;
}

// InboxItem is one row in the cowork dock's "邮件" tab: a trimmed-down mail
// envelope (from/subject/date + a short body preview). Returned by
// InboxPreview; an empty array means either no mailbox configured or no unread
// mail — the dock distinguishes via ProbeMailAccount's status.
export interface InboxItem {
  from: string;
  subject: string;
  date: string;
  preview: string;
}

export interface SettingsView {
  defaultModel: string;
  fastTaskModel: string;
  subagentModel: string;
  subagentEffort: string;
  autoPlan: string;
  providers: ProviderView[];
  officialProviders: ProviderView[];
  permissions: PermissionsView;
  sandbox: SandboxView;
  network: NetworkView;
  agent: AgentView;
  bot: BotSettingsView;
  cowork: CoWorkSettingsView;
  webSearch: WebSearchView;
  jiutian?: { imageUnderstand: boolean; imageGenerate: boolean; videoUnderstand: boolean };
  desktopLanguage: string; // "" | "en" | "zh"; empty = auto
  desktopTheme: string; // "auto" | "dark" | "light"
  desktopThemeStyle: string;
  closeBehavior: string; // "background" | "quit"
  displayMode: string;   // "standard" | "compact" | "minimal"
  checkUpdates: boolean; // check for new versions on startup
  telemetry: boolean; // anonymous launch ping (install id + version + OS)
  expandThinking: boolean; // show reasoning text expanded by default
  configPath: string;
  providerKinds: string[]; // provider implementations the kernel registered (for the kind picker)
  autoApproveTools: boolean;
  bypass: boolean; // legacy JSON key for live YOLO/full-access tool auto-approval
}

// Auto-updater payloads (desktop/updater.go). UpdateInfo drives the update banner;
// UpdateProgress streams on the "updater:progress" event during ApplyUpdate.
export interface UpdateInfo {
  available: boolean;
  current: string;
  latest: string;
  notes: string;
  canSelfUpdate: boolean; // win/linux true; macOS false (no cert → manual download)
  downloadUrl: string; // human-facing releases page (macOS path / fallback link)
  assetSize: number; // running platform's artifact size, for the progress bar
  err?: string; // set when the check itself failed (both endpoints down)
}

export interface UpdateProgress {
  phase: "downloading" | "verifying" | "applying" | "done" | "error";
  received: number;
  total: number;
  err?: string;
}

// BudgetStatusView describes the rate-limit budget window for live UI display
// (rpm/used/remaining). Mirrors desktop.BudgetStatusView on the Go side.
export interface BudgetStatusView {
  rpm: number;
  used: number;
  remaining: number;
  reserveMain: number;
  windowSecs: number;
}

// MergeCandidate represents one entity in the graph deduplication merge flow:
// the candidate to merge away (name/raw/score), the keep target (keepName/keepRaw),
// and the proposed merged result (mergeName/mergeRaw) for review before applying.
export interface MergeCandidate {
  name: string;
  raw?: string;
  score?: number;
  keepName?: string;
  keepRaw?: string;
  mergeName?: string;
  mergeRaw?: string;
}
