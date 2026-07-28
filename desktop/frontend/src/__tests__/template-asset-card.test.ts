import { describe, expect, it } from "vitest";

interface ExtractResult {
  hasData: boolean;
  entityCount: number;
  relationCount: number;
}

interface AssetCardViewModel {
  title: string;
  entityBadge: string;
  relationBadge: string;
  primaryAction: { label: string; isPrimary: boolean };
  secondaryAction: { label: string; isDanger: boolean };
}

function buildAssetCardModel(result: ExtractResult | null, loading: boolean, showResult: boolean): AssetCardViewModel | null {
  if (loading || !result || !result.hasData || showResult) {
    return null;
  }
  return {
    title: "已提取知识",
    entityBadge: `${result.entityCount} 实体`,
    relationBadge: `${result.relationCount} 关系`,
    primaryAction: { label: "查看详情", isPrimary: true },
    secondaryAction: { label: "清理", isDanger: true },
  };
}

describe("Template Asset Card UI Suite", () => {
  const mockResult: ExtractResult = {
    hasData: true,
    entityCount: 5,
    relationCount: 3,
  };

  it("should generate a structured layered card model when knowledge exists", () => {
    const model = buildAssetCardModel(mockResult, false, false);
    expect(model).not.toBeNull();
    expect(model?.title).toBe("已提取知识");
  });

  it("should split entity and relation counts into independent badge labels", () => {
    const model = buildAssetCardModel(mockResult, false, false);
    expect(model?.entityBadge).toBe("5 实体");
    expect(model?.relationBadge).toBe("3 关系");
  });

  it("should decouple primary viewing action from secondary cleanup action", () => {
    const model = buildAssetCardModel(mockResult, false, false);
    expect(model?.primaryAction.label).toBe("查看详情");
    expect(model?.primaryAction.isPrimary).toBe(true);
    expect(model?.secondaryAction.label).toBe("清理");
    expect(model?.secondaryAction.isDanger).toBe(true);
  });

  it("should hide card when currently loading or result view is already open", () => {
    expect(buildAssetCardModel(mockResult, true, false)).toBeNull();
    expect(buildAssetCardModel(mockResult, false, true)).toBeNull();
  });
});
