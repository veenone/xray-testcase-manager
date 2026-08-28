import { useQuery } from "@tanstack/react-query";
import {
  GetContainerBoard,
  GetExecutionMembersWithRuns,
  GetRunRollup,
  ListBugsForContainer,
  ListContainers,
} from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useContainers loads the Containers view's main list for the selected
// container kind (Test Set / Test Plan / Test Execution). `kind` is folded into
// the key as a query param; a sync/commit/mutation refreshes it via
// invalidateProfileData (keys.containers prefix). placeholderData keeps the
// previous list visible while the next loads. Shares its key shape with App's
// useGroupContainers, so the same kind dedupes to one cache entry.
export function useContainers(profileId: string, kind: string) {
  return useQuery({
    queryKey: [...keys.containers(profileId), kind],
    queryFn: () => call(() => ListContainers(profileId, kind)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}

// ContainersView's per-selection detail reads (Phase 4c). All nest under the
// "containers" prefix so invalidateProfileData refreshes them with the list.

// useContainerBoard loads the selected container's board (rows + run results).
export function useContainerBoard(profileId: string, containerKey: string) {
  return useQuery({
    queryKey: keys.containerBoard(profileId, containerKey),
    queryFn: () => call(() => GetContainerBoard(profileId, containerKey)),
    enabled: !!profileId && !!containerKey,
  });
}

// useContainerBugs loads the defects reached through the container's member
// tests (including cross-project members the per-test panel can't show, #219).
export function useContainerBugs(profileId: string, containerKey: string) {
  return useQuery({
    queryKey: keys.containerBugs(profileId, containerKey),
    queryFn: () => call(() => ListBugsForContainer(profileId, containerKey)),
    enabled: !!profileId && !!containerKey,
  });
}

// useContainerMembers loads run details for a Test Execution's member tests;
// only fetched for the testexec kind.
export function useContainerMembers(
  profileId: string,
  containerKey: string,
  kind: string,
) {
  return useQuery({
    queryKey: keys.containerMembers(profileId, containerKey),
    queryFn: () =>
      call(() => GetExecutionMembersWithRuns(profileId, containerKey)),
    enabled: !!profileId && !!containerKey && kind === "testexec",
  });
}

// useContainerRollup loads the run roll-up for a Test Plan / Test Set; not used
// for Test Executions (they show per-row run detail instead).
export function useContainerRollup(
  profileId: string,
  containerKey: string,
  kind: string,
) {
  return useQuery({
    queryKey: keys.containerRollup(profileId, containerKey),
    queryFn: () => call(() => GetRunRollup(profileId, containerKey)),
    enabled: !!profileId && !!containerKey && kind !== "testexec",
  });
}
