import type { QueryClient } from "@tanstack/react-query";
import { keys } from "./keys";

// invalidateProfileData invalidates every profile-scoped list / dashboard query
// that App's refreshKey counter used to bridge (Phase 4c). It is the targeted
// replacement for a global `setRefreshKey((k) => k + 1)` bump: each of these
// families is keyed [profileId, "<name>", …], so invalidating the prefix
// refetches all of that family's active variations.
//
// It deliberately does NOT touch the [profileId, "test", key] detail queries
// (those refresh via TestDetail's own signal) nor [profileId, "pending"] (that
// is invalidated separately by reloadPending) — matching exactly what the old
// refreshKey bump refetched.
export function invalidateProfileData(qc: QueryClient, profileId: string) {
  if (!profileId) return;
  const families = [
    // useTests is keyed [profileId, "tests", params, …]; the 2-element prefix
    // matches every filter/sort/group variation in the cache.
    [profileId, "tests"] as const,
    keys.folders(profileId),
    keys.syncState(profileId),
    keys.components(profileId),
    keys.containers(profileId),
    keys.preconditions(profileId),
    keys.requirements(profileId),
    keys.duplicates(profileId),
    keys.stats(profileId),
    keys.canonicalRequirements(profileId),
    keys.testCalls(profileId),
  ];
  for (const queryKey of families) {
    qc.invalidateQueries({ queryKey });
  }
}
