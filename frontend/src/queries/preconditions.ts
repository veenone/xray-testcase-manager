import { useQuery } from "@tanstack/react-query";
import { ListPreconditionsWithUsage, ListTestsForPrecondition } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// usePreconditions loads the Preconditions view's list with per-precondition
// usage counts (audit A3, Phase 3). The key is stable (Phase 4c): a
// sync/commit/mutation refreshes it via invalidateProfileData. placeholderData
// keeps the previous list visible while the next loads.
export function usePreconditions(profileId: string) {
  return useQuery({
    queryKey: keys.preconditions(profileId),
    queryFn: () => call(() => ListPreconditionsWithUsage(profileId)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}

// usePreconditionTests loads the tests linked to the selected precondition
// (the "Used by" list). Its key is nested under the "preconditions" prefix, so
// invalidateProfileData refreshes it alongside the list on any mutation.
export function usePreconditionTests(profileId: string, preconditionKey: string) {
  return useQuery({
    queryKey: keys.preconditionTests(profileId, preconditionKey),
    queryFn: () => call(() => ListTestsForPrecondition(profileId, preconditionKey)),
    enabled: !!profileId && !!preconditionKey,
  });
}
