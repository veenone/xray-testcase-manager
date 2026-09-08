import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ProfileForm } from "./ProfileForm";
import type { Profile } from "../api";

const getBugConnection = vi.fn();
const saveBugConnection = vi.fn();
const deleteBugConnection = vi.fn();
const updateProfile = vi.fn();
const createProfileReusingToken = vi.fn();

vi.mock("../api", () => ({
  CreateProfile: vi.fn(),
  CreateProfileReusingToken: (...args: unknown[]) => createProfileReusingToken(...args),
  UpdateProfile: (...args: unknown[]) => updateProfile(...args),
  SetProfileCrossProjectSources: vi.fn(async () => {}),
  AddConnection: vi.fn(),
  UpdateConnection: vi.fn(),
  TestConnection: vi.fn(),
  TestProfileConnection: vi.fn(),
  GetBugConnection: (...args: unknown[]) => getBugConnection(...args),
  SaveBugConnection: (...args: unknown[]) => saveBugConnection(...args),
  DeleteBugConnection: (...args: unknown[]) => deleteBugConnection(...args),
  errMsg: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));

// useConfirm wraps the app-wide themed confirm dialog (DialogContext), which
// isn't mounted in these component-only tests -- stub it directly so the
// "remove the bug connection" path can be driven deterministically.
const confirmMock = vi.fn();
vi.mock("./useConfirm", () => ({
  useConfirm: () => ({ confirm: (...args: unknown[]) => confirmMock(...args) }),
}));

const kiwiProfile: Profile = {
  id: "p1",
  name: "Kiwi profile",
  jiraUrl: "https://kiwi.example.com",
  projectKey: "MyProduct",
  scopeJql: "",
  crossProjectSources: "",
  bugIssueType: "",
  bugProjectMode: "test",
  bugProjectKey: "",
  caCert: "",
  allowUntrustedTls: false,
  backend: "kiwi",
  createdAt: "",
};

beforeEach(() => {
  getBugConnection.mockReset();
  getBugConnection.mockResolvedValue({
    id: "",
    url: "",
    projectKey: "",
    bugIssueType: "",
    caCert: "",
    allowUntrustedTls: false,
  });
  saveBugConnection.mockReset();
  saveBugConnection.mockResolvedValue({
    id: "bug1",
    url: "",
    projectKey: "",
    bugIssueType: "",
    caCert: "",
    allowUntrustedTls: false,
  });
  deleteBugConnection.mockReset();
  deleteBugConnection.mockResolvedValue(undefined);
  updateProfile.mockReset();
  updateProfile.mockImplementation(async (id: string) => ({
    ...kiwiProfile,
    id,
  }));
  createProfileReusingToken.mockReset();
  confirmMock.mockReset();
  confirmMock.mockResolvedValue(true);
});

describe("ProfileForm bug tracker", () => {
  // The bug-connection section only makes sense for a backend that cannot
  // file its own defects. An Xray profile already files into its own Jira.
  it("offers a bug tracker only for a Kiwi profile", async () => {
    render(<ProfileForm onCreated={() => {}} />);
    expect(screen.queryByRole("group", { name: /bug tracker/i })).toBeNull();

    await userEvent.selectOptions(screen.getByLabelText(/backend/i), "kiwi");

    expect(
      await screen.findByRole("group", { name: /bug tracker/i }),
    ).toBeInTheDocument();
  });

  // A saved connection must come back into the form, minus the token: the
  // credential lives in the OS credential manager and is never read back.
  it("round-trips a saved bug connection without its token", async () => {
    getBugConnection.mockResolvedValue({
      id: "bug1",
      url: "https://jira.example.com",
      projectKey: "DEF",
      bugIssueType: "Bug",
      caCert: "",
      allowUntrustedTls: false,
    });

    render(<ProfileForm profile={kiwiProfile} onCreated={() => {}} />);

    expect(await screen.findByLabelText(/bug tracker url/i)).toHaveValue(
      "https://jira.example.com",
    );
    expect(screen.getByLabelText(/bug project key/i)).toHaveValue("DEF");
    expect(screen.getByLabelText(/bug tracker token/i)).toHaveValue("");
  });

  // Leaving the token blank on an edit must not wipe the stored credential,
  // so the form sends "" and the backend keeps what it has.
  it("sends a blank token when the user does not retype it", async () => {
    getBugConnection.mockResolvedValue({
      id: "bug1",
      url: "https://jira.example.com",
      projectKey: "DEF",
      bugIssueType: "Bug",
      caCert: "",
      allowUntrustedTls: false,
    });
    const user = userEvent.setup();

    render(<ProfileForm profile={kiwiProfile} onCreated={() => {}} />);
    await user.click(
      await screen.findByRole("button", { name: /save changes/i }),
    );

    expect(saveBugConnection).toHaveBeenCalledWith(
      "p1",
      "https://jira.example.com",
      "DEF",
      "Bug",
      "",
      "",
      false,
    );
  });

  // The CA certificate and untrusted-TLS flag are part of the connection
  // (design spec: "URL, project key, issue type, credential, CA certificate,
  // and the untrusted-TLS flag") -- loaded values must flow straight through
  // to the next save, not get silently dropped.
  it("round-trips the CA certificate and untrusted-TLS flag through save", async () => {
    getBugConnection.mockResolvedValue({
      id: "bug1",
      url: "https://jira.example.com",
      projectKey: "DEF",
      bugIssueType: "Bug",
      caCert: "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----",
      allowUntrustedTls: true,
    });
    const user = userEvent.setup();

    render(<ProfileForm profile={kiwiProfile} onCreated={() => {}} />);
    await user.click(
      await screen.findByRole("button", { name: /save changes/i }),
    );

    expect(saveBugConnection).toHaveBeenCalledWith(
      "p1",
      "https://jira.example.com",
      "DEF",
      "Bug",
      "",
      "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----",
      true,
    );
  });

  // Clearing only one of URL/project key is an edit in progress, not a
  // removal request -- Save must block with a validation message instead of
  // guessing (and never call Delete on a half-cleared pair).
  it("blocks save instead of deleting when only one field is cleared", async () => {
    getBugConnection.mockResolvedValue({
      id: "bug1",
      url: "https://jira.example.com",
      projectKey: "DEF",
      bugIssueType: "Bug",
      caCert: "",
      allowUntrustedTls: false,
    });
    const user = userEvent.setup();

    render(<ProfileForm profile={kiwiProfile} onCreated={() => {}} />);
    await user.clear(await screen.findByLabelText(/bug tracker url/i));

    expect(
      await screen.findByText(/enter both a bug tracker url and a project key/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /save changes/i })).toBeDisabled();
    expect(saveBugConnection).not.toHaveBeenCalled();
    expect(deleteBugConnection).not.toHaveBeenCalled();
  });

  // Clearing BOTH fields is the one unambiguous "turn bug routing off"
  // signal. It still asks for confirmation before deleting the stored
  // connection and credential.
  it("removes the bug connection only when both fields are cleared, after confirming", async () => {
    getBugConnection.mockResolvedValue({
      id: "bug1",
      url: "https://jira.example.com",
      projectKey: "DEF",
      bugIssueType: "Bug",
      caCert: "",
      allowUntrustedTls: false,
    });
    confirmMock.mockResolvedValue(true);
    const user = userEvent.setup();

    render(<ProfileForm profile={kiwiProfile} onCreated={() => {}} />);
    await user.clear(await screen.findByLabelText(/bug tracker url/i));
    await user.clear(screen.getByLabelText(/bug project key/i));
    await user.click(screen.getByRole("button", { name: /save changes/i }));

    expect(confirmMock).toHaveBeenCalled();
    expect(deleteBugConnection).toHaveBeenCalledWith("p1");
    expect(saveBugConnection).not.toHaveBeenCalled();
  });

  // Declining the confirm must leave the stored connection untouched, and
  // the form must go back to showing what is actually stored -- otherwise
  // the displayed (blank) state contradicts reality and the next Save
  // re-prompts for the same deletion.
  it("keeps the stored connection and restores the fields when the user declines the removal confirm", async () => {
    getBugConnection.mockResolvedValue({
      id: "bug1",
      url: "https://jira.example.com",
      projectKey: "DEF",
      bugIssueType: "Bug",
      caCert: "",
      allowUntrustedTls: false,
    });
    confirmMock.mockResolvedValue(false);
    const user = userEvent.setup();

    render(<ProfileForm profile={kiwiProfile} onCreated={() => {}} />);
    await user.clear(await screen.findByLabelText(/bug tracker url/i));
    await user.clear(screen.getByLabelText(/bug project key/i));
    await user.click(screen.getByRole("button", { name: /save changes/i }));

    expect(confirmMock).toHaveBeenCalled();
    expect(deleteBugConnection).not.toHaveBeenCalled();
    expect(await screen.findByLabelText(/bug tracker url/i)).toHaveValue(
      "https://jira.example.com",
    );
    expect(screen.getByLabelText(/bug project key/i)).toHaveValue("DEF");
  });

  // A load failure must not be mistaken for "nothing configured" -- that
  // would let a user fill in fresh values and silently overwrite a
  // connection they never saw.
  it("shows an error instead of a blank section when the load fails", async () => {
    getBugConnection.mockRejectedValue(new Error("network down"));

    render(<ProfileForm profile={kiwiProfile} onCreated={() => {}} />);

    expect(
      await screen.findByText(/could not load the saved bug tracker connection/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /save changes/i })).toBeDisabled();
  });

  // On create, the profile itself is genuinely saved before the bug
  // connection is attempted. If the bug-connection step then fails, the
  // caller must still be told the profile exists -- otherwise a retry
  // creates a duplicate.
  it("still reports the profile as created when the bug-connection save fails", async () => {
    createProfileReusingToken.mockResolvedValue({
      ...kiwiProfile,
      id: "new1",
      name: "New Kiwi profile",
    });
    saveBugConnection.mockRejectedValue(
      new Error("a bug connection needs a token"),
    );
    const onCreated = vi.fn();
    const user = userEvent.setup();

    render(
      <ProfileForm
        onCreated={onCreated}
        profiles={[{ ...kiwiProfile, id: "existing1", name: "Existing Kiwi" }]}
      />,
    );

    await user.selectOptions(screen.getByLabelText(/backend/i), "kiwi");
    await user.type(screen.getByLabelText(/profile name/i), "New Kiwi profile");
    await user.type(
      screen.getByLabelText(/kiwi server url/i),
      "https://kiwi.example.com",
    );
    await user.type(screen.getByLabelText("Product"), "MyProduct");
    await user.selectOptions(screen.getByLabelText(/credential/i), "existing1");

    const bugSection = await screen.findByRole("group", { name: /bug tracker/i });
    await user.type(
      within(bugSection).getByLabelText(/bug tracker url/i),
      "https://jira.example.com",
    );
    await user.type(within(bugSection).getByLabelText(/bug project key/i), "def");

    await user.click(screen.getByRole("button", { name: /create profile/i }));

    expect(
      await screen.findByText(/bug tracker connection could not be saved/i),
    ).toBeInTheDocument();
    expect(onCreated).toHaveBeenCalledWith(
      expect.objectContaining({ id: "new1" }),
    );
  });
});
