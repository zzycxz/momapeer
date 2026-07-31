"""
PPTX 转 PDF 导出脚本。

用法: python export_pdf.py <input.pptx> <output.pdf>

支持：
- Microsoft PowerPoint COM 自动化
- LibreOffice 命令行转换（兜底）

依赖：
- Windows: PowerPoint 或 LibreOffice
- Linux/Mac: LibreOffice
"""
import os, sys, subprocess, shutil


def find_libreoffice():
    """查找 LibreOffice 路径"""
    # Windows
    paths = [
        r"C:\Program Files\LibreOffice\program\soffice.exe",
        r"C:\Program Files (x86)\LibreOffice\program\soffice.exe",
    ]
    for p in paths:
        if os.path.exists(p):
            return p
    # Linux/Mac
    for cmd in ["libreoffice", "soffice"]:
        if shutil.which(cmd):
            return cmd
    return None


def export_via_powerpoint(input_pptx, output_pdf):
    """通过 PowerPoint COM 导出 PDF"""
    try:
        import comtypes.client
        powerpoint = comtypes.client.CreateObject("PowerPoint.Application")
        powerpoint.Visible = 1
        prs = powerpoint.Presentations.Open(os.path.abspath(input_pptx), WithWindow=False)
        prs.SaveAs(os.path.abspath(output_pdf), 32)  # 32 = ppSaveAsPDF
        prs.Close()
        powerpoint.Quit()
        return True
    except Exception as e:
        print(f"  PowerPoint 导出失败: {e}")
        return False


def export_via_libreoffice(input_pptx, output_pdf):
    """通过 LibreOffice 导出 PDF"""
    lo_path = find_libreoffice()
    if not lo_path:
        print("  LibreOffice 未找到")
        return False

    try:
        out_dir = os.path.dirname(os.path.abspath(output_pdf))
        cmd = [lo_path, "--headless", "--convert-to", "pdf", "--outdir", out_dir, os.path.abspath(input_pptx)]
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=120)

        # LibreOffice 输出文件名可能不同，重命名
        base = os.path.splitext(os.path.basename(input_pptx))[0]
        lo_output = os.path.join(out_dir, base + ".pdf")
        if os.path.exists(lo_output) and lo_output != os.path.abspath(output_pdf):
            os.rename(lo_output, os.path.abspath(output_pdf))

        if os.path.exists(os.path.abspath(output_pdf)):
            return True
        else:
            print(f"  LibreOffice 导出失败: {result.stderr}")
            return False
    except Exception as e:
        print(f"  LibreOffice 导出失败: {e}")
        return False


def main():
    if len(sys.argv) < 3:
        print("Usage: python export_pdf.py <input.pptx> <output.pdf>")
        sys.exit(1)

    input_pptx = sys.argv[1]
    output_pdf = sys.argv[2]

    if not os.path.exists(input_pptx):
        print(f"ERROR: {input_pptx} not found")
        sys.exit(1)

    print(f"导出 PDF: {input_pptx} -> {output_pdf}")

    # 尝试 PowerPoint
    if export_via_powerpoint(input_pptx, output_pdf):
        size_kb = os.path.getsize(output_pdf) / 1024
        print(f"  成功 (PowerPoint): {size_kb:.0f} KB")
        return

    # 兜底: LibreOffice
    if export_via_libreoffice(input_pptx, output_pdf):
        size_kb = os.path.getsize(output_pdf) / 1024
        print(f"  成功 (LibreOffice): {size_kb:.0f} KB")
        return

    print("  失败: 未找到可用的办公软件")
    sys.exit(1)


if __name__ == "__main__":
    main()
