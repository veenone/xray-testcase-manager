import { describe, it, expect, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { invalidateProfileData } from "./invalidate";

describe("invalidateProfileData", () => {
  it("invalidates every refreshKey-bridged family for the profile", () => {
    const qc = new QueryClient();
    const spy = vi.spyOn(qc, "invalidateQueries");
    invalidateProfileData(qc, "p1");

    const invalidated = spy.mock.calls.map(
      (c) => (c[0] as { queryKey: readonly unknown[] }).queryKey,
    );
    const seconds = invalidated.map((k) => k[1]);
    // The 11 list/dashboard families the counter used to bridge.
    expect(new Set(seconds)).toEqual(
      new Set([
        "tests",
        "folders",
        "syncState",
        "components",
        "containers",
        "preconditions",
        "requirements",
        "duplicates",
        "stats",
        "canonicalRequirements",
        "testCalls",
      ]),
    );
    // Every key is scoped to the profile.
    expect(invalidated.every((k) => k[0] === "p1")).toBe(true);
    // It must NOT invalidate the detail queries or the pending journal.
    expect(seconds).not.toContain("test");
    expect(seconds).not.toContain("pending");
  });

  it("is a no-op without a profile", () => {
    const qc = new QueryClient();
    const spy = vi.spyOn(qc, "invalidateQueries");
    invalidateProfileData(qc, "");
    expect(spy).not.toHaveBeenCalled();
  });
});
