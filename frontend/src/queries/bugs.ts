import { useQuery } from "@tanstack/react-query";
import { ListBugsWithTests, ListTestsForBug } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// BugsPanel's reads (Phase 4c). The bug list is refreshed by
// invalidateProfileData (and a bugs-only sync calls refetch()); the selected
// bug's linked tests nest under the "bugs" prefix so they refresh with it.

// useBugs loads the profile's bugs with their linked test keys.
export function useBugs(profileId: string) {
  return useQuery({
    queryKey: keys.bugs(profileId),
    queryFn: () => call(() => ListBugsWithTests(profileId)),
    enabled: !!profileId,
  });
}

// useBugTests loads the tests linked to the selected bug.
export function useBugTests(profileId: string, bugKey: string) {
  return useQuery({
    queryKey: keys.bugTests(profileId, bugKey),
    queryFn: () => call(() => ListTestsForBug(profileId, bugKey)),
    enabled: !!profileId && !!bugKey,
  });
}
