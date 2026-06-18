# Changelog

## [0.1.8] — 2026-06-18

### Fixed

- **Dream / Distill 周期判定失效修复**：原 `dream.go` 通过扫描 `sessions/*.jsonl.meta`
  匹配 `"Auto Dream"` / `"Auto Distill"` topicTitle 来判定上次运行时间，但 Dream/Distill
  的子 agent 复用父 session（`New(...).Run(...)`），既不分配独立 sessionPath 也不落盘，
  因此匹配键**从未被写入**，`findLastSessionTime` 永远返回零值。后果：自动触发退化成
  "首次按项目年龄触发一次后即失效"，周期（7 天 / 30 天）实际从未生效。改为 dream.go
  自维护独立的 `dream_state.json`（位于 `.momapeer/` 下）记录每次运行的时间/状态/触发方式，
  周期判定与「上次运行」展示统一基于该文件。新增 `dream_test.go` 覆盖状态读写往返、
  每类历史记录上限、冷启动年龄门槛。

- **九天多模态开关点击无效 + 不持久化修复**：`SetJiutianTool`（`desktop/app.go`）原自行
  `config.Load()`+`WriteFile`，与 `Settings()` 读取链路不同源，前端 toggle 成功路径回读后把
  开关弹回原位（表现为"点不动"）。改为走 `applyConfigChange`（与 `SetSkillEnabled` 一致），
  读写同源。同时发现 `config/render.go` 的 `RenderTOMLForScope`（手写每一段）**完全缺少
  `[jiutian]` 段渲染**，导致开关写入被静默丢弃、重启后回默认——新增 `[jiutian]` 段
  （`image_understand`/`image_generate`/`video_understand`），写盘往返已验证。
- **拖入图片后历史消息满屏 base64 修复**：多模态消息 `provider.Message.Content` 为 `any`
  （`string` 或 `[]ContentPart`），但 `Message` 无 `UnmarshalJSON`，`LoadSession` 反序列化后
  `Content` 退化为 `[]interface{}`，`ContentString` 落入 `default` 分支 `fmt.Sprintf("%v", ...)`
  把整串 `data:image/png;base64,...` dump 成文本。新增 `(*Message).UnmarshalJSON`
  （`internal/provider/provider.go`）按 JSON 形态还原 `string` / `[]ContentPart`，下游
  `ContentString` / `buildRequest` / `imageContentParts` 自动恢复正确类型。
- **图片生成链接 401 修复**：`image_generate` 工具原把九天 `/fs/getFile?key=...` 裸链接返回给
  模型，而该端点必须带 `Authorization: Bearer` 才能访问（裸链接 401）。改为工具内用
  `jiutianDownloadFile`（`jiutian_api.go`，带 Bearer 下载）取回图片字节，`saveImageAttachment`
  （`jiutian_multimodal.go`）写入 `.momapeer/attachments/`，返回本地 `![](...)` markdown 路径；
  下载失败回退原链接 + 提示。顺带消除 `tool/builtin → control → agent` 循环依赖
  （builtin 自带存图函数，不再 import control，agent 测试包恢复构建）。

### Added

