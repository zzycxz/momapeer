// PreferencePanel is the workspace "偏好" panel: an inline editor for the
// active mode's portrait file (cowork.md under cowork, dev.md under dev). It is
// mounted in BOTH layouts at the same sidebar position, so the user edits the
// portrait that is actually injected in the current mode — dev sees coding
// preferences, cowork sees office preferences.
//
// The portrait is the small, always-injected core of memory (user.md +
// memory.md + <mode>.md). Only the <mode>.md part is user-editable here; the
// shared files are dream-maintained. Saves go through SaveDoc (the profile path
// is whitelisted in the backend), which reloads memory so the next turn injects
// the updated portrait.

import { useCallback, useEffect, useState } from "react";
import { Save, X } from "lucide-react";

import { app } from "../../lib/bridge";
import { useT } from "../../lib/i18n";
import { useToast } from "../../lib/toast";

// profileMaxChars mirrors the backend hard cap (memory.profileMaxChars). Kept as
// a soft target in the UI — the backend truncates regardless, but we warn before
// the user saves something that will be clipped.
const profileMaxChars = 300;

export function PreferencePanel({ 
  title, 
  hint,
  onClose
}: { 
  title?: string;
  hint?: string;
  onClose?: () => void;
}) {
  const t = useT();
  const { showToast } = useToast();
  const [path, setPath] = useState("");
  const [content, setContent] = useState("");
  const [original, setOriginal] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const pv = await app.PortraitProfile();
      setPath(pv.path);
      setContent(pv.content);
      setOriginal(pv.content);
    } catch {
      setPath("");
      setContent("");
      setOriginal("");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void reload(); }, [reload]);

  const dirty = content !== original;
  const overBudget = content.length > profileMaxChars;

  const save = useCallback(async () => {
    if (!dirty || saving || !path) return;
    setSaving(true);
    try {
      await app.SaveDoc(path, content);
      setOriginal(content);
      showToast(t("preference.saved"));
    } catch (err) {
      showToast(String((err as Error)?.message ?? err));
    } finally {
      setSaving(false);
    }
  }, [dirty, saving, path, content, showToast, t]);

  if (loading) {
    return (
      <div className="management-modal-backdrop" onClick={onClose}>
        <div className="management-modal history-modal" onClick={e => e.stopPropagation()}>
          <div className="preference-panel"><div className="empty">{t("common.loading")}</div></div>
        </div>
      </div>
    );
  }

  return (
    <div className="management-modal-backdrop" onClick={onClose}>
      <div className="management-modal history-modal" style={{ width: 640, maxWidth: "90vw", height: 560, display: "flex", flexDirection: "column", overflow: "hidden" }} onClick={e => e.stopPropagation()}>
        <div className="preference-panel" style={{ flex: 1, height: "100%", display: "flex", flexDirection: "column" }}>
          <header className="preference-panel__head">
            <div>
              <h2 className="preference-panel__title">{title || t("cowork.preference") || "办公偏好"}</h2>
              <p className="preference-panel__hint">{hint || t("preference.hint")}</p>
            </div>
            <div style={{ display: "flex", gap: "8px", alignItems: "center" }}>
              <button
                className="btn btn--primary btn--small"
                onClick={() => void save()}
                disabled={!dirty || saving || overBudget}
                type="button"
              >
                <Save size={13} />
                {t("common.save")}
              </button>
              {onClose && (
                <button className="btn btn--icon" onClick={onClose} type="button" aria-label={t("common.close") || "关闭"}>
                  <X size={16} />
                </button>
              )}
            </div>
          </header>

          <div className="preference-panel__editor" style={{ flex: 1, display: "flex", flexDirection: "column" }}>
            <textarea
              className="preference-panel__textarea"
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder={t("preference.placeholder")}
              spellCheck={false}
              style={{ flex: 1, resize: "none" }}
            />
            <div className="preference-panel__meta">
              <span className={overBudget ? "preference-panel__count preference-panel__count--over" : "preference-panel__count"}>
                {content.length} / {profileMaxChars}
              </span>
              {overBudget && (
                <span className="preference-panel__warn">{t("preference.tooLong")}</span>
              )}
              {path && <span className="preference-panel__path">{path}</span>}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
