"""
SVG 质量检查脚本（增强版）

用法: python check_svg.py <svg_file> [--config <config_file>] [--mode fast|validate]

检查项（按严重程度）：
ERROR（致命，必须修复）：
  - XML 格式错误
  - 禁止元素
  - 内容严重不足
  - 文字溢出边界
  - 文字重叠

WARN（警告，建议修复）：
  - 缺少背景图
  - viewBox 不正确
  - 配色不一致
  - 内容偏少
  - 对齐偏差
  - 间距不均匀
  - 字体不匹配

返回：0=通过, 1=有警告, 2=有错误
"""
import json, re, sys, os
import xml.etree.ElementTree as ET


def load_config(config_path=None):
    """加载配置文件"""
    if config_path and os.path.exists(config_path):
        with open(config_path, "r", encoding="utf-8") as f:
            return json.load(f)
    return {
        "colors": {
            "primary": "#0070C0", "secondary": "#2E8B57", "accent": "#FF8C00",
            "text": "#333333", "text_secondary": "#666666", "card_bg": "#F5F7FA"
        },
        "rules": {
            "forbidden_elements": ["filter", "feDropShadow", "pattern", "mask", "foreignObject"],
            "text_length": 20
        }
    }


def parse_text_elements(content):
    """解析所有 text 元素"""
    texts = []
    svg_ns = "{http://www.w3.org/2000/svg}"
    try:
        root = ET.fromstring(content)
        for elem in root.iter():
            tag = elem.tag.replace(svg_ns, "")
            if tag != "text":
                continue
            text_content = elem.text or ""
            if not text_content.strip():
                text_content = "".join(elem.itertext()).strip()
            if not text_content:
                continue
            x = _parse_numeric_attr(elem, "x", 0)
            y = _parse_numeric_attr(elem, "y", 0)
            font_size = _parse_font_size(elem)
            texts.append({
                "x": x, "y": y, "size": font_size,
                "content": text_content,
                "width": len(text_content) * font_size * 0.6
            })
    except ET.ParseError:
        for match in re.finditer(
            r'<text[^>]*?x="(\d+)"[^>]*?y="(\d+)"[^>]*?font-size="(\d+)"[^>]*>([^<]+)</text>',
            content
        ):
            texts.append({
                "x": int(match.group(1)), "y": int(match.group(2)),
                "size": int(match.group(3)), "content": match.group(4),
                "width": len(match.group(4)) * int(match.group(3)) * 0.6
            })
    return texts


def _parse_numeric_attr(elem, attr_name, default=0):
    val = elem.get(attr_name)
    if val is not None:
        try:
            return int(float(val))
        except ValueError:
            return default
    style = elem.get("style", "")
    match = re.search(rf'{attr_name}\s*:\s*(\d+(?:\.\d+)?)', style)
    if match:
        return int(float(match.group(1)))
    return default


def _parse_font_size(elem):
    val = elem.get("font-size")
    if val is not None:
        try:
            return int(float(val.replace("px", "").replace("pt", "")))
        except ValueError:
            pass
    style = elem.get("style", "")
    match = re.search(r'font-size\s*:\s*(\d+(?:\.\d+)?)', style)
    if match:
        return int(float(match.group(1)))
    return 16


def parse_rect_elements(content):
    """解析所有 rect 元素"""
    rects = []
    svg_ns = "{http://www.w3.org/2000/svg}"
    try:
        root = ET.fromstring(content)
        for elem in root.iter():
            tag = elem.tag.replace(svg_ns, "")
            if tag != "rect":
                continue
            x = _parse_numeric_attr(elem, "x", 0)
            y = _parse_numeric_attr(elem, "y", 0)
            w = _parse_numeric_attr(elem, "width", 0)
            h = _parse_numeric_attr(elem, "height", 0)
            if w > 0 and h > 0:
                rects.append({"x": x, "y": y, "width": w, "height": h})
    except ET.ParseError:
        for match in re.finditer(
            r'<rect[^>]*?x="(\d+)"[^>]*?y="(\d+)"[^>]*?width="(\d+)"[^>]*?height="(\d+)"[^>]*/?>',
            content
        ):
            rects.append({
                "x": int(match.group(1)), "y": int(match.group(2)),
                "width": int(match.group(3)), "height": int(match.group(4))
            })
    return rects


