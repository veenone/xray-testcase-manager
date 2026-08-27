import { useQuery } from "@tanstack/react-query";
import {
  GetSyncState,
  ListComponents,
  ListContainers,
  ListFolders,
} from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// App-shell profile-scoped loads (Phase 4b). These replace imperative fetch
// effects in App.tsx. Their keys are stable (Phase 4c): a mutation refreshes
// them via invalidateProfileData, not a counter folded into the key.
// placeholderData keeps the previous value visible while the next loads,
// matching the old "only replace on success" behaviour.

// useSyncState loads the profile's last-sync summary shown in the header.
export function useSyncState(profileId: string) {
  return useQuery({
    queryKey: keys.syncState(profileId),
    queryFn: () => call(() => GetSyncState(profileId)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}

// useFolders loads the Test Repository folder tree (FR-13.3).
export function useFolders(profileId: string) {
  return useQuery({
    queryKey: keys.folders(profileId),
    queryFn: () => call(() => ListFolders(profileId)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}

// useComponents loads the distinct components backing the group-by-component
// sidebar. Only fetched while that grouping is active.
export function useComponents(profileId: string, groupBy: string) {
  return useQuery({
    queryKey: keys.components(profileId),
    queryFn: () => call(() => ListComponents(profileId)),
    enabled: !!profileId && groupBy === "component",
    placeholderData: (prev) => prev,
  });
}

// useGroupContainers loads the Test Sets / Plans backing the group-by sidebar.
// Only fetched while grouping by testset/testplan. Once useContainers also drops
// its refreshKey bridge, both key on [profileId, "containers", kind] and the
// same kind dedupes to one entry.
export function useGroupContainers(profileId: string, groupBy: string) {
  return useQuery({
    queryKey: [...keys.containers(profileId), groupBy],
    queryFn: () => call(() => ListContainers(profileId, groupBy)),
    enabled: !!profileId && (groupBy === "testset" || groupBy === "testplan"),
    placeholderData: (prev) => prev,
  });
}
