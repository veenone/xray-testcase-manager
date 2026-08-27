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
// effects in App.tsx that re-ran off the refreshKey counter. `refreshKey` is
// folded into each key as the migration bridge (a sync/commit/mutation bumps it
// to force a refresh); Phase 4c replaces it with targeted invalidation and drops
// the parameter. placeholderData keeps the previous value visible while the next
// loads, matching the old "only replace on success" behaviour.

// useSyncState loads the profile's last-sync summary shown in the header.
export function useSyncState(profileId: string, refreshKey: number) {
  return useQuery({
    queryKey: [...keys.syncState(profileId), refreshKey],
    queryFn: () => call(() => GetSyncState(profileId)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}

// useFolders loads the Test Repository folder tree (FR-13.3).
export function useFolders(profileId: string, refreshKey: number) {
  return useQuery({
    queryKey: [...keys.folders(profileId), refreshKey],
    queryFn: () => call(() => ListFolders(profileId)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}

// useComponents loads the distinct components backing the group-by-component
// sidebar. Only fetched while that grouping is active.
export function useComponents(
  profileId: string,
  groupBy: string,
  refreshKey: number,
) {
  return useQuery({
    queryKey: [...keys.components(profileId), refreshKey],
    queryFn: () => call(() => ListComponents(profileId)),
    enabled: !!profileId && groupBy === "component",
    placeholderData: (prev) => prev,
  });
}

// useGroupContainers loads the Test Sets / Plans backing the group-by sidebar.
// Only fetched while grouping by testset/testplan. Shares keys.containers with
// the Containers view's useContainers, so the same kind dedupes to one entry.
export function useGroupContainers(
  profileId: string,
  groupBy: string,
  refreshKey: number,
) {
  return useQuery({
    queryKey: [...keys.containers(profileId), groupBy, refreshKey],
    queryFn: () => call(() => ListContainers(profileId, groupBy)),
    enabled: !!profileId && (groupBy === "testset" || groupBy === "testplan"),
    placeholderData: (prev) => prev,
  });
}
