package browserlaunch

// screencast.go subscribes to CDP Page.startScreencast on the launched browser
// and forwards each captured frame to a caller-supplied callback as a data URL.
//
// This is what powers the in-app browser panel: the agent drives the browser
// (via the Python sidecar over CDP), and this stream mirrors the visible page
// into the Wails frontend as base64 image frames drawn on a <canvas>. The
// browser stays a single shared instance — screencast is a passive observer
// that does NOT issue navigation/click commands, so it never contends with the
// driver.
//
// We attach our own chromedp session over the remote wsURL (NewRemoteAllocator)
// rather than reusing any builtin browserSession, because browserlaunch owns
// the launched process and exposes only the wsURL.

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ScreencastOptions tunes the frame stream.
type ScreencastOptions struct {
	// Format is "jpeg" (smaller, default) or "png".
	Format string
	// Quality is JPEG quality 0-100 (ignored for png). Default 70.
	Quality int
	// MaxWidth/MaxHeight cap the frame size. A cap keeps bandwidth bounded for
	// the panel; defaults 1024x720 are a good fidelity/bandwidth balance.
	MaxWidth  int
	MaxHeight int
}

// DefaultScreencastOptions are used when a field is zero.
func DefaultScreencastOptions() ScreencastOptions {
	return ScreencastOptions{
		Format:    "jpeg",
		Quality:   70,
		MaxWidth:  1024,
		MaxHeight: 720,
	}
}

// Frame is one captured browser frame delivered to the caller.
type Frame struct {
	// DataURL is a ready-to-render image data URL, e.g.
	// "data:image/jpeg;base64,....".
	DataURL string
	// Width/Height of the captured frame in pixels (from metadata).
	Width  int64
	Height int64
	// URL is the page URL at capture time (from the last top-frame navigation),
	// for the panel's address bar. Empty if no navigation has been observed yet.
	URL string
	// CapturedAt is when the frame was received from CDP.
	CapturedAt time.Time
}

// screencastSession holds the running screencast state on a Handle.
type screencastSession struct {
	mu     sync.Mutex
	ctx    context.Context // chromedp context (derived from the remote allocator)
	cancel context.CancelFunc
	stopped atomic.Bool
}

// StartScreencast begins streaming frames from the browser's active tab. The
// emit callback is invoked from a chromedp listener goroutine for each frame.
// Call StopScreencast to end the stream. Starting while already running is a
// no-op (returns nil).
//
// If opts fields are zero-valued, defaults are substituted.
func (h *Handle) StartScreencast(emit func(Frame), opts ScreencastOptions) error {
	h.screencastMu.Lock()
	defer h.screencastMu.Unlock()
	if h.sc != nil {
		return nil // already running
	}
	if emit == nil {
		return fmt.Errorf("browserlaunch: screencast emit callback is nil")
	}

	// Normalize options.
	if opts.Format == "" {
		opts.Format = "jpeg"
	}
	if opts.Quality <= 0 {
		opts.Quality = 70
	}
	if opts.MaxWidth <= 0 {
		opts.MaxWidth = 1024
	}
	if opts.MaxHeight <= 0 {
		opts.MaxHeight = 720
	}

	// Attach a fresh chromedp session to the remote browser. NewRemoteAllocator
	// does NOT spawn a process; it just opens the ws to the already-running
	// browser we launched in Launch().
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), h.WSURL)
	ctx, cancel := chromedp.NewContext(allocCtx)

	// Force a connection so target discovery is ready before we register the
	// listener (otherwise the listener may be attached before the target is
	// assigned and miss the StartScreencast Do).
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		allocCancel()
		return fmt.Errorf("browserlaunch: connect for screencast: %w", err)
	}

	sc := &screencastSession{
		ctx:    ctx,
		cancel: cancel,
	}

	// Track the current page URL so each frame can carry it for the panel's
	// address bar. The screencast event itself has no URL; we keep the last
	// navigated URL and stamp it onto each emitted frame.
	var urlMu sync.Mutex
	currentURL := ""

	// Ack every frame so Chrome keeps streaming; without the ack the stream
	// stalls after one frame. We ack asynchronously from the listener so a slow
	// emit callback never blocks the CDP reader.
	chromedp.ListenTarget(ctx, func(ev any) {
		switch e := ev.(type) {
		case *page.EventScreencastFrame:
			go func() {
				// Ack must run on a chromedp-derived context.
				ackCtx, ackCancel := context.WithTimeout(ctx, 5*time.Second)
				defer ackCancel()
				if err := page.ScreencastFrameAck(e.SessionID).Do(ackCtx); err != nil {
					slog.Debug("browserlaunch: screencast ack failed", "err", err)
				}
			}()

			frame := Frame{
				DataURL:    dataURL(opts.Format, e.Data),
				CapturedAt: time.Now(),
			}
			// DeviceWidth/Height are in DIPs (float64) — the captured frame's
			// pixel dimensions; handy for the frontend to size the canvas.
			if e.Metadata != nil {
				frame.Width = int64(e.Metadata.DeviceWidth)
				frame.Height = int64(e.Metadata.DeviceHeight)
			}
			urlMu.Lock()
			frame.URL = currentURL
			urlMu.Unlock()
			if sc.stopped.Load() {
				return
			}
			emit(frame)
		case *page.EventFrameNavigated:
			// Top-frame navigations update the tracked URL. Sub-frame iframes
			// (ParentID set) are ignored so the address bar reflects the page,
			// not an embedded ad.
			if e.Frame != nil && e.Frame.ParentID == "" {
				urlMu.Lock()
				currentURL = e.Frame.URL
				urlMu.Unlock()
			}
		}
	})

	// Start the cast. We keep Page.enable implicit (StartScreencast works
	// without it). Run in the chromedp ctx.
	startCtx, startCancel := context.WithTimeout(ctx, 10*time.Second)
	defer startCancel()
	p := page.StartScreencast().
		WithFormat(screencastFormat(opts.Format)).
		WithQuality(int64(opts.Quality)).
		WithMaxWidth(int64(opts.MaxWidth)).
		WithMaxHeight(int64(opts.MaxHeight))
	if err := p.Do(startCtx); err != nil {
		cancel()
		allocCancel()
		return fmt.Errorf("browserlaunch: start screencast: %w", err)
	}

	h.sc = sc
	return nil
}

// StopScreencast stops the frame stream and detaches the chromedp session.
// Safe to call when not running (no-op).
func (h *Handle) StopScreencast() {
	h.screencastMu.Lock()
	sc := h.sc
	h.sc = nil
	h.screencastMu.Unlock()
	if sc == nil {
		return
	}
	sc.stopped.Store(true)
	// Best-effort stop; ignore errors since the browser may already be gone.
	stopCtx, stopCancel := context.WithTimeout(sc.ctx, 3*time.Second)
	_ = page.StopScreencast().Do(stopCtx)
	stopCancel()
	sc.cancel()
}

// dataURL builds a renderable data URL from a base64-encoded frame payload.
func dataURL(format, b64 string) string {
	mime := "image/jpeg"
	if format == "png" {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + b64
}

// screencastFormat maps the string option to the cdproto enum.
func screencastFormat(s string) page.ScreencastFormat {
	if s == "png" {
		return page.ScreencastFormatPng
	}
	return page.ScreencastFormatJpeg
}
