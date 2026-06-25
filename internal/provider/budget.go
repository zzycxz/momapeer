package provider

// budget.go implements a per-API-key request budget (RPM rate limiter) shared
// across ALL providers in a process. It wraps every provider via the
// RateLimitedProvider decorator (see rate_limit.go), so main-agent + subagent +
// RAG extraction + IM-bot responses all draw from the same per-minute quota —
// matching the reality that an API key's RPM is shared regardless of which
// model/feature issued the request.
//
// Priority model (the "reserve_main" knob): main-agent requests (priority=true)
// are always granted immediately, even if that dips into the reserve. Background
// requests (expert teams, extraction, IM) wait for the next 60s window when the
// remaining quota is at or below reserve — so the main conversation stays
// responsive even while a multi-expert collaboration runs in the background.
//
// RPM=0 disables limiting entirely (Acquire returns immediately), preserving
// backward compatibility for users who don't configure [llm].

import (
	"context"
	"sync"
	"time"
)

// windowDuration is the RPM window. 60s matches how providers meter RPM.
const windowDuration = 60 * time.Second

// RequestBudget meters RPM across one or more API keys. Each key (identified by
// baseURL+apiKey) gets its own rolling window, because different providers have
// independent quotas. nil / RPM=0 means no limiting.
type RequestBudget struct {
	rpm         int
	reserveMain int
	mu          sync.Mutex
	buckets     map[string]*rateBucket
}

type rateBucket struct {
	windowStart time.Time
	count       int // total requests in the current window
}

// NewRequestBudget builds a budget. rpm<=0 returns a disabled budget (Acquire is
// a no-op) so callers don't need to branch on "is limiting configured".
func NewRequestBudget(rpm, reserveMain int) *RequestBudget {
	if rpm <= 0 {
		return &RequestBudget{} // disabled; buckets stays nil
	}
	if reserveMain < 0 {
		reserveMain = 0
	}
	if reserveMain >= rpm {
		// reserve >= rpm would block ALL background requests forever; clamp.
		reserveMain = rpm - 1
		if reserveMain < 0 {
			reserveMain = 0
		}
	}
	return &RequestBudget{
		rpm:         rpm,
		reserveMain: reserveMain,
		buckets:     make(map[string]*rateBucket),
	}
}

// Acquire blocks until a request slot is available for the given key, or ctx is
// cancelled. priority=true grants immediately (main-agent); priority=false
// (background) waits when remaining quota <= reserve.
//
// Returns nil on success, ctx.Err() if the context is cancelled while waiting.
func (b *RequestBudget) Acquire(ctx context.Context, key string, priority bool) error {
	if b == nil || b.rpm <= 0 {
		return nil // disabled
	}
	for {
		wait := b.tryAcquire(key, priority)
		if wait <= 0 {
			return nil // acquired
		}
		// Wait until the window resets (or context cancels), then retry. We
		// don't sleep the full wait in one go so a cancelled context surfaces
		// promptly — poll at most every second.
		if wait > time.Second {
			wait = time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// tryAcquire attempts to consume one slot. Returns the duration to wait before
// retrying (0 = acquired).
func (b *RequestBudget) tryAcquire(key string, priority bool) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	bk := b.buckets[key]
	if bk == nil {
		bk = &rateBucket{windowStart: now}
		b.buckets[key] = bk
	}
	// Reset window if it elapsed.
	if now.Sub(bk.windowStart) >= windowDuration {
		bk.windowStart = now
		bk.count = 0
	}
	remaining := b.rpm - bk.count
	if remaining <= 0 {
		// Window exhausted entirely — everyone waits for reset.
		return bk.windowStart.Add(windowDuration).Sub(now)
	}
	if !priority && remaining <= b.reserveMain {
		// Background request would dip into the reserve — wait for reset so
		// main-agent requests stay answerable.
		return bk.windowStart.Add(windowDuration).Sub(now)
	}
	bk.count++
	return 0
}

// Status reports the current-window usage for a key, for the UI's "RPM: 3/5
// remaining" indicator and cost estimates.
type BudgetStatus struct {
	RPM         int `json:"rpm"`
	Used        int `json:"used"`
	Remaining   int `json:"remaining"`
	ReserveMain int `json:"reserveMain"`
	WindowSecs  int `json:"windowSecs"` // seconds until the window resets
}

// Status returns the current budget status for a key. When limiting is
// disabled, returns a zeroed status (RPM=0).
func (b *RequestBudget) Status(key string) BudgetStatus {
	if b == nil || b.rpm <= 0 {
		return BudgetStatus{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	bk := b.buckets[key]
	now := time.Now()
	if bk == nil || now.Sub(bk.windowStart) >= windowDuration {
		return BudgetStatus{RPM: b.rpm, Remaining: b.rpm, ReserveMain: b.reserveMain, WindowSecs: int(windowDuration.Seconds())}
	}
	remaining := b.rpm - bk.count
	if remaining < 0 {
		remaining = 0
	}
	secs := int(windowDuration - now.Sub(bk.windowStart).Round(time.Second))
	if secs < 0 {
		secs = 0
	}
	return BudgetStatus{RPM: b.rpm, Used: bk.count, Remaining: remaining, ReserveMain: b.reserveMain, WindowSecs: secs}
}
