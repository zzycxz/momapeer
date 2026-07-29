package provider

// rate_limit.go wraps a Provider with a RequestBudget so every Stream call
// draws from the shared per-API-key RPM quota. boot.go installs this decorator
// at the NewProviderWithProxy return point, so ALL providers in a process
// (main-agent + subagent + any spawned by expert teams / RAG / IM) share one
// budget. The decorator is transparent: it forwards Name() and Stream()
// unchanged once a slot is acquired.
//
// The priority flag distinguishes main-agent requests (priority=true, always
// granted, may dip into reserve) from background requests (priority=false,
// waits when remaining <= reserve). boot sets priority on the main executor's
// provider; subagent/spawned providers default to priority=false.

import "context"

// RateLimitedProvider wraps an inner Provider with request-budget enforcement.
type RateLimitedProvider struct {
	inner    Provider
	budget   *RequestBudget
	key      string // baseURL+apiKey — the budget bucket key
	priority bool   // true = main-agent (always granted)
}

// NewRateLimitedProvider wraps inner. key identifies the budget bucket
// (baseURL+apiKey). priority=true marks this as a main-agent provider (always
// granted, may use reserve); false = background (waits on reserve).
func NewRateLimitedProvider(inner Provider, budget *RequestBudget, key string, priority bool) Provider {
	if budget == nil || budget.rpm <= 0 {
		return inner // limiting disabled — pass through unwrapped
	}
	return &RateLimitedProvider{inner: inner, budget: budget, key: key, priority: priority}
}

func (p *RateLimitedProvider) Name() string { return p.inner.Name() }

// Unwrap returns the inner Provider this decorator wraps, so callers (and tests)
// can reach the concrete provider underneath any number of decorators. Pairs
// with UnwrapProvider, the standard Go unwrap idiom: NewProviderWithProxy may
// wrap a provider with this rate limiter depending on the global budget, so a
// direct type assertion on its return value is unsafe — unwrap first.
func (p *RateLimitedProvider) Unwrap() Provider { return p.inner }

func (p *RateLimitedProvider) Stream(ctx context.Context, req Request) (<-chan Chunk, error) {
	if err := p.budget.Acquire(ctx, p.key, p.priority); err != nil {
		return nil, err
	}
	return p.inner.Stream(ctx, req)
}

// UnwrapProvider peels decorator layers (anything implementing Unwrap() Provider)
// off p until it reaches a provider that has no inner, returning that base.
// Use this before a type assertion on a provider that may have been wrapped by
// boot.NewProviderWithProxy (which adds RateLimitedProvider when a global RPM
// budget is configured). nil-safe: a nil p returns nil.
func UnwrapProvider(p Provider) Provider {
	for p != nil {
		u, ok := p.(interface{ Unwrap() Provider })
		if !ok {
			return p
		}
		inner := u.Unwrap()
		if inner == nil || inner == p {
			return p
		}
		p = inner
	}
	return p
}

// BudgetKeyForConfig returns the budget bucket key for a provider Config —
// baseURL + the resolved API key. Two providers hitting the same endpoint with
// the same key share one RPM quota (matching how providers actually meter: the
// platform counts requests per key, regardless of which model/feature/client
// issued them). The name parameter is accepted for call-site symmetry with
// provider.Config but intentionally NOT included, so the main conversation,
// subagents, Jiutian multimodal tools, RAG extraction/embedding, and RagAsk all
// draw from one bucket when they share an endpoint+key.
func BudgetKeyForConfig(name, baseURL, apiKey string) string {
	_ = name // accepted for symmetry; deliberately excluded from the key
	// The key is just an opaque string; it only needs to be consistent for a
	// given endpoint+key pair.
	return baseURL + "|" + apiKey
}
