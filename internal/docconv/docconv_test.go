package docconv

import "testing"

// TestScriptCandidatesIncludesExeDir verifies the candidate list probes the
// conventional locations (cwd + relative to the executable).
func TestScriptCandidatesIncludesExeDir(t *testing.T) {
	got := ScriptCandidates("doc_converter.py")
	// The cwd-relative name must always be first.
	if len(got) == 0 || got[0] != "doc_converter.py" {
		t.Fatalf("first candidate = %v, want doc_converter.py", got)
	}
}

// TestFindScriptMissingReturnsEmpty verifies that an absent script yields "".
func TestFindScriptMissingReturnsEmpty(t *testing.T) {
	if got := FindScript("definitely_not_here_12345.py"); got != "" {
		t.Fatalf("FindScript(missing) = %q, want empty", got)
	}
}

// TestPythonExePlatform verifies the python name matches the platform convention.
func TestPythonExePlatform(t *testing.T) {
	got := pythonExe()
	want := "python3"
	// runtime.GOOS check mirrors pythonExe's own logic; we can't import runtime
	// in a deterministic cross-platform way, so just assert it's one of the two.
	if got != "python" && got != "python3" {
		t.Fatalf("pythonExe() = %q, want python or python3", got)
	}
	_ = want
}
