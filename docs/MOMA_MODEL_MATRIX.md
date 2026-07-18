# MoMA 模型参数支持矩阵

> **说明**：MoMA（九天平台）官方支持的 18 个模型，及其 reasoning / vision / effort 参数支持情况。
>
> **数据来源**：本文档的结论来自以下三处代码（修改模型时请同步更新本表与对应代码，保持一致）：
>
> | 能力 | 代码位置 |
> |---|---|
> | 18 模型完整清单（single source of truth） | `internal/config/config.go` → `BuiltinMoMAModels` |
> | reasoning / thinking 支持白名单 | `internal/provider/openai/openai.go` → `MoMAThinkingModels` |
> | vision / 多模态 支持白名单 | `internal/provider/openai/openai.go` → `MoMAVisionModels` |
> | effort 级别归一化规则 | `internal/provider/openai/effort_test.go` → `TestEffortNormalization` |
>
> **实测日期**：
> - reasoning：2026-06-13（MoMA 平台测试，验证模型是否返回 `reasoning_content`）
> - vision：2026-06-17（MoMA API 实测，100x100 PNG 验证 `image_url` 支持）
> - effort：CHANGELOG 记录（18 模型 × 5 级别兼容性测试）

---

## 一、18 模型参数支持总表

| # | 模型 ID | 厂商 | reasoning<br/>(thinking) | vision<br/>(多模态) | effort<br/>(思考强度) |
|---|---|---|:---:|:---:|:---:|
| 1 | `jiutian/jiutian-lan-236b` | 九天 | ❌ | ❌ | ❌ |
| 2 | `jiutian/jiutian-lan-35b` | 九天 | ❌ | ❌ | ❌ |
| 3 | `jiutian/jiutian-lan-thinking` | 九天 | ✅ | ❌ | ✅ |
| 4 | `jiutian/jiutian-da-35b` | 九天 | ✅ | ❌ | ✅ |
| 5 | `qwen/qwen3.6-35b` | 通义千问 | ✅ | ✅ | ✅ |
| 6 | `qwen/qwen3.6-27b` | 通义千问 | ✅ | ✅ | ✅ |
| 7 | `qwen/qwen3.5-397b-a17b` | 通义千问 | ✅ | ✅ | ✅ |
| 8 | `deepseek/deepseek-v4-flash` | DeepSeek | ❌ | ❌ | ❌ |
| 9 | `z.ai/glm-5.1` | 智谱 | ✅ | ❌ | ✅ |
| 11 | `z.ai/glm-5.2` | 智谱 | ❌ | ❌ | ❌ |
| 12 | `minimax/minimax-m2.7` | MiniMax | ✅ | ❌ | ✅ |
| 13 | `minimax/minimax-m2.5` | MiniMax | ✅ | ❌ | ✅ |
| 14 | `moonshotai/kimi-k2.6` | 月之暗面 | ✅ | ✅ | ✅ |
| 15 | `moonshotai/kimi-k2.5-thinking` | 月之暗面 | ✅ | ❌ | ✅ |
| 16 | `openai/gpt-oss-120b` | OpenAI(开源) | ✅ | ❌ | ✅ |
| — | `moma/auto-router` | 自动路由 | ❓ | ❓ | ❓ |

### 统计

- **reasoning / thinking**：11/16 支持（`z.ai/glm-5.2` 实测不支持，发 thinking 参数会卡死无响应）
- **vision / 多模态**：4/16 支持（仅 `qwen3.6-35b`、`qwen3.6-27b`、`qwen3.5-397b-a17b`、`kimi-k2.6`）
- **effort 思考强度**：与 reasoning 一致（11 个），支持 high / medium / low

---

## 二、关键设计：MoMA 用 `thinking_effort`，不是 OpenAI 的 `reasoning_effort`

MoMA 平台与标准 OpenAI 协议的字段差异（来源：CHANGELOG `MoMA effort 字段修正` + `openai.go`）：

| 协议 | effort 字段 | 适用 |
|---|---|---|
| **MoMA** | `thinking_effort` | MoMA 直连（`jiutian.10086.cn`）的 reasoning 模型 |
| **OpenAI 标准** | `reasoning_effort` | 非 MoMA 后端 |

### 协议判定逻辑

是否走 MoMA 协议（`openai.go` 中 `moma` 标志）：

```
moma = reasoning_protocol == "moma"
    OR (reasoning_protocol == "" AND IsMoMA(BaseURL) AND 模型在 MoMAThinkingModels 白名单内)
```

- 用户可在 config 用 `reasoning_protocol` 显式指定（`"moma"` / `"none"` / 自动）
- 仅当 `moma == true` 时才发 `thinking: {type:"enabled"}` 请求参数

---

## 三、effort 级别归一化规则（MoMA 直连端点）

MoMA 直连端点（`jiutian.10086.cn`）对 effort 值有特殊归一化，源于 18 模型 × 5 级别的实测兼容性测试：

| 用户传入 | 实际下发 | 原因 |
|---|---|---|
| `max` | `high` | **16/18 MoMA 模型 reject "max"**，clamp 到 OpenAI 上限 |
| `high` | `high` | 正常 |
| `medium` | `medium` | 正常 |
| `low` | `medium` | **2/18 MoMA 模型 reject "low"**，clamp 到 medium |
| `auto` | `high` | MoMA 默认深度 |
| 空 | `high` | MoMA 默认深度 |

> 非 MoMA 端点也会把 `max` clamp 到 `high`（OpenAI 上限），但 `low` 不归一化。
>
> **合法 effort 值**只有 `low` / `medium` / `high`（其它值如 `turbo` 会在 provider 初始化时被拒绝）。

---

## 四、重要说明（避免误读）

1. **reasoning = "请求侧发 thinking 参数"**——白名单控制是否发 `thinking:{type:"enabled"}`，**不是**"模型本身能不能推理"。白名单外的模型也能用，只是不发思考参数（见 `openai.go` 注释）。

2. **`moma/auto-router` 特殊**：它是 MoMA 的智能路由模型，实际能力取决于它路由到哪个底层模型，所以两个白名单都**未包含它**——参数能力是动态的。

3. **`deepseek/*` 两个模型不在 reasoning 白名单**：DeepSeek 的推理能力走自己的 `reasoning_content` 响应字段，不需要请求侧 thinking 参数，所以代码没把它们列入。

4. **vision 白名单外的模型**：传入 `image_url` 会被 MoMA 400 拒绝（错误信息："不支持的消息部件类型 image_url"）。可通过 config 的 `vision = true` 按 provider 覆盖。

5. **响应侧 vs 请求侧**：
   - 请求侧：`MoMAThinkingModels` 控制 `thinking:{type:"enabled"}` 参数是否发送
   - 响应侧：`reasoning_content` / `reasoning` 字段由模型实际返回决定，代码不按白名单过滤

---

## 五、维护指南

当 MoMA 平台增减模型或更新能力时，按以下步骤同步：

1. **新增模型**：在 `config.go` 的 `BuiltinMoMAModels` 添加
2. **若支持 reasoning**：在 `openai.go` 的 `MoMAThinkingModels` 添加
3. **若支持 vision**：在 `openai.go` 的 `MoMAVisionModels` 添加
4. **若 effort 归一化规则变化**：更新 `effort_test.go` 的 `TestEffortNormalization` 测试用例
5. **同步更新本文档的总表**

> 三处代码（清单 / reasoning 白名单 / vision 白名单）是独立的 map/slice，修改时需手动保持本文档总表与代码一致。`config.go` 的注释强调 `BuiltinMoMAModels` 是 single source of truth，其他地方引用它以防止 drift。
