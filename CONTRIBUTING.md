# Contributing to momapeer

Thank you for your interest in contributing to momapeer! This guide covers
everything you need to get started.

See [momapeer.md](./momapeer.md) for the full architecture overview
and naming conventions.

## Prerequisites

- **Go 1.25+** (toolchain go1.26.4) — the project targets the latest stable Go release
- **Git** — for version control
- **Node.js** (optional) — only if you work on the desktop app (`desktop/`)

## Getting started

```bash
git clone https://github.com/zzycxz/momapeer.git
cd momapeer
make build    # builds the CLI binary
make test     # runs the full test suite
```

## Project structure

| Directory | Purpose |
|-----------|---------|
| `cmd/momapeer` | CLI entry point (minimal — delegates to `internal/cli`) |
| `cmd/momapeer-plugin-example` | Reference MCP stdio plugin |
| `cmd/e2ebench` | End-to-end benchmark harness |
| `internal/agent` | Agent loop, session, coordinator, compaction, storm breaker |
| `internal/cli` | TUI, subcommands, setup wizard, markdown rendering |
| `internal/control` | Transport-agnostic controller (single orchestration layer) |
| `internal/config` | TOML configuration loading (flag > project > user > defaults) |
| `internal/provider` | Provider interface + registry (kind → factory) |
| `internal/provider/openai` | OpenAI-compatible provider (MoMA, etc.) |
| `internal/provider/anthropic` | Anthropic Messages API with extended thinking |
| `internal/tool` | Tool interface + Registry |
| `internal/tool/builtin` | 20+ built-in tools (bash, read/write/edit, glob, grep, etc.) |
| `internal/plugin` | MCP client (stdio + Streamable HTTP) |
| `internal/event` | Typed event stream (Sink interface) |
| `internal/hook` | Shell hooks (PreToolUse, PostToolUse, UserPromptSubmit, Stop) |
| `internal/memory` | momapeer.md hierarchy + auto-memory store |
| `internal/skill` | Skill discovery from Markdown + built-in skills |
| `internal/sandbox` | OS-level sandboxing (Seatbelt on macOS) |
| `internal/permission` | Per-call policy: allow/ask/deny rules |
| `internal/checkpoint` | Snapshot-based rewind |
| `internal/bot` | Multi-channel IM bot gateway (QQ, Feishu, WeChat) |
| `internal/acp` | Agent Control Protocol server |
| `internal/lsp` | LSP client integration |
| `internal/billing` | Token cost/balance tracking |
| `internal/codegraph` | CodeGraph integration (tree-sitter code intelligence) |
| `internal/i18n` | Internationalization (en + zh) |
| `internal/evidence` | Tool receipt ledger for final-answer readiness |
| `internal/fileref` | File reference search (@path) |
| `internal/serve` | HTTP/SSE server frontend |
| `internal/notify` | OS notification sender (platform-specific) |
| `internal/command` | Custom slash commands from Markdown |
| `internal/diff` | Diff computation for file edits |
| `internal/doctor` | Diagnostics/reporting |
| `internal/boot` | Bootstrap/assembly from config |
| `internal/jobs` | Background job manager |
| `internal/instruction` | Project instructions + verify checks |
| `internal/installsource` | Plugin/skill installation from URLs |
| `internal/fileutil` | Atomic writes, encoding detection |
| `internal/frontmatter` | Frontmatter parsing for skills/commands |
| `internal/inspect` | Model response inspection |
| `internal/mcpdiag` | MCP auth diagnostics |
| `internal/netclient` | HTTP client abstraction |
| `internal/nilutil` | Nil-interface guard |
| `internal/outputstyle` | Output style/persona configuration |
| `internal/proc` | Process management (platform-specific) |
| `internal/sysproxy` | System proxy detection |
| `desktop/` | Wails-based desktop app (separate Go module) |
| `npm/` | npm distribution wrapper |
| `site/` | Astro-based project website |
| `workers/crash-report/` | Cloudflare Worker for crash reporting |
| `docs/` | Engineering spec, guides, migration docs |
| `benchmarks/` | E2E benchmark task suites |

### Dependency direction

```
cli → {agent, plugin, config} → {tool, provider}
```

Built-in subpackages import their parent to self-register via `init()`.
Parents never import children.

## Development workflow

### Building

```bash
make build          # go build — outputs to bin/
make test           # go test ./...
make vet            # go vet ./...
make fmt            # gofmt -w .
make hooks          # install git hooks (pre-push: go vet)
make cross          # cross-compile for all 6 targets
```

### Running tests

```bash
go test ./...                           # all tests
go test ./internal/agent/ -v            # verbose, one package
go test ./internal/tool/builtin/ -run TestGrep  # one test
```

### Code style

- `gofmt` is enforced by CI — format before committing
- Follow existing patterns: wrap errors with `fmt.Errorf("...: %w", err)`
- Library code never calls `os.Exit` or prints to stdout/stderr
- Only `cli/` and `main/` decide exit codes and user-facing messages
- Exported identifiers must have doc comments
- English is the primary language for all code — comments, user-facing strings,
  tool descriptions, system prompts. Chinese is allowed in i18n message files
  (`messages_zh.go`) and example config comments only.

### Commit messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(glob): add ** recursive pattern support
fix: replace silent error discards with structured logging
test(event): add comprehensive unit tests for event package
docs: add CONTRIBUTING.md
ci: add golangci-lint and govulncheck
```

## Adding a new built-in tool

1. Create `internal/tool/builtin/mytool.go`
2. Implement the `tool.Tool` interface: `Name()`, `Description()`, `Schema()`, `ReadOnly()`, `Execute()`
3. Register via `func init() { tool.RegisterBuiltin(myTool{}) }`
4. Add tests in `internal/tool/builtin/mytool_test.go`
5. The tool is automatically available — `main` blank-imports `builtin`

## Adding a new model provider

(For MCP tool servers see `internal/plugin` instead — that's a different layer.)

1. Create `internal/provider/myprovider/`
2. Implement `provider.Provider`: `Name()`, `Stream()`
3. Register via `func init() { provider.Register("mykind", New) }`
4. The provider is available from config with `kind = "mykind"`

## Adding i18n strings

1. Add the field to `internal/i18n/i18n.go` (`Messages` struct)
2. Add the value in `internal/i18n/messages_en.go` and `messages_zh.go`
3. The `TestCatalogsComplete` test will fail if you miss a locale

## Adding a bot channel

1. Create `internal/bot/mychannel/` with channel-specific adapter
2. Register in `internal/bot/gateway.go`
3. Add config section in `momapeer.example.toml`
4. Add i18n strings for channel-specific messages

## Submitting changes

1. Fork the repository
2. Create a feature branch from `main`
3. Make your changes with tests
4. Ensure `go test ./...` passes
5. Ensure `gofmt -l .` shows no changes
6. Submit a pull request to `main`

## Reporting issues

Open an issue on GitHub with:
- Steps to reproduce
- Expected vs actual behavior
- Go version and OS
- Relevant logs or error messages

## License

By contributing, you agree that your contributions will be licensed under the
same MIT license as the project.
