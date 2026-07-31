---
name: ppt-auto
description: 使用PPT模板生成演示文稿。根据用户主题，通过SVG路径或模板填充方式生成专业PPT。
runAs: subagent
effort: high
allowed-tools: bash, read_file, write_file, edit_file, grep, todo_write, image_understand, image_generate, web_search, web_fetch
---

# PPT 生成 Skill

根据用户主题和PPT模板，生成专业演示文稿，输出为 `.pptx` 文件。

## ⚠️ 前置条件：Python 3.10+

本 skill 需要系统已安装 **Python 3.10+**（不再携带嵌入式运行时，跨平台通用）。

**首次使用前，请安装依赖：**

- macOS / Linux：`bash <skill_dir>/setup_python.sh`
- Windows：双击或运行 `<skill_dir>\setup_python.bat`

脚本会执行 `pip install -r <skill_dir>/requirements.txt` 安装 python-pptx、Pillow、lxml 等依赖（纯 Python，无需 Office）。

> 若系统中没有 `python3` 命令，请先安装 Python 3.10+：
> - macOS：`brew install python`
> - Ubuntu/Debian：`sudo apt install python3 python3-pip`
> - Windows：从 https://www.python.org/downloads/ 安装并勾选 "Add to PATH"

## 你会收到什么 / 要产出什么

- **输入**：一个主题（如"企业数字化转型方案"），可选附带源文档（PDF/DOCX 等）和页数/风格要求。
- **输出**：`<project_dir>/exports/*.pptx`（+ 可选 PDF），每页带演讲者备注。
- **全程约 12–15 步**，建议用 `todo_write` 跟踪进度（尤其多页 PPT）。

## 路径占位符

下文命令中 `<skill_dir>` 和 `<project_dir>` 是占位符，调用时替换为实际绝对路径：

- `<skill_dir>`：本 skill 的安装目录（即 `SKILL.md` 所在目录）。
- `<project_dir>`：Step 4 `init` 创建的工作目录（`init` 会打印它的绝对路径）。后续所有文件（SVG、备注、导出）都在它下面。**在 Step 4 init 之前，`<project_dir>` 不存在**——需要它的步骤都在 init 之后。

> **Windows 提示**：下文统一用 `python3`。若 Windows 上 `python3` 命令不存在（python.org 的安装包只提供 `python.exe`），把 `python3` 换成 `python` 即可。
## 两条生成路线

**默认走路线 A（SVG 路线）**，除非用户明确要求"直接用模板填充/不改版式"才走路线 B：

- **路线 A（SVG 路线，Step 1–15）**：逐页生成 SVG → 转成 PPTX。布局完全自由，推荐用于从零设计。`svg_to_pptx.py` 用纯 Python（python-pptx），**不需要安装 Office**。
- **路线 B（模板填充，见末节）**：直接在原生 PPTX 模板的占位符里填内容（`template_fill_pptx.py`）。保留模板原有版式，适合"模板已经很完美，只想换文字/数据"。

---

## 路线 A：SVG 生成工作流

### Step 1: 提取源内容（如有文档）

如果用户提供了文档，先提取内容。注意此时尚未 init 项目，输出路径用一个临时位置（Step 4 init 后再移入 `<project_dir>/sources/`）：

```bash
python3 <skill_dir>/scripts/extract_content.py <file1.pdf> <file2.docx> source_content.json
```

支持格式：PDF、DOCX、XLSX、CSV、TXT、MD。init 完成后把产物挪到 `<project_dir>/sources/`。

### Step 2: 联网搜索（如需要）

如果用户主题需要最新数据：

```bash
web_search(query="<根据用户主题组织搜索关键词，如：XX行业 2024 最新趋势/数据/政策>")
```

### Step 3: 写内容大纲（构思 + 落盘）

**在生成 SVG 之前，先规划好每页的内容大纲。这一步分两段：先构思，init 项目后落盘成文件。**

根据主题、源文档、搜索结果，规划每页的内容大纲。**大纲内容必须来自用户给的具体主题和素材，不要套固定结构。**

每页大纲包含：
- 页面类型（封面/目录/内容/结尾）
- 页面标题
- 内容要点（3-6 条）
- 布局建议（表格/时间线/卡片/对比等）——参考 `references/layout_templates.md` 的布局选型

