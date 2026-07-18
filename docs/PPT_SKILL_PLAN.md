# PPT Skill 实施方案

## 一、目标

将 PPT 生成能力封装为 momapeer 的独立 skill 安装包。用户通过自然语言指令即可生成专业 PPT，不依赖完整 ppt-master 项目。

## 二、架构

```
用户："做一个华为智算中心的PPT"
    ↓
后端：analyze_template.py → 导出模板背景图（支持 PowerPoint 和 WPS）
    ↓
模型：读 template_config.json + 用户需求 → 逐页手写 SVG
    ↓
后端：svg_to_pptx.py → 转换 PPTX
    ↓
输出给用户
```

**原则：**
- 模型是设计师，自由创作布局和内容
- 配置是设计 brief，提供默认值，用户可编辑
- 后端只做体力活（模板分析、格式转换）
- 用户指定 > 配置默认值

## 三、文件结构

```
momapeer/skills/ppt-auto/
├── SKILL.md                    # 流程指引（给模型读）
├── template_config.json        # 默认设计约束（用户可编辑）
├── templates/
│   └── 中国移动模板.pptx        # 默认模板（打包内置）
├── scripts/
│   ├── analyze_template.py     # 模板分析（PowerPoint + WPS）
│   └── svg_to_pptx.py          # SVG 转 PPTX（从 ppt-master 提取）
└── README.md                   # 使用说明
```

## 四、各文件设计

### 4.1 template_config.json

**最小化约束，保留自由度。** 只约束必要的对齐和显示规则。

```json
{
  "template": "内置:中国移动模板.pptx",
  "template_override": null,
  "slides": { "cover": 1, "content": 2, "ending": 3 },
  "canvas": { "width": 1280, "height": 720 },
  "colors": {
    "primary": "#0070C0",
    "secondary": "#2E8B57",
    "accent": "#FF8C00",
    "text": "#333333",
    "text_secondary": "#666666",
    "card_bg": "#F5F7FA"
  },
  "fonts": {
    "family": "Microsoft YaHei, Arial, sans-serif"
  },
  "rules": {
    "text_in_box": "文字必须在框体内部，y = box_y + box_h/2 + font_size/3",
    "text_length": "单行不超过20个中文字符",
    "forbidden_elements": ["filter", "feDropShadow", "pattern", "mask", "foreignObject"]
  }
}
```

**设计意图：**
- `template_override`: 用户填本地路径后，默认模板失效
- `colors`: 只提供基础色，不限制怎么用
- `fonts`: 只指定字体族，不限制字号（模型自由选择）
- `rules`: 只约束"文字在框内"和"禁止元素"，不限制布局

### 4.2 SKILL.md

**给模型的工作流指引**，不含硬编码约束。约束全部来自配置文件。

内容要点：
1. 读取 `template_config.json` 获取设计规范
2. 解析用户自然语言输入，提取主题/页数/风格/特殊要求
3. 用户指定覆盖配置默认值
4. 调用 `analyze_template.py` 导出背景图
5. 规划页面结构（6-10页，版式多样）
6. 逐页生成 SVG：
   - 背景图作为第一个 `<image>` 元素
   - 遵守配置中的 rules
   - 内容要丰富具体
   - 布局自由创新
7. 调用 `svg_to_pptx.py` 转换

**SVG 生成注意事项**（通用，不针对特定模型）：
- 输出纯 SVG，不要加 markdown 代码块标记
- 确保 XML 格式正确（特殊字符转义：`<` → `&lt;`，`>` → `&gt;`）
- 不要在 SVG 代码后面附加说明文字
- viewBox 固定为 "0 0 1280 720"

### 4.3 analyze_template.py

**模板分析脚本**，支持 PowerPoint 和 WPS。

功能：
- 自动检测本机安装的办公软件（PowerPoint 优先，WPS 兜底）
- 导出模板每页为 PNG 背景图（1280×720）
- 根据 `slides` 配置映射封面/内容/结尾

COM 接口：
- PowerPoint: `comtypes.client.CreateObject("PowerPoint.Application")`
- WPS: `comtypes.client.CreateObject("KWPS.Application")` 或 `comtypes.client.CreateObject("PowerPoint.Application")`（WPS 兼容模式）

### 4.4 svg_to_pptx.py

**从 ppt-master 提取的核心转换脚本**。

需要提取的文件：
- `svg_to_pptx.py`（入口）
- `svg_to_pptx/` 目录（drawingml_converter、pptx_builder 等）

只提取转换相关的代码，不包含 ppt-master 的模板系统、确认 UI、图片生成等。

### 4.5 默认模板

`templates/中国移动模板.pptx` 打包内置。用户在 `template_config.json` 中设置 `template_override` 为本地路径后，默认模板失效。

## 五、配置覆盖机制

模型读取配置后，根据用户自然语言输入覆盖：

| 用户说 | 覆盖字段 | 模型行为 |
|---|---|---|
| "用XX模板" | template_override | 使用用户指定的模板 |
| "做10页" | 页数 | 生成10页 |
| "深色风格" | colors | 深色配色方案 |
| "用绿色主色" | colors.primary | 绿色为主 |
| "要有表格" | 布局偏好 | 包含表格页面 |
| "字号大一点" | 字号 | 增大字号 |

模型理解意图，不需要用户写 JSON。

## 六、实施步骤

### Phase 1：打包 skill ✅

- [x] 提取 `svg_to_pptx.py` 及其依赖（从 ppt-master）
- [x] 提取 `svg_finalize` 模块（svg_to_pptx 依赖）
- [x] 更新 `analyze_template.py` 支持 WPS
- [x] 精简 `template_config.json`（最小约束）
- [x] 完善 `SKILL.md`（工作流指引 + SVG 注意事项）
- [x] 内置 `中国移动模板.pptx`
- [x] 编写 `README.md`
- [x] 内置 Python 3.12 + python-pptx + comtypes + lxml + PIL

### Phase 2：端到端验证 ✅

- [x] 用内置 Python 测试 analyze_template.py（PowerPoint COM 正常）
- [x] 用内置 Python 测试 svg_to_pptx.py（SVG 转 PPTX 成功）
- [x] 验证 comtypes 运行时依赖（tools/ 不能删除）
- [x] 修复路径编码问题（os.path.normpath）

### Phase 3：发布（待定）

- [ ] 集成到 momapeer exe 安装包
- [ ] 测试新环境安装
- [ ] 编写用户文档

## 七、已知限制

1. **办公软件依赖**：需要 PowerPoint 或 WPS（用于模板分析）
2. **字体依赖**：中文渲染依赖系统字体（Microsoft YaHei）
3. **平台**：analyze_template.py 仅支持 Windows（COM 自动化）
4. **comtypes 首次运行**：慢 2-3 秒（重建 gen/ 缓存）

## 八、实际打包大小

| 组件 | 大小 |
|---|---|
| Python 运行时 + 依赖 | 48MB |
| Skill 脚本 + svg_to_pptx + svg_finalize | 891KB |
| 默认模板 | 960KB |
| **总计** | **50MB** |

依赖清单：python-pptx 1.0.2、comtypes、lxml、PIL/Pillow

## 九、不做的事

1. **不打包完整 ppt-master**——只提取 svg_to_pptx + svg_finalize 转换模块
2. **不在代码中硬编码布局约束**——约束在配置文件中，用户可改
3. **不校验模型输出**——模型是设计师，自己负责质量
4. **不绑定特定模型**——skill 适用于任何能生成 SVG 的模型
5. **不要求用户安装 Python**——全部内置在 skill 包中
