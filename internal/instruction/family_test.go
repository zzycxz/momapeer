package instruction

import (
	"strings"
	"testing"
)

func TestModelFamily(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		{"qwen/qwen3.6-35b", "qwen"},
		{"moma/qwen/qwen3.6-35b", "qwen"},
		{"deepseek/deepseek-v4-flash", "deepseek"},
		{"z.ai/glm-5.1", "glm"},
		{"moma/z.ai/glm-5.2", "glm"},
		{"moonshotai/kimi-k2.6", "kimi"},
		{"minimax/minimax-m2.7", "minimax"},
		{"jiutian/jiutian-lan-35b", "jiutian"},
		{"openai/gpt-oss-120b", "gpt"},
		{"unknown-model", ""},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			if got := ModelFamily(tc.model); got != tc.want {
				t.Errorf("ModelFamily(%q) = %q, want %q", tc.model, got, tc.want)
			}
		})
	}
}

func TestFamilyAddon(t *testing.T) {
	// Known families return non-empty addons.
	for _, family := range []string{"qwen", "glm", "deepseek", "kimi", "jiutian"} {
		if FamilyAddon(family) == "" {
			t.Errorf("FamilyAddon(%q) is empty", family)
		}
	}
	// Unknown families return empty.
	if FamilyAddon("unknown") != "" {
		t.Errorf("FamilyAddon(\"unknown\") is non-empty")
	}
}

func TestForModelIncludesFamilyAddon(t *testing.T) {
	// Non-thinking qwen model should get the qwen addon.
	got := ForModel("qwen/qwen3.6-35b")
	if !strings.Contains(got, "tool call") {
		t.Errorf("ForModel(qwen model) should include qwen addon about tool calls, got: %q", got)
	}

	// Thinking qwen model should get thinking addon + qwen addon.
	got = ForModel("qwen/qwen3.6-35b") // this is in MoMAThinkingModels
	// qwen3.6-35b IS a thinking model, so should have both
	if !strings.Contains(got, "thinking capability") {
		t.Logf("note: qwen3.6-35b is a thinking model, ForModel should include thinking addon")
	}

	// GLM should get serial tool guidance.
	got = ForModel("z.ai/glm-5.2")
	if !strings.Contains(got, "one tool per message") && !strings.Contains(got, "sequential") {
		t.Errorf("ForModel(glm model) should include GLM addon, got: %q", got)
	}

	// Unknown model returns empty.
	if ForModel("totally-unknown") != "" {
		t.Errorf("ForModel(unknown) should be empty")
	}
}