def check_text_overflow(texts, rects):
    """检查文字溢出边界（精确版）"""
    issues = []
    for text in texts:
        # 右边界
        if text["x"] + text["width"] > 1280:
            overflow = text["x"] + text["width"] - 1280
            issues.append(("error", f"文字 '{text['content'][:20]}' 超出右边界 {overflow:.0f}px"))
        # 下边界
        if text["y"] > 720:
            issues.append(("error", f"文字 '{text['content'][:20]}' 超出下边界 (y={text['y']})"))
        # 左边界
        if text["x"] < 0:
            issues.append(("error", f"文字 '{text['content'][:20]}' 超出左边界 (x={text['x']})"))
        # 检查是否在框体内（非标题文字）
        if text["size"] < 26:
            in_box = False
            for rect in rects:
                if (rect["x"] <= text["x"] <= rect["x"] + rect["width"] and
                    rect["y"] <= text["y"] <= rect["y"] + rect["height"]):
                    in_box = True
                    break
            if not in_box and rects:
                # 找最近的 rect
                min_dist = min(
                    ((text["x"] - (r["x"] + r["width"]/2))**2 +
                     (text["y"] - (r["y"] + r["height"]/2))**2)**0.5
                    for r in rects
                )
                if min_dist > 150:
                    issues.append(("warn", f"文字 '{text['content'][:15]}' 可能不在框体内"))
    return issues


def check_text_overlap(texts):
    """检查文字重叠（精确版）"""
    issues = []
    for i in range(len(texts)):
        for j in range(i + 1, len(texts)):
            t1, t2 = texts[i], texts[j]
            # 计算 bounding box
            box1 = {"x": t1["x"], "y": t1["y"] - t1["size"],
                     "w": t1["width"], "h": t1["size"] * 1.4}
            box2 = {"x": t2["x"], "y": t2["y"] - t2["size"],
                     "w": t2["width"], "h": t2["size"] * 1.4}
            # 检查 bounding box 交叉
            if (box1["x"] < box2["x"] + box2["w"] and
                box1["x"] + box1["w"] > box2["x"] and
                box1["y"] < box2["y"] + box2["h"] and
                box1["y"] + box1["h"] > box2["y"]):
                issues.append(("error", f"文字重叠: '{t1['content'][:15]}' 和 '{t2['content'][:15]}'"))
    return issues


def check_alignment(texts, rects):
    """检查对齐偏差"""
    issues = []
    # 收集所有 x 坐标
    x_coords = [t["x"] for t in texts if t["size"] < 26]
    if len(x_coords) < 3:
        return issues
    # 找最常见的 x 值（容差 5px）
    from collections import Counter
    rounded = [round(x / 10) * 10 for x in x_coords]
    common = Counter(rounded).most_common(3)
    for base_x, count in common:
        if count >= 3:
            outliers = [x for x in x_coords if abs(x - base_x) > 10]
            if outliers:
                issues.append(("warn", f"对齐偏差: {len(outliers)} 个元素偏离基准线 {base_x}px"))
    return issues


def check_spacing(texts, rects):
    """检查间距均匀性"""
    issues = []
    # 检查 rect 元素的垂直间距
    if len(rects) < 2:
        return issues
    y_gaps = []
    sorted_rects = sorted(rects, key=lambda r: r["y"])
    for i in range(len(sorted_rects) - 1):
        gap = sorted_rects[i+1]["y"] - (sorted_rects[i]["y"] + sorted_rects[i]["height"])
        if 0 < gap < 200:  # 只检查合理范围内的间距
            y_gaps.append(gap)
    if len(y_gaps) >= 2:
        avg_gap = sum(y_gaps) / len(y_gaps)
        for gap in y_gaps:
            if abs(gap - avg_gap) > avg_gap * 0.5:  # 偏差超过50%
                issues.append(("warn", f"间距不均匀: {gap:.0f}px vs 平均 {avg_gap:.0f}px"))
                break
    return issues


