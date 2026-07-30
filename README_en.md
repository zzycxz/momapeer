<p align="center">
  <img src="docs/logo.png" alt="momapeer" width="640"/>
</p>

<p align="center">
  <strong>English</strong>
  &nbsp;·&nbsp;
  <a href="./README.md">简体中文</a>
  &nbsp;·&nbsp;
  <a href="./docs/GUIDE.md">Guide</a>
  &nbsp;·&nbsp;
  <a href="./docs/SPEC.md">Spec</a>
</p>

<p align="center">
  <a href="https://github.com/zzycxz/momapeer/releases"><img src="https://img.shields.io/badge/version-v0.5.6-0153e5?style=flat-square" alt="Version 0.5.6"/></a>
  <a href="https://github.com/zzycxz/momapeer/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/zzycxz/momapeer/ci.yml?style=flat-square&label=ci&labelColor=161b22&logo=githubactions&logoColor=white" alt="CI"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/zzycxz/momapeer.svg?style=flat-square&color=8b949e&labelColor=161b22" alt="license"/></a>
  <a href="https://github.com/zzycxz/momapeer/stargazers"><img src="https://img.shields.io/github/stars/zzycxz/momapeer.svg?style=flat-square&color=dbab09&labelColor=161b22&logo=github&logoColor=white" alt="GitHub stars"/></a>
</p>

<br/>

<h3 align="center">China Mobile MoMA-native enterprise AI coding agent.</h3>
<p align="center">
  Built exclusively for the China Mobile MoMA (九天) aggregated model platform.<br/>
  A single static Go binary. Zero runtime dependencies. Cross-platform distribution.
</p>

<br/>

## What is momapeer?

momapeer is an enterprise-grade AI coding agent designed specifically for the China Mobile Jiutian (MoMA) ecosystem. Driven by a highly configurable core and Model Context Protocol (MCP) plugins, momapeer natively integrates with MoMA models (DeepSeek, Qwen, GLM, and 300+ others) to provide autonomous, natural-language-driven programming capabilities.

The agent can run in your **terminal** (TUI), as a **native desktop app** (Wails), as an **HTTP/SSE server**, or as a **multi-channel IM bot** (WeCom / Feishu / QQ) — all powered by a single, high-performance, transport-agnostic engine.

> **Open Source Attribution:** This project is derived from [DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix),
> deeply optimized and architecturally expanded for the China Mobile Jiutian platform and enterprise scenarios.

## Core Features

### Architecture & Ecosystem

- **MoMA-Native Architecture** — Deep integration with the MoMA platform: thinking mode protocol, reasoning_content round-trip, 16 built-in models with CNY pricing. Fully configuration-driven via `momapeer.toml`.
- **MCP Plugin Ecosystem** — Full support for Model Context Protocol (MCP). External tools run as subprocesses over stdio / HTTP, providing infinite extensibility.
- **Built-in Web Search** — Integrated Brave → Exa → Linkup three-engine fallback chain search, no external MCP needed.
- **Zero-Friction Distribution** — `CGO_ENABLED=0` single binary. Cross-compiled for 6 major OS/Architecture targets.

### Built-in Intelligent Tools

Native integration of a streamlined IDE-grade toolchain: `bash` · `read_file` (doubles as directory listing) · `write_file` · `edit_file` · `grep` (with timeout) ·
`web_fetch` · `web_search` · `todo_write` · `complete_step` ·
`codegraph_*` (Tree-sitter based project-wide symbol and call-graph semantic search).
Domain capabilities (browser/desktop automation, email, knowledge base, documents, scheduling, expert teams) are packaged as subagent skills, invoked on demand via `run_skill`.

### Exclusive Code Intelligence

- **CodeGraph Engine** — A lightweight, local code graph built on Tree-sitter + SQLite. Zero API cost, background silent indexing, enabling precise method invocation and symbol tracking.
- **Full-Stack LSP Integration** — Deep binding with mainstream language servers for diagnostics, go-to-definition, and cross-references.

### Autonomous Intelligence & Self-Evolution

