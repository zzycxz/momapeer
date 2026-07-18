# RAG 表格类文档分块（CSV/TSV）+ 中文切片乱码修复 — 实施规划

> 状态：待实施（Planning） · 涉及：`internal/rag/` · 无新依赖
> 编写日期：2026-06-30

## 一、背景与目标

知识库（RAG）当前对表格类文档处理存在缺陷。经核对源码确认问题如下，本次一并修复：

1. **表格竖列语义丢失**：`chunkDoc`（`store.go:310`）的 md/txt 分支按 `\n\n` 切段，CSV 不在其列 → 落到 `windowChunk` 按字节暴力切 → 行被腰斩、表头仅存于第一块。
2. **纯转 Markdown 无效**：Markdown 表格行间只有单 `\n`，整表被当成一个段落，超 1200 字符后仍被打回 `windowChunk`，问题原样复发。因此必须为表格写**专门的、按行切分**的 chunker，绕开 `\n\n` 分段。
3. **中英混排切片乱码**：`windowChunk` 用 `s[i:end]` 字节切片（`store.go:354-367`）。经实证（编译运行验证）：
   - **纯中文（3 字节字符）安全**：`1200 % 3 == 0`，1200 字节边界恰好对齐字符边界，不损坏。
   - **纯英文（1 字节）安全**：同理对齐。
   - **中英混排会损坏（高发）**：任何"前导非 3 倍数字节"（如一个英文字母/标点/数字）会让后续所有 1200 边界错位，切在 3 字节中文的中间产生非法 UTF-8。**真实中文技术文档（中文为主+少量英文术语/标点/数字）实测损坏率 100%**（3/3 块 `utf8.ValidString == false`）。
   - 影响面：`windowChunk` 是所有格式的兜底切片器，代码文件、超长段落、CSV 兜底都走它。修复后所有格式受益。

**目标**：表格类按行切分 + 每块强制保留表头（用 Markdown 管道表格表示）；并把 `windowChunk` 改为 rune 安全，使所有格式受益。

## 二、根因定位（已核对）

| 现象 | 位置 | 机理 |
|---|---|---|
| CSV 不走表格逻辑 | `store.go:318` 分支 `ext==""/md/txt/markdown` 不含 csv | 直接落到 `store.go:349` 的 `windowChunk` |
| 表头丢失 | `windowChunk` 无表头概念 | 切块后只首块有列名 |
| 行被腰斩 | `windowChunk` `s[i:end]` | 固定 1200 字符硬切，不分行 |
| 中文乱码 | `windowChunk` 字节切片 `s[i:end]` | 中英混排时 1200 边界落在 3 字节中文中间，产生非法 UTF-8（实测真实中文文档 3/3 块损坏） |
| 影响面全局 | `store.go:326`、`:349` 两处调用 `windowChunk` | md/txt 超长段落、代码文件、CSV 兜底都走它 |

`chunkDoc` 有两个调用点：`Import`（`store.go:141`）与 `enqueueFile`（`extract.go:230`），**两处都受益**，无需分别改。

## 三、改动清单（2 源文件 + 1 测试文件）

### 改动 1：`internal/rag/store.go`

**1a. 新增 `chunkTabular(body string, comma rune) []string`**（置于 `chunkDoc` 之前）

- 用标准库 `encoding/csv` 解析，正确处理带引号字段 `"张,三"` 与内嵌换行（普通 `strings.Split` 会切错）。
- 算法（表头驻留 / Header Retention）：

  ```
  读全部记录 recs
  if 解析失败 or len(recs) < 2 { return nil }   // 走通用兜底
  header := recs[0]
  mdHeader := renderTableHeader(header)         // "| 姓名 | 年龄 |\n| --- | --- |\n"
  cur := strings.Builder{}; cur.WriteString(mdHeader)
  for row in recs[1:]:
      mdRow := renderTableRow(row, 列数)        // "| 张三 | 35 |\n"
      if cur.Len() + len(mdRow) > maxChunk 且 cur 已含数据行 {
          flush(cur); cur.Reset(); cur.WriteString(mdHeader)   // 新块重带表头
      }
      cur.WriteString(mdRow)
  flush(cur 若含数据)
  ```

