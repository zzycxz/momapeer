// Package sandbox wraps a shell command in an OS-level jail so the model's
// `bash` calls are confined: it may read freely but write only inside the
// workspace (plus temp and toolchain caches) and reach the network only when
// allowed. This is the *enforcement* layer beneath the permission rules
// (*policy*): a permitted command still cannot escape the box.
//
// Only macOS (Seatbelt via sandbox-exec) is implemented; on every other OS, or
// when the OS tooling is missing, Command falls back to running the command
// unwrapped (see Available). Confining the in-process file-writer built-ins is
// handled separately, in package tool/builtin.
package sandbox

// Spec describes how to confine one command. The zero value (Mode == "") does
// not enforce, so an unconfigured caller runs commands unchanged.
type Spec struct {
	// Mode is "enforce" to wrap the command, anything else (incl. "off" and "")
	// to run it unwrapped.
	Mode string
	// WriteRoots are directories the command may write to (the workspace root
	// plus any configured extras). Temp dirs and common toolchain caches are
	// added automatically so builds and package managers keep working.
	WriteRoots []string
	// Network allows network egress from inside the sandbox. Off blocks it so a
	// command cannot exfiltrate or fetch; many dev commands (module/package
	// downloads) need it, so it defaults on at the config layer.
	Network bool
	// RequireAvailable, when true, makes Mode "enforce" fail-closed (refuse all
	// commands) if no OS sandbox backend exists on this platform, rather than
	// silently degrading to unconfined. Mirrors config.SandboxConfig.RequireAvailable.
	RequireAvailable bool
	// StrictWrites narrows the toolchain-cache write grants (macOS Seatbelt) to
	// true cache subdirs only — e.g. ~/.cargo/registry/cache instead of all of
	// ~/.cargo. Default (false) keeps the broad grants so `go install`/`cargo
	// build`/`npm install` keep working (they write to bin/pkg dirs outside the
	// cache). High-security deployments that don't expect build-tool execution
	// turn this on to close the "drop a binary in ~/.cargo/bin" persistence
	// vector. Audit A8.
	StrictWrites bool
}

// enforce reports whether the spec asks for confinement.
func (s Spec) enforce() bool { return s.Mode == "enforce" }
