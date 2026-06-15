// Package builtinmcp defines MCP servers that ship with momapeer without
// requiring user configuration. Currently only Context7 is bundled; the
// package is designed so more built-in servers can be added later.
package builtinmcp

import (
	"os/exec"

	"github.com/zzycxz/momapeer/internal/config"
)

const (
	Context7Name = "context7"
)

// lookPath is indirected so tests can inject a fake PATH lookup.
var lookPath = exec.LookPath

// Entries returns the built-in MCP servers that are always available. They use
// the lazy tier so startup never blocks on package installation or network.
func Entries() []config.PluginEntry {
	return []config.PluginEntry{
		context7Entry(),
	}
}

func context7Entry() config.PluginEntry {
	command, args := context7Command()
	return config.PluginEntry{
		Name:    Context7Name,
		Type:    "stdio",
		Command: command,
		Args:    args,
		Tier:    "lazy",
	}
}

// context7Command probes for a JS package runner on PATH and returns the
// command + args to launch @upstash/context7-mcp. Priority: npx > pnpm > bunx.
// If none is found, falls back to npx (will fail at runtime with a clear error).
func context7Command() (string, []string) {
	if _, err := lookPath("npx"); err == nil {
		return "npx", []string{"-y", "@upstash/context7-mcp"}
	}
	if _, err := lookPath("pnpm"); err == nil {
		return "pnpm", []string{"dlx", "@upstash/context7-mcp"}
	}
	if _, err := lookPath("bunx"); err == nil {
		return "bunx", []string{"@upstash/context7-mcp"}
	}
	return "npx", []string{"-y", "@upstash/context7-mcp"}
}

// Entry returns one built-in MCP entry by name.
func Entry(name string) (config.PluginEntry, bool) {
	for _, e := range Entries() {
		if e.Name == name {
			return e, true
		}
	}
	return config.PluginEntry{}, false
}

// IsBuiltIn reports whether name is a momapeer-shipped MCP server.
func IsBuiltIn(name string) bool {
	_, ok := Entry(name)
	return ok
}

// AppendEnabled appends enabled built-in MCP entries to out unless a configured
// or reserved entry with the same name already exists. Explicit user config
// wins, including auto_start=false.
func AppendEnabled(out []config.PluginEntry, configured []config.PluginEntry, enabledNames []string, reservedNames ...string) []config.PluginEntry {
	seen := make(map[string]bool, len(configured))
	for _, e := range configured {
		seen[e.Name] = true
	}
	for _, name := range reservedNames {
		seen[name] = true
	}
	enabled := make(map[string]bool, len(enabledNames))
	for _, name := range enabledNames {
		enabled[name] = true
	}
	for _, e := range Entries() {
		if enabled[e.Name] && !seen[e.Name] {
			out = append(out, e)
		}
	}
	return out
}
