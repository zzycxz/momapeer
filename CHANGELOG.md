# Changelog

## [0.1.6] — 2026-06-15

### Security

- **Checkpoint 路径穿越防护加固**：`safePath` 从 `strings.HasPrefix` 前缀检查改为
  `filepath.Rel` + `filepath.IsLocal`，显式拒绝 `..`、UNC 路径等平台特定逃逸向量，
  尤其修复了 Windows 大小写不敏感文件系统上的潜在绕过。
- **权限系统多 subject 评估**：`Decide()` 改为调用 `DecideSubjects()`，支持
  `move_file` 等多端点工具同时检查 source 和 destination 路径。
  此前仅检查第一个匹配的 subject，destination 的 deny 规则会被静默绕过。
  新增 `Subjects()` 函数提取所有 subject（含 `source_path`、`destination_path`）。
- **`move_file` 归类为文件变更工具**：`IsFileMutationTool`、`isWriterTool`、`extractPaths`、
  `repeatSuccessSignature` 均补充 `move_file`，使其受权限规则约束并被证据系统追踪。

### Fixed

- **Summarizer 超时保护**：`summarize()` 新增 90 秒超时（`context.WithTimeout` +
  `select` on `ctx.Done()`）。此前 LLM 流式响应卡死时 compaction 会永久阻塞，
  整个 agent 无法恢复，用户只能杀进程。
- **Transient 401 重试**：`SendWithRetry` 新增 `SendOptions.RetryAuth` 机制，
  当 key 首次认证成功后（`authed` 状态追踪），遇到 401/403 最多重试 2 次。
  修复了九天平台等网关偶发 401 导致的虚假会话失败。
  `AuthError` 新增 `HasKey` 字段区分"无 key"和"有 key 但认证失败"。
- **Todo 状态重建错误检测**：`failedToolCallIDs` 替换为 `successfulToolCallIDs` +
  `toolResultFailed`，错误匹配从仅 `error:`/`blocked:` 扩展为同时覆盖 `Error:`/`[error`。
  此前大写 `Error:` 和方括号前缀的错误结果被误判为成功，导致会话恢复后 todo 状态不一致。
- **最终答案压缩**：`Run()` 在 `return nil` 前新增 `maybeCompact(ctx, usage)` 调用。
  此前最后一轮的大量工具输出不压缩直接带入下一轮，可能导致立即溢出。
- **Grep 超时**：新增 `timeout_seconds` 参数（默认 30 秒，最大 300 秒），
  超时后返回部分结果并提示调大参数或缩小搜索范围。修复了大型目录树上 grep 无限挂起的问题。
- **MCP stdio PATH 缓存**：`stdioShellPATH` 改为 `cachedShellPATH` 包装器
  （`sync.Once` memoize），`resolveStdioExecutable` 改为急切预置 shell PATH
  （`enrichStdioShellPATH`）。修复了从 GUI/Dock 启动时找不到 npx/uvx 等命令的问题，
  并消除了每次插件启动时重复探测 shell PATH 的开销。
- **Checkpoint List() 进行中路径泄露**：`List()` 对当前进行中 turn 的 paths 置 nil，
  防止未提交的快照路径参与 CanCode 传播。
- **Desktop 自动更新修复**：`matchPlatform()` 过滤了非 `-installer.exe` 的 Windows 文件，
  导致 `latest.json` 缺少 Windows/macOS 平台条目，旧版本无法检测更新。
  修复后 `latest.json` 包含全部 6 个平台。
- **版本号注入**：Wails 构建时未通过 `-ldflags` 注入版本号，app 显示 "dev" 且跳过更新检查。
  CI 现从 tag 自动提取版本号注入 `main.version`。
- **签名文件分离**：`.minisig` 签名文件从主 release 移至独立的 `-sigs` release，
  主 release 页面只显示用户需要的安装包。
- **Windows ARM64 构建修复**：PowerShell 对 `-ldflags` 引号解析有问题，改用 bash shell。
- **Bot 功能对所有版本可见**：移除 `isDevBuild` 限制，IM bot 设置不再隐藏。
- **品牌名称修正**：修复 en.ts 15 处 "China Mobile"、zh.ts 9 处产品名混淆、
  bridge.ts/App.tsx/sessionExport.tsx 中的上游遗留品牌名。
- **系统提示词身份约束**：添加身份定义，防止模型自称 Claude/Qwen/DeepSeek。
- **欢迎页图片**：修复 `welcome-hero.jpg.png` 双重后缀导致的构建失败，
  替换为透明背景 PNG。
- **推理协议提示修正**：修复 en.ts/zh.ts 中 "moma uses moma reasoning fields" 同义反复。

### Added

- **PermissionRequest hook 事件**：新增 `PermissionRequest` hook 事件类型，
  支持在权限审批时触发外部策略引擎。`Payload` 新增 `Subject` 字段。
- **RenameSession API**：`branch.go` 新增 `RenameSession(sessionPath, title)` 方法，
  支持通过代码重命名会话分支。
- **货币符号标准化**：`Pricing.Symbol()` 新增 `currencySymbol()` 转换，
  `currency = "USD"` 显示为 `$`，`"EUR"` 显示为 `€`，而非原始字符串。

### Changed

- **CHANGELOG 分类重组**：按 MoMA 平台适配、MCP 与工具、基础设施三个类别组织条目。
- **CONTRIBUTING.md / RELEASING.md**：分支引用从 `main-v2` 更新为 `main`。
- **CI：桌面端 release 标记为 Latest**：添加 `make_latest: true`，
  确保 updater 的 `/releases/latest/` 指向桌面端而非 CLI。

[0.1.6]: https://github.com/zzycxz/momapeer/releases/tag/desktop-v0.1.6

