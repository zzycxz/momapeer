package scheduler

import (
	"testing"
	"time"
)

// TestNormalizeExpressionChineseNL guards the fix where Create/Update errored on
// save for Chinese natural-language phrases the preview had already resolved.
// NormalizeExpression now falls back to ResolveRelativeTime, so "9点50" / "后天
// 下午3点" store as concrete "at ..." one-shots instead of failing parse.
func TestNormalizeExpressionChineseNL(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.Local) // 09:50 today is in the future
	cases := []struct {
		name string
		in   string
		// wantPrefix: the normalized form must start with "at 2026-"
		wantPrefix string
	}{
		{"bare 点M", "9点50", "at 2026-07-30 09:50"},
		{"today future", "今天下午3点", "at 2026-07-30 15:00"},
		{"tomorrow", "明天上午9点半", "at 2026-07-31 09:30"},
		{"embedded sentence", "9点50给我提醒", "at 2026-07-30 09:50"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeExpression(c.in, now)
			if err != nil {
				t.Fatalf("NormalizeExpression(%q): %v", c.in, err)
			}
			if got != c.wantPrefix {
				t.Errorf("NormalizeExpression(%q) = %q, want %q", c.in, got, c.wantPrefix)
			}
			if !IsOneShot(got) {
				t.Errorf("NormalizeExpression(%q) result %q should be one-shot", c.in, got)
			}
		})
	}
}

// TestNormalizeExpressionStillRejectsGarbage ensures the NL fallback doesn't make
// the parser accept everything — genuinely non-time input still errors.
// (Note: ResolveRelativeTime is intentionally lenient with bare date words like
// "今天" — it parses any text containing a date/time word, so "今天吃什么" becomes
// "today 00:00". We test with inputs that have NO date/time word at all.)
func TestNormalizeExpressionStillRejectsGarbage(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.Local)
	for _, in := range []string{"你好世界", "random text", "asdjfkl", "测试一下"} {
		if _, err := NormalizeExpression(in, now); err == nil {
			t.Errorf("NormalizeExpression(%q) should error, got nil", in)
		}
	}
}

// TestNormalizeExpressionKnownFormsUnchanged ensures the NL fallback is a pure
// addition — known forms (every/daily/at/cron) still parse exactly as before.
func TestNormalizeExpressionKnownFormsUnchanged(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.Local)
	cases := []struct {
		in   string
		want string
	}{
		{"every 30m", "every 30m"},
		{"daily 09:00", "daily 09:00"},
		{"daily 09:00 Mon-Fri", "daily 09:00 Mon-Fri"},
		{"0 9 * * 1-5", "0 9 * * 1-5"},
	}
	for _, c := range cases {
		got, err := NormalizeExpression(c.in, now)
		if err != nil {
			t.Fatalf("NormalizeExpression(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("NormalizeExpression(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// "in 2h" still normalizes to absolute "at ...".
	got, err := NormalizeExpression("in 2h", now)
	if err != nil {
		t.Fatalf("NormalizeExpression(in 2h): %v", err)
	}
	if got != "at 2026-07-30 10:00" {
		t.Errorf("NormalizeExpression(in 2h) = %q, want at 2026-07-30 10:00", got)
	}
}