- 列数以**表头**为准；数据行列数不一致时，少则补空、多则截断，保证表格闭合。
- 渲染：单元格内 `|`、换行转义为字面文本（避免破坏表格结构）。
- 优雅降级：解析失败 / 仅表头无数据 → 返回 `nil`，由 `chunkDoc` 继续走通用兜底，**行为不退化**。

**1b. `chunkDoc` 增加表格路由**（`store.go:310`，段落分支**之前**插入）

```go
// Tabular formats: row-aware chunking with per-chunk header retention so the
// vertical (column) semantics survive splitting. Rendered as Markdown pipe
// tables, which carry column names into every chunk.
if ext == "csv" || ext == "tsv" {
    comma := ','
    if ext == "tsv" { comma = '\t' }
    if c := chunkTabular(body, comma); len(c) > 0 {
        return c
    }
    // 解析失败 → 继续往下走通用兜底
}
```

放在段落分支之前，确保表格**绕开** `\n\n` 切段。

**1c. `windowChunk` 改为 rune 安全**（`store.go:354`）

```go
func windowChunk(s string, max int) []string {
    if len(s) <= max { return []string{s} }      // 快路径：短串直接返回
    r := []rune(s)
    var chunks []string
    for i := 0; i < len(r); i += max {
        end := i + max
        if end > len(r) { end = len(r) }
        chunks = append(chunks, string(r[i:end]))
    }
    return chunks
}
```

把口径从"字节"修正为"字符"（与 maxChunk 注释 "chars" 一致）。代码文件、超长段落、CSV 兜底全部受益。

> 注意：md/txt 路径的 `cur.Len()`（`store.go:335`）仍为字节，但它只作 flush 触发阈值、实际分割在段落边界（`\n\n`），不下到字符中段，**不产生乱码**，故不在本次改动范围（避免扩大语义）。

### 改动 2：`internal/rag/extract.go`

`isSupportedExt`（`extract.go:519`）追加 `"tsv"`，使 TSV 进白名单并复用同一 chunker（零额外逻辑）。

### 改动 3：`internal/rag/store_test.go`（TDD，先写后改）

| 测试 | 验证点 |
|---|---|
| `TestChunkTabularHeaderRetention` | 多行 CSV → 多块，**每块**均以 `\| 姓名 \|` 表头行 + `\| --- \|` 分隔行开头 |
| `TestChunkTabularQuotedFields` | `"张,三",35` → 渲染保留为 `张,三`，不被逗号切碎 |
| `TestChunkCSVRoute` | `chunkDoc(body,"csv")` 走表格分支、输出 Markdown 表格（集成） |
| `TestChunkTSVRoute` | `chunkDoc(body,"tsv")` 同样走表格分支 |
| `TestChunkTabularOversizedRow` | 单行渲染即 >1200 → 仍单独成块且带表头、不丢弃 |
| `TestWindowChunkRuneSafe` | 中英混排（如 `"a" + 中文*500`）、字节数 >1200 → 每块 `utf8.ValidString == true`（修复前实测为 false）；并验证纯中文长文本同样不退化 |
| `TestCSVSearchRoundTrip` | 导入真实 CSV → 按某列值搜索能命中（端到端） |

## 四、边界情况处理

- **单行超长**：哪怕一行渲染就超 1200，仍强制单独成块（含表头），**宁可块大也不丢数据**。
- **CSV 解析失败**（损坏文件）：`chunkTabular` 返回 nil → 落到 `windowChunk` 兜底，不退化。
- **仅表头无数据行**：返回 nil → 走通用兜底。
- **空 body**：`chunkDoc` 开头已 `TrimSpace` 拦截，不变。
- **列数不一致**：以表头列数为准对齐（少补空、多截断）。
- **`|` 出现在单元格内**：转义为 `\|` 或全角，避免破坏管道表格结构。
- **maxChunk 单位**：`windowChunk` 改为按 rune 计数后，1200 = 1200 个字符（中文也按字符算），与注释语义一致。

