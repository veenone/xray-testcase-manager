import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import { ProfileProvider, useProfile } from "./ProfileContext";
import * as api from "../api";

vi.mock("../api", () => ({
  ListProfiles: vi.fn(),
  GetSettings: vi.fn(),
  SetTheme: vi.fn(),
  SetDefaultProfile: vi.fn(),
  errMsg: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));

const mock = (fn: unknown) => fn as ReturnType<typeof vi.fn>;

function wrapper({ children }: { children: React.ReactNode }) {
  return <ProfileProvider>{children}</ProfileProvider>;
}

const PROFILES = [
  { id: "p1", name: "Alpha", projectKey: "AL", jiraUrl: "demo" },
  { id: "p2", name: "Beta", projectKey: "BE", jiraUrl: "demo" },
];

describe("useProfile", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.documentElement.removeAttribute("data-theme");
  });

  it("throws when used outside a ProfileProvider", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => renderHook(() => useProfile())).toThrow(/ProfileProvider/);
    spy.mockRestore();
  });

  it("reloadProfiles loads profiles + settings, picks the default, applies theme", async () => {
    mock(api.ListProfiles).mockResolvedValue(PROFILES);
    mock(api.GetSettings).mockResolvedValue({
      defaultProfileId: "p2",
      theme: "dark",
      showCoverage: true,
      tourSeenVersion: 3,
    });

    const { result } = renderHook(() => useProfile(), { wrapper });

    let returned: unknown;
    await act(async () => {
      returned = await result.current.reloadProfiles();
    });

    expect(result.current.profiles).toHaveLength(2);
    // Default profile becomes active, not the first one.
    expect(result.current.activeId).toBe("p2");
    expect(result.current.activeProfile?.name).toBe("Beta");
    expect(result.current.defaultProfileId).toBe("p2");
    expect(result.current.theme).toBe("dark");
    expect(result.current.showCoverage).toBe(true);
    expect(document.documentElement.dataset.theme).toBe("dark");
    // Returns the raw settings so the caller can read non-profile fields.
    expect((returned as { tourSeenVersion: number }).tourSeenVersion).toBe(3);
  });

  it("reloadProfiles falls back to the first profile when no valid default", async () => {
    mock(api.ListProfiles).mockResolvedValue(PROFILES);
    mock(api.GetSettings).mockResolvedValue({
      defaultProfileId: "gone",
      theme: "",
      showCoverage: false,
      tourSeenVersion: 0,
    });

    const { result } = renderHook(() => useProfile(), { wrapper });
    await act(async () => {
      await result.current.reloadProfiles();
    });

    expect(result.current.activeId).toBe("p1");
    // Empty theme resolves to light.
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("setTheme applies the DOM attribute and persists", async () => {
    mock(api.SetTheme).mockResolvedValue(undefined);
    const { result } = renderHook(() => useProfile(), { wrapper });

    await act(async () => {
      await result.current.setTheme("dark");
    });

    expect(result.current.theme).toBe("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(api.SetTheme).toHaveBeenCalledWith("dark");
  });

  it("setDefault toggles the launch-default off when re-selected", async () => {
    mock(api.SetDefaultProfile).mockResolvedValue(undefined);
    const { result } = renderHook(() => useProfile(), { wrapper });

    await act(async () => {
      await result.current.setDefault("p1");
    });
    expect(result.current.defaultProfileId).toBe("p1");
    expect(api.SetDefaultProfile).toHaveBeenLastCalledWith("p1");

    // Selecting the same id again clears it.
    await act(async () => {
      await result.current.setDefault("p1");
    });
    await waitFor(() => expect(result.current.defaultProfileId).toBe(""));
    expect(api.SetDefaultProfile).toHaveBeenLastCalledWith("");
  });
});
