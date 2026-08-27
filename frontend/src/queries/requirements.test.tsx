import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  useRequirements,
  useRequirementTests,
  useRequirementLinks,
} from "./requirements";
import * as api from "../api";

vi.mock("../api", () => ({
  ListRequirementsWithCoverage: vi.fn(),
  ListTestsForRequirement: vi.fn(),
  GetRequirementLinks: vi.fn(),
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

describe("useRequirements", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the list on success", async () => {
    (
      api.ListRequirementsWithCoverage as ReturnType<typeof vi.fn>
    ).mockResolvedValue([{ key: "REQ-1" }]);
    const { result } = renderHook(() => useRequirements("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([{ key: "REQ-1" }]);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (
      api.ListRequirementsWithCoverage as ReturnType<typeof vi.fn>
    ).mockRejectedValue(new Error("boom"));
    const { result } = renderHook(() => useRequirements("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a profile id", () => {
    const { result } = renderHook(() => useRequirements(""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListRequirementsWithCoverage).not.toHaveBeenCalled();
  });
});

describe("useRequirementTests", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the covering tests on success", async () => {
    (api.ListTestsForRequirement as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "PROJ-1" },
    ]);
    const { result } = renderHook(
      () => useRequirementTests("p1", "REQ-1"),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.ListTestsForRequirement).toHaveBeenCalledWith("p1", "REQ-1");
    expect(result.current.data).toHaveLength(1);
  });

  it("does not fetch without a selected requirement", () => {
    const { result } = renderHook(() => useRequirementTests("p1", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListTestsForRequirement).not.toHaveBeenCalled();
  });
});

describe("useRequirementLinks", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the requirement links on success", async () => {
    (api.GetRequirementLinks as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "REQ-2" },
    ]);
    const { result } = renderHook(
      () => useRequirementLinks("p1", "REQ-1"),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.GetRequirementLinks).toHaveBeenCalledWith("p1", "REQ-1");
    expect(result.current.data).toHaveLength(1);
  });

  it("does not fetch without a selected requirement", () => {
    const { result } = renderHook(() => useRequirementLinks("p1", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetRequirementLinks).not.toHaveBeenCalled();
  });
});
