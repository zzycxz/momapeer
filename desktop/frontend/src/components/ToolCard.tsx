import { memo, useEffect, useRef, useState } from "react";
import { ChevronRight } from "lucide-react";
import { CodeViewer } from "./CodeViewer";
import { DiffView } from "./DiffView";
import { useT } from "../lib/i18n";
import { diffsFor, subjectOf } from "../lib/tools";
import { useShellExpand } from "../lib/shellExpand";
import { app } from "../lib/bridge";
import type { Item } from "../lib/useController";

type ToolItem = Extract<Item, { kind: "tool" }>;

const SUBAGENT_TOOLS = new Set(["task", "run_skill", "explore", "research"]);

/** Lines shown by default in a shell output block before the "show all" button. */
const SHELL_PREVIEW_LINES = 10;

function pretty(json: string): string {
  try {
    return JSON.stringify(JSON.parse(json), null, 2);
  } catch {
    return json;
  }
}

function formatToolDuration(ms?: number): string {
  if (typeof ms !== "number" || !Number.isFinite(ms) || ms < 0) return "";
  return `${Math.round(ms)} ms`;
}

// ToolAttachments renders image files a tool produced (e.g. image_generate
// pictures saved under .momapeer/attachments/) directly under the tool card.
// Paths can't be loaded by a bare <img> in the webview, so fetch a data URL via
// the kernel — the same bridge UserMessage uses for pasted-image previews.
function ToolAttachments({ paths }: { paths: string[] }) {
  const [previews, setPreviews] = useState<Record<string, string>>({});
  const key = paths.join("\n");
  useEffect(() => {
    const list = key ? key.split("\n") : [];
    if (list.length === 0) return;
    let cancelled = false;
    for (const p of list) {
      if (previews[p]) continue;
      app.AttachmentDataURL(p)
        .then((url) => { if (!cancelled) setPreviews((prev) => (prev[p] ? prev : { ...prev, [p]: url })); })
        .catch(() => {});
    }
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return (
    <div className="tool__attachments">
      {paths.map((p) => previews[p] ? <img key={p} src={previews[p]} alt="" loading="lazy" /> : null)}
    </div>
  );
}

/** Returns the first n lines of text and the total line count. */
function splitPreview(text: string, n: number): { preview: string; total: number; hasMore: boolean } {
  const lines = text.split("\n");
  const total = lines.length;
  if (total <= n) return { preview: text, total, hasMore: false };
  return { preview: lines.slice(0, n).join("\n"), total, hasMore: true };
}

// ToolCard renders one tool call. `subcalls` are sub-agent calls nested under a
// `task` card (their ParentID points at this call); they render inline, live, so
// the sub-agent's work is visible as it happens.
export const ToolCard = memo(function ToolCard({ item, subcalls }: { item: ToolItem; subcalls?: ToolItem[] }) {
  const t = useT();
  const diffs = diffsFor(item.name, item.args);
  const subject = subjectOf(item.name, item.args);
  const nested = subcalls ?? [];
  const hasNested = nested.length > 0;
  const profileText =
    SUBAGENT_TOOLS.has(item.name) && item.profile
      ? [item.profile.model, item.profile.effort ? `effort ${item.profile.effort}` : ""].filter(Boolean).join(" · ")
      : "";

  // A task's summary is its step count; everything else derives from the result.
  const summary =
    item.status === "running"
      ? ""
      : hasNested
        ? t(nested.length === 1 ? "tool.stepOne" : "tool.stepOther", { n: nested.length })
        : "";

  // edit diffs are the point of the card, so they're shown inline; everything
  // else folds its args/output away by default.  Open while running so the
  // user sees progress; closed by default once settled.
  const hasArgsOrOutput = diffs.length === 0 && (!!item.args || !!item.output);

  // Shell output: split into preview + "show all" toggle.
  const shellOutput = item.isShell && item.output ? item.output : null;
  const shellPreview = shellOutput ? splitPreview(shellOutput, SHELL_PREVIEW_LINES) : null;
  const hasAttachments = Boolean(item.attachments && item.attachments.some((a) => a.kind === "image"));
  const hasBody = Boolean(summary || diffs.length || hasNested || shellPreview || (!shellPreview && hasArgsOrOutput) || item.error || hasAttachments);
  // Open while running so the user sees live progress; closed once settled.
  // Shell cards (incl. agent-initiated bash) follow the same rule so streamed
  // stdout stays visible during a long command and auto-collapses on finish.
  const [open, setOpen] = useState((hasNested || item.isShell) ? item.status === "running" : false);
  const [showAll, setShowAll] = useState(false);
  // Track whether the user has manually toggled this card, so the auto-open /
  // auto-close effect below doesn't fight a deliberate interaction.
  const userToggledRef = useRef(false);
  const shellBodyRef = useRef<HTMLDivElement | null>(null);

  // Register this shell card's toggle with the global ShellExpand context so
  // Ctrl/Cmd+B can expand/collapse the most recent shell output.
  const shellExpand = useShellExpand();
  useEffect(() => {
    if (!item.isShell || !shellExpand) return;
    return shellExpand.register(item.id, () => {
      userToggledRef.current = true;
      setOpen((v) => !v);
    });
  }, [item.isShell, item.id, shellExpand]);

  // Auto-open shell cards while running (so streamed chunks are visible) and
  // auto-collapse when done — but only until the user manually toggles, after
  // which we respect their choice. Mirrors how streaming-thinking cards behave.
  useEffect(() => {
    if (!item.isShell || userToggledRef.current) return;
    const should = item.status === "running";
    if (should !== open) setOpen(should);
  }, [item.isShell, item.status, open]);

  // Keep the shell output pinned to the bottom as new chunks stream in, so the
  // latest lines (where errors appear) are always in view while running.
  useEffect(() => {
    if (!item.isShell || !open || item.status !== "running") return;
    const el = shellBodyRef.current?.querySelector("pre.code") as HTMLElement | null;
    if (el) el.scrollTop = el.scrollHeight;
  }, [item.isShell, open, item.status, item.output]);

  // Read-only "research" calls (read/grep/web_fetch) are hidden after
  // completion so they don't clutter the transcript. During execution they still
  // render so the user sees progress.
  const quiet =
    item.readOnly && !hasNested && item.status !== "error" && item.status !== "stopped";

  const duration = item.status === "running" ? "" : formatToolDuration(item.durationMs);

  return (
    <div className={`tool${quiet ? " tool--quiet" : ""}`}>
      <button
        type="button"
        className="tool__head"
        data-running={item.status === "running" ? "" : undefined}
        onClick={() => {
          if (!hasBody) return;
          userToggledRef.current = true;
          setOpen((v) => !v);
        }}
        aria-expanded={hasBody ? open : undefined}
      >
        <span className="tool__label-group">
          <span className="tool__name">{item.name}</span>
          {subject && <span className="tool__subject">{subject}</span>}
        </span>
        {profileText && <span className="tool__profile">{profileText}</span>}
        {duration && <span className="tool__duration">{duration}</span>}
        {hasBody && (
          <span className={`tool__chevron${open ? " tool__chevron--open" : ""}`}>
            <ChevronRight size={12} />
          </span>
        )}
      </button>

      {open && (
        <div className="tool__body">
          {summary && <div className="tool__summary">{summary}</div>}

        {diffs.map((d, i) => (
          <div key={i}>
            {d.label && <div className="tool__difflabel">{d.label}</div>}
            <DiffView original={d.original} modified={d.modified} language={d.lang} maxHeight={260} />
          </div>
        ))}

        {hasNested && (
          <div className="tool__nested">
            {nested.map((c) => (
              <ToolCard key={c.id} item={c} />
            ))}
          </div>
        )}

        {shellPreview && (
          <div ref={shellBodyRef}>
            <CodeViewer value={showAll ? shellOutput! : shellPreview.preview} maxHeight={showAll ? 480 : 260} />
            {shellPreview.hasMore && !showAll && (
              <button className="tool__showall" onClick={() => setShowAll(true)}>
                {t("tool.showAllLines", { n: shellPreview.total })}
              </button>
            )}
            {item.truncated && <div className="tool__note">{t("tool.truncated")}</div>}
          </div>
        )}

        {!shellPreview && hasArgsOrOutput && (
          <>
            {item.args && <CodeViewer value={pretty(item.args)} language="json" maxHeight={180} />}
            {item.output && (
              <>
                <CodeViewer value={item.output} maxHeight={280} />
                {item.truncated && <div className="tool__note">{t("tool.truncated")}</div>}
              </>
            )}
          </>
        )}

        {item.error && <div className="tool__err">{item.error}</div>}
        </div>
      )}

      {item.attachments && item.attachments.filter((a) => a.kind === "image").length > 0 && (
        <ToolAttachments paths={item.attachments!.filter((a) => a.kind === "image").map((a) => a.path)} />
      )}
    </div>
  );
});
