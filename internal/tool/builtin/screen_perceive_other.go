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
type screenPerceive struct{}

//nolint:unused // Windows-only stub; methods are used on Windows builds.
func (screenPerceive) Name() string { return "screen_perceive" }

//nolint:unused
func (screenPerceive) Description() string {
	return "Desktop perception (screenshot + UIA labeling + VLM selection). Windows-only — requires UIA COM and Win32 screen capture."
}

//nolint:unused
func (screenPerceive) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task_hint":{"type":"string"}},"required":["task_hint"]}`)
}

//nolint:unused
func (screenPerceive) ReadOnly() bool { return true }

//nolint:unused
func (screenPerceive) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return "", errors.New("screen_perceive requires Windows (UIA COM + Win32 capture)")
}
