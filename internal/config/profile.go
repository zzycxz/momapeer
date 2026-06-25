package config

import (
	"fmt"
	"strings"
)

// Profile is a named bundle of boot.Options overrides that switches the whole
// Controller between product modes — today "dev" (coding) and "cowork" (office).
// A profile does NOT replace configuration; it layers on top of it:
//
//   - Model / SubagentModel / Effort override the resolved provider knobs, so a
//     coWork profile can pin a cheaper/faster model without touching momapeer.toml.
//   - SystemPromptAddon is appended to the resolved system prompt (after the
//     instruction/output-style/memory/skill folding in boot.Build), so a profile
//     can bias behaviour (e.g. "you are an office agent") without owning the
//     whole prompt. Empty means no change.
//   - DisabledSkills / EnabledSkills flip skill availability. EnabledSkills is a
//     whitelist: when non-empty, only those (plus anything the profile does not
//     name) — see ResolveSkillDisabled for the exact merge. For Phase 0 both
//     stay empty so the skill set is unchanged.
//   - Plugins whitelists which [[plugins]] entries are visible. Empty = all
//     plugins (unchanged behaviour), so dev stays dev until coWork opts in.
//   - WorkspaceType is a frontend hint ("code" | "document") that selects the
//     layout; the backend ignores it. It rides the profile so the switch is
//     atomic across Go rebuild + React layout.
//
// Design rationale: profile switching reuses the proven SetModelForTab rebuild
// flow (acquire shared host → snapshot history → Close → boot.Build → Resume).
// A profile is therefore just "a richer set of boot.Options inputs" — not a new
// runtime concept. Everything here is resolved once in config and consumed in
// boot.Build / desktop.app.
type Profile struct {
	Name              string            `toml:"name"`
	DisplayName       string            `toml:"display_name"`
	Model             string            `toml:"model"`              // overrides DefaultModel; "" = config default
	SubagentModel     string            `toml:"subagent_model"`     // overrides agent.subagent_model; "" = unchanged
	Effort            string            `toml:"effort"`             // overrides effort; "" = provider default
	SystemPromptAddon string            `toml:"system_prompt_addon"` // appended to resolved prompt; "" = unchanged
	SystemPromptFile  string            `toml:"system_prompt_file"`  // when set, replaces the resolved prompt entirely
	EnabledSkills     []string          `toml:"enabled_skills"`      // whitelist; empty = all skills
	DisabledSkills    []string          `toml:"disabled_skills"`     // extra-disabled on top of config
	Plugins           []string          `toml:"plugins"`             // plugin name whitelist; empty = all plugins
	WorkspaceType     string            `toml:"workspace_type"`      // "code" | "document"; frontend hint only
}

const (
	// ProfileDev is the built-in coding mode. It mirrors the unprofiled behaviour
	// exactly (empty overrides), so a config with no [[profiles]] is effectively
	// always in dev. Resolving "dev" therefore always succeeds and never mutates
	// the Controller's tool/skill/plugin set beyond what config already declares.
	ProfileDev = "dev"
	// ProfileCowork is the office mode. For Phase 0 it is intentionally a thin
	// shell: a prompt addon that biases the model toward office tasks, but the
	// SAME tool/skill/plugin set as dev. Real coWork capabilities (browser,
	// desktop automation) arrive in later phases and turn on here.
	ProfileCowork = "cowork"
)

// builtinProfiles are the always-available profiles. They are the floor: a
// [[profiles]] entry in momapeer.toml with the same name overrides the builtin,
// so users can customise cowork's model or prompt without forking code.
func builtinProfiles() []Profile {
	return []Profile{
		{
			Name:        ProfileDev,
			DisplayName: "编码",
			// All overrides empty: identical to no profile.
		},
		{
			Name:              ProfileCowork,
			DisplayName:       "办公",
			WorkspaceType:     "document",
			SystemPromptAddon: coworkDefaultPromptAddon,
			// Phase 0: tool/skill/plugin set unchanged. Later phases will whitelist
			// cowork-only skills (browser-auto, computer-auto, …) here.
		},
	}
}

// coworkDefaultPromptAddon biases the resolved system prompt toward office work.
// Kept short on purpose: it layers onto the full coding prompt, so it should
// nudge rather than overwrite. It describes intent, not a hard tool list — the
// actual tools depend on platform (screen_* are Windows-only), host (scheduler/
// rag need the desktop app's store), and config (ppt_* need wps_ppt_server_path,
// email_* need [cowork.smtp]). The model sees the real tool registry, so this
// addon just sets priorities; if a capability isn't registered, the model won't
// (and can't) call it.
const coworkDefaultPromptAddon = "# Mode: coWork\n\nYou are operating in coWork (office) mode. Prioritize practical office outcomes — documents, slides, spreadsheets, browser and desktop automation, research, and scheduled/recurring tasks — over software-engineering work. Prefer producing or operating on real artifacts (files, pages, applications) over long chat explanations. Not every office capability is available in every environment (some depend on platform, config, or a running desktop app) — use the tools that are present and, when a task needs a capability you don't have, say so rather than pretending. When a task needs UI you cannot reach via tools, say so rather than pretending."

