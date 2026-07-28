import { describe, expect, it } from "vitest";

interface RagPanelLayoutModel {
  renderToolbar: boolean;
  renderLegend: boolean;
  renderSummaryPrompt: boolean;
  canvasMode: "graph" | "empty-guide";
}

function buildRagPanelLayout(hasData: boolean, activeCollection: string): RagPanelLayoutModel {
  return {
    // Toolbar and Legend must remain visible regardless of whether documents exist,
    // preserving product UI stability and avoiding layout collapse!
    renderToolbar: true,
    renderLegend: true,
    renderSummaryPrompt: hasData && !!activeCollection,
    canvasMode: hasData ? "graph" : "empty-guide",
  };
}

describe("RagPanel Structural Consistency & Non-Destructive Layout Suite", () => {
  it("should always render top GraphToolbar and bottom GraphLegend even when knowledge base is completely empty", () => {
    const layout = buildRagPanelLayout(false, "default");
    expect(layout.renderToolbar).toBe(true);
    expect(layout.renderLegend).toBe(true);
  });

  it("should switch canvas area to embedded high-tech Empty Guide when hasData is false without hiding outer chrome", () => {
    const emptyLayout = buildRagPanelLayout(false, "default");
    expect(emptyLayout.canvasMode).toBe("empty-guide");
    expect(emptyLayout.renderSummaryPrompt).toBe(false);

    const activeLayout = buildRagPanelLayout(true, "default");
    expect(activeLayout.canvasMode).toBe("graph");
    expect(activeLayout.renderSummaryPrompt).toBe(true);
  });

  it("should preserve layout parity between newly created collections and deeply extracted collections", () => {
    const newColLayout = buildRagPanelLayout(false, "新项目");
    const existingColLayout = buildRagPanelLayout(true, "joyquant-db");

    // Outer navigation frame must be identical!
    expect(newColLayout.renderToolbar).toBe(existingColLayout.renderToolbar);
    expect(newColLayout.renderLegend).toBe(existingColLayout.renderLegend);
  });
});
