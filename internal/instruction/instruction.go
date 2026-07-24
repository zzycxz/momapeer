package instruction

import (
	"context"
	"strings"

	"github.com/zzycxz/momapeer/internal/memory"
	"github.com/zzycxz/momapeer/internal/provider/openai"
)

// VerifyCheck is a host-observable project check extracted from structured
// project memory. It is runtime-only and is not serialized into prompts.
type VerifyCheck struct {
	Command    string
	SourcePath string
	Line       int
}

// ForModel returns a model-specific prompt addon based on MoMA model capabilities.
// The base DefaultSystemPrompt is model-agnostic; this function adds targeted
// instructions for models that need them (thinking models, serial-constraint models).
// Returns empty string for models that work fine with the default prompt.
func ForModel(modelID string) string {
	id := strings.ToLower(strings.TrimSpace(modelID))

	var parts []string

	// Thinking-capable models: encourage deep reasoning before tool calls.
	if openai.MoMAThinkingModels[id] {
		parts = append(parts, ThinkingAddon)
	}

	// Family-specific addon (tool-call format, serial constraints, etc.).
	if addon := FamilyAddon(ModelFamily(id)); addon != "" {
		parts = append(parts, addon)
	}

	return strings.Join(parts, "\n\n")
}

const ThinkingAddon = `You have extended thinking capability. When facing complex problems, think step by step before calling tools. Use your reasoning to plan the approach first, then execute with tools.`

const SerialAddon = `Important: Use exactly one tool per assistant message. Wait for the tool result before calling the next tool. Do not call multiple tools in parallel.`

type contextKey struct{}

func WithChecks(ctx context.Context, checks []VerifyCheck) context.Context {
	if len(checks) == 0 {
		return ctx
	}
	cp := append([]VerifyCheck(nil), checks...)
	return context.WithValue(ctx, contextKey{}, cp)
}

func FromContext(ctx context.Context) []VerifyCheck {
	checks, ok := ctx.Value(contextKey{}).([]VerifyCheck)
	if !ok || len(checks) == 0 {
		return nil
	}
	return append([]VerifyCheck(nil), checks...)
}

// ExtractHostChecks reads only the structured "momapeer host checks" section.
// Ordinary project instructions remain guidance and do not become hard gates.
func ExtractHostChecks(docs []memory.Source) []VerifyCheck {
	seen := map[string]bool{}
	var checks []VerifyCheck
	for _, doc := range docs {
		inSection := false
		for i, raw := range strings.Split(doc.Body, "\n") {
			line := strings.TrimRight(raw, "\r")
			if heading, ok := markdownHeading(line); ok {
				inSection = strings.EqualFold(heading, "momapeer host checks")
				continue
			}
			if !inSection {
				continue
			}
			command, ok := verifyBullet(line)
			if !ok || seen[command] {
				continue
			}
			seen[command] = true
			checks = append(checks, VerifyCheck{
				Command:    command,
				SourcePath: doc.Path,
				Line:       i + 1,
			})
		}
	}
	return checks
}

func markdownHeading(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#") {
		return "", false
	}
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i == 0 || i >= len(line) || line[i] != ' ' {
		return "", false
	}
	heading := strings.TrimSpace(line[i+1:])
	heading = strings.TrimSpace(strings.TrimRight(heading, "#"))
	return heading, heading != ""
}

func verifyBullet(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if len(line) < 2 || (line[:2] != "- " && line[:2] != "* ") {
		return "", false
	}
	body := strings.TrimSpace(line[2:])
	const prefix = "verify:"
	if len(body) < len(prefix) || !strings.EqualFold(body[:len(prefix)], prefix) {
		return "", false
	}
	command := strings.TrimSpace(body[len(prefix):])
	return command, command != ""
}
