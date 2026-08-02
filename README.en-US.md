<p align="center">
  <img src="docs/logo.png" alt="momapeer" width="640"/>
</p>

<p align="center">
  <a href="./README_en.md">English</a>
  &nbsp;·&nbsp;
  <strong>简体中文</strong>
  &nbsp;·&nbsp;
  <a href="./docs/GUIDE.zh-CN.md">使用指南</a>
  &nbsp;·&nbsp;
  <a href="./docs/SPEC.md">架构规范</a>
</p>

<p align="center">
  <a href="https://github.com/zzycxz/momapeer/releases"><img src="https://img.shields.io/badge/version-v0.5.6-0153e5?style=flat-square" alt="Version 0.5.6"/></a>
  <a href="https://github.com/zzycxz/momapeer/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/zzycxz/momapeer/ci.yml?style=flat-square&label=ci&labelColor=161b22&logo=githubactions&logoColor=white" alt="CI"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/zzycxz/momapeer.svg?style=flat-square&color=8b949e&labelColor=161b22" alt="license"/></a>
  <a href="https://github.com/zzycxz/momapeer/stargazers"><img src="https://img.shields.io/github/stars/zzycxz/momapeer.svg?style=flat-square&color=dbab09&labelColor=161b22&logo=github&logoColor=white" alt="GitHub stars"/></a>
</p>

<br/>

<h3 align="center">China Mobile's native enterprise-grade AI programming assistant for the Jiutian platform.</h3>
<p align="center">
  Deeply crafted based on China Mobile MoMA (Jiutian) large model platform, delivering unparalleled coding intelligence and terminal experience.<br/>
  A single static Go binary with zero runtime dependencies, seamlessly covering multiple platforms.
</p>

<br/>

## What is momapeer?

momapeer is an AI-powered programming assistant built specifically for the China Mobile Jiutian (MoMA) platform ecosystem. It leverages highly configurable architecture and the MCP plugin system as core drivers. It not only provides powerful local code understanding capabilities but also deeply integrates with Jiutian's large models (such as DeepSeek, Qwen, GLM, and over 300 other models) to enable autonomous programming driven by natural language.

Agents can run in **terminal** (TUI), **desktop client** (based on Wails), **HTTP/SSE server**, or **multi-channel IM bots** (WeCom / Feishu / QQ) across all scenarios—all frontends are driven by the same high-performance, transport-agnostic core engine.

> **Open Source Statement:** This project is developed based on [DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix),
> with deep architectural optimizations and extensions tailored for China Mobile Jiutian platform and enterprise scenarios.

## Core Features

### Engineering and Ecosystem

- **Jiutian-Native Architecture** — Deeply optimized for MoMA platform integration, supporting thinking mode protocol, reasoning_content feedback, 16 preconfigured model CNY pricing, fully configurable via `momapeer.toml`.
- **MCP Plugin Ecosystem** — Fully supports Model Context Protocol (MCP), with external tools running as subprocesses via stdio / HTTP, enabling unlimited expansion of Agent capabilities.
- **Built-in Web Search** — Integrates Brave → Exa → Linkup three search engines with chained fallback, enabling web retrieval without external MCP.
- **Ultra-Fast Lightweight Distribution** — Single binary packaging with `CGO_ENABLED=0`, minimalist deployment, cross-compilation support for 6 major OS architectures.

### Built-in Intelligent Toolset

Native integration of concise IDE-level toolchain: `bash` · `read_file` (merged directory lists) · `write_file` · `edit_file` · `grep` (supports timeout) ·
`web_fetch` · `web_search` · `todo_write` · `complete_step` ·
`codegraph_*` (precision search for project-level symbols and call graphs based on tree-sitter).
Domain capabilities (browser/desktop automation/email/knowledge base/document/schedule/expert team) are encapsulated as subagent skills, available on-demand via `run_skill` calls.

### Exclusive Code Intelligence

- **CodeGraph Engine** — Constructs a lightweight localized code graph based on tree-sitter + SQLite. Zero API call overhead, silently builds AST index in the background, achieving precise method call and symbol tracing.
- **Full-Stack LSP Integration** — Deeply bound to mainstream language servers, providing diagnostics, definition jumping, and cross-reference capabilities.

### Autonomous Intelligence and Self-Improvement

