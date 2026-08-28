import { useQuery } from "@tanstack/react-query";
import {
  GetRequirementTraceability,
  GetStatistics,
  GetSubTaskTraceability,
  GetTraceabilitySankey,
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
