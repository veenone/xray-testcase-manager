import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useStatistics } from "./statistics";
import * as api from "../api";

vi.mock("../api", () => ({
  GetStatistics: vi.fn(),
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

describe("useStatistics", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns stats on success", async () => {
    (api.GetStatistics as ReturnType<typeof vi.fn>).mockResolvedValue({
      total: 5,
    });
    const { result } = renderHook(() => useStatistics("p1", "", "", ""), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.total).toBe(5);
  });

  it("surfaces errors", async () => {
    (api.GetStatistics as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("boom"),
    );
    const { result } = renderHook(() => useStatistics("p1", "", "", ""), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without profileId", () => {
    const { result } = renderHook(() => useStatistics("", "", "", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetStatistics).not.toHaveBeenCalled();
  });

  it("re-fetches for a different filter combination (separate key)", async () => {
    (api.GetStatistics as ReturnType<typeof vi.fn>).mockResolvedValue({
      total: 0,
    });
    const { result, rerender } = renderHook(
      ({ folder }: { folder: string }) => useStatistics("p1", folder, "", ""),
      { wrapper: makeWrapper(), initialProps: { folder: "" } },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.GetStatistics).toHaveBeenCalledTimes(1);
    rerender({ folder: "A" });
    await waitFor(() => expect(api.GetStatistics).toHaveBeenCalledTimes(2));
    expect(api.GetStatistics).toHaveBeenLastCalledWith("p1", "A", "", "");
  });
});
