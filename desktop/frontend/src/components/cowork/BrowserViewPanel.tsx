// BrowserViewPanel is the coWork in-app browser mirror: while the
// autonomous-browsing agent (browser_auto) drives a shared Chrome/Edge, this
// panel shows a live CDP screencast of what it's doing — clicks, typing,
// navigation. It does NOT drive the browser itself; it is a passive observer.
//
// The frames arrive over the "browser:view:frame" Wails event as base64 data
// URLs (see browser_view_app.go). We draw them on a <canvas>, scaling to fit
// while preserving aspect ratio. The header shows the current page URL (an
// address bar) and a Stop button to cancel a runaway run. When no run is
// active, a placeholder prompts the user to ask the agent to browse.

import { useEffect, useRef, useState } from "react";
import { Globe, Square } from "lucide-react";

import { app, onBrowserViewFrame, type BrowserViewFrame } from "../../lib/bridge";
import { useT } from "../../lib/i18n";

// staleAfter: if no frame has arrived for this long, treat the mirror as idle
// (the browser may have closed or the agent finished). Tuned to a few seconds
// so a brief pause (e.g. a slow page load) doesn't flicker the placeholder.
const STALE_MS = 5000;

export function BrowserViewPanel() {
  const t = useT();
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const imgRef = useRef<HTMLImageElement | null>(null);
  const [lastFrame, setLastFrame] = useState<BrowserViewFrame | null>(null);
  const [frameDims, setFrameDims] = useState<{ w: number; h: number } | null>(null);
  const [currentURL, setCurrentURL] = useState("");
  const [isStale, setIsStale] = useState(false);
  const [stopping, setStopping] = useState(false);

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

  const handleStop = async () => {
    setStopping(true);
    try {
      await app.StopBrowserAuto();
    } catch {
      // best-effort; the panel just stops updating
    } finally {
      setStopping(false);
    }
  };

  return (
    <div
      className="cowork-main__transcript"
      style={{
        display: "flex",
        flexDirection: "column",
        minHeight: 0,
        padding: "16px",
        gap: "12px",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 8, flexShrink: 0 }}>
        <Globe size={16} />
        <strong>{t("cowork.browserView") || "浏览器实时画面"}</strong>
        {hasLive ? (
          <span style={{ fontSize: 12, color: "var(--text-muted, #888)" }}>
            {t("cowork.browserViewLive") || "· 正在跟随 AI 操作"}
          </span>
        ) : (
          <span style={{ fontSize: 12, color: "var(--text-muted, #888)" }}>
            {t("cowork.browserViewIdle") || "· 空闲"}
          </span>
        )}
        {hasLive && (
          <button
            onClick={handleStop}
            disabled={stopping}
            title={t("cowork.browserViewStop") || "停止当前浏览任务"}
            style={{
              marginLeft: "auto",
              display: "inline-flex",
              alignItems: "center",
              gap: 4,
              padding: "4px 10px",
              fontSize: 12,
              cursor: stopping ? "default" : "pointer",
            }}
          >
            <Square size={12} />
            {stopping
              ? t("cowork.browserViewStopping") || "停止中…"
              : t("cowork.browserViewStop") || "停止"}
          </button>
        )}
      </div>

      {/* Address bar: shows the page the agent is currently on. Read-only —
          the panel observes, it does not navigate. */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 6,
          padding: "5px 10px",
          background: "var(--surface-2, #222)",
          border: "1px solid var(--border, #333)",
          borderRadius: 6,
          fontSize: 12,
          color: currentURL ? "var(--text, #ddd)" : "var(--text-muted, #888)",
          fontFamily: "monospace",
          overflow: "hidden",
          whiteSpace: "nowrap",
          textOverflow: "ellipsis",
          flexShrink: 0,
        }}
      >
        <Globe size={12} style={{ opacity: 0.6, flexShrink: 0 }} />
        <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>
          {currentURL || (t("cowork.browserViewNoUrl") || "等待页面加载…")}
        </span>
      </div>

      <div
        style={{
          flex: 1,
          minHeight: 0,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          border: "1px solid var(--border, #333)",
          borderRadius: 8,
          background: "var(--surface, #1a1a1a)",
          overflow: "hidden",
          position: "relative",
        }}
      >
        {/* Hidden decoder image; the visible surface is the canvas. */}
        <img
          ref={imgRef}
          onLoad={onImgLoad}
          alt=""
          style={{ display: "none" }}
        />
        {hasLive ? (
          <canvas
            ref={canvasRef}
            style={{
              maxWidth: "100%",
              maxHeight: "100%",
              objectFit: "contain",
            }}
          />
        ) : (
          <div
            style={{
              color: "var(--text-muted, #888)",
              textAlign: "center",
              padding: 24,
              maxWidth: 360,
            }}
          >
            <Globe size={32} style={{ opacity: 0.4, marginBottom: 8 }} />
            <div style={{ marginBottom: 4 }}>
              {lastFrame
                ? t("cowork.browserViewStopped") || "浏览器会话已结束"
                : t("cowork.browserViewEmpty") || "暂无浏览器画面"}
            </div>
            <div style={{ fontSize: 12, opacity: 0.7 }}>
              {t("cowork.browserViewHint") ||
                "让 AI 执行浏览器任务（如「打开某网站并搜索」），它会在此处实时显示操作过程。"}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