- **Dream / Distill 可见化与可配置**：此前两个后台自进化智能体（Dream 记忆整合、
  Distill 工作流提炼）已接入主循环但用户完全无感知、且周期硬编码强制开启。本次新增
  完整的配置与可见化链路：
  - **`[dream]` 配置节**：`config.go` 新增 `DreamConfig{ Enabled, DreamInterval, DistillInterval }`，
    支持总开关与自定义周期（默认 `true / 7 / 30` 天）。`render.go` 渲染、`edit.go` 提供
    `SetDreamEnabled` / `SetDreamIntervals`（校验 ≥1）setter，配置写回往返已验证。
  - **live load 配置读取**：`maybeDreamDistill` 每个 turn 内 `config.Load()` 读取最新配置，
    用户在设置面板改周期后当前会话即时生效，无需重开标签页。`SpawnDream` / `SpawnDistill`
    签名保持不变，`Options` 未新增字段，现有控制器测试零改动。
  - **SpawnCoordinator 并发控制**：用带 `sync.Mutex` 的结构替换原先无锁的包级
    `lastDreamSpawn` / `lastDistillSpawn` 全局变量。`inFlight`（每类一个，防手动+自动+多标签页
    并发 data race）与 `lastAuto`（自动路径防抖）职责分离；周期判定永远只看磁盘，手动触发
    不污染自动周期调度。
  - **手动触发 API**：新增 `RunDreamOnce` / `RunDistillOnce`（跳过周期判定、尊重总开关、
    返回 error/超时给前端反馈），`controller.go` 暴露 `TriggerDream` / `TriggerDistill` /
    `LastDreamRun` / `DreamInFlight`。
  - **desktop RPC**：`app.go` 新增 `DreamStatus`（返回开关/周期/上次运行/是否在跑/历史列表）、
    `SetDreamEnabled` / `SetDreamIntervals`（走 `applyConfigChange` 同源写路径，避免开关回弹）、
    `TriggerDream` / `TriggerDistill`。历史复用 `dream_state.json`，不新建扫描逻辑。
  - **Memory 设置页「自进化」区块**：`MemoryPanel.tsx` 新增 `SelfEvolutionSection`，置于
    Memory 页顶部。包含总开关、Dream / Distill 两张配置卡（周期数字输入 + 「立即运行」按钮）、
    「上次运行：X 天前 / 从未运行」静态状态展示、以及最近运行记录列表（成功 / 超时 / 失败）。
    交互采用乐观更新（仿 `JiutianSection`），失败回滚。`bridge.ts` / `types.ts` 补全
    `DreamStatusView` / `DreamRunView` 类型与 mock；`zh.ts` / `en.ts` 补全 `dream.*` 文案。

- **生成的图片直接显示在工具卡下方**：`image_generate` 产出的图片此前只随工具结果文本进
  `RoleTool` 消息，用户能否看到取决于模型是否复述路径——实测模型常无视本地路径、自编旧的
  九天 401 链接。改为把图片作为结构化附件下发，绕开模型复述：`agent.go` 的 `executeOne` 用
  正则 `extractImageAttachments` 从结果文本提取 `.momapeer/attachments/` 图片路径，填入
  `toolOutcome.attachments`，随 `event.ToolResult` 下发；`event.Tool`/`wireTool`
  （`event.go` + `desktop/wire.go` + `serve/wire.go`）新增 `Attachments` 字段；前端
  `useController.ts`/`types.ts` 的 tool item 加 `attachments`，`ToolCard.tsx` 新增
  `ToolAttachments` 子组件（复用 `AttachmentDataURL` 转 data URL 渲染 `<img>`）。
  配套可见性修复：`image_generate` 的 `ReadOnly()` 改 `false`（产出图片非背景只读操作，
  不再被 `ReadOnlyBatch` 折叠隐藏）；`Transcript.tsx` 的 readOnlyBatch / TurnCollapse
  判断排除有附件的工具，含附件的 turn 默认展开——图片生成后常驻工具卡下方可见。
- **九天多模态能力卡片 UI 对齐 + 图标更新**：`JiutianSection`（`SettingsPanel.tsx`）此前的
  DOM 是简化拼装版，与上方技能卡（`SkillRow`）共用 `.cap-skill-card` CSS 但结构不一致
  （缺 `cap-skill-card__toggle` 包裹、switch 无 `Tooltip`），导致样式差异 + 开关点击区被挤压。
  对齐 `SkillRow` 骨架；左侧图标字符「九」改为「/」，与技能卡一致。
