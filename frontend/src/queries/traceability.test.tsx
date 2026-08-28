import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  useTraceabilityStats,
  useTraceabilityReqOptions,
  useRequirementSankey,
  usePlanExecSankey,
  useSubTaskSankey,
  useTraceabilityPlanContainers,
  useTraceabilityExecContainers,
  useTraceabilityExecutions,
  useTraceabilityBugs,
} from "./traceability";
import * as api from "../api";

vi.mock("../api", () => ({
  GetStatistics: vi.fn(),
  ListRequirementsWithCoverage: vi.fn(),
  GetRequirementTraceability: vi.fn(),
  GetTraceabilitySankey: vi.fn(),
  GetSubTaskTraceability: vi.fn(),
  ListContainers: vi.fn(),
  GetExecutionsForPlans: vi.fn(),
  ListBugsWithTests: vi.fn(),
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

describe("useTraceabilityStats", () => {
  beforeEach(() => vi.clearAllMocks());

  it("loads the unfiltered stats", async () => {
    (api.GetStatistics as ReturnType<typeof vi.fn>).mockResolvedValue({
      total: 5,
    });
    const { result } = renderHook(() => useTraceabilityStats("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.GetStatistics).toHaveBeenCalledWith("p1", "", "", "");
  });

  it("does not fetch without a profile", () => {
    const { result } = renderHook(() => useTraceabilityStats(""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetStatistics).not.toHaveBeenCalled();
  });
});

describe("useTraceabilityReqOptions", () => {
  beforeEach(() => vi.clearAllMocks());

  it("loads the requirement options", async () => {
    (
      api.ListRequirementsWithCoverage as ReturnType<typeof vi.fn>
    ).mockResolvedValue([{ key: "REQ-1" }]);
    const { result } = renderHook(() => useTraceabilityReqOptions("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });
});

describe("useRequirementSankey", () => {
  beforeEach(() => vi.clearAllMocks());

  it("passes the requirement selection", async () => {
    (api.GetRequirementTraceability as ReturnType<typeof vi.fn>).mockResolvedValue(
      { nodes: [], links: [] },
    );
    const { result } = renderHook(
      () => useRequirementSankey("p1", ["REQ-1"]),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.GetRequirementTraceability).toHaveBeenCalledWith("p1", ["REQ-1"]);
  });
});

describe("usePlanExecSankey", () => {
  beforeEach(() => vi.clearAllMocks());

  it("passes plan/exec selections and the cross-project flag", async () => {
    (api.GetTraceabilitySankey as ReturnType<typeof vi.fn>).mockResolvedValue({
      nodes: [],
      links: [],
    });
    const { result } = renderHook(
      () => usePlanExecSankey("p1", ["PLAN-1"], ["EXEC-1"], true),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.GetTraceabilitySankey).toHaveBeenCalledWith(
      "p1",
      ["PLAN-1"],
      ["EXEC-1"],
      true,
    );
  });
});

describe("useSubTaskSankey", () => {
  beforeEach(() => vi.clearAllMocks());

  it("passes the parent selection and cross-members flag", async () => {
    (api.GetSubTaskTraceability as ReturnType<typeof vi.fn>).mockResolvedValue({
      nodes: [],
      links: [],
    });
    const { result } = renderHook(
      () => useSubTaskSankey("p1", ["PARENT-1"], false),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.GetSubTaskTraceability).toHaveBeenCalledWith(
      "p1",
      ["PARENT-1"],
      false,
    );
  });
});

describe("useTraceabilityPlanContainers / ExecContainers", () => {
  beforeEach(() => vi.clearAllMocks());

  it("loads test plans and test executions by kind", async () => {
    (api.ListContainers as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "PLAN-1" },
    ]);
    const plans = renderHook(() => useTraceabilityPlanContainers("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(plans.result.current.isSuccess).toBe(true));
    expect(api.ListContainers).toHaveBeenCalledWith("p1", "testplan");

    const execs = renderHook(() => useTraceabilityExecContainers("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(execs.result.current.isSuccess).toBe(true));
    expect(api.ListContainers).toHaveBeenCalledWith("p1", "testexec");
  });
});

describe("useTraceabilityExecutions", () => {
  beforeEach(() => vi.clearAllMocks());

  it("passes the plan selection", async () => {
    (api.GetExecutionsForPlans as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    const { result } = renderHook(
      () => useTraceabilityExecutions("p1", ["PLAN-1"]),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(api.GetExecutionsForPlans).toHaveBeenCalledWith("p1", ["PLAN-1"]);
  });
});

describe("useTraceabilityBugs", () => {
  beforeEach(() => vi.clearAllMocks());

  it("fetches only while the cross-project toggle is on", async () => {
    (api.ListBugsWithTests as ReturnType<typeof vi.fn>).mockResolvedValue([{}]);
    const { result } = renderHook(() => useTraceabilityBugs("p1", true), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("does not fetch when cross-project is off", () => {
    const { result } = renderHook(() => useTraceabilityBugs("p1", false), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListBugsWithTests).not.toHaveBeenCalled();
  });
});
