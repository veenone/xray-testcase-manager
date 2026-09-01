import React from "react";
import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { NavProvider, useNav } from "./NavContext";

function wrapper({ children }: { children: React.ReactNode }) {
  return <NavProvider>{children}</NavProvider>;
}

describe("useNav", () => {
  it("throws when used outside a NavProvider", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => renderHook(() => useNav())).toThrow(/NavProvider/);
    spy.mockRestore();
  });
});

describe("NavProvider", () => {
  it("defaults to the browse view, folder grouping, empty selections, panel closed", () => {
    const { result } = renderHook(() => useNav(), { wrapper });
    expect(result.current.view).toBe("browse");
    expect(result.current.groupBy).toBe("folder");
    expect(result.current.selectedFolder).toBe("");
    expect(result.current.selectedContainer).toBe("");
    expect(result.current.selectedComponent).toBe("");
    expect(result.current.showNewTest).toBe(false);
    expect(result.current.newTestFolder).toBe("");
  });

  it("routes to another view and switches the grouping dimension", () => {
    const { result } = renderHook(() => useNav(), { wrapper });
    act(() => result.current.setView("coverage"));
    act(() => result.current.setGroupBy("testplan"));
    expect(result.current.view).toBe("coverage");
    expect(result.current.groupBy).toBe("testplan");
  });

  it("opens the New Test panel seeded with a folder", () => {
    const { result } = renderHook(() => useNav(), { wrapper });
    act(() => {
      result.current.setNewTestFolder("Regression/Login");
      result.current.setShowNewTest(true);
    });
    expect(result.current.showNewTest).toBe(true);
    expect(result.current.newTestFolder).toBe("Regression/Login");
  });
});
