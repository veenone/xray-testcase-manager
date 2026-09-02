import { describe, it, expect } from "vitest";
import { computeRenames, renameCounts, SUMMARY_MAX } from "./rename";

const t = (key: string, summary: string) => ({ key, summary });

describe("computeRenames", () => {
  it("leaves everything unchanged when both affixes are empty", () => {
    const rows = computeRenames([t("QA-1", "Login works")], {
      prefix: "",
      suffix: "",
    });
    expect(rows[0].state).toBe("unchanged");
    expect(rows[0].after).toBe("Login works");
  });

  it("adds a prefix literally, with no separator", () => {
    const rows = computeRenames([t("QA-1", "Login works")], {
      prefix: "[SMOKE]",
      suffix: "",
    });
    expect(rows[0].state).toBe("changed");
    expect(rows[0].after).toBe("[SMOKE]Login works");
  });

  it("adds a suffix", () => {
    const rows = computeRenames([t("QA-1", "Login works")], {
      prefix: "",
      suffix: " (v2)",
    });
    expect(rows[0].after).toBe("Login works (v2)");
  });

  it("adds both affixes", () => {
    const rows = computeRenames([t("QA-1", "Login works")], {
      prefix: "[A] ",
      suffix: " [B]",
    });
    expect(rows[0].after).toBe("[A] Login works [B]");
  });

  it("does not re-add a prefix the summary already has", () => {
    const rows = computeRenames([t("QA-1", "[SMOKE] Login works")], {
      prefix: "[SMOKE] ",
      suffix: "",
    });
    expect(rows[0].state).toBe("unchanged");
    expect(rows[0].after).toBe("[SMOKE] Login works");
    expect(rows[0].reason).toMatch(/prefix/i);
  });

  it("does not re-add a suffix the summary already has", () => {
    const rows = computeRenames([t("QA-1", "Login works (v2)")], {
      prefix: "",
      suffix: " (v2)",
    });
    expect(rows[0].state).toBe("unchanged");
    expect(rows[0].reason).toMatch(/suffix/i);
  });

  it("still adds the suffix when only the prefix is already present", () => {
    const rows = computeRenames([t("QA-1", "[A] Login works")], {
      prefix: "[A] ",
      suffix: " [B]",
    });
    expect(rows[0].state).toBe("changed");
    expect(rows[0].after).toBe("[A] Login works [B]");
  });

  it("is case-sensitive, so a different case is a different prefix", () => {
    const rows = computeRenames([t("QA-1", "[smoke] Login")], {
      prefix: "[SMOKE] ",
      suffix: "",
    });
    expect(rows[0].state).toBe("changed");
    expect(rows[0].after).toBe("[SMOKE] [smoke] Login");
  });

  it("accepts a result of exactly the maximum length", () => {
    const before = "x".repeat(SUMMARY_MAX - 3);
    const rows = computeRenames([t("QA-1", before)], {
      prefix: "abc",
      suffix: "",
    });
    expect(rows[0].after).toHaveLength(SUMMARY_MAX);
    expect(rows[0].state).toBe("changed");
  });

  it("flags a result one character over the maximum", () => {
    const before = "x".repeat(SUMMARY_MAX - 3);
    const rows = computeRenames([t("QA-1", before)], {
      prefix: "abcd",
      suffix: "",
    });
    expect(rows[0].state).toBe("too-long");
    // The computed value is kept so the preview can show what would happen.
    expect(rows[0].after).toHaveLength(SUMMARY_MAX + 1);
  });

  it("counts length in characters, not UTF-16 code units", () => {
    // An emoji is two code units but one character to a person and to the limit
    // as users perceive it. Using [...s].length keeps them in step.
    const before = "🙂".repeat(SUMMARY_MAX - 1);
    const rows = computeRenames([t("QA-1", before)], { prefix: "x", suffix: "" });
    expect(rows[0].state).toBe("changed");
  });

  it("blames the summary, not the affix, when it was already over the limit", () => {
    const before = "x".repeat(SUMMARY_MAX + 10);
    const rows = computeRenames([t("QA-1", before)], { prefix: "p", suffix: "" });
    expect(rows[0].state).toBe("too-long");
    expect(rows[0].reason).toMatch(/already over/i);
  });

  it("returns nothing for an empty test list", () => {
    expect(computeRenames([], { prefix: "x", suffix: "" })).toEqual([]);
  });

  it("preserves input order", () => {
    const rows = computeRenames(
      [t("QA-3", "c"), t("QA-1", "a"), t("QA-2", "b")],
      { prefix: "p", suffix: "" },
    );
    expect(rows.map((r) => r.key)).toEqual(["QA-3", "QA-1", "QA-2"]);
  });
});

describe("renameCounts", () => {
  it("tallies each state", () => {
    const rows = computeRenames(
      [
        t("QA-1", "Login works"),
        t("QA-2", "[A] Logout works"),
        t("QA-3", "x".repeat(SUMMARY_MAX)),
      ],
      { prefix: "[A] ", suffix: "" },
    );
    expect(renameCounts(rows)).toEqual({ changed: 1, unchanged: 1, tooLong: 1 });
  });
});
