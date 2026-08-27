import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useCanonicalRequirements, useCoverageDetail } from "./coverage";
import * as api from "../api";

vi.mock("../api", () => ({
  ListCanonicalRequirements: vi.fn(),
  GetParamModel: vi.fn(),
  GetCoverageReport: vi.fn(),
  ListCoverageGaps: vi.fn(),
  ListCanonicalReuse: vi.fn(),
  DetectStaleCoverageMappings: vi.fn(),
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

describe("useCanonicalRequirements", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the list on success", async () => {
    (
      api.ListCanonicalRequirements as ReturnType<typeof vi.fn>
    ).mockResolvedValue([{ id: "C1", name: "Sign" }]);
    const { result } = renderHook(() => useCanonicalRequirements("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (
      api.ListCanonicalRequirements as ReturnType<typeof vi.fn>
    ).mockRejectedValue(new Error("boom"));
    const { result } = renderHook(() => useCanonicalRequirements("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a profile id", () => {
    const { result } = renderHook(() => useCanonicalRequirements(""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListCanonicalRequirements).not.toHaveBeenCalled();
  });
});

describe("useCoverageDetail", () => {
  beforeEach(() => vi.clearAllMocks());

  it("bundles the five per-selection reads on success", async () => {
    (api.GetParamModel as ReturnType<typeof vi.fn>).mockResolvedValue({ id: "m" });
    (api.GetCoverageReport as ReturnType<typeof vi.fn>).mockResolvedValue({
      total: 3,
    });
    (api.ListCoverageGaps as ReturnType<typeof vi.fn>).mockResolvedValue([{}]);
    (api.ListCanonicalReuse as ReturnType<typeof vi.fn>).mockResolvedValue([{}, {}]);
    (api.DetectStaleCoverageMappings as ReturnType<typeof vi.fn>).mockResolvedValue(
      [],
    );
    const { result } = renderHook(
      () => useCoverageDetail("p1", "C1", "v1"),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.model).toEqual({ id: "m" });
    expect(result.current.data?.report).toEqual({ total: 3 });
    expect(result.current.data?.gaps).toHaveLength(1);
    expect(result.current.data?.reuse).toHaveLength(2);
    expect(result.current.data?.stale).toHaveLength(0);
    expect(api.ListCanonicalReuse).toHaveBeenCalledWith("p1", "C1");
    expect(api.GetParamModel).toHaveBeenCalledWith("p1", "v1");
  });

  it("does not fetch without a selection or version", () => {
    const { result } = renderHook(() => useCoverageDetail("p1", "", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetParamModel).not.toHaveBeenCalled();
  });
});
