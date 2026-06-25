package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zzycxz/momapeer/internal/tool"
)

// statusTool reports memory system health: counts per layer, warnings, suggestions.
type statusTool struct {
	store Store
	cfg   DecayConfig
}

// NewStatusTool returns the `memory_status` tool.
func NewStatusTool(store Store, cfg DecayConfig) tool.Tool {
	return statusTool{store: store, cfg: cfg}
}

func (statusTool) Name() string { return "memory_status" }

func (statusTool) Description() string {
	return "Report memory system health: how many facts are active/dormant/archived, " +
		"warnings about stale facts or expiring TTLs, and suggestions for maintenance. " +
		"Use at session start or when the user asks about memory health."
}

func (statusTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`)
}

func (t statusTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	cfg := t.cfg
	if cfg.DecayDays <= 0 {
		cfg = DefaultDecayConfig()
	}

	active := t.store.List()
	dormant := t.store.ListDormant()
	archived := t.store.ListArchived()

	var out strings.Builder
	out.WriteString("## Memory System Status\n\n")

	// Counts.
	out.WriteString("### Global Memory\n")
	out.WriteString(fmt.Sprintf("  Active:     %d facts  (Hot)\n", len(active)))
	out.WriteString(fmt.Sprintf("  Dormant:    %d facts  (Warm, searchable)\n", len(dormant)))
	out.WriteString(fmt.Sprintf("  Archived:   %d facts  (Cold, time-queryable)\n\n", len(archived)))

	// Health.
	out.WriteString("### Health\n")
	hotPct := 0
	if cfg.HotLimit > 0 {
		hotPct = len(active) * 100 / cfg.HotLimit
	}
	status := "healthy"
	if hotPct > 100 {
		status = "🔴 over capacity"
	} else if hotPct > 90 {
		status = "⚠️ near capacity"
	}
	out.WriteString(fmt.Sprintf("  Hot layer: %d/%d (%d%%) — %s\n", len(active), cfg.HotLimit, hotPct, status))

	// Warnings.
	now := time.Now().UTC()
	var warnings []string
	for _, m := range active {
		if m.Importance == "high" {
			continue
		}
		refTime := m.LastAccessedAt
		if refTime.IsZero() {
			refTime = m.CreatedAt
		}
		if !refTime.IsZero() {
			days := int(now.Sub(refTime).Hours() / 24)
			threshold := cfg.DecayDays
			if m.Importance == "low" {
				threshold /= 2
			}
			if days > threshold*8/10 {
				warnings = append(warnings, fmt.Sprintf("  ⚠️  Oldest active unaccessed: %s (%d days, threshold: %d)", m.Name, days, threshold))
			}
		}
		if m.TTL != "" {
			ttlDate, err := time.Parse("2006-01-02", m.TTL)
			if err == nil && ttlDate.Sub(now) < 7*24*time.Hour && ttlDate.After(now) {
				warnings = append(warnings, fmt.Sprintf("  ⚠️  TTL expiring soon: %s (expires %s)", m.Name, m.TTL))
			}
		}
	}
	if len(warnings) > 0 {
		for _, w := range warnings {
			out.WriteString(w + "\n")
		}
	} else {
		out.WriteString("  No warnings.\n")
	}

	return strings.TrimSpace(out.String()), nil
}

func (statusTool) ReadOnly() bool { return true }
