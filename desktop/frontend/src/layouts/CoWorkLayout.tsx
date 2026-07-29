import { useEffect, useState, type ReactNode, type PointerEvent as ReactPointerEvent, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { BookOpen, CalendarDays, SquarePen, Users, SlidersHorizontal } from "lucide-react";

import { useT } from "../lib/i18n";
import { app, onExpertsCollab } from "../lib/bridge";
import { CalendarTaskPanel } from "../components/cowork/CalendarTaskPanel";
import { RagPanel } from "../components/cowork/RagPanel";
import { PreferencePanel } from "../components/cowork/PreferencePanel";
import { ExpertPanel } from "../components/cowork/ExpertPanel";
import { CoworkDock } from "../components/cowork/CoworkDock";

export type CoWorkPanel = "taskCenter" | "preference" | "calendarTask" | "rag" | "experts";

export interface CoWorkLayoutProps {
  headerNode?: ReactNode;
  mainNode?: ReactNode;
  footerNode?: ReactNode;
  projectTreeNode?: ReactNode;
  rightDockOpen?: boolean;
  sidebarCollapsed?: boolean;
  onNewSession?: () => void;
  dockCwd?: string;
  dockMaximized?: boolean;
  dockOnClose?: () => void;
  dockOnToggleMaximized?: () => void;
  // Right dock width resizer — the cowork dock shares the same width state and
  // drag logic as the coding-mode workspace panel, so App.tsx hands the same
  // handlers in. Without these the cowork dock had no resizer and couldn't be
  // resized.
  dockWidth?: number;
  dockMinWidth?: number;
  dockMaxAriaWidth?: number;
  onDockResizeStart?: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onDockResizeKey?: (event: ReactKeyboardEvent<HTMLButtonElement>) => void;
  onDockResetWidth?: () => void;
  // Left sidebar width resizer — absolute-positioned (like coding-mode), driven
  // by the shared sidebarWidth state. The coding-mode .sidebar-resizer is hidden
  // under .app--cowork, so cowork renders its own.
  sidebarWidth?: number;
  sidebarMinWidth?: number;
  sidebarMaxWidth?: number;
  onSidebarResizeStart?: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onSidebarResizeKey?: (event: ReactKeyboardEvent<HTMLButtonElement>) => void;
  onSidebarResetWidth?: () => void;
}

export function CoWorkLayout({
  headerNode,
  mainNode,
  footerNode,
  projectTreeNode,
  rightDockOpen = false,
  sidebarCollapsed = false,
  onNewSession,
  dockCwd,
  dockMaximized = false,
  dockOnClose,
  dockOnToggleMaximized,
  dockWidth,
  dockMinWidth,
  dockMaxAriaWidth,
  onDockResizeStart,
  onDockResizeKey,
  onDockResetWidth,
  sidebarWidth,
  sidebarMinWidth,
  sidebarMaxWidth,
  onSidebarResizeStart,
  onSidebarResizeKey,
  onSidebarResetWidth,
}: CoWorkLayoutProps) {
  const t = useT();
  const [activePanel, setActivePanel] = useState<CoWorkPanel>("taskCenter");
  const [preferenceOpen, setPreferenceOpen] = useState(false);

  // When an expert-team run kicks off from the chat (the agent called
  // expert_team_run), auto-switch to the experts panel so the user sees the
  // streamed collaboration instead of staring at a frozen chat waiting on a
  // multi-minute tool call. We react only to the run-start event, not every
  // chunk, and only when the user isn't already viewing experts (don't yank
  // them away mid-edit in another panel they deliberately opened). Panel-runs
  // (started from the ExpertPanel itself) already have activePanel=="experts",
  // so this no-ops for them.
  useEffect(() => {
    return onExpertsCollab((ev) => {
      // Auto-switch to the experts panel only when NOT already viewing the
      // expert-session tab (whose ExpertSessionView shows the live stream in the
      // main area). Switching to the sidebar panel here would yank the user out
      // of the ExpertSessionView they're watching.
      if (ev.phase === "expert_start" && activePanel !== "experts" && activePanel !== "taskCenter") {
        setActivePanel("experts");
      }
    });
  }, [activePanel]);

  // Global event listener to reset view to Task Center when opening or creating a session.
  useEffect(() => {
    const handleReset = () => setActivePanel("taskCenter");
    window.addEventListener("cowork:reset-panel", handleReset);
    return () => window.removeEventListener("cowork:reset-panel", handleReset);
  }, []);

  // Cross-navigation: a caller dispatches cowork:open-experts with detail.teamId
  // to jump to that team's expert-session tab (which occupies the main chat area
  // as a group-chat view). Opening the tab makes it active, so the main area
  // swaps to ExpertSessionView automatically. We also reset to taskCenter so
  // the user actually sees the ExpertSessionView — without this, if they were
  // on another panel (preference/calendar/rag), the tab opens but stays hidden.
  useEffect(() => {
    const handleOpenExperts = (e: Event) => {
      const detail = (e as CustomEvent<{ teamId?: string; teamName?: string }>).detail;
      if (detail?.teamId) {
        void app.OpenExpertSessionTab(detail.teamId, detail.teamName ?? "").catch(() => {});
        setActivePanel("taskCenter");
        window.dispatchEvent(new CustomEvent("cowork:reset-panel"));
      }
    };
    window.addEventListener("cowork:open-experts", handleOpenExperts as EventListener);
    return () => window.removeEventListener("cowork:open-experts", handleOpenExperts as EventListener);
  }, []);

  return (
    <div className={`cowork-layout ${sidebarCollapsed ? "cowork-layout--sidebar-collapsed" : ""}`}>
      {/* Left sidebar width resizer — absolute-positioned (mirrors coding-mode
         .sidebar-resizer). Rendered only when not collapsed and handlers wired.
         The coding-mode .sidebar-resizer is display:none under .app--cowork, so
         cowork renders its own to keep the sidebar resizable. */}
      {!sidebarCollapsed && onSidebarResizeStart && (
        <button
          className="sidebar-resizer"
          type="button"
          role="separator"
          aria-orientation="vertical"
          aria-label={t("sidebar.resize")}
          aria-valuemin={sidebarMinWidth}
          aria-valuemax={sidebarMaxWidth}
          aria-valuenow={sidebarWidth}
          onPointerDown={onSidebarResizeStart}
          onKeyDown={onSidebarResizeKey}
          onDoubleClick={onSidebarResetWidth}
        />
      )}
      {/* Left: workspace / knowledge / scheduled / skills.
          Kept mounted (not conditionally removed) on collapse so the grid column
          width animates smoothly — the sidebar slot shrinks to 0px via the
          cowork-layout--sidebar-collapsed class (mirroring coding-mode .layout),
          and the aside's overflow:hidden clips its content as it collapses. */}
      <aside className="cowork-sidebar">
        <div className="cowork-sidebar__scroll">
          <button
            className="sidebar__new"
            onClick={() => {
              if (onNewSession) onNewSession();
              setActivePanel("taskCenter");
            }}
          >
            <SquarePen size={18} />
            <span>{t("cowork.newTask") || "新建任务"}</span>
          </button>

          <section className="sidebar__section sidebar__section--projects" style={{ marginBottom: '8px', minHeight: 0, display: 'flex', flexDirection: 'column' }}>
            {projectTreeNode}
          </section>



          <section className="cowork-sidebar__group" style={{ marginBottom: '0px', marginTop: 'auto' }}>
            <button
              className="cowork-sidebar__item"
              onClick={() => setPreferenceOpen(true)}
            >
              <SlidersHorizontal size={14} />
              <span>{t("cowork.preference") || "办公偏好"}</span>
            </button>
            <button
              className={`cowork-sidebar__item ${activePanel === "calendarTask" ? "cowork-sidebar__item--active" : ""}`}
              onClick={() => {
                setActivePanel("calendarTask");
                if (dockOnClose) dockOnClose();
              }}
            >
              <CalendarDays size={14} />
              <span>{"日历与任务"}</span>
            </button>
            <button
              className={`cowork-sidebar__item ${activePanel === "experts" ? "cowork-sidebar__item--active" : ""}`}
              onClick={() => {
                setActivePanel("experts");
                if (dockOnClose) dockOnClose();
              }}
            >
              <Users size={14} />
              <span>{t("cowork.expert") || "专家团"}</span>
            </button>
            <button
              className={`cowork-sidebar__item ${activePanel === "rag" ? "cowork-sidebar__item--active" : ""}`}
              onClick={() => setActivePanel("rag")}
            >
              <BookOpen size={14} />
              <span>{t("cowork.knowledgeBase") || "知识库"}</span>
            </button>
          </section>
        </div>
      </aside>

      {/* Center: dynamic panel based on selection */}
      <section className="cowork-main">
        {/* Only display session header in active taskCenter (chat) mode */}
        {activePanel === "taskCenter" && (
          <div className="cowork-main__global-topbar" style={{ flexShrink: 0, zIndex: 10 }}>
            {headerNode}
          </div>
        )}

        {/* taskCenter stays mounted (hidden when inactive) so an expert-session
            run's live stream (ExpertSessionView inside mainNode) isn't torn down
            when the user peeks at another panel. The backend goroutine survives
            panel switches regardless, but the React subscription (onExpertsCollab)
            lives in ExpertSessionView — unmounting it loses mid-run chunks that
            arrive while the user is away. Keeping it mounted (like ExpertPanel
            below) preserves the stream. Hidden via display:none so it doesn't
            capture layout space. */}
        <div style={{ display: activePanel === "taskCenter" ? "contents" : "none" }}>
          <div className="cowork-main__transcript">{mainNode}</div>
          <div className="cowork-main__composer">{footerNode}</div>
        </div>


        {activePanel === "calendarTask" && (
          <CalendarTaskPanel />
        )}

        {/* ExpertPanel stays mounted (hidden when inactive) so streaming
            conversation state survives panel switches. */}
        <div style={{ display: activePanel === "experts" ? "flex" : "none", flex: 1, minHeight: 0, flexDirection: "column" }}>
          <ExpertPanel />
        </div>

        {activePanel === "rag" && (
          <RagPanel />
        )}
      </section>

      {/* Right dock width resizer — mirrors the coding-mode
         workspace-panel-resizer. Sits in grid column 3 (between main=2 and
         dock=4); dragging updates the shared dock width state. Only rendered
         when the dock is open and the resize handlers are wired. */}
      {rightDockOpen && onDockResizeStart && (
        <button
          className="workspace-panel-resizer"
          type="button"
          role="separator"
          aria-orientation="vertical"
          aria-label={t("rightDock.resize")}
          aria-valuemin={dockMinWidth}
          aria-valuemax={dockMaxAriaWidth}
          aria-valuenow={dockWidth}
          onPointerDown={onDockResizeStart}
          onKeyDown={onDockResizeKey}
          onDoubleClick={onDockResetWidth}
        />
      )}

      {/* Right: tabbed dock (今日 / 邮件 / 文件) or RAG knowledge nav. */}
      {rightDockOpen && (
        <CoworkDock
          cwd={dockCwd}
          maximized={dockMaximized}
          onClose={dockOnClose ?? (() => {})}
          onToggleMaximized={dockOnToggleMaximized ?? (() => {})}
          mode={activePanel === "rag" ? "rag" : "default"}
          onEntityClick={(name) => {
            // Dispatch event so the graph can highlight the node.
            window.dispatchEvent(new CustomEvent("rag:highlight-node", { detail: { name } }));
          }}
          onFileClick={(path) => {
            // Dispatch event so the main panel can open the file.
            window.dispatchEvent(new CustomEvent("rag:open-file", { detail: { path } }));
          }}
        />
      )}

      {preferenceOpen && (
        <PreferencePanel onClose={() => setPreferenceOpen(false)} />
      )}
    </div>
  );
}
