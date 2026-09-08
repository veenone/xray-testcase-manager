import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ContainersView } from "./ContainersView";
import type { Capabilities } from "../api";
import { clearViewState } from "../lib/viewState";

// ContainersView pulls a lot in (containers/board/members queries, several
// modal children, three dialog hooks). This test only cares about the "Create
// bug" gate, so every dependency below is stubbed to the minimum shape that
// gets a Test Execution with one FAILed member row on screen.

vi.mock("../api", () => ({
  ListContainers: vi.fn(async () => [
    {
      key: "TE-1",
      kind: "testexec",
      summary: "Nightly run",
      status: "OPEN",
      parentKey: "",
      parentSummary: "",
      issueType: "Test Execution",
      labels: [],
      environments: [],
      fixVersions: [],
      description: "",
    },
  ]),
  GetContainerBoard: vi.fn(async () => ({
    key: "TE-1",
    summary: "Nightly run",
    description: "",
    rows: [
      {
        testKey: "QA-1",
        summary: "Login works",
        status: "Approved",
        runStatus: "FAIL",
        isExternal: false,
      },
    ],
    runCounts: [{ label: "FAIL", count: 1 }],
  })),
  GetExecutionMembersWithRuns: vi.fn(async () => []),
  GetRunRollup: vi.fn(async () => null),
  ListBugsForContainer: vi.fn(async () => []),
  SeedSampleContainers: vi.fn(),
  CleanSampleData: vi.fn(),
  CreateContainerAndAllocate: vi.fn(),
  EditContainer: vi.fn(),
  DeleteContainer: vi.fn(),
  SetContainerEnvironments: vi.fn(),
  DeallocateTests: vi.fn(),
  SetTestRunStatus: vi.fn(),
  BulkSetTestRunStatus: vi.fn(),
  UnlinkBugFromRun: vi.fn(),
  SetTestRunComment: vi.fn(),
  BulkEditContainers: vi.fn(),
  ExportPytest: vi.fn(),
  SyncContainers: vi.fn(),
  BrowserOpenURL: vi.fn(),
  GetRunRollupBreakdown: vi.fn(async () => []),
  errMsg: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));

vi.mock("../contexts/ProfileContext", () => ({
  useProfile: () => ({ activeId: "p1" }),
}));

// The Create/Link bug modals aren't exercised by this test (it only checks
// button presence), and mounting them for real would pull in their own
// query/dialog dependencies for no benefit here.
vi.mock("./CreateBugModal", () => ({
  CreateBugModal: () => null,
}));
vi.mock("./LinkBugPicker", () => ({
  LinkBugPicker: () => null,
}));
vi.mock("./AddTestsModal", () => ({
  AddTestsModal: () => null,
}));
vi.mock("./BugsPanel", () => ({
  BugsPanel: () => null,
}));
vi.mock("./JUnitImportModal", () => ({
  JUnitImportModal: () => null,
}));
vi.mock("./JUnitNewExecModal", () => ({
  JUnitNewExecModal: () => null,
}));
vi.mock("./TestDetail", () => ({
  TestDetail: () => null,
}));

// usePrompt / useConfirm / useNotice wrap DialogContext, which isn't mounted
// in this component-only render (mirrors ProfileForm.test.tsx's useConfirm stub).
vi.mock("./usePrompt", () => ({
  usePrompt: () => ({ prompt: vi.fn(async () => null) }),
}));
vi.mock("./useConfirm", () => ({
  useConfirm: () => ({ confirm: vi.fn(async () => false) }),
}));
vi.mock("./useNotice", () => ({
  useNotice: () => ({ notice: vi.fn(async () => {}) }),
}));

const mockUseCapabilities = vi.fn();
vi.mock("../features", () => ({
  useCapabilities: (...args: unknown[]) => mockUseCapabilities(...args),
}));

// A permissive baseline (mirrors features.ts's defaultCapabilities) so tests
// only need to spell out the two fields under test.
const fullCaps: Capabilities = {
  name: "xray",
  idStyle: "opaque",
  supportsJqlScope: true,
  stepModel: "objects",
  supportsTestTypes: true,
  supportsFolders: true,
  supportsFolderWrites: true,
  supportsPreconditionObjects: true,
  supportsRequirementObjects: true,
  supportsIssueLinkTypes: true,
  supportsEnvironments: true,
  supportsContainers: true,
  containerKinds: ["testset", "testplan", "testexec"],
  supportsTestRuns: true,
  statusModel: "workflow",
  supportsWorkflowTransitions: true,
  supportsBugCreation: true,
  supportsBugLinks: true,
  supportsBugRouting: false,
  supportsTags: false,
};

function renderWithCaps(overrides: Partial<Capabilities>) {
  mockUseCapabilities.mockReturnValue({ ...fullCaps, ...overrides });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <ContainersView onChanged={() => {}} isDemo={false} />
    </QueryClientProvider>,
  );
}

// The board defaults to the Test Plan kind; switch to Test Execution so the
// FAILed member row (and its "Create bug" control) is on screen.
async function selectTestExecutions() {
  const typeSelect = await screen.findByRole("combobox", { name: /type/i });
  await userEvent.selectOptions(typeSelect, "testexec");
}

beforeEach(() => {
  mockUseCapabilities.mockReset();
  // useViewState persists selections in a module-level store keyed by
  // profileId, which otherwise leaks the "testexec" kind chosen by one test
  // into the next (all tests share profileId "p1").
  clearViewState("p1");
});

describe("ContainersView Create bug gate", () => {
  it("offers Create bug when the backend can create one directly (Xray)", async () => {
    renderWithCaps({ supportsBugCreation: true, supportsBugRouting: false });
    await selectTestExecutions();

    expect(
      await screen.findByRole("button", { name: /create bug for this failed test/i }),
    ).toBeInTheDocument();
  });

  it("offers Create bug when the profile routes bugs elsewhere (configured Kiwi)", async () => {
    renderWithCaps({ supportsBugCreation: false, supportsBugRouting: true });
    await selectTestExecutions();

    expect(
      await screen.findByRole("button", { name: /create bug for this failed test/i }),
    ).toBeInTheDocument();
  });

  it("hides Create bug when the backend cannot file one and nothing is routed (unconfigured Kiwi)", async () => {
    renderWithCaps({ supportsBugCreation: false, supportsBugRouting: false });
    await selectTestExecutions();

    // Wait for the FAIL row to render so the query isn't just still loading.
    await screen.findByText("QA-1");
    expect(
      screen.queryByRole("button", { name: /create bug for this failed test/i }),
    ).toBeNull();
  });
});