def check_vertical_coverage(texts, rects, config):
    """检查内容在垂直方向的分布均匀性"""
    vc_config = config.get("rules", {}).get("content_density", {}).get("vertical_coverage", {})
    content_area = vc_config.get("content_area", {"y_start": 90, "y_end": 620})
    y_start = content_area.get("y_start", 90)
    y_end = content_area.get("y_end", 620)
    zone_count = vc_config.get("zone_count", 4)
    min_zones = vc_config.get("min_zones_with_content", 3)

    zone_height = (y_end - y_start) / zone_count
    zones_with_content = set()

    for text in texts:
        if y_start <= text["y"] <= y_end:
            zone_idx = int((text["y"] - y_start) / zone_height)
            zone_idx = min(zone_idx, zone_count - 1)
            zones_with_content.add(zone_idx)

    for rect in rects:
        rect_center_y = rect["y"] + rect["height"] / 2
        if y_start <= rect_center_y <= y_end:
            zone_idx = int((rect_center_y - y_start) / zone_height)
            zone_idx = min(zone_idx, zone_count - 1)
            zones_with_content.add(zone_idx)

    filled_count = len(zones_with_content)
    if filled_count < min_zones:
        empty_zones = [i for i in range(zone_count) if i not in zones_with_content]
        return [("error", f"垂直覆盖不足: {zone_count}个区域仅{filled_count}个有内容")]
    return []


def check_spatial_coverage(texts, rects, config):
    """检查空间覆盖率"""
    density_config = config.get("rules", {}).get("content_density", {})
    vc_config = density_config.get("vertical_coverage", {})
    sc_config = density_config.get("spatial_coverage", {})
    y_start = vc_config.get("content_area", {}).get("y_start", 90)
    y_end = vc_config.get("content_area", {}).get("y_end", 620)
    min_ratio = sc_config.get("min_coverage_ratio", 0.45)

    total_area = 1160 * (y_end - y_start)
    if total_area <= 0:
        return []

    covered_area = sum(r["width"] * r["height"] for r in rects)
    covered_area += sum(t["width"] * t["size"] * 1.4 for t in texts)

    ratio = covered_area / total_area
    if ratio < min_ratio:
        return [("error", f"空间覆盖率不足: {ratio:.1%} (至少 {min_ratio:.0%})")]
    elif ratio < min_ratio + 0.1:
        return [("warn", f"空间覆盖率偏低: {ratio:.1%} (建议 {min_ratio + 0.1:.0%})")]
    return []


def check_element_variety(content, config):
    """检查元素类型多样性"""
    ev_config = config.get("rules", {}).get("content_density", {}).get("element_variety", {})
    min_types = ev_config.get("min_element_types", 2)
    allowed_types = ev_config.get("allowed_types", ["rect", "text", "line", "circle", "path", "polygon"])

    found_types = set()
    for elem_type in allowed_types:
        if re.search(rf'<{elem_type}[\s>/]', content):
            found_types.add(elem_type)

    if len(found_types) < min_types:
        return [("warn", f"元素类型单调: 仅 {len(found_types)} 种 ({', '.join(sorted(found_types))})")]
    return []


