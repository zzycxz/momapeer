<p align="center">
  <img src="docs/logo.png" alt="momapeer" width="640"/>
</p>

<p align="center">
  <strong>English</strong>
  &nbsp;·&nbsp;
  <a href="./README.zh-CN.md">简体中文</a>
  &nbsp;·&nbsp;
  <a href="./docs/GUIDE.md">Guide</a>
  &nbsp;·&nbsp;
  <a href="./docs/SPEC.md">Spec</a>
</p>

<p align="center">
  <a href="https://github.com/zzycxz/momapeer/releases/tag/v0.1.0"><img src="https://img.shields.io/badge/version-v0.1.0-0153e5?style=flat-square" alt="Version 0.1.0"/></a>
  <a href="https://github.com/zzycxz/momapeer/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/zzycxz/momapeer/ci.yml?style=flat-square&label=ci&labelColor=161b22&logo=githubactions&logoColor=white" alt="CI"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/zzycxz/momapeer.svg?style=flat-square&color=8b949e&labelColor=161b22" alt="license"/></a>
  <a href="https://github.com/zzycxz/momapeer/stargazers"><img src="https://img.shields.io/github/stars/zzycxz/momapeer.svg?style=flat-square&color=dbab09&labelColor=161b22&logo=github&logoColor=white" alt="GitHub stars"/></a>
</p>

<br/>

<h3 align="center">China Mobile MoMA-native AI coding agent for your terminal, desktop, and server.</h3>
<p align="center">
  Built exclusively for the China Mobile MoMA (九天) aggregated model platform.<br/>
  A single static Go binary. Zero runtime dependencies. Cross-platform distribution.
</p>

<br/>

## What is momapeer?

momapeer (v0.1.0) is an enterprise-grade AI coding agent designed specifically for the China Mobile Jiutian (MoMA) ecosystem. Driven by a highly configurable core and Model Context Protocol (MCP) plugins, momapeer natively integrates with powerful MoMA models (like `jiutian-lan-35b`) to provide autonomous, natural-language-driven programming capabilities.

The agent can run in your **terminal** (TUI), as a **native desktop app** (Wails), as an **HTTP/SSE server**, or as a **multi-channel IM bot** (WeCom / Feishu) — all powered by a single, high-performance, transport-agnostic engine.

## Core Features

### Architecture & Ecosystem

- **MoMA-Native Architecture** — Seamlessly connects to the China Mobile MoMA platform. Fully configuration-driven (`momapeer.toml`) with zero intrusive hardcoded logic.
- **Dual-Model Coordination** — Supports advanced dual-model orchestration (e.g., a logic-driven Planner + a code-generation Executor) to drastically reduce hallucination.
- **MCP Plugin Ecosystem** — Full support for the Model Context Protocol (MCP). External tools run as subprocesses over stdio JSON-RPC, providing infinite extensibility.
- **Zero-Friction Distribution** — Packaged as a `CGO_ENABLED=0` single binary. Cross-compiled for 6 major OS/Architecture targets for instant deployment.

### Built-in Intelligent Tools (20+)

Native integration of a full IDE-grade toolchain: `bash` · `read_file` · `write_file` · `edit_file` · `multi_edit` · `glob` · `grep` · `ls` ·
`web_fetch` · `todo_write` · `complete_step` · `notebook_edit` · `workspace` · `preview` · `gitignore` ·
`codegraph_*` (Tree-sitter based project-wide symbol and call-graph semantic search).

### Exclusive Code Intelligence

- **CodeGraph Engine** — A lightweight, local code graph built on Tree-sitter + SQLite. Zero API cost, background silent indexing, enabling precise method invocation and symbol tracking.
- **Full-Stack LSP Integration** — Deep binding with mainstream language servers for diagnostics, go-to-definition, and cross-references.

### Automation & Security

- **Plan Mode** — Automatically intercepts high-risk operations. The Agent must submit an "execution plan" and wait for human sign-off before modifying files or executing sensitive shell commands.
- **Hierarchical Memory** — Multi-tiered knowledge base (Project / Personal / Global) with automatic memory storage. The model learns your codebase over time.
- **Checkpoints & Rewind** — Snapshot-based safety net for code modifications. Supports `/rewind` for instant undo, providing maximum fault tolerance.

## Frontend Channels

| Frontend | Command | Description |
|------|------|------|
| **Terminal TUI** | `momapeer chat` | For geeks: Immersive terminal UI (Charm Bubble Tea) |
| **API Server** | `momapeer serve` | Open capabilities: Standard HTTP/SSE programmatic interface |
| **Desktop App** | Wails Launcher | UI interaction: Native macOS / Windows / Linux experience |
| **Enterprise Bot** | `momapeer bot start` | Team collaboration: WeCom / Feishu IM gateway integration |
| **ACP Server** | `momapeer acp` | Protocol bridge: Agent Control Protocol remote execution layer |

