import { useQuery } from "@tanstack/react-query";
import { ScanDuplicates, ScanPreconditionDuplicates } from "../api";
import type {
  DuplicateReport,
  PreconditionDuplicateReport,
} from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useDuplicates loads the tests-mode duplicate scan for DuplicatesView. The key
// is stable (Phase 4c): a mutation refreshes it via invalidateProfileData.
//
// `enabled` mirrors the old guard: only "tests" mode scans through this path
// (preconditions mode has its own query below).
//
// placeholderData keeps the previous report visible while the next scan loads,
// matching the old "only replace the report on success" behaviour.
export function useDuplicates(
  profileId: string,
  mode: "tests" | "preconditions",
) {
  return useQuery({
    queryKey: keys.duplicates(profileId),
    queryFn: async () =>
      (await call(() => ScanDuplicates(profileId))) as unknown as DuplicateReport,
    enabled: !!profileId && mode === "tests",
    placeholderData: (prev) => prev,
  });
}

// usePreconditionDuplicates loads the preconditions-mode duplicate scan. It
// replaces PreconditionDuplicatesView's imperative fetch effect; its key nests
// under "duplicates" so invalidateProfileData refreshes it with the tests scan.
export function usePreconditionDuplicates(profileId: string) {
  return useQuery({
    queryKey: keys.preconditionDuplicates(profileId),
    queryFn: async () =>
      (await call(() =>
        ScanPreconditionDuplicates(profileId),
      )) as unknown as PreconditionDuplicateReport,
    enabled: !!profileId,
    placeholderData: (prev) => prev,
  });
}