- **skill「关闭但可见」**：skill 禁用语义从"完全隐藏"改为"关闭但可见"。此前关闭的 skill
  既不进系统提示索引、LLM 也不知其存在。现在关闭的 skill 仍出现在 skills 索引里并标
  `[关闭]`（`skill.Skill` 加 `Disabled` 字段；`boot.go` 索引数据源改用 `allSkillStore.List()`
  + `cfg.IsSkillDisabled` 标记；`index.go` 的 `indexLine` 加 `[关闭]` tag，`indexHeader`
  增加"关闭项不可调用、匹配任务时提示用户去 设置 → Skills 开启"的说明）。"不可调用"语义
  不变（`skillStore.List/Read` 仍剔除禁用项，`run_skill`/子代理工具绑定 `skillStore`）。
  效果：用户可放心关闭不常用 skill 控制上下文，LLM 仍保有全局能力视野并按需提示开启。
  默认状态保持开启，不破坏现有行为。

## [0.1.7] — 2026-06-17

### Security

- **awk/sed 需要用户审批**：从 `bash_readonly.go` 的只读命令列表中移除 `awk` 和 `sed`。
  这两个命令可通过 `system()` 执行任意 shell 代码（如 `awk '{system("rm -rf /")}' file`），
  不应被自动放行。与 Reasonix v1.9.0 安全加固对齐。

### Added

- **Edit 4 级模糊匹配**：`edit_file` 和 `multi_edit` 工具从纯精确匹配升级为 4 级降级策略：
  Level 1 精确子串 → Level 2 行 trim 匹配 → Level 3 缩进归一化匹配 → Level 4 首尾行锚定匹配。
  新增 `edit_fuzzy.go`（`fuzzyMatch` / `lineTrimMatch` / `indentNormMatch` / `blockAnchorMatch`）。
  解决了 LLM 生成的 `old_string` 因缩进、空白、换行符差异导致编辑频繁失败的问题。
  每个级别匹配后映射回原始内容位置，逐文件 Semaphore 防竞态。
- **`apply_patch` 多文件 Patch 工具**：新增 `apply_patch.go`，支持自定义 patch 格式
  （`*** Begin Patch` / `*** End Patch`），一次调用完成多个文件的 Add / Update / Delete 操作。
  两阶段提交：先验证所有 hunk（路径安全 + 文件存在性 + 内容匹配），再统一应用。
  Update 操作支持 `@@` 上下文锚定，精确匹配 → trim 匹配双通道。
- **System Prompt 模型路由**：`instruction.go` 新增 `ForModel(modelID)` 函数，
  根据 `MoMAThinkingModels` 白名单自动为推理模型注入深度思考指令（`ThinkingAddon`）。
  在 `boot.go` 的 system prompt 组装链中接入，位于 base prompt 之后、language policy 之前。
  为未来按模型能力分组（串行约束、推理增强等）预留扩展点。
- **Dream / Distill 执行闭环**：`dream.go` 新增 `SpawnDream` / `SpawnDistill` 函数，
  将原本死代码的 `ShouldAutoDream` / `ShouldAutoDistill` 布尔判断接入实际的子 agent 启动逻辑。
  在 `controller.go` 的 `runTurnWithRawDisplay` 入口处新增 `maybeDreamDistill` 触发器，
  每个 primary session turn 开始时检查是否需要自动触发记忆整合（7 天）或工作流提炼（30 天）。
- **Prune 软剪枝**：`prune.go` 新增 `SoftTrimLargeResults` 方法，
  对 prune 区域中超过 4KB 的工具输出做渐进式修剪（保留首尾各 1.5KB，中间替换为截断标记）。
  作为硬剪枝（全量替换为 `[elided]`）之前的独立阶段，保留输出中最关键的部分
  （顶部的命令/配置、底部的结果/错误）。
- **FTS5 查询端 CJK 支持**：`fts.go` 新增 `isCJK` 函数覆盖 CJK 统一表意文字、
  扩展 A 区、兼容区、日语假名、韩文。`tokenize` 改为 CJK 字符逐字拆分
  （与 FTS5 `unicode61` 索引端行为一致），`isTokenChar` 改用 `unicode.IsLetter` 覆盖全 Unicode。
