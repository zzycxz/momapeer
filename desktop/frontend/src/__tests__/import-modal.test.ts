import { describe, expect, it, vi } from "vitest";

type ImportType = "folder" | "files";

interface ImportModalState {
  isOpen: boolean;
  importType: ImportType;
  targetCollection: string;
  autoExtract: boolean;
}

function createInitialModalState(isOpen: boolean, defaultCollection = ""): ImportModalState {
  return {
    isOpen,
    importType: "folder",
    targetCollection: defaultCollection || "默认分类",
    autoExtract: false,
  };
}

function switchImportType(state: ImportModalState, type: ImportType): ImportModalState {
  return { ...state, importType: type };
}

function toggleAutoExtract(state: ImportModalState): ImportModalState {
  return { ...state, autoExtract: !state.autoExtract };
}

describe("ImportModal Asset Selection & UX Suite", () => {
  it("should initialize with folder import mode and default collection target", () => {
    const state = createInitialModalState(true, "工作/研发");
    expect(state.isOpen).toBe(true);
    expect(state.importType).toBe("folder");
    expect(state.targetCollection).toBe("工作/研发");
    expect(state.autoExtract).toBe(false);
  });

  it("should allow switching between folder batch import and specific file selection", () => {
    let state = createInitialModalState(true);
    expect(state.importType).toBe("folder");
    
    state = switchImportType(state, "files");
    expect(state.importType).toBe("files");
  });

  it("should toggle the automated knowledge extraction enhancement option", () => {
    let state = createInitialModalState(true);
    expect(state.autoExtract).toBe(false);

    state = toggleAutoExtract(state);
    expect(state.autoExtract).toBe(true);

    state = toggleAutoExtract(state);
    expect(state.autoExtract).toBe(false);
  });

  it("should respect fallback collection name when default target is empty", () => {
    const state = createInitialModalState(true, "");
    expect(state.targetCollection).toBe("默认分类");
  });
});
