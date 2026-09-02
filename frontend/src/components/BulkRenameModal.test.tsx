import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BulkRenameModal } from "./BulkRenameModal";

const bulkRename = vi.fn();

vi.mock("../api", () => ({
  GetTestSummaries: vi.fn(async () => [
    { key: "QA-1", summary: "Login works" },
    { key: "QA-2", summary: "[SMOKE] Logout works" },
  ]),
  BulkRenameTests: (...args: unknown[]) => bulkRename(...args),
  errMsg: (e: unknown) => String(e),
}));

vi.mock("../contexts/ProfileContext", () => ({
  useProfile: () => ({ activeId: "p1" }),
}));

function renderModal(onComplete = () => {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <BulkRenameModal
        testKeys={["QA-1", "QA-2"]}
        onComplete={onComplete}
        onCancel={() => {}}
      />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  bulkRename.mockReset();
  bulkRename.mockResolvedValue({ succeeded: ["QA-1"], failed: [] });
});

describe("BulkRenameModal", () => {
  it("previews the prefix on each summary as you type", async () => {
    renderModal();
    await screen.findByText("QA-1");

    await userEvent.type(screen.getByRole("textbox", { name: /prefix/i }), "[[SMOKE] ");

    // The after column splits into <mark>affix</mark> + untouched body, so the
    // whole string no longer lives in one node. Assert the mark instead: that
    // the inserted text is visibly marked is the point of the preview.
    await waitFor(() => {
      const marks = document.querySelectorAll("mark.rename-add");
      expect(marks.length).toBeGreaterThan(0);
      expect([...marks].map((m) => m.textContent)).toContain("[SMOKE] ");
    });
    // And the row still reads as the full new summary end to end.
    const row = document.querySelector(".rename-row.rename-changed");
    expect(row?.textContent).toContain("[SMOKE] Login works");
  });

  it("sends only the rows that actually change, with the value the preview used", async () => {
    renderModal();
    await screen.findByText("QA-1");

    await userEvent.type(screen.getByRole("textbox", { name: /prefix/i }), "[[SMOKE] ");
    await userEvent.click(screen.getByRole("button", { name: /^rename/i }));

    await waitFor(() => expect(bulkRename).toHaveBeenCalledTimes(1));
    // QA-2 already carries the prefix, so it must not be in the payload.
    expect(bulkRename).toHaveBeenCalledWith("p1", [
      {
        key: "QA-1",
        summary: "[SMOKE] Login works",
        expectedBefore: "Login works",
      },
    ]);
  });

  it("disables apply until something would change", async () => {
    renderModal();
    await screen.findByText("QA-1");

    expect(screen.getByRole("button", { name: /^rename/i })).toBeDisabled();
  });

  it("stays open and lists failures when some rows are rejected", async () => {
    bulkRename.mockResolvedValue({
      succeeded: [],
      failed: [
        { testKey: "QA-1", error: "summary changed since the preview was taken" },
      ],
    });
    renderModal();
    await screen.findByText("QA-1");

    await userEvent.type(screen.getByRole("textbox", { name: /prefix/i }), "[[SMOKE] ");
    await userEvent.click(screen.getByRole("button", { name: /^rename/i }));

    expect(await screen.findByText(/failed \(1\)/i)).toBeInTheDocument();
    expect(screen.getByText(/changed since the preview/i)).toBeInTheDocument();
  });
});
