import { useQuery } from "@tanstack/react-query";
import { ListPreconditionsWithUsage } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// usePreconditions loads the Preconditions view's list with per-precondition
// usage counts (audit A3, Phase 3). refreshKey is folded into the key as the
// migration bridge (a sync/commit bumps it to force a refresh); Phase 4
// replaces it with targeted invalidation of keys.preconditions(profileId).
// placeholderData keeps the previous list visible while the next loads.
export function usePreconditions(profileId: string, refreshKey: number) {
  return useQuery({
    queryKey: [...keys.preconditions(profileId), refreshKey],
    queryFn: () => call(() => ListPreconditionsWithUsage(profileId)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}
