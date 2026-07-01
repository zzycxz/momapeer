package agent

// verify.go implements the post-execution verify + TDD retry loop, an optional
// stage the Coordinator runs after the executor finishes. It is the momapeer
// analogue of DeepSeek-Reasonix/MiMo-Code's compose "Verify" phase: run the
// project's verification commands (go test/build for code, a screenshot diff
// for desktop tasks), and if they fail, feed the failures back to the executor
// for a bounded number of debug retries.
//
// Design:
//   - Verifier is a profile-dispatched interface. DevVerifier runs go test/build
//     in the workspace; CoworkVerifier checks a screenshot against the expected
//     state. A nil verifier (the default) disables the stage entirely, so the
//     change is opt-in and never perturbs existing flows.
//   - The stage lives in Coordinator.Run (plan -> exec -> verify -> retry), not
//     in the single-Agent path, because the Coordinator is already the
//     phase-orchestration layer. Controllers that build a single Agent (no
//     planner) keep today's behaviour unchanged.
//   - Retries are bounded (default 1) to avoid burning the tool budget; the
//     executor sees the verify failures as a follow-up user message, mirroring
//     the existing finalReadiness / emptyFinal retry shape.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zzycxz/momapeer/internal/event"
)

// VerifyResult is the outcome of one verification pass.
type VerifyResult struct {
	Passed   bool   // true when the project's verify commands succeeded.
	Detail   string // human-readable summary (commands run + pass/fail).
	Failures string // failure output fed back to the executor on retry; "" when Passed.
}

// Verifier checks whether the executor's changes satisfy the project's
// definition of "done". It is profile-specific: a coding workspace runs its test
// suite / typecheck / build, a desktop task compares a screenshot to the
// expected state. The Verify method receives the workspace root so the verifier
// can run commands in the right directory.
type Verifier interface {
	// Verify runs the project's verification commands and returns the result.
	// A nil error means the commands ran (pass or fail is in result.Passed);
	// a non-nil error means verification itself could not run (e.g. no test
	// toolchain), in which case the stage is skipped rather than treated as a
	// failure — a missing toolchain is not a code defect.
	Verify(ctx context.Context, workspaceRoot string) (VerifyResult, error)
}

// verifyOptions configures the verify + retry stage.
type verifyOptions struct {
	// Verifier performs one verification pass. nil disables the whole stage.
	Verifier Verifier
	// MaxRetries is the number of debug retries after a failed verify (default
	// 1). 0 means verify once and never retry; the executor still sees the
	// failure as a notice.
	MaxRetries int
}

// verifyAndRetry runs the verifier, and on failure feeds the failures back to
// the executor for up to opts.MaxRetries debug rounds. It is a no-op (returns
// nil) when opts.Verifier is nil, so callers can wire it unconditionally. The
// executor re-runs via runDebug (an executor.Run with a failures follow-up
// message); each retry is logged as a Phase event so the UI shows the loop.
func (c *Coordinator) verifyAndRetry(ctx context.Context, opts verifyOptions, workspaceRoot string) error {
	if opts.Verifier == nil {
		return nil
	}
	maxRetries := opts.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	for attempt := 0; ; attempt++ {
		c.sink.Emit(event.Event{Kind: event.Phase, Text: "verify · checking changes"})
		result, err := opts.Verifier.Verify(ctx, workspaceRoot)
		if err != nil {
			// Verification could not run (no toolchain, unsupported workspace).
			// Surface it as a notice but do not fail the turn: the executor's
			// own work already completed.
			c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
				Text: "verify skipped: " + err.Error()})
			return nil
		}

		if result.Passed {
			c.sink.Emit(event.Event{Kind: event.Phase, Text: "verify · passed"})
			if result.Detail != "" {
				c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: result.Detail})
			}
			return nil
		}

		// Failed. If no retries remain, surface the failure and stop: the
		// executor's turn is still considered done (it produced changes); verify
		// is advisory, not a hard gate that discards completed work.
		if attempt >= maxRetries {
			msg := "verify failed"
			if result.Failures != "" {
				msg += ":\n" + truncateForNotice(result.Failures, 1200)
			}
			c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: msg})
			return nil
		}

		// Retry: hand the failures back to the executor as a follow-up so it
		// can debug, then re-verify on the next loop iteration.
		if c.executor == nil {
			// No executor to drive a debug round — surface the failure and stop.
			msg := "verify failed (no executor to retry)"
			if result.Failures != "" {
				msg += ":\n" + truncateForNotice(result.Failures, 1200)
			}
			c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: msg})
			return nil
		}
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
			Text: fmt.Sprintf("verify failed (attempt %d/%d); asking executor to fix", attempt+1, attempt+1+maxRetries)})
		if err := c.runDebug(ctx, result.Failures); err != nil {
			return fmt.Errorf("verify debug retry: %w", err)
		}
	}
}

