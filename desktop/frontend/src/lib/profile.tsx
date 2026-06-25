import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";

import { app, onProfileChanged } from "./bridge";

// A product profile switches the whole app between product modes: "dev" (coding)
// and "cowork" (office). It is per-tab — switching rebuilds that tab's controller
// with the profile's model/prompt/skill/plugin bundle (see app.SwitchProfileForTab
// in desktop/app.go). This provider tracks the ACTIVE tab's profile so the layout
// can branch on it, and exposes switchProfile to trigger a rebuild.
//
// The active-tab contract: app.Profile() returns the current tab's profile, and
// the "profile:changed" event fires with {tabId, profile}. We refresh whenever
// either the event fires OR the active tab changes (the latter is signalled by
// App re-mounting this provider under the active-tab subtree — see App.tsx). To
// stay simple and robust we also poll-free re-fetch on tab-switch via a key prop.

export type ProfileName = "dev" | "cowork";

export interface ProfileContextValue {
  profile: ProfileName;
  // switchProfile triggers a controller rebuild on the active tab and updates
  // local state optimistically; the backend's profile:changed event confirms it.
  switchProfile: (name: ProfileName) => Promise<void>;
  // isCowork is the common layout branch.
  isCowork: boolean;
}

const ProfileContext = createContext<ProfileContextValue | null>(null);

function normalizeProfile(name: string | undefined | null): ProfileName {
  const n = (name ?? "").trim().toLowerCase();
  return n === "cowork" ? "cowork" : "dev";
}

export function ProfileProvider({ children }: { children: ReactNode }) {
  const [profile, setProfile] = useState<ProfileName>("dev");

  // Fetch the active tab's profile once on mount.
  useEffect(() => {
    let cancelled = false;
    app.Profile()
      .then((name) => {
        if (!cancelled) setProfile(normalizeProfile(name));
      })
      .catch(() => {
        /* dev mock or backend not ready yet — stay on default */
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Listen for profile changes emitted by the backend after a SwitchProfile
  // rebuild. The event is per-tab; we adopt it as the active profile (the
  // backend only emits for the tab the user switched, which is the active one).
  useEffect(() => {
    return onProfileChanged((e) => {
      setProfile(normalizeProfile(e.profile));
    });
  }, []);

  const switchProfile = useCallback(async (name: ProfileName) => {
    // Optimistic: update local state immediately so the layout swaps without
    // waiting for the rebuild (the event will confirm or correct). If the rebuild
    // fails, the backend stays on the old profile and the next Profile() fetch
    // (e.g. on tab switch) corrects the display.
    setProfile(name);
    try {
      await app.SwitchProfile(name);
    } catch (err) {
      // Revert on error — the backend did not switch.
      const current = await app.Profile().catch(() => "dev");
      setProfile(normalizeProfile(current));
      throw err;
    }
  }, []);

  return (
    <ProfileContext.Provider value={{ profile, switchProfile, isCowork: profile === "cowork" }}>
      {children}
    </ProfileContext.Provider>
  );
}

export function useProfile(): ProfileContextValue {
  const ctx = useContext(ProfileContext);
  if (!ctx) throw new Error("useProfile must be used within a ProfileProvider");
  return ctx;
}
