package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestAgentSubagentModelConfigDecodesFromTOML(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(`
[agent]
subagent_model = "moma"
subagent_models = { explore = "moma", "security-review" = "moma" }
`, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if cfg.Agent.SubagentModel != "moma" {
		t.Fatalf("subagent_model = %q, want moma", cfg.Agent.SubagentModel)
	}
	if cfg.Agent.SubagentModels["explore"] != "moma" {
		t.Fatalf("explore model = %q", cfg.Agent.SubagentModels["explore"])
	}
	if cfg.Agent.SubagentModels["security-review"] != "moma" {
		t.Fatalf("security-review model = %q", cfg.Agent.SubagentModels["security-review"])
	}
	if cfg.Agent.SubagentEffort != "" {
		t.Fatalf("subagent_effort should be empty by default, got %q", cfg.Agent.SubagentEffort)
	}
	if len(cfg.Agent.SubagentEfforts) != 0 {
		t.Fatalf("subagent_efforts should be empty by default, got %v", cfg.Agent.SubagentEfforts)
	}
}

func TestAgentSubagentEffortConfigDecodesFromTOML(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(`
	[agent]
	subagent_effort = "max"
	subagent_efforts = { review = "max", task = "high" }
	`, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if cfg.Agent.SubagentEffort != "max" {
		t.Fatalf("subagent_effort = %q, want max", cfg.Agent.SubagentEffort)
	}
	if cfg.Agent.SubagentEfforts["review"] != "max" {
		t.Fatalf("review effort = %q", cfg.Agent.SubagentEfforts["review"])
	}
	if cfg.Agent.SubagentEfforts["task"] != "high" {
		t.Fatalf("task effort = %q", cfg.Agent.SubagentEfforts["task"])
	}
}
