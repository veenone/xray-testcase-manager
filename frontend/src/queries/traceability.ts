import { useQuery } from "@tanstack/react-query";
import {
  GetExecutionsForPlans,
  GetRequirementTraceability,
  GetStatistics,
  GetSubTaskTraceability,
  GetTraceabilitySankey,
  ListBugsWithTests,
  ListContainers,
  ListRequirementsWithCoverage,
} from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// TraceabilityTabs' read-only loads (Phase 4c). All keys sit under the
// [profileId, "traceability"] prefix so invalidateProfileData refreshes the
// whole view; the selection-dependent Sankeys re-key on their filter arrays.

// useTraceabilityStats loads the unfiltered profile statistics shown above the
// Sankeys.
export function useTraceabilityStats(profileId: string) {
  return useQuery({
    queryKey: keys.traceabilityStats(profileId),
    queryFn: () => call(() => GetStatistics(profileId, "", "", "")),
    enabled: !!profileId,
  });
}

// useTraceabilityReqOptions loads the requirement filter options.
export function useTraceabilityReqOptions(profileId: string) {
  return useQuery({
    queryKey: keys.traceabilityReqOptions(profileId),
    queryFn: () => call(() => ListRequirementsWithCoverage(profileId)),
    enabled: !!profileId,
  });
}

// useRequirementSankey loads the requirement-coverage Sankey for the selected
// requirements.
export function useRequirementSankey(profileId: string, reqSel: string[]) {
  return useQuery({
    queryKey: keys.requirementSankey(profileId, reqSel),
    queryFn: () => call(() => GetRequirementTraceability(profileId, reqSel)),
    enabled: !!profileId,
  });
}

// usePlanExecSankey loads the plan -> execution -> status Sankey for the
// selected plans/executions and the cross-project toggle.
export function usePlanExecSankey(
  profileId: string,
  planSel: string[],
  execSel: string[],
  crossProject: boolean,
) {
  return useQuery({
    queryKey: keys.planExecSankey(profileId, planSel, execSel, crossProject),
    queryFn: () =>
      call(() =>
        GetTraceabilitySankey(profileId, planSel, execSel, crossProject),
      ),
    enabled: !!profileId,
  });
}

// useSubTaskSankey loads the sub-task parent -> execution -> status Sankey.
export function useSubTaskSankey(
  profileId: string,
  parentSel: string[],
  crossMembers: boolean,
) {
  return useQuery({
    queryKey: keys.subTaskSankey(profileId, parentSel, crossMembers),
    queryFn: () =>
      call(() => GetSubTaskTraceability(profileId, parentSel, crossMembers)),
    enabled: !!profileId,
  });
}

// useTraceabilityPlanContainers loads the Test Plan options for the plan filter.
export function useTraceabilityPlanContainers(profileId: string) {
  return useQuery({
    queryKey: keys.traceabilityPlanContainers(profileId),
    queryFn: () => call(() => ListContainers(profileId, "testplan")),
    enabled: !!profileId,
  });
}

// useTraceabilityExecContainers loads the Test Execution containers; the view
// derives the distinct parent options from them.
export function useTraceabilityExecContainers(profileId: string) {
  return useQuery({
    queryKey: keys.traceabilityExecContainers(profileId),
    queryFn: () => call(() => ListContainers(profileId, "testexec")),
    enabled: !!profileId,
  });
}

// useTraceabilityExecutions loads the executions that cascade from the selected
// plans; re-keys on the plan selection.
export function useTraceabilityExecutions(
  profileId: string,
  planSel: string[],
) {
  return useQuery({
    queryKey: keys.traceabilityExecutions(profileId, planSel),
    queryFn: () => call(() => GetExecutionsForPlans(profileId, planSel)),
    enabled: !!profileId,
    // Keep the previous executions visible while a new plan selection loads, so
    // the dropdown count doesn't blink to 0 mid-cascade. The prune effect keys
    // on the query data ref, which placeholderData holds stable during the
    // refetch, so it still prunes only once the new options land.
    placeholderData: (prev) => prev,
  });
}

// useTraceabilityBugs loads the bugs-with-tests list for the cross-project view;
// only fetched while the cross-project toggle is on.
export function useTraceabilityBugs(profileId: string, crossProject: boolean) {
  return useQuery({
    queryKey: keys.traceabilityBugs(profileId),
    queryFn: () => call(() => ListBugsWithTests(profileId)),
    enabled: !!profileId && crossProject,
  });
}
