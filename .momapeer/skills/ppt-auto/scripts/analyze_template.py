"""
模板分析脚本：导出 PPT 模板每页为 PNG 背景图。
支持 PowerPoint 和 WPS。

用法: python analyze_template.py <template.pptx> <output_dir>

输出:
  output_dir/slide_01.png  (封面)
  output_dir/slide_02.png  (内容页)
  output_dir/slide_03.png  (结尾)
"""
import sys, os


def detect_office_app():
    """检测本机安装的办公软件，返回 COM 对象名称"""
    try:
        import comtypes.client
        # 尝试 PowerPoint
        try:
            app = comtypes.client.CreateObject("PowerPoint.Application")
            app.Quit()
            return "PowerPoint.Application"
        except Exception:
            pass
        # 尝试 WPS (KWPS.Application 或 WPS.Application)
        for name in ["KWPS.Application", "WPS.Application"]:
            try:
                app = comtypes.client.CreateObject(name)
                app.Quit()
                return name
            except Exception:
                pass
        # WPS 兼容模式（注册为 PowerPoint.Application）
        try:
            app = comtypes.client.CreateObject("PowerPoint.Application")
            app.Quit()
            return "PowerPoint.Application"
        except Exception:
            pass
    except ImportError:
        pass
    return None


def analyze_template(template_path, output_dir):
    """分析模板，导出背景图"""
    try:
        import comtypes.client
    except ImportError:
        print("ERROR: comtypes not installed. Run: pip install comtypes")
        sys.exit(1)

    app_name = detect_office_app()
    if not app_name:
        print("ERROR: No office application found. Need PowerPoint or WPS.")
        sys.exit(1)

    print(f"Using: {app_name}")
    os.makedirs(output_dir, exist_ok=True)

    powerpoint = comtypes.client.CreateObject(app_name)
    powerpoint.Visible = 1
    prs = powerpoint.Presentations.Open(os.path.abspath(template_path), WithWindow=False)

    slide_count = prs.Slides.Count
    print(f"Template: {os.path.basename(template_path)} ({slide_count} slides)")

    backgrounds = {}
    for i in range(1, slide_count + 1):
        slide = prs.Slides(i)
        out_path = os.path.join(output_dir, f"slide_{i:02d}.png")
        slide.Export(out_path, 'PNG', 1280, 720)
        print(f"  Slide {i} -> {out_path}")
        backgrounds[i] = out_path

    prs.Close()
    powerpoint.Quit()

    # 映射角色
    if slide_count >= 3:
        result = {
            "cover": backgrounds[1],
            "content": backgrounds[2],
            "ending": backgrounds[slide_count],
            "slide_count": slide_count,
        }
    elif slide_count == 2:
        result = {
            "cover": backgrounds[1],
            "content": backgrounds[2],
            "ending": backgrounds[2],
            "slide_count": slide_count,
        }
    else:
        result = {
            "cover": backgrounds[1],
            "content": backgrounds[1],
            "ending": backgrounds[1],
            "slide_count": slide_count,
        }

    # 复制为标准名称
    import shutil
    for role, src in result.items():
        if role in ("cover", "content", "ending"):
            dst = os.path.join(output_dir, f"bg_{role}.png")
            if src != dst:
                shutil.copy2(src, dst)
                print(f"  {role}: {dst}")

    print(f"\nDone. Backgrounds in {output_dir}")
    return result


if __name__ == "__main__":
    if len(sys.argv) < 3:
        print("Usage: python analyze_template.py <template.pptx> <output_dir>")
        sys.exit(1)
    # Normalize paths for Windows
    template = os.path.normpath(sys.argv[1])
    output = os.path.normpath(sys.argv[2])
    analyze_template(template, output)
