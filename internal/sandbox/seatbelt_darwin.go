package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Command returns the argv to run `command` through sh, wrapped in sandbox-exec
// when the spec enforces and the tool is available. The second return is whether
// wrapping happened; false means the command runs unconfined (sandbox off, or
// sandbox-exec missing — a graceful fallback rather than a hard failure, since
// the permission layer still gates the call). When spec.RequireAvailable is set
// and enforcement is requested but the tool is unavailable, the argv is nil to
// signal the caller (bash tool) to refuse the command (fail-closed).
func Command(spec Spec, sh Shell, command string) ([]string, bool) {
	if !spec.enforce() {
		return sh.argv(command), false
	}
	if !Available() {
		if spec.RequireAvailable {
			return nil, false
		}
		return sh.argv(command), false
	}
	return append([]string{"sandbox-exec", "-p", seatbeltProfile(spec)}, sh.argv(command)...), true
}

// Available reports whether sandbox-exec is on PATH (it ships with macOS).
func Available() bool {
	_, err := exec.LookPath("sandbox-exec")
	return err == nil
}

// seatbeltProfile builds an SBPL profile that allows everything, then denies
// all file writes and re-allows them only under the write-roots (workspace +
// temp + caches). Network is denied unless allowed. Reads are left open so the
// toolchain (compilers reading GOROOT, git reading ~/.gitconfig, …) keeps
// working — the boundary this draws is "can't write outside the workspace, and
// optionally can't talk to the network", which is the Phase 0 blast-radius made
// to also cover arbitrary shell commands.
func seatbeltProfile(spec Spec) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n(deny file-write*)\n(allow file-write*\n")
	for _, p := range writeAllowDirs(spec.WriteRoots, spec.StrictWrites) {
		fmt.Fprintf(&b, "    (subpath %s)\n", sbplString(p))
	}
	b.WriteString(")\n")
	if !spec.Network {
		b.WriteString("(deny network*)\n")
	}
	return b.String()
}

// writeAllowDirs is the deduplicated, symlink-resolved set of directories the
// sandbox permits writes to: the caller's roots plus temp dirs, /dev, and the
// common toolchain caches under $HOME. Symlinks are resolved because macOS's
// /tmp and $TMPDIR live under /private, which is the path Seatbelt matches.
//
// strict narrows the toolchain grants to true cache subdirs only (e.g.
// ~/.cargo/registry/cache instead of all of ~/.cargo). The default (false)
// keeps the broad grants because `go install`/`cargo build`/`npm install`
// legitimately write to bin/pkg dirs outside the cache. Audit A8.
func writeAllowDirs(roots []string, strict bool) []string {
	dirs := append([]string{}, roots...)
	dirs = append(dirs, "/dev", "/tmp", "/private/tmp", "/private/var/folders", os.TempDir())
	if home, err := os.UserHomeDir(); err == nil {
		if strict {
			// Cache-only subdirs: these hold downloaded artifacts the toolchain
			// regenerates on demand, so confining writes here does not break a
			// cold build's ability to fetch dependencies. Bin/pkg/lifecycle
			// dirs (cargo/bin, npm, go/bin) are deliberately excluded — a
			// prompt-injected command cannot drop an executable that a later
			// `cargo run` or npm script would execute. Builds that need to
			// install tooling will fail under strict mode; that's the point.
			for _, sub := range []string{
				"Library/Caches",        // macOS toolchain caches (Swift, Xcode)
				".cache",                // pip / generic XDG cache
				".cargo/registry/cache", // cargo downloaded crate tarballs
				".cargo/registry/src",   // cargo extracted crate sources
				"go/pkg/mod/cache",      // go module cache (downloaded modules)
			} {
				dirs = append(dirs, filepath.Join(home, sub))
			}
		} else {
			// SECURITY NOTE (audit A8): these whole-directory write grants cover not
			// just cache subdirs but also the toolchain's bin/persistent locations
			// (e.g. ~/.cargo/bin, ~/.npm, ~/go/bin). A prompt-injected bash command
			// could drop a binary in ~/.cargo/bin and have it execute on the next
			// `cargo run` / npm lifecycle script — a persistence backdoor. We keep
			// the broad grants here because narrowing them to cache-only subdirs
			// (e.g. .cargo/registry/cache) breaks `go install`, `cargo build`, and
			// `npm install` which legitimately write to bin/pkg dirs. The strict
			// mode above scopes these to true cache subdirs for high-security
			// deployments where build-tool execution is not expected.
			for _, sub := range []string{"Library/Caches", ".cache", ".npm", ".cargo", "go"} {
				dirs = append(dirs, filepath.Join(home, sub))
			}
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d == "" {
			continue
		}
		abs, err := filepath.Abs(d)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out
}

// sbplString quotes a path as an SBPL string literal, escaping backslash and
// double-quote so a path can't break out of the profile syntax.
func sbplString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
