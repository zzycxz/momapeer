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

<h3 align="center">中国移动九天原生的企业级全场景 AI 编程助手。</h3>
<p align="center">
  基于中国移动 MoMA（九天）大模型平台深度打造，提供极致的编码智能与终端体验。<br/>
  单一静态 Go 二进制，零运行时依赖，多平台无缝覆盖。
</p>

<br/>

## momapeer 是什么？

momapeer 是一款专为中国移动九天 (MoMA) 平台生态打造的 AI 智能编程助手，以高度可配置化和 MCP 插件体系为核心驱动力。
它不仅提供强大的本地代码理解能力，更能深度接入九天大模型（如 DeepSeek、Qwen、GLM 等 300+ 模型）实现自然语言驱动的自主编程。

Agent 可以在 **终端**（TUI）、**桌面客户端**（基于 Wails）、**HTTP/SSE 服务器** 或 **多通道 IM 机器人**（企业微信 / 飞书 / QQ）等全场景中运行——所有前端均由同一个高性能、传输无关的核心引擎驱动。

> **开源声明：** 本项目基于 [DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix) 进行二次开发，
> 针对中国移动九天平台与企业级场景进行了深度的架构优化与扩展。

## 核心特性

### 工程化与生态

- **九天原生架构** — 深度优化对接 MoMA 平台，支持 thinking mode 协议、reasoning_content 回传、16 个预置模型 CNY 定价，通过 `momapeer.toml` 完全配置驱动。
- **MCP 插件生态** — 全面支持 Model Context Protocol (MCP)，外部工具以子进程形式通过 stdio / HTTP 运行，无限扩展 Agent 能力。
- **内置 Web Search** — 集成 Brave → Exa → Linkup 三引擎链式降级搜索，无需外部 MCP 即可联网检索。
- **极速轻量分发** — `CGO_ENABLED=0` 单二进制打包，极简部署，支持交叉编译 6 大操作系统架构。

### 内置智能工具箱

原生集成精简 IDE 级工具链：`bash` · `read_file`（兼并目录列表） · `write_file` · `edit_file` · `grep`（支持超时） ·
`web_fetch` · `web_search` · `todo_write` · `complete_step` ·
`codegraph_*`（基于 tree-sitter 的项目级符号与调用图谱精准搜索）。
领域能力（浏览器/桌面自动化/邮件/知识库/文档/日程/专家团队）封装为 subagent skill，按需通过 `run_skill` 调用。

### 独家代码智能

- **CodeGraph 引擎** — 基于 tree-sitter + SQLite 构建本地化轻量级代码图谱。零 API 调用开销，后台静默建立 AST 索引，实现精准的方法调用与符号追踪。
- **全栈 LSP 集成** — 与主流语言服务器深度绑定，提供诊断、跳转定义与交叉引用能力。

### 自主智能与自进化

- **Goal 独立 Judge** — 目标达成评估由独立 LLM 模型执行（基于 transcript 证据，temperature=0），防止代理乐观停止。
- **Dream / Distill 自进化** — Dream（7 天周期）自动沉淀会话知识到项目记忆；Distill（30 天周期）自动发现重复工作流并打包为可复用 Skill。
- **Memory Archive 软删除** — 记忆删除后移至 `.archive/` 目录，可追溯恢复，不再永久丢失。
- **Profile 隔离记忆** — dev/cowork 模式记忆目录完全隔离，切换模式不丢失积累。用户偏好和反馈指导记忆在同模式所有项目间共享。

### 安全与可靠性

