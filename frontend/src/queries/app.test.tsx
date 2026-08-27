import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  useSyncState,
  useFolders,
  useComponents,
  useGroupContainers,
} from "./app";
import * as api from "../api";

vi.mock("../api", () => ({
  GetSyncState: vi.fn(),
  ListFolders: vi.fn(),
  ListComponents: vi.fn(),
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

describe("useSyncState", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the sync state on success", async () => {
    (api.GetSyncState as ReturnType<typeof vi.fn>).mockResolvedValue({
      profileId: "p1",
      lastSyncedAt: "2026-01-01",
      testCount: 42,
    });
    const { result } = renderHook(() => useSyncState("p1", 0), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.testCount).toBe(42);
  });

  it("does not fetch without a profile", () => {
    const { result } = renderHook(() => useSyncState("", 0), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetSyncState).not.toHaveBeenCalled();
  });

  it("refetches when the refreshKey bridge changes", async () => {
    (api.GetSyncState as ReturnType<typeof vi.fn>).mockResolvedValue({});
    const { result, rerender } = renderHook(
      ({ rk }: { rk: number }) => useSyncState("p1", rk),
      { wrapper: makeWrapper(), initialProps: { rk: 0 } },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.GetSyncState).toHaveBeenCalledTimes(1);
    rerender({ rk: 1 });
    await waitFor(() => expect(api.GetSyncState).toHaveBeenCalledTimes(2));
  });
});

describe("useFolders", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the folder list on success", async () => {
    (api.ListFolders as ReturnType<typeof vi.fn>).mockResolvedValue([
      { path: "A" },
    ]);
    const { result } = renderHook(() => useFolders("p1", 0), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("does not fetch without a profile", () => {
    const { result } = renderHook(() => useFolders("", 0), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListFolders).not.toHaveBeenCalled();
  });
});

describe("useComponents", () => {
  beforeEach(() => vi.clearAllMocks());

  it("fetches only while grouping by component", async () => {
    (api.ListComponents as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "core" },
    ]);
    const { result } = renderHook(
      () => useComponents("p1", "component", 0),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("does not fetch for a non-component grouping", () => {
    const { result } = renderHook(() => useComponents("p1", "folder", 0), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListComponents).not.toHaveBeenCalled();
  });
});

describe("useGroupContainers", () => {
  beforeEach(() => vi.clearAllMocks());

  it("fetches while grouping by testset", async () => {
    (api.ListContainers as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "SET-1" },
    ]);
    const { result } = renderHook(
      () => useGroupContainers("p1", "testset", 0),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.ListContainers).toHaveBeenCalledWith("p1", "testset");
    expect(result.current.data).toHaveLength(1);
  });

  it("does not fetch for a non-container grouping", () => {
    const { result } = renderHook(
      () => useGroupContainers("p1", "component", 0),
      { wrapper: makeWrapper() },
    );
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListContainers).not.toHaveBeenCalled();
  });
});
