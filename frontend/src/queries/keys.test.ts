import { describe, it, expect } from "vitest";
import { keys } from "./keys";

describe("query keys", () => {
  it("prefixes every key with the profile id", () => {
    expect(keys.pending("p1")[0]).toBe("p1");
    expect(keys.tests("p1", {})[0]).toBe("p1");
    expect(keys.test("p1", "PROJ-1")[0]).toBe("p1");
    expect(keys.folders("p1")[0]).toBe("p1");
  });

  it("distinguishes entities by their second segment", () => {
    expect(keys.pending("p1")[1]).toBe("pending");
    expect(keys.tests("p1", {})[1]).toBe("tests");
  });

  it("keys the detail sub-reads by section under a stable test prefix", () => {
    // Every Test detail key shares the [profileId, "test", key] prefix, so one
    // invalidateQueries on `test` refetches the base read plus every section.
    const base = keys.test("p1", "PROJ-1");
    const m = keys.testMeta("p1", "PROJ-1");
    expect(m.slice(0, 3)).toEqual(base);
    expect(m[3]).toBe("meta");
    const rh = keys.testRunHistory("p1", "PROJ-1");
    expect(rh.slice(0, 3)).toEqual(base);
    expect(rh[3]).toBe("runHistory");
    // No trailing reload counter — keys are stable (Phase 4a).
    expect(m).toHaveLength(4);
    expect(keys.testReview("p1", "PROJ-1")).toHaveLength(4);
  });
});
