import { describe, it, expect } from "vitest";
import { ApiError, normalizeError } from "./apiError";

describe("normalizeError", () => {
  it("wraps a plain Error into an ApiError preserving the message", () => {
    const out = normalizeError(new Error("boom"));
    expect(out).toBeInstanceOf(ApiError);
    expect(out.message).toBe("boom");
  });

  it("returns the same ApiError unchanged", () => {
    const original = new ApiError("already");
    expect(normalizeError(original)).toBe(original);
  });

  it("always yields a non-empty message", () => {
    expect(normalizeError(undefined).message.length).toBeGreaterThan(0);
  });
});
