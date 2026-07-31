package scheduler

import (
	"testing"
	"time"
)

// fixedNow is a stable "now" for deterministic tests: Monday 2026-06-22 10:00 local.
func fixedNow() time.Time {
	return time.Date(2026, 6, 22, 10, 0, 0, 0, time.Local) // Monday
}

func TestParseAt(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"at 2026-06-24 15:00", false},
		{"at 2026/06/24 15:04", false},
		{"at 2026-06-24 15:04:05", false},
		{"at 2026-06-24", true}, // missing time
		{"at bogus", true},
		{"at ", true},
	}
	for _, c := range cases {
		_, err := parseAt(c.in)
		if c.wantErr && err == nil {
			t.Errorf("parseAt(%q) should error", c.in)
		}
		if !c.wantErr && err != nil {
			t.Errorf("parseAt(%q): %v", c.in, err)
		}
	}
}

func TestNextRunAt(t *testing.T) {
	now := fixedNow()
	// Future instant → returns that instant.
	got := nextRun("at 2026-06-24 15:00", now)
	want := time.Date(2026, 6, 24, 15, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("at future: got %v, want %v", got, want)
	}
	// Past instant → zero (no future fire).
	got = nextRun("at 2020-01-01 00:00", now)
	if !got.IsZero() {
		t.Errorf("at past: got %v, want zero", got)
	}
}