- **权限系统 subject 评估** — 写工具（`write_file`/`edit_file`）和不可逆操作（`email_send`/`rag_delete`）的 subject 路径经 glob 匹配审批，deny 规则不会被绕过。
- **Checkpoint 路径穿越防护** — `safePath` 使用 `filepath.IsLocal` 显式拒绝 `..`、UNC 路径等逃逸向量。
- **Memory store 路径防护** — `safeJoin` 防止通过 `remember` 工具注入路径穿越攻击。
- **Summarizer 超时保护** — 90 秒超时防止 LLM 流式卡死导致 compaction 永久阻塞。
- **Transient 401 重试** — 网关偶发认证失败自动重试，减少虚假会话中断。
- **检查点与时光倒流** — 引入代码修改快照系统，支持 `/rewind` 一键撤销，提供极致的容错安全网。

### 规划驱动模式

- **Plan Mode** — 自动拦截高危操作，Agent 在执行文件修改或敏感 Shell 命令前需提交"执行规划"并等待人工签核。
- **Evidence-Backed 完成** — 每个计划步骤必须引用证据（验证命令、diff、文件路径），防止代理声称完成而无实际产出。
- **PlanModeFromContext** — 工具可自查是否在 plan mode 下运行，条件性禁用写入相关界面。

## 全场景接入

| 前端形态 | 启动命令 | 场景说明 |
|------|------|------|
| **终端 TUI** | `momapeer chat` | 极客首选：沉浸式终端界面（基于 Charm Bubble Tea） |
| **API 服务** | `momapeer serve` | 开放能力：提供标准 HTTP/SSE 编程接入接口 |
| **桌面客户端** | Wails 图标启动 | UI 交互：提供原生 macOS / Windows / Linux 多标签体验 |
| **企业机器人** | `momapeer bot start` | 团队协作：企业微信 / 飞书 / QQ 等 IM 网关接入 |
| **ACP 服务** | `momapeer acp` | 协议桥接：Agent Control Protocol 远程控制层 |

## 安装指南

当前版本：**v0.5.6**

```sh
npm i -g momapeer                        # 任意系统——自动拉取对应平台的原生二进制
brew install zzycxz/momapeer/momapeer    # macOS 用户
```

