// SidebarFooter renders the four always-available controls at the bottom bar
// of the window (the global-bottom-bar that spans under the sidebar + chat +
// workspace). It is shared by both the coding sidebar (App.tsx) and the cowork
// sidebar (CoWorkLayout.tsx) so the office mode gets the same entry points.
//
// The footer uses footer__nav / footer__navitem classes (NOT sidebar__nav) so
// the bottom-bar-scoped CSS in styles.css applies: 28x28 icon-only buttons with
// hover backgrounds, IM dot in the top-right corner, tooltips opening upward
// (side="top") since the bar is at the window bottom.
//
// Keeping this as a standalone component means a single source of truth for the
// icons, labels, tooltips, and ordering across both product profiles.

import { History, MessageSquare, Settings as SettingsIcon, Trash2 } from "lucide-react";

import { useT } from "../lib/i18n";
import { Tooltip } from "./Tooltip";

export interface SidebarFooterProps {
  imConnectionCount: number; // 0 = show "no IM configured" hint
  imOnline: boolean;
  tooltipDisabled: boolean;
  onOpenIm: () => void;
  onOpenHistory: () => void;
  onOpenTrash: () => void;
  onOpenSettings: () => void;
}

export function SidebarFooter({
  imConnectionCount,
  imOnline,
  tooltipDisabled,
  onOpenIm,
  onOpenHistory,
  onOpenTrash,
  onOpenSettings,
}: SidebarFooterProps) {
  const t = useT();
  return (
    <nav className="footer__nav">
      <Tooltip
        label={imConnectionCount === 0 ? t("sidebar.imEmpty") : t("sidebar.im")}
        fill
        side="top"
        disabled={tooltipDisabled}
      >
        <button
          className="footer__navitem footer__navitem--im"
          type="button"
          onClick={() => void onOpenIm()}
        >
          <MessageSquare size={16} />
          {imOnline && <span className="sidebar-im-dot" />}
        </button>
      </Tooltip>
      <Tooltip label={t("sidebar.allHistory")} fill side="top" disabled={tooltipDisabled}>
        <button
          className="footer__navitem"
          type="button"
          onClick={() => void onOpenHistory()}
        >
          <History size={16} />
        </button>
      </Tooltip>
      <Tooltip label={t("sidebar.trash")} fill side="top" disabled={tooltipDisabled}>
        <button
          className="footer__navitem"
          type="button"
          onClick={() => void onOpenTrash()}
        >
          <Trash2 size={16} />
        </button>
      </Tooltip>
      <Tooltip label={t("topbar.settings")} fill side="top" disabled={tooltipDisabled}>
        <button
          className="footer__navitem"
          type="button"
          onClick={() => void onOpenSettings()}
        >
          <SettingsIcon size={16} />
        </button>
      </Tooltip>
    </nav>
  );
}
