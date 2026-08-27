import { useQuery } from "@tanstack/react-query";
import { ListCanonicalRequirements } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useCanonicalRequirements loads the Coverage view's left-hand canonical
// (functional-requirement) list (audit A3, Phase 3). refreshKey is the
// migration bridge (a sync/commit or a canonical add/delete refetches);
// Phase 4 replaces it with targeted invalidation of
// keys.canonicalRequirements(profileId). placeholderData keeps the previous
// list visible while the next loads.
export function useCanonicalRequirements(
  profileId: string,
  refreshKey: number,
) {
  return useQuery({
    queryKey: [...keys.canonicalRequirements(profileId), refreshKey],
    queryFn: () => call(() => ListCanonicalRequirements(profileId)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}
