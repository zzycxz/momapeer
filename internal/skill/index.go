package skill

import (
	"fmt"
	"strings"
)

// IndexMaxChars caps the pinned skills-index block so it can't bloat the
// cache-stable system-prompt prefix; bodies never enter the prefix. (MoMA
// currently does not report cache tokens; the prefix stability still helps.)
const IndexMaxChars = 4000

const missingDescPlaceholder = `(no description — frontmatter is missing a "description:" line; tell the user to add one)`

// indexHeader introduces the skills block in the system prompt: the invocation
// policy (mandatory for inline, judgment-based for subagent) and how to call one.
// Kept compact: every token here goes into the system prompt every turn, so
// only the rules the model can't infer from the index entries themselves are
// stated. The index lines (name + description + tag) carry the specifics.
const indexHeader = "# Skills\n\n" +
	"Call `run_skill({ name: \"<name>\", arguments: \"<task>\" })` — name is the identifier only (e.g. `\"explore\"`), not the tag. Users can also invoke via `/<name>`.\n" +
	"- Untagged (inline): body loads as a tool result you act on directly. Invoke on plausible relevance before pre-judging — loading one is cheap.\n" +
	"- `[🧬 subagent]`: spawns an isolated agent; its reasoning/tool calls stay out of your context, only the final answer comes back. Use for context-heavy work, not weak relevance.\n" +
	"- `[关闭]`: disabled by user — not callable. If a task fits a disabled skill, tell the user to enable it in Settings → Skills.\n" +
	"Prefer the dedicated top-level tool when one exists for a built-in subagent skill."

// ApplyIndex appends the skills index to basePrompt, or returns it unchanged
// when there are no skills. Only names + descriptions (+ a subagent tag) are
// listed; bodies load on demand via run_skill.
func ApplyIndex(basePrompt string, skills []Skill) string {
	if len(skills) == 0 {
		return basePrompt
	}
	lines := make([]string, 0, len(skills))
	for _, sk := range skills {
		lines = append(lines, indexLine(sk))
	}
	joined := strings.Join(lines, "\n")
	if r := []rune(joined); len(r) > IndexMaxChars {
		joined = string(r[:IndexMaxChars]) + fmt.Sprintf("\n… (truncated %d chars)", len(r)-IndexMaxChars)
	}
	return basePrompt + "\n\n" + indexHeader + "\n\n```\n" + joined + "\n```"
}

// indexLine renders one skill as "- name [tag] — description", clipped to a
// stable width. The subagent tag goes after the name so a model copying the line
// into run_skill's `name` arg still yields a clean identifier.
func indexLine(sk Skill) string {
	desc := strings.TrimSpace(strings.ReplaceAll(sk.Description, "\n", " "))
	if desc == "" {
		desc = missingDescPlaceholder
	}
	tag := ""
	if sk.RunAs == RunSubagent {
		tag = " [🧬 subagent]"
	}
	if sk.Disabled {
		tag += " [关闭]"
	}
	max := 130 - len([]rune(sk.Name)) - len([]rune(tag))
	clipped := clipRunes(desc, max)
	if clipped == "" {
		return "- " + sk.Name + tag
	}
	return "- " + sk.Name + tag + " — " + clipped
}

// clipRunes truncates s to at most max runes (ellipsis included), never
// splitting a multi-byte rune.
func clipRunes(s string, max int) string {
	if max < 1 {
		max = 1
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max-1 < 1 {
		return string(r[:1])
	}
	return string(r[:max-1]) + "…"
}
