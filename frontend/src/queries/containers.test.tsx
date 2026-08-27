import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useContainers } from "./containers";
import * as api from "../api";

vi.mock("../api", () => ({
  ListContainers: vi.fn(),
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

describe("useContainers", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the list on success", async () => {
    (api.ListContainers as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "PLAN-1" },
    ]);
    const { result } = renderHook(() => useContainers("p1", "testplan", 0), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (api.ListContainers as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("boom"),
    );
    const { result } = renderHook(() => useContainers("p1", "testplan", 0), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a profile id", () => {
    const { result } = renderHook(() => useContainers("", "testplan", 0), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListContainers).not.toHaveBeenCalled();
  });

  it("refetches when the refreshKey bridge changes", async () => {
    (api.ListContainers as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    const { result, rerender } = renderHook(
      ({ rk }: { rk: number }) => useContainers("p1", "testplan", rk),
      { wrapper: makeWrapper(), initialProps: { rk: 0 } },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.ListContainers).toHaveBeenCalledTimes(1);
    rerender({ rk: 1 });
    await waitFor(() =>
      expect(api.ListContainers).toHaveBeenCalledTimes(2),
    );
  });
});