**构思完成后，在 Step 4 init 项目之后，把大纲写入 `<project_dir>/design_spec.md`**（项目根目录）。这一步**必须落盘**——项目的验证逻辑（`project_manager.py validate`）会检查该文件是否存在，缺失会警告 "Missing design specification file"；且生成中途你要反复回看大纲保证每页贴合规划，不落盘容易跑偏。

### Step 4: 初始化项目

```bash
python3 <skill_dir>/scripts/project_manager.py init <project_name> --format ppt169
```

init 会创建标准项目目录，后续所有步骤的文件都落到对应子目录，**路径不要写错**：

| 目录 | 用途 | 由哪一步写入 |
|------|------|--------------|
| `svg_output/` | 逐页 SVG 源文件（`slide_01.svg`…） | Step 8 生成 SVG |
| `notes/` | 演讲者备注 | Step 11 |
| `exports/` | 最终 PPTX / PDF | Step 12、Step 14 |
| `backgrounds/` | 模板导出的背景图 | Step 6 |
| `previews/` | PPTX 截图（仅 validate 模式） | Step 13a |
| `sources/` | 源文档 | Step 1（可选） |

> `svg_to_pptx.py` 默认从 `svg_output/` 读 SVG、从 `notes/` 读备注、输出到 `exports/`。若 SVG 没写到 `svg_output/`，转换会读不到。

**init 完成后，立刻把 Step 3 构思的大纲写入 `<project_dir>/design_spec.md`**（项目根目录的 markdown 文件）。这是后续生成 SVG 时的内容蓝图，也是项目完整性校验的必需文件。

### Step 5: 读取配置（单一事实源，只读一次）

**在任务开始时，读取一次 `template_config.json`，并把其中约束牢记在心、贯穿后续所有 SVG 生成：**

```bash
read_file <skill_dir>/template_config.json
```

**`template_config.json` 是配色、字号、布局规则、内容密度的唯一事实源。** 生成每一页 SVG 时严格遵守其中字段——`check_svg.py`（Step 10）会按这份配置检查，自行发挥的配色会通不过。关键字段：`colors`（hex 值）、`fonts`、`rules`（forbidden 元素/字数/对齐/留白/`background_rules`/`content_density`）、`default_prompt`。

> 下方 Step 7/8 会把这些约束展开成生成时可直接套用的具体值，方便逐页生成 SVG 时参考——但若与 config 有出入，**以 `template_config.json` 为准**（config 是检查器实际校验的依据）。

### Step 6: 分析模板（导出背景图）

```bash
python3 <skill_dir>/scripts/analyze_template.py <template.pptx> <project_dir>/backgrounds/
```

> ⚠️ 此脚本通过 COM 调用 **PowerPoint 或 WPS** 导出每页背景图。若机器上两者都未安装，会报错——此时回退为不使用模板背景（SVG 用纯白底 + config 配色），并告知用户"未检测到 Office/WPS，已用纯色背景代替模板背景"。

### Step 7: 规划页面布局

根据 Step 3 的大纲，为每页选定布局类型。版式要多样，同一版式不超过总页数的 1/5。

**布局选型参考 `references/layout_templates.md`**——它给出每种布局的坐标规格（封面、2×2卡片网格、时间线、左右对比、表格、仪表盘等）。规划时就对照它选，不要等到生成时才找。

模板使用原则（来自 `template_config.json` 的 `template_usage`）：背景保持原样、保留 logo、遵循配置配色、布局可调整但须商务克制。

### Step 8: 逐页生成 SVG

逐页生成 SVG，每页写入 `<project_dir>/svg_output/slide_01.svg`、`slide_02.svg`…（文件名按页序编号，`svg_to_pptx` 按此顺序合成）。每页 SVG 都必须：

1. 背景图作为第一个 `<image>` 元素
2. viewBox 固定为 `"0 0 1280 720"`
3. 字体：`font-family="Microsoft YaHei, Arial, sans-serif"`
4. 配色：主色 `#1084CD`，强调色 `#FF7F00`，浅蓝 `#D9EAF7`/`#EAF5FC`，正文 `#333333`，辅助文字 `#888888`
5. 文字颜色：标题用主色 `#1084CD`，正文用 `#333333`，辅助说明用 `#888888`；不要在深色底上放大量正文
6. 框体、图标、装饰线：优先用主蓝和浅蓝；橙色 `#FF7F00` **只用于**关键数据、按钮、警告、待办/风险标记，每页最多 1 个橙色焦点
7. 每页以白底和模板背景为主，主蓝为结构色，橙色只做点状强调；不要把所有颜色堆在一页
8. 同类元素必须对齐，间距均匀，不要参差不齐
9. 同一层级字号一致：标题 26px，卡片标题 18px，正文 14px
10. 内容页信息密度要高、尽量减少空白（封面/结尾页除外，它们天然简洁）
11. **禁止** `filter`/`feDropShadow`/`pattern`/`mask`/`foreignObject`
12. 背景图保持原样，不加遮罩、渐变叠加或半透明层
13. 内容页标题：`x=60, y=60`，`font-size=26`，`fill=#1084CD`，bold（左上角固定，勿居中）
14. 输出纯 SVG 代码，不要 markdown 代码块

