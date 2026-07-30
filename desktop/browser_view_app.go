package main

// browser_view_app.go powers the in-app browser panel: it owns a launched
// browser (internal/browserlaunch) and streams CDP screencast frames to the
// Wails frontend over the "browser:view:frame" event channel. The agent
// drives the SAME browser (the sidecar attaches via connect_over_cdp), so the
// panel is a faithful live mirror, not a separate instance.
//
// Event channel + data-URL rendering mirror the existing experts:collab live
// stream (experts_app.go) and the AttachmentDataURL image pipeline, so the
// frontend reuses proven infra rather than a new transport.

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/zzycxz/momapeer/internal/browserlaunch"
)

// BrowserViewFrame is one frame pushed to the frontend via the
// "browser:view:frame" event. The frontend draws DataURL on a <canvas>/<img>.
type BrowserViewFrame struct {
	DataURL string `json:"dataUrl"`
	Width   int64  `json:"width,omitempty"`
	Height  int64  `json:"height,omitempty"`
	URL     string `json:"url,omitempty"`
}

// browserViewSession holds the launched browser + its screencast subscription.
type browserViewSession struct {
	handle  *browserlaunch.Handle
	cancel  context.CancelFunc
	doneCh  chan struct{}
}

// browserView owns the live browser-view session on the App.
// Fields are guarded by browserViewMu; Start/Stop may be called from any
// goroutine (the browser_auto tool on the agent thread, or an App method on
// the Wails-bound call thread).
type browserView struct {
	mu      sync.Mutex
	session *browserViewSession

	// emitThrottle avoids flooding the frontend with frames faster than it can
	// paint. The last frame within the throttle window is always delivered, so
	// motion is not lost — only burst rate is capped.
	lastEmit time.Time
}

// emitBrowserFrame pushes one screencast frame to the frontend. It is called
// from the screencast listener goroutine for every captured frame; a simple
// ~30fps throttle keeps the event bus from saturating on a fast-changing page.
func (a *App) emitBrowserFrame(frame browserlaunch.Frame) {
	bv := a.browserView
	bv.mu.Lock()
	if bv.session == nil {
		bv.mu.Unlock()
		return
	}
	now := time.Now()
	if now.Sub(bv.lastEmit) < 33*time.Millisecond { // ~30fps cap
		bv.mu.Unlock()
		return
	}
	bv.lastEmit = now
	bv.mu.Unlock()

	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "browser:view:frame", BrowserViewFrame{
		DataURL: frame.DataURL,
		Width:   frame.Width,
		Height:  frame.Height,
		URL:     frame.URL,
	})
}

// startBrowserViewForHandle attaches a screencast to an already-launched
// browser handle and begins streaming frames to the frontend. It is intended
// to be called right after browserlaunch.Launch (e.g. inside browser_auto)
// so the panel mirrors the agent's browser from the very first navigation.
// If a previous view is running it is stopped first.
func (a *App) startBrowserViewForHandle(handle *browserlaunch.Handle) {
	bv := a.browserView
	bv.mu.Lock()
	if bv.session != nil && bv.session.handle == handle {
		bv.mu.Unlock()
		return // already streaming this handle
	}
	prev := bv.session
	bv.mu.Unlock()
	if prev != nil {
		prev.handle.StopScreencast()
		close(prev.doneCh)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := handle.StartScreencast(a.emitBrowserFrame, browserlaunch.DefaultScreencastOptions()); err != nil {
		slog.Warn("browser view: screencast start failed", "err", err)
		cancel()
		return
	}
	doneCh := make(chan struct{})
	bv.mu.Lock()
	bv.session = &browserViewSession{handle: handle, cancel: cancel, doneCh: doneCh}
	bv.mu.Unlock()

	// If the browser dies under us, stop the session so a later Start can
	// re-arm against a fresh handle.
	go func() {
		select {
		case <-handle.Done():
			a.stopBrowserView()
		case <-ctx.Done():
		}
		close(doneCh)
	}()
}

// stopBrowserView stops streaming and (if the App owns the handle) closes the
// browser. It is safe to call when not running (no-op).
func (a *App) stopBrowserView() {
	bv := a.browserView
	bv.mu.Lock()
	session := bv.session
	bv.session = nil
	bv.mu.Unlock()
	if session == nil {
		return
	}
	session.handle.StopScreencast()
	session.cancel()
}

// BrowserViewRunning reports whether a live browser view is currently streaming.
// Exposed as an App method for the frontend to poll panel state if needed.
func (a *App) BrowserViewRunning() bool {
	a.browserView.mu.Lock()
	defer a.browserView.mu.Unlock()
	return a.browserView.session != nil
}

// StopBrowserAuto cancels the in-flight autonomous-browsing run (if any) by
// asking the sidecar to /stop its current loop, and tears down the live view.
// Exposed as an App method for the panel's Stop button. Best-effort: if the
// sidecar has already finished, this is a no-op.
func (a *App) StopBrowserAuto() {
	if a.buService != nil && a.buService.IsRunning() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := a.buService.Client().Stop(stopCtx); err != nil {
			slog.Warn("browser view: sidecar stop failed", "err", err)
		}
		cancel()
	}
	a.stopBrowserView()
}
