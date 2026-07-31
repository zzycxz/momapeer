import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";
import { AlertTriangle, HelpCircle } from "lucide-react";

import "../styles.css"; // ensure rag-create-overlay/modal classes are present

// confirm.tsx provides a global, app-styled confirmation dialog that replaces
// the jarring native window.confirm() everywhere. It mirrors the ToastProvider
// pattern: a Context + Provider mounted at the root, and a useConfirm() hook
// returning an async `confirm(opts) => Promise<boolean>`. Call sites stay nearly
// identical to the old code: `if (!await confirm({...})) return;` instead of
// `if (!window.confirm(...)) return;` — no per-component state lifting needed.
//
// The modal reuses the rag-create-overlay/rag-create-modal look (same as
// ConfirmModal / ImportModal) for visual consistency, with a danger variant
// (red, for deletes) and an info variant (blue, for cost-confirm prompts).

interface ConfirmOptions {
  title: string;
  message: string;
  confirmLabel?: string; // default "确认"
  cancelLabel?: string;  // default "取消"
  danger?: boolean;      // default true (red); false → blue info icon
}

type ConfirmFn = (opts: ConfirmOptions) => Promise<boolean>;

const ConfirmContext = createContext<ConfirmFn>(() => Promise.resolve(false));

export function useConfirm(): ConfirmFn {
  return useContext(ConfirmContext);
}

interface PendingConfirm extends ConfirmOptions {
  resolve: (ok: boolean) => void;
}

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<PendingConfirm | null>(null);
  // Guard against a second confirm() call while one is already open: queue is
  // overkill for this UI — just resolve the new one as "cancelled" so the caller
  // no-ops rather than dropping the in-flight dialog.
  const openRef = useRef(false);

  const confirm = useCallback<ConfirmFn>((opts) => {
    if (openRef.current) {
      // A dialog is already showing; reject the second to avoid stacking.
      return Promise.resolve(false);
    }
    return new Promise<boolean>((resolve) => {
      openRef.current = true;
      setPending({ ...opts, resolve });
    });
  }, []);

  const close = useCallback((ok: boolean) => {
    setPending((cur) => {
      if (cur) cur.resolve(ok);
      openRef.current = false;
      return null;
    });
  }, []);

  const danger = pending?.danger ?? true;

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {pending && (
        <div
          className="rag-create-overlay"
          onClick={() => close(false)}
          style={{ zIndex: 10000 }}
        >
          <div
            className="rag-create-modal"
            onClick={(e) => e.stopPropagation()}
            style={{ width: 380, maxWidth: "90vw", borderRadius: 14, overflow: "hidden" }}
          >
            <div className="rag-create-modal__body" style={{ padding: "20px 20px 16px", gap: 14 }}>
              <div style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
                <div
                  style={{
                    flex: "0 0 auto",
                    width: 32,
                    height: 32,
                    borderRadius: 8,
                    background: danger ? "rgba(239, 68, 68, 0.12)" : "rgba(59, 130, 246, 0.12)",
                    color: danger ? "#ef4444" : "#3b82f6",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                  }}
                >
                  {danger ? <AlertTriangle size={17} /> : <HelpCircle size={17} />}
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <h3 style={{ margin: 0, fontSize: 14.5, fontWeight: 600, color: "var(--fg)" }}>{pending.title}</h3>
                  <p style={{ margin: "4px 0 0", fontSize: 12.5, lineHeight: 1.5, color: "var(--fg-dim)", whiteSpace: "pre-wrap" }}>{pending.message}</p>
                </div>
              </div>
              <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginTop: 2 }}>
                <button
                  type="button"
                  className="btn btn--small"
                  onClick={() => close(false)}
                  style={{ fontSize: 12.5, padding: "7px 14px", borderRadius: 8 }}
                >
                  {pending.cancelLabel ?? "取消"}
                </button>
                <button
                  type="button"
                  className={danger ? "btn btn--small btn--danger" : "btn btn--small"}
                  onClick={() => close(true)}
                  style={{ fontSize: 12.5, padding: "7px 14px", borderRadius: 8, fontWeight: 600 }}
                >
                  {pending.confirmLabel ?? "确认"}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </ConfirmContext.Provider>
  );
}
