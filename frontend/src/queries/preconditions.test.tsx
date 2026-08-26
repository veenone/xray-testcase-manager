import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { usePreconditions } from "./preconditions";
import * as api from "../api";

vi.mock("../api", () => ({
  ListPreconditionsWithUsage: vi.fn(),
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
    const { result } = renderHook(() => usePreconditions("p1", 0), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (
      api.ListPreconditionsWithUsage as ReturnType<typeof vi.fn>
    ).mockRejectedValue(new Error("boom"));
    const { result } = renderHook(() => usePreconditions("p1", 0), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a profile id", () => {
    const { result } = renderHook(() => usePreconditions("", 0), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListPreconditionsWithUsage).not.toHaveBeenCalled();
  });

  it("refetches when the refreshKey bridge changes", async () => {
    (
      api.ListPreconditionsWithUsage as ReturnType<typeof vi.fn>
    ).mockResolvedValue([]);
    const { result, rerender } = renderHook(
      ({ rk }: { rk: number }) => usePreconditions("p1", rk),
      { wrapper: makeWrapper(), initialProps: { rk: 0 } },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.ListPreconditionsWithUsage).toHaveBeenCalledTimes(1);
    rerender({ rk: 1 });
    await waitFor(() =>
      expect(api.ListPreconditionsWithUsage).toHaveBeenCalledTimes(2),
    );
  });
});
