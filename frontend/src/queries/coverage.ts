import { useQuery } from "@tanstack/react-query";
import {
  DetectStaleCoverageMappings,
  GetCoverageProjectStatus,
  GetCoverageRelationSankey,
  GetCoverageReport,
  GetParamModel,
  ListCanonicalReuse,
  ListCanonicalRequirements,
  ListCoverageGaps,
  ListCoverageProjects,
} from "../api";
import type {
  CoverageGap,
  CoverageReport,
  ParamModel,
  ProjectConfig,
  ProjectCoverageRow,
  ReuseRow,
  Sankey,
  StaleMapping,
} from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useCanonicalRequirements loads the Coverage view's left-hand canonical
// (functional-requirement) list (audit A3, Phase 3). The key is stable (Phase
// 4c): a sync/commit or a canonical add/delete refreshes it via
// invalidateProfileData. placeholderData keeps the previous list visible while
// the next loads.
export function useCanonicalRequirements(profileId: string) {
  return useQuery({
    queryKey: keys.canonicalRequirements(profileId),
    queryFn: () => call(() => ListCanonicalRequirements(profileId)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}

// CoverageDetail bundles the five per-selection reads CoverageView loads
// together for the chosen canonical + version.
export interface CoverageDetail {
  model: ParamModel | null;
  report: CoverageReport | null;
  gaps: CoverageGap[];
  reuse: ReuseRow[];
  stale: StaleMapping[];
}

// useCoverageDetail replaces CoverageView's imperative Promise.all. It fetches
// all five reads together for the selected canonical + version; keyed under the
// "canonicalRequirements" prefix so invalidateProfileData refreshes it.
export function useCoverageDetail(
  profileId: string,
  canonicalKey: string,
  versionId: string,
) {
  return useQuery({
    queryKey: keys.coverageDetail(profileId, canonicalKey, versionId),
    queryFn: async (): Promise<CoverageDetail> => {
      const [model, report, gaps, reuse, stale] = await Promise.all([
        call(() => GetParamModel(profileId, versionId)),
        call(() => GetCoverageReport(profileId, versionId)),
        call(() => ListCoverageGaps(profileId, versionId)),
        call(() => ListCanonicalReuse(profileId, canonicalKey)),
        call(() => DetectStaleCoverageMappings(profileId, versionId)),
      ]);
      return {
        model: model ?? null,
        report: report ?? null,
        gaps: gaps ?? [],
        reuse: reuse ?? [],
        stale: stale ?? [],
      };
    },
    enabled: !!profileId && !!canonicalKey && !!versionId,
  });
}

// CoverageMapData bundles CoverageMap's three reads. `projects` seeds an
// editable draft in the component.
export interface CoverageMapData {
  rows: ProjectCoverageRow[];
  sankey: Sankey;
  projects: ProjectConfig[];
}

// useCoverageMapData replaces CoverageMap's imperative loadAll Promise.all.
// Keyed under the "canonicalRequirements" prefix so invalidateProfileData
// refreshes it; placeholderData keeps the map visible while a refetch runs.
export function useCoverageMapData(profileId: string) {
  return useQuery({
    queryKey: keys.coverageMap(profileId),
    queryFn: async (): Promise<CoverageMapData> => {
      const [rows, sankey, projects] = await Promise.all([
        call(() => GetCoverageProjectStatus(profileId)),
        call(() => GetCoverageRelationSankey(profileId)),
        call(() => ListCoverageProjects(profileId)),
      ]);
      return {
        rows: rows ?? [],
        sankey: sankey ?? { nodes: [], links: [] },
        projects: projects ?? [],
      };
    },
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}