- **九天多模态工具**：新增 3 个调用九天平台专属 API 的内置工具：
  - `image_understand`（`/v3/image/text`）：图片理解，支持 base64 和文件路径输入，
    使用 `LLMImage2Text` 专用视觉模型。
  - `image_generate`（`/v3/images/generations`）：文生图 / 图生图，支持 prompt 扩写、
    多张生成（1-4 张）、参考图输入。返回下载 URL。
  - `video_understand`（`/v3/video/text`）：视频理解，需先通过文件上传 API 获取视频路径。
  新增 `jiutian_multimodal.go` + `jiutian_multimodal_test.go`（5 个单元测试 + 2 个集成测试）。
- **图片自动识别降级**：当用户使用非 vision 模型粘贴图片时，自动调用九天 `image_understand`
  API 获取图片文字描述，替换 `image_url` content part 为 `[Image content: ...]` 文本。
  实现所有 18 个 MoMA 模型的图片理解能力（vision 模型走 chat API 直接看图，非 vision 模型走
  独立 API 先识别再分析）。新增 `jiutianImageUnderstand()` 函数。
- **MoMAVisionModels 多模态白名单**：新增 18 模型 × 图片输入的 API 兼容性测试，
  确认 4 个模型支持原生 vision（qwen3.5-397b、qwen3.6-35b、qwen3.6-27b、kimi-k2.6）。
  白名单从 2 个更新为 4 个（移除不支持的 gpt-oss-120b，新增确认支持的 3 个）。
- **MCP 服务器调用超时**：`transport_stdio.go` 的 `call()` 方法新增默认 60 秒超时，
  防止慢/卡死的 MCP server 无限阻塞 agent。通过 `[[plugins]]` 的 `call_timeout` 字段可配置
  （Go duration 格式，如 `"30s"`、`"2m"`、`"0"` 禁用）。Spec 结构体新增 `CallTimeout` 字段。
- **Per-model vision 能力注册表**：新增 `MoMAVisionModels` 白名单（与 `MoMAThinkingModels` 同模式），
  模型级别判断是否支持图片输入。`ModelSupportsVision(model, configOverride)` 同时检查注册表和
  config 覆盖。非 vision 模型收到图片时返回明确错误信息并列出支持的模型，不再静默剥离。

### Fixed

- **Max Mode 类型修复**：`max_mode.go` 的 `describeToolsForJudge` 参数从 `[]provider.ToolCall`
  （工具调用记录，含 Arguments）修正为 `[]provider.ToolSchema`（工具定义，含 Description + Parameters）。
  此前 Judge 看到的是工具调用历史而非工具定义，语义错误。
- **MoMA effort 级别修复**：经 18 模型 × 5 级别的完整 API 测试验证，MoMA 平台仅 `medium` 和 `high`
  两个级别 18/18 全部通过。`low` 被 2 个模型拒绝（kimi-k2.6、jiutian-lan-236b），
  `xhigh`/`max` 被 16 个模型拒绝。新增自动降级：`low`→`medium`，`xhigh`/`max`→`high`。
  新增 `tests/effort_level_test.go` 集成测试。
- **Desktop 模型选择器光标**：`.modelsw__item` 补充 `cursor: pointer`，
  修复 WebView2 下按钮默认显示为普通箭头而非手型光标的问题。
- **Desktop 模型选择器子菜单稳定性**：级联子菜单的 `mouseLeave` 关闭增加 150ms 延迟，
  并通过 `::after` 伪元素桥接分类行与子菜单之间的 4px 间隙，
  修复鼠标从分类行移到子菜单时因经过间隙导致子菜单突然消失的问题。

### Changed

- **MoMA effort 字段从 `reasoning_effort` 改为 `thinking_effort`**：MoMA 平台使用 `thinking_effort`
  而非 OpenAI 标准的 `reasoning_effort`。`chatRequest` 新增 `ThinkingEffort` 字段，
  MoMA 模型使用 `thinking_effort`，非 MoMA 模型继续使用 `reasoning_effort`。
