import { useQuery } from "@tanstack/react-query";
import { GetStatistics } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useStatistics loads the Dashboard's per-profile statistics (FR-9), computed
// entirely from the local store.
//
// `bridge` is the strangler bridge: it folds Dashboard's refreshKey (bumped by
// sync/commit) and nonce (the manual "Refresh" button) counters into a single
// string, so a bump changes the key and refetches. Phase 4 replaces this with
// targeted invalidation of keys.stats(profileId) and drops the parameter.
//
// placeholderData keeps the previous numbers visible while the next filter
// combination loads, matching the old "only replace the panel on success"
// behaviour.
export function useStatistics(
  profileId: string,
  folder: string,
  component: string,
  status: string,
  bridge: string,
) {
  return useQuery({
    queryKey: [...keys.stats(profileId), folder, component, status, bridge],
    queryFn: () => call(() => GetStatistics(profileId, folder, component, status)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}
