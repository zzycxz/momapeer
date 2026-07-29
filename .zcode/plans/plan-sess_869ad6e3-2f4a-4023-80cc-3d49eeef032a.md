## 目标
让所有发 LLM/API 请求的路径（含工具内部）都遵守用户配置的 RPM 限制，且运行时改 RPM 后立即对所有路径生效。

## 核心设计：单一拦截点 + 接口注入

绕过限流的路径绝大多数（多模态工具、embedding、RagAsk 兜底、VLM 兜底）都经过 `internal/jiutian/api.go:58` 的 `APICall`，或直接构造九天 `/chat/completions` 请求。**在 `jiutian` 包注入 budget 是覆盖面最广、改动最集中的方案**。

**避免循环依赖**：`jiutian` 被 `provider/openai` import，所以 `jiutian` 不能 import `provider`。仿照已有的 `rag.BudgetAcquirer` 接口隔离模式，在 `jiutian` 包内定义同名接口，与 `*provider.RequestBudget` duck-type 兼容，boot 直接把 `globalBudget` 注入。

---

## 修改清单（5 个文件）

### 1. `internal/jiutian/api.go` — 新增 budget 注入 + 在 APICall 开头限流

- 新增接口与全局变量（参照 `rag/extract.go:57` 的 `BudgetAcquirer`）：
  ```go
  // BudgetAcquirer gates a request through the global RPM limiter. Subset of
  // *provider.RequestBudget, as an interface to avoid a jiutian→provider cycle
  // (provider/openai imports jiutian). boot injects the real budget at startup.
  type BudgetAcquirer interface {
      Acquire(ctx context.Context, key string, priority bool) error
  }
  var (
      budget    BudgetAcquirer
      budgetKey string
  )
  // SetBudget installs the global RPM limiter for all Jiutian platform LLM calls
  // (image/text, video/text, images/generations, embeddings). boot calls this
  // after building globalBudget. Pass nil to disable.
  func SetBudget(b BudgetAcquirer, key string) { budget = b; budgetKey = key }
  ```
- 在 `APICall`（api.go:58）开头，对**LLM-类路径**调用 `Acquire`（background 优先级 false），文件存储类路径（`/fs/`）跳过：
  ```go
  func APICall(ctx context.Context, method, path string, payload any, out any) error {
      if budget != nil && isLLMPath(path) {
          if err := budget.Acquire(ctx, budgetKey, false); err != nil {
              return fmt.Errorf("jiutian %s rate-limited: %w", path, err)
          }
      }
      // ... 原有逻辑
  }
  ```
  新增 `isLLMPath(path)` helper：匹配 `/image/text`、`/video/text`、`/images/generations`、`/embeddings`、`/chat/completions`，排除 `/fs/`。

### 2. `internal/boot/boot.go` — 注入 jiutian budget，修复 RAG extractor 绑定

在 `boot.go:206`（`globalBudget = provider.NewRequestBudget(...)`）之后，**立即**注入 jiutian 包：
```go
globalBudget = provider.NewRequestBudget(cfg.LLM.RPM, cfg.LLM.ReserveMain)
// 让所有九天平台 LLM 调用（多模态工具/embedding/VLM兜底）共享同一 RPM 桶。
// key 用九天默认 baseURL+key 形式（与 BudgetKeyForConfig 风格一致），保证所有
// 九天直调路径走同一 bucket。
jiutian.SetBudget(globalBudget, jiutianBudgetKey())
```
- 同时**修复已知断点**：把 `GlobalBudget()`（boot.go:68 死代码）从"仅定义"改为真正可用——但更干净的做法是直接在 boot.go 暴露一个 `BindRAGBudget(extractor)` 给 desktop 层调用。新增导出函数：
  ```go
  // RebindRAGBudget re-injects the current globalBudget into an extractor/RAG
  // pipeline, so a runtime RPM change (settings rebuild) propagates to RAG.
  // Called by desktop.rebuild() and after the first boot.Build.
  func RebindRAGBudget(extractor any) {
      if globalBudget == nil { return }
      if bs, ok := extractor.(rag.BudgetSetter); ok {
          bs.SetBudget(globalBudget, ragBudgetKey())
      }
  }
  ```
  （注：boot import rag 无循环依赖风险——已验证 boot.go 不 import rag，但 rag 也不 import boot，加入安全。）

### 3. `desktop/app.go` — 修复 initRAG 的 localBudget 断点

