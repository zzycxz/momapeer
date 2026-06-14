// Command momapeer is a config- and plugin-driven coding agent CLI.
package main

import (
	"os"

	"github.com/zzycxz/momapeer/internal/cli"

	// Blank imports wire compile-time built-ins into their registries.
	_ "github.com/zzycxz/momapeer/internal/provider/anthropic"
	_ "github.com/zzycxz/momapeer/internal/provider/openai"
	_ "github.com/zzycxz/momapeer/internal/tool/builtin"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version))
}
