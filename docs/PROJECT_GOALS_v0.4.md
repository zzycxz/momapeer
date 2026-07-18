# momapeer 项目目标文档（PRD）

> **版本**：v0.3.10 → 目标 v0.4.0
> **日期**：2026-07-04
> **配套文档**：`CODING_BASELINE_2026-07-04.md`（现状核查，逐行源码验证）
> **写法**：结果导向——讲清"做到什么程度、用户能感受到什么、怎么验收"，不写实现细节

---

## 一、项目定位（不变）

momapeer 是中国移动九天（MoMA）生态的**企业级全场景 AI 助手**，具备双重能力：

- **编码智能体**：自主完成"理解需求 → 规划 → 实现 → 验证 → 交付"全流程
- **办公自动化**：浏览器/桌面控制、邮件、文档、PPT、日程、知识库

载体特性（必须保持）：
- Go 单二进制，零运行时依赖，6 平台交叉编译
- 多前端：TUI / 桌面（Wails）/ HTTP/SSE / IM 机器人 / ACP
- 九天平台深度对接（thinking mode、reasoning_content、300+ 模型）
- **差异化优势（不能丢）**：codegraph 符号图、evidence 证据门、checkpoint rewind、办公自动化双能力

---

## 二、当前痛点（来自核查报告，逐行源码验证）

### 用户能直接感受到的问题

1. **编码任务"做完一半就停"或"声称做完但没验证"**——没有执行后验证/重试环，CHANGELOG 自认"无验证重试，编码质量核心瓶颈"
2. **规划阶段探索能力弱**——plan mode 下 explore/research subagent 全被拦（H2），bash 也不能用（M4），模型只能靠 read_file/grep 硬探索
3. **复杂任务无法端到端完成**——没有 compose 工作流（spec→实现→验证→评审→合并），MiMo-Code 有 7 阶段编排，momapeer 完全缺失
4. **serve 用户切模型后审批失效**（H1）——浏览器看不到工具审批请求
5. **auto-plan 触发后 TUI 状态错乱**（H4）——模式标签不显示 Plan，用户困惑
6. **多 tab 状态串扰**（H5/H6）——关 tab 残留状态、plan revision 跨 tab 发错

### 维护者能感受到的问题

7. **文档系统性撒谎**——README/GUIDE/SPEC 仍宣传已删的"双模型协作引擎"，新贡献者照 GUIDE 配 planner_model 得到未知字段被静默忽略
8. **死代码堆积**（11 处）——PlanModeFromContext、FilterReadOnlyRegistry 等从未被调用，干扰阅读
9. **跨包重复定义**——exit_plan_mode 在 control/cli 各一份，无编译期保证
10. **prompt 指令失效**——PlanModeMarker 把已删工具（ls/glob）列为"available"，模型照指令撞墙

---

## 三、目标全景（v0.4.0）

### 三条主线

```
主线 A：编码能力升级（对标 MiMo-Code， mommypeer 自己的 Go 实现）
  └─ 让 momapeer 能自主完成"需求→交付"全流程，带验证和重试

主线 B：地基修复（真 bug + 死代码 + 文档债）
  └─ 让现有功能正确、文档诚实、代码可读

主线 C：cowork 继承编码升级（办公能力复用编码底盘）
  └─ 编码底盘升级后，办公自动化的探索/执行/验证自动受益
```

**优先级**：B（清理）→ A（编码升级）→ C（cowork 继承）。B 是 A 的前置（地基不稳不能盖楼），A 是 C 的前置（编码底盘是 cowork 的脊柱）。

---

## 四、主线 B：地基修复（第 1-2 周）

### B1. 真 bug 修复

**目标**：用户在任意前端、任意操作下，状态正确、无串扰。

| 编号 | 用户感受 | 验收标准 |
|------|---------|---------|
| H1 | serve 切模型后仍能正常审批工具 | serve 下 `/model x` 后，写工具触发 approval_request 事件，浏览器能看到并批准 |
| H4 | TUI 模式标签始终与实际一致 | auto-plan 触发后 TUI 立即显示 "Plan" 标签和气泡前缀；Shift+Tab 切换无残留 |
| H5 | 关闭 tab 不残留状态 | 关 tab 后立刻查该 tab 的 collaborationMode/goal/toolApproval，均为空（不靠 refreshTabMetas 兜底） |
| H6 | plan revision 不跨 tab 串扰 | 在 tab A 草拟 revision，切到 tab B，B 不会收到 A 的 revision |
| M1 | goal 草稿态不发到后端 | goal 为空时 collaborationMode 发 "normal" 而非 "goal" |
| R6 | plan 批准后执行轮有 reasoning language | ComposeSynthetic 路径补 reasoning language（或删除 no-op 改走 Compose） |

