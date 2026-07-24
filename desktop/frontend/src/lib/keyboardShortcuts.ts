import { useEffect } from "react";

export interface KeyboardShortcut {
  key: string;
  ctrl?: boolean;
  shift?: boolean;
  alt?: boolean;
  description: string;
  action: () => void;
}

// ShortcutCombo represents a parsed key combination.
export interface ShortcutCombo {
  key: string;
  ctrl?: boolean;
  shift?: boolean;
  alt?: boolean;
  meta?: boolean;
}

// ShortcutPlatform determines display style (macOS uses symbols, others use names).
export type ShortcutPlatform = "darwin" | "win32" | "linux";

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

// formatShortcutCombo renders a combo as a human-readable string.
export function formatShortcutCombo(combo: ShortcutCombo, platform: ShortcutPlatform): string {
  const parts = formatShortcutComboParts(combo, platform);
  return platform === "darwin" ? parts.join("") : parts.join("+");
}

// formatShortcutComboParts returns individual key-cap segments.
export function formatShortcutComboParts(combo: ShortcutCombo, platform: ShortcutPlatform): string[] {
  const parts: string[] = [];
  const isDarwin = platform === "darwin";
  if (combo.ctrl) parts.push(isDarwin ? "⌃" : "Ctrl");
  if (combo.alt) parts.push(isDarwin ? "⌥" : "Alt");
  if (combo.shift) parts.push(isDarwin ? "⇧" : "Shift");
  if (combo.meta) parts.push(isDarwin ? "⌘" : "Win");
  parts.push(combo.key.length === 1 ? combo.key.toUpperCase() : combo.key);
  return parts;
}
