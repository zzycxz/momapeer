package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ConflictDetector determines whether a new memory contradicts an existing one.
// When a conflict is detected, the old memory should be superseded.
type ConflictDetector interface {
	// Detect returns true if newMem makes oldMem obsolete.
	Detect(ctx context.Context, old, new Memory) bool
}

// LLMConflictDetector uses a lightweight LLM call to judge whether two memories
// contradict each other. It is designed to be cheap (short prompt, small model,
// 10s timeout) and degrades gracefully: on any error it returns false (no
// conflict), so a save is never blocked by detection failure.
type LLMConflictDetector struct {
	// Chat sends a prompt to the LLM and returns the response text.
	// Injected by boot.go to decouple the memory package from provider details.
	Chat func(ctx context.Context, prompt string) (string, error)
}

// Detect sends a short comparison prompt to the LLM and interprets the response.
// Only the body and valid_from are compared — metadata differences (type, title)
// are not conflicts.
func (d *LLMConflictDetector) Detect(ctx context.Context, old, new Memory) bool {
	if d == nil || d.Chat == nil {
		return false // no detector configured; skip
	}
	// Only detect conflicts for user/project types (the ones most likely to
	// contain mutable real-world facts).
	if old.Type != TypeUser && old.Type != TypeProject {
		return false
	}

	prompt := fmt.Sprintf(`判断以下两条记忆是否矛盾（一条使另一条过时）：
A: %s（生效时间：%s）
B: %s（生效时间：%s）
如果 B 使 A 过时，回答 "conflict"。如果可以共存，回答 "compatible"。只回答一个词。`,
		oneLine(old.Body), orEmpty(old.ValidFrom),
		oneLine(new.Body), orEmpty(new.ValidFrom))

	// 10s timeout; on failure degrade to "no conflict".
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := d.Chat(ctx, prompt)
	if err != nil {
		return false // degrade: don't block the save
	}
	return strings.TrimSpace(strings.ToLower(resp)) == "conflict"
}

func orEmpty(s string) string {
	if s == "" {
		return "无"
	}
	return s
}

// NewLLMConflictDetector creates a detector with the given chat function.
// Pass nil to disable conflict detection entirely.
func NewLLMConflictDetector(chat func(ctx context.Context, prompt string) (string, error)) ConflictDetector {
	if chat == nil {
		return nil
	}
	return &LLMConflictDetector{Chat: chat}
}
