"""Unified document converter using markitdown.

Converts office documents (docx, xlsx, pptx, pdf, html, epub, etc.) to
structured Markdown for better knowledge extraction.

Usage:
    python doc_converter.py <file_path>

Output (JSON to stdout):
    {"text": "...", "title": "...", "format": "markdown"}
    {"error": "..."}
"""

import json
import os
import subprocess
import sys


def _ensure_markitdown():
    """Install markitdown from local directory if not available."""
    try:
        from markitdown import MarkItDown  # noqa: F401
        return True
    except ImportError:
        pass

    # Find local markitdown directory.
    script_dir = os.path.dirname(os.path.abspath(__file__))
    candidates = [
        os.path.join(script_dir, "markitdown", "packages", "markitdown"),
        os.path.join(script_dir, "..", "markitdown", "packages", "markitdown"),
    ]
    for pkg_dir in candidates:
        if os.path.isfile(os.path.join(pkg_dir, "pyproject.toml")):
            try:
                subprocess.check_call(
                    [sys.executable, "-m", "pip", "install",
                     f"{pkg_dir}[pdf,docx,pptx,xlsx]",
                     "--quiet", "--disable-pip-version-check"],
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                )
                return True
            except subprocess.CalledProcessError:
                continue

    # Fallback: try installing from PyPI.
    try:
        subprocess.check_call(
            [sys.executable, "-m", "pip", "install",
             "markitdown[pdf,docx,pptx,xlsx]",
             "--quiet", "--disable-pip-version-check"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        return True
    except subprocess.CalledProcessError:
        return False


def convert_file(path: str) -> dict:
    """Convert a file to Markdown using markitdown."""
    if not os.path.isfile(path):
        return {"error": f"file not found: {path}"}

    if not _ensure_markitdown():
        return {"error": "markitdown install failed"}

    try:
        from markitdown import MarkItDown
        md = MarkItDown()
        result = md.convert(path)
        text = result.markdown or ""
        title = result.title or ""
        return {"text": text, "title": title, "format": "markdown"}
    except Exception as e:
        return {"error": str(e)}


def main():
    if len(sys.argv) < 2:
        print(json.dumps({"error": "usage: doc_converter.py <file_path>"}))
        sys.exit(1)

    path = sys.argv[1]
    result = convert_file(path)
    print(json.dumps(result, ensure_ascii=False))
    sys.exit(0 if "error" not in result else 1)


if __name__ == "__main__":
    main()
