package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zzycxz/momapeer/internal/fileutil"
)

// BranchMeta is the small sidecar record that turns flat session files into a
// navigable conversation tree. The conversation itself remains in the .jsonl
// file; metadata lives beside it at <session>.meta.
type BranchMeta struct {
	ID               string    `json:"id"`
	Name             string    `json:"name,omitempty"`
	ParentID         string    `json:"parent_id,omitempty"`
	ForkTurn         int       `json:"fork_turn,omitempty"`
	ForkMessageIndex int       `json:"fork_message_index,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Scope            string    `json:"scope,omitempty"`
	WorkspaceRoot    string    `json:"workspace_root,omitempty"`
	TopicID          string    `json:"topic_id,omitempty"`
	TopicTitle       string    `json:"topic_title,omitempty"`
	// Profile records the product mode ("dev"|"cowork") this session was created
	// under. Empty (on legacy sidecars) is treated as "dev". It lets findTopic*
	// scope its scan so a dev session is never matched as a cowork one and vice
	// versa, even before topic storage is fully partitioned.
	Profile string `json:"profile,omitempty"`
	// CachedTurns/CachedPreview mirror what previewSession computes by decoding
	// the .jsonl. Session.Save refreshes them so ListSessions reads the sidecar
	// instead of re-decoding every session file on each render. Older readers
	// ignore these fields; older sidecars without them fall back to the decode.
	// Ported from DeepSeek-Reasonix perf(sessions) work (#4882/#4886).
	CachedTurns   int    `json:"cached_turns,omitempty"`
	CachedPreview string `json:"cached_preview,omitempty"`
	// PlanMode records whether plan mode (read-only gate) was active when the
	// session was last saved, so Resume can restore it. Restoring plan mode is
	// safe (it only restricts writes, never starts background work). See C8.
	PlanMode bool `json:"plan_mode,omitempty"`
	// ToolApprovalMode records the writer-tool approval stance ("ask"/"auto"/
	// "yolo") so Resume can restore it. Restoring YOLO is intentional — if the
	// user had auto-approve on, they expect it to stay on across a restart.
	ToolApprovalMode string `json:"tool_approval_mode,omitempty"`
	// NOTE: Goal is intentionally NOT persisted here. Restoring a non-empty goal
	// would auto-resume the goal loop (continueGoal), re-running a pursuit the
	// user may have manually stopped. Goal state is ephemeral; users re-enter it
	// via /goal when they want to resume. See C8 risk discussion.
}

func (m BranchMeta) DefaultScope() string {
	switch m.Scope {
	case "project":
		return "project"
	default:
		return "global"
	}
}

// BranchInfo combines sidecar metadata with the session file details needed for
// pickers and tree rendering.
type BranchInfo struct {
	BranchMeta
	Path    string
	ModTime time.Time
	Preview string
	Turns   int
}

func BranchID(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

func BranchMetaPath(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionPath + ".meta"
}

func LoadBranchMeta(sessionPath string) (BranchMeta, bool, error) {
	metaPath := BranchMetaPath(sessionPath)
	if metaPath == "" {
		return BranchMeta{}, false, nil
	}
	b, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return BranchMeta{}, false, nil
		}
		return BranchMeta{}, false, err
	}
	var m BranchMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return BranchMeta{}, false, fmt.Errorf("decode branch meta %s: %w", metaPath, err)
	}
	if m.ID == "" {
		m.ID = BranchID(sessionPath)
	}
	return m, true, nil
}

func SaveBranchMeta(sessionPath string, m BranchMeta) error {
	return saveBranchMeta(sessionPath, m, true)
}

func SaveBranchMetaPreserveUpdated(sessionPath string, m BranchMeta) error {
	return saveBranchMeta(sessionPath, m, false)
}

func saveBranchMeta(sessionPath string, m BranchMeta, touchUpdated bool) error {
	metaPath := BranchMetaPath(sessionPath)
	if metaPath == "" {
		return fmt.Errorf("empty session path")
	}
	now := time.Now().UTC()
	if m.ID == "" {
		m.ID = BranchID(sessionPath)
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	if touchUpdated || m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(metaPath), ".branch.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return fileutil.ReplaceFile(tmpPath, metaPath)
}

func EnsureBranchMeta(sessionPath string) (BranchMeta, error) {
	if sessionPath == "" {
		return BranchMeta{}, fmt.Errorf("empty session path")
	}
	if m, ok, err := LoadBranchMeta(sessionPath); err != nil || ok {
		return m, err
	}
	when := time.Now().UTC()
	if info, err := os.Stat(sessionPath); err == nil {
		when = info.ModTime().UTC()
	}
	m := BranchMeta{
		ID:        BranchID(sessionPath),
		CreatedAt: when,
		UpdatedAt: when,
	}
	return m, saveBranchMeta(sessionPath, m, false)
}

func TouchBranchMeta(sessionPath string) error {
	m, err := EnsureBranchMeta(sessionPath)
	if err != nil {
		return err
	}
	m.UpdatedAt = time.Now().UTC()
	return saveBranchMeta(sessionPath, m, false)
}

func ListBranches(dir string) ([]BranchInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []BranchInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, e.Name())
		meta, ok, err := LoadBranchMeta(path)
		if err != nil {
			continue
		}
		// Use the sidecar cache when available (same pattern as ListSessions
		// in save.go). Older sidecars without CachedTurns/CachedPreview fall
		// back to the full .jsonl decode.
		var preview string
		var turns int
		if ok && (meta.CachedTurns > 0 || meta.CachedPreview != "") {
			preview, turns = meta.CachedPreview, meta.CachedTurns
		} else {
			preview, turns = previewSession(path)
		}
		if turns == 0 {
			continue
		}
		if !ok {
			meta = BranchMeta{
				ID:        BranchID(path),
				CreatedAt: info.ModTime().UTC(),
				UpdatedAt: info.ModTime().UTC(),
			}
		}
		if meta.ID == "" {
			meta.ID = BranchID(path)
		}
		out = append(out, BranchInfo{
			BranchMeta: meta,
			Path:       path,
			ModTime:    info.ModTime(),
			Preview:    preview,
			Turns:      turns,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// RenameSession updates the topic title of the session at sessionPath.
func RenameSession(sessionPath string, title string) error {
	if sessionPath == "" {
		return fmt.Errorf("empty session path")
	}
	m, err := EnsureBranchMeta(sessionPath)
	if err != nil {
		return err
	}
	m.TopicTitle = title
	return SaveBranchMeta(sessionPath, m)
}
