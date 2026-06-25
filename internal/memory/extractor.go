package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// FactExtractor pulls durable facts worth remembering out of a conversation
// turn, so the system can capture them passively rather than relying on the
// model to call `remember` explicitly. This mirrors the WorkBuddy / Trae Work
// "auto-capture memory" pattern: after each turn, a lightweight LLM pass scans
// the user message + assistant reply for user attributes, preferences, and
// time-sensitive facts, and returns them as candidate Memory records.
//
// Design constraints (shared with LLMConflictDetector):
//   - Cheap: short prompt, single call, 10s hard timeout.
//   - Fail-safe: any error (network, timeout, malformed JSON) degrades to an
//     empty result. Auto-capture must NEVER block or crash a turn.
//   - Decoupled: the Chat func is injected by boot.go so this package stays
//     free of provider imports.
type FactExtractor interface {
	// Extract returns zero or more candidate memories distilled from the last
	// user message and assistant reply. The returned memories are NOT yet saved;
	// the caller (controller turn-end hook) is responsible for persisting them
	// (with Status "pending") and surfacing them for user confirmation.
	Extract(ctx context.Context, lastUserMsg, lastAssistant string) []Memory
}

// LLMFactExtractor implements FactExtractor with a single LLM call. The model
// is asked to return a strict JSON array; anything it can't parse is dropped.
type LLMFactExtractor struct {
	// Chat sends a prompt to the LLM and returns the response text. Injected
	// by boot.go (reuses providerChatFunc, the same one-shot helper the conflict
	// detector uses). nil disables extraction entirely.
	Chat func(ctx context.Context, prompt string) (string, error)
}

// extractionResult is the JSON shape the LLM is asked to produce. Fields map
// 1:1 onto Memory's user-facing schema. The prompt instructs the model to
// return {"facts": [ ... ]} so we can detect "no facts" (empty array) vs a
// parse failure cleanly.
type extractionResult struct {
	Facts []extractedFact `json:"facts"`
}

type extractedFact struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Body        string   `json:"body"`
	ValidFrom   string   `json:"valid_from,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// Extract runs one LLM pass over the conversation tail and parses the result
// into candidate Memory records. All candidates are tagged Status "pending" so
// they are excluded from the active prompt/profile until the user confirms them
// (or the controller auto-promotes after N turns). Returns nil on any error.
func (e *LLMFactExtractor) Extract(ctx context.Context, lastUserMsg, lastAssistant string) []Memory {
	if e == nil || e.Chat == nil {
		return nil
	}
	// Skip trivial turns: a one-word user message or a tiny assistant reply
	// rarely contains a durable fact, and skipping saves a wasted LLM call.
	if strings.TrimSpace(lastUserMsg) == "" || len(strings.TrimSpace(lastAssistant)) < 40 {
		return nil
	}

	prompt := buildExtractionPrompt(lastUserMsg, lastAssistant)

	// 10s hard timeout — same budget as the conflict detector. Auto-capture is
	// background work and must never stall the foreground turn.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := e.Chat(ctx, prompt)
	if err != nil {
		return nil // degrade silently: no extraction this turn
	}

	facts := parseExtraction(resp)
	out := make([]Memory, 0, len(facts))
	for _, f := range facts {
		// Normalize type/category the same way the remember tool does, so the
		// pending record is consistent with manually-saved ones once promoted.
		m := Memory{
			Type:        NormalizeType(f.Type),
			Description: f.Description,
			Body:        f.Body,
			ValidFrom:   f.ValidFrom,
			Category:    NormalizeCategory(f.Category),
			Tags:        f.Tags,
			Status:      "pending", // exclude from active prompt until confirmed
		}
		// Derive a name from the description (Save slugifies it) if the LLM
		// didn't give one. Title is left empty; the panel de-kebabs the name.
		m.Name = f.Description
		out = append(out, m)
	}
	return out
}

// buildExtractionPrompt asks the LLM to return ONLY a JSON object with a
// "facts" array. The schema guidance mirrors the remember tool's so extracted
// facts are consistent with manually-saved ones. The prompt is in Chinese
// because the surrounding app's conflict prompt is, and the target users are
// Chinese-speaking; the LLM is multilingual regardless.
func buildExtractionPrompt(lastUserMsg, lastAssistant string) string {
	return fmt.Sprintf(`从下面的对话中提取值得长期记住的事实（用户偏好、身份属性、时间敏感信息、对助手的工作指导）。
不要提取：临时性内容、代码细节、单次任务结果、已在记忆中的内容。

用户消息:
%s

助手回复:
%s

只返回 JSON，不要任何解释或 markdown 代码块：
{"facts": [{"type": "user|feedback|project|reference", "description": "一行摘要", "body": "完整事实，Markdown", "valid_from": "YYYY-MM-DD 或留空", "category": "identity|style|belief|temporal|feedback", "tags": ["可选标签"]}]}

如果没有值得提取的事实，返回 {"facts": []}。最多提取 3 条最重要的事实。`,
		truncate(lastUserMsg, 1500),
		truncate(lastAssistant, 2500))
}

// parseExtraction extracts the JSON object from the LLM response, tolerating
// common impurities: leading/trailing prose, ```json fences, and extra text
// after the JSON. Returns nil on any parse failure (caller treats nil as
// "no facts extracted", never as an error).
func parseExtraction(raw string) []extractedFact {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	// Strip markdown code fences if present (LLMs sometimes wrap JSON despite
	// the instruction not to).
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	// Find the outermost JSON object { ... } in case the model prefixed it with
	// conversational text. We scan for the first '{' and last '}'.
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return nil
	}
	var res extractionResult
	if err := json.Unmarshal([]byte(s[start:end+1]), &res); err != nil {
		return nil
	}

	// Filter out malformed entries (missing required fields) so a single bad
	// fact doesn't invalidate the whole batch.
	cleaned := make([]extractedFact, 0, len(res.Facts))
	for _, f := range res.Facts {
		if strings.TrimSpace(f.Description) == "" || strings.TrimSpace(f.Body) == "" {
			continue
		}
		cleaned = append(cleaned, f)
	}
	return cleaned
}

// truncate keeps s to at most max runes (approximate, by bytes with rune-aware
// fallback) so the prompt stays within token limits. Long assistant replies
// (multi-K tool outputs) would otherwise blow the extraction budget.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Back off to the previous valid UTF-8 boundary.
	cut := max
	for cut > 0 && !utf8Start(s, cut) {
		cut--
	}
	return s[:cut] + "…"
}

// utf8Start reports whether the byte at index i is the start of a UTF-8
// sequence (not a continuation byte). Used by truncate to avoid splitting a
// multibyte rune.
func utf8Start(s string, i int) bool {
	if i <= 0 || i >= len(s) {
		return true
	}
	return s[i]&0xC0 != 0x80
}

// NewLLMFactExtractor creates an extractor with the given chat function.
// Pass nil to disable auto-capture entirely (the controller's turn-end hook
// checks for nil before calling).
func NewLLMFactExtractor(chat func(ctx context.Context, prompt string) (string, error)) FactExtractor {
	if chat == nil {
		return nil
	}
	return &LLMFactExtractor{Chat: chat}
}
