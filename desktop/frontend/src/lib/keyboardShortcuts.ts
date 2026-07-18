import { useEffect } from "react";

export interface KeyboardShortcut {
  key: string;
  ctrl?: boolean;
  shift?: boolean;
  alt?: boolean;
  description: string;
  action: () => void;
}

export function registerShortcut(_shortcut: KeyboardShortcut) {
  // Future: register global keyboard shortcuts
}

export function unregisterShortcut(_key: string) {
  // Future: unregister global keyboard shortcuts
}

export function useGlobalShortcut(_key: string, _callback: () => void, _deps?: unknown[]) {
  // Future: register global keyboard shortcut
  useEffect(() => {}, []);
}
