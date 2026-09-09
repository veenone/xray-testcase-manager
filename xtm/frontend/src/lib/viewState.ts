// frontend/src/lib/viewState.ts
import { useCallback, useState } from "react";

// Module-level store: survives component unmount (so a view restores its state
// when you switch away and back), but NOT a full app reload. Keyed by
// "<profileId>:<viewKey>:<fieldKey>".
const store = new Map<string, unknown>();

function k(profileId: string, viewKey: string, fieldKey: string): string {
  return `${profileId}:${viewKey}:${fieldKey}`;
}

// useViewState behaves like useState but persists the value in the module store,
// so leaving and returning to a view restores the field. Pass the active
// profileId, a stable viewKey (e.g. "bugs"), and a fieldKey (e.g. "selected").
export function useViewState<T>(
  profileId: string,
  viewKey: string,
  fieldKey: string,
  initial: T,
): [T, (next: T | ((prev: T) => T)) => void] {
  const key = k(profileId, viewKey, fieldKey);
  const read = () => (store.has(key) ? (store.get(key) as T) : initial);
  // The key is tracked alongside the value so a key change re-reads the store.
  // Seeding useState from the store only runs on mount, and a profile switch
  // does not remount these views: the key moved to the new profile while the
  // value stayed on the old one, so the Preconditions detail panel went on
  // showing the previous profile's precondition. This is React's documented
  // "adjust state when a prop changes" pattern: set during render, which React
  // re-runs immediately without committing the discarded pass.
  const [held, setHeld] = useState<{ key: string; value: T }>(() => ({
    key,
    value: read(),
  }));
  if (held.key !== key) {
    setHeld({ key, value: read() });
  }
  const value = held.key === key ? held.value : read();
  const set = useCallback(
    (next: T | ((prev: T) => T)) => {
      setHeld((prev) => {
        const base = prev.key === key ? prev.value : (store.get(key) as T);
        const resolved =
          typeof next === "function" ? (next as (p: T) => T)(base) : next;
        store.set(key, resolved);
        // Returning the same object when nothing moved keeps React's bail-out,
        // which the plain useState this replaced got for free. Without it an
        // effect that re-sets an unchanged value (the Preconditions view has
        // three) re-rendered forever, since a fresh wrapper object is never
        // Object.is-equal to the last one.
        if (prev.key === key && Object.is(prev.value, resolved)) return prev;
        return { key, value: resolved };
      });
    },
    [key],
  );
  return [value, set];
}

// clearViewState drops all stored state for a profile. Call this when the active
// profile changes so a new profile does not inherit stale selections.
export function clearViewState(profileId: string): void {
  const prefix = `${profileId}:`;
  for (const key of [...store.keys()]) {
    if (key.startsWith(prefix)) store.delete(key);
  }
}
