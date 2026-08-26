import { useQuery } from "@tanstack/react-query";
import { ListTestCallLinks } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useTestCallLinks loads TestCallsView's flat call-link list (audit A3
// follow-up). It replaces the manual fetch effect + links/loading/error state.
//
// `bridge` is the strangler bridge: during the migration, TestCallsView still
// folds its three reload counters (refreshKey, detailVersion, reload) into a
// single string, so a bump to any of them changes the query key and refetches.
// Phase 4 replaces this with targeted invalidation of keys.testCalls(profileId)
// and drops the parameter.
//
// placeholderData keeps the previous list visible while the next one loads,
// matching the old "only replace the list on success" behaviour.
export function useTestCallLinks(profileId: string, bridge: string) {
  return useQuery({
    queryKey: [...keys.testCalls(profileId), bridge],
    queryFn: () => call(() => ListTestCallLinks(profileId)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}