- **删除** `app.go:461-473` 创建独立 `localBudget` 的代码块（注释承诺的 re-wire 从未实现）。
- 改为：extractor 创建后暂不绑定 budget；由首次 `boot.Build` 完成后（在 `restoreOrBuildTabs` 末尾，或 `startup` 中 boot.Build 后）调用 `boot.RebindRAGBudget(a.ragExtractor)` 注入真正的 globalBudget。
- 需要让 `initRAG` 把 extractor 存为 App 字段（`a.ragExtractor`），供 rebind 使用。新增字段：
  ```go
  ragExtractor rag.Extractor  // 供 RebindRAGBudget 在 boot.Build 后注入 globalBudget
  ```
- 在 `startup`（app.go:324）的 `go a.restoreOrBuildTabs()` 之前/之后安排一次 rebind（因为 restoreOrBuildTabs 在 goroutine 里跑 boot.Build，需在它内部 boot.Build 成功后调用）。

### 4. `desktop/settings_app.go` — rebuild() 中同步 RAG budget

- 在 `rebuild()`（settings_app.go:688 `return nil` 之前）加一行：
  ```go
  // boot.Build 已重建 globalBudget；把新 budget 同步给 RAG 抽取/九天直调路径，
  // 保证运行时改 RPM 立即对全部路径生效（而非仅主对话）。
  boot.RebindRAGBudget(a.ragExtractor)
  ```
  （jiutian 包的 budget 由 boot.Build 内部重新 SetBudget，无需 desktop 额外处理。）

### 5. `desktop/rag_app.go` — RagAsk 知识库问答限流

- `RagAsk`（rag_app.go:774）当前直调 `/chat/completions`（rag_app.go:872）完全绕过限流。
- 最小改动：在构造请求前（rag_app.go:869 `apiCtx` 之后），调用 `boot.GlobalBudget()`（将其修复为可用）或经 `rag.BudgetAcquirer` 接口注入。优先复用 boot 暴露的 budget：
  ```go
  if b := boot.GlobalBudget(); b != nil {
      if err := b.Acquire(apiCtx, ragBudgetKey(), false); err != nil {
          return "", fmt.Errorf("RagAsk rate-limited: %w", err)
      }
  }
  ```
  为此需把 `boot.GlobalBudget()`（boot.go:68）从死代码激活——它本来就返回 `*provider.RequestBudget`，无副作用，只需确保 desktop 能调用（已在 boot 包，导出即可）。

---

## 修复后覆盖矩阵

| 路径 | 当前状态 | 修复后 | 拦截点 |
|---|---|---|---|
| 主Agent/Subagent/压缩/判定/max | ✅ 已覆盖 | ✅ | RateLimitedProvider（不变）|
| auto_plan_classifier / experts / screenshot | ✅ 已覆盖 | ✅ | NewProviderWithProxy（不变）|
| 九天多模态工具 image/video/gen | ❌ 绕过 | ✅ | jiutian.APICall + SetBudget |
| RAG embedding 检索 | ❌ 绕过 | ✅ | jiutian.APICall + SetBudget |
| RAG 抽取 jiutianExtractor | ⚠️ 独立 localBudget | ✅ 共享 globalBudget | RebindRAGBudget |
| RagAsk 知识库问答 | ❌ 绕过 | ✅ | GlobalBudget().Acquire |
| openai Stream 内九天 VLM 兜底 | ❌ 绕过 | ✅ | jiutian.APICall（兜底走 jiutian）|
| HE Python 服务器 | ❌ 间接 | ⏸ 本次不动（待确认 key）|
| serve 标题生成 | ❌ 绕过 | ⏸ 本次不动（频率极低）|

## 关键设计决策
1. **单一拦截点优先**：`jiutian.APICall` 一次注入覆盖 4 类路径（多模态/embedding/RagAsk兜底可用/VLM兜底）。
2. **接口隔离防循环依赖**：`jiutian` 用自己的 `BudgetAcquirer` 接口，不 import provider，与 `rag.BudgetAcquirer` 同模式。
3. **运行时同步**：`rebuild()` 加一行 `RebindRAGBudget` + boot.Build 内重设 `jiutian.SetBudget`，保证改 RPM 即时全路径生效。
4. **Budget key 选择**：九天直调路径用统一的九天 key（baseURL+JIUTIAN_API_KEY），确保所有九天调用共享同一 RPM 桶，符合"同 key 同配额"的现实。

## 验证方式
- `go build ./...` 确认无循环依赖、编译通过
- 手动测试：设置 RPM=5，连续触发主对话 + 多模态工具 + 知识库导入，观察是否都受限（UI 的 RPM 指标应反映合计用量）
- 改 RPM 后立即测试 RAG 抽取，确认即时生效（修复前需重启）

需要我开始实施吗？