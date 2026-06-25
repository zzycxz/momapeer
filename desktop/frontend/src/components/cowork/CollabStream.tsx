// CollabStream renders the live multi-expert collaboration discussion: each
// expert's output (grouped by round), then the moderator synthesis. Text
// streams in real-time as CollabEvents arrive.

import type { RefObject } from "react";
import type { Translator } from "../../lib/i18n";

export interface StreamMessage {
  kind: "expert" | "synthesis";
  expertName: string;
  round: number;
  text: string;
  streaming: boolean;
}

export function CollabStream({
  messages,
  endRef,
  t,
}: {
  messages: StreamMessage[];
  endRef: RefObject<HTMLDivElement | null>;
  t: Translator;
}) {
  // Group expert messages by round.
  const rounds = new Map<number, StreamMessage[]>();
  let synthesis: StreamMessage | null = null;
  for (const m of messages) {
    if (m.kind === "synthesis") {
      synthesis = m;
    } else {
      const arr = rounds.get(m.round) ?? [];
      arr.push(m);
      rounds.set(m.round, arr);
    }
  }
  const roundNums = [...rounds.keys()].sort((a, b) => a - b);

  return (
    <div className="cowork-expert__stream">
      {roundNums.map((rn) => (
        <div key={rn} className="cowork-expert__round">
          <div className="cowork-expert__round-label">
            {t("cowork.expertRound").replace("{n}", String(rn))}
          </div>
          {(rounds.get(rn) ?? []).map((m, i) => (
            <div key={i} className={`cowork-expert__turn ${m.streaming ? "cowork-expert__turn--streaming" : ""}`}>
              <div className="cowork-expert__turn-head">
                <span className="cowork-expert__turn-name">{m.expertName}</span>
                {m.streaming && <span className="cowork-expert__turn-cursor">▋</span>}
              </div>
              <div className="cowork-expert__turn-body">{m.text}</div>
            </div>
          ))}
        </div>
      ))}
      {synthesis && (
        <div className="cowork-expert__round cowork-expert__round--synthesis">
          <div className="cowork-expert__round-label cowork-expert__round-label--synthesis">
            {t("cowork.expertSynthesis")}
          </div>
          <div className="cowork-expert__turn-body cowork-expert__turn-body--synthesis">
            {synthesis.text}
            {synthesis.streaming && <span className="cowork-expert__turn-cursor">▋</span>}
          </div>
        </div>
      )}
      <div ref={endRef} />
    </div>
  );
}
