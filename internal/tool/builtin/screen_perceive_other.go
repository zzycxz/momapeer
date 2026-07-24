//go:build !windows

package builtin

import (
	"context"
	"encoding/json"
	"errors"
)

// screen_perceive is Windows-only (requires UIA COM + Win32 screen capture).
// On other platforms it returns a clear error — the rest of coWork (browser,
// RAG, docs) still works, just not desktop perception.
type screenPerceive struct{} //nolint:unused

func (screenPerceive) Name() string { return "screen_perceive" }
func (screenPerceive) Description() string {
	return "Desktop perception (screenshot + UIA labeling + VLM selection). Windows-only — requires UIA COM and Win32 screen capture."
}
func (screenPerceive) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task_hint":{"type":"string"}},"required":["task_hint"]}`)
}
func (screenPerceive) ReadOnly() bool { return true }
func (screenPerceive) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return "", errors.New("screen_perceive requires Windows (UIA COM + Win32 capture)")
}