您也可以在 [GitHub Releases](https://github.com/zzycxz/momapeer/releases) 获取预编译归档文件（支持 `darwin|linux|windows × amd64|arm64`）。

> **⚠️ macOS 桌面版安装必读：**
> 如果您下载了 `.zip` 格式的 macOS 桌面端应用，由于这是开源项目未进行 Apple 开发者签名，解压后双击运行可能会提示 **"App is damaged and can't be opened"（文件已损坏，请移至废纸篓）**。
>
> **解决办法：** 打开终端，运行以下命令解除隔离保护（假设 App 在下载目录）：
> ```sh
> xattr -cr ~/Downloads/momapeer.app
> ```
> 然后即可正常双击运行。

### 从源码编译

```sh
make build    # 编译到 bin/ 目录
make cross    # 交叉编译至 dist/（生成 6 个目标平台二进制）
```
*(需安装 Go 1.25+)*

## 快速上手与配置

```sh
momapeer setup                        # 启动配置向导 → 生成 ./momapeer.toml
export JIUTIAN_API_KEY=your-key-here  # 设置九天平台密钥 (或写入 .env)
momapeer chat                         # 进入交互终端，输入 /init 生成项目上下文
momapeer run "实现 main.go 里的所有 TODO"
momapeer run --model moma/deepseek/deepseek-v4-flash "补充单元测试"
echo "解释这段代码" | momapeer run
```

## 接入中国移动 MoMA（九天平台）

[MoMA 平台](https://jiutian.10086.cn) 是中国移动打造的企业级聚合模型平台，全面兼容标准协议。

### 第一步：获取平台 API Key
1. 前往 [九天官方平台](https://jiutian.10086.cn) 注册并登录。
2. 进入 **密钥管理** 页面，创建您的专属鉴权密钥。
3. 复制该密钥，作为环境变量 `JIUTIAN_API_KEY` 使用。

### 第二步：配置环境变量
```sh
# Linux / macOS
export JIUTIAN_API_KEY="您的真实密钥"

# Windows (PowerShell)
$env:JIUTIAN_API_KEY = "您的真实密钥"
```

### 第三步：配置 Provider (`momapeer.toml`)
在项目根目录创建或修改 `momapeer.toml`：

```toml
default_model = "moma"

[[providers]]
name        = "moma"
kind        = "openai"
base_url    = "https://jiutian.10086.cn/largemodel/moma/api/v3"
model       = "moma/jiutian/jiutian-lan-35b"
api_key_env = "JIUTIAN_API_KEY"
```

完成配置后，只需执行 `momapeer chat`，即可开始体验九天大模型的智能编程赋能。

> **💡 进阶技巧：定制 AI 身份与规范**
> 如果你想让 AI 更懂你们团队的开发规范，可以在项目根目录创建或修改 `momapeer.md`，写上你的专属规则和身份声明。AI 会在每次对话时自动读取并遵循这些设定。

### MoMA 推荐模型

在 `model` 字段中，支持使用 `provider/厂商/模型名` 灵活切换。九天平台专属推荐：

| 模型 ID | 核心优势 | 适用场景 |
|---------|------|------|
| `moma/jiutian/jiutian-lan-35b` | 综合能力强大，逻辑严密 | 核心架构设计、复杂需求分析、主力编码 |
| `moma/jiutian/jiutian-lan-236b` | 旗舰模型，推理最强 | 复杂架构设计、疑难 bug、跨模块重构 |
| `moma/deepseek/deepseek-v4-flash` | 极速响应，代码专精 | 代码片段补全、快速重构、单元测试生成 |

> 完整模型列表请登录 [九天平台控制台](https://jiutian.10086.cn/largemodel/llmstudio/#/modelHub) 查看。只需修改 `model` 字段即可无缝热切换，零代码侵入。

## 文档指引

| 参考文档 | 涵盖内容 |
|------|------|
| **[使用指南](./docs/GUIDE.zh-CN.md)** | 权限控制、沙盒运行、MCP 插件、终端斜杠命令、`@` 语法、Plan 模式、后台模型 |
| **[架构规格](./docs/SPEC.md)** | 工程契约：系统架构、Registry 机制、数据类型约束与长期路线图 |
| **[快照机制](./docs/CHECKPOINTS.md)** | 基于文件快照的代码修改安全网设计 |
| **[Session 架构](./docs/SESSION_REFERENCE_ARCHITECTURE.md)** | 会话生命周期管理、状态持久化与无缝恢复机制 |
| **[RAG 知识库指南](./docs/RAG_GUIDE.md)** | 文档导入、知识图谱、实体提取、语义搜索、@引用 |
| **[专家团指南](./docs/EXPERT_GUIDE.md)** | 多模型协作、团队配置、协作模式、会话历史 |
| **[办公自动化指南](./docs/OFFICE_GUIDE.md)** | 邮件集成、日历任务、PPT 生成、定时任务 |
| **[贡献指南](./CONTRIBUTING.md)** | 开发者必读：如何添加新工具、新 Provider 以及定制机器人通道 |
| **[更新日志](./CHANGELOG.md)** | 历史发版记录与功能迭代 |

## 核心架构图

```
Developer → CLI / Desktop / HTTP / Bot / ACP
             ↓
             control.Controller  (传输协议无关的会话驱动层)
             ↓
             agent.Agent         (ReAct 循环核心: 思考流 → 工具调度 → 结果解析 → …)
             ↓
             provider.Provider   (对接九天大模型等标准接口)
             tool.Registry       (执行器沙盒：内置 Native 工具 + MCP 外挂插件)
```

项目包含 40 余个严格解耦的 Internal 包，依赖图谱遵循严格的无环单向流动：
`cli → {agent, plugin, config} → {tool, provider}`

---

<p align="center">
  <sub>MIT License —— 详情参见 <a href="./LICENSE">LICENSE</a> 文件。</sub>
</p>
# Updated
