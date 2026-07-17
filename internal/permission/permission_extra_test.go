package permission

import (
	"context"
	"encoding/json"
	"testing"
)

// --- ParseDecision ---

func TestParseDecisionAllow(t *testing.T) {
	if ParseDecision("allow") != Allow {
		t.Error("ParseDecision(\"allow\") should be Allow")
	}
	if ParseDecision("ALLOW") != Allow {
		t.Error("ParseDecision(\"ALLOW\") should be Allow")
	}
	if ParseDecision("  allow  ") != Allow {
		t.Error("ParseDecision with whitespace should be Allow")
	}
}

func TestParseDecisionDeny(t *testing.T) {
	if ParseDecision("deny") != Deny {
		t.Error("ParseDecision(\"deny\") should be Deny")
	}
	if ParseDecision("DENY") != Deny {
		t.Error("ParseDecision(\"DENY\") should be Deny")
	}
}

func TestParseDecisionAsk(t *testing.T) {
	if ParseDecision("ask") != Ask {
		t.Error("ParseDecision(\"ask\") should be Ask")
	}
}

func TestParseDecisionUnknown(t *testing.T) {
	if ParseDecision("unknown") != Ask {
		t.Error("ParseDecision(\"unknown\") should default to Ask")
	}
	if ParseDecision("") != Ask {
		t.Error("ParseDecision(\"\") should default to Ask")
	}
	if ParseDecision("  ") != Ask {
		t.Error("ParseDecision(\"  \") should default to Ask")
	}
}

// --- Decision.String ---

func TestDecisionString(t *testing.T) {
	if Allow.String() != "allow" {
		t.Errorf("Allow.String() = %q", Allow.String())
	}
	if Ask.String() != "ask" {
		t.Errorf("Ask.String() = %q", Ask.String())
	}
	if Deny.String() != "deny" {
		t.Errorf("Deny.String() = %q", Deny.String())
	}
	if Decision(99).String() != "unknown" {
		t.Errorf("unknown Decision.String() = %q", Decision(99).String())
	}
}

// --- matchGlob edge cases ---

func TestMatchGlobEmptyPattern(t *testing.T) {
	// Empty pattern matches empty name (both consumed simultaneously).
	if !matchGlob("", "") {
		t.Error("empty pattern should match empty name")
	}
	if matchGlob("", "anything") {
		t.Error("empty pattern should not match non-empty name")
	}
}

func TestMatchGlobOnlyStars(t *testing.T) {
	if !matchGlob("***", "anything") {
		t.Error("pattern *** should match anything")
	}
	if !matchGlob("*", "") {
		t.Error("pattern * should match empty string")
	}
}

func TestMatchGlobPatternLongerThanName(t *testing.T) {
	if matchGlob("abcdefgh", "abc") {
		t.Error("pattern longer than name should not match")
	}
}

func TestMatchGlobConsecutiveStars(t *testing.T) {
	if !matchGlob("a**c", "abc") {
		t.Error("a**c should match abc")
	}
}

func TestMatchGlobQuestionMark(t *testing.T) {
	if !matchGlob("?", "a") {
		t.Error("? should match single char")
	}
	if matchGlob("?", "") {
		t.Error("? should not match empty")
	}
	if matchGlob("?", "ab") {
		t.Error("? should not match two chars")
	}
}

// --- Subject edge cases ---

func TestSubjectNestedJSON(t *testing.T) {
	// Array values should not match.
	got := Subject(json.RawMessage(`{"command": ["array", "value"]}`))
	if got != "" {
		t.Errorf("array command should return empty, got %q", got)
	}
}

func TestSubjectNullValue(t *testing.T) {
	got := Subject(json.RawMessage(`{"command": null}`))
	if got != "" {
		t.Errorf("null command should return empty, got %q", got)
	}
}

func TestSubjectEmptyCommand(t *testing.T) {
	got := Subject(json.RawMessage(`{"command": ""}`))
	if got != "" {
		t.Errorf("empty command should return empty, got %q", got)
	}
}

