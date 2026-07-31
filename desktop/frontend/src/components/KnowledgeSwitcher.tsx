import { useCallback, useEffect, useRef, useState } from "react";
import { Check, ChevronsUpDown, Library } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { RagCollectionView } from "../lib/types";
import { ANCHORED_POPOVER_CLOSE_MS, AnchoredPopover } from "./AnchoredPopover";

// KnowledgeSwitcher mirrors EffortSwitcher/ModelSwitcher: an AnchoredPopover
// dropdown that lets the user pick which knowledge-base collection (if any)
// gets auto-injected into each message. The first option is always "不使用"
// (scope = "" → no injection, the default); the rest are the user's collections
// (loaded lazily from ListRagCollections when the menu opens). Nested
// collections (path "工作/领导材料") are indented by their parent presence.
export function KnowledgeSwitcher({
  scope,
  disabled = false,
  onPick,
}: {
  scope: string;
  disabled?: boolean;
  onPick: (scope: string) => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [closing, setClosing] = useState(false);
  const [collections, setCollections] = useState<RagCollectionView[]>([]);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const closeTimerRef = useRef<number | null>(null);

  const clearCloseTimer = useCallback(() => {
    if (closeTimerRef.current === null) return;
    window.clearTimeout(closeTimerRef.current);
    closeTimerRef.current = null;
  }, []);

  const openMenu = useCallback(() => {
    clearCloseTimer();
    setClosing(false);
    setOpen(true);
  }, [clearCloseTimer]);

  const closeMenu = useCallback(
    (afterClose?: () => void) => {
      clearCloseTimer();
      setClosing(true);
      window.requestAnimationFrame(() => setOpen(false));
      const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      closeTimerRef.current = window.setTimeout(() => {
        closeTimerRef.current = null;
        setClosing(false);
        afterClose?.();
      }, reduceMotion ? 0 : ANCHORED_POPOVER_CLOSE_MS);
    },
    [clearCloseTimer],
  );

  useEffect(() => () => clearCloseTimer(), [clearCloseTimer]);

  // Lazy-load collections the first time the menu opens (mirrors ModelSwitcher's
  // useEffect keyed on `open`). Avoids a ListRagCollections round-trip on every
  // render when the user never touches the dropdown.
  useEffect(() => {
    if (!open || closing) return;
    let cancelled = false;
    app.ListRagCollections()
      .then((cols) => {
        if (!cancelled) setCollections(cols ?? []);
      })
      .catch(() => {
        if (!cancelled) setCollections([]);
      });
    return () => {
      cancelled = true;
    };
  }, [open, closing]);

  const pick = (next: string) => {
    closeMenu(() => onPick(next));
  };

  // Display label: the selected collection's leaf name, or "知识库" when off.
  // Display label: the selected collection's leaf name, or "知识库" when off.
  // Fall back to a client-side leaf split when collections haven't loaded yet
  // (lazy load fires on first menu open) so the trigger never shows the raw
  // full path like "工作/管理办法" before the server data arrives.
  const label =
    scope === ""
      ? t("composer.knowledgeBase")
      : collections.find((c) => c.path === scope)?.name ?? scope.split("/").pop() ?? scope;

  return (
    <div className="modelsw kbsw">
      <button
        ref={triggerRef}
        type="button"
        disabled={disabled}
        className={`modelsw__trigger kbsw__trigger ${scope !== "" ? "kbsw__trigger--explicit" : ""}`}
        aria-expanded={open && !closing}
        onClick={() => (open || closing ? closeMenu() : openMenu())}
      >
        <Library size={13} className="modelsw__kind" />
        <span className="modelsw__label">{label}</span>
        <ChevronsUpDown size={11} />
      </button>
      <AnchoredPopover
        open={open && !disabled}
        closing={closing}
        anchorRef={triggerRef}
        onClose={() => closeMenu()}
        className="modelsw__menu modelsw__menu--portal kbsw__menu"
        align="end"
      >
        <div role="listbox">
          {/* "不使用" — opt out of injection (the default). Wrapped in the same
              modelsw__copy layout as collection rows so every row shares height
              + baseline; the kbsw__item--off modifier adds a divider beneath. */}
          <button
            type="button"
            role="option"
            aria-selected={scope === ""}
            className={`modelsw__item kbsw__item kbsw__item--off ${scope === "" ? "modelsw__item--current" : ""}`}
            onClick={() => pick("")}
          >
            <span className="modelsw__copy">
              <span className="modelsw__model">{t("composer.knowledgeNone")}</span>
            </span>
            {scope === "" && <Check size={13} className="modelsw__check" />}
          </button>
          {collections.length === 0 ? (
            <div className="modelsw__empty">{t("composer.knowledgeEmpty")}</div>
          ) : (
            collections.map((c) => (
              <button
                key={c.path || c.name}
                type="button"
                role="option"
                aria-selected={c.path === scope}
                title={c.name}
                className={`modelsw__item kbsw__item ${c.path === scope ? "modelsw__item--current" : ""} ${
                  c.parent ? "kbsw__item--nested" : ""
                }`}
                onClick={() => pick(c.path || c.name)}
              >
                <span className="modelsw__copy kbsw__copy">
                  <span className="modelsw__model">{c.name}</span>
                  {c.documents > 0 && (
                    <span className="modelsw__provider" title={`${c.documents}`}>
                      {t("composer.knowledgeDocCount", { count: c.documents })}
                    </span>
                  )}
                </span>
                {c.path === scope && <Check size={13} className="modelsw__check" />}
              </button>
            ))
          )}
        </div>
      </AnchoredPopover>
    </div>
  );
}
