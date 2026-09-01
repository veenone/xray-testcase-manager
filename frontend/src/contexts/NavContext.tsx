import { createContext, useContext, useMemo, useState } from "react";
import type { Dispatch, ReactNode, SetStateAction } from "react";

// NavContext owns App.tsx's view-routing and browse-grouping state (spec §5.4):
// the active top-level view, the group-by dimension, and the three sidebar
// selections (folder / container / component) that filter the grid. It also
// holds the New Test panel state — showNewTest is a *panel* that shares the
// detail slot with TestDetail (not a modal overlay), so it belongs with the
// view/nav state rather than the modal reducer.
//
// State-first extraction, matching the other contexts: the raw setters are
// exposed so App keeps driving the cross-context reset effects (clearing the
// sidebar selections + the bulk selection when the profile or group-by dimension
// changes) — those coordinate NavContext with SelectionContext, so they stay in
// App.

export type View =
  | "browse"
  | "preconditions"
  | "requirements"
  | "duplicates"
  | "gapanalysis"
  | "testcalls"
  | "dashboard"
  | "traceability"
  | "plans"
  | "coverage"
  | "misspellings";

export type GroupBy = "folder" | "testset" | "testplan" | "component";

interface NavApi {
  view: View;
  setView: Dispatch<SetStateAction<View>>;
  groupBy: GroupBy;
  setGroupBy: Dispatch<SetStateAction<GroupBy>>;
  selectedFolder: string;
  setSelectedFolder: Dispatch<SetStateAction<string>>;
  selectedContainer: string;
  setSelectedContainer: Dispatch<SetStateAction<string>>;
  selectedComponent: string;
  setSelectedComponent: Dispatch<SetStateAction<string>>;
  // The New Test panel (shares the detail slot with TestDetail — a panel, not a
  // modal overlay) and the folder it seeds new tests into.
  showNewTest: boolean;
  setShowNewTest: Dispatch<SetStateAction<boolean>>;
  newTestFolder: string;
  setNewTestFolder: Dispatch<SetStateAction<string>>;
}

const NavContext = createContext<NavApi | null>(null);

export function useNav(): NavApi {
  const ctx = useContext(NavContext);
  if (!ctx) {
    throw new Error("useNav must be used within a NavProvider");
  }
  return ctx;
}

export function NavProvider({ children }: { children: ReactNode }) {
  const [view, setView] = useState<View>("browse");
  const [groupBy, setGroupBy] = useState<GroupBy>("folder");
  const [selectedFolder, setSelectedFolder] = useState<string>("");
  const [selectedContainer, setSelectedContainer] = useState<string>("");
  const [selectedComponent, setSelectedComponent] = useState<string>("");
  const [showNewTest, setShowNewTest] = useState(false);
  const [newTestFolder, setNewTestFolder] = useState<string>("");

  const api = useMemo<NavApi>(
    () => ({
      view,
      setView,
      groupBy,
      setGroupBy,
      selectedFolder,
      setSelectedFolder,
      selectedContainer,
      setSelectedContainer,
      selectedComponent,
      setSelectedComponent,
      showNewTest,
      setShowNewTest,
      newTestFolder,
      setNewTestFolder,
    }),
    [
      view,
      groupBy,
      selectedFolder,
      selectedContainer,
      selectedComponent,
      showNewTest,
      newTestFolder,
    ],
  );

  return <NavContext.Provider value={api}>{children}</NavContext.Provider>;
}
