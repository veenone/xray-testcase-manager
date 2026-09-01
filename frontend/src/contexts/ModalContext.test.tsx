import React from "react";
import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { ModalProvider, useModal } from "./ModalContext";

function wrapper({ children }: { children: React.ReactNode }) {
  return <ModalProvider>{children}</ModalProvider>;
}

describe("useModal", () => {
  it("throws when used outside a ModalProvider", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => renderHook(() => useModal())).toThrow(/ModalProvider/);
    spy.mockRestore();
  });
});

describe("ModalProvider", () => {
  it("starts with nothing open", () => {
    const { result } = renderHook(() => useModal(), { wrapper });
    expect(result.current.current).toBeNull();
    expect(result.current.isOpen("form")).toBe(false);
  });

  it("openModal opens exactly one modal", () => {
    const { result } = renderHook(() => useModal(), { wrapper });
    act(() => result.current.openModal("form"));
    expect(result.current.current).toBe("form");
    expect(result.current.isOpen("form")).toBe(true);
    expect(result.current.isOpen("profiles")).toBe(false);
  });

  it("opening a second modal replaces the first (one-at-a-time invariant)", () => {
    const { result } = renderHook(() => useModal(), { wrapper });
    act(() => result.current.openModal("bridge"));
    act(() => result.current.openModal("connections"));
    expect(result.current.current).toBe("connections");
    expect(result.current.isOpen("bridge")).toBe(false);
  });

  it("closeModal clears the open modal", () => {
    const { result } = renderHook(() => useModal(), { wrapper });
    act(() => result.current.openModal("import"));
    act(() => result.current.closeModal());
    expect(result.current.current).toBeNull();
    expect(result.current.isOpen("import")).toBe(false);
  });
});