- **MoMA effort 级别升降级策略**：`/effort` 命令的可用级别从 `auto|high|max` 改为 `auto|low|medium|high`。
  基于 18 个模型 × 5 个级别的完整 API 兼容性测试做出的映射决策：
  `low` → `medium`（升级，2/18 模型不支持 low），`xhigh`/`max` → `high`（降级，16/18 模型不支持）。
  `medium` 和 `high` 是唯一 18/18 全部通过的级别。用户输入 `max` 不会报错，静默降级为 `high` 正常执行。
  新增 `tests/effort_level_test.go` 集成测试覆盖全部 18 个 MoMA 模型。
- **`MoMAReasoningModels` 重命名为 `MoMAThinkingModels`**：控制请求端 `thinking` 字段的白名单
  从 `MoMAReasoningModels` 更名为 `MoMAThinkingModels`，明确区分于响应端的 `reasoning_content` 字段。
  同步更新 `openai.go`、`effort.go`、`effort_test.go` 中的所有引用及注释。
- **Edit 工具描述更新**：`edit_file` 的 description 从 "Replace an exact string"
  更新为 "Uses fuzzy matching: exact match first, then tries line-trim, indent-normalize, and block-anchor matching"。
- **Desktop 版本号更新**：`wails.json` 的 `productVersion` 从 `0.1.5` 更新为 `0.1.7`。
- **冗余代码合并**（5 项）：
  - `extractJSON()` 提取到 `internal/agent/util.go`，消除 goal_judge.go / max_mode.go 重复。
  - `jiutianAPICall` 提取到 `internal/jiutian/api.go` 共享包，消除 openai.go / jiutian_api.go 重复。
  - `netclient.ProxyURLFor()` 提取到 netclient 包，消除 websearch.go / webfetch.go 重复。
  - `netclient.BlockedFetchIP()` + `CGNATRange` 提取到 netclient 包，消除 webfetch.go / ssrf.go 重复。
  - `jiutianClient` 包级别变量消除 jiutian_api.go 内两个相同 HTTP client 实例。

### 测试

- **18 模型 × 5 级别 effort 兼容性测试**（`tests/effort_level_test.go`）：覆盖全部 BuiltinMoMAModels，
  验证 low/medium/high/xhigh/max 在每个模型上的实际表现。
- **18 模型 × 图片输入 vision 兼容性测试**（`tests/vision_probe_test.go`）：探测哪些模型支持
  `image_url` content part，确认 4 个支持、8 个拒绝、6 个超时。
- **九天多模态工具集成测试**（`tests/jiutian_multimodal_test.go`）：验证 image_understand
  正确识别图片内容、image_generate 正确生成图片。

## [0.1.6] — 2026-06-15

### Security

- **Checkpoint 路径穿越防护加固**：`safePath` 从 `strings.HasPrefix` 前缀检查改为
  `filepath.Rel` + `filepath.IsLocal`，显式拒绝 `..`、UNC 路径等平台特定逃逸向量，
  尤其修复了 Windows 大小写不敏感文件系统上的潜在绕过。
- **Memory store 路径穿越防护**：新增 `safeJoin(base, name)` 函数，应用于 `Save`、
  `Path`、`Delete` 方法，防止通过 `remember` 工具的 name 参数注入 `../../` 等路径穿越。
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
- **FTS5 Upsert 重复行**：`ON CONFLICT` 在 FTS5 虚拟表上不生效（无 UNIQUE 约束），
  改为 DELETE + INSERT 模式，避免搜索结果重复。
- **Goal Judge 超时**：独立 judge 调用增加 60 秒超时保护，避免无响应 provider 导致永久阻塞。
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

- **Memory Archive 软删除**：`Delete()` 改为调用 `Archive()`，记忆文件移至 `.archive/`
  目录并附加时间戳，而非永久删除。新增 `ListArchived()` 方法浏览归档历史。
  用户误删记忆可追溯恢复。
- **GlobalDir 跨项目记忆**：`Store` 新增 `GlobalDir` 字段，`user` 和 `feedback` 类型的记忆
  路由到全局目录（`~/.config/momapeer/memory/global`），在所有项目间共享。
  用户偏好和反馈指导不再因切换项目而丢失。
