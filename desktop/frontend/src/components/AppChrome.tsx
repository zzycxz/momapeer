import { Minus, PanelLeft, PanelRight, Search, Square, X } from "lucide-react";
import { TabBar } from "./TabBar";
import type { TabMeta } from "../lib/types";
import { useT } from "../lib/i18n";

type DesktopPlatform = "darwin" | "windows" | "linux";

interface AppChromeProps {
  platform: DesktopPlatform;
  browserPreviewChrome: boolean;
  tabs: TabMeta[];
  activeTabId?: string;
  revealActiveSignal: number;
  commandCompact: boolean;
  sidebarTogglePressed: boolean;
  sidebarExpandBlocked: boolean;
  sidebarCollapsed: boolean;
  sidebarToggleTitle: string;
  workspacePanelMaximized: boolean;
  workspacePanelRenderable: boolean;
  workspaceTogglePressed: boolean;
  workspacePanelLabel: string;
  onToggleSidebar: () => void;
  onToggleWorkspacePanel: () => void;
  onTabChange: (tabId: string) => void;
  onTabClose: (tabId: string) => void;
  onTabsClose: (tabIds: string[], nextActiveTabId?: string) => void;
  onTabsReorder: (tabIds: string[]) => void;
  onNewTab: () => void;
  onOpenPalette: () => void;
  // Product profile (dev/cowork). profile is the active tab's mode; onSwitchProfile
  // rebuilds the controller with the new profile's bundle.
  profile: string;
  onSwitchProfile: (name: string) => void;
}

export function AppChrome({
  platform,
  browserPreviewChrome,
  tabs,
  activeTabId,
  revealActiveSignal,
  commandCompact,
  sidebarTogglePressed,
  sidebarExpandBlocked,
  sidebarCollapsed,
  sidebarToggleTitle,
  workspacePanelMaximized,
  workspacePanelRenderable,
  workspaceTogglePressed,
  workspacePanelLabel,
  onToggleSidebar,
  onToggleWorkspacePanel,
  onTabChange,
  onTabClose,
  onTabsClose,
  onTabsReorder,
  onNewTab,
  onOpenPalette,
  profile,
  onSwitchProfile,
}: AppChromeProps) {
  const t = useT();
  const darwinChrome = platform === "darwin";
  const detachCommand = !darwinChrome;
  const showWindowsPreviewControls = browserPreviewChrome && platform === "windows";
  const chromeClassName = [
    "app-chrome",
    "app-chrome--tabs",
    darwinChrome ? "app-chrome--darwin-tabs" : "app-chrome--native-tabs",
    !darwinChrome ? "app-chrome--identityless" : "",
    showWindowsPreviewControls ? "app-chrome--preview-window-controls" : "",
    `app-chrome--platform-${platform}`,
  ].filter(Boolean).join(" ");

  const tabBar = (
    <TabBar
      tabs={tabs}
      activeTabId={activeTabId}
      revealActiveSignal={revealActiveSignal}
      onTabChange={onTabChange}
      onTabClose={onTabClose}
      onTabsClose={onTabsClose}
      onTabsReorder={onTabsReorder}
      onNewTab={onNewTab}
      onOpenPalette={detachCommand ? undefined : onOpenPalette}
      commandCompact={commandCompact}
    />
  );

  return (
    <header className={chromeClassName}>
      {browserPreviewChrome && darwinChrome && (
        <div className="app-chrome__traffic" aria-hidden="true">
          <span />
          <span />
          <span />
        </div>
      )}
      {darwinChrome && <span className="app-chrome__drag-rail" aria-hidden="true" />}
      <button
        className={[
          "app-chrome__panel-toggle",
          "app-chrome__panel-toggle--left",
          sidebarTogglePressed ? "app-chrome__panel-toggle--pressed" : "",
          sidebarExpandBlocked ? "app-chrome__panel-toggle--blocked" : "",
        ].filter(Boolean).join(" ")}
        type="button"
        onClick={sidebarExpandBlocked ? undefined : onToggleSidebar}
        aria-label={sidebarToggleTitle}
        aria-pressed={!sidebarCollapsed}
        aria-disabled={sidebarExpandBlocked}
      >
        <PanelLeft size={16} />
      </button>

      {darwinChrome ? (
        <div className="app-chrome__tab-strip app-chrome__tab-strip--darwin">
          {tabBar}
        </div>
      ) : (
        <>
          <div className="app-chrome__tab-strip app-chrome__tab-strip--native">
            {tabBar}
          </div>
          {detachCommand && (
            <div
              className={[
                "app-chrome__tools",
                workspaceTogglePressed ? "app-chrome__tools--workspace-pressed" : "",
              ].filter(Boolean).join(" ")}
              aria-label={t("tabBar.commandSearch")}
            >
              <button
                className="tabbar__command tabbar__command--compact app-chrome__command"
                type="button"
                onClick={onOpenPalette}
                aria-label={t("palette.placeholder")}
              >
                <Search size={13} className="tabbar__command-icon" />
                <span className="tabbar__command-text tabbar__command-text--full">{t("tabBar.commandSearch")}</span>
                <span className="tabbar__command-text tabbar__command-text--compact">{t("tabBar.commandSearchCompact")}</span>
                <kbd className="tabbar__command-kbd">Ctrl+K</kbd>
              </button>
            </div>
          )}
        </>
      )}

      {!workspacePanelMaximized && (
        <button
          className={[
            "app-chrome__panel-toggle",
            "app-chrome__panel-toggle--right",
            workspacePanelRenderable ? "app-chrome__panel-toggle--active" : "",
            workspaceTogglePressed ? "app-chrome__panel-toggle--pressed" : "",
          ].filter(Boolean).join(" ")}
          type="button"
          onClick={onToggleWorkspacePanel}
          aria-label={workspacePanelLabel}
          aria-pressed={workspacePanelRenderable}
        >
          <PanelRight size={16} />
        </button>
      )}
      {/* Profile switcher: toggles between dev (coding) and cowork (office).
          Button shows the TARGET mode (what you'll switch TO), not the current mode.
          A dot indicator marks the current active mode. */}
      <button
        className={[
          "app-chrome__profile-toggle",
          profile.toLowerCase() === "cowork" ? "app-chrome__profile-toggle--cowork" : "",
        ].filter(Boolean).join(" ")}
        type="button"
        onClick={() => onSwitchProfile(profile.toLowerCase() === "cowork" ? "dev" : "cowork")}
        aria-label={profile.toLowerCase() === "cowork" ? t("cowork.switchToDev") : t("cowork.switchToCoWork")}
        title={profile.toLowerCase() === "cowork" ? t("cowork.switchToDev") : t("cowork.switchToCoWork")}
      >
        <span className="app-chrome__profile-dot" aria-hidden="true" />
        <span className="app-chrome__profile-label">
          {profile.toLowerCase() === "cowork" ? t("cowork.badgeCoWork") : t("cowork.badgeDev")}
        </span>
      </button>
      {showWindowsPreviewControls && (
        <div className="app-chrome__window-controls app-chrome__window-controls--windows" aria-hidden="true">
          <span className="app-chrome__window-control app-chrome__window-control--minimize">
            <Minus size={12} strokeWidth={1.9} />
          </span>
          <span className="app-chrome__window-control app-chrome__window-control--maximize">
            <Square size={10} strokeWidth={1.8} />
          </span>
          <span className="app-chrome__window-control app-chrome__window-control--close">
            <X size={12} strokeWidth={1.9} />
          </span>
        </div>
      )}
    </header>
  );
}
