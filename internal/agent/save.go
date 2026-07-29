package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zzycxz/momapeer/internal/fileutil"
	"github.com/zzycxz/momapeer/internal/provider"
)

// executorHandoffMarker is the header the (now-removed) two-model Coordinator
// stamped on the message handing a task from planner to executor. HandoffTask
// still recognizes it so historical session transcripts saved under the old
// architecture surface the user's original words in previews/titles instead of
// the handoff boilerplate (#3860).
const executorHandoffMarker = "momapeer executor handoff"

// HandoffTask returns the original user task embedded in an executor handoff
// message, or s unchanged when it is not one. Session previews and auto-titles
// use it so legacy dual-model sessions surface the user's words, not the handoff
// boilerplate (#3860).
func HandoffTask(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "# "+executorHandoffMarker) {
		return s
	}
	const header = "Original task:\n"
	i := strings.Index(trimmed, header)
	if i < 0 {
		return s
	}
	rest := trimmed[i+len(header):]
	if j := strings.Index(rest, "\n\nPlanner output:"); j >= 0 {
		rest = rest[:j]
	}
	if task := strings.TrimSpace(rest); task != "" {
		return task
	}
	return s
}

// Save writes the session's messages to path in JSONL — one provider.Message
// per line — so a user can resume the conversation later. The file is
// rewritten in full on every save: chat sessions are small (kilobytes), and
// append-only would have to be reconciled with the compaction pass that
// mutates the middle of session.Messages.
func (s *Session) Save(path string) error {
	if path == "" {
		return fmt.Errorf("empty session path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	// Write to a sibling tmp file then rename, so a crash mid-write can't
	// leave a partial JSONL that won't reload.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session.*.tmp")
	if err != nil {
		return fmt.Errorf("create session tmp: %w", err)
	}
	tmpPath := tmp.Name()
	enc := json.NewEncoder(tmp)
	for _, m := range s.Snapshot() { // copy under the lock — a turn may be appending
		if err := enc.Encode(m); err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("encode message: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		return err
	}
	// Refresh the turn-count + preview cache in the .meta sidecar so ListSessions
	// reads the sidecar instead of re-decoding every .jsonl on each render. Load
	// the existing meta first to preserve its branch/scope/topic fields; only the
	// cached counts change. PreserveUpdated keeps UpdatedAt stable — Save fires
	// every turn and would otherwise churn the activity sort key.
	s.cachePreviewInMeta(path)
	return nil
}

// cachePreviewInMeta computes the user-turn count and first-user-message preview
// from the in-memory snapshot and writes them into the session's .meta sidecar.
// It is best-effort: a sidecar write failure is logged away, never propagated,
// because the cache is an optimization — ListSessions falls back to decoding the
// .jsonl when the cached fields are absent.
func (s *Session) cachePreviewInMeta(path string) {
	turns, preview := countTurnsAndPreview(s.Snapshot())
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		meta = BranchMeta{}
	}
	if meta.CachedTurns == turns && meta.CachedPreview == preview {
		return // already current; avoid an unnecessary write
	}
	meta.CachedTurns = turns
	meta.CachedPreview = preview
	_ = SaveBranchMetaPreserveUpdated(path, meta)
}

// countTurnsAndPreview mirrors previewSession's logic but reads the in-memory
// snapshot (under the session lock) instead of re-decoding the .jsonl. Returns
// the number of user-role messages and a truncated first-user-message preview.
func countTurnsAndPreview(msgs []provider.Message) (turns int, preview string) {
	for _, m := range msgs {
		if m.Role != provider.RoleUser {
			continue
		}
		turns++
		if preview == "" {
			s := strings.TrimSpace(HandoffTask(provider.ContentString(m.Content)))
			if r := []rune(s); len(r) > 80 {
				s = string(r[:77]) + "…"
			}
			preview = s
		}
	}
	return turns, preview
}

// LoadSession reads a JSONL file written by Save into a fresh Session value.
// Missing files surface as os.IsNotExist so callers can fall through to a
// new session.
func LoadSession(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &Session{}
	// Decode a stream of JSON values rather than scanning lines: a single
	// message (e.g. a multi-MiB bash output) can exceed any line-buffer cap, and
	// Save's json.Encoder has no such limit — a Scanner here made sessions that
	// saved fine fail to reload.
	dec := json.NewDecoder(f)
	for {
		var m provider.Message
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		s.Messages = append(s.Messages, m)
	}
	// Normalize right after load: repair assistant tool-call turns written by an
	// older code version or cut short mid-turn (backfill empty tool-call names
	// from results, close truncated call args, answer interrupted calls with a
	// placeholder). Well-formed histories pass through unchanged (zero alloc);
	// the repairs are persisted lazily by the next Save. See NormalizeSession.
	s.Messages = NormalizeSession(s.Messages)
	return s, nil
}

// SessionInfo summarises a saved session for the --resume picker: where it is on
// disk, when it was created/last active, the first user message as a preview, and
// a rough turn count.
type SessionInfo struct {
	Path           string
	CreatedAt      time.Time
	LastActivityAt time.Time
	ModTime        time.Time // compatibility alias for LastActivityAt
	Preview        string
	Turns          int
	Scope          string
	WorkspaceRoot  string
	TopicID        string
	TopicTitle     string
	Profile        string
}

// ListSessions returns every *.jsonl session under dir, most-recently-active
// first, each with a preview line so the picker can show something the user
// recognises. A missing directory is not an error — it just means there's
// nothing to resume yet.
func ListSessions(dir string) ([]SessionInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []SessionInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		full := filepath.Join(dir, e.Name())
		// Read the sidecar once; it carries branch/scope/topic metadata AND the
		// cached turn-count + preview (refreshed by Session.Save). When the cache
		// is present, skip the expensive .jsonl decode that previewSession does —
		// a session list with hundreds of files otherwise re-decodes them all on
		// every render.
		meta, metaOK, _ := LoadBranchMeta(full)
		var preview string
		var turns int
		if metaOK && (meta.CachedTurns > 0 || meta.CachedPreview != "") {
			preview, turns = meta.CachedPreview, meta.CachedTurns
		} else {
			preview, turns = previewSession(full)
		}
		if turns == 0 {
			// Skip sessions that have never had user interaction — they are
			// empty conversations that should not appear in the history panel
			// or the resume picker.
			continue
		}
		createdAt := info.ModTime()
		lastActivityAt := info.ModTime()
		scope := "global"
		workspaceRoot := ""
		topicID := ""
		topicTitle := ""
		profile := ""
		if metaOK {
			if !meta.CreatedAt.IsZero() {
				createdAt = meta.CreatedAt
			}
			if !meta.UpdatedAt.IsZero() {
				lastActivityAt = meta.UpdatedAt
			}
			scope = meta.DefaultScope()
			workspaceRoot = meta.WorkspaceRoot
			topicID = meta.TopicID
			topicTitle = meta.TopicTitle
			profile = meta.Profile
		}
		out = append(out, SessionInfo{
			Path:           full,
			CreatedAt:      createdAt,
			LastActivityAt: lastActivityAt,
			ModTime:        lastActivityAt,
			Preview:        preview,
			Turns:          turns,
			Scope:          scope,
			WorkspaceRoot:  workspaceRoot,
			TopicID:        topicID,
			TopicTitle:     topicTitle,
			Profile:        profile,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastActivityAt.Equal(out[j].LastActivityAt) {
			return out[i].Path < out[j].Path
		}
		return out[i].LastActivityAt.After(out[j].LastActivityAt)
	})
	return out, nil
}

// previewSession returns the first user message (truncated) and the number of
// user-role messages so the picker can show "5 turns · 'help me debug the…'".
// Errors are swallowed — a malformed file just shows up with an empty preview.
func previewSession(path string) (string, int) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	first := ""
	turns := 0
	for {
		var m provider.Message
		if err := dec.Decode(&m); err != nil {
			break // EOF or a malformed tail — return the preview gathered so far
		}
		if m.Role == provider.RoleUser {
			turns++
			if first == "" {
				s := strings.TrimSpace(HandoffTask(provider.ContentString(m.Content)))
				if r := []rune(s); len(r) > 80 {
					s = string(r[:77]) + "…"
				}
				first = s
			}
		}
	}
	return first, turns
}

// ContinueSessionPath returns where a conversation carried into a rebuilt
// controller (model switch, config change) should keep auto-saving: its existing
// file when it has one, so the continued session stays a single file instead of
// the old one being orphaned as an identical duplicate (#2807). A session with no
// file yet gets a fresh path; "" when persistence is disabled.
func ContinueSessionPath(prevPath, dir, model string) string {
	if prevPath != "" {
		return prevPath
	}
	if dir == "" {
		return ""
	}
	return NewSessionPath(dir, model)
}

// NewSessionPath returns the path to use for a fresh session, namespaced by
// the model so the filename hints at what the conversation was with. dir is
// typically config.SessionDir().
func NewSessionPath(dir, model string) string {
	safe := strings.NewReplacer("/", "-", "\\", "-").Replace(model)
	if safe == "" {
		safe = "session"
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%s.jsonl", time.Now().UTC().Format("20060102-150405.000000000"), safe))
}
