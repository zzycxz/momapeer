package compose

// Compose-phase discipline prompts. These are injected into the verify/implement
// nudges so the model follows TDD/verification/debugging discipline without
// needing a separate skill registry entry. They distill the iron rules from
// MiMo-Code's compose:tdd / compose:verify / compose:debug skills into compact
// text that rides in the nudge message.

// verifyDiscipline is appended to the verify nudge. It enforces the "no
// completion claims without fresh verification evidence" iron law, and tells
// the model how to signal a plan-level problem (vs a code bug).
const verifyDiscipline = `
Verification discipline:
- RUN the project's actual test/build commands. Do not infer pass/fail from code reading.
- Capture the REAL output: exit code, test counts, compiler errors. Quote the relevant lines.
- If you don't know the test command, check for: go.mod (go test ./...), package.json (npm test), Cargo.toml (cargo test), Makefile (make test).
- A silent or skipped run is NOT a pass. If tests don't exist for this change, say so explicitly.
- End with VERDICT: PASS (commands genuinely succeeded) or VERDICT: FAIL (with the failure summary).
- If the failure is NOT a code bug but a plan-level problem — an assumed API doesn't exist, a dependency conflicts, the approach is fundamentally flawed — emit VERDICT: PLAN_INVALID followed by a short explanation of what assumption is wrong. This pauses execution and asks the user to approve a revised plan.`

// implementDiscipline is appended to the implement nudge on retries (when the
// previous verify failed). It enforces root-cause debugging, not symptom
// patching, and tells the model it can declare the plan invalid.
const implementDiscipline = `
Implementation discipline (fixing a verification failure):
- Read the ACTUAL error before changing code. Do not guess the cause from the failure summary alone.
- Find the root cause. Trace the error to its source line. A symptom patch (try/catch, commenting out a test, loosening an assertion) is NOT a fix.
- If the failing test is wrong (testing the wrong thing), fix the test — but say so explicitly.
- Do not disable or skip a failing test to make it green. That defeats the verification loop.
- If you've fixed the same symptom 2+ times without success, stop patching and reconsider the approach.
- If you discover the plan's premise is wrong (an API you assumed exists doesn't, the approach can't work), do not force a fix. Emit VERDICT: PLAN_INVALID with the reason. The user will be asked to approve a revised plan.`

// tddDiscipline enforces the RED-GREEN-REFACTOR cycle for new feature work.
// Appended to the implement nudge when the task involves writing new code
// (not just fixing a bug). Distills MiMo's compose:tdd 359-line SKILL.md into
// the core iron law + cycle + anti-patterns.
const tddDiscipline = `
TDD discipline (for new features and bug fixes with test coverage):
- RED: Write a failing test that describes the desired behavior BEFORE writing implementation code. Run it and confirm it fails for the right reason (not a compile error).
- GREEN: Write the minimum code to make the test pass. Do not add extra features, edge cases, or "while I'm here" cleanup.
- REFACTOR: Clean up the code (rename, extract, simplify) while keeping the test green.
- NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST. If you catch yourself writing implementation before the test, stop and write the test.
- If the project has no test framework, write a minimal verification script (a main() that asserts) instead. The point is: prove it fails before, passes after.`

// reviewNudge is sent when the verify phase passes, to trigger a code review
// pass before the compose run completes. It distills MiMo-Code's compose:review
// skill: evidence-gated, structured findings.
const reviewNudge = `
Code review pass. Examine the changes you just made (use git diff if available, or re-read the files you edited). Report findings in three buckets:
- CRITICAL: bugs, security issues, data loss, or broken contracts. Must fix before done.
- IMPORTANT: maintainability, missing edge cases, performance. Should fix.
- MINOR: style, naming, comments. Optional.
End with VERDICT: CLEAN (no critical issues) or VERDICT: ISSUES (list the critical ones). If there are critical issues, fix them now and re-verify.`
