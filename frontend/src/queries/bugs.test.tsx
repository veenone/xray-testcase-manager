import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useBugs, useBugTests } from "./bugs";
import * as api from "../api";

vi.mock("../api", () => ({
  ListBugsWithTests: vi.fn(),
  ListTestsForBug: vi.fn(),
  errMsg: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));

function makeWrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useBugs", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the bug list on success", async () => {
    (api.ListBugsWithTests as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "BUG-1", testKeys: [] },
    ]);
    const { result } = renderHook(() => useBugs("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (api.ListBugsWithTests as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("boom"),
    );
    const { result } = renderHook(() => useBugs("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a profile", () => {
    const { result } = renderHook(() => useBugs(""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListBugsWithTests).not.toHaveBeenCalled();
  });
});

describe("useBugTests", () => {
  beforeEach(() => vi.clearAllMocks());

  it("loads the tests linked to the selected bug", async () => {
    (api.ListTestsForBug as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "PROJ-1" },
    ]);
    const { result } = renderHook(() => useBugTests("p1", "BUG-1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.ListTestsForBug).toHaveBeenCalledWith("p1", "BUG-1");
    expect(result.current.data).toHaveLength(1);
  });

  it("does not fetch without a selected bug", () => {
    const { result } = renderHook(() => useBugTests("p1", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListTestsForBug).not.toHaveBeenCalled();
  });
});
