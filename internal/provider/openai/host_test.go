package openai

import "testing"

// TestIsMoMA pins the host-matching rule for MoMA: the canonical
// api.jiutian.10086.cn, any *.moma.com subdomain, but NOT the apex
// moma.com (a misconfiguration we explicitly reject).
func TestIsMoMA(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    bool
	}{
		// Canonical
		{"https://jiutian.10086.cn", true},
		{"https://jiutian.10086.cn/v1", true},
		{"https://jiutian.10086.cn/anthropic", true},
		// Regional subdomains under the apex
		{"https://jiutian.10086.cn/v1", true},
		{"https://jiutian.10086.cn/v1", true},
		// Apex rejected (would require a user pointing their base_url at
		// the apex domain, which is a misconfiguration)
		{"https://moma.com/v1", false},
		{"https://moma.com", false},
		// Other vendors must not match
		{"https://api.minimaxi.com/v1", false},
		{"https://api.openai.com/v1", false},
		// Wrong-spelling TLDs (e.g. "moma.io") must not match
		{"https://api.moma.io", false},
		{"https://moma.io", false},
		// Garbage
		{"", false},
		{"not-a-url", false},
	} {
		if got := IsMoMA(tc.baseURL); got != tc.want {
			t.Errorf("IsMoMA(%q) = %v, want %v", tc.baseURL, got, tc.want)
		}
	}
}

// TestIsMiniMax pins the host-matching rule for MiniMax. The spelling is
// `minimaxi`, not `minimax` — the latter is reserved for any future
// minimax-branded gateway so the two never collide.
func TestIsMiniMax(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    bool
	}{
		// Canonical
		{"https://api.minimaxi.com", true},
		{"https://api.minimaxi.com/v1", true},
		{"https://api.minimaxi.com/anthropic", true},
		// Regional subdomains under the apex
		{"https://eu.minimaxi.com/v1", true},
		{"https://us.minimaxi.com/v1", true},
		// Apex rejected
		{"https://minimaxi.com/v1", false},
		{"https://minimaxi.com", false},
		// Other vendors must not match
		{"https://jiutian.10086.cn", false},
		{"https://api.openai.com/v1", false},
		// Wrong spelling — minimax, not minimaxi — must not match
		{"https://api.minimax.com/v1", false},
		{"https://api.minimax.example.com", false},
		// Garbage
		{"", false},
		{"not-a-url", false},
	} {
		if got := IsMiniMax(tc.baseURL); got != tc.want {
			t.Errorf("IsMiniMax(%q) = %v, want %v", tc.baseURL, got, tc.want)
		}
	}
}
