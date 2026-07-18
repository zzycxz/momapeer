// ExpertSessionView is the main-area view for an expert-session tab. It renders
// the team's collaboration history (each finished run as a group-chat block:
// rounds → expert turns → synthesis) PLUS the live in-progress stream (experts
// speaking in real time), and a composer for follow-up tasks that append to the
// same session (multi-turn).
//
// Two data sources:
//   - Persisted history: state.items → expert_collab Items (finished runs, from
//     the expert tab's controller session).
//   - Live stream: the "experts:collab" event channel (experts speaking during
//     a run, accumulated into local StreamMessage[] — same reducer logic as
//     ExpertPanel). When the run finishes, the persisted expert_collab Item
//     arrives via the tab's event routing and the live stream is cleared.

import { useEffect, useRef, useState } from "react";
import { Send, Users } from "lucide-react";

import { useT, type Translator } from "../../lib/i18n";
import type { Item } from "../../lib/useController";
import type { WireCollab } from "../../lib/types";
import { app, onExpertsCollab } from "../../lib/bridge";
import { useToast } from "../../lib/toast";
import { CollabStream, type StreamMessage } from "./CollabStream";

interface CollabItem {
  kind: "expert_collab";
  id: string;
  collab: WireCollab;
}

const MODE_OPTIONS = [
  { value: "debate", labelKey: "cowork.expertModeDebate" },
  { value: "parallel", labelKey: "cowork.expertModeParallel" },
  { value: "pipeline", labelKey: "cowork.expertModePipeline" },
] as const;

