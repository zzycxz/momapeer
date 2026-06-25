package main

import (
	"testing"
	"time"

	"github.com/zzycxz/momapeer/internal/memory"
)

// TestMemoryFactViewMapsBitemporalFields verifies the DTO mapper carries the
// bitemporal fields (ValidFrom/ValidTo/Status/Category/Tags/SupersededBy/
// CreatedAt/UpdatedAt) through to the frontend. This is the contract the
// timeline view depends on — a regression here silently breaks the timeline.
func TestMemoryFactViewMapsBitemporalFields(t *testing.T) {
	created := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	src := memory.Memory{
		Name:         "lives-in-beijing",
		Title:        "Lives in Beijing",
		Description:  "User lives in Beijing.",
		Type:         memory.TypeUser,
		Body:         "User lives in Beijing.",
		ValidFrom:    "2026-03-01",
		ValidTo:      "2026-04-30",
		Status:       "superseded",
		Category:     "temporal",
		Tags:         []string{"location", "address"},
		SupersededBy: "lives-in-shanghai",
		CreatedAt:    created,
		UpdatedAt:    updated,
	}

	got := memoryFactView(src)

	if got.ValidFrom != "2026-03-01" {
		t.Errorf("ValidFrom = %q, want 2026-03-01", got.ValidFrom)
	}
	if got.ValidTo != "2026-04-30" {
		t.Errorf("ValidTo = %q, want 2026-04-30", got.ValidTo)
	}
	if got.Status != "superseded" {
		t.Errorf("Status = %q, want superseded", got.Status)
	}
	if got.Category != "temporal" {
		t.Errorf("Category = %q, want temporal", got.Category)
	}
	if got.SupersededBy != "lives-in-shanghai" {
		t.Errorf("SupersededBy = %q, want lives-in-shanghai", got.SupersededBy)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "location" || got.Tags[1] != "address" {
		t.Errorf("Tags = %v, want [location address]", got.Tags)
	}
	if got.CreatedAt != created.Format(time.RFC3339) {
		t.Errorf("CreatedAt = %q, want %q", got.CreatedAt, created.Format(time.RFC3339))
	}
	if got.UpdatedAt != updated.Format(time.RFC3339) {
		t.Errorf("UpdatedAt = %q, want %q", got.UpdatedAt, updated.Format(time.RFC3339))
	}
}

// TestMemoryFactViewOmitsZeroTime ensures a zero CreatedAt/UpdatedAt serializes
// to "" (via omitempty), not the Go zero-time sentinel "0001-01-01T00:00:00Z".
// The timeline's "valid from" labels would otherwise render bogus 0001 dates.
func TestMemoryFactViewOmitsZeroTime(t *testing.T) {
	got := memoryFactView(memory.Memory{Name: " timeless", Type: memory.TypeUser})
	if got.CreatedAt != "" {
		t.Errorf("CreatedAt = %q, want empty for zero time", got.CreatedAt)
	}
	if got.UpdatedAt != "" {
		t.Errorf("UpdatedAt = %q, want empty for zero time", got.UpdatedAt)
	}
}

// TestMemoryFactViewPreservesBasics is a sanity check that the original 5
// fields still map correctly after the DTO expansion.
func TestMemoryFactViewPreservesBasics(t *testing.T) {
	src := memory.Memory{
		Name:        "prefers-tabs",
		Title:       "Prefers tabs",
		Description: "User prefers tabs",
		Type:        memory.TypeUser,
		Body:        "Indent with tabs.",
	}
	got := memoryFactView(src)
	if got.Name != src.Name || got.Title != src.Title || got.Description != src.Description ||
		got.Type != "user" || got.Body != src.Body {
		t.Errorf("basic fields not preserved: %+v", got)
	}
}
