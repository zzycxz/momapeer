// File-type color mapping for the RAG file tree. Gives each common document
// format a subtle, distinct tint (like IDE file icons) so the tree is scannable
// at a glance instead of a wall of identical gray file icons.
//
// Colors are muted so they sit comfortably in both light/dark themes; unknown
// extensions fall back to the tree's default icon color (callers omit a color).

const FILE_TYPE_COLORS: Record<string, string> = {
  // code
  go: "#00ADD8",
  js: "#E8C547",
  jsx: "#61DAFB",
  ts: "#3178C6",
  tsx: "#61DAFB",
  py: "#3776AB",
  rs: "#DEA584",
  sh: "#89E051",
  // markup / data
  md: "#7E8AA2",
  markdown: "#7E8AA2",
  html: "#E44D26",
  css: "#563D7C",
  json: "#A0A0A0",
  yaml: "#CB171E",
  yml: "#CB171E",
  toml: "#9C4221",
  csv: "#2E7D32",
  tsv: "#2E7D32",
  // documents (office)
  pdf: "#D93025",
  doc: "#2B579A",
  docx: "#2B579A",
  xls: "#217346",
  xlsx: "#217346",
  ppt: "#D24726",
  pptx: "#D24726",
  epub: "#81A2BE",
  msg: "#B58900",
};

/** fileIconColor returns a tint for a filename based on its extension, or "" when unknown (caller uses default). */
export function fileIconColor(filename: string): string {
  const dot = filename.lastIndexOf(".");
  if (dot < 0) return "";
  const ext = filename.slice(dot + 1).toLowerCase();
  return FILE_TYPE_COLORS[ext] ?? "";
}
