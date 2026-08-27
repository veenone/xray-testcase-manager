import { useQuery } from "@tanstack/react-query";
import { ListContainers } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useContainers loads the Containers view's main list for the selected
// container kind (Test Set / Test Plan / Test Execution). `kind` is folded
// into the key as a query param (like folder/component were for
// useStatistics). refreshKey is folded in as the migration bridge (a
// sync/commit bumps it to force a refresh); Phase 4 replaces it with
// targeted invalidation of keys.containers(profileId). placeholderData keeps
// the previous list visible while the next loads.
export function useContainers(profileId: string, kind: string, refreshKey: number) {
  return useQuery({
    queryKey: [...keys.containers(profileId), kind, refreshKey],
    queryFn: () => call(() => ListContainers(profileId, kind)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}