func TestSubjectPriority(t *testing.T) {
	// command > file_path > path > pattern
	got := Subject(json.RawMessage(`{"pattern":"pat","path":"/p","file_path":"/f","command":"cmd"}`))
	if got != "cmd" {
		t.Errorf("priority: got %q, want cmd", got)
	}
}

func TestRememberRuleWithBashSubjectUsesPrefixWhenAvailable(t *testing.T) {
	// Bash commands with a safe prefix prefer the prefix over the exact command
	// so "always allow" covers similar invocations (e.g. different search terms).
	got := RememberRuleForScope("bash", "go test ./...")
	if got != "Bash(go test:*)" {
		t.Errorf("RememberRuleForScope = %q, want Bash(go test:*)", got)
	}
	if r, ok := ParseRule(got); !ok || r.Literal || r.Tool != "Bash" || r.Subject != "go test:*" {
		t.Errorf("ParseRule(%q) = {%q,%q,lit=%v,ok=%v}", got, r.Tool, r.Subject, r.Literal, ok)
	}
	// Verify the prefix rule matches similar commands.
	if !RuleMatchesString(got, "bash", "go test ./...") {
		t.Errorf("prefix rule should match the exact command")
	}
	if !RuleMatchesString(got, "bash", "go test ./internal/control") {
		t.Errorf("prefix rule should match similar go test command")
	}
	if RuleMatchesString(got, "bash", "go build ./...") {
		t.Errorf("prefix rule should not match different go subcommand")
	}
}

func TestRememberRuleForBashUsesPrefixWhenAvailable(t *testing.T) {
	got := RememberRuleForScope("bash", "go test ./...")
	if got != "Bash(go test:*)" {
		t.Errorf("RememberRuleForScope prefix = %q", got)
	}
	if !RuleMatchesString(got, "bash", "go test ./internal/control") {
		t.Errorf("prefix rule should match similar go test command")
	}
	if !RuleMatchesString(got, "bash", "go test") {
		t.Errorf("prefix rule should match the base command without extra args")
	}
	if RuleMatchesString(got, "bash", "go build ./...") {
		t.Errorf("prefix rule should not match different go subcommand")
	}
	if RuleMatchesString(got, "bash", "go testing ./...") {
		t.Errorf("prefix rule should not match partial command words")
	}
	if RuleMatchesString(got, "bash", "go test ./... && rm -rf /tmp/x") {
		t.Errorf("prefix rule should not match commands with shell syntax")
	}
	if !RuleMatchesString("Bash(go test *)", "bash", "go test ./legacy") {
		t.Errorf("legacy space-star prefix should still match similar commands")
	}
	if RuleMatchesString("Bash(go test *)", "bash", "go test ./legacy && rm -rf /tmp/x") {
		t.Errorf("legacy space-star prefix should not match commands with shell syntax")
	}
}

func TestRememberRuleWithFileSubjectIsToolWide(t *testing.T) {
	// File mutation tools are remembered tool-wide so "always allow editing"
	// covers any file, matching the session-grant behaviour.
	got := RememberRuleForScope("edit_file", "src/app.go")
	if got != "Edit" {
		t.Errorf("RememberRuleForScope = %q, want Edit", got)
	}
	if r, ok := ParseRule(got); !ok || r.Literal || r.Tool != "Edit" || r.Subject != "" {
		t.Errorf("ParseRule(%q) = {%q,%q,lit=%v,ok=%v}", got, r.Tool, r.Subject, r.Literal, ok)
	}
}

// TestPersistedEditRuleIsToolWide asserts a deliberate design choice: when a
// user persists an "always allow" for a file-mutation tool, the saved rule is
// "Edit" — tool-wide, with no path restriction.  This means approving one
// edit_file call and choosing "Always allow (save to config)" grants blanket
// edit permission for every file, across sessions, for every file-mutation
// tool (write_file, edit_file).  Deny rules still take precedence.
func TestPersistedEditRuleIsToolWide(t *testing.T) {
	rule := RememberRuleForScope("edit_file", "src/app.go")
	if rule != "Edit" {
		t.Fatalf("persisted rule = %q, want tool-wide Edit (no path restriction)", rule)
	}
	// The tool-wide Edit rule matches any file-mutation tool on any file.
	allMutationTools := []string{"write_file", "edit_file"}
	for _, tm := range allMutationTools {
		if !RuleMatchesString(rule, tm, "any/path/at/all.txt") {
			t.Errorf("tool-wide Edit should match %s on any path", tm)
		}
	}
	// It must NOT match non-mutation tools (otherwise a denylist would be
	// needed for every tool, which isn't the intent).
	if RuleMatchesString(rule, "bash", "rm -rf /") {
		t.Errorf("tool-wide Edit must not match bash")
	}
}

