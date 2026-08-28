import { useQuery } from "@tanstack/react-query";
import { GetStatistics } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useStatistics loads the Dashboard's per-profile statistics (FR-9), computed
// entirely from the local store. The key is stable (Phase 4c) — the filter
// params are part of the key so each combination caches separately; a
// sync/commit refreshes it via invalidateProfileData, and the manual "Refresh"
// button calls refetch().
//
// placeholderData keeps the previous numbers visible while the next filter
// combination loads, matching the old "only replace the panel on success"
// behaviour.
export function useStatistics(
  profileId: string,
  folder: string,
  component: string,
  status: string,
) {
  return useQuery({
    queryKey: [...keys.stats(profileId), folder, component, status],
    queryFn: () => call(() => GetStatistics(profileId, folder, component, status)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}
