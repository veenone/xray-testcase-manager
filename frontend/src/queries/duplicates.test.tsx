import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useDuplicates } from "./duplicates";
import * as api from "../api";

vi.mock("../api", () => ({
  ScanDuplicates: vi.fn(),
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

describe("useDuplicates", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the report on success", async () => {
    (api.ScanDuplicates as ReturnType<typeof vi.fn>).mockResolvedValue({
      groupCount: 2,
      groups: [],
    });
    const { result } = renderHook(() => useDuplicates("p1", "tests", 0), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.groupCount).toBe(2);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (api.ScanDuplicates as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("boom"),
    );
    const { result } = renderHook(() => useDuplicates("p1", "tests", 0), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch when mode is not tests", () => {
    const { result } = renderHook(
      () => useDuplicates("p1", "preconditions", 0),
      { wrapper: makeWrapper() },
    );
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ScanDuplicates).not.toHaveBeenCalled();
  });

  it("refetches when the refreshKey bridge changes (same mode, new key)", async () => {
    (api.ScanDuplicates as ReturnType<typeof vi.fn>).mockResolvedValue({
      groupCount: 0,
      groups: [],
    });
    const { result, rerender } = renderHook(
      ({ rk }: { rk: number }) => useDuplicates("p1", "tests", rk),
      { wrapper: makeWrapper(), initialProps: { rk: 0 } },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.ScanDuplicates).toHaveBeenCalledTimes(1);
    rerender({ rk: 1 });
    await waitFor(() => expect(api.ScanDuplicates).toHaveBeenCalledTimes(2));
  });
});
