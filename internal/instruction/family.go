package instruction

import "strings"

// family.go adds model-family-specific prompt addons. The base DefaultSystemPrompt
// (config.go) is model-agnostic; ForModel (instruction.go) layers on top of it.
//
// Why family-level addons matter on the MoMA platform: MoMA hosts 10+ model
// families (Qwen, GLM, DeepSeek, Kimi, MiniMax, Jiutian, GPT-OSS), each with
// different tool-calling conventions, instruction-following quality, and
// known quirks. A single generic prompt works acceptably for the strongest
// models but degrades fast on weaker ones. Family addons target the specific
// failure modes observed in testing — they are concise, surgical nudges, not
// full prompt rewrites.

// ModelFamily detects the vendor family from a model ID like "qwen/qwen3.6-35b"
// or "moma/qwen/qwen3.6-35b" or bare "qwen3.6-35b".
func ModelFamily(modelID string) string {
	id := strings.ToLower(strings.TrimSpace(modelID))
	id = strings.TrimPrefix(id, "moma/") // strip moma/ prefix if present

	// Check vendor prefixes in order of specificity.
	switch {
	case strings.HasPrefix(id, "qwen/") || strings.Contains(id, "qwen"):
		return "qwen"
	case strings.HasPrefix(id, "deepseek/") || strings.Contains(id, "deepseek"):
		return "deepseek"
	case strings.HasPrefix(id, "z.ai/") || strings.Contains(id, "glm"):
		return "glm"
	case strings.HasPrefix(id, "moonshotai/") || strings.Contains(id, "kimi"):
		return "kimi"
	case strings.HasPrefix(id, "minimax/") || strings.Contains(id, "minimax"):
		return "minimax"
	case strings.HasPrefix(id, "jiutian/"):
		return "jiutian"
	case strings.HasPrefix(id, "openai/") || strings.Contains(id, "gpt"):
		return "gpt"
	default:
		return ""
	}
}

// FamilyAddon returns a model-family-specific prompt nudge, or "" when the
// family is unknown or needs no special handling. These addons are appended
// after the base system prompt + thinking/serial addons (see ForModel).
//
// Each addon targets observed failure modes:
//   - qwen: tool-call format drift (mixed prose + tool args)
//   - glm: parallel tool calls sometimes drop; serial is safer
//   - deepseek: weak instruction following on multi-step; needs explicit sequencing
//   - kimi: over-elaboration; needs conciseness guard
//   - jiutian (lan/da): domestic model line; benefits from explicit code formatting
func FamilyAddon(family string) string {
	switch family {
	case "qwen":
		return QwenAddon
	case "glm":
		return GLMAddon
	case "deepseek":
		return DeepSeekAddon
	case "kimi":
		return KimiAddon
	case "jiutian":
		return JiutianAddon
	default:
		return ""
	}
}

const QwenAddon = `When you decide to call a tool, output ONLY the tool call — do not mix prose with tool arguments in the same response. Each tool call must have well-formed JSON arguments matching the tool's schema.`

const GLMAddon = `Prefer calling one tool per message when the tools have data dependencies. If multiple independent read-only calls are safe to batch, you may, but if a tool call fails or returns unexpected output, switch to sequential calls so each result is visible before the next decision.`

const DeepSeekAddon = `Break multi-step tasks into explicit, sequential steps: complete one step, verify its result, then proceed to the next. Do not attempt to plan multiple file edits in your head without reading each result first. After each edit, confirm the change succeeded before moving on.`

const KimiAddon = `Be concise. Answer the question directly without restating it. When editing code, make the minimal change — do not refactor surrounding code unless asked. Keep explanations of your changes to one or two sentences.`

const JiutianAddon = `When writing code, use standard formatting (gofmt/rustfmt/prettier conventions for the language). Keep function signatures on one line when they fit. Avoid non-standard syntax variations.`
