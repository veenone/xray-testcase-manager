import { useQuery } from "@tanstack/react-query";
import {
  GetRequirementLinks,
  ListRequirementsWithCoverage,
  ListTestsForRequirement,
} from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useRequirements loads the requirement coverage list for RequirementsView's
// master list. The key is stable (Phase 4c): a sync/commit/mutation refreshes it
// via invalidateProfileData. placeholderData keeps the previous list visible
// while the next one loads.
export function useRequirements(profileId: string) {
  return useQuery({
    queryKey: keys.requirements(profileId),
    queryFn: () => call(() => ListRequirementsWithCoverage(profileId)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}

// useRequirementTests loads the tests covering the selected requirement. Keyed
// under the "requirements" prefix so invalidateProfileData refreshes it with the
// list on any mutation.
export function useRequirementTests(profileId: string, requirementKey: string) {
  return useQuery({
    queryKey: keys.requirementTests(profileId, requirementKey),
    queryFn: () => call(() => ListTestsForRequirement(profileId, requirementKey)),
    enabled: !!profileId && !!requirementKey,
  });
}

// useRequirementLinks loads the requirement-to-requirement links for the
// selected requirement. Same prefix nesting as useRequirementTests.
export function useRequirementLinks(profileId: string, requirementKey: string) {
  return useQuery({
    queryKey: keys.requirementLinks(profileId, requirementKey),
    queryFn: () => call(() => GetRequirementLinks(profileId, requirementKey)),
    enabled: !!profileId && !!requirementKey,
  });
}
