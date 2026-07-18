"""PDF text extraction combining pdfplumber (tables + text) and PaddleOCR (scanned pages).

Usage:
    python ocr_pdf.py <pdf_path> [--lang ch]

Pipeline per page:
    1. pdfplumber extracts tables (validated) + plain text.
    2. If text < threshold, page is likely scanned → PaddleOCR.
    3. Results merged: [table] blocks + plain text paragraphs.

Table validation (防止误识别):
    - >= 2 rows, >= 2 columns.
    - All rows same column count (±30% tolerance for merged cells).
    - >= 50% cells non-empty.
    - Average cell length < 200 chars.
"""

import json
import os
import sys
import tempfile


def _install(pkg: str):
    import subprocess
    subprocess.check_call(
        [sys.executable, "-m", "pip", "install", pkg,
         "--quiet", "--disable-pip-version-check"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )


def _ensure_pdfplumber():
    try:
        import pdfplumber  # noqa: F401
    except ImportError:
        _install("pdfplumber")


def _ensure_ocr():
    try:
        from paddleocr import PaddleOCR  # noqa: F401
    except ImportError:
        _install("paddleocr")
        _install("paddlepaddle")


def _ensure_fitz():
    try:
        import fitz  # noqa: F401
    except ImportError:
        _install("PyMuPDF")


# ---------------------------------------------------------------------------
# Table validation
# ---------------------------------------------------------------------------

def _is_valid_table(table: list, min_rows: int = 2, min_cols: int = 2) -> bool:
    """Validate that a detected table is likely a real table."""
    if not table or len(table) < min_rows:
        return False

    col_counts = {}
    for row in table:
        n = len(row)
        col_counts[n] = col_counts.get(n, 0) + 1

    most_common_cols = max(col_counts, key=col_counts.get)
    if most_common_cols < min_cols:
        return False

    # Allow up to 30% rows with different column count (merged cells).
    mismatched = sum(v for k, v in col_counts.items() if k != most_common_cols)
    if mismatched > len(table) * 0.3:
        return False

    # At least 50% cells non-empty.
    total, filled = 0, 0
    for row in table:
        for cell in row:
            total += 1
            if cell and str(cell).strip():
                filled += 1
    if total == 0 or filled / total < 0.5:
        return False

    # Average cell length sanity check.
    avg_len = sum(len(str(c or "")) for row in table for c in row) / total
    if avg_len > 200:
        return False

    return True


def _table_to_tsv(table: list) -> str:
    """Convert a table to TSV string."""
    lines = []
    for row in table:
        cells = [str(c or "").replace("\n", " ").replace("\t", " ") for c in row]
        lines.append("\t".join(cells))
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# OCR for scanned pages
# ---------------------------------------------------------------------------

def _ocr_page(pdf_path: str, page_idx: int, ocr_engine) -> str:
    """Render a page to image and run PaddleOCR."""
    import fitz

    doc = fitz.open(pdf_path)
    page = doc[page_idx]
    mat = fitz.Matrix(2, 2)  # 2x for better accuracy
    pix = page.get_pixmap(matrix=mat)
    tmp = tempfile.NamedTemporaryFile(suffix=f"_p{page_idx}.png", delete=False)
    pix.save(tmp.name)
    doc.close()

    try:
        result = ocr_engine.ocr(tmp.name, cls=True)
        lines = []
        if result and result[0]:
            for line in result[0]:
                if isinstance(line, (list, tuple)) and len(line) >= 2:
                    text_info = line[1]
                    if isinstance(text_info, (list, tuple)):
                        lines.append(str(text_info[0]))
                    else:
                        lines.append(str(text_info))
        return "\n".join(lines)
    finally:
        try:
            os.unlink(tmp.name)
        except OSError:
            pass


# ---------------------------------------------------------------------------
# Main pipeline
# ---------------------------------------------------------------------------

_MIN_TEXT = 50  # chars per page; below → likely scanned


def extract_pdf(pdf_path: str, lang: str = "ch") -> str:
    """Extract text from PDF. Tables get [table]...[/table] wrapping."""
    _ensure_pdfplumber()
    import pdfplumber

    all_parts = []
    scanned_indices = []

    with pdfplumber.open(pdf_path) as pdf:
        for i, page in enumerate(pdf.pages):
            # Tables.
            tables = page.extract_tables() or []
            valid_tsvs = []
            for t in tables:
                if _is_valid_table(t):
                    valid_tsvs.append(_table_to_tsv(t))

            # Plain text.
            plain = (page.extract_text() or "").strip()

            # Build page output.
            parts = []
            for tsv in valid_tsvs:
                parts.append(f"[table]\n{tsv}[/table]")
            if plain:
                parts.append(plain)

            page_text = "\n\n".join(parts)

            if len(page_text.strip()) < _MIN_TEXT:
                scanned_indices.append(i)
            elif page_text.strip():
                all_parts.append(page_text.strip())

    # OCR scanned pages.
    if scanned_indices:
        _ensure_ocr()
        _ensure_fitz()
        from paddleocr import PaddleOCR
        ocr = PaddleOCR(use_angle_cls=True, lang=lang, show_log=False)
        for idx in scanned_indices:
            try:
                text = _ocr_page(pdf_path, idx, ocr, lang)
                if text.strip():
                    all_parts.append(f"[page {idx + 1}]\n{text.strip()}")
            except Exception as e:
                all_parts.append(f"[page {idx + 1} OCR error: {e}]")

    return "\n\n".join(all_parts)


def main():
    if len(sys.argv) < 2:
        print(json.dumps({"error": "usage: ocr_pdf.py <pdf_path> [--lang ch]"}))
        sys.exit(1)

    pdf_path = sys.argv[1]
    lang = "ch"
    if "--lang" in sys.argv:
        idx = sys.argv.index("--lang")
        if idx + 1 < len(sys.argv):
            lang = sys.argv[idx + 1]

    if not os.path.isfile(pdf_path):
        print(json.dumps({"error": f"file not found: {pdf_path}"}))
        sys.exit(1)

    try:
        text = extract_pdf(pdf_path, lang)
        print(json.dumps({"text": text}, ensure_ascii=False))
    except Exception as e:
        print(json.dumps({"error": str(e)}, ensure_ascii=False))
        sys.exit(1)


if __name__ == "__main__":
    main()
