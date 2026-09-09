import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PreconditionsView } from "./PreconditionsView";
import { clearViewState } from "../lib/viewState";

// PreconditionsView pulls in two queries, three dialog hooks and several modal
// children. These tests only cover the detail header's Jira link, so every
// dependency below is stubbed to the least that gets one precondition on
// screen (mirrors ContainersView.test.tsx).

const browserOpenURL = vi.fn();
const syncPreconditions = vi.fn(async (..._args: unknown[]) => {});
const testDetailProps = vi.fn();

vi.mock("../api", () => ({
  CreatePreconditionDetailed: vi.fn(),
  EditPreconditionField: vi.fn(),
  DeletePrecondition: vi.fn(),
  BulkAssociatePreconditions: vi.fn(),
  SyncPreconditions: (...args: unknown[]) => syncPreconditions(...args),
  BrowserOpenURL: (...args: unknown[]) => browserOpenURL(...args),
  isDemoUrl: (u: string) => u.startsWith("demo:"),
  errMsg: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));

vi.mock("../contexts/ProfileContext", () => ({
  useProfile: () => ({ activeId: "p1" }),
}));

vi.mock("../queries/preconditions", () => ({
  usePreconditions: () => ({
    data: [
      {
        key: "PC-1",
        summary: "SNMP client tools reachable",
        type: "Manual",
        description: "",
        condition: "snmpget -v3 ... $BOARD sysName.0",
        testCount: 2,
      },
    ],
    isLoading: false,
    error: null,
  }),
  usePreconditionTests: () => ({
    data: [{ key: "QA-7", summary: "Login works", status: "Approved" }],
    isLoading: false,
    error: null,
  }),
}));

vi.mock("./AddTestsModal", () => ({ AddTestsModal: () => null }));
vi.mock("./TestDetail", () => ({
  TestDetail: (props: Record<string, unknown>) => {
    testDetailProps(props);
    return null;
  },
}));
vi.mock("./useConfirm", () => ({
  useConfirm: () => ({ confirm: vi.fn(async () => false) }),
}));

const onChanged = vi.fn();

function renderView(jiraUrl: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <PreconditionsView onChanged={onChanged} jiraUrl={jiraUrl} />
    </QueryClientProvider>,
  );
}

async function openPrecondition() {
  // Addressed by the summary: once the detail header renders, its key is a
  // second button carrying "PC-1", so the key alone is ambiguous.
  await userEvent.click(
    screen.getByRole("button", { name: /SNMP client tools reachable/ }),
  );
}

describe("PreconditionsView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    clearViewState("p1");
  });

  it("opens the precondition in Jira from its key", async () => {
    renderView("https://jira.example.com/");
    await openPrecondition();

    // The list row and the detail key both carry the key, so the link is
    // addressed by what it says it does rather than by its text.
    await userEvent.click(
      screen.getByTitle("Open this precondition in Jira (browser)"),
    );
    expect(browserOpenURL).toHaveBeenCalledWith(
      "https://jira.example.com/browse/PC-1",
    );
  });

  it("does not offer the link on a demo profile", async () => {
    renderView("demo://acme");
    await openPrecondition();

    expect(
      screen.queryByTitle("Open this precondition in Jira (browser)"),
    ).toBeNull();
    // The key is still shown, just not as a link.
    expect(screen.getAllByText("PC-1").length).toBeGreaterThan(0);
  });

  it("refreshes only the preconditions from its own Sync button", async () => {
    renderView("https://jira.example.com/");

    await userEvent.click(screen.getByRole("button", { name: "Sync" }));

    // The whole point of the button: one stage, not a full sync.
    expect(syncPreconditions).toHaveBeenCalledWith("p1");
    expect(onChanged).toHaveBeenCalled();
    expect(
      await screen.findByText("Preconditions refreshed from Jira."),
    ).toBeInTheDocument();
  });

  it("hands the profile URL to the test detail it opens", async () => {
    renderView("https://jira.example.com/");
    await openPrecondition();
    await userEvent.click(screen.getByTitle("Open test detail"));

    // Without this the test key in that panel is dead text, since TestDetail
    // builds its own Jira link out of the URL it is given.
    expect(testDetailProps).toHaveBeenCalledWith(
      expect.objectContaining({
        testKey: "QA-7",
        jiraUrl: "https://jira.example.com/",
      }),
    );
  });
});