func TestRememberRuleWithoutSubject(t *testing.T) {
	got := RememberRuleForScope("ls", "")
	if got != "ls" {
		t.Errorf("RememberRuleForScope = %q", got)
	}
}

func TestSessionGrantKeyScopesBashByCommand(t *testing.T) {
	a := SessionGrantKey("bash", "go build")
	b := SessionGrantKey("bash", "go test ./...")
	if a == b {
		t.Fatalf("bash session grant keys should differ by command: %q", a)
	}
}

func TestSessionGrantKeyGroupsFileMutationTools(t *testing.T) {
	a := SessionGrantKey("edit_file", "src/a.go")
	b := SessionGrantKey("write_file", "src/b.go")
	if a != b {
		t.Fatalf("file mutation session grant keys should match, got %q and %q", a, b)
	}
}

func TestSessionGrantRuleForBashUsesPrefix(t *testing.T) {
	got := SessionGrantRuleForScope("bash", "npm run test -- --watch")
	if got != "Bash(npm run test:*)" {
		t.Errorf("SessionGrantRuleForScope prefix = %q", got)
	}
	if !RuleMatchesString(got, "bash", "npm run test -- src") {
		t.Errorf("prefix session rule should match same package script")
	}
	if RuleMatchesString(got, "bash", "npm run build") {
		t.Errorf("prefix session rule should not match another package script")
	}
}

func TestBashCommandPrefixRejectsShellSyntax(t *testing.T) {
	if got := BashCommandPrefix("go test ./... && rm -rf /tmp/x"); got != "" {
		t.Errorf("BashCommandPrefix with shell syntax = %q, want empty", got)
	}
	if got := BashCommandPrefix("rm -rf /tmp/x"); got != "" {
		t.Errorf("BashCommandPrefix dangerous command = %q, want empty", got)
	}
	if got := BashCommandPrefix("go test ./..."); got != "go test:*" {
		t.Errorf("BashCommandPrefix = %q", got)
	}
}

func TestRuleCoversString(t *testing.T) {
	cases := []struct {
		existing  string
		candidate string
		want      bool
	}{
		{"Bash(go test:*)", "Bash(go test ./...)", true},
		{"Bash(go test *)", "Bash(go test ./...)", true}, // legacy generated prefix
		{"bash(go test *)", "Bash(go test)", true},
		{"bash=go test ./...", "Bash(go test ./...)", true},
		{"Bash(go test *)", "Bash(go test:*)", true}, // existing legacy prefix covers the new shape
		{"Bash(go test:*)", "Bash(go test *)", true}, // new prefix prunes legacy prefix on save
		{"Bash(go test ./...)", "Bash(go test:*)", false},
		{"Edit", "Edit(src/app.go)", true},
		{"file_mutation", "Edit(src/app.go)", true},
		{"Edit(src/app.go)", "Edit", false},
		{"Bash(go test:*)", "Bash(go build ./...)", false},
	}
	for _, c := range cases {
		if got := RuleCoversString(c.existing, c.candidate); got != c.want {
			t.Errorf("RuleCoversString(%q, %q) = %v, want %v", c.existing, c.candidate, got, c.want)
		}
	}
}

