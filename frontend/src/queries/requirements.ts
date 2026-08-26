import { useQuery } from "@tanstack/react-query";
import { ListRequirementsWithCoverage } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useRequirements loads the requirement coverage list for RequirementsView's
// master list. It replaces the manual fetch effect + list/loading/error state.
//
// `refreshKey` is the strangler bridge: during the migration, syncs/commits
// still bump a global counter to force a refresh, so we fold it into the query
// key -- a bump changes the key and refetches the list. Phase 4 replaces this
// with targeted invalidation of keys.requirements(profileId) and drops the
// parameter.
//
// placeholderData keeps the previous list visible while the next one loads,
// matching the old "only replace the list on success" behaviour.
export function useRequirements(profileId: string, refreshKey: number) {
  return useQuery({
    queryKey: [...keys.requirements(profileId), refreshKey],
    queryFn: () => call(() => ListRequirementsWithCoverage(profileId)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}
