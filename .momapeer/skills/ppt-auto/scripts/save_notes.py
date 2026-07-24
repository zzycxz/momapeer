"""
保存演讲者备注到 notes/ 目录。

用法: python save_notes.py <project_dir> <notes_json>

notes_json 格式:
{
  "01_cover": "这是封面的演讲备注...",
  "02_toc": "目录页的备注...",
  ...
}
"""
import json, sys, os


def save_notes(project_dir, notes_data):
    """保存备注文件"""
    notes_dir = os.path.join(project_dir, "notes")
    os.makedirs(notes_dir, exist_ok=True)

    for stem, content in notes_data.items():
        if not content:
            continue
        path = os.path.join(notes_dir, f"{stem}.md")
        with open(path, "w", encoding="utf-8") as f:
            f.write(content)
        print(f"  {stem}.md: {len(content)} chars")

    print(f"\nSaved {len(notes_data)} notes to {notes_dir}")


if __name__ == "__main__":
    if len(sys.argv) < 3:
        print("Usage: python save_notes.py <project_dir> <notes_json>")
        sys.exit(1)

    project_dir = sys.argv[1]
    notes_path = sys.argv[2]

    with open(notes_path, "r", encoding="utf-8") as f:
        notes_data = json.load(f)

    save_notes(project_dir, notes_data)
