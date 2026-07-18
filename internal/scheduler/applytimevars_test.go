package scheduler

import (
	"strings"
	"testing"
	"time"
)

func TestApplyTimeVars(t *testing.T) {
	// 2026-06-10 is a Wednesday. With week = Mon..Sun, week_start=2026-06-08,
	// week_end=2026-06-14; month_start=2026-06-01.
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 6, 10, 14, 30, 0, 0, loc)

	out := applyTimeVars("today={today} week={week_start}..{week_end} m={month_start} lm={last_month_start}..{last_month_end}", now)

	if !strings.Contains(out, "today=2026-06-10") {
		t.Fatalf("today token wrong: %s", out)
	}
	if !strings.Contains(out, "week=2026-06-08..2026-06-14") {
		t.Fatalf("week tokens wrong: %s", out)
	}
	if !strings.Contains(out, "m=2026-06-01") {
		t.Fatalf("month_start wrong: %s", out)
	}
	// Last month = May 2026 → 2026-05-01 .. 2026-05-31.
	if !strings.Contains(out, "lm=2026-05-01..2026-05-31") {
		t.Fatalf("last_month tokens wrong: %s", out)
	}
}

func TestApplyTimeVarsSundayWeekStart(t *testing.T) {
	// Sunday should map to weekday=7, giving week_start = the previous Monday.
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, loc) // 2026-06-14 is a Sunday
	out := applyTimeVars("{week_start}..{week_end}", now)
	// Week containing Sun 2026-06-14 starts Mon 2026-06-08, ends Sun 2026-06-14.
	if !strings.Contains(out, "2026-06-08..2026-06-14") {
		t.Fatalf("sunday week math wrong: %s", out)
	}
}

func TestApplyTimeVarsUnknownLeftAsIs(t *testing.T) {
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	out := applyTimeVars("keep {unknown} and {today}", now)
	if !strings.Contains(out, "{unknown}") {
		t.Fatalf("unknown token should be preserved: %s", out)
	}
	if !strings.Contains(out, "and 2026-06-10") {
		t.Fatalf("known token beside unknown should still resolve: %s", out)
	}
}