def check_svg(svg_path, config=None, mode="fast"):
    """检查 SVG 质量

    Args:
        svg_path: SVG 文件路径
        config: 配置字典
        mode: "fast"（宽松）或 "validate"（严格）
    """
    if config is None:
        config = load_config()

    with open(svg_path, "r", encoding="utf-8") as f:
        content = f.read()

    filename = os.path.basename(svg_path).lower()
    # Detect page type from filename. Supports both naming conventions the
    # skill produces: slide_01.svg / slide_10.svg (underscore before number)
    # and the legacy 01_cover.svg / 10_ending.svg (underscore after).
    is_cover = "cover" in filename or "01_" in filename or "_01." in filename
    is_ending = "ending" in filename or "10_" in filename or "_10." in filename

    errors = []
    warnings = []

    # === 始终检查的项目 ===

    # 1. XML 格式
    try:
        ET.fromstring(content)
    except ET.ParseError as e:
        errors.append(f"XML 格式错误: {e}")

    # 2. 背景图
    if '<image' not in content:
        warnings.append("缺少背景图")

    # 3. 禁止元素
    forbidden = config.get("rules", {}).get("forbidden_elements", [])
    for elem in forbidden:
        if f"<{elem}" in content:
            errors.append(f"包含禁止元素: <{elem}>")

    # === 校验模式才检查的项目 ===
    if mode == "validate":
        texts = parse_text_elements(content)
        rects = parse_rect_elements(content)
        text_count = len(texts)
        rect_count = len(rects)

        # 4. 内容密度
        density_config = config.get("rules", {}).get("content_density", {})
        min_text = density_config.get("min_text_elements", 6)
        min_rect = density_config.get("min_rect_elements", 2)

        if is_cover or is_ending:
            if text_count < 3:
                errors.append(f"内容严重不足: {text_count} 个文字")
        else:
            if text_count < 5:
                errors.append(f"内容严重不足: {text_count} 个文字")
            elif text_count < min_text:
                warnings.append(f"内容偏少: {text_count} 个文字 (建议 {min_text}+)")

        # 5. 文字溢出（精确检测）
        overflow_issues = check_text_overflow(texts, rects)
        for level, msg in overflow_issues:
            if level == "error":
                errors.append(msg)
            else:
                warnings.append(msg)

        # 6. 文字重叠（精确检测）
        overlap_issues = check_text_overlap(texts)
        for level, msg in overlap_issues:
            errors.append(msg)

        # 7. 垂直覆盖 — 封面/结尾页只有标题和致谢，内容天然少，跳过密度检查
        if not is_cover and not is_ending:
            vc_issues = check_vertical_coverage(texts, rects, config)
            for level, msg in vc_issues:
                errors.append(msg)

        # 8. 空间覆盖 — 同上，封面/结尾页不受空间覆盖率约束
        if not is_cover and not is_ending:
            sc_issues = check_spatial_coverage(texts, rects, config)
            for level, msg in sc_issues:
                if level == "error":
                    errors.append(msg)
                else:
                    warnings.append(msg)

        # 9. 对齐检查
        align_issues = check_alignment(texts, rects)
        for level, msg in align_issues:
            warnings.append(msg)

        # 10. 间距检查
        spacing_issues = check_spacing(texts, rects)
        for level, msg in spacing_issues:
            warnings.append(msg)

        # 11. 元素多样性
        variety_issues = check_element_variety(content, config)
        for level, msg in variety_issues:
            warnings.append(msg)

    # === 输出结果 ===
    print(f"=== {os.path.basename(svg_path)} [{mode}] ===")
    if errors:
        print(f"  [ERROR] ({len(errors)}):")
        for e in errors:
            print(f"    - {e}")
    if warnings:
        print(f"  [WARN] ({len(warnings)}):")
        for w in warnings:
            print(f"    - {w}")
    if not errors and not warnings:
        print(f"  [OK]")

    return 2 if errors else (1 if warnings else 0)


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python check_svg.py <svg_file> [--config <config>] [--mode fast|validate]")
        sys.exit(1)

    svg_path = sys.argv[1]
    config_path = None
    mode = "fast"

    if "--config" in sys.argv:
        idx = sys.argv.index("--config")
        if idx + 1 < len(sys.argv):
            config_path = sys.argv[idx + 1]

    if "--mode" in sys.argv:
        idx = sys.argv.index("--mode")
        if idx + 1 < len(sys.argv):
            mode = sys.argv[idx + 1]

    config = load_config(config_path)
    result = check_svg(svg_path, config, mode)
    sys.exit(result)
