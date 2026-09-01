import React from "react";
import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { SelectionProvider, useSelection } from "./SelectionContext";

function wrapper({ children }: { children: React.ReactNode }) {
  return <SelectionProvider>{children}</SelectionProvider>;
}

describe("useSelection", () => {
  it("throws when used outside a SelectionProvider", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => renderHook(() => useSelection())).toThrow(/SelectionProvider/);
    spy.mockRestore();
  });
});

describe("SelectionProvider", () => {
  it("starts with an empty selection and no open row", () => {
    const { result } = renderHook(() => useSelection(), { wrapper });
    expect(result.current.selectedSet.size).toBe(0);
    expect(result.current.selectedKey).toBeNull();
  });

  it("toggle adds a key, then removes it", () => {
    const { result } = renderHook(() => useSelection(), { wrapper });
    act(() => result.current.toggle("T-1"));
    expect([...result.current.selectedSet]).toEqual(["T-1"]);
    act(() => result.current.toggle("T-1"));
    expect(result.current.selectedSet.size).toBe(0);
  });

  it("togglePage selects the whole page, then clears it when all are selected", () => {
    const { result } = renderHook(() => useSelection(), { wrapper });
    act(() => result.current.togglePage(["A", "B", "C"]));
    expect([...result.current.selectedSet].sort()).toEqual(["A", "B", "C"]);
    // All present → the header checkbox clears them.
    act(() => result.current.togglePage(["A", "B", "C"]));
    expect(result.current.selectedSet.size).toBe(0);
  });

  it("togglePage adds only the missing keys when the page is partially selected", () => {
    const { result } = renderHook(() => useSelection(), { wrapper });
    act(() => result.current.toggle("A"));
    act(() => result.current.togglePage(["A", "B"]));
    // Not all were selected, so it adds the rest rather than clearing.
    expect([...result.current.selectedSet].sort()).toEqual(["A", "B"]);
  });

  it("selectAllMatching replaces the current selection", () => {
    const { result } = renderHook(() => useSelection(), { wrapper });
    act(() => result.current.toggle("old"));
    act(() => result.current.selectAllMatching(["X", "Y", "Z"]));
    expect([...result.current.selectedSet].sort()).toEqual(["X", "Y", "Z"]);
  });

  it("setSelectedKey opens a row and accepts a functional update (the temp→real remap)", () => {
    const { result } = renderHook(() => useSelection(), { wrapper });
    act(() => result.current.setSelectedKey("TMP-1"));
    expect(result.current.selectedKey).toBe("TMP-1");
    act(() =>
      result.current.setSelectedKey((cur) => (cur === "TMP-1" ? "REAL-9" : cur)),
    );
    expect(result.current.selectedKey).toBe("REAL-9");
  });
});