### B2. 死代码清理

**目标**：代码库无"定义了没人调用"的代码，降低阅读干扰。

**验收**：D1-D11（11 处）全部删除或降级。删后 `go build` 通过、`go test` 通过、前端 `tsc` 通过。具体：
- PlanModeFromContext 死链（agent.go 5 处连删）
- FilterReadOnlyRegistry（task.go）
- PlanTodosJSON 降级未导出
- acp.go 三函数
- rememberRule、setControllerMode 死链、SetFastTaskModel 重复声明等

### B3. 文档诚实化

**目标**：文档不再宣传已删功能，新用户照文档操作不会踩坑。

**验收**：
- README/README_en 的"双模型协作引擎"卖点删除或改为 fast_task_model（dream/distill）说明
- GUIDE/GUIDE.zh-CN 的 planner_model 配置教程整节删除或重写
- SPEC §3.5 Coordinator 整节删除或重写
- CONTRIBUTING 的 `feat(glob):` 改为现存工具
- serve/index.html 的 multi_edit 等已删工具引用清理
- 全仓 grep `planner_model` 在文档中零命中（CHANGELOG 历史记录除外）

### B4. 注释/常量归一

**目标**：跨包共享的常量有单一真源，过时注释清除。

**验收**：
- exit_plan_mode 提为导出常量 `control.PlanApprovalTool`，cli 和前端引用它（消除 R1/R3 跨包重复）
- boot.go:4/123、cli.go:732 的 "two-model Coordinator"/"planner_model" 注释修正
- boot.go:1030 "3B-ACT MoE" 注释改为 qwen3.6-35b

---

## 五、主线 A：编码能力升级（第 3-6 周）

### A1. 规划阶段探索能力修复（高优先，解锁后续）

**当前痛点**：plan mode 下 explore/research subagent 全被拦（H2），bash 全拦（M4），PlanModeMarker 列已删工具（H3）。

**目标**：plan mode 下模型能高效探索代码库，不撞墙。

**验收**：
- PlanModeMarker 的工具清单只列**当前真实存在且 plan mode 可用**的工具（read_file/grep/codegraph/lsp_definition/lsp_references 等），不再出现 ls/glob
- 探索类 subagent（explore/research）在 plan mode 下可用（它们本身就是只读调查）——通过让它们的 ReadOnly 反映"子代理实际是否只读"，或提供 plan-mode-safe 的探索入口
- 用户在 plan mode 让 agent "调研这块代码怎么改"，agent 能用 explore subagent 深入探索（保护主 context），而不是只能逐个 read_file

### A2. 编码 prompt 纪律（低风险高收益）

**当前痛点**：DefaultSystemPrompt（config.go:1270）对编码几乎无指导，没有编辑原则、没有 reversibility 框架、没有工具分层。

**目标**：系统提示词让模型编码时有纪律——编辑前先读、用 edit 不用 write 重写、改完跑测试、reversibility 判断。

**验收**：
- DefaultSystemPrompt 包含：编辑原则（不过度抽象、不加防御性错误处理）、reversibility/blast-radius 决策框架、工具分层指导（专用工具优先于 bash cat/sed）、edit vs write 选择指南
- 同等编码任务下，模型产生"声称完成但没验证"的比例下降（可用 benchmark 度量）

### A3. edit/write 后自动 LSP 诊断回灌

**当前痛点**：edit/write 后不跑 LSP 诊断，模型不知道有没有类型错误，要等用户跑 typecheck。

**目标**：模型每次 edit/write 后立即看到 LSP 诊断，形成"编辑→诊断→修复"闭环。

**验收**：
- edit_file/write_file 成功后，工具结果末尾附带该文件的 LSP diagnostics（如果 LSP 启用且有对应语言服务器）
- 模型改完代码能立即看到 "undefined: foo" 这类错误，自动修复，不用用户提示

### A4. Goal Judge 接线

**当前痛点**：Goal Judge 代码完整（goal_judge.go）但 boot.go 从不注入，goal 完成靠模型自报文本标记，会"乐观提前收工"。

**目标**：goal 完成判定由独立 LLM 冷读 transcript，对抗主 agent 的乐观。

**验收**：
- boot.go 注入 GoalJudge（controller 有现成分支 controller.go:699-714）
- goal 模式下模型说 `[goal:complete]` 时，goal judge 独立验证；不满足则注入 synthetic turn 继续
- 可配置开关（默认开，可关回纯文本标记）

### A5. compose 工作流（核心，大工程）

