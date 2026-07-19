package experts

import (
	"testing"
)

// TestBuiltinTeamsInvariants validates the BuiltinTeams seed rosters so a typo
// (missing ID, duplicate name within a team, illegal mode) can't slip in. These
// run on every CI build; the builtins are shipped to all users via the
// seedBuiltinTeamsInto migration, so they must be self-consistent.
func TestBuiltinTeamsInvariants(t *testing.T) {
	if len(BuiltinTeams) == 0 {
		t.Fatal("BuiltinTeams must not be empty — the panel would be blank")
	}

	seenIDs := make(map[string]int, len(BuiltinTeams))
	validModes := map[string]bool{"parallel": true, "debate": true, "pipeline": true}

	for i, tm := range BuiltinTeams {
		// ID: non-empty, unique, stable builtin_ prefix (the migration relies on
		// stable IDs to detect "already seeded").
		if tm.ID == "" {
			t.Fatalf("BuiltinTeams[%d] (%s): ID is empty", i, tm.Name)
		}
		if seenIDs[tm.ID] > 0 {
			t.Fatalf("BuiltinTeams[%d]: duplicate ID %q", i, tm.ID)
		}
		seenIDs[tm.ID]++

		// Name: non-empty.
		if tm.Name == "" {
			t.Fatalf("BuiltinTeams[%d] (%s): Name is empty", i, tm.ID)
		}

		// Experts: at least one (Store.Create rejects empty rosters).
		if len(tm.Experts) == 0 {
			t.Fatalf("BuiltinTeams[%d] (%s): no experts", i, tm.ID)
		}

		// Expert names MUST be unique within a team — the frontend CollabStream
		// reducer keys streamed chunks by (expertName, round), so duplicates would
		// mis-accumulate into the wrong message card.
		seenNames := make(map[string]bool, len(tm.Experts))
		for j, ex := range tm.Experts {
			if ex.Name == "" {
				t.Fatalf("BuiltinTeams[%d] (%s): expert[%d] has empty Name", i, tm.ID, j)
			}
			if seenNames[ex.Name] {
				t.Fatalf("BuiltinTeams[%d] (%s): duplicate expert name %q (would break CollabStream)", i, tm.ID, ex.Name)
			}
			seenNames[ex.Name] = true
			if ex.Perspective == "" {
				t.Fatalf("BuiltinTeams[%d] (%s): expert %q has empty Perspective", i, tm.ID, ex.Name)
			}
			// Model is intentionally left empty — resolved to the active provider at
			// runtime (desktopExpertRunner.resolveEntry). We don't assert on it.
		}

		// DefaultMode: must be a value the orchestrator's Run switch handles.
		// Store.Create defaults empty to "debate", but builtins ship explicit values.
		if !validModes[tm.DefaultMode] {
			t.Fatalf("BuiltinTeams[%d] (%s): illegal DefaultMode %q", i, tm.ID, tm.DefaultMode)
		}

		// DefaultRounds: positive; debate rounds are clamped 1-5 in the UI, but the
		// backend accepts any positive int — we just sanity-check > 0.
		if tm.DefaultRounds <= 0 {
			t.Fatalf("BuiltinTeams[%d] (%s): DefaultRounds must be > 0, got %d", i, tm.ID, tm.DefaultRounds)
		}
		// Rounds only matters for debate; pipeline/parallel ignore it. Don't over-
		// constrain — just flag implausibly high defaults.
		if tm.DefaultMode == "debate" && tm.DefaultRounds > 5 {
			t.Fatalf("BuiltinTeams[%d] (%s): debate DefaultRounds %d exceeds UI max 5", i, tm.ID, tm.DefaultRounds)
		}
	}
}

// TestBuiltinTeamsExpectedIDs pins the stable builtin IDs that the
// seedBuiltinTeamsInto migration keys off of. If someone renames a builtin ID,
// the migration would re-insert the team under the new ID while leaving the old
// one orphaned in existing users' stores. This test makes such a rename loud.
func TestBuiltinTeamsExpectedIDs(t *testing.T) {
	want := []string{
		"builtin_review",
		"builtin_brainstorm",
		"builtin_doc",
		"builtin_data",
		"builtin_translate",
		"builtin_meeting",
		"builtin_project",
		"builtin_email",
		"builtin_college_major",
		"builtin_event_predict",
		"builtin_research",
		"builtin_legal",
		"builtin_medical",
		"builtin_exam_plan",
		"builtin_stock",
		"builtin_operation",
		"builtin_dev_arch",
	}
	got := make([]string, 0, len(BuiltinTeams))
	for _, tm := range BuiltinTeams {
		got = append(got, tm.ID)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d builtin teams, got %d (%v)", len(want), len(got), got)
	}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("BuiltinTeams[%d]: expected ID %q, got %q (order matters — migration depends on stable IDs)", i, id, got[i])
		}
	}
}

// TestBuiltinTeamsAllowSearch pins the AllowSearch design intent: the three
// scenario teams that need real-time data opt in (true), and everything else
// stays on the fast one-shot path (false). If someone flips a team's flag by
// mistake, this catches it — a wrong default either makes a research team
// answer stale, or makes a translation team burn the RPM budget on useless
// searches.
func TestBuiltinTeamsAllowSearch(t *testing.T) {
	wantSearch := map[string]bool{
		"builtin_college_major": true,
		"builtin_event_predict": true,
		"builtin_research":      true,
	}
	for _, tm := range BuiltinTeams {
		got := tm.AllowSearch
		want, listed := wantSearch[tm.ID]
		if listed {
			if !got || !want {
				t.Errorf("team %q should have AllowSearch=true (it needs real-time data)", tm.ID)
			}
		} else {
			if got {
				t.Errorf("team %q should have AllowSearch=false (default; user can enable in panel)", tm.ID)
			}
		}
	}
}
