import { useQuery } from "@tanstack/react-query";
import { ListTestCallLinks } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useTestCallLinks loads TestCallsView's flat call-link list (audit A3
// follow-up). It replaces the manual fetch effect + links/loading/error state.
//
// The global refresh now comes through invalidateProfileData (which invalidates
// keys.testCalls). `bridge` carries only TestCallsView's LOCAL counters
// (detailVersion + reload) folded into the key, so its detail-panel edits and
// post-partial-sync reload refetch the list without a global bump.
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

// useTestCallerKeys derives the set of test keys that call another test — used
// by TestTable to badge caller rows. It nests under the testCalls key prefix, so
// invalidateProfileData refreshes it with the call-link list.
export function useTestCallerKeys(profileId: string) {
  return useQuery({
    queryKey: [...keys.testCalls(profileId), "callers"],
    queryFn: async () => {
      const links = await call(() => ListTestCallLinks(profileId));
      return new Set((links ?? []).map((l) => l.callerKey));
    },
    enabled: !!profileId,
  });
}
