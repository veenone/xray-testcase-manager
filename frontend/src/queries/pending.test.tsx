import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { usePendingChanges } from "./pending";
import * as api from "../api";

vi.mock("../api", () => ({
  ListPendingChanges: vi.fn(),
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

describe("usePendingChanges", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the pending changes on success", async () => {
    (api.ListPendingChanges as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: "1" },
    ]);
    const { result } = renderHook(() => usePendingChanges("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (api.ListPendingChanges as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("boom"),
    );
    const { result } = renderHook(() => usePendingChanges("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a profile id", () => {
    const { result } = renderHook(() => usePendingChanges(""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListPendingChanges).not.toHaveBeenCalled();
  });
});
