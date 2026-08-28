import { useQuery } from "@tanstack/react-query";
import { ListTests } from "../api";
import type { TestQuery } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useTests loads one page of the browse grid (audit A3). It replaces the manual
// fetch effect + page/loading/error state in TestTable.
//
// The key is stable (Phase 4c): a mutation refreshes the grid by invalidating
// the [profileId, "tests"] prefix via invalidateProfileData.
//
// placeholderData keeps the previous page visible while the next one loads,
// matching the old "only replace the grid on success" behaviour.
export function useTests(profileId: string, params: TestQuery) {
  return useQuery({
    queryKey: keys.tests(profileId, params),
    queryFn: () => call(() => ListTests(profileId, params)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}
