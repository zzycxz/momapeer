package builtinmcp

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/zzycxz/momapeer/internal/config"
)

// WPSPPTName is the MCP server name for the PPT generation capability. Tools it
// exposes appear as mcp__wps-ppt__<tool> (e.g. mcp__wps-ppt__ppt_create).
const WPSPPTName = "wps-ppt"

// WPSPPTEntry builds the plugin entry for the wps-ppt-mcp-server, a stdio MCP
// server that generates PPT via WPS COM automation. The server is a separate
// Python project (FastMCP + pywin32); this entry launches it via the configured
// or auto-detected Python interpreter running serverPath.
//
// serverPath must point at the server's server.py. pythonExe selects the
// interpreter (empty → "python" on PATH). The entry uses the "background" tier
// so cowork startup isn't blocked by Python/WPS warmup; the server connects
// while the agent works and its tools appear once ready.
func WPSPPTEntry(serverPath, pythonExe string) (config.PluginEntry, error) {
	if strings.TrimSpace(serverPath) == "" {
		return config.PluginEntry{}, errors.New("wps_ppt_server_path is empty — set [cowork] wps_ppt_server_path to the server's server.py")
	}
	if _, err := os.Stat(serverPath); err != nil {
		return config.PluginEntry{}, err
	}
	py := strings.TrimSpace(pythonExe)
	if py == "" {
		// Prefer "python", fall back to "py" (Windows launcher) if python isn't found.
		if _, err := exec.LookPath("python"); err == nil {
			py = "python"
		} else if _, err := exec.LookPath("py"); err == nil {
			py = "py"
		} else {
			py = "python" // will fail at launch with a clear PATH error
		}
	}
	return config.PluginEntry{
		Name:    WPSPPTName,
		Type:    "stdio",
		Command: py,
		Args:    []string{serverPath},
		Tier:    "background",
	}, nil
}

// WPSPPTDepsMissing reports whether the wps-ppt server's Python dependencies are
// not installed (fastmcp + pywin32). Used by the agent-facing dep-check so a
// helpful "install these" message surfaces instead of a cryptic import error.
// Returns the missing package list (empty = all present) and any probe error.
func WPSPPTDepsMissing(pythonExe string) ([]string, error) {
	py := strings.TrimSpace(pythonExe)
	if py == "" {
		py = "python"
	}
	missing := []string{}
	for _, mod := range []string{"fastmcp", "win32com"} { // win32com is the pywin32 import name
		cmd := exec.Command(py, "-c", "import "+mod)
		if err := cmd.Run(); err != nil {
			missing = append(missing, depPkgFor(mod))
		}
	}
	return missing, nil
}

func depPkgFor(mod string) string {
	switch mod {
	case "win32com":
		return "pywin32"
	default:
		return mod
	}
}

// EnsureWPSPPTDeps installs the wps-ppt server's Python dependencies (fastmcp,
// pywin32) via pip into the given interpreter. Called by the agent when
// WPSPPTDepsMissing reports gaps, so the user doesn't have to manually pip
// install. Returns combined stdout+stderr for surfacing.
func EnsureWPSPPTDeps(pythonExe string) (string, error) {
	py := strings.TrimSpace(pythonExe)
	if py == "" {
		py = "python"
	}
	cmd := exec.Command(py, "-m", "pip", "install", "fastmcp", "pywin32")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
