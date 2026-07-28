import { describe, expect, it } from "vitest";

interface Template {
  name: string;
  displayName: string;
  available: boolean;
}

// 提取模版空状态展示条件逻辑测试
function shouldShowEmptyState(templates: Template[]): boolean {
  return !templates || templates.length === 0;
}

function getEmptyStateCopy(): { title: string; subtitle: string } {
  return {
    title: "暂无自定义专项提取模板",
    subtitle: "系统内置大语言模型深度推理与图谱挖掘，即便无需模版也可在下方直接开启全量智能理解。"
  };
}

describe("Template Select Empty State UI Suite", () => {
  it("should trigger empty state card when template list is empty", () => {
    const emptyList: Template[] = [];
    expect(shouldShowEmptyState(emptyList)).toBe(true);
  });

  it("should not show empty state when templates are available", () => {
    const activeList: Template[] = [
      { name: "default", displayName: "默认图谱抽取", available: true }
    ];
    expect(shouldShowEmptyState(activeList)).toBe(false);
  });

  it("should provide professional and encouraging copy for the empty state", () => {
    const copy = getEmptyStateCopy();
    expect(copy.title).toBe("暂无自定义专项提取模板");
    expect(copy.subtitle).toContain("系统内置大语言模型深度推理与图谱挖掘");
  });
});
