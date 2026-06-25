package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zzycxz/momapeer/internal/tool"
)

// compactTool triggers memory compaction: downgrades low-access facts to dormant
// and archives long-dormant cold data.
type compactTool struct {
	store Store
	cfg   DecayConfig
}

// NewCompactTool returns the `memory_compact` tool.
func NewCompactTool(store Store, cfg DecayConfig) tool.Tool {
	return compactTool{store: store, cfg: cfg}
}

func (compactTool) Name() string { return "memory_compact" }

func (compactTool) Description() string {
	return "Compact the memory store by expiring TTL-bounded facts, downgrading rarely-accessed facts to dormant, and archiving old dormant data. " +
		"Use when the memory index is growing large or the system suggests compaction. " +
		"Step 0: TTL past today → archived. " +
		"Step A: access_count=0 and importance!=high → dormant. " +
		"Step B: dormant facts older than cold_days → archived. " +
		"The process is reversible: superseded/archived facts are kept in .archive/ and can be recalled."
}

func (compactTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`)
}

func (t compactTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	cfg := t.cfg
	if cfg.DecayDays <= 0 {
		cfg = DefaultDecayConfig()
	}

	var out strings.Builder
	out.WriteString("## Memory Compaction\n\n")

	// Step 0: Expire TTL. Hard-expired facts are archived before decay so they
	// never enter the dormant layer (a TTL is an explicit, user-set expiry and
	// should be honored first). Previously ExpireTTL had no caller at all, so
	// time-bounded facts silently lived forever.
	expiredN, err := t.store.ExpireTTL()
	if err != nil {
		return "", fmt.Errorf("expire-ttl step: %w", err)
	}
	out.WriteString(fmt.Sprintf("Step 0 — Expire TTL: %d fact(s) past their TTL archived.\n", expiredN))

	// Step A: Decay inactive facts.
	dormantN, err := t.store.Decay(cfg)
	if err != nil {
		return "", fmt.Errorf("decay step: %w", err)
	}
	out.WriteString(fmt.Sprintf("Step A — Decay: %d fact(s) downgraded to dormant.\n", dormantN))

	// Step B: Archive cold dormant facts.
	coldN, err := t.archiveDormant(cfg.ColdDays)
	if err != nil {
		return "", fmt.Errorf("archive step: %w", err)
	}
	out.WriteString(fmt.Sprintf("Step B — Archive: %d dormant fact(s) older than %d days archived.\n", coldN, cfg.ColdDays))

	// Summary.
	active := t.store.List()
	out.WriteString(fmt.Sprintf("\nResult: %d active facts remaining.\n", len(active)))

	if expiredN == 0 && dormantN == 0 && coldN == 0 {
		out.WriteString("No compaction needed — memory is already lean.\n")
	}

	return strings.TrimSpace(out.String()), nil
}

// archiveDormant moves dormant facts older than coldDays to .archive/.
// Uses UpdatedAt (set to now when Decay made it dormant) as reference, so a
// freshly-dormant fact is not immediately archived even if CreatedAt is old.
func (t compactTool) archiveDormant(coldDays int) (int, error) {
	if coldDays <= 0 {
		coldDays = 90
	}
	now := time.Now().UTC()
	var count int
	for _, m := range t.store.ListDormant() {
		refTime := m.UpdatedAt // when it became dormant
		if refTime.IsZero() {
			refTime = m.LastAccessedAt
		}
		if refTime.IsZero() {
			refTime = m.CreatedAt
		}
		if refTime.IsZero() {
			continue
		}
		if now.Sub(refTime) > time.Duration(coldDays)*24*time.Hour {
			if _, err := t.store.Archive(slug(m.Name)); err == nil {
				count++
			}
		}
	}
	return count, nil
}

func (compactTool) ReadOnly() bool { return false }
