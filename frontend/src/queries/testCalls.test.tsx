import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useTestCallLinks } from "./testCalls";
import * as api from "../api";

vi.mock("../api", () => ({
  ListTestCallLinks: vi.fn(),
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

describe("useTestCallLinks", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns links on success", async () => {
    (api.ListTestCallLinks as ReturnType<typeof vi.fn>).mockResolvedValue([
      { callerKey: "PROJ-1", calledKey: "PROJ-2" },
    ]);
    const { result } = renderHook(() => useTestCallLinks("p1", "0:0:0"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.length).toBe(1);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (api.ListTestCallLinks as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("boom"),
    );
    const { result } = renderHook(() => useTestCallLinks("p1", "0:0:0"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a profile id", () => {
    const { result } = renderHook(() => useTestCallLinks("", "0:0:0"), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListTestCallLinks).not.toHaveBeenCalled();
  });

  it("refetches when the bridge string changes (call-count 1 -> 2)", async () => {
    (api.ListTestCallLinks as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    const { result, rerender } = renderHook(
      ({ bridge }: { bridge: string }) => useTestCallLinks("p1", bridge),
      { wrapper: makeWrapper(), initialProps: { bridge: "0:0:0" } },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.ListTestCallLinks).toHaveBeenCalledTimes(1);
    rerender({ bridge: "1:0:0" });
    await waitFor(() => expect(api.ListTestCallLinks).toHaveBeenCalledTimes(2));
  });
});