func TestFileMutationRuleMatchesMutationToolsByPath(t *testing.T) {
	p := New("ask", []string{"Edit(src/app.go)"}, nil, nil)

	if got := p.Decide("write_file", false, json.RawMessage(`{"path":"src/app.go"}`)); got != Allow {
		t.Errorf("write_file same path = %v, want Allow", got)
	}
	if got := p.Decide("edit_file", false, json.RawMessage(`{"path":"src/app.go"}`)); got != Allow {
		t.Errorf("edit_file same path = %v, want Allow", got)
	}
	if got := p.Decide("edit_file", false, json.RawMessage(`{"path":"src/other.go"}`)); got == Allow {
		t.Errorf("edit_file different path = %v, want not Allow", got)
	}
	if got := p.Decide("bash", false, json.RawMessage(`{"command":"cat src/app.go"}`)); got == Allow {
		t.Errorf("bash should not match Edit rule")
	}
}

// --- New ---

func TestNewPolicy(t *testing.T) {
	p := New("deny",
		[]string{"ls"},
		[]string{"read_file"},
		[]string{"bash(rm*)"},
	)
	if p.Mode != Deny {
		t.Errorf("Mode = %v", p.Mode)
	}
	if len(p.Allow) != 1 {
		t.Errorf("Allow count = %d", len(p.Allow))
	}
	if len(p.Ask) != 1 {
		t.Errorf("Ask count = %d", len(p.Ask))
	}
	if len(p.Deny) != 1 {
		t.Errorf("Deny count = %d", len(p.Deny))
	}
}

// --- NewGate ---

func TestNewGate(t *testing.T) {
	p := New("ask", nil, nil, nil)
	g := NewGate(p, nil)
	if g.Policy.Mode != Ask {
		t.Errorf("Policy.Mode = %v", g.Policy.Mode)
	}
	if g.Approver != nil {
		t.Error("Approver should be nil")
	}
}

// --- emailSubjects cc/bcc coverage (security regression A1) ---

// TestEmailSubjectsIncludesCCBCC confirms that deny rules scoped to a domain
// cannot be bypassed by hiding a recipient in cc or bcc. Before the fix,
// emailSubjects only inspected the "to" field, so a message with
// to=colleague@company.com + bcc=exfil@evil.com would only surface company.com
// and an allow(company.com) rule would wrongly permit delivery to evil.com.
func TestEmailSubjectsIncludesCCBCC(t *testing.T) {
	args := json.RawMessage(`{"to":"colleague@company.com","cc":"peer@corp.org","bcc":"exfil@evil.com"}`)
	got := emailSubjects(args)
	want := map[string]bool{"company.com": true, "corp.org": true, "evil.com": true}
	if len(got) != len(want) {
		t.Fatalf("emailSubjects = %v, want all of %v", got, want)
	}
	for _, d := range got {
		if !want[d] {
			t.Errorf("unexpected domain %q in %v", d, got)
		}
	}
}

// TestEmailSubjectsArrayCC confirms cc/bcc accept JSON array form too.
func TestEmailSubjectsArrayCC(t *testing.T) {
	args := json.RawMessage(`{"to":["a@x.com"],"cc":["b@y.com","c@z.com"],"bcc":"d@y.com"}`)
	got := emailSubjects(args)
	gotSet := map[string]bool{}
	for _, d := range got {
		gotSet[d] = true
	}
	for _, want := range []string{"x.com", "y.com", "z.com"} {
		if !gotSet[want] {
			t.Errorf("missing domain %q in %v", want, got)
		}
	}
	// y.com appears in both cc and bcc but must be deduped.
	yCount := 0
	for _, d := range got {
		if d == "y.com" {
			yCount++
		}
	}
	if yCount != 1 {
		t.Errorf("y.com should appear once (deduped), got %d", yCount)
	}
}

// TestEmailSubjectsBCCDefeatsDeny is the end-to-end attack scenario: a deny
// rule on evil.com must still fire when evil.com is only in bcc.
func TestEmailSubjectsBCCDefeatsDeny(t *testing.T) {
	args := json.RawMessage(`{"to":"colleague@company.com","bcc":"exfil@evil.com"}`)
	got := emailSubjects(args)
	for _, d := range got {
		if d == "evil.com" {
			return // good: bcc domain surfaced, deny rule will match
		}
	}
	t.Fatalf("bcc domain evil.com missing from subjects %v — deny(exvil.com) would be bypassed", got)
}

// --- multi_edit deny coverage (security regression A2) ---

