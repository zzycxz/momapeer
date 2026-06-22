package provider

import "encoding/json"

// NormalizeMessages repairs a conversation history so it satisfies the tool-call
// contract the OpenAI-compatible and Anthropic APIs enforce: every assistant
// tool_calls entry must be answered by a following tool message for its id, and
// a tool message must follow such a call. It backfills a placeholder result for
// any unanswered call (so the turn stays intact), drops orphan tool messages,
// backfills empty tool-call names from their results (old sessions saved before
// name-tracking landed can carry an empty name), and closes truncated
// call-argument JSON (some gateways 400 on replayed half-streamed args).
//
// This is the wire-safe entry point for provider requests. Stored session loads
// use NormalizeSessionMessages so they can share the assistant-turn repairs
// without deleting standalone tool messages that must round-trip through resume.
//
// A well-formed history — no unanswered calls, no orphan results, no empty
// tool-call names, no truncated args — returns the input slice unchanged (same
// backing array, zero allocation). This keeps the prefix-cache key stable for
// healthy sessions and makes repeated normalization cheap.
//
// Ported from DeepSeek-Reasonix (PR #4811 unifying history normalization).
func NormalizeMessages(msgs []Message) []Message {
	return normalizeMessages(msgs, true)
}

// NormalizeSessionMessages applies only repairs that are safe to persist in a
// saved session. It shares assistant-turn repairs with NormalizeMessages, but
// preserves existing tool messages instead of dropping or reordering them so
// Save/LoadSession remains a byte-for-byte conversation round trip for
// histories that were already on disk.
func NormalizeSessionMessages(msgs []Message) []Message {
	return normalizeMessages(msgs, false)
}

func normalizeMessages(msgs []Message, dropOrphanTools bool) []Message {
	if normalized, ok := tryNormalizeFastPath(msgs, dropOrphanTools); ok {
		return normalized // well-formed: pass through without allocating
	}
	out := make([]Message, 0, len(msgs))
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			j := i + 1
			for j < len(msgs) && msgs[j].Role == RoleTool {
				j++
			}
			// Backfill empty tool-call names from the corresponding tool results
			// so the model sees which tool was invoked. The wire-format fix
			// (openai.go) ensures empty fields are never omitted, so this
			// backfill is a UX improvement, not a correctness requirement.
			calls := backfillToolCallNames(m.ToolCalls, msgs[i+1:j])
			m.ToolCalls = calls
			out = append(out, repairToolCallArgs(m))
			if dropOrphanTools {
				out = append(out, pairToolResults(calls, msgs[i+1:j])...)
			} else {
				out = append(out, sessionToolResults(calls, msgs[i+1:j])...)
			}
			i = j
			continue
		}
		if m.Role == RoleTool {
			if !dropOrphanTools {
				out = append(out, m)
			}
			// Orphan tool message: provider sends drop it; session loads preserve it.
			i++
			continue
		}
		out = append(out, m)
		i++
	}
	return out
}

// tryNormalizeFastPath reports whether msgs needs no repair and, if so, returns
// it as-is so the caller can skip allocating. Healthy tool-call/tool-result
// turns pass through unchanged; malformed turns take the slow path.
func tryNormalizeFastPath(msgs []Message, dropOrphanTools bool) ([]Message, bool) {
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			j := i + 1
			for j < len(msgs) && msgs[j].Role == RoleTool {
				j++
			}
			if !toolTurnWellFormed(m.ToolCalls, msgs[i+1:j]) || needsToolCallArgRepair(m.ToolCalls) {
				return nil, false
			}
			i = j
			continue
		}
		if m.Role == RoleTool && dropOrphanTools {
			return nil, false
		}
		i++
	}
	return msgs, true
}

func toolTurnWellFormed(calls []ToolCall, results []Message) bool {
	if len(calls) != len(results) {
		return false
	}
	for _, tc := range calls {
		if tc.Name == "" {
			return false
		}
	}
	for k, tc := range calls {
		if results[k].ToolCallID != tc.ID {
			return false
		}
	}
	return true
}

func needsToolCallArgRepair(calls []ToolCall) bool {
	for _, tc := range calls {
		if tc.Arguments != "" && !json.Valid([]byte(tc.Arguments)) {
			return true
		}
	}
	return false
}

// sessionToolResults preserves every stored tool result and appends placeholders
// only for calls that have no recorded answer. Load-time normalization must not
// drop or reorder user history; provider sends can still use pairToolResults for
// strict wire formatting.
func sessionToolResults(calls []ToolCall, avail []Message) []Message {
	out := append([]Message(nil), avail...)
	if idDistinct(calls) {
		answered := make(map[string]struct{}, len(avail))
		for _, r := range avail {
			answered[r.ToolCallID] = struct{}{}
		}
		for _, tc := range calls {
			if _, ok := answered[tc.ID]; !ok {
				out = append(out, Message{Role: RoleTool, ToolCallID: tc.ID, Name: tc.Name, Content: interruptedToolResult})
			}
		}
		return out
	}
	for k := len(avail); k < len(calls); k++ {
		tc := calls[k]
		out = append(out, Message{Role: RoleTool, ToolCallID: tc.ID, Name: tc.Name, Content: interruptedToolResult})
	}
	return out
}

// backfillToolCallNames returns calls with any empty Name filled in from the
// matching tool result (by id, then by position). Old sessions may have saved
// assistant tool-calls with an empty name; backfilling gives the model useful
// context during replay. The common case (no empty names) returns the input
// unchanged without allocating.
func backfillToolCallNames(calls []ToolCall, results []Message) []ToolCall {
	missing := false
	for _, c := range calls {
		if c.Name == "" {
			missing = true
			break
		}
	}
	if !missing {
		return calls
	}
	out := make([]ToolCall, len(calls))
	copy(out, calls)
	if idDistinct(calls) {
		byID := make(map[string]string, len(results))
		for _, r := range results {
			if r.Name != "" {
				byID[r.ToolCallID] = r.Name
			}
		}
		for k := range out {
			if out[k].Name == "" {
				if n, ok := byID[out[k].ID]; ok {
					out[k].Name = n
				}
			}
		}
		return out
	}
	for k := range out {
		if out[k].Name == "" && k < len(results) {
			out[k].Name = results[k].Name
		}
	}
	return out
}