// runDebug hands a failures block to the executor as a follow-up user turn so
// it can fix the issues verify found, mirroring the finalReadiness retry shape.
func (c *Coordinator) runDebug(ctx context.Context, failures string) error {
	if strings.TrimSpace(failures) == "" {
		failures = "(verify reported a failure with no detail)"
	}
	msg := "The post-change verification failed. Fix the issues below, then finish.\n\n## Verify failures\n\n" + failures
	return c.executor.Run(ctx, msg)
}

// truncateForNotice caps s to roughly max runes for a user-facing notice,
// appending an ellipsis when truncated, so a huge test log never floods the UI.
func truncateForNotice(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// DevVerifier runs a coding workspace's verification commands: go vet, go build,
// then go test. It reports the first failure's output (so the executor can fix
// the root cause), and passes only when all three succeed (test is skipped when
// the workspace has no _test.go files, which is not itself a failure).
type DevVerifier struct{}

// Verify runs go vet / build / test in workspaceRoot and returns the result.
func (DevVerifier) Verify(ctx context.Context, workspaceRoot string) (VerifyResult, error) {
	// Detect a Go workspace. If go is absent or the dir has no go.mod, this
	// verifier is the wrong tool — return an error so the stage skips rather
	// than reporting a spurious failure.
	if !fileExists(filepath.Join(workspaceRoot, "go.mod")) {
		return VerifyResult{}, errors.New("not a Go workspace (no go.mod)")
	}

	var detail strings.Builder
	run := func(name string, args ...string) (string, bool, bool) {
		// Returns (combined-output, passed, ran). ran=false means the tool is
		// missing; we treat that as skip, not failure.
		cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cctx, name, args...).CombinedOutput()
		// exec.LookPath error => tool not installed.
		if _, lookErr := exec.LookPath(name); lookErr != nil {
			return "", true, false
		}
		s := strings.TrimSpace(string(out))
		passed := err == nil
		fmt.Fprintf(&detail, "%s %s: %s\n", name, strings.Join(args, " "), statusWord(passed))
		return s, passed, true
	}

	// 1. go vet — catches obvious mistakes cheaply.
	if out, passed, ran := run("go", "vet", "./..."); ran && !passed {
		return VerifyResult{Passed: false, Detail: detail.String(), Failures: "go vet ./...\n" + out}, nil
	}
	// 2. go build — a build break is always a defect.
	if out, passed, ran := run("go", "build", "./..."); ran && !passed {
		return VerifyResult{Passed: false, Detail: detail.String(), Failures: "go build ./...\n" + out}, nil
	}
	// 3. go test — the real correctness gate. No _test.go files is a skip, not
	// a pass or fail: a workspace can legitimately have no tests.
	if out, passed, ran := run("go", "test", "./..."); ran {
		if !passed {
			return VerifyResult{Passed: false, Detail: detail.String(), Failures: "go test ./...\n" + out}, nil
		}
	} else {
		fmt.Fprintln(&detail, "go test ./...: skipped")
	}
	return VerifyResult{Passed: true, Detail: detail.String()}, nil
}

func statusWord(passed bool) string {
	if passed {
		return "ok"
	}
	return "FAIL"
}

// fileExists reports whether a non-directory file exists at p.
func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
