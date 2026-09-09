import { describe, it, expect } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useViewState, clearViewState } from "./viewState";

describe("useViewState", () => {
  it("restores a field when the same view is revisited", () => {
    clearViewState("p1");
    const first = renderHook(() =>
      useViewState("p1", "preconditions", "selected", ""),
    );
    act(() => first.result.current[1]("PC-1"));
    first.unmount();

    const second = renderHook(() =>
      useViewState("p1", "preconditions", "selected", ""),
    );
    expect(second.result.current[0]).toBe("PC-1");
  });

  it("drops the previous profile's value when the profile changes", () => {
    clearViewState("p1");
    clearViewState("p2");
    const { result, rerender } = renderHook(
      ({ profileId }) => useViewState(profileId, "preconditions", "selected", ""),
      { initialProps: { profileId: "p1" } },
    );
    act(() => result.current[1]("PC-1"));
    expect(result.current[0]).toBe("PC-1");

    // The component does not remount on a profile switch, so the state has to
    // notice the key changed. Without that the Preconditions detail panel kept
    // showing the previous profile's precondition after switching.
    rerender({ profileId: "p2" });
    expect(result.current[0]).toBe("");
  });

  it("brings back each profile's own value when switching between them", () => {
    clearViewState("p1");
    clearViewState("p2");
    const { result, rerender } = renderHook(
      ({ profileId }) => useViewState(profileId, "preconditions", "selected", ""),
      { initialProps: { profileId: "p1" } },
    );
    act(() => result.current[1]("PC-1"));
    rerender({ profileId: "p2" });
    act(() => result.current[1]("PC-9"));

    rerender({ profileId: "p1" });
    expect(result.current[0]).toBe("PC-1");
    rerender({ profileId: "p2" });
    expect(result.current[0]).toBe("PC-9");
  });
});
