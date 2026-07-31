package main

import (
	"testing"
	"time"
)

// TestExtractLLMTime covers the response-parsing logic against the ways the
// fast_task_model deviates from the bare "YYYY-MM-DD HH:MM" instruction. This
// guards the regression where trySlice cut a fixed byte width and broke on
// seconds, prose prefixes, and markdown wrapping.
func TestExtractLLMTime(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 30, 0, 0, time.Local)
	want := time.Date(2026, 8, 14, 15, 0, 0, 0, time.Local)

	cases := []struct {
		name string
		in   string
		want time.Time
		ok   bool
	}{
		// Bare ideal-form output.
		{"bare", "2026-08-14 15:00", want, true},
		// With seconds (model sometimes pads).
		{"with_seconds", "2026-08-14 15:00:00", want, true},
		// Slash separator.
		{"slash", "2026/08/14 15:00", want, true},
		// Wrapped in quotes / markdown.
		{"quoted", `"2026-08-14 15:00"`, want, true},
		{"markdown", "**2026-08-14 15:00**", want, true},
		// Prefixed by prose despite instructions.
		{"prose_prefix", "解析结果：2026-08-14 15:00", want, true},
		{"prose_en", "The time is 2026-08-14 15:00.", want, true},
		// T separator (ISO 8601-ish).
		{"t_sep", "2026-08-14T15:00", want, true},
		// Model says not a time phrase.
		{"na", "N/A", time.Time{}, false},
		{"na_lower", "n/a", time.Time{}, false},
		// Empty / junk.
		{"empty", "", time.Time{}, false},
		{"junk", "你好世界", time.Time{}, false},
		// A phrase that isn't a time at all (model ignored instructions).
		{"not_time", "这是一个红色的物体", time.Time{}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := extractLLMTime(c.in, now)
			if ok != c.ok {
				t.Fatalf("extractLLMTime(%q) ok=%v, want %v", c.in, ok, c.ok)
			}
			if ok && !got.Equal(c.want) {
				t.Errorf("extractLLMTime(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