- **Goal Independent Judge** — Goal completion evaluation by a separate LLM (based on transcript evidence, temperature=0), preventing premature optimistic stops.
- **Max Mode (Best-of-N)** — N parallel candidate reasoning + independent judge selection. Ideal for complex architecture design and hard bugs, significantly improving reasoning quality.
- **Dream / Distill Self-Evolution** — Dream (7-day cycle) auto-consolidates session knowledge into project memory; Distill (30-day cycle) auto-discovers repeated workflows and packages them as reusable Skills.
- **Memory FTS5 Full-Text Search** — SQLite FTS5 + BM25 ranked memory search. Retrieves by relevance instead of injecting all memories, with token cost growing linearly with memory count.
- **Memory Archive Soft Delete** — Deleted memories are moved to `.archive/` directory, traceable and recoverable, never permanently lost.
- **GlobalDir Cross-Project Memory** — User preferences and feedback memories are shared across all projects, preserving accumulated knowledge when switching contexts.

### Safety & Reliability

- **Subject-based Permission Evaluation** — Writer tools (`write_file`/`edit_file`) and irreversible operations (`email_send`/`rag_delete`) have their subject paths glob-matched for approval; deny rules cannot be bypassed.
- **Checkpoint Path Traversal Protection** — `safePath` uses `filepath.IsLocal` to explicitly reject `..`, UNC paths, and other escape vectors.
- **Memory Store Path Protection** — `safeJoin` prevents path traversal attacks via the `remember` tool.
- **Summarizer Timeout Protection** — 90-second timeout prevents LLM stream stalls from permanently blocking compaction.
- **Transient 401 Retry** — Automatic retry on transient gateway authentication failures, reducing spurious session interruptions.
- **Checkpoints & Rewind** — Snapshot-based safety net for code modifications. Supports `/rewind` for instant undo, providing maximum fault tolerance.

### Plan-Driven Mode

- **Plan Mode** — Automatically intercepts high-risk operations. The Agent must submit an "execution plan" and wait for human sign-off before modifying files or executing sensitive shell commands.
- **Evidence-Backed Completion** — Every plan step must cite evidence (verification command, diff, file paths), preventing the agent from claiming completion without actual output.
- **PlanModeFromContext** — Tools can introspect whether they are running under plan mode, conditionally disabling writer-only surfaces.

## Frontend Channels

| Frontend | Command | Description |
|------|------|------|
| **Terminal TUI** | `momapeer chat` | For geeks: Immersive terminal UI (Charm Bubble Tea) |
| **API Server** | `momapeer serve` | Open capabilities: Standard HTTP/SSE programmatic interface |
| **Desktop App** | Wails Launcher | UI interaction: Native macOS / Windows / Linux multi-tab experience |
| **Enterprise Bot** | `momapeer bot start` | Team collaboration: WeCom / Feishu / QQ IM gateway integration |
| **ACP Server** | `momapeer acp` | Protocol bridge: Agent Control Protocol remote execution layer |

## Install

Current Release: **v0.5.6**

```sh
npm i -g momapeer                        # Any OS — pulls the prebuilt native binary
brew install zzycxz/momapeer/momapeer    # macOS users
```

You can also download prebuilt archives (`darwin|linux|windows × amd64|arm64`) directly from [GitHub Releases](https://github.com/zzycxz/momapeer/releases).

> **⚠️ macOS Desktop Installation Guide:**
> If you downloaded the `.zip` archive for the macOS desktop app, because this is an open-source project without Apple developer signing, extracting and running the app might trigger an **"App is damaged and can't be opened"** warning.
>
> **Solution:** Open your terminal and run the following command to remove the quarantine attribute (assuming the app is in your Downloads folder):
> ```sh
> xattr -cr ~/Downloads/momapeer.app
> ```
> After running this, you can double-click and open the app normally.

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
momapeer run --model moma/deepseek/deepseek-v4-flash "add unit tests"
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
| `moma/jiutian/jiutian-lan-236b` | Flagship model, strongest reasoning | Complex architecture design, tricky bugs, cross-module refactoring |
| `moma/deepseek/deepseek-v4-flash` | Extremely fast response, code-specialized | Code snippet completion, quick refactoring, unit tests |

> For the full list of available models, visit the [Jiutian Platform Console](https://jiutian.10086.cn/largemodel/llmstudio/#/modelHub). Switch models seamlessly by changing the `model` field — zero code changes required.

## Documentation Reference

| Document | Contents |
|------|------|
| **[Guide](./docs/GUIDE.md)** | Permissions, sandbox execution, MCP plugins, slash commands, `@` refs, plan mode, background model |
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
