import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
} from "react";
import type { Dispatch, ReactNode, SetStateAction } from "react";

// SelectionContext owns the two pieces of browse selection App.tsx used to
// hold (spec §5.3 / audit A1): `selectedSet` — the bulk multi-select — and
// `selectedKey` — the row whose detail panel is open. They live in their own
// context because selection changes are high-frequency; keeping them out of
// App's other state stops a checkbox toggle or row open from re-rendering the
// profile / sync consumers.
//
// PR scope: state-first, matching the ProfileContext extraction. The three
// non-trivial actions (toggle / togglePage / selectAllMatching) move here; the
// raw setters are exposed transitionally for App's composite handlers (the
// profile-change reset, applyCreatedRemap's temp→real key remap, the bulk
// modals' onComplete clears). TestTable and the bulk modals still receive these
// via props; migrating them to useSelection() is a later step.
//
// (A value/dispatch split — a stable actions context separate from the
// frequently-changing value — is worthwhile once the child consumers read from
// context directly; today App is the only consumer, so a single memoized value
// keeps the same re-render surface it already had.)

interface SelectionApi {
  selectedSet: Set<string>;
  selectedKey: string | null;
  setSelectedSet: Dispatch<SetStateAction<Set<string>>>;
  setSelectedKey: Dispatch<SetStateAction<string | null>>;
  // toggle flips one key in the bulk selection.
  toggle: (key: string) => void;
  // togglePage selects the whole page's keys, or clears them if all are
  // already selected (the header checkbox).
  togglePage: (keys: string[]) => void;
  // selectAllMatching replaces the selection with every filter-matching key
  // (FR-3.1); TestTable owns the query and hands the result here.
  selectAllMatching: (keys: string[]) => void;
}

const SelectionContext = createContext<SelectionApi | null>(null);

export function useSelection(): SelectionApi {
  const ctx = useContext(SelectionContext);
  if (!ctx) {
    throw new Error("useSelection must be used within a SelectionProvider");
  }
  return ctx;
}

export function SelectionProvider({ children }: { children: ReactNode }) {
  const [selectedSet, setSelectedSet] = useState<Set<string>>(new Set());
  const [selectedKey, setSelectedKey] = useState<string | null>(null);

  const toggle = useCallback((key: string) => {
    setSelectedSet((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  const togglePage = useCallback((keys: string[]) => {
    setSelectedSet((prev) => {
      const allSelected = keys.every((k) => prev.has(k));
      const next = new Set(prev);
      if (allSelected) {
        for (const k of keys) next.delete(k);
      } else {
        for (const k of keys) next.add(k);
      }
      return next;
    });
  }, []);

  const selectAllMatching = useCallback((keys: string[]) => {
    setSelectedSet(new Set(keys));
  }, []);

  const api = useMemo<SelectionApi>(
    () => ({
      selectedSet,
      selectedKey,
      setSelectedSet,
      setSelectedKey,
      toggle,
      togglePage,
      selectAllMatching,
    }),
    [selectedSet, selectedKey, toggle, togglePage, selectAllMatching],
  );

  return (
    <SelectionContext.Provider value={api}>
      {children}
    </SelectionContext.Provider>
  );
}
