// ExpertPanel is the coWork "专家团" panel: manage expert teams + run
// multi-model collaborations with live streaming. Subscribes to
// "experts:collab" (streamed expert outputs) and "experts:changed" (team-list
// refresh).
//
// Layout: top bar (team selector + new-team + budget indicator) → task input +
// run button → collaboration stream (each expert's output streamed live) →
// team management modal.

import { useCallback, useEffect, useRef, useState } from "react";
import { Plus, Sparkles, Trash2, Pencil, Users } from "lucide-react";

import { app, onExpertsChanged, onExpertsCollab } from "../../lib/bridge";
import type { BudgetStatusView, TeamView } from "../../lib/types";
import { useT } from "../../lib/i18n";
import { useToast } from "../../lib/toast";
import { CollabStream, type StreamMessage } from "./CollabStream";
import { TeamManager } from "./TeamManager";

export function ExpertPanel() {
  const t = useT();
  const { showToast } = useToast();
  const [teams, setTeams] = useState<TeamView[] | null>(null);
  const [activeTeamId, setActiveTeamId] = useState("");
  const [budget, setBudget] = useState<BudgetStatusView | null>(null);
  const [task, setTask] = useState("");
  const [mode, setMode] = useState("debate");
  const [rounds, setRounds] = useState(2);
  const [running, setRunning] = useState(false);
  const [messages, setMessages] = useState<StreamMessage[]>([]);
  const [showTeamMgr, setShowTeamMgr] = useState(false);
  const [editingTeam, setEditingTeam] = useState<TeamView | null>(null);
  const streamEndRef = useRef<HTMLDivElement>(null);

  const refresh = useCallback(async () => {
    try {
      const [tms, bd] = await Promise.all([app.ListExpertTeams(), app.ExpertBudgetStatus()]);
      setTeams(tms);
      setBudget(bd);
    } catch {
      setTeams([]);
      setBudget(null);
    }
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => onExpertsChanged(() => void refresh()), [refresh]);

  // Stream collaboration events into the message list.
  useEffect(() => {
    return onExpertsCollab((ev) => {
      if (ev.phase === "expert_start") {
        setMessages((prev) => [...prev, {
          kind: "expert", expertName: ev.expertName, round: ev.round, text: "", streaming: true,
        }]);
      } else if (ev.phase === "expert_chunk") {
        // Append delta to the last message matching this expert+round.
        setMessages((prev) => {
          const out = [...prev];
          for (let i = out.length - 1; i >= 0; i--) {
            if (out[i].kind === "expert" && out[i].expertName === ev.expertName && out[i].round === ev.round) {
              out[i] = { ...out[i], text: out[i].text + ev.text };
              break;
            }
          }
          return out;
        });
      } else if (ev.phase === "expert_done") {
        setMessages((prev) => {
          const out = [...prev];
          for (let i = out.length - 1; i >= 0; i--) {
            if (out[i].kind === "expert" && out[i].expertName === ev.expertName && out[i].round === ev.round) {
              out[i] = { ...out[i], streaming: false };
              break;
            }
          }
          return out;
        });
      } else if (ev.phase === "synthesis") {
        if (ev.text) {
          setMessages((prev) => {
            const last = prev[prev.length - 1];
            if (last && last.kind === "synthesis") {
              return [...prev.slice(0, -1), { ...last, text: last.text + ev.text }];
            }
            return [...prev, { kind: "synthesis" as const, expertName: "", round: 0, text: ev.text, streaming: true }];
          });
        } else if (ev.message) {
          setMessages((prev) => [...prev, { kind: "synthesis" as const, expertName: "", round: 0, text: "", streaming: true }]);
        }
      } else if (ev.phase === "run_done") {
        setRunning(false);
        setMessages((prev) => prev.map((m) => ({ ...m, streaming: false })));
      } else if (ev.phase === "error") {
        setRunning(false);
        showToast(ev.message || "error", "error");
      }
    });
  }, [showToast]);

  // Auto-scroll to bottom on new content.
  useEffect(() => {
    streamEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  const handleRun = async () => {
    if (!activeTeamId || !task.trim()) return;
    setRunning(true);
    setMessages([]);
    try {
      await app.RunExpertTeam(activeTeamId, task.trim(), mode, rounds);
    } catch (e) {
      setRunning(false);
      showToast(String(e), "error");
    }
  };

  const handleDelete = async (team: TeamView) => {
    if (!window.confirm(t("cowork.expertConfirmDelete").replace("{name}", team.name))) return;
    try {
      await app.DeleteExpertTeam(team.id);
      if (activeTeamId === team.id) setActiveTeamId("");
      void refresh();
    } catch (e) { showToast(String(e), "error"); }
  };

  const activeTeam = teams?.find((tm) => tm.id === activeTeamId) ?? null;

  return (
    <div className="cowork-expert">
      <header className="cowork-main__header">
        <h2>{t("cowork.expert")}</h2>
        <div className="cowork-expert__header-actions">
          {budget && budget.rpm > 0 && (
            <span className="cowork-expert__budget" title={t("cowork.expertBudget")}>
              {t("cowork.expertBudget")}: {budget.remaining}/{budget.rpm}
            </span>
          )}
          {budget && budget.rpm === 0 && (
            <span className="cowork-expert__budget cowork-expert__budget--off">{t("cowork.expertBudgetUnlimited")}</span>
          )}
          <button className="btn btn--small" onClick={() => { setEditingTeam(null); setShowTeamMgr(true); }}>
            <Plus size={14} />
            {t("cowork.expertNew")}
          </button>
        </div>
      </header>

      <div className="cowork-expert__body">
        {/* Team selector + edit/delete */}
        {teams === null ? (
          <div className="cowork-expert__loading">…</div>
        ) : teams.length === 0 ? (
          <div className="cowork-expert__empty">{t("cowork.expertEmpty")}</div>
        ) : (
          <div className="cowork-expert__teams">
            <select
              className="cowork-expert__select"
              value={activeTeamId}
              onChange={(e) => {
                setActiveTeamId(e.target.value);
                const tm = teams.find((x) => x.id === e.target.value);
                if (tm) { setMode(tm.defaultMode); setRounds(tm.defaultRounds); }
              }}
            >
              <option value="">{t("cowork.expert")}…</option>
              {teams.map((tm) => (
                <option key={tm.id} value={tm.id}>
                  {tm.name} · {tm.experts.length} {t("cowork.expertMembers")}
                </option>
              ))}
            </select>
            {activeTeam && (
              <>
                <button className="cowork-task-card__btn" title={t("cowork.expertEdit")} onClick={() => { setEditingTeam(activeTeam); setShowTeamMgr(true); }}>
                  <Pencil size={14} />
                </button>
                <button className="cowork-task-card__btn cowork-task-card__btn--danger" title={t("cowork.expertDelete")} onClick={() => void handleDelete(activeTeam)}>
                  <Trash2 size={14} />
                </button>
              </>
            )}
          </div>
        )}

        {/* Active team experts preview */}
        {activeTeam && (
          <div className="cowork-expert__roster">
            {activeTeam.experts.map((ex, i) => (
              <span key={i} className="cowork-expert__expert-chip">
                <Users size={12} />
                {ex.name}
                {ex.model && <span className="cowork-expert__expert-model">{ex.model}</span>}
              </span>
            ))}
          </div>
        )}

        {/* Mode + rounds + task */}
        {activeTeam && (
          <div className="cowork-expert__controls">
            <select className="cowork-expert__select cowork-expert__select--mode" value={mode} onChange={(e) => setMode(e.target.value)}>
              <option value="debate">{t("cowork.expertModeDebate")}</option>
              <option value="parallel">{t("cowork.expertModeParallel")}</option>
              <option value="pipeline">{t("cowork.expertModePipeline")}</option>
            </select>
            {mode === "debate" && (
              <label className="cowork-expert__rounds">
                {t("cowork.expertRounds")}
                <input type="number" min={1} max={5} value={rounds} onChange={(e) => setRounds(Math.max(1, Math.min(5, Number(e.target.value) || 2)))} />
              </label>
            )}
          </div>
        )}

        {activeTeam && (
          <div className="cowork-expert__input-row">
            <textarea
              className="cowork-expert__task-input"
              placeholder={t("cowork.expertTaskHint")}
              value={task}
              rows={3}
              onChange={(e) => setTask(e.target.value)}
              disabled={running}
            />
            <button
              className="btn btn--primary btn--small"
              disabled={running || !task.trim()}
              onClick={() => void handleRun()}
            >
              <Sparkles size={14} />
              {running ? t("cowork.expertRunning") : t("cowork.expertRun")}
            </button>
          </div>
        )}

        {/* Collaboration stream */}
        {messages.length > 0 && (
          <CollabStream messages={messages} endRef={streamEndRef} t={t} />
        )}
      </div>

      {showTeamMgr && (
        <TeamManager
          initial={editingTeam}
          onSave={async (team) => {
            try {
              if (editingTeam) { await app.UpdateExpertTeam({ ...team, id: editingTeam.id }); }
              else { await app.CreateExpertTeam(team); }
              setShowTeamMgr(false);
              setEditingTeam(null);
              void refresh();
            } catch (e) { showToast(String(e), "error"); }
          }}
          onCancel={() => { setShowTeamMgr(false); setEditingTeam(null); }}
        />
      )}
    </div>
  );
}