**当前痛点**：复杂编码任务（"实现这个 feature"）没有端到端工作流，执行无验证无重试。

**目标**：momapeer 能自主完成"设计→实现→验证→评审"全流程，带失败重试，不退化成盲写。

**用户感受**：
- 用户给一个多步任务（如"加一个 TOML 配置加载器"）
- momapeer 自动：探索代码 → 出方案 → 审批 → 实现 → 跑测试验证 → 失败则定位修复重试 → 评审 → 报告
- 全程任务面板可见进度，每步有证据，用户可随时介入

**验收标准**：
- **编排确定性**：compose 是 Go 编排器驱动（不是 prompt 引导模型自己走），阶段顺序固定
- **验证闭环**：Implement 后自动跑项目测试（go test/npm test/build），allPassed 才进下一步；失败喂回重试，≤3 轮
- **评审闭环**：Verify 通过后跑 review，critical 问题进 fix 循环，≤2 轮
- **证据门**：每步完成必须附证据（复用 evidence Ledger + complete_step），不允许"声称完成无证据"
- **不破坏 Plan Mode**：compose 接在 Plan Mode 审批 gate 之后（task 数量≥3 时走 compose 循环，<3 走现状单次执行），Plan Mode 的 /plan、Shift+Tab、plan card 全部不变
- **不做 worktree**：串行执行（尊重 v0.3.7 决策），拓扑排序仍强制依赖顺序
- **不复活双模型**：编排器是单 agent + subagent 调度，不是 planner+executor（尊重 v0.3.9 决策）

### A6. compose skill 体系

**目标**：compose 的每个阶段有专门 skill 提供纪律约束，新 skill 可按阶段角色被编排器调用。

**验收**：
- 移植 MiMo 的核心 compose skill（tdd/verify/debug），放 internal/skill/compose/
- tdd skill："无失败测试不写产品代码"铁律
- verify skill："无新鲜验证证据不声称完成"铁律
- Skill 结构体加 Hidden 字段，compose skill 在非 compose 上下文不污染主索引

---

## 六、主线 C：cowork 继承编码升级（第 7-8 周，A 完成后）

### C1. cowork 复用编码底盘

**原理**：COWORK_TOOL_SLIMDOWN 的三桶分解已确立——编码核 = Universal + Coding，cowork 核 = Universal + Office。Universal 层（bash/read/edit/grep/todo/ask/run_skill）是共享脊柱。

**目标**：A 主线的编码底盘升级，cowork 自动继承。

**验收**：
- A3（edit 后 LSP 回灌）：cowork 编辑脚本/配置时也享受诊断（如果该文件类型有 LSP）
- A5（compose 工作流）：cowork 的复杂任务（如"重构所有 PPT 模板"）也能走 compose 的验证闭环
- A2（prompt 纪律）：cowork 的 CUA prompt 继承编码底盘的 reversibility 框架
- 办公工具（browser/email/ppt/rag）保持 skill 封装，不进编码核

### C2. cowork 工具瘦身收尾

**现状**：9 个编码专属工具早已删完（apply_patch/glob/ls 等），office 工具已 Hide。

**目标**：cowork 工具面精简到位，主循环只暴露必要的 universal + office 入口。

**验收**：
- cowork profile 下主循环可见工具数 ≤ 18（COWORK_TOOL_SLIMDOWN 目标）
- 领域能力全部经 run_skill 委派
- 编码核的工具在 cowork 下不可见（profile 隔离，不是删工具文件）

---

## 七、跨主线的硬约束（任何改动都不能违反）

来自 CHANGELOG "经核实不做"段 + 设计决策，逐行核查确认：

1. **纯 Go，零 Python/faiss/langchain 依赖**（主进程零 CGO）——PPT 的 Python 桥是办公外挂不算
2. **不复活双模型 planner-executor**（v0.3.9 决策）——规划靠 Plan Mode + 主模型自规划 + compose 编排
3. **不做 worktree**（v0.3.7 决策）——无并行执行阶段，串行够用
4. **不做并行 subagent**（task 串行化避 write race）
5. **memory 不加回 FTS5/bitemporal**（v0.3.8 决策）——除非重新论证推翻
6. **permission deny-rule 不重造**（permission.go:108 已实现）——且 bash 参数级只读判定它表达不了，plan mode 用独立布尔门是合理的
7. **不重构 Plan Mode 为 compose.Design**（~30 文件 + 4 硬伤，增量叠加更优）
8. **不用 prompt 引导 compose 7 阶段**（必须确定性 Go 编排器）

---

## 八、里程碑与验收

