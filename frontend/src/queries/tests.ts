import { useQuery } from "@tanstack/react-query";
import { ListTests } from "../api";
import type { TestQuery } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useTests loads one page of the browse grid (audit A3). It replaces the manual
// fetch effect + page/loading/error state in TestTable.
//
// `refreshKey` is the strangler bridge: during the migration, mutations still
// bump a global counter to force a refresh, so we fold it into the query key —
// a bump changes the key and refetches the page. Phase 4 replaces this with
// targeted invalidation of keys.tests(profileId) and drops the parameter.
//
// placeholderData keeps the previous page visible while the next one loads,
// matching the old "only replace the grid on success" behaviour.
export function useTests(
  profileId: string,
  params: TestQuery,
  refreshKey: number,
) {
  return useQuery({
    queryKey: [...keys.tests(profileId, params), refreshKey],
    queryFn: () => call(() => ListTests(profileId, params)),
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}
