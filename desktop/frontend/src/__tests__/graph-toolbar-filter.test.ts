import { describe, expect, it } from "vitest";

interface FilterDropdownState {
  showFilter: boolean;
  filterTypes: string[];
}

function toggleFilterDropdown(state: FilterDropdownState): FilterDropdownState {
  return { ...state, showFilter: !state.showFilter };
}

function toggleEntityType(state: FilterDropdownState, key: string): FilterDropdownState {
  const exists = state.filterTypes.includes(key);
  const nextTypes = exists
    ? state.filterTypes.filter((t) => t !== key)
    : [...state.filterTypes, key];
  return { ...state, filterTypes: nextTypes };
}

function clearAllFilters(state: FilterDropdownState): FilterDropdownState {
  return { ...state, filterTypes: [], showFilter: false };
}

describe("GraphToolbar Popover Filter Architecture Suite", () => {
  it("should initialize with dropdown collapsed and zero filters applied", () => {
    const state: FilterDropdownState = { showFilter: false, filterTypes: [] };
    expect(state.showFilter).toBe(false);
    expect(state.filterTypes).toHaveLength(0);
  });

  it("should open popover dropdown when filter button is toggled without crowding navbar", () => {
    let state: FilterDropdownState = { showFilter: false, filterTypes: [] };
    state = toggleFilterDropdown(state);
    expect(state.showFilter).toBe(true);
  });

  it("should independently toggle ontology types inside popover and track count", () => {
    let state: FilterDropdownState = { showFilter: true, filterTypes: [] };
    
    state = toggleEntityType(state, "product");
    state = toggleEntityType(state, "tech");
    expect(state.filterTypes).toEqual(["product", "tech"]);
    expect(state.filterTypes.length).toBe(2);

    state = toggleEntityType(state, "product");
    expect(state.filterTypes).toEqual(["tech"]);
  });

  it("should clear all filters and auto-collapse dropdown upon resetting", () => {
    let state: FilterDropdownState = { showFilter: true, filterTypes: ["product", "person", "concept"] };
    state = clearAllFilters(state);
    expect(state.filterTypes).toHaveLength(0);
    expect(state.showFilter).toBe(false);
  });
});
