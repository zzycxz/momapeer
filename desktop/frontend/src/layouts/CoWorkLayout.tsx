import { useState, type ReactNode } from "react";
import { BookOpen, Clock, Inbox, SquarePen, Users } from "lucide-react";

import { useT } from "../lib/i18n";
import { app } from "../lib/bridge";
import { AutomationPanel } from "../components/cowork/AutomationPanel";
import { RagPanel } from "../components/cowork/RagPanel";
import { ExpertPanel } from "../components/cowork/ExpertPanel";
import { CoworkDock } from "../components/cowork/CoworkDock";

export type CoWorkPanel = "taskCenter" | "experts" | "automation" | "rag";

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
  const [analyzing, setAnalyzing] = useState(false);
  const [analysis, setAnalysis] = useState<string>("");
  const [activePanel, setActivePanel] = useState<CoWorkPanel>("taskCenter");

  const screenshotAnalyze = async () => {
    setAnalyzing(true);
    setAnalysis("");
    try {
      await app.Submit("分析当前屏幕内容并总结");
    } catch {
      setAnalysis(t("cowork.screenshotFailed"));
    } finally {
      setAnalyzing(false);
    }
  };

  return (
    <div className={`cowork-layout ${sidebarCollapsed ? "cowork-layout--sidebar-collapsed" : ""}`}>
      {/* Left: workspace / knowledge / scheduled / skills */}
      {!sidebarCollapsed && (
        <aside className="cowork-sidebar">
        <div className="cowork-sidebar__scroll">
          <button
            className="sidebar__new"
            onClick={() => setActivePanel("taskCenter")}
          >
            <SquarePen size={18} />
            <span>{t("cowork.newTask") || "新建任务"}</span>
          </button>

          <section className="sidebar__section sidebar__section--projects" style={{ marginBottom: '16px', flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
            {projectTreeNode}
          </section>

          <section className="cowork-sidebar__group" style={{ marginBottom: '8px' }}>
            <button
              className={`cowork-sidebar__item ${activePanel === "taskCenter" ? "cowork-sidebar__item--active" : ""}`}
              onClick={() => setActivePanel("taskCenter")}
            >
              <Inbox size={14} />
              <span>{t("cowork.taskCenter") || "助理"}</span>
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
              className={`cowork-sidebar__item ${activePanel === "automation" ? "cowork-sidebar__item--active" : ""}`}
              onClick={() => setActivePanel("automation")}
            >
              <Clock size={14} />
              <span>{t("cowork.scheduled") || "自动化"}</span>
            </button>
          </section>

          <section className="cowork-sidebar__group" style={{ marginBottom: '8px' }}>
            <button
              className={`cowork-sidebar__item ${activePanel === "rag" ? "cowork-sidebar__item--active" : ""}`}
              onClick={() => setActivePanel("rag")}
            >
              <BookOpen size={14} />
              <span>{t("cowork.knowledgeBase") || "资料库"}</span>
            </button>
          </section>
        </div>
      </aside>
      )}

      {/* Center: dynamic panel based on selection */}
      <section className="cowork-main">
        {activePanel === "taskCenter" && (
          <>
            <header className="cowork-main__header">
              <h2>{t("cowork.taskCenter") || "助理任务看板"}</h2>
              <div className="cowork-main__header-actions">
                <button
                  className="btn btn--primary btn--small"
                  onClick={() => void screenshotAnalyze()}
                  disabled={analyzing}
                  title={t("cowork.screenshotHint")}
                >
                  {analyzing ? t("cowork.analyzing") : t("cowork.screenshotAnalyze")}
                </button>
                {sessionActions}
              </div>
            </header>
            {analysis && <div className="cowork-main__analysis">{analysis}</div>}
            <div className="cowork-main__transcript">{mainNode}</div>
            <div className="cowork-main__composer">{footerNode}</div>
          </>
        )}
        
        {activePanel === "experts" && (
          <ExpertPanel />
        )}

        {activePanel === "automation" && (
          <AutomationPanel />
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
