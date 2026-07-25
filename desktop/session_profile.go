package main

import (
	"strings"

	"github.com/zzycxz/momapeer/internal/config"
)

// desktopSessionDirFor returns the session directory for a workspace root
// partitioned by profile. The default profile (empty / "dev") falls back to the
// un-profiled per-project session dir (backward compatible with
// desktopSessionDir). A named profile (e.g. "cowork") routes to
// <userDir>/projects/<slug>/<profileKey>/sessions so conversations stay in their
// own partition. When workspaceRoot is empty, the global profile session dir is
// used instead. Used by tabSessionDir so a tab's pre-build path already lands
// in the right profile partition.
func desktopSessionDirFor(workspaceRoot, profile string) string {
	root := strings.TrimSpace(workspaceRoot)
	key := config.ProfileNameKey(profile)
	if root != "" {
		if dir := config.ProjectSessionDirFor(root, key); dir != "" {
			return dir
		}
	}
	if key != "" && key != config.ProfileDev && key != "default" {
		if dir := config.SessionDirFor(key); dir != "" {
			return dir
		}
	}
	return desktopSessionDir(root)
}

// activeProfileKey returns the profile key of the currently active tab, or the
// default ("dev") when no tab is active or the active tab has no profile. The
// returned value is normalized via config.ProfileNameKey so it is safe to use
// directly as a projects-file path segment.
func (a *App) activeProfileKey() string {
	a.mu.RLock()
	tab := a.activeTabLocked()
	a.mu.RUnlock()
	if tab == nil {
		return config.ProfileDev
	}
	key := config.ProfileNameKey(tab.profile)
	if key == "" {
		return config.ProfileDev
	}
	return key
}

// activeProfileKeyRaw returns the active tab's profile string verbatim (no
// normalization, no defaulting). Used where the caller needs to preserve an
// empty profile to indicate "inherit the default" rather than forcing dev —
// e.g. CreateTopic on a new workspace should match whatever profile the tab is
// actually in, including the un-profiled default.
func (a *App) activeProfileKeyRaw() string {
	a.mu.RLock()
	tab := a.activeTabLocked()
	a.mu.RUnlock()
	if tab == nil {
		return ""
	}
	return strings.TrimSpace(tab.profile)
}

// activeProfileKeyLocked is the lock-free variant of activeProfileKey for use
// when the caller already holds a.mu (Lock or RLock). Calling activeProfileKey()
// from within a held lock causes a deadlock (RLock blocks on held Lock).
func (a *App) activeProfileKeyLocked() string {
	tab := a.activeTabLocked()
	if tab == nil {
		return config.ProfileDev
	}
	key := config.ProfileNameKey(tab.profile)
	if key == "" {
		return config.ProfileDev
	}
	return key
}