// DefaultProfiles returns the profiles effective when momapeer.toml declares no
// [[profiles]]. The caller (Config.Profiles resolution) merges user entries on
// top of these by name.
func DefaultProfiles() []Profile { return builtinProfiles() }

// ProfileNameKey normalizes a profile identifier for comparisons. Profile names
// are case- and whitespace-insensitive so "Cowork" / "COWORK" / "cowork" all
// resolve the same. Empty stays empty (resolved to ProfileDev upstream).
func ProfileNameKey(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.ToLower(name)
}

// ResolveProfile returns the effective profile for name, or an error if the name
// is unknown. The builtin floor is merged with the config's [[profiles]] entries
// (config wins on name collision). Empty name resolves to ProfileDev so callers
// that never set a profile get unprofiled behaviour. The returned *Profile is a
// copy of the merged entry; mutating it does not affect the Config.
func (c *Config) ResolveProfile(name string) (*Profile, error) {
	key := ProfileNameKey(name)
	if key == "" {
		key = ProfileDev
	}
	// User config entries override builtins by name.
	for i := range c.Profiles {
		if ProfileNameKey(c.Profiles[i].Name) == key {
			p := c.Profiles[i]
			p.Name = key
			if p.DisplayName == "" {
				p.DisplayName = p.Name
			}
			return &p, nil
		}
	}
	for _, b := range builtinProfiles() {
		if ProfileNameKey(b.Name) == key {
			p := b
			return &p, nil
		}
	}
	return nil, fmt.Errorf("unknown profile %q (available: %s)", name, c.profileNames())
}

// profileNames lists the effective profile names (builtins + configured), for
// error messages.
func (c *Config) profileNames() string {
	seen := map[string]bool{}
	var names []string
	for _, b := range builtinProfiles() {
		k := ProfileNameKey(b.Name)
		if !seen[k] {
			seen[k] = true
			names = append(names, b.Name)
		}
	}
	for _, p := range c.Profiles {
		k := ProfileNameKey(p.Name)
		if k != "" && !seen[k] {
			seen[k] = true
			names = append(names, p.Name)
		}
	}
	return strings.Join(names, ", ")
}

// IsProfileKnown reports whether name resolves to a profile (builtin or
// configured). Empty returns true (resolves to dev).
func (c *Config) IsProfileKnown(name string) bool {
	_, err := c.ResolveProfile(name)
	return err == nil
}

// PluginAllowedByProfile reports whether pluginName is visible under profile p.
// An empty p.Plugins list means "all plugins allowed" (the dev default), so a
// profile that does not opt into plugin filtering keeps the full MCP set. When
// p is nil (no profile), all plugins are allowed.
func PluginAllowedByProfile(p *Profile, pluginName string) bool {
	if p == nil || len(p.Plugins) == 0 {
		return true
	}
	target := strings.TrimSpace(pluginName)
	for _, n := range p.Plugins {
		if strings.TrimSpace(n) == target {
			return true
		}
	}
	return false
}

// ResolveSkillDisabled merges the config-wide disabled-skill set with a profile's
// skill overrides and returns the effective disabled set (skill name key → true).
//
// Merge rules:
//   - Start from cfg.DisabledSkillNames() (the [skills].disabled config).
//   - Profile.DisabledSkills is additive (a profile can disable more).
//   - Profile.EnabledSkills, when non-empty, is a whitelist: any skill NOT in it
//     is disabled. This lets a future cowork profile expose only office skills.
//     Empty EnabledSkills (Phase 0) means "no whitelist, keep all".
//
// The returned map uses SkillNameKey normalization so it composes with the
// existing config-disabled set regardless of platform case rules.
func (p *Profile) ResolveSkillDisabled(configDisabled []string) map[string]bool {
	out := make(map[string]bool)
	for _, n := range configDisabled {
		if k := SkillNameKey(n); k != "" {
			out[k] = true
		}
	}
	if p == nil {
		return out
	}
	for _, n := range p.DisabledSkills {
		if k := SkillNameKey(n); k != "" {
			out[k] = true
		}
	}
	if len(p.EnabledSkills) > 0 {
		whitelist := make(map[string]bool, len(p.EnabledSkills))
		for _, n := range p.EnabledSkills {
			if k := SkillNameKey(n); k != "" {
				whitelist[k] = true
			}
		}
		// Any disabled entry the profile explicitly re-enables is removed.
		for k := range out {
			if whitelist[k] {
				delete(out, k)
			}
		}
		// We cannot enumerate "all skills" here to disable the rest; that
		// happens in boot.go where the full skill list is known. This map only
		// carries the additive config + profile-disabled set; the whitelist
		// enforcement point is boot.go (see applyProfileToSkills).
	}
	return out
}
