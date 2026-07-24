package compose

import (
	"testing"
)

func TestShouldComposeThreshold(t *testing.T) {
	tests := []struct {
		name string
		json string
		want bool
	}{
		{"empty", "", false},
		{"two tasks (below threshold)", `{"todos":[{"content":"a"},{"content":"b"}]}`, false},
		{"three tasks (at threshold)", `{"todos":[{"content":"a"},{"content":"b"},{"content":"c"}]}`, true},
		{"five tasks", `{"todos":[{"content":"a"},{"content":"b"},{"content":"c"},{"content":"d"},{"content":"e"}]}`, true},
		{"invalid json", `{not json`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldCompose(tc.json); got != tc.want {
				t.Errorf("ShouldCompose(%q) = %v, want %v", tc.json, got, tc.want)
			}
		})
	}
}

func TestReadVerifyVerdict(t *testing.T) {
	tests := []struct {
		name     string
		reply    string
		wantPass bool
		wantFail string
	}{
		{
			name:     "pass verdict",
			reply:    "All tests passed.\nVERDICT: PASS",
			wantPass: true,
		},
		{
			name:     "pass verdict lowercase",
			reply:    "verdict: pass",
			wantPass: true,
		},
		{
			name:     "fail verdict with summary",
			reply:    "2 tests failed.\nVERDICT: FAIL TestFoo/TestBar: expected 3 got 4",
			wantPass: false,
			wantFail: "TestFoo/TestBar: expected 3 got 4",
		},
		{
			name:     "no verdict (treated as non-pass)",
			reply:    "I ran the tests and they look fine.",
			wantPass: false,
			wantFail: "",
		},
		{
			name:     "empty reply",
			reply:    "",
			wantPass: false,
			wantFail: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := readVerifyVerdict(tc.reply)
			if v.passed != tc.wantPass {
				t.Errorf("passed = %v, want %v", v.passed, tc.wantPass)
			}
			if tc.wantFail != "" && v.failures == "" {
				t.Errorf("failures empty, want to contain %q", tc.wantFail)
			}
			if tc.wantFail != "" && !contains(v.failures, tc.wantFail) {
				t.Errorf("failures = %q, want to contain %q", v.failures, tc.wantFail)
			}
		})
	}
}

func TestParseSeededTodos(t *testing.T) {
	todos, err := parseSeededTodos(`{"todos":[{"content":"step 1","status":"in_progress"},{"content":"step 2","status":"pending"}]}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(todos) != 2 {
		t.Fatalf("got %d todos, want 2", len(todos))
	}
	if todos[0].Content != "step 1" || todos[0].Status != "in_progress" {
		t.Errorf("first todo = %+v", todos[0])
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short = %q, want %q", got, "hello")
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncate long = %q, want %q", got, "hello...")
	}
}

func TestReadReviewVerdict(t *testing.T) {
	tests := []struct {
		name       string
		reply      string
		wantClean  bool
		wantIssues string
	}{
		{"clean", "Looks good.\nVERDICT: CLEAN", true, ""},
		{"clean lowercase", "verdict: clean", true, ""},
		{"issues", "Found a null-pointer risk in handler.go.\nVERDICT: ISSUES null deref on empty input", false, "null deref on empty input"},
		{"no verdict (conservative re-verify)", "I reviewed the code.", false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := readReviewVerdict(tc.reply)
			if v.clean != tc.wantClean {
				t.Errorf("clean = %v, want %v", v.clean, tc.wantClean)
			}
			if tc.wantIssues != "" && !contains(v.issues, tc.wantIssues) {
				t.Errorf("issues = %q, want to contain %q", v.issues, tc.wantIssues)
			}
		})
	}
}

func TestHasPlanInvalidVerdict(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  bool
	}{
		{"plan_invalid", "The assumed API doesn't exist.\nVERDICT: PLAN_INVALID", true},
		{"plan_invalid lowercase", "verdict: plan_invalid", true},
		{"regular fail", "VERDICT: FAIL tests broken", false},
		{"pass", "VERDICT: PASS", false},
		{"no verdict", "tests look fine", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasPlanInvalidVerdict(tc.reply); got != tc.want {
				t.Errorf("hasPlanInvalidVerdict(%q) = %v, want %v", tc.reply, got, tc.want)
			}
		})
	}
}

func TestExtractPlanInvalidReason(t *testing.T) {
	reply := "Discovered plugin.Host doesn't expose the reload hook we assumed.\nVERDICT: PLAN_INVALID plugin.Host has no Reload method; need to add one first"
	reason := extractPlanInvalidReason(reply)
	if !contains(reason, "plugin.Host has no Reload method") {
		t.Errorf("reason = %q, want to contain the explanation", reason)
	}

	empty := extractPlanInvalidReason("no marker here")
	if empty != "(no reason given)" {
		t.Errorf("empty reason = %q, want placeholder", empty)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