## 五、设计选择

1. 表格用 **Markdown 管道表格**表示（对 LLM 语义友好），不复用 `formatRows`（`officedoc.go:231`）的空格对齐文本。
2. **顺带修 `windowChunk` 乱码**（全局隐患，改动小、收益大）。
3. **加入 TSV 支持**（白名单 +1 行，复用 chunker）。

## 六、执行顺序（TDD）

1. 写 `store_test.go` 新增测试 → 跑，预期**失败**（无 chunkTabular、windowChunk 仍字节切）。
2. 改 `store.go`（`chunkTabular` + 路由 + `windowChunk` rune 化）。
3. 改 `extract.go`（`isSupportedExt` + tsv）。
4. 跑新测试 → 预期**全绿**。
5. 全量回归：`go test ./internal/rag/`、`gofmt -l`、`go vet ./internal/rag/`。

## 七、验证与回归

- `go test ./internal/rag/ -run "Tabular|CSV|TSV|Window|Rune" -v` 全绿。
- `go test ./internal/rag/` 全套通过（md/txt/html/json/搜索/CJK 不退化）。
- `gofmt -l internal/rag/*.go` 无输出；`go vet ./internal/rag/` 干净。
- 环境：`go1.26.4 windows/amd64`（已确认可用）。

## 八、不做的事（明确范围）

- 不改 `readDoc` 的二进制拒绝逻辑（PDF/xlsx 仍需先转文本——这是独立话题）。
- 不改 md/txt 路径的 `cur.Len()` 字节阈值（仅阈值、不产生乱码，避免扩大语义）。
- 不引入新依赖（复用标准库 `encoding/csv`）。
- 不动调用方（两个调用点都经 `chunkDoc`，自动受益）。
- **不与 Hyper-Extract 牵连**：HE 的 RAG（`light_rag.py`/`graph_rag.py` 等）用的是通用 `chunk_size/overlap` 文本切片，**没有表头驻留、没有 CSV 按行切**——在"表格分块"这一具体问题上 HE 没有更优实现可借鉴。本方案反而比 HE 更专门。"学习/对标 HE 的 RAG 能力"是一个独立的、更大范围议题，另立规划，不在本方案内。

## 九、实现注记（复核补充）

- `store.go` 当前 import 块（`:12-23`）**没有 `encoding/csv`**，新增 `chunkTabular` 时需补 `encoding/csv` 这一行 import。
- `maxChunk` 为 `chunkDoc` 内 `const`（`store.go:311`），`chunkTabular` 与其同包可直接复用，无需传参。
- `windowChunk` 有两处调用：`store.go:326`（md/txt 超长段落兜底）与 `store.go:349`（全局兜底），rune 化后两处自动受益。

## 十、复核记录

- **源码核对**（两轮）：`chunkDoc` 插入点（`store.go:318` 分支不含 csv）、`windowChunk` 字节切（`store.go:364`）、`cur.Len` 阈值（`store.go:335`，仅阈值不产生乱码）、白名单（`extract.go:519` 含 csv 不含 tsv）、调用点（`store.go:141` + `extract.go:230`，均经 `chunkDoc` 自动受益）。全部自洽。
- **实证核对**（编译运行）：复刻当前 `windowChunk` 字节切片逻辑，构造测试样例验证乱码根因：
  - 纯中文 1500 字节 → 2 块，`utf8 有效性 = true`（1200 % 3 == 0，安全）。
  - `"a" + 中文*500` → 2 块，**`utf8 有效性 = false`**（前导英文使边界错位，确认 bug 真实）。
  - 真实中文技术文档 2460 字节 → 3 块，**3/3 块损坏**（确认高发、改动必要）。
  - 据此修正第一节/第二节的乱码表述：原"切在中文字节中间"结论对，但需说明纯中文安全、混排才损坏，避免立论被质疑。