> 以上 4–10、13 条的具体值（配色、字号、标题位置）均与 `template_config.json` 的 `colors`/`rules`/`default_prompt` 一致，列出是为了生成时直接套用。若将来改了 config 的配色，以 config 为准。

**中国移动商务风格补充约束**（也源自 config，生成时务必遵守）：

- **信息密度高、空白少（仅内容页）**：内容页要填满内容区域（y=90~620），4 个纵向区域都必须有内容，不要留大块空白——中国移动 PPT 要求信息充实。**封面和结尾页不受此约束**（它们只有标题/致谢，天然留白多，检查器也会跳过密度检查）
- 白色/近白背景应占页面主要视觉面积，普通内容页不得做成满屏色块页（底色留白，但内容区要填满）
- 橙色视觉面积不得超过 3%–5%，不得用于整块卡片背景、整页标题底、普通装饰线
- 禁止紫色、棕色、土黄色、霓虹色、彩虹图表、多色渐变
- 图表最多 3 个主色：主蓝、浅蓝、橙色（橙色只标重点项）
- 封面只叠加标题和副标题；结尾页只叠加致谢文字（感谢聆听+副标题+联系方式），不加卡片/图表/色块

**视觉元素来源：**

| 需求 | 方式 |
|---|---|
| 封面主视觉图 / 内容页配图 | `image_generate` 工具 |
| 图表/流程图 | 引用 `templates/charts/` 模板（见下方资源；`charts_index.json` 含选型规则） |
| 图标 | 引用 `templates/icons/` 库 |
| 装饰元素 | 直接写 SVG 代码 |

### Step 9: 修复 SVG（XML 合法性）

`fix_svg.py` 处理**单个文件**，对 `svg_output/` 下每一页都跑一次（输入和输出可以是同一个文件，就地修复）：

```bash
python3 <skill_dir>/scripts/fix_svg.py <project_dir>/svg_output/slide_01.svg <project_dir>/svg_output/slide_01.svg
# 对 slide_02.svg、slide_03.svg … 重复
```

### Step 10: 质量检查（根据模式决定是否运行）

`check_svg.py` 同样是**单文件**，对每页跑一次：

```bash
python3 <skill_dir>/scripts/check_svg.py <project_dir>/svg_output/slide_01.svg --config <skill_dir>/template_config.json --mode <fast|validate>
# 对每页重复
```

模式由用户在设置页选择（`template_config.json` 的 `mode` 字段）：**`fast`（默认）**跳过本步直接进 Step 11；**`validate`**逐页检查，有问题的页用 `edit_file` 改 SVG 后重查，最多 3 轮。

### Step 11: 生成演讲者备注

```bash
python3 <skill_dir>/scripts/save_notes.py <project_dir> <notes_json>
```

备注规则：
- 每页 2-5 句话，是演讲者会说的话
- 不要重复页面上的文字，而是解释和补充
- 开头可以用过渡句，结尾可以用总结句

### Step 12: 转换 PPTX

```bash
python3 <skill_dir>/scripts/svg_to_pptx.py <project_dir>
```

此步用 python-pptx（纯 Python），**不需要 Office**。

### Step 13: 视觉质量检查（仅 validate 模式）

**仅在 `mode=validate` 时执行。快速模式跳过。**

**Step 13a: 导出截图**（需要 PowerPoint/WPS）

```bash
python3 <skill_dir>/scripts/export_previews.py <project_dir>
```

> ⚠️ 此脚本通过 COM 调用 PowerPoint/WPS 截图。未安装则跳过本步并告知用户。

**Step 13b: 逐页检查**

对每页截图调用 `image_understand`：

