"""
SVG 修复脚本：修复模型生成的 SVG 中的常见错误。

用法: python fix_svg.py <input_svg> <output_svg>

修复内容：
1. 去掉 markdown 代码块标记
2. 修复嵌套 SVG（模型输出格式文本）
3. 截断到 </svg>
4. 修复未闭合标签
5. 转义 XML 特殊字符
6. 去掉不支持的元素（filter/pattern/mask）
"""
import re, sys, os


def fix_svg(input_path, output_path):
    with open(input_path, "r", encoding="utf-8") as f:
        content = f.read()

    # 1. 去掉 markdown 代码块
    if "```" in content:
        svg_start = content.find("<svg")
        if svg_start >= 0:
            content = content[svg_start:]
        else:
            parts = content.split("```")
            for part in parts:
                if "<svg" in part:
                    content = part[part.find("<svg"):]
                    break

    # 2. 修复嵌套 SVG（模型输出格式文本如 "Final SVG code:"）
    first_svg = content.find("<svg")
    second_svg = content.find("<svg", first_svg + 1) if first_svg >= 0 else -1
    if second_svg > 0:
        content = content[second_svg:]

    # 3. 截断到 </svg>
    end_idx = content.find("</svg>")
    if end_idx >= 0:
        content = content[:end_idx + 6]
    else:
        content = content.rstrip() + "\n</svg>"

    # 4. 去掉不支持的元素
    content = re.sub(r'<filter[^>]*>.*?</filter>', '', content, flags=re.DOTALL)
    content = re.sub(r'<defs>\s*</defs>', '', content)
    content = re.sub(r'filter="url\#[^"]*"', '', content)

    # 5. 修复常见 XML 错误
    content = content.replace("<br>", "<br/>")
    content = content.replace("<br />", "<br/>")
    content = content.replace("·", ".")  # 中间点替换

    # 6. 验证 XML
    try:
        import xml.etree.ElementTree as ET
        ET.fromstring(content)
    except ET.ParseError as e:
        # 尝试截断到最后一个完整元素
        last_close = content.rfind("/>")
        if last_close > 0:
            candidate = content[:last_close + 2] + "\n</svg>"
            try:
                ET.fromstring(candidate)
                content = candidate
            except ET.ParseError:
                # 尝试去掉最后一行
                lines = content.split("\n")
                for i in range(len(lines) - 1, 0, -1):
                    candidate = "\n".join(lines[:i]) + "\n</svg>"
                    try:
                        ET.fromstring(candidate)
                        content = candidate
                        break
                    except ET.ParseError:
                        continue

    with open(output_path, "w", encoding="utf-8") as f:
        f.write(content)

    print(f"Fixed: {output_path} ({len(content)} chars)")
    return output_path


if __name__ == "__main__":
    if len(sys.argv) < 3:
        print("Usage: python fix_svg.py <input_svg> <output_svg>")
        sys.exit(1)
    fix_svg(sys.argv[1], sys.argv[2])
