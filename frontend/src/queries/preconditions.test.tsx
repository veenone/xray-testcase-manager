import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { usePreconditions, usePreconditionTests } from "./preconditions";
import * as api from "../api";

vi.mock("../api", () => ({
  ListPreconditionsWithUsage: vi.fn(),
  ListTestsForPrecondition: vi.fn(),
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

describe("usePreconditions", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the list on success", async () => {
    (
      api.ListPreconditionsWithUsage as ReturnType<typeof vi.fn>
    ).mockResolvedValue([{ key: "PRE-1" }]);
    const { result } = renderHook(() => usePreconditions("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (
      api.ListPreconditionsWithUsage as ReturnType<typeof vi.fn>
    ).mockRejectedValue(new Error("boom"));
    const { result } = renderHook(() => usePreconditions("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a profile id", () => {
    const { result } = renderHook(() => usePreconditions(""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListPreconditionsWithUsage).not.toHaveBeenCalled();
  });
});

describe("usePreconditionTests", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the linked tests on success", async () => {
    (api.ListTestsForPrecondition as ReturnType<typeof vi.fn>).mockResolvedValue(
      [{ key: "PROJ-1" }],
    );
    const { result } = renderHook(
      () => usePreconditionTests("p1", "PRE-1"),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.ListTestsForPrecondition).toHaveBeenCalledWith("p1", "PRE-1");
    expect(result.current.data).toHaveLength(1);
  });

  it("does not fetch without a selected precondition", () => {
    const { result } = renderHook(() => usePreconditionTests("p1", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListTestsForPrecondition).not.toHaveBeenCalled();
  });
});
