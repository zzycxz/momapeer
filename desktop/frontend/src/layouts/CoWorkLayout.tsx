import { useState, type ReactNode } from "react";
import { BookOpen, CalendarClock, Settings, SquarePen, Users } from "lucide-react";

import { useT } from "../lib/i18n";
import { app } from "../lib/bridge";
import { RagPanel } from "../components/cowork/RagPanel";
import { ExpertPanel } from "../components/cowork/ExpertPanel";
import { PreferencePanel } from "../components/cowork/PreferencePanel";
import { CalendarTaskPanel } from "../components/cowork/CalendarTaskPanel";
import { CoworkDock } from "../components/cowork/CoworkDock";

// CoWorkPanel enumerates the four left-dock tabs in cowork mode:
//   - preference: 办公偏好 — inline editor for the active mode's portrait
//     (cowork.md / dev.md). Always injected into the prompt so the AI knows
//     the user's working style.
//   - calendarTask: 日历与任务 — calendar grid + scheduled-task list,
//     merged into one panel.
//   - experts: 专家团 — multi-expert collaboration (parallel/debate/pipeline).
//   - rag: 知识库 — knowledge base import/search/extract.
export type CoWorkPanel = "preference" | "calendarTask" | "experts" | "rag";

export function CoWorkLayout({
  mainNode,
  footerNode,
  projectTreeNode,
  rightDockOpen = false,
  sidebarCollapsed = false,
  sessionActions,
}: {
  mainNode?: ReactNode;
  footerNode?: ReactNode;
  projectTreeNode?: ReactNode;
  rightDockOpen?: boolean;
  sidebarCollapsed?: boolean;
  sessionActions?: ReactNode;
}) {
  const t = useT();
  const [activePanel, setActivePanel] = useState<CoWorkPanel>("preference");

  return (
    <div className={`cowork-layout ${sidebarCollapsed ? "cowork-layout--sidebar-collapsed" : ""}`}>
      {/* Left: workspace / knowledge / scheduled / skills */}
      {!sidebarCollapsed && (
        <aside className="cowork-sidebar">
        <div className="cowork-sidebar__scroll">
          <button
            className="sidebar__new"
            onClick={() => setActivePanel("preference")}
          >
            <SquarePen size={18} />
            <span>{t("cowork.newTask") || "新建任务"}</span>
          </button>

          <section className="sidebar__section sidebar__section--projects" style={{ marginBottom: '16px', flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
            {projectTreeNode}
          </section>

          <section className="cowork-sidebar__group" style={{ marginBottom: '8px' }}>
            <button
              className={`cowork-sidebar__item ${activePanel === "preference" ? "cowork-sidebar__item--active" : ""}`}
              onClick={() => setActivePanel("preference")}
            >
              <Settings size={14} />
              <span>{t("cowork.preference") || "办公偏好"}</span>
            </button>
          </section>

          <section className="cowork-sidebar__group" style={{ marginBottom: '8px' }}>
            <button
              className={`cowork-sidebar__item ${activePanel === "calendarTask" ? "cowork-sidebar__item--active" : ""}`}
              onClick={() => setActivePanel("calendarTask")}
            >
              <CalendarClock size={14} />
              <span>{"日历与任务"}</span>
            </button>
          </section>

          <section className="cowork-sidebar__group" style={{ marginBottom: '8px' }}>
            <button
              className={`cowork-sidebar__item ${activePanel === "experts" ? "cowork-sidebar__item--active" : ""}`}
              onClick={() => setActivePanel("experts")}
            >
              <Users size={14} />
              <span>{t("cowork.expert") || "专家团"}</span>
            </button>
          </section>

          <section className="cowork-sidebar__group" style={{ marginBottom: '8px' }}>
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
      )}

      {/* Center: dynamic panel based on selection.
          The preference/calendarTask/experts/rag panels own their full
          surface area; only preference shows the chat transcript below the
          editor so the user can talk to the AI while tweaking the portrait. */}
      <section className="cowork-main">
        {activePanel === "preference" && (
          <>
            <header className="cowork-main__header">
              <h2>{t("cowork.preference") || "办公偏好"}</h2>
              <div className="cowork-main__header-actions">
                {sessionActions}
              </div>
            </header>
            <div style={{ padding: "0 16px", overflow: "auto", flex: "0 0 auto", maxHeight: "40%" }}>
              <PreferencePanel />
            </div>
            <div className="cowork-main__transcript" style={{ flex: 1 }}>{mainNode}</div>
            <div className="cowork-main__composer">{footerNode}</div>
          </>
        )}

        {activePanel === "calendarTask" && (
          <CalendarTaskPanel />
        )}

        {activePanel === "experts" && (
          <ExpertPanel />
        )}

        {activePanel === "rag" && (
          <RagPanel />
        )}
      </section>

      {/* Right: dock — today/mail/files overview */}
      {rightDockOpen && (
        <CoworkDock
          maximized={false}
          onClose={() => {}}
          onToggleMaximized={() => {}}
        />
      )}
    </div>
  );
}
