// BrowserViewPanel is the coWork in-app browser: a one-stop surface for
// autonomous web tasks. The user types a GOAL here ("打开淘宝搜索…"), it is
// submitted to the active tab's agent (which calls browser_auto), and the
// panel mirrors the agent-driven browser live via CDP screencast. So the user
// both launches AND watches the task from one place — no need to jump back to
// the chat composer.
//
// Visual design uses the app's design tokens (styles.css): dark elevated
// surfaces, the orange accent gradient, layered shadows — so it reads as part
// of momapeer, not a foreign box.

import { useEffect, useRef, useState } from "react";
import { Globe, Play, Square, Sparkles } from "lucide-react";

import { app, onBrowserViewFrame, type BrowserViewFrame } from "../../lib/bridge";
import { useT } from "../../lib/i18n";

// staleAfter: if no frame has arrived for this long, treat the mirror as idle
// (the browser may have closed or the agent finished). Tuned to a few seconds
// so a brief pause (e.g. a slow page load) doesn't flicker the placeholder.
const STALE_MS = 5000;

// Example goals surfaced as one-click chips to teach the user what works.
const EXAMPLE_GOALS = [
  "打开 example.com 并截图",
  "搜索今天的新闻头条",
  "查一下北京明天的天气",
];

export function BrowserViewPanel() {
  const t = useT();
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const imgRef = useRef<HTMLImageElement | null>(null);
  const [lastFrame, setLastFrame] = useState<BrowserViewFrame | null>(null);
  const [frameDims, setFrameDims] = useState<{ w: number; h: number } | null>(null);
  const [currentURL, setCurrentURL] = useState("");
  const [isStale, setIsStale] = useState(true);
  const [stopping, setStopping] = useState(false);
  const [goal, setGoal] = useState("");

  useEffect(() => {
    // Hidden <img> decodes each data URL; onload we blit it onto the canvas.
    // Using an Image avoids manual base64→bitmap and handles both jpeg/png.
    const off = onBrowserViewFrame((f) => {
      setLastFrame(f);
      setIsStale(false);
      if (f.width && f.height) {
        setFrameDims({ w: f.width, h: f.height });
      }
      if (f.url) {
        setCurrentURL(f.url);
      }
      const img = imgRef.current;
      if (img) img.src = f.dataUrl;
    });
    return off;
  }, []);

  // Stale detector: flips to a placeholder if the stream goes quiet.
  useEffect(() => {
    if (!lastFrame) return;
    const id = window.setTimeout(() => setIsStale(true), STALE_MS);
    return () => window.clearTimeout(id);
  }, [lastFrame]);

  const onImgLoad = () => {
    const img = imgRef.current;
    const canvas = canvasRef.current;
    if (!img || !canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const w = img.naturalWidth || frameDims?.w || canvas.width;
    const h = img.naturalHeight || frameDims?.h || canvas.height;
    canvas.width = w;
    canvas.height = h;
    ctx.drawImage(img, 0, 0, w, h);
  };

  const hasLive = lastFrame && !isStale;

  // Submit the goal to the active tab's agent. The agent reads it as a normal
  // turn and invokes browser_auto; the screencast then mirrors the run into
  // this panel automatically. Empty tabID routes to the active tab.
  const handleSubmit = async () => {
    const trimmed = goal.trim();
    if (!trimmed) return;
    try {
      await app.SubmitToTab("", trimmed);
      setGoal("");
    } catch {
      // best-effort; the panel just won't start streaming
    }
  };

  const handleStop = async () => {
    setStopping(true);
    try {
      await app.StopBrowserAuto();
    } catch {
      // best-effort
    } finally {
      setStopping(false);
    }
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // Ctrl/Cmd+Enter submits; plain Enter inserts a newline (goals are often
    // multi-line). Matches the main composer's convention.
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      void handleSubmit();
    }
  };

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        minHeight: 0,
        height: "100%",
        background: "var(--bg)",
        gap: 0,
      }}
    >
      {/* Header: title + live/idle status + stop. */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          padding: "10px 14px",
          borderBottom: "1px solid var(--border-soft)",
          flexShrink: 0,
        }}
      >
        <div
          style={{
            width: 26,
            height: 26,
            borderRadius: 7,
            background: "var(--grad)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            flexShrink: 0,
            boxShadow: "var(--shadow-1)",
          }}
        >
          <Globe size={15} color="var(--accent-fg)" />
        </div>
        <strong style={{ fontSize: "var(--text-base)" }}>
          {t("cowork.browserView") || "浏览器"}
        </strong>
        <span
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: 5,
            fontSize: "var(--text-xs)",
            color: hasLive ? "var(--ok)" : "var(--fg-faint)",
            marginLeft: 2,
          }}
        >
          <span
            style={{
              width: 7,
              height: 7,
              borderRadius: "50%",
              background: hasLive ? "var(--ok)" : "var(--fg-faint)",
              boxShadow: hasLive ? "0 0 6px var(--ok)" : "none",
            }}
          />
          {hasLive
            ? t("cowork.browserViewLive") || "正在操作"
            : t("cowork.browserViewIdle") || "空闲"}
        </span>
        {hasLive && (
          <button
            onClick={handleStop}
            disabled={stopping}
            className="ghost-btn"
            title={t("cowork.browserViewStop") || "停止"}
            style={{
              marginLeft: "auto",
              display: "inline-flex",
              alignItems: "center",
              gap: 5,
              padding: "5px 11px",
              fontSize: "var(--text-sm)",
              border: "1px solid var(--danger)",
              color: "var(--danger)",
              background: "transparent",
              borderRadius: 6,
              cursor: stopping ? "default" : "pointer",
              opacity: stopping ? 0.6 : 1,
            }}
          >
            <Square size={12} />
            {stopping
              ? t("cowork.browserViewStopping") || "停止中…"
              : t("cowork.browserViewStop") || "停止"}
          </button>
        )}
      </div>

      {/* Address bar: shows the page the agent is currently on (read-only). */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 7,
          margin: "10px 14px 0",
          padding: "6px 11px",
          background: "var(--bg-elev)",
          border: "1px solid var(--border-soft)",
          borderRadius: 7,
          fontSize: "var(--text-sm)",
          color: currentURL ? "var(--fg-dim)" : "var(--fg-faint)",
          fontFamily: "var(--font-code, monospace)",
          flexShrink: 0,
        }}
      >
        <Globe size={12} style={{ opacity: 0.55, flexShrink: 0 }} />
        <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {currentURL || (t("cowork.browserViewNoUrl") || "等待页面加载…")}
        </span>
      </div>

      {/* Viewport: the live screencast canvas, or an inviting placeholder when idle. */}
      <div
        style={{
          flex: 1,
          minHeight: 0,
          margin: "10px 14px 0",
          borderRadius: 10,
          border: "1px solid var(--border-soft)",
          background: "var(--bg-soft)",
          overflow: "hidden",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          position: "relative",
        }}
      >
        {/* Hidden decoder image; the visible surface is the canvas. */}
        <img ref={imgRef} onLoad={onImgLoad} alt="" style={{ display: "none" }} />
        {hasLive ? (
          <canvas
            ref={canvasRef}
            style={{ maxWidth: "100%", maxHeight: "100%", objectFit: "contain" }}
          />
        ) : (
          <div
            style={{
              color: "var(--fg-faint)",
              textAlign: "center",
              padding: 28,
              maxWidth: 380,
            }}
          >
            <div
              style={{
                width: 52,
                height: 52,
                margin: "0 auto 14px",
                borderRadius: 14,
                background: "var(--accent-soft)",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
              }}
            >
              <Globe size={26} style={{ color: "var(--accent)" }} />
            </div>
            <div
              style={{
                color: "var(--fg-dim)",
                fontSize: "var(--text-base)",
                marginBottom: 6,
              }}
            >
              {lastFrame
                ? t("cowork.browserViewStopped") || "操作已结束"
                : t("cowork.browserViewEmpty") || "浏览器待命中"}
            </div>
            <div style={{ fontSize: "var(--text-sm)", lineHeight: 1.6 }}>
              {t("cowork.browserViewHint") ||
                "在下方输入你想让 AI 完成的浏览任务，点开始后这里会实时显示它的操作。"}
            </div>
          </div>
        )}
      </div>

      {/* Composer: the user's entry point — type a goal, press start. */}
      <div style={{ padding: "10px 14px 14px", flexShrink: 0 }}>
        <div
          style={{
            position: "relative",
            border: "1px solid var(--border)",
            borderRadius: 9,
            background: "var(--bg-elev)",
            boxShadow: "var(--shadow-1)",
            transition: "border-color .15s",
          }}
        >
          <textarea
            value={goal}
            onChange={(e) => setGoal(e.target.value)}
            onKeyDown={onKeyDown}
            rows={2}
            placeholder={
              t("cowork.browserViewGoalPlaceholder") ||
              "描述你想让 AI 完成的浏览任务，例如：打开知乎搜索「AI Agent」并总结前 3 条回答"
            }
            style={{
              width: "100%",
              background: "transparent",
              border: "none",
              outline: "none",
              resize: "none",
              padding: "10px 12px",
              paddingRight: 44,
              color: "var(--fg)",
              fontSize: "var(--text-base)",
              fontFamily: "inherit",
              lineHeight: 1.5,
            }}
          />
          <button
            onClick={handleSubmit}
            disabled={!goal.trim()}
            title={t("cowork.browserViewStart") || "开始"}
            style={{
              position: "absolute",
              right: 7,
              bottom: 7,
              width: 30,
              height: 30,
              borderRadius: 7,
              border: "none",
              background: goal.trim() ? "var(--grad)" : "var(--bg-elev-2)",
              color: goal.trim() ? "var(--accent-fg)" : "var(--fg-faint)",
              cursor: goal.trim() ? "pointer" : "default",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              boxShadow: goal.trim() ? "var(--shadow-1)" : "none",
            }}
          >
            <Play size={15} fill="currentColor" />
          </button>
        </div>

        {/* One-click example goals, shown only before the first run. */}
        {!hasLive && !goal.trim() && (
          <div
            style={{
              display: "flex",
              gap: 7,
              marginTop: 9,
              flexWrap: "wrap",
            }}
          >
            {EXAMPLE_GOALS.map((g) => (
              <button
                key={g}
                onClick={() => setGoal(g)}
                style={{
                  display: "inline-flex",
                  alignItems: "center",
                  gap: 5,
                  padding: "4px 10px",
                  fontSize: "var(--text-xs)",
                  color: "var(--fg-dim)",
                  background: "var(--bg-elev)",
                  border: "1px solid var(--border-soft)",
                  borderRadius: 14,
                  cursor: "pointer",
                }}
              >
                <Sparkles size={11} style={{ color: "var(--accent)", opacity: 0.8 }} />
                {g}
              </button>
            ))}
          </div>
        )}
        <div
          style={{
            marginTop: 8,
            fontSize: "var(--text-2xs)",
            color: "var(--fg-faint)",
            textAlign: "center",
          }}
        >
          {t("cowork.browserViewShortcut") || "Ctrl/Cmd + Enter 快速开始 · 复杂任务可在聊天区跟进"}
        </div>
      </div>
    </div>
  );
}
