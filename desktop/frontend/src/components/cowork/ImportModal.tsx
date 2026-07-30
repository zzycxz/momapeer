import { useState } from "react";
import { FolderUp, FilePlus, Sparkles, Folder, CheckSquare, Square } from "lucide-react";
import { CustomSelect } from "./CustomSelect";
import { app } from "../../lib/bridge";
import { useToast } from "../../lib/toast";
import type { RagCollectionView } from "../../lib/types";

interface ImportModalProps {
  isOpen: boolean;
  onClose: () => void;
  collections: RagCollectionView[];
  defaultCollection?: string;
  onSuccess: (collectionName: string) => void;
}

type ImportType = "folder" | "files";

export function ImportModal({
  isOpen,
  onClose,
  collections,
  defaultCollection = "",
  onSuccess,
}: ImportModalProps) {
  const [importType, setImportType] = useState<ImportType>("folder");
  const [targetCollection, setTargetCollection] = useState(defaultCollection);
  const [autoExtract, setAutoExtract] = useState(false);
  const [loading, setLoading] = useState(false);
  const { showToast } = useToast();

  if (!isOpen) return null;

  const handleExecuteImport = async () => {
    // A specific collection is REQUIRED — "全部/default" is a view scope, not
    // an import target. The import button is disabled when empty, but defend
    // in depth here too.
    const chosenCollection = targetCollection.trim();
    if (!chosenCollection) {
      showToast("请先选择一个目标分类（不能导入到“全部”）", "error");
      return;
    }
    try {
      setLoading(true);

      let paths: string[] = [];
      if (importType === "folder") {
        const folder = await app.PickImportFolder();
        if (folder) paths = [folder];
      } else {
        const files = await app.PickImportFiles();
        if (files && files.length > 0) paths = files;
      }

      if (paths.length === 0) {
        setLoading(false);
        return;
      }

      const res = await app.RagImportPaths(chosenCollection, paths);
      showToast(`成功导入 ${res.files} 个文件到「${chosenCollection}」！FTS5 全文检索已就绪`, "info");

      if (autoExtract) {
        void app.RagStartExtract(chosenCollection, "general/graph", "incremental").then(() => {
          showToast(`已为您自动开启「${chosenCollection}」的深度智能抽取与知识图谱建库！`, "info");
        }).catch((err) => {
          showToast(`抽取提示: ${String(err)}`, "error");
        });
      }

      onSuccess(chosenCollection);
      onClose();
    } catch (e) {
      showToast(String(e), "error");
    } finally {
      setLoading(false);
    }
  };

  // Build options for CustomSelect. Use c.path (full "工作/管理办法") as the
  // value — the backend stores collections by full path, so the leaf name alone
  // would create a duplicate top-level collection instead of importing into the
  // existing nested one.
  const existingPaths = new Set(collections.map(c => c.path || c.name));
  
  const presetOptions = [];
  if (!existingPaths.has("个人")) {
    presetOptions.push({ value: "个人", label: "个人", indent: true, subtitle: "预置二级分类", icon: <Folder size={13} style={{ color: "var(--fg-dim)" }} /> });
  }
  if (!existingPaths.has("工作")) {
    presetOptions.push({ value: "工作", label: "工作", indent: true, subtitle: "预置二级分类", icon: <Folder size={13} style={{ color: "var(--fg-dim)" }} /> });
  }

  const collectionOptions = [
    ...presetOptions,
    ...collections
      // Exclude "default" — it's a view scope (全部), not an import target.
      // Files must go into a real named collection (工作/管理办法, 个人/读书, …).
      .filter((c) => (c.path || c.name) !== "default")
      .map((c) => ({
        value: c.path || c.name,
        label: c.name,
        subtitle: c.documents > 0 ? `${c.documents} 篇` : undefined,
        indent: !!c.parent,
        icon: <Folder size={13} />,
      })),
  ];

  return (
    <div className="rag-create-overlay" onClick={onClose} style={{ zIndex: 9999 }}>
      <div
        className="rag-create-modal"
        onClick={(e) => e.stopPropagation()}
        style={{ width: 440, maxWidth: "90vw", borderRadius: 14, overflow: "hidden" }}
      >
        {/* 头部导航与标题 */}
        <div className="rag-create-modal__head" style={{ borderBottom: "1px solid var(--border-soft)", padding: "14px 18px" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <div style={{ width: 28, height: 28, borderRadius: 8, background: "rgba(59, 130, 246, 0.12)", color: "#3b82f6", display: "flex", alignItems: "center", justifyContent: "center" }}>
              <FolderUp size={16} />
            </div>
            <h3 className="rag-create-modal__title" style={{ fontSize: 15, fontWeight: 600 }}>导入知识库资产</h3>
          </div>
          <button className="rag-create-modal__close" onClick={onClose} style={{ fontSize: 16 }}>✕</button>
        </div>

        {/* 弹窗主体区 */}
        <div className="rag-create-modal__body" style={{ padding: "18px 20px", display: "flex", flexDirection: "column", gap: 18 }}>
          {/* 导入模式大卡片选择 (Source Mode) */}
          <div className="rag-create-modal__section" style={{ gap: 8 }}>
            <label className="rag-create-modal__label" style={{ fontWeight: 600, fontSize: 12, color: "var(--fg)" }}>选择资产导入来源</label>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 10 }}>
              <button
                type="button"
                onClick={() => setImportType("folder")}
                style={{
                  display: "flex",
                  flexDirection: "column",
                  alignItems: "flex-start",
                  gap: 6,
                  padding: "12px 14px",
                  borderRadius: 10,
                  border: importType === "folder" ? "2px solid #3b82f6" : "1px solid var(--border-soft)",
                  background: importType === "folder" ? "rgba(59, 130, 246, 0.05)" : "var(--bg-soft)",
                  cursor: "pointer",
                  transition: "all 0.15s ease",
                  textAlign: "left",
                }}
              >
                <div style={{ display: "flex", alignItems: "center", gap: 6, color: importType === "folder" ? "#3b82f6" : "var(--fg)", fontWeight: 600, fontSize: 13 }}>
                  <FolderUp size={16} />
                  <span>批量导入文件夹</span>
                </div>
                <span style={{ fontSize: 11, color: "var(--fg-faint)", lineHeight: 1.4 }}>
                  自动递归整包导入目录树中的多层级各文档资产
                </span>
              </button>

              <button
                type="button"
                onClick={() => setImportType("files")}
                style={{
                  display: "flex",
                  flexDirection: "column",
                  alignItems: "flex-start",
                  gap: 6,
                  padding: "12px 14px",
                  borderRadius: 10,
                  border: importType === "files" ? "2px solid #a855f7" : "1px solid var(--border-soft)",
                  background: importType === "files" ? "rgba(168, 85, 247, 0.05)" : "var(--bg-soft)",
                  cursor: "pointer",
                  transition: "all 0.15s ease",
                  textAlign: "left",
                }}
              >
                <div style={{ display: "flex", alignItems: "center", gap: 6, color: importType === "files" ? "#a855f7" : "var(--fg)", fontWeight: 600, fontSize: 13 }}>
                  <FilePlus size={16} />
                  <span>添加指定文件</span>
                </div>
                <span style={{ fontSize: 11, color: "var(--fg-faint)", lineHeight: 1.4 }}>
                  精确选择指定的 PDF / Word / Excel / Markdown 文本
                </span>
              </button>
            </div>
          </div>

          {/* 导入目标层级指定 (Target Collection) */}
          <div className="rag-create-modal__section" style={{ gap: 8 }}>
            <label className="rag-create-modal__label" style={{ fontWeight: 600, fontSize: 12, color: "var(--fg)" }}>
              导入至分类层级
              <span style={{ fontWeight: 400, fontSize: 11, color: "var(--fg-faint)", marginLeft: 6 }}>（必选，不能导入到“全部”）</span>
            </label>
            <CustomSelect
              value={targetCollection}
              onChange={setTargetCollection}
              icon={<Folder size={14} style={{ color: "var(--accent)" }} />}
              options={collectionOptions}
            />
            {/* 常用分类快捷选中芯片 */}
            <div style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap", marginTop: 2 }}>
              <span style={{ fontSize: 11, color: "var(--fg-faint)" }}>快速指定：</span>
              {["工作", "个人", "学习材料"].map((tag) => (
                <button
                  key={tag}
                  type="button"
                  onClick={() => setTargetCollection(tag)}
                  style={{
                    padding: "2px 8px",
                    borderRadius: 12,
                    border: "1px solid var(--border-soft)",
                    background: targetCollection === tag ? "var(--accent)" : "transparent",
                    color: targetCollection === tag ? "#fff" : "var(--fg-dim)",
                    fontSize: 11,
                    cursor: "pointer",
                    transition: "all 0.15s",
                  }}
                >
                  {tag}
                </button>
              ))}
            </div>
          </div>

          {/* 智能增强与构建开关 (Advanced Automation) */}
          <div
            onClick={() => setAutoExtract(!autoExtract)}
            style={{
              padding: "12px 14px",
              borderRadius: 10,
              background: autoExtract ? "rgba(234, 179, 8, 0.08)" : "var(--bg-soft)",
              border: autoExtract ? "1px solid rgba(234, 179, 8, 0.4)" : "1px solid var(--border-soft)",
              display: "flex",
              alignItems: "flex-start",
              gap: 10,
              cursor: "pointer",
              transition: "all 0.15s",
            }}
          >
            <div style={{ color: autoExtract ? "#eab308" : "var(--fg-dim)", marginTop: 2 }}>
              {autoExtract ? <CheckSquare size={16} /> : <Square size={16} />}
            </div>
            <div style={{ display: "flex", flexDirection: "column", gap: 3, flex: 1 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 5, fontSize: 12.5, fontWeight: 600, color: autoExtract ? "#ca8a04" : "var(--fg)" }}>
                <Sparkles size={14} style={{ color: "#eab308" }} />
                <span>导入后立即开启智能分析与建库 (自研推荐)</span>
              </div>
              <span style={{ fontSize: 11, color: "var(--fg-faint)", lineHeight: 1.4 }}>
                后台自动调用高精度 LLM 解析全文实体与多层级关系图谱，导入完成直接可查。
              </span>
            </div>
          </div>
        </div>

        {/* 底部功能按钮与提交区 */}
        <div className="rag-create-modal__foot" style={{ borderTop: "1px solid var(--border-soft)", padding: "12px 18px", display: "flex", justifyContent: "flex-end", gap: 10, background: "var(--bg-soft)" }}>
          <button
            type="button"
            className="btn btn--small"
            onClick={onClose}
            disabled={loading}
            style={{ padding: "6px 14px", fontSize: 12 }}
          >
            取消
          </button>
          <button
            type="button"
            className="btn btn--small btn--primary"
            onClick={() => void handleExecuteImport()}
            disabled={loading || !targetCollection.trim()}
            style={{ padding: "6px 16px", fontSize: 12, fontWeight: 500, display: "inline-flex", alignItems: "center", gap: 6 }}
          >
            <FolderUp size={14} />
            <span>{loading ? "正在选择与导入..." : "立即导入资产"}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
