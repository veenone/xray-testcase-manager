import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
} from "react";
import type { Dispatch, ReactNode, SetStateAction } from "react";
import {
  ListProfiles,
  GetSettings,
  SetTheme,
  SetDefaultProfile,
  errMsg,
} from "../api";
import type { Profile, Settings } from "../api";

// ProfileContext owns the active-workspace state that was previously threaded
// through App.tsx as the app's most-drilled value (`profileId={activeId}`).
// This is step 1 of the App.tsx context decomposition (spec
// 2026-08-26-app-context-decomposition-design.md, §5.1).
//
// PR-A scope: hold the profile/settings-display state and its self-contained
// persistence actions here; App.tsx reads them via useProfile(). Consumers
// still receive the profileId prop for now — a later PR swaps those prop reads
// for useProfile(). The raw setters are exposed transitionally because several
// composite handlers (importProfile, deleteProfile, toggleCoverage) still live
// in App and will move into their own contexts (Nav / Modal) in later steps.

// applyTheme resolves the preference ("system" follows the OS) and sets the
// data-theme attribute the CSS tokens key off (FR-12.2).
function applyTheme(theme: string) {
  const dark =
    theme === "dark" ||
    (theme === "system" &&
      window.matchMedia?.("(prefers-color-scheme: dark)").matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
}

interface ProfileApi {
  profiles: Profile[];
  activeId: string;
  defaultProfileId: string;
  theme: string;
  showCoverage: boolean;
  loadingProfiles: boolean;
  activeProfile: Profile | undefined;
  // Transitional raw setters — composite handlers in App still drive these.
  setProfiles: Dispatch<SetStateAction<Profile[]>>;
  setActiveId: Dispatch<SetStateAction<string>>;
  setDefaultProfileId: Dispatch<SetStateAction<string>>;
  setShowCoverage: Dispatch<SetStateAction<boolean>>;
  // Self-contained persistence actions.
  setTheme: (next: string) => Promise<void>;
  setDefault: (id: string) => Promise<void>;
  // Loads profiles + settings, applies theme, picks the launch profile, and
  // returns the loaded Settings so the caller can handle non-profile settings
  // (e.g. the onboarding tour version) that live outside this context.
  reloadProfiles: () => Promise<Settings | null>;
}

const ProfileContext = createContext<ProfileApi | null>(null);

export function useProfile(): ProfileApi {
  const ctx = useContext(ProfileContext);
  if (!ctx) {
    throw new Error("useProfile must be used within a ProfileProvider");
  }
  return ctx;
}

export function ProfileProvider({ children }: { children: ReactNode }) {
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [activeId, setActiveId] = useState<string>("");
  const [defaultProfileId, setDefaultProfileId] = useState<string>("");
  const [theme, setThemeState] = useState<string>("light");
  // The Coverage module is opt-in; its top-nav tab is hidden until enabled.
  const [showCoverage, setShowCoverage] = useState(false);
  const [loadingProfiles, setLoadingProfiles] = useState(false);

  // setTheme applies + persists a colour-theme preference (FR-12.2).
  const setTheme = useCallback(async (next: string) => {
    setThemeState(next);
    applyTheme(next);
    try {
      await SetTheme(next);
    } catch (e) {
      console.error("set theme:", errMsg(e));
    }
  }, []);

  // setDefault toggles the launch-default for a profile (clears it if it's
  // already the default), used by the Manage Profiles modal.
  const setDefault = useCallback(
    async (id: string) => {
      const next = defaultProfileId === id ? "" : id;
      try {
        await SetDefaultProfile(next);
        setDefaultProfileId(next);
      } catch (e) {
        console.error("set default profile:", errMsg(e));
      }
    },
    [defaultProfileId],
  );

  const reloadProfiles = useCallback(async (): Promise<Settings | null> => {
    setLoadingProfiles(true);
    try {
      const [ps, s] = await Promise.all([ListProfiles(), GetSettings()]);
      setProfiles(ps);
      setDefaultProfileId(s.defaultProfileId ?? "");
      const t = s.theme || "light";
      setThemeState(t);
      applyTheme(t);
      setShowCoverage(!!s.showCoverage);
      if (ps.length > 0) {
        const def =
          s.defaultProfileId && ps.some((p) => p.id === s.defaultProfileId)
            ? s.defaultProfileId
            : ps[0].id;
        setActiveId(def);
      }
      return s;
    } catch (e) {
      console.error("load profiles:", errMsg(e));
      return null;
    } finally {
      setLoadingProfiles(false);
    }
  }, []);

  const activeProfile = useMemo(
    () => profiles.find((p) => p.id === activeId),
    [profiles, activeId],
  );

  const api = useMemo<ProfileApi>(
    () => ({
      profiles,
      activeId,
      defaultProfileId,
      theme,
      showCoverage,
      loadingProfiles,
      activeProfile,
      setProfiles,
      setActiveId,
      setDefaultProfileId,
      setShowCoverage,
      setTheme,
      setDefault,
      reloadProfiles,
    }),
    [
      profiles,
      activeId,
      defaultProfileId,
      theme,
      showCoverage,
      loadingProfiles,
      activeProfile,
      setTheme,
      setDefault,
      reloadProfiles,
    ],
  );

  return (
    <ProfileContext.Provider value={api}>{children}</ProfileContext.Provider>
  );
}
