import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useTests } from "./tests";
import * as api from "../api";

vi.mock("../api", () => ({
  ListTests: vi.fn(),
  errMsg: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));

const PARAMS = { search: "", status: "", limit: 100, offset: 0 } as never;

function makeWrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useTests", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the page on success", async () => {
    (api.ListTests as ReturnType<typeof vi.fn>).mockResolvedValue({
      tests: [{ key: "PROJ-1" }],
      total: 1,
    });
    const { result } = renderHook(() => useTests("p1", PARAMS), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.total).toBe(1);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (api.ListTests as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("boom"),
    );
    const { result } = renderHook(() => useTests("p1", PARAMS), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a profile id", () => {
    const { result } = renderHook(() => useTests("", PARAMS), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListTests).not.toHaveBeenCalled();
  });

  it("re-fetches for a different params combination (separate key)", async () => {
    (api.ListTests as ReturnType<typeof vi.fn>).mockResolvedValue({
      tests: [],
      total: 0,
    });
    const { result, rerender } = renderHook(
      ({ p }: { p: never }) => useTests("p1", p),
      { wrapper: makeWrapper(), initialProps: { p: PARAMS } },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.ListTests).toHaveBeenCalledTimes(1);
    rerender({
      p: { search: "", status: "", limit: 100, offset: 100 } as never,
    });
    await waitFor(() => expect(api.ListTests).toHaveBeenCalledTimes(2));
  });
});