// TestMultiEditCoveredByFileMutationDeny confirms that a deny rule expressed
// as Edit/edit_file/file_mutation (the natural forms users write) also blocks
// multi_edit. Before the fix, IsFileMutationTool omitted multi_edit, so a user
// who denied editing secrets/ was bypassable via multi_edit.
func TestMultiEditCoveredByFileMutationDeny(t *testing.T) {
	for _, rule := range []string{
		"Edit(secrets/*)",
		"edit_file(secrets/*)",
		"file_mutation(secrets/*)",
	} {
		t.Run(rule, func(t *testing.T) {
			p := New("allow", nil, nil, []string{rule})
			got := p.Decide("multi_edit", false, json.RawMessage(`{"path":"secrets/key.pem","edits":[{"old_string":"a","new_string":"b"}]}`))
			if got != Deny {
				t.Errorf("multi_edit with %q = %v, want Deny (was bypassable before fix)", rule, got)
			}
		})
	}
}

// --- path normalization vs ./ bypass (security regression A3) ---

// TestPathNormalizationDefeatsDotSlashBypass confirms that a deny rule on a
// path prefix cannot be evaded by prepending "./" or doubling separators.
// Before the fix, Subjects returned the raw "./secrets/key.pem" which did not
// match the glob "secrets/*", so deny edit_file(secrets/*) was bypassable.
func TestPathNormalizationDefeatsDotSlashBypass(t *testing.T) {
	p := New("allow", nil, nil, []string{"edit_file(secrets/*)"})
	for _, raw := range []string{
		`{"path":"./secrets/key.pem"}`,
		`{"path":"secrets//key.pem"}`,
		`{"path":"secrets/./key.pem"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			got := p.Decide("edit_file", false, json.RawMessage(raw))
			if got != Deny {
				t.Errorf("edit_file %s = %v, want Deny (./ bypass worked before fix)", raw, got)
			}
		})
	}
}

// TestPathNormalizationKeepsBashCommandVerbatim confirms command subjects are
// NOT filepath-cleaned (a bash command like "git log ./x" must not be mangled).
func TestPathNormalizationKeepsBashCommandVerbatim(t *testing.T) {
	got := Subjects(json.RawMessage(`{"command":"cat ./secrets/x"}`))
	if len(got) != 1 || got[0] != "cat ./secrets/x" {
		t.Errorf("bash command subject should be verbatim, got %v", got)
	}
}

// --- headless irreversible-operation deny (security review #1) ---

// TestHeadlessDeniesIrreversibleOutward confirms that in headless mode
// (Approver=nil), email_send and rag_delete are DENIED by default rather than
// silently allowed. Without explicit allow rules, an unattended scheduled task
// must not be able to send email or delete a knowledge base with no human to
// confirm. See security review finding #1.
func TestHeadlessDeniesIrreversibleOutward(t *testing.T) {
	p := New("ask", nil, nil, nil) // default mode, no allow/deny rules
	g := NewGate(p, nil)           // Approver=nil → headless
	for _, tool := range []string{"email_send", "rag_delete"} {
		t.Run(tool, func(t *testing.T) {
			var args json.RawMessage
			if tool == "email_send" {
				args = json.RawMessage(`{"to":"x@y.com"}`)
			} else {
				args = json.RawMessage(`{"collection":"test"}`)
			}
			allow, _, _ := g.Check(context.Background(), tool, args, false)
			if allow {
				t.Errorf("headless %s should be denied (no approver, irreversible outward op), got allow", tool)
			}
		})
	}
}

// TestHeadlessAllowsOrdinaryWriter confirms headless mode still allows ordinary
// write tools (write_file etc.) so a scheduled task can still do useful work
// inside the workspace — only the irreversible outward ops are blocked.
func TestHeadlessAllowsOrdinaryWriter(t *testing.T) {
	p := New("ask", nil, nil, nil)
	g := NewGate(p, nil)
	allow, _, _ := g.Check(context.Background(), "write_file", json.RawMessage(`{"path":"x.txt"}`), false)
	if !allow {
		t.Error("headless write_file should be allowed (ordinary writer, not irreversible outward)")
	}
}
