export function ShortcutsCheatsheet({ open, platform, onClose, t }: { open: boolean; platform?: string; onClose: () => void; t?: unknown }) {
  if (!open) return null;
  const translate = t as ((key: string) => string) | undefined;
  return (
    <div className="shortcuts-cheatsheet">
      <h3>{translate?.("shortcuts.title") || "Keyboard Shortcuts"}</h3>
      <p>Platform: {platform || "unknown"}</p>
      <button onClick={onClose}>{translate?.("common.close") || "Close"}</button>
    </div>
  );
}
