import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  useTest,
  useTestBugs,
  useTestContainers,
  useTestMeta,
  useTestPreconditions,
  useTestRequirements,
  useTestReview,
  useTestRunHistory,
  useAllPreconditions,
  useRequirementCoverage,
} from "./testDetail";
import * as api from "../api";

vi.mock("../api", () => ({
  GetTest: vi.fn(),
  GetTestBugs: vi.fn(),
  GetTestContainers: vi.fn(),
  GetTestMeta: vi.fn(),
  GetTestPreconditions: vi.fn(),
  GetTestRequirements: vi.fn(),
  GetTestReview: vi.fn(),
  GetTestRunHistory: vi.fn(),
  ListAllPreconditions: vi.fn(),
  ListRequirementsWithCoverage: vi.fn(),
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

describe("useTest", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the test on success", async () => {
    (api.GetTest as ReturnType<typeof vi.fn>).mockResolvedValue({
      key: "PROJ-1",
      summary: "Login works",
    });
    const { result } = renderHook(() => useTest("p1", "PROJ-1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.summary).toBe("Login works");
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (api.GetTest as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("boom"),
    );
    const { result } = renderHook(() => useTest("p1", "PROJ-1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a test key", () => {
    const { result } = renderHook(() => useTest("p1", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetTest).not.toHaveBeenCalled();
  });
});

describe("useTestBugs", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the linked bugs on success", async () => {
    (api.GetTestBugs as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "BUG-1" },
    ]);
    const { result } = renderHook(() => useTestBugs("p1", "PROJ-1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("does not fetch for a not-yet-created NEW- test", () => {
    const { result } = renderHook(() => useTestBugs("p1", "NEW-3"), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetTestBugs).not.toHaveBeenCalled();
  });

  it("does not fetch without a test key", () => {
    const { result } = renderHook(() => useTestBugs("p1", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetTestBugs).not.toHaveBeenCalled();
  });
});

describe("useTestRequirements", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the covered requirements on success", async () => {
    (api.GetTestRequirements as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "REQ-1" },
    ]);
    const { result } = renderHook(
      () => useTestRequirements("p1", "PROJ-1"),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("does not fetch without a test key", () => {
    const { result } = renderHook(() => useTestRequirements("p1", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetTestRequirements).not.toHaveBeenCalled();
  });
});

describe("useTestPreconditions", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the linked preconditions on success", async () => {
    (api.GetTestPreconditions as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "PRE-1" },
    ]);
    const { result } = renderHook(
      () => useTestPreconditions("p1", "PROJ-1"),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("uses the cached read (forceRefresh=false)", async () => {
    (api.GetTestPreconditions as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    renderHook(() => useTestPreconditions("p1", "PROJ-1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() =>
      expect(api.GetTestPreconditions).toHaveBeenCalledWith("p1", "PROJ-1", false),
    );
  });

  it("does not fetch without a test key", () => {
    const { result } = renderHook(
      () => useTestPreconditions("p1", ""),
      { wrapper: makeWrapper() },
    );
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetTestPreconditions).not.toHaveBeenCalled();
  });
});

describe("useAllPreconditions", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the precondition pool on success", async () => {
    (api.ListAllPreconditions as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "PRE-1" },
      { key: "PRE-2" },
    ]);
    const { result } = renderHook(() => useAllPreconditions("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(2);
  });

  it("does not fetch without a profile", () => {
    const { result } = renderHook(() => useAllPreconditions(""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListAllPreconditions).not.toHaveBeenCalled();
  });
});

describe("useTestContainers", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the membership list on success", async () => {
    (api.GetTestContainers as ReturnType<typeof vi.fn>).mockResolvedValue([
      { key: "SET-1", kind: "testset" },
    ]);
    const { result } = renderHook(
      () => useTestContainers("p1", "PROJ-1"),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("does not fetch without a test key", () => {
    const { result } = renderHook(() => useTestContainers("p1", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetTestContainers).not.toHaveBeenCalled();
  });
});

describe("useTestReview", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the review verdict on success", async () => {
    (api.GetTestReview as ReturnType<typeof vi.fn>).mockResolvedValue({
      verdict: "approved",
      note: "looks good",
    });
    const { result } = renderHook(() => useTestReview("p1", "PROJ-1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.verdict).toBe("approved");
  });

  it("treats a null review (unreviewed) as success, not error", async () => {
    (api.GetTestReview as ReturnType<typeof vi.fn>).mockResolvedValue(null);
    const { result } = renderHook(() => useTestReview("p1", "PROJ-1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toBeNull();
  });

  it("does not fetch without a test key", () => {
    const { result } = renderHook(() => useTestReview("p1", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetTestReview).not.toHaveBeenCalled();
  });
});

describe("useRequirementCoverage", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the coverage list on success", async () => {
    (
      api.ListRequirementsWithCoverage as ReturnType<typeof vi.fn>
    ).mockResolvedValue([{ key: "REQ-1", covered: true }]);
    const { result } = renderHook(() => useRequirementCoverage("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("does not fetch without a profile", () => {
    const { result } = renderHook(() => useRequirementCoverage(""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListRequirementsWithCoverage).not.toHaveBeenCalled();
  });
});

describe("useTestMeta", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the meta on success", async () => {
    (api.GetTestMeta as ReturnType<typeof vi.fn>).mockResolvedValue({
      created: "2026-01-01",
    });
    const { result } = renderHook(() => useTestMeta("p1", "PROJ-1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.created).toBe("2026-01-01");
  });

  it("does not fetch without a test key", () => {
    const { result } = renderHook(() => useTestMeta("p1", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetTestMeta).not.toHaveBeenCalled();
  });
});

describe("useTestRunHistory", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the run history on success", async () => {
    (api.GetTestRunHistory as ReturnType<typeof vi.fn>).mockResolvedValue([
      { execKey: "TE-1" },
    ]);
    const { result } = renderHook(
      () => useTestRunHistory("p1", "PROJ-1"),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (api.GetTestRunHistory as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("boom"),
    );
    const { result } = renderHook(
      () => useTestRunHistory("p1", "PROJ-1"),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a test key", () => {
    const { result } = renderHook(() => useTestRunHistory("p1", ""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.GetTestRunHistory).not.toHaveBeenCalled();
  });
});
