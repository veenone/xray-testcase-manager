import { useQuery } from "@tanstack/react-query";
import { GetTestMeta, GetTestRunHistory } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// Isolated, read-only sections of the Test detail panel (audit A3, Phase 2b).
// These are lazy loads with no optimistic updates, so they migrate cleanly to
// their own queries — decoupling them from TestDetail's main Promise.all
// waterfall. `reload` folds TestDetail's version + localReloadKey counters into
// the key as the migration bridge (see keys.ts).

export function useTestMeta(profileId: string, testKey: string, reload: string) {
  return useQuery({
    queryKey: keys.testMeta(profileId, testKey, reload),
    queryFn: () => call(() => GetTestMeta(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

export function useTestRunHistory(
  profileId: string,
  testKey: string,
  reload: string,
) {
  return useQuery({
    queryKey: keys.testRunHistory(profileId, testKey, reload),
    queryFn: () => call(() => GetTestRunHistory(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}
