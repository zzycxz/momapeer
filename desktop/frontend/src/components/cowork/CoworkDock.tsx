// CoworkDock is the cowork-mode right-side panel.
//
// Two modes:
//   - mode="default" (the classic cowork dock): 3 tabs — 今日 / 邮件 / 文件
//     (today's calendar events, mailbox probe status, workspace files).
//   - mode="rag" (knowledge base navigation): 4 tabs — 集合 / 实体 / 文件 / 提取.
//     This mirrors the navigation sidebar described in RagPanel.tsx: a list of
//     collections, the active collection's entities, its file tree, and the
//     deep-extraction UI (TemplateSelect). Tab strip styling intentionally
//     matches the coding-mode workbench-dock (see styles.css comment:
//     "Tab strip mirrors .workbench-dock__tools/__tabs/__tab so the two docks
//     read as the same control").
//
// Sub-views (EntityDetail / DocPreview) replace the __body content with a
// "返回" affordance back to the tab list.
//
// Go methods used (all verified to exist on AppBindings):
//   - ListCalendarEvents(since, before string) → []CalendarEventView
//   - ListScheduledTasks() → []TaskView
//   - ProbeMailAccount() → MailProbeResult
//   - ListDir(rel string) → []DirEntry
//   - ListRagCollections() → []RagCollectionView
//   - ListRagTree(collection string) → []RagNodeView
//   - GetTopEntities(collection string, limit number) → GraphDataView
//   - GetEntityDetail(collection, name string) → EntityDetailView
//   - GetDocumentPreview(collection, docPath string) → DocPreviewView
//   - RagExtractResult(collection string) → RagExtractResultView
//   - RagImportPaths(collection, paths string[]) → RagImportResult
//   - RagCleanCollection(collection string) → void
//   - PickWorkspace() → string

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  CalendarDays,
  Circle,
  FileText,
  Folder,
  FolderOpen,
  Inbox,
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
  DirEntry,
  GraphDataView,
  GraphNodeView,
  MailProbeResult,
  RagCollectionView,
  RagNodeView,
  TaskView,
} from "../../lib/types";
import { useT } from "../../lib/i18n";
import { useToast } from "../../lib/toast";
import { ENTITY_TYPES, ENTITY_TYPE_LABELS, colorFor } from "./entityTypes";
import { fileIconColor } from "./fileTypeColors";
import { TemplateSelect } from "./TemplateSelect";
import { EntityDetail } from "./EntityDetail";
import { DocPreview } from "./DocPreview";

type DockTab = "today" | "mail" | "files";

// RAG-mode tabs: 集合 / 实体 / 文件 / 提取
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

interface TodayData {
  events: CalendarEventView[];
  tasks: TaskView[];
  loading: boolean;
}

