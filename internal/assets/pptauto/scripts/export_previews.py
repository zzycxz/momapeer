"""
导出 PPTX 每页为 PNG 截图。

用法: python export_previews.py <project_dir>

输出: <project_dir>/previews/slide_01.png, slide_02.png, ...

依赖: PowerPoint 或 WPS (COM 自动化)
"""
import os, sys, glob


def find_office_app():
    """检测本机安装的办公软件"""
    try:
        import comtypes.client
        for name in ["PowerPoint.Application", "KWPS.Application"]:
            try:
                app = comtypes.client.CreateObject(name)
                app.Quit()
                return name
            except:
                pass
    except ImportError:
        pass
    return None


def main():
    if len(sys.argv) < 2:
        print("Usage: python export_previews.py <project_dir>")
        sys.exit(1)

    project_dir = sys.argv[1]
    exports_dir = os.path.join(project_dir, "exports")
    previews_dir = os.path.join(project_dir, "previews")
    os.makedirs(previews_dir, exist_ok=True)

    # 找到最新的 PPTX 文件
    pptx_files = sorted(glob.glob(os.path.join(exports_dir, "*.pptx")))
    if not pptx_files:
        print("ERROR: No PPTX files found in exports/")
        sys.exit(1)

    pptx_path = pptx_files[-1]
    print(f"导出截图: {pptx_path}")

    # 检测办公软件
    app_name = find_office_app()
    if not app_name:
        print("ERROR: No office application found")
        sys.exit(1)

    print(f"  使用: {app_name}")

    import comtypes.client
    powerpoint = comtypes.client.CreateObject(app_name)
    powerpoint.Visible = 1
    prs = powerpoint.Presentations.Open(os.path.abspath(pptx_path), WithWindow=False)

    for i in range(1, prs.Slides.Count + 1):
        slide = prs.Slides(i)
        out_path = os.path.join(previews_dir, f"slide_{i:02d}.png")
        slide.Export(out_path, 'PNG', 1280, 720)
        print(f"  Slide {i} -> {out_path}")

    prs.Close()
    powerpoint.Quit()
    print(f"\n导出完成: {previews_dir}")


if __name__ == "__main__":
    main()