export function ExpertSessionView({
  items,
  teamName,
  teamId,
  running,
  onSend,
}: {
  items: Item[];
  teamName: string;
  teamId: string;
  running: boolean;
  onSend: (task: string, mode: string, rounds: number) => void;
}) {
  const t = useT();
  const { showToast } = useToast();
  const [task, setTask] = useState("");
  const [mode, setMode] = useState("debate");
  const [rounds, setRounds] = useState(2);
  const [liveMessages, setLiveMessages] = useState<StreamMessage[]>([]);
  const [liveRunning, setLiveRunning] = useState(false);
  const [liveTask, setLiveTask] = useState("");
  const streamEndRef = useRef<HTMLDivElement>(null);

  // Reset ALL local live state when the team changes. Without this, switching
  // from one expert-session tab to another reuses the same component instance
  // (same element type in the tree), and liveMessages/liveTask/liveRunning leak
  // the previous team's in-progress stream into the new tab. This is the
  // hard guard behind the `key={teamId}` in App.tsx — belt and suspenders, so
  // a stale key (e.g. meta not yet swapped) can't show the wrong team's content.
  useEffect(() => {
    setLiveMessages([]);
    setLiveTask("");
    setLiveRunning(false);
  }, [teamId]);

  // When a finished collaboration is persisted (a new expert_collab item lands
  // in `items`), clear the live stream so the same run doesn't render twice
  // (once as liveMessages, once as a persisted CollabBlock). This replaces the
  // old fixed 500ms setTimeout, which could either duplicate (persisted record
  // arrives before the timer) or flash empty (arrives after). Reacting to the
  // actual item count is deterministic. Only fires while a live run was active
  // (liveMessages non-empty), so loading persisted history on tab open doesn't
  // spuriously reset anything.
  const collabCount = items.filter((it) => it.kind === "expert_collab").length;
  const prevCollabCountRef = useRef(collabCount);
  useEffect(() => {
    if (collabCount > prevCollabCountRef.current && liveMessages.length > 0) {
      setLiveMessages([]);
      setLiveTask("");
    }
    prevCollabCountRef.current = collabCount;
  }, [collabCount, liveMessages.length]);

  // Recover in-flight run state on mount/team-change. When the expert tab was
  // activated mid-run (e.g. a sidebar-initiated run that opened this tab, or
  // the user switched away and back), the backend's GetActiveExpertRun reports
  // the running status, the task text, AND the cached live-stream messages
  // that accumulated while the tab was hidden. Restoring both liveTask and
  // liveMessages here makes the user's question + the experts' progress
  // visible immediately — without this, switching tabs mid-run would lose all
  // the events that arrived while nobody was listening.
  useEffect(() => {
    let cancelled = false;
    setLiveRunning(false);
    setLiveTask("");
    setLiveMessages([]);
    void app.GetActiveExpertRun(teamId).then((v) => {
      if (cancelled) return;
      if (v && v.status === "running") {
        setLiveRunning(true);
        if (v.task) setLiveTask(v.task);
        // Restore the backend-cached stream messages so the experts' progress
        // during the tab-switch gap is visible immediately. The onExpertsCollab
        // subscription (declared below) registers synchronously before this
        // async promise resolves, so events arriving in the gap are already in
        // liveMessages. We merge by only seeding from cache when liveMessages
        // is still empty (no live events arrived yet) — otherwise we'd
        // overwrite and lose those gap events.
        if (v.messages && v.messages.length > 0) {
          setLiveMessages((prev) => {
            if (prev.length > 0) return prev; // live events already arrived; keep them
            return v.messages!.map((m) => ({
              kind: m.kind as "expert" | "synthesis",
              expertName: m.expertName,
              round: m.round,
              text: m.text,
              streaming: m.streaming,
            }));
          });
        }
      }
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [teamId]);

  // Subscribe to the live collaboration stream. Same phase-reducer as
  // ExpertPanel — each expert's chunks accumulate into StreamMessage[], shown
  // via CollabStream below the persisted history. Cleared when a new run starts
  // or finishes.
  useEffect(() => {
    return onExpertsCollab((ev) => {
      // STRICT match: only accept events for THIS team. The old guard
      // `ev.teamId && ev.teamId !== teamId` let events with an EMPTY teamId
      // through (the bridge defaults missing teamId to ""), polluting this
      // view with any teamless event. Require an exact teamId match so a
      // wrong-team or untagged event can never land here.
      if (ev.teamId !== teamId) return;
      if (ev.phase === "expert_start") {
        if (!ev.expertName) return; // ignore the aggregate "协作开始" event
        setLiveMessages((prev) => [...prev, {
          kind: "expert", expertName: ev.expertName, round: ev.round, text: "", streaming: true,
        }]);
      } else if (ev.phase === "expert_chunk") {
        setLiveMessages((prev) => {
          const out = [...prev];
          for (let i = out.length - 1; i >= 0; i--) {
            if (out[i].kind === "expert" && out[i].expertName === ev.expertName && out[i].round === ev.round) {
              out[i] = { ...out[i], text: out[i].text + ev.text };
              return out;
            }
          }
          return [...out, { kind: "expert" as const, expertName: ev.expertName, round: ev.round, text: ev.text, streaming: true }];
        });
      } else if (ev.phase === "expert_done") {
        setLiveMessages((prev) => {
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
          setLiveMessages((prev) => {
            const last = prev[prev.length - 1];
            if (last && last.kind === "synthesis") {
              return [...prev.slice(0, -1), { ...last, text: last.text + ev.text }];
            }
            return [...prev, { kind: "synthesis" as const, expertName: "", round: 0, text: ev.text, streaming: true }];
          });
        }
      } else if (ev.phase === "run_done") {
        setLiveRunning(false);
        // Don't clear liveMessages on a timer. The persisted expert_collab Item
        // arrives via the tab's event routing (as a new entry in `collabs`).
        // The collabCount effect below clears the live stream the moment that
        // persisted record appears, avoiding both the duplicate window (live +
        // persisted showing the same content) and the empty-flash (clearing
        // before the persisted record arrives). If the persisted record never
        // arrives (persistence error), the live stream stays as the record.
      } else if (ev.phase === "error") {
        setLiveRunning(false);
        // Clear the live stream on error — no persisted record will arrive
        // (the run failed), so the collabCount effect won't fire. Without
        // this, partial chunks + the user's task would stay rendered forever.
        setLiveMessages([]);
        setLiveTask("");
        if (ev.message) showToast(ev.message, "error");
      }
    });
  }, [teamId, showToast]);

  // Auto-scroll on new live content.
  useEffect(() => {
    streamEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [liveMessages]);

  // Defense-in-depth: only render expert_collab items that belong to THIS team.
  // Each expert tab has its own session/history so items are usually already
  // scoped, but if an expert_collab event is ever misrouted (e.g. an event with
  // an empty tabId falls back to the active tab at useController.ts), the wrong
  // team's collaboration would render here. The teamId check is the hard guard.
  const collabs = items.filter(
    (it): it is CollabItem => it.kind === "expert_collab" && (!it.collab.teamId || it.collab.teamId === teamId)
  );
  const busy = running || liveRunning;

  const handleSend = () => {
    if (!task.trim() || busy) return;
    const userTask = task.trim();
    setLiveRunning(true);
    setLiveTask(userTask);
    setLiveMessages([]);
    onSend(userTask, mode, mode === "debate" ? rounds : 0);
    setTask("");
  };

  return (
    <div className="expert-session">
      <header className="expert-session__header">
        <Users size={16} />
        <span className="expert-session__title">{teamName}</span>
        <span className="expert-session__subtitle">{t("cowork.expertCollabCard")}</span>
      </header>

      <div className="expert-session__body">
        {collabs.length === 0 && liveMessages.length === 0 && !liveRunning && (
          <div className="expert-session__empty">{t("cowork.expertSessionEmpty")}</div>
        )}
        {collabs.map((it) => (
          <CollabBlock key={it.id} item={it} t={t} />
        ))}
        {/* Live in-progress stream — shown while the run is active, before the
            persisted record arrives. The user's task (liveTask) is shown above
            the stream so the user sees their own question. liveTask shows even
            before the first expert event arrives (liveMessages may still be
            empty right after send), so the user isn't left staring at a blank
            area while the team warms up. */}
        {(liveMessages.length > 0 || liveRunning) && (
          <div className="expert-session__live-block">
            {liveTask && <div className="expert-session__user-task">{liveTask}</div>}
            {liveMessages.length === 0 && (
              <div className="expert-session__starting">{t("cowork.expertStarting")}</div>
            )}
            <CollabStream messages={liveMessages} endRef={streamEndRef} t={t} />
          </div>
        )}
      </div>

      <footer className="expert-session__composer">
        <textarea
          className="expert-session__input"
          placeholder={t("cowork.expertTaskHint")}
          rows={2}
          value={task}
          onChange={(e) => setTask(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) handleSend(); }}
          disabled={busy}
        />
        <div className="expert-session__controls">
          <select value={mode} onChange={(e) => setMode(e.target.value)} disabled={busy}>
            {MODE_OPTIONS.map((o) => (<option key={o.value} value={o.value}>{t(o.labelKey)}</option>))}
          </select>
          {mode === "debate" && (
            <input type="number" min={1} max={5} value={rounds} onChange={(e) => setRounds(Math.max(1, Math.min(5, Number(e.target.value) || 1)))} disabled={busy} />
          )}
          <button className="btn btn--small" onClick={handleSend} disabled={busy || !task.trim()}>
            <Send size={13} />
            {busy ? t("cowork.expertRunning") : t("cowork.expertRun")}
          </button>
        </div>
      </footer>
    </div>
  );
}

// CollabBlock renders one finished collaboration as a group-chat block.
function CollabBlock({ item, t }: { item: CollabItem; t: Translator }) {
  const c = item.collab;
  const rounds = Array.isArray(c.rounds) ? c.rounds : [];
  return (
    <div className="expert-session__collab">
      <div className="expert-session__collab-task">{c.task}</div>
      <div className="cowork-expert__stream">
        {rounds.map((round, ri) => (
          <div key={ri} className="cowork-expert__round">
            <div className="cowork-expert__round-label">{t("cowork.expertRound").replace("{n}", String(ri + 1))}</div>
            {round.map((ans, ai) => (
              <div key={ai} className="cowork-expert__turn">
                <div className="cowork-expert__turn-head"><span className="cowork-expert__turn-name">{ans.expertName}</span></div>
                <div className="cowork-expert__turn-body">{ans.text}</div>
              </div>
            ))}
          </div>
        ))}
        {c.synthesis && (
          <div className="cowork-expert__round cowork-expert__round--synthesis">
            <div className="cowork-expert__round-label cowork-expert__round-label--synthesis">{t("cowork.expertSynthesis")}</div>
            <div className="cowork-expert__turn-body cowork-expert__turn-body--synthesis">{c.synthesis}</div>
          </div>
        )}
      </div>
    </div>
  );
}