interface FilesData {
  entries: DirEntry[];
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
  onEntityClick,
  onFileClick,
}: CoworkDockProps) {
  const t = useT();
  const isRag = mode === "rag";
  const [tab, setTab] = useState<DockTab>("today");
  const [ragTab, setRagTab] = useState<RagTab>("collections");

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

  // --- files data (default mode: workspace top-level entries) ---
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

  // --- RAG collections ---
  const [collections, setCollections] = useState<RagCollectionView[]>([]);
  const [collectionsLoading, setCollectionsLoading] = useState(true);
  const [collectionsError, setCollectionsError] = useState<string | null>(null);
  const refreshCollections = useCallback(async () => {
    setCollectionsLoading(true);
    setCollectionsError(null);
    try {
      const cols = await app.ListRagCollections().catch((e) => {
        throw e;
      });
      setCollections(cols ?? []);
    } catch (e) {
      setCollections([]);
      setCollectionsError(String(e));
    } finally {
      setCollectionsLoading(false);
    }
  }, []);

  // Active collection: persisted in window so RagPanel and CoworkDock agree.
  // Default to first collection once loaded.
  const [activeCollection, setActiveCollection] = useState<string>("");
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
    void refreshMail();
    void refreshFiles();
    const unsub1 = onCalendarChanged(() => void refreshToday());
    const unsub2 = onSchedulerChanged(() => void refreshToday());
    return () => {
      unsub1();
      unsub2();
    };
  }, [isRag, refreshToday, refreshMail, refreshFiles, refreshCollections]);

  return (
    <aside className="cowork-dock" aria-label={t("coworkDock.label") || "办公概览"}>
      <div className="cowork-dock__tools">
        {isRag ? (
          <div className="cowork-dock__tabs">
            <RagTabButton
              active={ragTab === "collections"}
              onClick={() => setRagTab("collections")}
              icon={<NetworkIcon size={13} />}
              label={t("coworkDock.collections") || "集合"}
            />
            <RagTabButton
              active={ragTab === "entities"}
              onClick={() => setRagTab("entities")}
              icon={<NetworkIcon size={13} />}
              label={t("coworkDock.entities") || "实体"}
            />
            <RagTabButton
              active={ragTab === "files"}
              onClick={() => setRagTab("files")}
              icon={<FileText size={13} />}
              label={t("coworkDock.files") || "文件"}
            />
            <RagTabButton
              active={ragTab === "extract"}
              onClick={() => setRagTab("extract")}
              icon={<Zap size={13} />}
              label={t("coworkDock.extract") || "提取"}
            />
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
        {isRag ? (
          <RagBody
            tab={ragTab}
            collections={collections}
            collectionsLoading={collectionsLoading}
            collectionsError={collectionsError}
            activeCollection={activeCollection}
            onSelectCollection={(name) => {
              setActiveCollection(name);
              // Notify the graph (and anything listening) of the active switch.
              window.dispatchEvent(new CustomEvent("rag:active-collection", { detail: { collection: name } }));
            }}
            onRefreshCollections={() => void refreshCollections()}
            onEntityClick={onEntityClick}
            onFileClick={onFileClick}
          />
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

// --- RAG tab button (wrapper to keep the strip tidy) -----------------------

function RagTabButton({
  active,
  onClick,
  icon,
  label,
}: {
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  label: string;
}) {
  return (
    <button
      className={`cowork-dock__tab ${active ? "cowork-dock__tab--active" : ""}`}
      type="button"
      onClick={onClick}
      title={label}
    >
      {icon}
      <span className="cowork-dock__tab-label">{label}</span>
    </button>
  );
}

// --- RAG body router: renders the active tab's content ---------------------

function RagBody({
  tab,
  collections,
  collectionsLoading,
  collectionsError,
  activeCollection,
  onSelectCollection,
  onRefreshCollections,
  onEntityClick,
  onFileClick,
}: {
  tab: RagTab;
  collections: RagCollectionView[];
  collectionsLoading: boolean;
  collectionsError: string | null;
  activeCollection: string;
  onSelectCollection: (name: string) => void;
  onRefreshCollections: () => void;
  onEntityClick?: (name: string) => void;
  onFileClick?: (path: string) => void;
}) {
  // Sub-view navigation: when set, replaces the tab list with a detail panel.
  const [entityDetailName, setEntityDetailName] = useState<string | null>(null);
  const [docPreviewPath, setDocPreviewPath] = useState<string | null>(null);

  // Reset sub-views when switching tabs (back to the tab list).
  useEffect(() => {
    setEntityDetailName(null);
    setDocPreviewPath(null);
  }, [tab]);

  // 集合 tab -------------------------------------------------------------
  if (tab === "collections") {
    return (
      <CollectionsTab
        collections={collections}
        loading={collectionsLoading}
        error={collectionsError}
        activeCollection={activeCollection}
        onSelect={onSelectCollection}
        onRefresh={onRefreshCollections}
      />
    );
  }

  // 实体 tab -------------------------------------------------------------
  if (tab === "entities") {
    if (entityDetailName) {
      return (
        <EntityDetail
          collection={activeCollection}
          entityName={entityDetailName}
          onBack={() => setEntityDetailName(null)}
          onHighlightInGraph={(name) => onEntityClick?.(name)}
          onNavigatePeer={(name) => setEntityDetailName(name)}
        />
      );
    }
    return (
      <EntitiesTab
        collection={activeCollection}
        onOpenEntity={(name) => setEntityDetailName(name)}
        onEntityClick={onEntityClick}
      />
    );
  }

  // 文件 tab -------------------------------------------------------------
  if (tab === "files") {
    if (docPreviewPath) {
      return (
        <DocPreview
          collection={activeCollection}
          docPath={docPreviewPath}
          onBack={() => setDocPreviewPath(null)}
        />
      );
    }
    return (
      <FilesTab
        collection={activeCollection}
        onOpenFile={(path) => setDocPreviewPath(path)}
        onFileClick={onFileClick}
      />
    );
  }

  // 提取 tab -------------------------------------------------------------
  return <TemplateSelect collection={activeCollection} onBack={() => { /* no-op: stay in dock */ }} />;
}

// --- 集合 tab: list collections, switch active, create/delete -----------

function CollectionsTab({
  collections,
  loading,
  error,
  activeCollection,
  onSelect,
  onRefresh,
}: {
  collections: RagCollectionView[];
  loading: boolean;
  error: string | null;
  activeCollection: string;
  onSelect: (name: string) => void;
  onRefresh: () => void;
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
        <button className="cowork-dock__tab" type="button" onClick={onRefresh} title="刷新">
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
        <button className="cowork-dock__tab" type="button" onClick={onRefresh} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
    );
  }

  const handleCreate = async () => {
    const name = window.prompt(t("coworkDock.createCollectionPrompt") || "输入新集合名称：", "");
    if (!name) return;
    try {
      // Importing into a new collection name creates it implicitly.
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
    <div className="cowork-rag__body">
      <div className="cowork-mailtab__head">
        <span className="cowork-mailtab__status">
          {t("coworkDock.collections") || "集合"} ({collections.length})
        </span>
        <button className="cowork-mailtab__refresh" type="button" onClick={() => void handleCreate()} title="新建集合">
          <Plus size={13} />
        </button>
        <button className="cowork-mailtab__refresh" type="button" onClick={onRefresh} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
      <div className="cowork-dock__group">
        <ul className="cowork-today__list">
          {collections.map((c) => {
            const active = c.name === activeCollection;
            return (
              <li
                key={c.name}
                className={`cowork-today__row${active ? " cowork-today__row--active" : ""}`}
                onClick={() => onSelect(c.name)}
                style={{ cursor: "pointer" }}
                title={c.name}
              >
                {active ? <FolderOpen size={13} /> : <Folder size={13} />}
                <span className="cowork-today__text" style={{ flex: 1, minWidth: 0 }}>
                  {c.name}
                </span>
                {c.documents > 0 && (
                  <span className="cowork-dock__empty-hint" style={{ margin: 0 }}>
                    {c.documents}文 / {c.entities}实
                  </span>
                )}
                <button
                  className="cowork-mailtab__refresh"
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    void handleDelete(c.name);
                  }}
                  title={t("cowork.ragRemove") || "清理"}
                >
                  <Trash2 size={12} />
                </button>
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}

// --- 实体 tab: list entities in active collection, filter by type --------

function EntitiesTab({
  collection,
  onOpenEntity,
  onEntityClick,
}: {
  collection: string;
  onOpenEntity: (name: string) => void;
  onEntityClick?: (name: string) => void;
}) {
  const t = useT();
  const [data, setData] = useState<GraphDataView | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [filterTypes, setFilterTypes] = useState<string[]>([]);

  const refresh = useCallback(async () => {
    if (!collection) return;
    setLoading(true);
    setError(null);
    try {
      const d = await app.GetTopEntities(collection, 200);
      setData(d);
    } catch (e) {
      setData(null);
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }, [collection]);

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

  if (!collection) {
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
        <button className="cowork-dock__tab" type="button" onClick={() => void refresh()} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
    );
  }

  const toggleType = (key: string) => {
    setFilterTypes((cur) => (cur.includes(key) ? cur.filter((x) => x !== key) : [...cur, key]));
  };

  return (
    <div className="cowork-rag__body">
      {/* Search box */}
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

      {/* Type filter chips */}
      <div className="cowork-rag__entity" style={{ flexWrap: "wrap", gap: 4 }}>
        {ENTITY_TYPES.map((def) => {
          const on = filterTypes.includes(def.key);
          return (
            <button
              key={def.key}
              type="button"
              className={`cowork-dock__tab${on ? " cowork-dock__tab--active" : ""}`}
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
            className="cowork-dock__tab"
            onClick={() => setFilterTypes([])}
            style={{ fontSize: 10.5, padding: "1px 6px", height: 18 }}
          >
            ✕
          </button>
        )}
      </div>

      {/* Entity list */}
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
      <span className="cowork-rag__entity-name" style={{ flex: "1 1 auto", minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {node.label}
      </span>
      <span className="cowork-dock__empty-hint" style={{ margin: 0, fontSize: 10.5 }}>
        {label}
        {node.relationCnt > 0 ? ` · ${node.relationCnt}` : ""}
      </span>
    </li>
  );
}

// --- 文件 tab: file tree of the active collection ------------------------

function FilesTab({
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
      const nodes = await app.ListRagTree(collection);
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

  // Drag-and-drop import: hand dropped paths to RagImportPaths.
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
        <button className="cowork-dock__tab" type="button" onClick={() => void refresh()} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
    );
  }

  // Flatten the tree into a simple depth-aware list (folders + files).
  const flat: Array<{ node: RagNodeView; depth: number }> = [];
  const walk = (nodes: RagNodeView[], depth: number) => {
    for (const n of nodes) {
      flat.push({ node: n, depth });
      if (n.children && n.children.length > 0) walk(n.children, depth + 1);
    }
  };
  walk(tree, 0);

  if (flat.length === 0) {
    return (
      <div className="cowork-dock__empty-state" style={{ "--wails-drop-target": "drop" } as React.CSSProperties}>
        <FolderOpen size={22} />
        <p>{t("coworkDock.dragFilesHint") || "拖入文件以导入"}</p>
        <span className="cowork-dock__empty-hint">
          {t("coworkDock.dragFilesHint2") || "支持 md / docx / pdf / xlsx / csv / 代码 等格式"}
        </span>
        <button className="cowork-dock__tab" type="button" onClick={() => void refresh()} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
    );
  }

  return (
    <div className="cowork-rag__tree cowork-rag__body" style={{ "--wails-drop-target": "drop" } as React.CSSProperties}>
      <div className="cowork-mailtab__head">
        <span className="cowork-mailtab__status">
          {t("coworkDock.files") || "文件"} ({flat.filter((f) => f.node.kind === "file").length})
        </span>
        <button className="cowork-mailtab__refresh" type="button" onClick={() => void refresh()} title="刷新">
          <RefreshCw size={12} />
        </button>
      </div>
      <ul className="cowork-today__list">
        {flat.map(({ node, depth }) => {
          const isDir = node.kind === "folder" || node.isDir;
          const iconColor = !isDir ? fileIconColor(node.label) : undefined;
          return (
            <li
              key={node.key || node.path || node.label}
              className="cowork-today__row"
              style={{
                cursor: isDir ? "default" : "pointer",
                paddingLeft: 8 + depth * 14,
              }}
              title={node.label}
              onClick={() => {
                if (isDir) return;
                onFileClick?.(node.path || node.relPath || node.label);
                onOpenFile(node.path || node.relPath || node.label);
              }}
            >
              {isDir ? <Folder size={13} /> : <FileText size={13} style={iconColor ? { color: iconColor } : undefined} />}
              <span className="cowork-today__text" style={{ flex: 1, minWidth: 0 }}>
                {node.label}
              </span>
              {!isDir && node.status === "enriched" && node.entityCount > 0 && (
                <span className="cowork-dock__empty-hint" style={{ margin: 0 }}>
                  {node.entityCount}实
                </span>
              )}
              {!isDir && node.status === "extracting" && (
                <span className="cowork-dock__empty-hint" style={{ margin: 0 }}>
                  {node.totalChunks > 0 ? Math.round((node.doneChunks / node.totalChunks) * 100) : 0}%
                </span>
              )}
            </li>
          );
        })}
      </ul>
    </div>
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

// --- 文件 view: workspace top-level entries (default mode) ------------------

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

