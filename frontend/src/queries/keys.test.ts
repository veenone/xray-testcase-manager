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
});
