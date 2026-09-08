import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClientProvider } from "@tanstack/react-query";
import { DialogProvider, ProfileProvider, createQueryClient, useProfile } from "@agile-suite/core";
import * as api from "../api";
import { profileBackend } from "../profileBackend";
import { NewIssueModal } from "./NewIssueModal";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    ListProfiles: vi.fn(),
    GetSettings: vi.fn(),
    SetTheme: vi.fn(),
    SetDefaultProfile: vi.fn(),
    GetCreateFields: vi.fn(),
    ListEpics: vi.fn(),
    SearchUsers: vi.fn(),
    ListPriorities: vi.fn(),
    GetSubtaskTypeName: vi.fn(),
    CreateIssue: vi.fn(),
  };
});

function Loader() {
  const { reload } = useProfile<api.Profile, api.Settings>();
  React.useEffect(() => { void reload(); }, [reload]);
  return null;
}

function renderModal(
  onCreated = vi.fn(),
  onClose = vi.fn(),
  initialType?: api.IssueType,
  lockType?: boolean,
  parentKey?: string,
) {
  render(
    <QueryClientProvider client={createQueryClient()}>
      <DialogProvider>
        <ProfileProvider backend={profileBackend}>
          <Loader />
          <NewIssueModal
            onClose={onClose}
            onCreated={onCreated}
            initialType={initialType}
            lockType={lockType}
            parentKey={parentKey}
          />
        </ProfileProvider>
      </DialogProvider>
    </QueryClientProvider>,
  );
  return { onCreated, onClose };
}

// The submit button reads "Checking Jira" and stays disabled until the
// required-field query settles, so every test waits for it rather than
// racing it.
async function submitButton(scope: HTMLElement) {
  return waitFor(() => {
    const btn = within(scope).getByRole("button", { name: "Create draft" });
    expect(btn).toBeEnabled();
    return btn;
  });
}

const epic = (key: string, summary: string): api.Issue => ({
  key, id: key, project: "PLAT", type: "epic", summary, status: "In Progress", assignee: "",
  reporter: "", priority: "", labels: [], sprintId: "", sprintName: "", parentKey: "",
  storyPoints: null, rank: "", created: "", updated: "",
});

// The draft every test expects, minus whatever that test changes.
const baseDraft = {
  type: "task", summary: "", description: "", priority: "", labels: [] as string[],
  assignee: "", storyPoints: null as number | null, parentKey: "", extra: {},
};

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.ListProfiles).mockResolvedValue([
    { id: "p1", name: "Acme", jiraUrl: "demo", projectKey: "PLAT", backend: "jira", createdAt: "" },
  ]);
  vi.mocked(api.GetSettings).mockResolvedValue({ defaultProfileId: "p1", theme: "light" });
  vi.mocked(api.GetCreateFields).mockImplementation(async (_p, type) =>
    type === "bug"
      ? [{ id: "customfield_10050", name: "Severity", type: "option", required: true, allowedValues: [{ id: "1", value: "Minor" }, { id: "3", value: "Critical" }] }]
      : [],
  );
  vi.mocked(api.ListEpics).mockResolvedValue([
    epic("PLAT-350", "Promotions and discounts"),
    epic("PLAT-360", "Checkout revamp"),
  ]);
  vi.mocked(api.SearchUsers).mockResolvedValue([
    { name: "mortiz", displayName: "M. Ortiz" },
    { name: "ranand", displayName: "R. Anand" },
  ]);
  vi.mocked(api.ListPriorities).mockResolvedValue(["Highest", "High", "Medium", "Low"]);
  vi.mocked(api.GetSubtaskTypeName).mockResolvedValue("Technical task");
  vi.mocked(api.CreateIssue).mockResolvedValue("TAM-NEW-1");
});

