package provider

import (
	"context"
	"testing"
	"time"
)

// TestRequestBudgetDisabled confirms RPM<=0 produces a no-op budget (Acquire
// returns immediately, never blocks). This is the backward-compat path.
func TestRequestBudgetDisabled(t *testing.T) {
	b := NewRequestBudget(0, 0)
	if err := b.Acquire(context.Background(), "key", false); err != nil {
		t.Errorf("disabled budget should not block: %v", err)
	}
	// Many acquires should all succeed instantly.
	for i := 0; i < 100; i++ {
		if err := b.Acquire(context.Background(), "key", true); err != nil {
			t.Fatalf("disabled budget acquire %d: %v", i, err)
		}
	}
	st := b.Status("key")
	if st.RPM != 0 {
		t.Errorf("disabled status RPM = %d, want 0", st.RPM)
	}
}

// TestRequestBudgetPriority confirms main-agent (priority=true) requests are
// always granted even when dipping into the reserve, while background requests
// (priority=false) block at the reserve boundary.
func TestRequestBudgetPriority(t *testing.T) {
	// RPM=3, reserve=2 → background can use 1 (3-2), main can use all 3.
	b := NewRequestBudget(3, 2)
	ctx := context.Background()
	key := "test"

	// Background: first 1 should succeed.
	if err := b.Acquire(ctx, key, false); err != nil {
		t.Fatalf("background acquire 1: %v", err)
	}
	// Background 2nd should block (would dip into reserve). Use a short-timeout
	// ctx to confirm it waits rather than returns nil.
	shortCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := b.Acquire(shortCtx, key, false); err == nil {
		t.Error("background acquire 2 should block at reserve, but succeeded")
	}

	// Main (priority): should still succeed immediately despite background
	// being blocked — it's allowed to use the reserve.
	if err := b.Acquire(ctx, key, true); err != nil {
		t.Fatalf("main acquire should succeed into reserve: %v", err)
	}
	// Main 2nd — reserve is now 1 left (3 total - 2 used); still allowed.
	if err := b.Acquire(ctx, key, true); err != nil {
		t.Fatalf("main acquire 2: %v", err)
	}
	// Now window is fully exhausted (3/3). Even main should block.
	shortCtx2, cancel2 := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel2()
	if err := b.Acquire(shortCtx2, key, true); err == nil {
		t.Error("main acquire past RPM should block, but succeeded")
	}
}

// TestRequestBudgetSeparateKeys confirms different API keys get independent
// buckets (matching per-key RPM quotas).
func TestRequestBudgetSeparateKeys(t *testing.T) {
	b := NewRequestBudget(2, 0)
	ctx := context.Background()
	// Drain key A.
	b.Acquire(ctx, "A", false)
	b.Acquire(ctx, "A", false)
	// Key A now exhausted; key B should still have 2.
	if err := b.Acquire(ctx, "B", false); err != nil {
		t.Errorf("key B acquire 1 should succeed (separate bucket): %v", err)
	}
}

// TestRequestBudgetReserveClamp confirms reserve >= rpm is clamped (otherwise
// background would block forever).
func TestRequestBudgetReserveClamp(t *testing.T) {
	b := NewRequestBudget(3, 10) // reserve > rpm
	// Should clamp to rpm-1 = 2, leaving 1 for background.
	if err := b.Acquire(context.Background(), "k", false); err != nil {
		t.Errorf("background should still get 1 slot after clamp: %v", err)
	}
}

// TestRequestBudgetStatus confirms the status reflects current usage for the UI.
func TestRequestBudgetStatus(t *testing.T) {
	b := NewRequestBudget(5, 2)
	b.Acquire(context.Background(), "k", true)
	b.Acquire(context.Background(), "k", true)
	st := b.Status("k")
	if st.RPM != 5 || st.Used != 2 || st.Remaining != 3 || st.ReserveMain != 2 {
		t.Errorf("status = %+v, want rpm=5 used=2 remaining=3 reserve=2", st)
	}
}

// TestRateLimitedProviderPassthrough confirms the decorator forwards Name and
// Stream faithfully when the budget is disabled (the common no-config path).
func TestRateLimitedProviderPassthrough(t *testing.T) {
	inner := &fakeProvider{name: "test"}
	// Disabled budget → NewRateLimitedProvider returns inner unwrapped.
	p := NewRateLimitedProvider(inner, NewRequestBudget(0, 0), "k", true)
	if p != inner {
		t.Error("disabled budget should return inner unwrapped, got wrapper")
	}
}

// TestRateLimitedProviderAcquires confirms the decorator calls Acquire before
// Stream, using a counting budget.
func TestRateLimitedProviderAcquires(t *testing.T) {
	inner := &fakeProvider{name: "test"}
	b := NewRequestBudget(5, 0)
	p := NewRateLimitedProvider(inner, b, "k", true)
	// Stream should succeed 5 times, then the 6th would block.
	for i := 0; i < 5; i++ {
		if _, err := p.Stream(context.Background(), Request{}); err != nil {
			t.Fatalf("stream %d: %v", i, err)
		}
	}
	// 6th: use short timeout to confirm it blocks.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := p.Stream(ctx, Request{}); err == nil {
		t.Error("6th stream should block (budget exhausted), but succeeded")
	}
	// Inner Stream was called exactly 5 times (once per successful acquire).
	if inner.streamCalls != 5 {
		t.Errorf("inner Stream called %d times, want 5", inner.streamCalls)
	}
}

// fakeProvider is a no-op Provider for decorator tests.
type fakeProvider struct {
	name        string
	streamCalls int
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Stream(ctx context.Context, req Request) (<-chan Chunk, error) {
	f.streamCalls++
	ch := make(chan Chunk)
	close(ch) // immediate empty stream
	return ch, nil
}
