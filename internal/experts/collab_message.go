package experts

// collab_message.go defines the persisted representation of a finished expert
// collaboration, so it can be stored as a single message in the main session's
// transcript and later re-rendered as a "folded block" in the chat.
//
// Design: the whole record is JSON-serialized into the Content field of a
// provider.Message{Role: tool, Name: ExpertCollabToolName}. Storing it as a
// plain JSON *string* (not a new Content type) means it round-trips through
// the session's save/load unchanged — provider.Message.UnmarshalJSON only
// special-cases Content when it's an array; a JSON string stays a Go string.
// The chat's LLM-context layer (agent.stream) detects these messages and
// rewrites Content to a synthesis-only summary before sending to the model,
// so the full multi-expert transcript never bloats the context window.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExpertCollabToolName is the Name field stamped on the tool message that
// carries an expert collaboration record. The agent's context layer keys off
// it to swap in the synthesis-only view for the model.
const ExpertCollabToolName = "expert_team_collab"

// collabMarker is embedded in the persisted JSON so the detection is robust to
// a tool result that merely happens to carry the name. It is never shown.
const collabMarker = "__expert_collab__"

// CollabRecord is the full persisted payload of one finished collaboration:
// everything needed to re-render the folded block (per-round expert answers +
// synthesis) and to recover its provenance (team, task, mode). This is the
// "archive layer" — the "context layer" is derived from it by ContextSummary.
type CollabRecord struct {
	Marker    string           `json:"__type"` // always collabMarker
	RunID     string           `json:"runId"`
	TeamID    string           `json:"teamId"`
	TeamName  string           `json:"teamName"`
	Task      string           `json:"task"`
	Mode      string           `json:"mode"`
	Rounds    [][]ExpertAnswer `json:"rounds"`
	Synthesis string           `json:"synthesis"`
	CreatedAt int64            `json:"createdAt"` // unix ms
}

// NewCollabRecord builds a record from a run's outcome.
func NewCollabRecord(runID, teamID, teamName, task, mode string, res *CollabResult, createdAtMs int64) CollabRecord {
	r := CollabRecord{
		Marker: collabMarker, RunID: runID, TeamID: teamID, TeamName: teamName,
		Task: task, Mode: mode, CreatedAt: createdAtMs,
	}
	if res != nil {
		r.Rounds = res.Rounds
		r.Synthesis = res.Synthesis
	}
	return r
}

// MarshalContent serializes the record to a JSON string suitable for a tool
// message's Content field.
func (r CollabRecord) MarshalContent() (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("marshal collab record: %w", err)
	}
	return string(b), nil
}

// ParseCollabRecord decodes a JSON string Content back into a record. Returns
// ok=false for any input that isn't a valid collab record (wrong/missing
// marker), so callers can treat non-collab tool results transparently.
func ParseCollabRecord(content string) (CollabRecord, bool) {
	var r CollabRecord
	if err := json.Unmarshal([]byte(content), &r); err != nil {
		return CollabRecord{}, false
	}
	if r.Marker != collabMarker {
		return CollabRecord{}, false
	}
	return r, true
}

// ContextSummary is the compact text the LLM sees instead of the full
// transcript: just the synthesis, framed so the model knows its provenance.
// When synthesis is empty, a brief note is emitted so the model isn't given a
// blank tool result (which some providers reject).
func (r CollabRecord) ContextSummary() string {
	head := fmt.Sprintf("[专家团协作 · %s · %s模式]", r.TeamName, modeLabelSafe(r.Mode))
	body := strings.TrimSpace(r.Synthesis)
	if body == "" {
		return head + "\n(本次协作未产出综合结论。)"
	}
	return head + "\n" + body
}

// modeLabelSafe returns a human mode label without importing the orchestrator's
// private helper; falls back to the raw mode string for unknown values.
func modeLabelSafe(mode string) string {
	switch mode {
	case "parallel":
		return "并行"
	case "debate":
		return "辩论"
	case "pipeline":
		return "流水线"
	default:
		if mode == "" {
			return "协作"
		}
		return mode
	}
}