- **Goal-Independent Judge** — Goal achievement assessment executed by independent LLM model (based on transcript evidence, temperature=0), preventing agent from prematurely stopping due to optimism.
- **Dream / Distill Self-Improvement** — Dream (7-day cycle) automatically consolidates session knowledge into project memory; Distill (30-day cycle) automatically discovers repeated workflows and packages them as reusable Skills.
- **Memory Archive Soft-Delete** — Deleted memories are moved to `.archive/` directory, traceable and restorable, no longer permanently lost.
- **Profile Isolated Memory** — dev/cowork mode memories are completely isolated, switching modes preserves accumulated data. User preferences and feedback-guided memories are shared across all projects in the same mode.

### Security and Reliability

- **Permission System Subject Evaluation** — Subject paths for writing tools (`write_file`/`edit_file`) and irreversible operations (`email_send`/`rag_delete`) undergo glob-matching approval, deny rules cannot be bypassed.
- **Checkpoint Path Traversal Protection** — `safePath` explicitly rejects escape vectors like `..`, UNC paths using `filepath.IsLocal`.
- **Memory Store Path Protection** — `safeJoin` prevents path traversal attacks via `remember` tool injection.
- **Summarizer Timeout Protection** — 90 second timeout prevents LLM streaming deadlocks that would permanently block compaction.
- **Transient 401 Retry** — Automatic retry on gateway occasional authentication failures, reducing false session interruptions.
- **Checkpoints and Time Travel** — Introduces file snapshot code modification safety net with `/rewind` for instant reversal, providing ultimate error recovery.

### Planning-Driven Mode

- **Plan Mode** — Automatically intercepts high-risk operations; Agent must submit "execution plan" and wait for manual approval before executing file modifications or sensitive shell commands.
- **Evidence-Backed Completion** — Each plan step must cite evidence (verification commands, diffs, file paths), preventing agents from claiming completion without actual output.
- **PlanModeFromContext** — Tools can self-check if running in plan mode, conditionally disabling write-related interfaces.

## All-Scenario Integration

| Frontend Form | Startup Command | Scenario Description |
|------|------|------|
| **Terminal TUI** | `momapeer chat` | Geek's choice: immersive terminal interface (based on Charm Bubble Tea) |
| **API Service** | `momapeer serve` | Open capability: provides standard HTTP/SSE programming interface |
| **Desktop Client** | Launch via Wails icon | UI interaction: native multi-tab experience across macOS / Windows / Linux |
| **Enterprise Bot** | `momapeer bot start` | Team collaboration: IM gateway integration with WeCom / Feishu / QQ |
| **ACP Service** | `momapeer acp` | Protocol bridging: Agent Control Protocol remote control layer |

## Installation Guide

Current Version: **v0.5.6**

```sh
npm i -g momapeer                        # Any system — automatically fetches native binary for the platform
brew install zzycxz/momapeer/momapeer    # macOS users
```

