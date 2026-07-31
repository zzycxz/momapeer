// ExpertCollabCard renders a finished expert-team collaboration as a folded
// card in the main chat transcript. It's the "archive layer" view of an
// expert_collab Item: the full per-round transcript + moderator synthesis are
// kept on disk (and shown here on expand), while the model's context layer
// (Go side) projects it down to a synthesis-only summary before each turn.
//
// Collapsed by default: a one-line header (🤝 team · mode · synthesis preview).
// Expanded: reuses the cowork-expert__* classes from CollabStream so the visual
// structure matches the live panel (rounds → expert turns → synthesis).

import { useState, type MouseEvent } from "react";
import { ChevronRight } from "lucide-react";

import { useT } from "../lib/i18n";
import { useConfirm } from "../lib/confirm";
import type { WireCollab } from "../lib/types";

type CollabItem = { kind: "expert_collab"; id: string; collab: WireCollab };

const MODE_LABEL: Record<string, string> = {
  parallel: "并行",
  debate: "辩论",
  pipeline: "流水线",
};

function modeLabel(mode: string): string {
  return MODE_LABEL[mode] ?? (mode || "协作");
}

export function ExpertCollabCard({ item, onDelete }: { item: CollabItem; onDelete?: (id: string) => void }) {
  const t = useT();
  const confirm = useConfirm();
  const [open, setOpen] = useState(false);
  const c = item.collab;
  const rounds = Array.isArray(c.rounds) ? c.rounds : [];
  const preview = (c.synthesis || "").trim();

  // Cross-navigation to the experts panel (main-chat → experts). Dispatching
  // cowork:open-experts with the team id makes CoWorkLayout switch panels and
  // ExpertPanel select this team.
  const openInExperts = (e: MouseEvent) => {
    e.stopPropagation();
    window.dispatchEvent(new CustomEvent("cowork:open-experts", { detail: { teamId: c.teamId } }));
  };

  // "不采纳": discard this collaboration from the transcript + context. A
  // confirm guard prevents an accidental loss of a long collaboration.
  const handleDelete = async (e: MouseEvent) => {
    e.stopPropagation();
    if (!onDelete) return;
    if (!(await confirm({ title: "删除协作", message: t("cowork.expertCollabDeleteConfirm") }))) return;
    onDelete(item.id);
  };

  return (
    <div className="expert-collab">
      <div
        role="button"
        tabIndex={0}
        className="expert-collab__head"
        onClick={() => setOpen((v) => !v)}
        onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); setOpen((v) => !v); } }}
        aria-expanded={open}
      >
        <span className="expert-collab__icon">🤝</span>
        <span className="expert-collab__title">{t("cowork.expertCollabCard")}</span>
        <span className="expert-collab__team">{c.teamName}</span>
        <span className="expert-collab__mode">{modeLabel(c.mode)}</span>
        <span className="expert-collab__actions">
          <button type="button" className="expert-collab__open-btn" onClick={openInExperts} title={t("cowork.expertCollabOpen")}>
            {t("cowork.expertCollabOpen")}
          </button>
          {onDelete && (
            <button type="button" className="expert-collab__delete-btn" onClick={handleDelete} title={t("cowork.expertCollabDelete")}>
              {t("cowork.expertCollabDelete")}
            </button>
          )}
          <ChevronRight className={open ? "expert-collab__chevron--open" : ""} size={12} />
        </span>
      </div>
      {!open && preview && <div className="expert-collab__preview">{preview}</div>}
      {open && (
        <div className="cowork-expert__stream">
          {rounds.map((round, ri) => (
            <div key={ri} className="cowork-expert__round">
              <div className="cowork-expert__round-label">
                {t("cowork.expertRound").replace("{n}", String(ri + 1))}
              </div>
              {round.map((ans, ai) => (
                <div key={ai} className="cowork-expert__turn">
                  <div className="cowork-expert__turn-head">
                    <span className="cowork-expert__turn-name">{ans.expertName}</span>
                  </div>
                  <div className="cowork-expert__turn-body">{ans.text}</div>
                </div>
              ))}
            </div>
          ))}
          {preview && (
            <div className="cowork-expert__round cowork-expert__round--synthesis">
              <div className="cowork-expert__round-label cowork-expert__round-label--synthesis">
                {t("cowork.expertSynthesis")}
              </div>
              <div className="cowork-expert__turn-body cowork-expert__turn-body--synthesis">{preview}</div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