## [0.1.5] — 2026-06-15

### Added

- **Time MCP server**：新增内置时间查询 MCP server（纯 Go 实现，零外部依赖），
  提供 `get_current_time` 和 `convert_time` 两个工具，支持 IANA 时区查询与转换。
  默认启用，通过 `momapeer builtin-mcp time` 子命令以 stdio 方式运行。
- **Built-in MCP toggle 支持**：Desktop MCP 面板现在正确展示所有 built-in MCP server
  （time、Context7），并支持通过 toggle 开关启用/禁用，变更同步持久化到用户配置。

### Fixed

- **计费缺失修复**：MoMA 不返回 `prompt_cache_hit/miss_tokens` 时，prompt token
  成本从 0 恢复为按全价 (`Input`) 计费，修复了 MoMA 场景下会话费用始终为 0 的 bug。
- **CLI usage line 误导修复**：MoMA 下不再显示无意义的 `(0 cached / N new)`，
  当 provider 不报告 cache split 时隐藏该列。
- **Desktop StatusBar 误导修复**：MoMA 下 cache hit rate 从错误的 `0.00%` 改为正确的 `-`。
- **grep 工具 goroutine 泄漏修复**：非 UTF-8 文件 grep 在达到匹配上限时，
  `io.Pipe` 读端未关闭导致 writer goroutine 永久阻塞，现已加 `defer pr.Close()`。
- **Context7 MCP 前端不显示修复**：`Capabilities()` 方法缺少 built-in MCP entries
  遍历逻辑，导致 Context7 即使在配置中启用也不会出现在 Desktop MCP 面板中。
  现对齐 DeepSeek-Reasonix 实现，遍历 `builtinmcp.Entries()` 展示所有 built-in server。

### Changed

- **Dead code 清理**：删除未使用的 `internal/inspect/` 包（CLI 和 desktop 各有独立的
  能力投影逻辑，该包从未被引用）和未使用的前端组件 `InlineDiff.tsx`。
- **Cache token 报告降级为 optional**：移除 "MoMA 一定会返回 cache token" 的假设，
  prefix 稳定性架构保留（减少 token 传输、为未来 cache 做准备）。
- **计费公式统一**：`run_metrics.go` 内联公式改为调用 `p.Cost(u)`，消除重复代码。
- **Cache e2e 测试降级**：`cachehit_e2e_test.go` 和 `realcache_test.go` 改为 opt-in，
  MoMA 下自动 skip。
- **配置文件修正**：`momapeer.example.toml` 中 MoMA 的 `cache_hit` 从 `0.02` 改为 `0`。
- **前端 mock 数据对齐**：browser dev mock 的 cache token 改为 0，贴近 MoMA 现实。
- **前端 locale 文案更新**：cache 相关 tooltip 明确标注 "Provider 返回时" / "when reported by the provider"。
- **源码注释更新**：~20 个文件的 cache 相关注释补充 MoMA 说明，便于后续维护者理解设计意图。

## [0.1.0] — 2026-06-14

**momapeer 初始化版本** — 对中国移动 MoMA（九天）聚合模型平台进行适配，启动二次开发。

momapeer 最初是个人研究与学习项目，探索 Go 语言 AI agent 的工程实践。如果它能帮助到其他开发者的工作与学习，我们将深感荣幸。

### MoMA 平台适配

- **MoMA provider preset**: 新增中国移动九天 MoMA 聚合模型平台作为默认 provider，
  支持 DeepSeek、Qwen、GLM 等 300+ 模型一键接入。
- **Configuration branding**: 配置文件迁移至 `momapeer.toml`，
  环境变量前缀统一为 `MOMAPEER_*`。
- **Multi-model adaptation**: 适配 MoMA 平台多模型体系，支持 reasoning（-Pro /
  -Pro）与 fast（-Flash / -Flash）双档位自动切换；统一了不同模型
  的 `thinking_mode` / `reasoning_content` 解析逻辑，确保 reasoning token 正确提取与展示。
- **Token calculation & billing**: 新增 MoMA 平台 token 计费适配（`internal/billing/`），
  按 MoMA 各模型实际定价计算 input / output / cache_hit token 费用，
  支持 `¥` / `$` 双币种展示；在 agent loop 中实时追踪用量并在 turn 结束时汇总。
- **Example config**: 提供 `momapeer.example.toml` 覆盖 MoMA 等多 provider 配置示例，含定价与 reasoning 参数。

### MCP 与工具

- **Search API with chain fallback**: 新增搜索 API 及前端输入界面，支持链式降级策略：
  Brave → Exa → Linkup，任一搜索引擎不可用时自动切换至下一个，确保搜索结果可达。
- **Image recognition**: 新增图片识别能力，上传图片自动转换为 base64 编码并传递给大模型进行理解与分析。
- **CodeGraph**: 将内置代码智能检索（基于 tree-sitter）模块 CodeGraph 提升至 1.0.0 版本。
- **LSP client**: 新增语言服务器协议客户端（`internal/lsp/`），
  集成诊断、跳转定义与引用查找。

### 基础设施

- **Bot gateway**: 新增 QQ / 飞书 / 微信多通道 IM bot 基础设施（`internal/bot/`），
  支持白名单、消息合并去抖和沙盒执行。
- **ACP server**: 新增 Agent Control Protocol 服务端（`internal/acp/`），
  提供结构化的机器间交互协议。
- **i18n**: 新增中英文双语 UI（`internal/i18n/`），支持 `$LANG` 自动检测。

[0.1.5]: https://github.com/zzycxz/momapeer/releases/tag/desktop-v0.1.5
[0.1.0]: https://github.com/zzycxz/momapeer/releases/tag/v0.1.0
