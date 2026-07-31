// ExpertPanel is the coWork "专家团" panel: manage expert teams + run
// multi-model collaborations with live streaming. Subscribes to
// "experts:collab" (streamed expert outputs) and "experts:changed" (team-list
// refresh).
//
// Layout: top bar (team selector + new-team) → task input + run button →
// collaboration stream (each expert's output streamed live) → team management
// modal. RPM limiting is enforced by the backend (the global [llm] RPM budget
// wraps every expert's provider), so there's no budget indicator here.

import { useCallback, useEffect, useRef, useState } from "react";
import { Plus, Sparkles, Trash2, Pencil, Users } from "lucide-react";

import { app, onExpertsChanged, onExpertsCollab } from "../../lib/bridge";
import type { TeamView } from "../../lib/types";
import { useT } from "../../lib/i18n";
import { useToast } from "../../lib/toast";
import { useConfirm } from "../../lib/confirm";
import type { StreamMessage } from "./CollabStream";
import { TeamManager } from "./TeamManager";

export function ExpertPanel() {
  const t = useT();
  const { showToast } = useToast();
  const confirm = useConfirm();
  const [teams, setTeams] = useState<TeamView[] | null>(null);
  const [activeTeamId, setActiveTeamId] = useState("");
  const [task, setTask] = useState("");
  const [mode, setMode] = useState("debate");
  const [rounds, setRounds] = useState(2);
  const [running, setRunning] = useState(false);
  const [messages, setMessages] = useState<StreamMessage[]>([]);
  const [showTeamMgr, setShowTeamMgr] = useState(false);
  const [editingTeam, setEditingTeam] = useState<TeamView | null>(null);
  const [filter, setFilter] = useState<"all" | "scenario" | "general" | "search">("all");
  // searchCostConfirmed resets when the selected team changes, so switching TO a
  // search team re-warns, but re-running the same search team doesn't nag.
  const [searchCostConfirmed, setSearchCostConfirmed] = useState(false);
  const streamEndRef = useRef<HTMLDivElement>(null);

  const refresh = useCallback(async () => {
    try {
      const tms = await app.ListExpertTeams();
      setTeams(tms);
    } catch {
      setTeams([]);
    }
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => onExpertsChanged(() => void refresh()), [refresh]);

  // Auto-select the first team if none is selected.
  useEffect(() => {
    if (teams && teams.length > 0 && !activeTeamId) {
      setActiveTeamId(teams[0].id);
      setMode(teams[0].defaultMode);
      setRounds(teams[0].defaultRounds);
    }
  }, [teams, activeTeamId]);

  // Reset cross-team state when the selected team changes, so the messages
  // and running flag from a previous team don't bleed into the new selection.
  // (The stream subscription below is also keyed on activeTeamId, so it only
  // receives the new team's events going forward; this clear handles the
  // already-accumulated state.)
  useEffect(() => {
    setMessages([]);
    setRunning(false);
  }, [activeTeamId]);

  // Recover an in-flight run after the CoWorkLayout was torn down (tab/profile
  // switch unmounted this panel mid-run). The backend goroutine keeps running
  // independent of the frontend; on (re)mount or team selection we ask whether
  // a run is still going and restore the running indicator so the live stream
  // (re-subscribed by the effect below) isn't missed. Only sets running=true —
  // never false, so it doesn't clobber a run that just finished in this mount.
  useEffect(() => {
    if (!activeTeamId) return;
    let cancelled = false;
    void app.GetActiveExpertRun(activeTeamId).then((v) => {
      if (cancelled) return;
      if (v && v.status === "running") setRunning(true);
    }).catch(() => { /* offline store: nothing to recover */ });
    return () => { cancelled = true; };
  }, [activeTeamId]);

  // Cross-navigation target: the main chat transcript (an ExpertCollabCard) or
  // CoWorkLayout dispatches cowork:open-experts with detail.teamId to jump here
  // and select a specific team. CoWorkLayout handles the panel switch; this
  // effect sets the team so the user lands on the right collaboration.
  useEffect(() => {
    const handle = (e: Event) => {
      const teamId = (e as CustomEvent<{ teamId?: string }>).detail?.teamId;
      if (teamId) setActiveTeamId(teamId);
    };
    window.addEventListener("cowork:open-experts", handle as EventListener);
    return () => window.removeEventListener("cowork:open-experts", handle as EventListener);
  }, []);

  // Stream collaboration events into the message list — but ONLY for the
  // currently-selected team. Without this filter, a hidden ExpertPanel (it
  // stays mounted via display:none in CoWorkLayout) would accumulate EVERY
  // team's events, and its `running` flag would flip on any team's completion
  // — cross-team state pollution that leaks back when the user reopens this
  // sidebar panel. Matching on activeTeamId keeps this panel's state scoped.
  useEffect(() => {
    if (!activeTeamId) return;
    return onExpertsCollab((ev) => {
      if (ev.teamId !== activeTeamId) return; // strict: only this panel's team
      if (ev.phase === "expert_start") {
        // Ignore the initial "协作开始" event (empty expertName, round=0).
        if (!ev.expertName) return;
        setMessages((prev) => [...prev, {
          kind: "expert", expertName: ev.expertName, round: ev.round, text: "", streaming: true,
        }]);
      } else if (ev.phase === "expert_chunk") {
        // Append delta to the last message matching this expert+round.
        // Self-healing: if no matching message exists (e.g. the expert_start
        // event was missed because this panel mounted mid-run after the
        // CoWorkLayout auto-switched here from a chat-initiated run), create
        // the message lazily so the first expert's opening chunks aren't lost.
        setMessages((prev) => {
          const out = [...prev];
          for (let i = out.length - 1; i >= 0; i--) {
            if (out[i].kind === "expert" && out[i].expertName === ev.expertName && out[i].round === ev.round) {
              out[i] = { ...out[i], text: out[i].text + ev.text };
              return out;
            }
          }
          // No match — lazy-create so a late-mounting panel still captures the
          // first expert's streamed text (covers the chat→panel handoff race).
          return [...out, {
            kind: "expert" as const, expertName: ev.expertName, round: ev.round,
            text: ev.text, streaming: true,
          }];
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
  }, [showToast, activeTeamId]);

  // Auto-scroll to bottom on new content.
  useEffect(() => {
    streamEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  const handleRun = async () => {
    if (!activeTeamId || !task.trim()) return;
    // A search-capable team runs a mini-agent per expert — meaningfully slower
    // and costlier. Warn once per team selection so the user isn't surprised by
    // a 3-5 minute wait, but don't nag on every re-run of the same team.
    if (activeTeam?.allowSearch && !searchCostConfirmed) {
      if (!(await confirm({ title: "搜索确认", message: t("cowork.expertSearchCostConfirm"), danger: false }))) {
        return;
      }
      setSearchCostConfirmed(true);
    }
    setRunning(true);
    setMessages([]);
    try {
      // Open the team's independent expert-session tab (which occupies the main
      // chat area as a group-chat view) and start the run. The live stream and
      // persisted history live in that tab, not here — this panel is just the
      // launch entry point.
      await app.RunExpertTeam(activeTeamId, task.trim(), mode, rounds);
      // Switch to the task center so the user sees the ExpertSessionView in the
      // main area (the expert tab was just activated by RunExpertTeam).
      window.dispatchEvent(new CustomEvent("cowork:reset-panel"));
      setTask("");
    } catch (e) {
      setRunning(false);
      showToast(String(e), "error");
    }
  };

  const handleDelete = async (team: TeamView) => {
    if (!(await confirm({ title: "删除专家团", message: t("cowork.expertConfirmDelete").replace("{name}", team.name) }))) return;
    try {
      await app.DeleteExpertTeam(team.id);
      if (activeTeamId === team.id) setActiveTeamId("");
      void refresh();
      // The backend closes the team's expert-session tab (and cancels any
      // in-flight run). Dispatching cowork:reset-panel triggers App.tsx's
      // listener to refreshTabMetas + syncActiveTab, so the closed tab
      // disappears from the TabBar immediately and the active tab is corrected
      // (if the deleted team's tab was active, we fall back to another tab).
      window.dispatchEvent(new CustomEvent("cowork:reset-panel"));
    } catch (e) { showToast(String(e), "error"); }
  };

  const activeTeam = teams?.find((tm) => tm.id === activeTeamId) ?? null;

  // Scenario teams ship with web search on (real-time-data rosters); general
  // role teams default off. Used by the filter tabs so 17 teams don't drown the
  // user — they can scope to "needs real-time data" or "fast one-shot" quickly.
  // The classification keys off the stable builtin IDs so user-created teams
  // (no matching ID) fall through to "general".
  const SCENARIO_IDS = new Set([
    "builtin_college_major", "builtin_event_predict", "builtin_research",
  ]);
  const isScenario = (tm: TeamView) => SCENARIO_IDS.has(tm.id);
  const filteredTeams = (teams ?? []).filter((tm) => {
    if (filter === "all") return true;
    if (filter === "scenario") return isScenario(tm);
    if (filter === "general") return !isScenario(tm);
    if (filter === "search") return tm.allowSearch;
    return true;
  });

  return (
    <div className="cowork-expert">
      <header className="cowork-main__header">
        <h2>{t("cowork.expert")}</h2>
        <div className="cowork-expert__header-actions">
          {teams && teams.length > 0 && (
            <span className="cowork-expert__header-count">
              {t("cowork.expertTeamsCount").replace("{n}", String(filteredTeams.length))}
            </span>
          )}
          <button className="btn btn--small" onClick={() => { setEditingTeam(null); setShowTeamMgr(true); }}>
            <Plus size={14} />
            {t("cowork.expertNew")}
          </button>
        </div>
      </header>

      <div className="cowork-expert__body">
        {/* Team card grid: each team is a flat card; click selects, hover shows edit/delete. */}
        {teams === null ? (
          <div className="cowork-expert__loading">…</div>
        ) : teams.length === 0 ? (
          <div className="cowork-expert__empty">{t("cowork.expertEmpty")}</div>
        ) : (
          <>
          {/* Filter tabs: scope 17 teams so the user isn't drowned. "scenario"
              = the 3 real-time-data rosters (ship with search on); "general" =
              role teams; "search" = any team currently allowing web search. */}
          <div className="cowork-expert__filters">
            {([
              ["all", t("cowork.expertFilterAll")],
              ["scenario", t("cowork.expertFilterScenario")],
              ["general", t("cowork.expertFilterGeneral")],
              ["search", t("cowork.expertFilterSearch")],
            ] as const).map(([key, label]) => (
              <button
                key={key}
                className={`cowork-expert__filter${filter === key ? " cowork-expert__filter--active" : ""}`}
                onClick={() => setFilter(key)}
              >
                {label}
              </button>
            ))}
          </div>
          {filteredTeams.length === 0 ? (
            <div className="cowork-expert__empty">{t("cowork.expertFilterEmpty")}</div>
          ) : (
          <div className="cowork-expert__grid">
            {filteredTeams.map((tm) => {
              const selected = tm.id === activeTeamId;
              const experts = tm.experts ?? [];
              const preview = experts.slice(0, 3);
              const extra = experts.length - preview.length;
              const modeLabel =
                tm.defaultMode === "debate" ? t("cowork.expertModeDebate")
                  : tm.defaultMode === "pipeline" ? t("cowork.expertModePipeline")
                    : t("cowork.expertModeParallel");
              return (
                <div
                  key={tm.id}
                  className={`cowork-expert__card${selected ? " cowork-expert__card--selected" : ""}`}
                  onClick={() => {
                    setActiveTeamId(tm.id);
                    setMode(tm.defaultMode);
                    setRounds(tm.defaultRounds);
                    setSearchCostConfirmed(false);
                  }}
                >
                  <div className="cowork-expert__card-head">
                    <span className={`cowork-expert__card-avatar${selected ? " cowork-expert__card-avatar--on" : ""}`}>
                      <Users size={12} />
                    </span>
                    <span className="cowork-expert__card-name">
                      {tm.name}
                      {tm.allowSearch && <span className="cowork-expert__search-badge" title={t("cowork.expertSearchHint")}>🔍</span>}
                    </span>
                  </div>
                  <div className="cowork-expert__card-actions">
                    <button
                      className="cowork-task-card__btn"
                      title={t("cowork.expertEdit")}
                      onClick={(e) => { e.stopPropagation(); setEditingTeam(tm); setShowTeamMgr(true); }}
                    >
                      <Pencil size={13} />
                    </button>
                    <button
                      className="cowork-task-card__btn cowork-task-card__btn--danger"
                      title={t("cowork.expertDelete")}
                      onClick={(e) => { e.stopPropagation(); void handleDelete(tm); }}
                    >
                      <Trash2 size={13} />
                    </button>
                  </div>
                  {experts.length > 0 && (
                    <div className="cowork-expert__card-roster" style={{ display: 'block', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', fontSize: '12px', color: 'var(--text-muted)', marginTop: '8px' }}>
                      专家成员: {experts.map((ex) => ex.name).join(", ")}
                    </div>
                  )}
                  <div className="cowork-expert__card-foot">
                    <span className="cowork-expert__card-mode">{modeLabel}</span>
                    <span>{(tm.experts ?? []).length} {t("cowork.expertMembers")}</span>
                  </div>
                </div>
              );
            })}
            {/* Dashed "new team" placeholder card (only on "all" — don't clutter
                a filtered view where the user is hunting for a specific team). */}
            {filter === "all" && (
              <div
                className="cowork-expert__card cowork-expert__card--new"
                onClick={() => { setEditingTeam(null); setShowTeamMgr(true); }}
              >
                <Plus size={14} />
                {t("cowork.expertNewTeamCard")}
              </div>
            )}
          </div>
          )}
          </>
        )}

      </div>

      {/* Card-style "start collaboration" composer — mirrors the task-center
          composer-card: a rounded card that prominently labels the selected
          team, holds the task textarea, and puts mode/run controls in a foot
          bar like composer-meta. */}
      {activeTeam && (
        <footer className="footer">
          <div className="composer-wrap">
            <div className="cowork-expert__composer">
            <textarea
              className="cowork-expert__composer-input"
              placeholder={t("cowork.expertTaskHint")}
              value={task}
              rows={3}
              onChange={(e) => setTask(e.target.value)}
              disabled={running}
            />
            {activeTeam.allowSearch && (
              <div className="cowork-expert__search-banner">{t("cowork.expertSearchBadge")}</div>
            )}

            <div className="cowork-expert__composer-foot">
              <div className="cowork-expert__composer-head" style={{ marginBottom: 0 }}>
                <span className="cowork-expert__composer-avatar">
                  <Users size={13} />
                </span>
                <div className="cowork-expert__composer-team">
                  <span className="cowork-expert__composer-name">{activeTeam.name}</span>
                  <span className="cowork-expert__composer-roster">
                    {(activeTeam.experts ?? []).map((ex) => ex.name).filter(Boolean).join(" · ") || t("cowork.expertNoMembers")}
                  </span>
                </div>
                <span className="cowork-expert__composer-tag">{t("cowork.expert")}</span>
              </div>

              <div style={{ display: 'flex', gap: '12px', alignItems: 'center' }}>
                <div className="cowork-expert__composer-opts">
                  <select className="cowork-expert__composer-select" value={mode} onChange={(e) => setMode(e.target.value)} disabled={running}>
                    <option value="debate">{t("cowork.expertModeDebate")}</option>
                    <option value="parallel">{t("cowork.expertModeParallel")}</option>
                    <option value="pipeline">{t("cowork.expertModePipeline")}</option>
                  </select>
                  {mode === "debate" && (
                    <label className="cowork-expert__composer-rounds">
                      {t("cowork.expertRounds")}
                      <input type="number" min={1} max={5} value={rounds} onChange={(e) => setRounds(Math.max(1, Math.min(5, Number(e.target.value) || 2)))} disabled={running} />
                    </label>
                  )}
                </div>
                <div className="cowork-expert__run-group">
                  {activeTeam.allowSearch && !running && (
                    <span className="cowork-expert__cost-hint" title={t("cowork.expertSearchHint")}>
                      {t("cowork.expertSearchCostTag")}
                    </span>
                  )}
                  <button
                    className="btn btn--primary btn--small"
                    disabled={running || !task.trim()}
                    onClick={() => void handleRun()}
                  >
                    <Sparkles size={14} />
                    {running ? t("cowork.expertRunning") : t("cowork.expertRun")}
                  </button>
                </div>
              </div>
            </div>
            </div>
          </div>
        </footer>
      )}

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
              // Refresh tab metas so an open expert-session tab picks up the
              // renamed team (the TabBar title and ExpertSessionView header
              // read from state.meta.expertSession.teamName, which is sourced
              // from MetaForTab → tab.ExpertTeamName). Without this, renaming a
              // team leaves the tab showing the old name until the next poll.
              window.dispatchEvent(new CustomEvent("cowork:reset-panel"));
            } catch (e) { showToast(String(e), "error"); }
          }}
          onCancel={() => { setShowTeamMgr(false); setEditingTeam(null); }}
        />
      )}
    </div>
  );
}
