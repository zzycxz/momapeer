import { Fragment } from "react";
import {
  formatShortcutCombo,
  formatShortcutComboParts,
  type ShortcutCombo,
  type ShortcutPlatform,
} from "../lib/keyboardShortcuts";

// ShortcutComboDisplay renders a key combo as a styled sequence of key-cap
// segments. On macOS modifiers become symbols (⌘⇧S) with no separators; on
// other platforms they are full names joined by "+". Used both in the
// cheatsheet (as <kbd>) and can be reused inline anywhere a shortcut is shown.
export function ShortcutComboDisplay({
  combo,
  platform,
  as = "span",
  className,
}: {
  combo: ShortcutCombo;
  platform: ShortcutPlatform;
  as?: "span" | "kbd";
  className?: string;
}) {
  const Tag = as;
  const label = formatShortcutCombo(combo, platform);
  const parts = formatShortcutComboParts(combo, platform);
  const separator = platform === "darwin" ? null : "+";
  return (
    <Tag className={`shortcut-combo${className ? ` ${className}` : ""}`} aria-label={label}>
      {parts.map((part, index) => (
        <Fragment key={`${part}-${index}`}>
          {index > 0 && separator && (
            <span className="shortcut-combo__separator" aria-hidden="true">
              {separator}
            </span>
          )}
          <span className="shortcut-combo__part">{part}</span>
        </Fragment>
      ))}
    </Tag>
  );
}
