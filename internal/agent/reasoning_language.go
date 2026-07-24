package agent

import (
	"strings"
)

// reasoningLanguageContextKey is reserved for threading a per-request reasoning
// language preference through a context (sub-agents inherit it without depending
// on config). Not yet wired through every call site; included so the option
// exists without a later API break.
type reasoningLanguageContextKey struct{} //nolint:unused

// NormalizeReasoningLanguage returns one of auto|zh|en for runtime-only visible
// reasoning preferences. Keep this local to the agent package so sub-agents can
// inherit the preference without depending on config.
//
// "auto" (the default) leaves the reasoning text language up to the provider —
// it deliberately does NOT force a language, since some models reason better in
// their training-dominant language and forcing it can hurt quality.
func NormalizeReasoningLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "cn", "chinese", "中文":
		return "zh"
	case "en", "english":
		return "en"
	default:
		return "auto"
	}
}

// ReasoningLanguageBlock is transient user-turn context. It deliberately does
// not belong in the stable system prompt or tool schemas — those must stay
// byte-stable across turns so the provider's prefix cache stays warm. Injecting
// it as a per-turn prefix on the user message keeps the cache-stable prefix
// untouched while still steering the visible reasoning text language.
//
// The block only steers the THINKING/reasoning text. It explicitly does NOT
// override the user's choice for the final answer language, and it keeps code,
// identifiers, file paths, shell commands, and untranslated technical terms in
// their original form.
func ReasoningLanguageBlock(lang string) string {
	switch NormalizeReasoningLanguage(lang) {
	case "zh":
		return "<reasoning-language>\nVisible reasoning/thinking text preference: use Simplified Chinese when the provider exposes reasoning text. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form. This preference does not override an explicit user request for the final answer language.\n</reasoning-language>"
	case "en":
		return "<reasoning-language>\nVisible reasoning/thinking text preference: use English when the provider exposes reasoning text. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form. This preference does not override an explicit user request for the final answer language.\n</reasoning-language>"
	default:
		return ""
	}
}

// WithReasoningLanguage prefixes content with the transient reasoning-language
// block unless the turn already starts with an injected reasoning-language
// block. User-authored mentions of the tag later in the prompt must not suppress
// the configured preference, so only a LEADING block counts as "already
// injected". Returns content unchanged when lang is "auto" (no preference).
func WithReasoningLanguage(content, lang string) string {
	block := ReasoningLanguageBlock(lang)
	if block == "" || hasLeadingReasoningLanguageBlock(content) {
		return content
	}
	return block + "\n\n" + content
}

func hasLeadingReasoningLanguageBlock(content string) bool {
	s := strings.TrimLeft(content, " \t\r\n")
	return strings.HasPrefix(s, "<reasoning-language>")
}
