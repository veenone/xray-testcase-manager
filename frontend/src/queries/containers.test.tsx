import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  useContainers,
  useContainerBoard,
  useContainerBugs,
  useContainerMembers,
  useContainerRollup,
} from "./containers";
import * as api from "../api";

vi.mock("../api", () => ({
  ListContainers: vi.fn(),
  GetContainerBoard: vi.fn(),
  ListBugsForContainer: vi.fn(),
  GetExecutionMembersWithRuns: vi.fn(),
  GetRunRollup: vi.fn(),
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
    const { result } = renderHook(() => useContainers("p1", "testplan"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (api.ListContainers as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("boom"),
    );
    const { result } = renderHook(() => useContainers("p1", "testplan"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a profile id", () => {
    const { result } = renderHook(() => useContainers("", "testplan"), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListContainers).not.toHaveBeenCalled();
  });

  it("refetches when the container kind changes", async () => {
    (api.ListContainers as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    const { result, rerender } = renderHook(
      ({ kind }: { kind: string }) => useContainers("p1", kind),
      { wrapper: makeWrapper(), initialProps: { kind: "testset" } },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.ListContainers).toHaveBeenCalledTimes(1);
    rerender({ kind: "testplan" });
    await waitFor(() =>
      expect(api.ListContainers).toHaveBeenCalledTimes(2),
    );
    expect(api.ListContainers).toHaveBeenLastCalledWith("p1", "testplan");
  });
});

describe("useContainerBoard / Bugs", () => {
  beforeEach(() => vi.clearAllMocks());

  it("loads the board and related bugs for a selection", async () => {
    (api.GetContainerBoard as ReturnType<typeof vi.fn>).mockResolvedValue({
      rows: [],
    });
    (api.ListBugsForContainer as ReturnType<typeof vi.fn>).mockResolvedValue([
      {},
    ]);
    const board = renderHook(() => useContainerBoard("p1", "SET-1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(board.result.current.isSuccess).toBe(true));
    expect(api.GetContainerBoard).toHaveBeenCalledWith("p1", "SET-1");
    const bugs = renderHook(() => useContainerBugs("p1", "SET-1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(bugs.result.current.isSuccess).toBe(true));
    expect(bugs.result.current.data).toHaveLength(1);
  });

  it("does not fetch without a selection", () => {
    const { result } = renderHook(() => useContainerBoard("p1", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetContainerBoard).not.toHaveBeenCalled();
  });
});

describe("useContainerMembers / Rollup kind gating", () => {
  beforeEach(() => vi.clearAllMocks());

  it("fetches members only for testexec", async () => {
    (
      api.GetExecutionMembersWithRuns as ReturnType<typeof vi.fn>
    ).mockResolvedValue([]);
    const exec = renderHook(
      () => useContainerMembers("p1", "EXEC-1", "testexec"),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(exec.result.current.isSuccess).toBe(true));

    const plan = renderHook(
      () => useContainerMembers("p1", "PLAN-1", "testplan"),
      { wrapper: makeWrapper() },
    );
    expect(plan.result.current.fetchStatus).toBe("idle");
  });

  it("fetches rollup only for non-testexec", async () => {
    (api.GetRunRollup as ReturnType<typeof vi.fn>).mockResolvedValue({});
    const plan = renderHook(
      () => useContainerRollup("p1", "PLAN-1", "testplan"),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(plan.result.current.isSuccess).toBe(true));

    const exec = renderHook(
      () => useContainerRollup("p1", "EXEC-1", "testexec"),
      { wrapper: makeWrapper() },
    );
    expect(exec.result.current.fetchStatus).toBe("idle");
  });
});
