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
  <a href="https://github.com/zzycxz/momapeer/releases/tag/v0.1.0"><img src="https://img.shields.io/badge/version-v0.1.0-0153e5?style=flat-square" alt="Version 0.1.0"/></a>
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

momapeer (v0.1.0) 是一款专为中国移动九天 (MoMA) 平台生态打造的 AI 智能编程助手，以高度可配置化和 MCP 插件体系为核心驱动力。
它不仅提供强大的本地代码理解能力，更能深度接入九天大模型（如 `jiutian-lan-35b`）实现自然语言驱动的自主编程。

Agent 可以在 **终端**（TUI）、**桌面客户端**（基于 Wails）、**HTTP/SSE 服务器** 或 **多通道 IM 机器人**（企业微信 / 飞书）等全场景中运行——所有前端均由同一个高性能、传输无关的核心引擎驱动。

> **开源声明：** 本项目是在 [deepseek-reasonix](https://github.com/esengine/DeepSeek-Reasonix) 基础上进行的二次开发与重构，针对中国移动九天平台与企业级场景进行了深度的架构优化与扩展。

## 核心特性

### 工程化与生态

- **九天原生架构** — 深度优化对接 MoMA 平台，通过 `momapeer.toml` 完全配置驱动，无侵入性。
- **双模型协作引擎** — 支持双端模型协作（例如：逻辑推演规划器 + 代码生成执行器），大幅降低幻觉。
- **MCP 插件生态** — 全面支持 Model Context Protocol (MCP)，外部工具以子进程形式通过 stdio JSON-RPC 运行，无限扩展 Agent 能力。
- **极速轻量分发** — `CGO_ENABLED=0` 单二进制打包，极简部署，支持交叉编译 6 大操作系统架构。

### 内置智能工具箱（20+）

原生集成全套 IDE 级工具链：`bash` · `read_file` · `write_file` · `edit_file` · `multi_edit` · `glob` · `grep` · `ls` ·
`web_fetch` · `todo_write` · `complete_step` · `notebook_edit` · `workspace` · `preview` · `gitignore` ·
`codegraph_*`（基于 tree-sitter 的项目级符号与调用图谱精准搜索）。

### 独家代码智能

- **CodeGraph 引擎** — 基于 tree-sitter + SQLite 构建本地化轻量级代码图谱。零 API 调用开销，后台静默建立 AST 索引，实现精准的方法调用与符号追踪。
- **全栈 LSP 集成** — 与主流语言服务器深度绑定，提供诊断、跳转定义与交叉引用能力。

### 自动化与安全

- **规划驱动模式 (Plan Mode)** — 自动拦截高危操作，Agent 在执行文件修改或敏感 Shell 命令前需提交“执行规划”并等待人工签核。
- **记忆化沉淀** — 分层知识库（项目级/个人/全局）与自动记忆存储，模型越用越懂你的代码库。
- **检查点与时光倒流** — 引入代码修改快照系统，支持 `/rewind` 一键撤销，提供极致的容错安全网。

## 全场景接入

| 前端形态 | 启动命令 | 场景说明 |
|------|------|------|
| **终端 TUI** | `momapeer chat` | 极客首选：沉浸式终端界面（基于 Charm Bubble Tea） |
| **API 服务** | `momapeer serve` | 开放能力：提供标准 HTTP/SSE 编程接入接口 |
| **桌面客户端** | Wails 图标启动 | UI 交互：提供原生 macOS / Windows / Linux 体验 |
| **企业机器人** | `momapeer bot start` | 团队协作：企业微信 / 飞书等 IM 网关接入 |
| **ACP 服务** | `momapeer acp` | 协议桥接：Agent Control Protocol 远程控制层 |

## 安装指南

当前版本：**v0.1.0**

```sh
npm i -g momapeer                        # 任意系统——自动拉取对应平台的原生二进制
brew install zzycxz/momapeer/momapeer    # macOS 用户
```

您也可以在 [GitHub Releases](https://github.com/zzycxz/momapeer/releases) 获取预编译归档文件（支持 `darwin|linux|windows × amd64|arm64`）。

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
momapeer run --model moma/jiutian/jiutian-code-8b "补充单元测试"
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
| `moma/jiutian/jiutian-code-8b` | 极速响应，代码专精 | 代码片段补全、快速重构、单元测试生成 |
| `moma/jiutian/jiutian-lan-8b` | 成本与速度最优解 | 文档翻译、简单文本处理、快速指令路由 |

> 完整模型列表请登录 [九天平台控制台](https://jiutian.10086.cn/largemodel/llmstudio/#/modelHub) 查看。只需修改 `model` 字段即可无缝热切换，零代码侵入。

## 文档指引

| 参考文档 | 涵盖内容 |
|------|------|
| **[使用指南](./docs/GUIDE.zh-CN.md)** | 权限控制、沙盒运行、MCP 插件、终端斜杠命令、`@` 语法、双模型配置 |
| **[架构规格](./docs/SPEC.md)** | 工程契约：系统架构、Registry 机制、数据类型约束与长期路线图 |
| **[快照机制](./docs/CHECKPOINTS.md)** | 基于文件快照的代码修改安全网设计 |
| **[Session 架构](./docs/SESSION_REFERENCE_ARCHITECTURE.md)** | 会话生命周期管理、状态持久化与无缝恢复机制 |
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
