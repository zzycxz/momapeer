package memory

import (
	"context"
	"errors"
	"testing"
)

// stubChat is a test double for the extractor's Chat func: it returns a fixed
// response, recording the prompt it was called with.
type stubChat struct {
	resp string
	err  error
	last string
}

func (s *stubChat) chat(_ context.Context, prompt string) (string, error) {
	s.last = prompt
	if s.err != nil {
		return "", s.err
	}
	return s.resp, nil
}

// TestExtractParsesValidJSON verifies the happy path: a well-formed JSON
// response yields Memory records with normalized type/category and Status
// "pending" (so they stay out of the active prompt until confirmed).
func TestExtractParsesValidJSON(t *testing.T) {
	stub := &stubChat{resp: `{"facts": [
		{"type": "user", "description": "Lives in Shanghai", "body": "User moved to Shanghai in May 2026.", "valid_from": "2026-05-01", "category": "temporal", "tags": ["location"]},
		{"type": "feedback", "description": "Prefers concise replies", "body": "**Why:** Values efficiency.\n**How to apply:** Keep answers short.", "category": "feedback"}
	]}`}
	ex := NewLLMFactExtractor(stub.chat)

	got := ex.Extract(context.Background(), "I just moved to Shanghai", "Welcome! I'll keep that in mind and keep my replies concise from now on. Is there anything specific...")
	if len(got) != 2 {
		t.Fatalf("got %d facts, want 2", len(got))
	}
	if got[0].Type != TypeUser {
		t.Errorf("fact[0].Type = %q, want user", got[0].Type)
	}
	if got[0].Status != "pending" {
		t.Errorf("fact[0].Status = %q, want pending (excluded from active prompt)", got[0].Status)
	}
	if got[0].ValidFrom != "2026-05-01" {
		t.Errorf("fact[0].ValidFrom = %q, want 2026-05-01", got[0].ValidFrom)
	}
	if got[0].Category != "temporal" {
		t.Errorf("fact[0].Category = %q, want temporal", got[0].Category)
	}
}

// TestExtractDegradesOnLLMError confirms the fire-and-forget contract: a Chat
// error (network, timeout) yields nil, never a propagated error. This is the
// invariant that lets the turn-end hook call Extract without fear of crashing
// the foreground turn.
func TestExtractDegradesOnLLMError(t *testing.T) {
	stub := &stubChat{err: errors.New("network timeout")}
	ex := NewLLMFactExtractor(stub.chat)
	got := ex.Extract(context.Background(), "a real user message here", "a sufficiently long assistant reply that exceeds the forty character threshold for extraction")
	if got != nil {
		t.Errorf("expected nil on Chat error, got %v", got)
	}
}

// TestExtractSkipsTrivialTurns verifies that short / empty turns are skipped
// without calling the LLM at all (saves a wasted API call).
func TestExtractSkipsTrivialTurns(t *testing.T) {
	stub := &stubChat{resp: `{"facts": []}`}
	ex := NewLLMFactExtractor(stub.chat)

	// Empty user message.
	ex.Extract(context.Background(), "", "long enough assistant reply to pass the threshold")
	if stub.last != "" {
		t.Error("Chat should not be called for empty user message")
	}

	// Short assistant reply (under 40 chars).
	stub.last = ""
	ex.Extract(context.Background(), "hi", "ok")
	if stub.last != "" {
		t.Error("Chat should not be called for a trivial short turn")
	}
}

// TestExtractHandlesMarkdownFences confirms the parser tolerates LLMs that
// wrap the JSON in ```json ... ``` despite the instruction not to.
func TestExtractHandlesMarkdownFences(t *testing.T) {
	stub := &stubChat{resp: "Here are the facts:\n```json\n{\"facts\": [{\"type\": \"user\", \"description\": \"d\", \"body\": \"b\"}]}\n```\nHope that helps!"}
	ex := NewLLMFactExtractor(stub.chat)
	got := ex.Extract(context.Background(), "real message", "a sufficiently long assistant reply exceeding forty characters total")
	if len(got) != 1 {
		t.Fatalf("got %d facts from fenced JSON, want 1", len(got))
	}
	if got[0].Description != "d" {
		t.Errorf("Description = %q, want d", got[0].Description)
	}
}

// TestExtractHandlesProsePrefix confirms the parser finds the JSON object even
// when the model prefixes it with conversational text.
func TestExtractHandlesProsePrefix(t *testing.T) {
	stub := &stubChat{resp: `I found one fact: {"facts": [{"type": "user", "description": "name", "body": "User's name is Zhang."}]}`}
	ex := NewLLMFactExtractor(stub.chat)
	got := ex.Extract(context.Background(), "real message", "a sufficiently long assistant reply exceeding forty characters total")
	if len(got) != 1 {
		t.Fatalf("got %d facts from prose-prefixed JSON, want 1", len(got))
	}
}

// TestExtractEmptyArrayIsZeroFacts confirms {"facts": []} yields an empty
// (non-nil-but-length-0) slice, distinct from a parse failure (nil).
func TestExtractEmptyArrayIsZeroFacts(t *testing.T) {
	stub := &stubChat{resp: `{"facts": []}`}
	ex := NewLLMFactExtractor(stub.chat)
	got := ex.Extract(context.Background(), "real message", "a sufficiently long assistant reply exceeding forty characters total")
	if got == nil {
		t.Fatal("expected empty slice, got nil (indistinguishable from parse failure)")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 facts, got %d", len(got))
	}
}

// TestExtractFiltersMalformedEntries verifies that a batch with one good and
// one bad fact (missing body) yields only the good one, not a total failure.
func TestExtractFiltersMalformedEntries(t *testing.T) {
	stub := &stubChat{resp: `{"facts": [
		{"type": "user", "description": "good", "body": "a real fact"},
		{"type": "user", "description": "bad, no body", "body": ""}
	]}`}
	ex := NewLLMFactExtractor(stub.chat)
	got := ex.Extract(context.Background(), "real message", "a sufficiently long assistant reply exceeding forty characters total")
	if len(got) != 1 {
		t.Fatalf("got %d facts, want 1 (malformed entry dropped)", len(got))
	}
	if got[0].Description != "good" {
		t.Errorf("kept the wrong fact: %q", got[0].Description)
	}
}

// TestExtractNilChatDisabled confirms a nil Chat func (or nil extractor)
// disables extraction entirely and returns nil without panicking.
func TestExtractNilChatDisabled(t *testing.T) {
	ex := NewLLMFactExtractor(nil)
	if ex != nil {
		t.Fatal("NewLLMFactExtractor(nil) should return nil (disabled)")
	}
}
