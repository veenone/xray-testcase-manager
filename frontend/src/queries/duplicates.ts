import { useQuery } from "@tanstack/react-query";
import { ScanDuplicates } from "../api";
import type { DuplicateReport } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useDuplicates loads the tests-mode duplicate scan for DuplicatesView.
//
// `refreshKey` is the strangler bridge: during the migration, callers still
// bump a global counter (and the view's own "load" callers) to force a
// refresh, so we fold it into the query key — a bump changes the key and
// refetches. Phase 4 replaces this with targeted invalidation of
// keys.duplicates(profileId) and drops the parameter.
//
// `enabled` mirrors the old guard: only "tests" mode scans through this path
// (preconditions mode has its own self-contained view).
//
// placeholderData keeps the previous report visible while the next scan
// loads, matching the old "only replace the report on success" behaviour.
export function useDuplicates(
  profileId: string,
  mode: string,
  refreshKey: number,
) {
  return useQuery({
    queryKey: [...keys.duplicates(profileId), refreshKey],
    queryFn: async () =>
      (await call(() => ScanDuplicates(profileId))) as unknown as DuplicateReport,
    enabled: !!profileId && mode === "tests",
    placeholderData: (prev) => prev,
  });
}
