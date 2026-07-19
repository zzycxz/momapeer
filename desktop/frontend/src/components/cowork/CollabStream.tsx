// CollabStream renders the live multi-expert collaboration discussion: each
// expert's output (grouped by round), then the moderator synthesis. Text
// streams in real-time as CollabEvents arrive.
//
// When an expert team allows web search, the desktop runner injects "🔍 搜索:
// <query>" markers into the text stream (one per web_search call). We split
// those out into distinct "searching" cards so the user can clearly see the
// expert researching, separate from its spoken answer.

import type { RefObject } from "react";
import type { Translator } from "../../lib/i18n";

export interface StreamMessage {
  kind: "expert" | "synthesis";
  expertName: string;
  round: number;
  text: string;
  streaming: boolean;
}

// A parsed segment of an expert's streamed text: either spoken answer prose,
// or a "searching" marker (the 🔍 line the runner injects). Splitting at render
// time (rather than in the event reducer) keeps the reducer a simple append and
// avoids restructuring StreamMessage for the no-search teams.
type Segment =
  | { type: "text"; text: string }
  | { type: "search"; query: string };

const SEARCH_MARKER = "🔍 搜索:";

// splitSegments pulls "🔍 搜索: query" lines out of an expert's text, returning
// alternating text/search segments. Lines without the marker are answer prose.
// The trailing partial marker (still streaming) is preserved as a search segment
// so the user sees a live "searching…" card mid-stream.
function splitSegments(text: string): Segment[] {
  const segs: Segment[] = [];
  const lines = text.split("\n");
  let buf: string[] = [];
  const flushBuf = () => {
    if (buf.length > 0) {
      const joined = buf.join("\n");
      if (joined.trim()) segs.push({ type: "text", text: joined });
      buf = [];
    }
  };
  for (const line of lines) {
    const idx = line.indexOf(SEARCH_MARKER);
    if (idx >= 0) {
      flushBuf();
      const query = line.slice(idx + SEARCH_MARKER.length).trim();
      segs.push({ type: "search", query: query || "…" });
    } else {
      buf.push(line);
    }
  }
  flushBuf();
  return segs;
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
          {(rounds.get(rn) ?? []).map((m, i) => {
            // Split out "🔍 搜索" markers into distinct search cards so a
            // search-capable expert's researching is visually separated from
            // its spoken answer. No-search teams have no markers, so this is a
            // single text segment — unchanged rendering for them.
            const segs = splitSegments(m.text);
            return (
              <div key={i} className={`cowork-expert__turn ${m.streaming ? "cowork-expert__turn--streaming" : ""}`}>
                <div className="cowork-expert__turn-head">
                  <span className="cowork-expert__turn-name">{m.expertName}</span>
                  {m.streaming && <span className="cowork-expert__turn-cursor">▋</span>}
                </div>
                {segs.map((seg, si) =>
                  seg.type === "search" ? (
                    <div key={si} className="cowork-expert__search-card">
                      <span className="cowork-expert__search-card-icon">🔍</span>
                      <span className="cowork-expert__search-card-label">{t("cowork.expertSearching")}</span>
                      <span className="cowork-expert__search-card-query">{seg.query}</span>
                    </div>
                  ) : (
                    <div key={si} className="cowork-expert__turn-body">{seg.text}</div>
                  )
                )}
              </div>
            );
          })}
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