```
image_understand(
  image="<project_dir>/previews/slide_03.png",
  prompt="检查这个PPT页面：
1. 文字是否压住了边框或装饰线？
2. 文字是否超出了框体边界？
3. 元素是否对齐整齐？
4. 颜色是否协调统一？
5. 文字是否清晰可读？有没有内容丢失或看不清？
6. 结尾页是否只有致谢文字（没有多余内容）？
每项回答：是/否
最后总结：通过/需改进+具体问题"
)
```

**Step 13c: 修正问题**

如果发现问题：记录问题页面 → 用 `edit_file` 只改该页 SVG（不要整体重写）→ 修复 + 检查 + 转换 → 再次检查，直到通过。

### Step 14: 导出 PDF（可选，需要 PowerPoint/WPS）

```bash
python3 <skill_dir>/scripts/export_pdf.py <input.pptx> <output.pdf>
```

> ⚠️ 此脚本通过 COM 调用 PowerPoint/WPS。未安装则跳过并告知用户。

### Step 15: 用户反馈处理

- **解析反馈**：如"第3页太空"→增内容密度；"换布局"→换布局类型重生成该页；"颜色不好看"→调整（仍须在 config 配色范围内）
- **只改有问题的页**：用 `edit_file` 改单页 SVG，其他页保持不变
- **确认**：修正后询问"已修正第X页，您看看是否满意？"，最多 3 轮

---

## 路线 B：模板填充工作流（不经 SVG）

当用户明确要求"直接用模板填充/不改版式"时，用 `template_fill_pptx.py` 直接在原生 PPTX 模板的占位符里填内容：

```
analyze（分析模板结构）→ scaffold（搭内容骨架）→ check-plan（校验计划）→ apply（填充）→ validate（校验）
```

与路线 A 二选一，不要混用。详细参数见 `template_fill_pptx.py --help`。

---

## 用户输入覆盖

用户自然语言输入覆盖 `template_config.json` 中的默认值：

| 用户说 | 覆盖内容 |
|--------|----------|
| "用XX模板" | template |
| "做10页" | pages |
| "深色风格" | style + colors |
| "用绿色主色" | colors.primary |
| "要有表格" | content_requirements 追加 |
| "快速模式" | mode = fast |
| "校验模式" | mode = validate |

---

## 脚本说明

| 脚本 | 功能 | 依赖 |
|------|------|------|
| extract_content.py | 提取源文档内容（PDF/DOCX/XLSX/CSV/TXT）→ JSON | 纯 Python |
| project_manager.py init | 初始化项目目录结构（**推荐**）。⚠️ `import-sources` 子命令依赖未打包的脚本，本 skill 内不可用，导入源文件请改用 `extract_content.py` | 纯 Python |
| fix_svg.py | 修复 SVG XML 错误 | 纯 Python |
| check_svg.py | SVG 质量检查，支持 `--config <json> --mode fast\|validate`（按 config 校验） | 纯 Python |
| svg_to_pptx.py | `svg_output/` + `notes/` → `exports/*.pptx` | python-pptx（纯 Python） |
| save_notes.py | 保存演讲者备注到 `notes/` | 纯 Python |
| svg_quality_checker.py | SVG 质量检查（详细版，支持 `--template-mode`/`--format`） | 纯 Python |
| analyze_template.py | 模板分析，COM 导出每页背景图 | **需 PowerPoint 或 WPS** |
| export_previews.py | 从 `exports/*.pptx` 导出每页 PNG 截图 | **需 PowerPoint 或 WPS** |
| export_pdf.py | PPTX → PDF 导出 | **需 PowerPoint 或 WPS** |
| template_fill_pptx.py | **路线 B 专用**：不经 SVG，直接填充原生 PPTX 模板 | 见脚本内说明 |

---

## 资源

| 资源 | 数量 | 用途 |
|------|------|------|
| templates/charts/ | 23 个图表 SVG + `charts_index.json`（含 71 个图表的选型规则） | 图表/流程图，可直接引用 |
| templates/icons/ | 5 个图标库（chunk-filled / phosphor-duotone / simple-icons / tabler-filled / tabler-outline） | 图标，可直接引用 |
| templates/visual-styles/ | 19 种视觉风格 | 设计参考 |
| templates/modes/ | 6 种叙事模式 | 内容组织参考 |
| references/layout_templates.md | 多种布局坐标规格 | Step 7 规划时对照选型 |
| references/error_handling.md | 错误处理指引 | 排错参考 |
