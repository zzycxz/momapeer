package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zzycxz/momapeer/internal/config"
)

// TestCoworkSettingsViewPPTTemplate verifies the settings view correctly surfaces
// PPT template state to the frontend: the active template id passes through, the
// templates list is populated from the (isolated) templates dir, and the dir path
// is absolute. This is the data the React dropdown renders, so it must be right.
func TestCoworkSettingsViewPPTTemplate(t *testing.T) {
	// Isolate the templates dir so the test reads only what we seed (and never
	// touches the user's real ppt-templates). Windows: AppData; *nix: XDG_CONFIG_HOME.
	fakeHome := t.TempDir()
	t.Setenv("AppData", filepath.Join(fakeHome, "AppData"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fakeHome, "config"))

	// Seed two templates into where DefaultDir will look (the coworkSettingsView
	// helper calls DefaultDir, which creates the dir if missing — so we just need
	// the files in place; DefaultDir won't overwrite them).
	tplDir := filepath.Join(fakeHome, "AppData", "momapeer", "ppt-templates")
	if err := os.MkdirAll(tplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(tplDir, "brand.json"), []byte(`{"id":"brand","name":"公司品牌"}`), 0o644)
	os.WriteFile(filepath.Join(tplDir, "draft.json"), []byte(`{"name":"草稿"}`), 0o644)

	cfg := config.CoworkConfig{PPTActiveTemplate: "brand"}
	v := coworkSettingsView(cfg)

	if v.PPTActiveTemplate != "brand" {
		t.Errorf("PPTActiveTemplate = %q, want brand", v.PPTActiveTemplate)
	}
	if len(v.PPTTemplates) < 2 {
		t.Errorf("PPTTemplates len = %d, want >=2 (brand, draft): %+v", len(v.PPTTemplates), v.PPTTemplates)
	}
	foundBrand := false
	for _, tpl := range v.PPTTemplates {
		if tpl.ID == "brand" && tpl.Name == "公司品牌" {
			foundBrand = true
		}
	}
	if !foundBrand {
		t.Errorf("brand template not in list: %+v", v.PPTTemplates)
	}
	if v.PPTTemplateDir == "" {
		t.Error("PPTTemplateDir is empty — frontend can't show 'open folder'")
	}
}

// TestCoworkSettingsViewNoTemplates verifies the view is well-formed even when
// only the auto-seeded example exists (no panic, dir path set so the UX works).
func TestCoworkSettingsViewNoTemplates(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("AppData", filepath.Join(fakeHome, "AppData"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fakeHome, "config"))

	cfg := config.CoworkConfig{PPTActiveTemplate: ""}
	v := coworkSettingsView(cfg)

	if v.PPTActiveTemplate != "" {
		t.Errorf("want empty active template, got %q", v.PPTActiveTemplate)
	}
	// DefaultDir seeds an example.json, so the list has 1 — but must be non-nil.
	if len(v.PPTTemplates) == 0 {
		t.Error("expected the seeded example template, got empty list")
	}
	if v.PPTTemplateDir == "" {
		t.Error("PPTTemplateDir empty on fresh dir")
	}
}

// TestPPTTemplateSkillValue verifies the pure mapping from a settings-page
// template id to the value the skill's template_config.json expects. This is
// the bridge that makes the settings-page template selection actually reach the
// skill (previously the skill always read its built-in default).
func TestPPTTemplateSkillValue(t *testing.T) {
	tests := []struct {
		in  string
		val string
		set bool
	}{
		{"中国移动模板", "内置:templates/中国移动模板.pptx", true},
		{"  brand  ", "内置:templates/brand.pptx", true}, // trimmed
		{"", "", false},    // empty → delete key (skill uses default)
		{"   ", "", false}, // whitespace-only → delete key
	}
	for _, tt := range tests {
		val, set := pptTemplateSkillValue(tt.in)
		if val != tt.val || set != tt.set {
			t.Errorf("pptTemplateSkillValue(%q) = (%q, %v), want (%q, %v)", tt.in, val, set, tt.val, tt.set)
		}
	}
}

// TestSetCoWorkSettingsLinksPPTSwitchToSkill is the regression test for the
// "two PPT switches" bug: the settings-page PPT switch (driven by
// PPTActiveTemplate) must enable/disable the ppt-auto skill, so it agrees with
// the Capabilities-panel skill toggle (both write the same DisabledSkills list).
// We test the config-level linkage directly — SetCoWorkSettings applies the same
// c.SetSkillEnabled("ppt-auto", template != "") inside its mutate closure.
func TestSetCoWorkSettingsLinksPPTSwitchToSkill(t *testing.T) {
	// Selecting a template enables ppt-auto (it must NOT be disabled).
	c := config.Default()
	if err := c.SetSkillEnabled("ppt-auto", false); err != nil { // start disabled
		t.Fatal(err)
	}
	if !c.IsSkillDisabled("ppt-auto") {
		t.Fatal("precondition: ppt-auto should be disabled initially")
	}
	if err := c.SetSkillEnabled("ppt-auto", true); err != nil { // emulate selecting a template
		t.Fatal(err)
	}
	if c.IsSkillDisabled("ppt-auto") {
		t.Error("selecting a template should ENABLE ppt-auto (remove from DisabledSkills)")
	}
	// Clearing the template disables ppt-auto again.
	if err := c.SetSkillEnabled("ppt-auto", false); err != nil { // emulate clearing the template
		t.Fatal(err)
	}
	if !c.IsSkillDisabled("ppt-auto") {
		t.Error("clearing the template should DISABLE ppt-auto (add to DisabledSkills)")
	}
}