## Install

Current Release: **v0.1.0**

```sh
npm i -g momapeer                        # Any OS — pulls the prebuilt native binary
brew install zzycxz/momapeer/momapeer    # macOS users
```

You can also download prebuilt archives (`darwin|linux|windows × amd64|arm64`) directly from [GitHub Releases](https://github.com/zzycxz/momapeer/releases).

### Build from source

```sh
make build    # Output to bin/ directory
make cross    # Cross-compile for 6 target platforms to dist/
```
*(Requires Go 1.25+)*

## Quick Start & Configuration

```sh
momapeer setup                        # Configuration wizard → generates ./momapeer.toml
export JIUTIAN_API_KEY=your-key-here  # Set your Jiutian platform API key (or add to .env)
momapeer chat                         # Enter the interactive TUI, type /init to generate project context
momapeer run "implement all TODOs in main.go"
momapeer run --model moma/jiutian/jiutian-code-8b "add unit tests"
echo "explain this code block" | momapeer run
```

## Connecting to China Mobile MoMA (九天)

[MoMA](https://jiutian.10086.cn) is China Mobile's enterprise-grade aggregated model platform, fully compatible with standard API protocols.

### Step 1: Obtain an API Key
1. Go to the [Jiutian Official Platform](https://jiutian.10086.cn) to register and log in.
2. Navigate to **Key Management** (密钥管理) and create a new authentication key.
3. Copy this key to use as your `JIUTIAN_API_KEY`.

### Step 2: Set the Environment Variable
```sh
# Linux / macOS
export JIUTIAN_API_KEY="your-real-api-key"

# Windows (PowerShell)
$env:JIUTIAN_API_KEY = "your-real-api-key"
```

### Step 3: Configure the Provider (`momapeer.toml`)
Create or modify `momapeer.toml` in your project root:

```toml
default_model = "moma"

[[providers]]
name        = "moma"           
kind        = "openai"         
base_url    = "https://jiutian.10086.cn/largemodel/moma/api/v3"
model       = "moma/jiutian/jiutian-lan-35b"
api_key_env = "JIUTIAN_API_KEY"
```

Once configured, simply run `momapeer chat` to experience the intelligent programming power of Jiutian models.

> **💡 Pro Tip: Customizing AI Identity & Rules**
> If you want the AI to better understand your team's development standards, you can create or modify `momapeer.md` in your project root to write down your specific rules and identity declarations. The AI will automatically read and follow these instructions in every conversation.

### Recommended MoMA Models

In the `model` field, you can flexibly switch using the `provider/vendor/model-name` format. Recommended Jiutian models:

| Model ID | Core Advantage | Best For |
|---------|------|------|
| `moma/jiutian/jiutian-lan-35b` | Strong comprehensive capability, rigorous logic | Core architecture design, complex analysis, primary coding |
| `moma/jiutian/jiutian-code-8b` | Extremely fast response, code-specialized | Code snippet completion, quick refactoring, unit tests |
| `moma/jiutian/jiutian-lan-8b` | Optimal balance of cost and speed | Documentation translation, simple text processing, routing |

> For the full list of available models, visit the [Jiutian Platform Console](https://jiutian.10086.cn). Switch models seamlessly by changing the `model` field — zero code changes required.

## Documentation Reference

| Document | Contents |
|------|------|
| **[Guide](./docs/GUIDE.md)** | Permissions, sandbox execution, MCP plugins, slash commands, `@` refs, dual-model setup |
| **[Specification](./docs/SPEC.md)** | Engineering contract: architecture, registry mechanism, data types, and roadmap |
| **[Checkpoints](./docs/CHECKPOINTS.md)** | Snapshot-based safety net for code modifications |
| **[Session Architecture](./docs/SESSION_REFERENCE_ARCHITECTURE.md)** | Session lifecycle management, persistence, and seamless resumption |
| **[Contributing](./CONTRIBUTING.md)** | Developer guide: Adding new tools, Providers, and custom bot channels |
| **[Changelog](./CHANGELOG.md)** | Historical release records and feature iterations |

## Core Architecture

```
Developer → CLI / Desktop / HTTP / Bot / ACP
             ↓
             control.Controller  (Transport-agnostic session driver)
             ↓
             agent.Agent         (ReAct loop core: think stream → tool dispatch → parse → …)
             ↓
             provider.Provider   (Connects to Jiutian LLMs via standard interfaces)
             tool.Registry       (Execution sandbox: Native tools + MCP external plugins)
```

The project contains over 40 strictly decoupled internal packages. The dependency graph follows a strict, acyclic, one-way flow:
`cli → {agent, plugin, config} → {tool, provider}`



---

<p align="center">
  <sub>MIT License — see the <a href="./LICENSE">LICENSE</a> file for details.</sub>
</p>
