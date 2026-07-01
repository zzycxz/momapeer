package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/zzycxz/momapeer/internal/event"
)

// fakeVerifier returns a scripted sequence of results, one per Verify call.
type fakeVerifier struct {
	results []VerifyResult
	calls   int
}

func (f *fakeVerifier) Verify(ctx context.Context, workspaceRoot string) (VerifyResult, error) {
	if f.calls >= len(f.results) {
		return VerifyResult{Passed: true, Detail: "script exhausted"}, nil
	}
	r := f.results[f.calls]
	f.calls++
	return r, nil
}

// capturingSink records emitted events for assertions.
type capturingSink struct{ notices []string }

func (s *capturingSink) Emit(e event.Event) {
	if e.Kind == event.Notice {
		s.notices = append(s.notices, e.Text)
	}
}

// TestVerifyAndRetryNoVerifier verifies the stage is a no-op when no verifier
// is wired (the default, original behaviour).
func TestVerifyAndRetryNoVerifier(t *testing.T) {
	c := &Coordinator{sink: event.Discard}
	if err := c.verifyAndRetry(context.Background(), verifyOptions{}, "/tmp"); err != nil {
		t.Fatalf("nil verifier should be a no-op, got err=%v", err)
	}
}

// TestVerifyAndRetryPassOnFirstCheck verifies a passing verifier terminates
// immediately with one call and no retries.
func TestVerifyAndRetryPassOnFirstCheck(t *testing.T) {
	v := &fakeVerifier{results: []VerifyResult{{Passed: true, Detail: "all good"}}}
	sink := &capturingSink{}
	c := &Coordinator{sink: sink}
	if err := c.verifyAndRetry(context.Background(), verifyOptions{Verifier: v, MaxRetries: 3}, "/tmp"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if v.calls != 1 {
		t.Fatalf("verifier calls = %d, want 1 (pass on first check)", v.calls)
	}
	if len(sink.notices) == 0 || sink.notices[0] != "all good" {
		t.Fatalf("expected 'all good' notice, got %v", sink.notices)
	}
}

// TestVerifyAndRetrySkipOnError verifies that a verifier that itself can't run
// (e.g. not a Go workspace) causes the stage to skip, not fail.
func TestVerifyAndRetrySkipOnError(t *testing.T) {
	skip := &errorVerifier{}
	sink := &capturingSink{}
	c := &Coordinator{sink: sink}
	if err := c.verifyAndRetry(context.Background(), verifyOptions{Verifier: skip, MaxRetries: 2}, "/tmp"); err != nil {
		t.Fatalf("skip-on-error should not return err, got %v", err)
	}
	if skip.calls != 1 {
		t.Fatalf("errorVerifier calls = %d, want 1", skip.calls)
	}
	if len(sink.notices) == 0 || !strings.Contains(sink.notices[0], "verify skipped") {
		t.Fatalf("expected a 'verify skipped' notice, got %v", sink.notices)
	}
}

// TestDevVerifierNonGoWorkspace verifies DevVerifier skips a non-Go workspace.
func TestDevVerifierNonGoWorkspace(t *testing.T) {
	_, err := DevVerifier{}.Verify(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a Go workspace") {
		t.Fatalf("expected 'not a Go workspace' error, got %v", err)
	}
}

// TestTruncateForNotice verifies the notice truncation helper caps long output.
func TestTruncateForNotice(t *testing.T) {
	short := "abc"
	if got := truncateForNotice(short, 10); got != short {
		t.Fatalf("short string should pass through, got %q", got)
	}
	long := strings.Repeat("x", 5000)
	got := truncateForNotice(long, 100)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated output should end with ellipsis, got len=%d", len(got))
	}
	if len([]rune(got)) > 200 {
		t.Fatalf("truncated output too long: %d runes", len([]rune(got)))
	}
}

// errorVerifier always returns an error (verification could not run).
type errorVerifier struct{ calls int }

func (e *errorVerifier) Verify(ctx context.Context, workspaceRoot string) (VerifyResult, error) {
	e.calls++
	return VerifyResult{}, &skipErr{}
}

type skipErr struct{}

func (e *skipErr) Error() string { return "no toolchain" }

// TestReviewAndFixDisabled verifies the review stage is a no-op when disabled.
func TestReviewAndFixDisabled(t *testing.T) {
	c := &Coordinator{sink: event.Discard}
	if err := c.reviewAndFix(context.Background(), reviewOptions{Enabled: false}, "/tmp"); err != nil {
		t.Fatalf("disabled review should be a no-op, got err=%v", err)
	}
}

// TestReviewAndFixNoExecutor verifies the stage skips when there is no executor
// to drive a review turn.
func TestReviewAndFixNoExecutor(t *testing.T) {
	c := &Coordinator{sink: event.Discard}
	if err := c.reviewAndFix(context.Background(), reviewOptions{Enabled: true}, "/tmp"); err != nil {
		t.Fatalf("review with no executor should be a no-op, got err=%v", err)
	}
}

// TestGitDiffNonRepo verifies gitDiff reports ran=true but empty diff (skip) on
// a non-git directory, rather than erroring.
func TestGitDiffNonRepo(t *testing.T) {
	// A fresh temp dir is not a git repo, so git diff returns empty/exit-128.
	diff, ran, err := gitDiff(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("gitDiff on non-repo should not error, got %v", err)
	}
	// ran depends on git being installed; if git is absent, ran=false is fine.
	// Either way diff should be empty (nothing to review).
	if diff != "" {
		t.Fatalf("non-repo should yield empty diff, got %q", diff[:min(50, len(diff))])
	}
	_ = ran
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