- **Memory FTS5 全文检索**：新增 SQLite FTS5 索引（`internal/memory/fts.go`），
  支持 BM25 排序搜索。`SearchService` 提供 `Search(query)` 接口，自动懒调和磁盘文件。
  记忆从全量注入系统提示改为按需检索，token 开销随记忆数量线性增长而非全量注入。
- **Goal 独立 Judge**：新增 `GoalJudge` 函数，当模型报告 `[goal:complete]` 时调用独立
  LLM 模型评估目标是否真正达成（基于 transcript 证据，temperature=0）。
  通过 `Options.GoalJudge` 配置注入，默认关闭（向后兼容）。防止代理乐观停止。
- **Max Mode（Best-of-N）**：新增 `RunMaxStep` 函数，运行 N 个并行 propose-only 候选，
  独立 judge 模型选择最佳候选，胜者的工具调用返回给调用方实际执行。
  适用于复杂推理任务（架构设计、疑难 bug），可显著提升推理质量。
- **Dream / Distill 后台智能体**：新增记忆整合（Dream，7 天周期）和工作流提炼（Distill，
  30 天周期）两个后台智能体任务，将会话中的持久知识提取到项目记忆、将重复工作流沉淀为技能文件。
  通过 `ShouldAutoDream` / `ShouldAutoDistill` 检查是否触发。
- **ComposeSynthetic**：新增 `Controller.ComposeSynthetic(text)` 方法，为控制器注入的合成消息
  （如 plan 审批后的执行指令）提供独立的组装路径，避免重复注入 plan mode 标记和 memory notes。
- **PlanModeFromContext**：`callContext` 新增 `planMode` 字段，新增 `PlanModeFromContext(ctx)`
  导出函数。工具可自查是否在 plan mode 下运行，条件性禁用写入相关界面。
- **InheritLifecycleFrom**：新增 `Controller.InheritLifecycleFrom(prev)` 方法，
  模型切换时保留 `startedOnce` 和 `turn` 计数，防止 SessionStart hook 重复触发。
- **BeginDestroySession**：新增两阶段会话销毁机制（`BeginDestroySession` +
  `SessionDestroyHandle`），分离后台任务取消和资源释放，避免孤儿进程。
- **PermissionRequest hook 事件**：新增 `PermissionRequest` hook 事件类型，
  支持在权限审批时触发外部策略引擎。`Payload` 新增 `Subject` 字段。
- **RenameSession API**：`branch.go` 新增 `RenameSession(sessionPath, title)` 方法，
  支持通过代码重命名会话分支。
- **货币符号标准化**：`Pricing.Symbol()` 新增 `currencySymbol()` 转换，
  `currency = "USD"` 显示为 `$`，`"EUR"` 显示为 `€`，而非原始字符串。

### Changed

- **模型列表精简**：`BuiltinMoMAModels` 从 32 个精简至 19 个，移除小模型
  （jiutian-lan-13b/8b、math-8b、code-8b、qwen3.5-4b）、老版本
  （deepseek-v3/r1、qwen3-235b、minimax-latest、qwen3-next-80b、qwen3.5-27b/9b）
  和非主流模型（nvidia/nemotron-3-super）。
- **前端模型分类重组**：ModelSwitcher 新增 DeepSeek、月之暗面独立分类，
  MoMA auto-router 移入"其他"。
- **CHANGELOG 分类重组**：按 MoMA 平台适配、MCP 与工具、基础设施三个类别组织条目。
- **CONTRIBUTING.md / RELEASING.md**：分支引用从 `main-v2` 更新为 `main`。
- **CI：桌面端 release 标记为 Latest**：添加 `make_latest: true`，
  确保 updater 的 `/releases/latest/` 指向桌面端而非 CLI。
- **Provider.Content 类型统一**：新增 `modernc.org/sqlite` 依赖（纯 Go，无 CGo）
  用于 Memory FTS5 索引。

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
