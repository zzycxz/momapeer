package openai

import (
	"strings"
	"testing"

	"github.com/zzycxz/momapeer/internal/provider"
)

func newClient(t *testing.T, baseURL, effort string) *client {
	t.Helper()
	extra := map[string]any{}
	if effort != "" {
		extra["effort"] = effort
	}
	// Use a model from MoMAThinkingModels so the thinking branch is taken
	p, err := New(provider.Config{Name: "p", BaseURL: baseURL, Model: "jiutian/jiutian-lan-thinking", APIKey: "k", Extra: extra})
	if err != nil {
		t.Fatalf("New(%q, effort=%q): %v", baseURL, effort, err)
	}
	return p.(*client)
}

func TestEffortNormalization(t *testing.T) {
	const MoMA = "https://example.com"
	const moma = "https://jiutian.10086.cn/v1"

	tests := []struct {
		base, effort, want string
	}{
		{MoMA, "max", "high"}, // MoMA-ism clamped to the OpenAI ceiling — MoMA 400s on "max"
		{MoMA, "high", "high"},
		{MoMA, "medium", "medium"},
		{MoMA, "low", "low"},
		{MoMA, "MAX", "high"}, // case-insensitive
		{MoMA, "auto", ""},    // UI/config auto means omit provider-specific effort
		{MoMA, "", ""},        // unset stays omitted
		{moma, "max", "high"}, // max clamped to high (16/18 MoMA models reject)
		{moma, "high", "high"},
		{moma, "medium", "medium"},
		{moma, "low", "medium"}, // low clamped to medium (2/18 MoMA models reject)
		{moma, "auto", "high"},
		{moma, "", "high"}, // MoMA default depth
	}
	for _, tc := range tests {
		if got := newClient(t, tc.base, tc.effort).effort; got != tc.want {
			t.Errorf("base=%s effort=%q: got %q, want %q", tc.base, tc.effort, got, tc.want)
		}
	}
}

func TestEffortInvalidRejected(t *testing.T) {
	_, err := New(provider.Config{
		Name: "p", BaseURL: "https://example.com", Model: "m", APIKey: "k",
		Extra: map[string]any{"effort": "turbo"},
	})
	if err == nil || !strings.Contains(err.Error(), "low, medium, or high") {
		t.Fatalf("expected a low/medium/high validation error, got: %v", err)
	}
}

func TestReasoningProtocolOverridesEndpointHeuristic(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "momaxy",
		BaseURL: "https://proxy.example.com/v1",
		Model:   "qwen3.6-35b",
		APIKey:  "k",
		Extra:   map[string]any{"reasoning_protocol": "MoMA"},
	})
	if err != nil {
		t.Fatalf("New MoMA protocol: %v", err)
	}
	c := p.(*client)
	if !c.moma || c.effort != "high" {
		t.Fatalf("MoMA=%v effort=%q, want true/high", c.moma, c.effort)
	}

	p, err = New(provider.Config{
		Name:    "MoMA-direct",
		BaseURL: "https://example.com",
		Model:   "qwen3.6-35b",
		APIKey:  "k",
		Extra:   map[string]any{"reasoning_protocol": "none", "effort": "max"},
	})
	if err != nil {
		t.Fatalf("New none protocol: %v", err)
	}
	c = p.(*client)
	if c.moma || c.effort != "" {
		t.Fatalf("MoMA=%v effort=%q, want false/empty", c.moma, c.effort)
	}
}
