import { useEffect, useState } from "react";
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
      {/* Profile segmented switcher: a pill control with a sliding highlight
          indicator. See ProfileSegmented below. */}
      <ProfileSegmented profile={profile} onSwitchProfile={onSwitchProfile} t={t} />
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

/* Profile segmented switcher: a pill control with a sliding highlight indicator.
   Config-driven — to add a third entry (e.g. "academic"), push a segment to the
   array and the indicator auto-splits to fit (width = 100%/N via --seg-count);
   the slider tracks activeIdx with translateX(idx * 100%). No CSS/JS change.

   Visual feedback: keeps a LOCAL optimistic active index so clicking a segment
   instantly moves the highlight and flips it to the accent color, without
   waiting for the backend's profile:changed event to round-trip. An effect
   re-syncs the local index whenever the `profile` prop changes (e.g. when the
   switch lands, or when the active tab changes elsewhere), so the indicator
   never desyncs from the real state. */
const PROFILE_SEGMENTS = [
  { key: "dev", labelKey: "cowork.badgeDev", titleKey: "cowork.switchToDev" },
  { key: "cowork", labelKey: "cowork.badgeCoWork", titleKey: "cowork.switchToCoWork" },
] as const;

function ProfileSegmented({
  profile,
  onSwitchProfile,
  t,
}: {
  profile: string;
  onSwitchProfile: (name: string) => void;
  t: (key: never, vars?: Record<string, string | number>) => string;
}) {
  const activeKey = profile.toLowerCase() === "cowork" ? "cowork" : "dev";
  const propIdx = Math.max(
    0,
    PROFILE_SEGMENTS.findIndex((s) => s.key === activeKey),
  );
  // Optimistic local index — updates instantly on click; re-synced from prop.
  const [activeIdx, setActiveIdx] = useState(propIdx);
  useEffect(() => {
    setActiveIdx(propIdx);
  }, [propIdx]);

  return (
    <div
      className="app-chrome__profile-seg"
      role="tablist"
      aria-label="Profile"
      style={{ "--seg-count": PROFILE_SEGMENTS.length } as Record<string, number>}
    >
      <span
        className="app-chrome__profile-seg-indicator"
        style={{ transform: `translateX(${activeIdx * 100}%)` }}
        aria-hidden="true"
      />
      {PROFILE_SEGMENTS.map((seg, i) => (
        <button
          key={seg.key}
          type="button"
          role="tab"
          aria-selected={i === activeIdx}
          className={[
            "app-chrome__profile-seg-item",
            i === activeIdx ? "app-chrome__profile-seg-item--active" : "",
          ].filter(Boolean).join(" ")}
          onClick={() => {
            // Optimistic: move highlight immediately for instant feedback.
            setActiveIdx(i);
            onSwitchProfile(seg.key);
          }}
          title={t(seg.titleKey as never)}
        >
          {t(seg.labelKey as never)}
        </button>
      ))}
    </div>
  );
}
