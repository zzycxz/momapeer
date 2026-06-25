// SidebarFooter renders the four always-available controls at the bottom of
// the sidebar: IM connections, session history, trash, and settings. It is
// shared by both the coding sidebar (App.tsx) and the cowork sidebar
// (CoWorkLayout.tsx) so the office mode gets the same entry points — the only
// thing that differs is which sessions history/trash show, which the caller
// handles by passing a profile-scoped openAllHistory/openTrash.
//
// Keeping this as a standalone component (rather than inlining twice) means a
// single source of truth for the icons, labels, tooltips, and ordering.

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
    <nav className="sidebar__nav">
      <Tooltip
        label={imConnectionCount === 0 ? t("sidebar.imEmpty") : t("sidebar.im")}
        fill
        side="right"
        disabled={tooltipDisabled}
      >
        <button
          className="sidebar__navitem sidebar__navitem--im"
          type="button"
          onClick={() => void onOpenIm()}
        >
          <MessageSquare size={15} />
          <span>{t("sidebar.im")}</span>
          {imOnline && <span className="sidebar-im-dot" />}
        </button>
      </Tooltip>
      <Tooltip label={t("sidebar.allHistory")} fill side="right" disabled={tooltipDisabled}>
        <button
          className="sidebar__navitem"
          onClick={() => void onOpenHistory()}
        >
          <History size={15} />
          <span>{t("sidebar.allHistory")}</span>
        </button>
      </Tooltip>
      <Tooltip label={t("sidebar.trash")} fill side="right" disabled={tooltipDisabled}>
        <button
          className="sidebar__navitem"
          onClick={() => void onOpenTrash()}
        >
          <Trash2 size={15} />
          <span>{t("sidebar.trash")}</span>
        </button>
      </Tooltip>
      <Tooltip label={t("topbar.settings")} fill side="right" disabled={tooltipDisabled}>
        <button
          className="sidebar__navitem"
          onClick={() => void onOpenSettings()}
        >
          <SettingsIcon size={15} />
          <span>{t("topbar.settings")}</span>
        </button>
      </Tooltip>
    </nav>
  );
}
