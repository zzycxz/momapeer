package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestPPTActiveTemplateRoundTrip verifies the new PPT template field survives a
// render → parse cycle: RenderTOML must emit `ppt_active_template`, and toml
// must parse it back into CoworkConfig.PPTActiveTemplate. A field missing from
// either direction silently breaks the feature (user sets a template, restart,
// it's gone), so this is a focused guard on the whole cowork config path.
func TestPPTActiveTemplateRoundTrip(t *testing.T) {
	c := Default()
	c.Cowork.PPTActiveTemplate = "company-brand"

	// 1. Render → must contain the key with the value.
	out := RenderTOML(c)
	if !strings.Contains(out, `ppt_active_template = "company-brand"`) {
		t.Errorf("rendered config missing ppt_active_template key\n--- got ---\n%s", out)
	}

	// 2. Parse the rendered output back → must populate the field.
	var back Config
	if _, err := toml.Decode(out, &back); err != nil {
		t.Fatalf("re-parse rendered config: %v", err)
	}
	if back.Cowork.PPTActiveTemplate != "company-brand" {
		t.Errorf("after round-trip PPTActiveTemplate = %q, want company-brand", back.Cowork.PPTActiveTemplate)
	}
}

// TestPPTActiveTemplateEmptyRenderedAsComment ensures an unset template renders
// as a commented hint (not a stray active key), so a fresh config file is clean.
// Note: the comment line IS `# ppt_active_template = ""`, so we check there's no
// UNcommented occurrence (a real key would be `ppt_active_template = ""` at line
// start, not after `# `).
func TestPPTActiveTemplateEmptyRenderedAsComment(t *testing.T) {
	c := Default()
	c.Cowork.PPTActiveTemplate = ""
	out := RenderTOML(c)
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		// An active (uncommented) key starts with the field name, not "#".
		if !strings.HasPrefix(trimmed, "#") && strings.HasPrefix(trimmed, "ppt_active_template") {
			t.Errorf("empty template should NOT render an active key, but found: %q", line)
		}
	}
	if !strings.Contains(out, "# ppt_active_template") {
		t.Errorf("empty template should render as a commented hint, got:\n%s", out)
	}
}