### M0 · 地基修复 ✅ 完成
- B1-B4 全部验收通过（6 真 bug + 11 死代码 + 30 条文档债 + 常量归一）
- `go test ./...` + `tsc` 全绿
- 文档 grep `planner_model` 零命中（CHANGELOG 除外）
- 死代码清理后代码量净减少

### M1 · 编码底盘升级 ✅ 完成
- A1（plan mode 探索修复）：✅ ReadOnlyCallChecker（bash 参数级判定）+ PlanModeMarker 工具清单修正 + hardPermission 数据层（HardDeny）
- A2（prompt 纪律）：✅ DefaultSystemPrompt 加编辑原则/工具分层/reversibility
- A3（edit LSP 回灌）：✅ edit/write 后自动 LSP diagnostics
- A4（Goal Judge）：✅ boot.go 注入 GoalJudgeWithRetry
- **可度量**：待 benchmark 验证"声称完成但未验证"比例下降

### M2 · compose MVP ✅ 完成
- A5（compose 工作流）：✅ 确定性 Go 编排器（Implement→Verify→Review→retry）
- A6（compose skill 纪律）：✅ verifyDiscipline/implementDiscipline/reviewNudge
- PLAN_INVALID 重新规划：✅ 模型区分"代码 bug"vs"计划前提错误"，后者暂停通知用户审批
- **可度量**：待端到端验证 3-5 步 feature 端到端完成率

### M3 · cowork 继承（待做）
- C1/C2：编码底盘升级自动惠及 cowork（universal 层共享）

### P0 编码能力补强 ✅ 完成
- 模型族 prompt 分发（qwen/glm/deepseek/kimi/jiutian 各族针对性 addon）
- glob 工具（**/*.go 递归匹配）
- LSP 扩展（lsp_workspace_symbol + lsp_implementation）
- hardPermission 数据层（Policy.HardDeny 不可被用户配置覆盖）

### 全局 RPM 治理 ✅ 完成
- 默认 RPM 5→60
- RAG extractor 堆上全局 RPM（堵住唯一绕过的路径）

### M1 · 编码底盘升级（第 4 周末）
- A1（plan mode 探索）、A2（prompt 纪律）、A3（edit LSP 回灌）、A4（Goal Judge）验收通过
- **可度量**：同等编码任务 benchmark，"声称完成但未验证"比例下降 ≥50%

### M2 · compose MVP（第 6 周末）
- A5（compose 工作流）+ A6（compose skill）验收通过
- **可度量**：给一个 3-5 步的 feature 任务，compose 能端到端完成（设计→实现→验证→评审），最终测试通过率 ≥80%，无需人工干预

### M3 · cowork 继承（第 8 周末）
- C1/C2 验收通过
- cowork 复杂任务也能走 compose 验证闭环

---

## 九、成功标准（v0.4.0 发布时）

**用户侧**：
1. 给 momapeer 一个中等复杂度的编码任务（如"给 config 加个 TOML 加载器并写测试"），它能端到端完成，最终测试通过，无需我反复提示"你没验证"
2. plan mode 下让 agent 调研代码，它能用 explore subagent 高效探索，不撞 blocked
3. 我在 serve 切模型、在桌面开多 tab，状态都不错乱
4. 照 README/GUIDE 操作，不会配到不存在的字段

**维护者侧**：
5. 新人读 boot.go 不再被 "two-model Coordinator" 注释误导
6. grep `PlanModeFromContext` 零结果（死代码已清）
7. 改 exit_plan_mode 常量只需改一处（单一真源）
8. DefaultSystemPrompt 里有清晰的编码纪律，模型行为更可预期

**不变**：
9. 单 Go 二进制、多前端、九天对接、codegraph/evidence/checkpoint 独家优势——全部保留
10. 纯 Go、不做 worktree/双模型/并行 subagent——硬约束全部遵守

---

## 附录 · 与现状核查报告的映射

本目标文档的每条都对应 `CODING_BASELINE_2026-07-04.md` 的具体发现：
- B1 对应 H1/H4/H5/H6/M1/R6
- B2 对应 D1-D11
- B3 对应文档债 30 条
- B4 对应 R1/R3/R9/R10
- A1 对应 H2/H3
- A2 对应 DefaultSystemPrompt 现状
- A3 对应 edit/write 无 LSP 回灌
- A4 对应 Goal Judge 死接线
- A5 对应 compose 完全缺失 + 4 硬伤处理
- A6 对应 Skill.Hidden 缺失（M6）

现状核查报告提供"问题在哪、源码行号、连锁影响"；本目标文档提供"做到什么程度、用户感受、验收标准"。两份配合使用。