func TestNormalizeInExpression(t *testing.T) {
	now := fixedNow()
	cases := []struct {
		in   string
		want string
	}{
		{"in 2h", "at 2026-06-22 12:00"},
		{"in 1d", "at 2026-06-23 10:00"},
		{"in 3d", "at 2026-06-25 10:00"},
		{"in 1w", "at 2026-06-29 10:00"},
		{"in 2d3h", "at 2026-06-24 13:00"},
	}
	for _, c := range cases {
		got, err := NormalizeExpression(c.in, now)
		if err != nil {
			t.Errorf("NormalizeExpression(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeExpression(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Raw "in" rejected by parseExpression (must go through NormalizeExpression).
	if _, err := parseExpression("in 2h"); err == nil {
		t.Error("parseExpression should reject raw \"in\" — must normalize first")
	}
	// Non-"in" passes through parseExpression.
	if _, err := NormalizeExpression("daily 09:00", now); err != nil {
		t.Errorf("NormalizeExpression(daily 09:00): %v", err)
	}
}

func TestIsOneShot(t *testing.T) {
	if !IsOneShot("at 2026-06-24 15:00") {
		t.Error("at ... should be one-shot")
	}
	if IsOneShot("daily 09:00") {
		t.Error("daily should not be one-shot")
	}
	if IsOneShot("every 1h") {
		t.Error("every should not be one-shot")
	}
}

// --- ResolveRelativeTime ----------------------------------------------------

func TestResolveRelativeTimeOffsets(t *testing.T) {
	now := fixedNow()
	cases := []struct {
		name string
		in   string
		want time.Time
	}{
		{"明天", "明天下午3点", time.Date(2026, 6, 23, 15, 0, 0, 0, time.Local)},
		{"后天", "后天上午9点半", time.Date(2026, 6, 24, 9, 30, 0, 0, time.Local)},
		{"大后天", "大后天 18:00", time.Date(2026, 6, 25, 18, 0, 0, 0, time.Local)},
		{"今天未来时间", "今天下午3点", time.Date(2026, 6, 22, 15, 0, 0, 0, time.Local)},
		{"今天已过时间", "今天早上8点", time.Date(2026, 6, 23, 8, 0, 0, 0, time.Local)}, // rolls to tomorrow
		{"仅时间未来", "下午3点", time.Date(2026, 6, 22, 15, 0, 0, 0, time.Local)},
		{"仅时间已过", "早上8点", time.Date(2026, 6, 23, 8, 0, 0, 0, time.Local)}, // rolls to tomorrow
		{"24小时制", "15:30", time.Date(2026, 6, 22, 15, 30, 0, 0, time.Local)},
		{"月底", "月底 23:59", lastDayOfMonth(now, 23, 59)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveRelativeTime(c.in, now)
			if err != nil {
				t.Fatalf("ResolveRelativeTime(%q): %v", c.in, err)
			}
			if !got.Equal(c.want) {
				t.Errorf("ResolveRelativeTime(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestResolveRelativeTimeBareMinutes guards the regression where a bare
// "N点M" (no 分 suffix) silently dropped the minutes — "8点50" parsed as 8:00.
// People write this form constantly, so both the bare and period variants, with
// and without 分, must keep the minute component.
func TestResolveRelativeTimeBareMinutes(t *testing.T) {
	now := fixedNow() // 2026-06-22 10:00
	cases := []struct {
		name string
		in   string
		want time.Time
	}{
		// Bare "N点M" (no 分) — the originally broken case.
		{"bare 点M未来", "8点50", time.Date(2026, 6, 23, 8, 50, 0, 0, time.Local)}, // past → rolls to tomorrow
		{"bare 点M已过", "明天8点50", time.Date(2026, 6, 23, 8, 50, 0, 0, time.Local)},
		// Period + 点M (no 分).
		{"下午点M", "下午3点20", time.Date(2026, 6, 22, 15, 20, 0, 0, time.Local)},
		{"晚上点M", "晚上10点15", time.Date(2026, 6, 22, 22, 15, 0, 0, time.Local)},
		// With 分 suffix must still work (didn't regress).
		{"bare 点M分", "8点50分", time.Date(2026, 6, 23, 8, 50, 0, 0, time.Local)},
		{"下午点M分", "下午3点20分", time.Date(2026, 6, 22, 15, 20, 0, 0, time.Local)},
		// Half hour unaffected.
		{"点半", "9点半", time.Date(2026, 6, 23, 9, 30, 0, 0, time.Local)},
		// Mixed sentence where the time phrase is embedded in free text.
		// now is 10:00, so 8:50 today has passed → future-guard rolls to tomorrow
		// (matches the existing "今天已过时间" semantics).
		{"嵌入句子", "今天8点50提醒我去开会", time.Date(2026, 6, 23, 8, 50, 0, 0, time.Local)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveRelativeTime(c.in, now)
			if err != nil {
				t.Fatalf("ResolveRelativeTime(%q): %v", c.in, err)
			}
			if !got.Equal(c.want) {
				t.Errorf("ResolveRelativeTime(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestResolveRelativeTimeWeekday(t *testing.T) {
	now := fixedNow() // Monday 2026-06-22
	cases := []struct {
		name string
		in   string
		want time.Time
	}{
		// Today is Monday. "下周一" = next week's Monday = 2026-06-29.
		{"下周一", "下周一上午9点", time.Date(2026, 6, 29, 9, 0, 0, 0, time.Local)},
		// "下周三" = next week's Wednesday = 2026-07-01.
		{"下周三", "下周三 10:00", time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local)},
		// Bare "周三" = the coming Wednesday = 2026-06-24.
		{"周三", "周三 14:00", time.Date(2026, 6, 24, 14, 0, 0, 0, time.Local)},
		// Bare "周一" from Monday = rolls a full week (today's Monday is "now").
		{"周一从周一开始", "周一 8:00", time.Date(2026, 6, 29, 8, 0, 0, 0, time.Local)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveRelativeTime(c.in, now)
			if err != nil {
				t.Fatalf("ResolveRelativeTime(%q): %v", c.in, err)
			}
			if !got.Equal(c.want) {
				t.Errorf("ResolveRelativeTime(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestResolveRelativeTimeInvalid(t *testing.T) {
	now := fixedNow()
	bad := []string{
		"", "你好", "随便写写", "颜色是红色",
	}
	for _, in := range bad {
		if _, err := ResolveRelativeTime(in, now); err == nil {
			t.Errorf("ResolveRelativeTime(%q) should error", in)
		}
	}
}

func TestResolveRelativeTimeFullDate(t *testing.T) {
	now := fixedNow()
	got, err := ResolveRelativeTime("2026年12月25日 10:00", now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 12, 25, 10, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("full date: got %v, want %v", got, want)
	}
}

func lastDayOfMonth(now time.Time, hour, min int) time.Time {
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	last := first.AddDate(0, 1, -1)
	return time.Date(last.Year(), last.Month(), last.Day(), hour, min, 0, 0, now.Location())
}
