import { describe, it, expect } from "vitest";
import { keyCompare } from "../sort";

describe("test runner smoke", () => {
  it("runs and evaluates assertions", () => {
    expect(1 + 1).toBe(2);
  });

  it("can import project source modules", () => {
    // keyCompare sorts Jira keys numerically on the trailing number.
    expect(keyCompare("PROJ-2", "PROJ-10")).toBeLessThan(0);
  });
});