// pickEpic waits for an epic to actually be on the select before choosing it.
//
// Waiting for the control to be enabled is not enough, and was flaky on CI: the
// epics query is enabled only once a profile id arrives, and a disabled
// TanStack query reports isLoading false. So there is a window where the select
// is enabled and carries nothing but the placeholder. The wait passed there,
// the query then started, isLoading flipped true, and the select went back to
// "(loading)" with no options for selectOptions to find. Waiting for the option
// itself covers both the load and the enabled state.
async function pickEpic(scope: HTMLElement, key: string) {
  await waitFor(() =>
    expect(within(scope).getByRole("option", { name: new RegExp(key) })).toBeInTheDocument(),
  );
  await userEvent.selectOptions(within(scope).getByLabelText("Epic"), key);
}

describe("NewIssueModal", () => {
  it("creates a task from the minimal form", async () => {
    const user = userEvent.setup();
    const { onCreated, onClose } = renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    await user.type(within(dialog).getByLabelText("Summary *"), "Add a retry to the payment webhook consumer");
    await user.type(within(dialog).getByLabelText("Labels"), "payments, webhooks");
    await user.type(within(dialog).getByLabelText("Story points"), "3");
    // The assignee is picked, not typed: what the draft carries has to be the
    // username Jira accepts, and the grid only ever had the display name.
    await user.click(within(dialog).getByLabelText("Assignee"));
    await user.click(await within(dialog).findByRole("option", { name: /M. Ortiz/ }));
    await user.selectOptions(within(dialog).getByLabelText("Priority"), "High");
    await user.click(await submitButton(dialog));
    await waitFor(() => expect(api.CreateIssue).toHaveBeenCalledWith("p1", {
      ...baseDraft,
      summary: "Add a retry to the payment webhook consumer",
      labels: ["payments", "webhooks"], assignee: "mortiz", priority: "High", storyPoints: 3,
    }));
    expect(onCreated).toHaveBeenCalledWith("TAM-NEW-1");
    expect(onClose).toHaveBeenCalled();
  });

  // The title used to say "New issue" whichever button opened the dialog, so
  // "+ New epic" opened something that did not agree it was about an epic.
  it("names itself after the type it is about to create", async () => {
    const user = userEvent.setup();
    renderModal(vi.fn(), vi.fn(), "epic");
    expect(await screen.findByRole("dialog", { name: "New epic" })).toBeInTheDocument();
    await user.selectOptions(screen.getByLabelText("Type"), "story");
    expect(screen.getByRole("dialog", { name: "New story" })).toBeInTheDocument();
    expect(screen.getByLabelText("Epic")).toBeInTheDocument();
  });

  it("files a non-epic draft under the epic that was picked", async () => {
    const user = userEvent.setup();
    renderModal(vi.fn(), vi.fn(), "story");
    const dialog = await screen.findByRole("dialog", { name: "New story" });
    await pickEpic(dialog, "PLAT-350");
    await user.type(within(dialog).getByLabelText("Summary *"), "Apply a promo code");
    await user.click(await submitButton(dialog));
    await waitFor(() => expect(api.CreateIssue).toHaveBeenCalled());
    expect(vi.mocked(api.CreateIssue).mock.calls[0][1].parentKey).toBe("PLAT-350");
  });

  it("has no epic picker for an epic, and drops a parent when the type becomes one", async () => {
    const user = userEvent.setup();
    renderModal(vi.fn(), vi.fn(), "story");
    const dialog = await screen.findByRole("dialog", { name: "New story" });
    await pickEpic(dialog, "PLAT-360");
    await user.selectOptions(within(dialog).getByLabelText("Type"), "epic");
    expect(within(dialog).queryByLabelText("Epic")).not.toBeInTheDocument();
    await user.type(within(dialog).getByLabelText("Summary *"), "Checkout revamp");
    await user.click(await submitButton(dialog));
    await waitFor(() => expect(api.CreateIssue).toHaveBeenCalled());
    expect(vi.mocked(api.CreateIssue).mock.calls[0][1].parentKey).toBe("");
  });

  // "+ New epic" is a statement, not an opening question.
  it("fixes the type and drops the select when the caller locks it", async () => {
    renderModal(vi.fn(), vi.fn(), "epic", true);
    const dialog = await screen.findByRole("dialog", { name: "New epic" });
    expect(within(dialog).queryByLabelText("Type")).not.toBeInTheDocument();
    expect(within(dialog).queryByLabelText("Epic")).not.toBeInTheDocument();
    expect(within(dialog).queryByLabelText("Story points")).not.toBeInTheDocument();
  });

  // Submitting before the required fields are known skipped them entirely and
  // deferred the failure to a Jira 400 at Commit.
  it("will not submit until it knows which fields Jira requires", async () => {
    let release: (specs: api.FieldSpec[]) => void = () => {};
    vi.mocked(api.GetCreateFields).mockImplementation(
      () => new Promise((resolve) => { release = resolve; }),
    );
    renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    const submit = within(dialog).getByRole("button", { name: "Checking Jira" });
    expect(submit).toBeDisabled();
    // The query only starts once the profile has loaded, so release is not
    // assigned until then; releasing before that would resolve nothing.
    await waitFor(() => expect(api.GetCreateFields).toHaveBeenCalled());
    release([]);
    await waitFor(() => expect(within(dialog).getByRole("button", { name: "Create draft" })).toBeEnabled());
  });

  it("asks for the type's required create-meta fields and sends them as extra", async () => {
    const user = userEvent.setup();
    renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    await user.selectOptions(within(dialog).getByLabelText("Type"), "bug");
    const severity = await within(dialog).findByLabelText("Severity *");
    await user.type(within(dialog).getByLabelText("Summary *"), "Promo field accepts spaces");
    await user.click(await submitButton(dialog));
    expect(await within(dialog).findByText("Severity is required.")).toBeInTheDocument();
    // The message names a field, so it points at that field too.
    expect(severity).toHaveAttribute("aria-invalid", "true");
    expect(severity).toHaveFocus();
    expect(api.CreateIssue).not.toHaveBeenCalled();
    await user.selectOptions(severity, "3");
    await user.click(await submitButton(dialog));
    await waitFor(() => expect(api.CreateIssue).toHaveBeenCalled());
    expect(vi.mocked(api.CreateIssue).mock.calls[0][1].extra).toEqual({ customfield_10050: "3" });
    expect(vi.mocked(api.CreateIssue).mock.calls[0][1].type).toBe("bug");
  });

  it("takes more than one value for an array field", async () => {
    const user = userEvent.setup();
    vi.mocked(api.GetCreateFields).mockResolvedValue([
      { id: "components", name: "Components", type: "array", required: true, allowedValues: [
        { id: "10", value: "Frontend" }, { id: "11", value: "Backend" }, { id: "12", value: "API" },
      ] },
    ]);
    renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    const components = await within(dialog).findByLabelText("Components *");
    await user.selectOptions(components, ["10", "12"]);
    await user.type(within(dialog).getByLabelText("Summary *"), "Split the checkout bundle");
    await user.click(await submitButton(dialog));
    await waitFor(() => expect(api.CreateIssue).toHaveBeenCalled());
    // Joined with a comma, which is what the Jira backend splits into the
    // array Jira wants.
    expect(vi.mocked(api.CreateIssue).mock.calls[0][1].extra).toEqual({ components: "10,12" });
  });

  it("checks a required number field the same way it checks story points", async () => {
    const user = userEvent.setup();
    vi.mocked(api.GetCreateFields).mockResolvedValue([
      { id: "customfield_10099", name: "Effort", type: "number", required: true, allowedValues: [] },
    ]);
    renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    const effort = await within(dialog).findByLabelText("Effort *");
    await user.type(within(dialog).getByLabelText("Summary *"), "Rework the retry policy");
    await user.type(effort, "soon");
    await user.click(await submitButton(dialog));
    expect(await within(dialog).findByText("Effort must be a number.")).toBeInTheDocument();
    expect(api.CreateIssue).not.toHaveBeenCalled();
  });

  it("degrades to the minimal form when create-meta cannot be read", async () => {
    const user = userEvent.setup();
    vi.mocked(api.GetCreateFields).mockRejectedValue(new Error("GET failed: 403"));
    renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    expect(await within(dialog).findByText(/Jira's required fields could not be read/)).toBeInTheDocument();
    // A failed read still lets the user draft: that is the intended degrade,
    // unlike a read still in flight.
    await user.type(within(dialog).getByLabelText("Summary *"), "Still works");
    await user.click(await submitButton(dialog));
    await waitFor(() => expect(api.CreateIssue).toHaveBeenCalled());
  });

  it("refuses a blank summary and shows the backend's error", async () => {
    const user = userEvent.setup();
    renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    await user.click(await submitButton(dialog));
    expect(await within(dialog).findByText("Summary cannot be empty.")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Summary *")).toHaveFocus();
    vi.mocked(api.CreateIssue).mockRejectedValueOnce(new Error("summary cannot be empty"));
    await user.type(within(dialog).getByLabelText("Summary *"), "x");
    await user.click(await submitButton(dialog));
    expect(await within(dialog).findByText(/summary cannot be empty/)).toBeInTheDocument();
  });

  // The sentence that explains the whole offline-first model used to live in
  // the slot the error takes, so it vanished exactly when a confused user
  // needed it.
  it("keeps saying the draft is local even while showing an error", async () => {
    const user = userEvent.setup();
    renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    expect(within(dialog).getByText(/Commit creates it in Jira/)).toBeInTheDocument();
    await user.click(await submitButton(dialog));
    expect(await within(dialog).findByText("Summary cannot be empty.")).toBeInTheDocument();
    expect(within(dialog).getByText(/Commit creates it in Jira/)).toBeInTheDocument();
  });

  it("offers Requirement and hides story points for it", async () => {
    const user = userEvent.setup();
    renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    expect(within(dialog).getByLabelText("Story points")).toBeInTheDocument();
    await user.selectOptions(within(dialog).getByLabelText("Type"), "requirement");
    expect(within(dialog).queryByLabelText("Story points")).not.toBeInTheDocument();
    await user.type(within(dialog).getByLabelText("Summary *"), "Single-use promo codes");
    await user.click(await submitButton(dialog));
    await waitFor(() => expect(api.CreateIssue).toHaveBeenCalled());
    const draft = vi.mocked(api.CreateIssue).mock.calls[0][1];
    expect(draft.type).toBe("requirement");
    expect(draft.storyPoints).toBeNull();
  });

  // Points typed as a Story used to survive a switch to Epic: hidden, nulled
  // in the payload, and back again if the user switched away.
  it("forgets story points when the type stops having them", async () => {
    const user = userEvent.setup();
    renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    await user.type(within(dialog).getByLabelText("Story points"), "5");
    await user.selectOptions(within(dialog).getByLabelText("Type"), "epic");
    await user.selectOptions(within(dialog).getByLabelText("Type"), "story");
    expect(within(dialog).getByLabelText("Story points")).toHaveValue("");
  });

  it("preselects Epic and hides Story points when initialType is epic", async () => {
    const user = userEvent.setup();
    renderModal(vi.fn(), vi.fn(), "epic");
    const dialog = await screen.findByRole("dialog", { name: "New epic" });
    expect(within(dialog).getByLabelText("Type")).toHaveValue("epic");
    expect(within(dialog).queryByLabelText("Story points")).not.toBeInTheDocument();
    await user.type(within(dialog).getByLabelText("Summary *"), "Checkout revamp");
    await user.click(await submitButton(dialog));
    await waitFor(() => expect(api.CreateIssue).toHaveBeenCalled());
    const draft = vi.mocked(api.CreateIssue).mock.calls[0][1];
    expect(draft.type).toBe("epic");
    expect(draft.storyPoints).toBeNull();
  });

  it("offers the instance's priorities instead of asking for one from memory", async () => {
    renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    const priority = within(dialog).getByLabelText("Priority");
    await waitFor(() => expect(within(priority as HTMLElement).getByRole("option", { name: "Highest" })).toBeInTheDocument());
    expect(within(priority as HTMLElement).getByRole("option", { name: "Jira's default" })).toBeInTheDocument();
  });

  // A lookup that cannot reach its list must not be the reason an issue
  // cannot be assigned or prioritised.
  it("falls back to typing when the lookups fail", async () => {
    const user = userEvent.setup();
    vi.mocked(api.SearchUsers).mockRejectedValue(new Error("GET failed: 503"));
    vi.mocked(api.ListPriorities).mockRejectedValue(new Error("GET failed: 503"));
    renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    await waitFor(() =>
      expect(within(dialog).getByText(/The priority list could not be read/)).toBeInTheDocument(),
    );
    await user.type(within(dialog).getByLabelText("Assignee"), "mortiz");
    expect(await within(dialog).findByText(/Type a Jira username instead/)).toBeInTheDocument();
    await user.type(within(dialog).getByLabelText("Priority"), "Blocker");
    await user.type(within(dialog).getByLabelText("Summary *"), "Still assignable");
    await user.click(await submitButton(dialog));
    await waitFor(() => expect(api.CreateIssue).toHaveBeenCalled());
    const draft = vi.mocked(api.CreateIssue).mock.calls[0][1];
    expect(draft.assignee).toBe("mortiz");
    expect(draft.priority).toBe("Blocker");
  });

  // A sub-task is never drafted on its own: it is drafted from the issue it
  // hangs off, and its Jira type name is whatever the instance calls it.
  it("drafts a sub-task under the issue it was opened from", async () => {
    const user = userEvent.setup();
    renderModal(vi.fn(), vi.fn(), "subtask", true, "PLAT-412");
    const dialog = await screen.findByRole("dialog", { name: "New technical task" });
    // The parent is stated, not offered: no epic picker, no type select.
    expect(within(dialog).getByText("PLAT-412")).toBeInTheDocument();
    expect(within(dialog).queryByLabelText("Epic")).not.toBeInTheDocument();
    expect(within(dialog).queryByLabelText("Type")).not.toBeInTheDocument();
    await user.type(within(dialog).getByLabelText("Summary *"), "Wire the promo input");
    await user.click(await submitButton(dialog));
    await waitFor(() => expect(api.CreateIssue).toHaveBeenCalled());
    const draft = vi.mocked(api.CreateIssue).mock.calls[0][1];
    expect(draft.type).toBe("subtask");
    expect(draft.parentKey).toBe("PLAT-412");
  });

  // The dialog names the level the instance uses, not TAM's own word.
  it("falls back to sub-task when the instance's name cannot be read", async () => {
    vi.mocked(api.GetSubtaskTypeName).mockRejectedValue(new Error("GET failed: 503"));
    renderModal(vi.fn(), vi.fn(), "subtask", true, "PLAT-412");
    expect(await screen.findByRole("dialog", { name: "New sub-task" })).toBeInTheDocument();
  });

  it("closes without asking when nothing has been typed", async () => {
    const user = userEvent.setup();
    const { onClose } = renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalled();
  });

  it("asks before discarding typed work", async () => {
    const user = userEvent.setup();
    const { onClose } = renderModal();
    const dialog = await screen.findByRole("dialog", { name: "New task" });
    await user.type(within(dialog).getByLabelText("Summary *"), "Half a thought");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    const confirmDialog = await screen.findByRole("alertdialog", { name: /Discard this draft/ });
    expect(onClose).not.toHaveBeenCalled();
    await user.click(within(confirmDialog).getByRole("button", { name: "Discard" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });
});