You can also obtain pre-built archives from [GitHub Releases](https://github.com/zzycxz/momapeer/releases) (supports `darwin|linux|windows × amd64|arm64`).

> **⚠️ macOS Desktop Installation Note:**
> If you downloaded the macOS desktop app in `.zip` format, since this is an open-source project without Apple developer signature, launching it by double-clicking after extraction may prompt **"App is damaged and can't be opened" (file is damaged, please move to trash).**
>
> **Solution:** Open Terminal and run the following command to remove quarantine protection (assuming the app is in the Downloads folder):
> ```sh
> xattr -cr ~/Downloads/momapeer.app
> ```
> Then it can be launched normally by double-clicking.

### Build From Source

```sh
make build    # Compile to bin/ directory
make cross    # Cross-compile to dist/ (generates binaries for 6 target platforms)
```
*(Requires Go 1.25+)*

## Quick Start and Configuration

```sh
momapeer setup                        # Launch configuration wizard → generate ./momapeer.toml
export JIUTIAN_API_KEY=your-key-here  # Set Jiutian platform key (or write to .env)
momapeer chat                         # Enter interactive terminal, input /init to generate project context
momapeer run "Implement all TODOs in main.go"
momapeer run --model moma/deepseek/deepseek-v4-flash "Add unit tests"
echo "Explain this code" | momapeer run
```

## Integration with China Mobile MoMA (Jiutian Platform)

[MoMA Platform](https://jiutian.10086.cn) is an enterprise-grade aggregated model platform created by China Mobile, fully compatible with standard protocols.

### Step 1: Obtain Platform API Key
1. Visit the [Jiutian Official Platform](https://jiutian.10086.cn) to register and log in.
2. Enter the **Key Management** page, create your exclusive authorization key.
3. Copy this key to use as environment variable `JIUTIAN_API_KEY`.

### Step 2: Configure Environment Variables
```sh
# Linux / macOS
export JIUTIAN_API_KEY="Your actual key"

# Windows (PowerShell)
$env:JIUTIAN_API_KEY = "Your actual key"
```

### Step 3: Configure Provider (`momapeer.toml`)
Create or modify `momapeer.toml` in the project root directory:

```toml
default_model = "moma"

[[providers]]
name        = "moma"
kind        = "openai"
base_url    = "https://jiutian.10086.cn/largemodel/moma/api/v3"
model       = "moma/jiutian/jiutian-lan-35b"
api_key_env = "JIUTIAN_API_KEY"
```

After configuration completion, simply execute `momapeer chat` to experience intelligent programming empowered by Jiutian's large models.

> **💡 Advanced Tip: Customize AI Identity and Standards**
> If you want the AI to better understand your team's development standards, create or modify `momapeer.md` in the project root directory, writing your exclusive rules and identity declarations. The AI will automatically read and follow these settings in each conversation.

### MoMA Recommended Models

In the `model` field, support using `provider/brand/model_name` to flexibly switch models. Jiutian platform recommended models:

| Model ID | Core Advantages | Applicable Scenarios |
|---------|------|------|
| `moma/jiutian/jiutian-lan-35b` | Strong comprehensive capabilities, logical rigor | Core architecture design, complex demand analysis, primary coding |
| `moma/jiutian/jiutian-lan-236b` | Flagship model, strongest reasoning | Complex architecture design, difficult bugs, cross-module refactoring |
| `moma/deepseek/deepseek-v4-flash` | Extremely fast response, code-specialized | Code snippet completion, rapid refactoring, unit test generation |

For a complete list of models, log in to the [Jiutian Platform Console](https://jiutian.10086.cn/largemodel/llmstudio/#/modelHub). Simply modify the `model` field to achieve seamless hot switching, zero code intrusion.

## Documentation Guide

| Reference Document | Content Coverage |
|------|------|
| **[User Guide](./docs/GUIDE.zh-CN.md)** | Permission control, sandbox execution, MCP plugins, terminal slash commands, `@` syntax, Plan mode, background models |
| **[Architecture Specification](./docs/SPEC.md)** | Engineering contract: system architecture, Registry mechanisms, data type constraints and long-term roadmap |
| **[Snapshot Mechanism](./docs/CHECKPOINTS.md)** | File snapshot code modification safety net design |
| **[Session Architecture](./docs/SESSION_REFERENCE_ARCHITECTURE.md)** | Session lifecycle management, state persistence and seamless recovery mechanism |
| **[RAG Knowledge Base Guide](./docs/RAG_GUIDE.md)** | Document import, knowledge graph, entity extraction, semantic search, @ references |
| **[Expert Team Guide](./docs/EXPERT_GUIDE.md)** | Multi-model collaboration, team configuration, collaboration mode, session history |
| **[Office Automation Guide](./docs/OFFICE_GUIDE.md)** | Email integration, calendar tasks, PPT generation, scheduled tasks |
| **[Contribution Guide](./CONTRIBUTING.md)** | Developer essentials: how to add new tools, new Providers, and customize robot channels |
| **[Change Log](./CHANGELOG.md)** | Historical release records and feature iterations |

## Core Architecture Diagram

```
Developer → CLI / Desktop / HTTP / Bot / ACP
             ↓
             control.Controller  (Transport-protocol-agnostic session driver layer)
             ↓
             agent.Agent         (ReAct loop core: thought stream → tool scheduling → result parsing → …)
             ↓
             provider.Provider   (Interface integration with Jiutian large models, etc.)
             tool.Registry       (Execution sandbox: Built-in Native tools + MCP external plugins)
```

The project contains over 40 strictly decoupled internal packages, with dependency graphs following strict acyclic unidirectional flow:
`cli → {agent, plugin, config} → {tool, provider}`

---

<p align="center">
  <sub>MIT License —— See <a href="./LICENSE">LICENSE</a> file for details.</sub>
</p>
# Updated
