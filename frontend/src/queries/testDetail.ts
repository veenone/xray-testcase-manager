import { useQuery } from "@tanstack/react-query";
import {
  GetTest,
  GetTestBugs,
  GetTestContainers,
  GetTestMeta,
  GetTestRequirements,
  GetTestReview,
  GetTestRunHistory,
  ListRequirementsWithCoverage,
} from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// Isolated, read-only sections of the Test detail panel (audit A3, Phase 2b).
// These are lazy loads with no optimistic updates, so they migrate cleanly to
// their own queries — decoupling them from TestDetail's main Promise.all
// waterfall. `reload` folds TestDetail's version + localReloadKey counters into
// the key as the migration bridge (see keys.ts).

// useTest is the panel's primary read — the `test` itself. Unlike meta/history
// it DOES carry optimistic updates (field edit, folder move, status
// transition), which TestDetail applies via queryClient.setQueryData on this
// same key. `reload` keeps the version/localReloadKey refetch behaviour of the
// old load effect.
export function useTest(profileId: string, testKey: string, reload: string) {
  return useQuery({
    queryKey: keys.testDetail(profileId, testKey, reload),
    queryFn: () => call(() => GetTest(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

export function useTestMeta(profileId: string, testKey: string, reload: string) {
  return useQuery({
    queryKey: keys.testMeta(profileId, testKey, reload),
    queryFn: () => call(() => GetTestMeta(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

export function useTestRunHistory(
  profileId: string,
  testKey: string,
  reload: string,
) {
  return useQuery({
    queryKey: keys.testRunHistory(profileId, testKey, reload),
    queryFn: () => call(() => GetTestRunHistory(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

// useTestBugs loads the Jira bugs linked to this Test (read-only display). It is
// disabled for not-yet-created "NEW-" tests, matching the old load's skipBugs
// guard (a placeholder Test has no remote bugs to fetch).
export function useTestBugs(profileId: string, testKey: string, reload: string) {
  return useQuery({
    queryKey: keys.testBugs(profileId, testKey, reload),
    queryFn: () => call(() => GetTestBugs(profileId, testKey)),
    enabled: !!profileId && !!testKey && !testKey.startsWith("NEW-"),
  });
}

// useTestReview loads this Test's review verdict. It carries an optimistic
// update: TestDetail's setVerdict handler re-fetches after SetTestReview and
// patches this key via queryClient.setQueryData. `reload` preserves the old
// load's version/localReloadKey refresh.
export function useTestReview(
  profileId: string,
  testKey: string,
  reload: string,
) {
  return useQuery({
    queryKey: keys.testReview(profileId, testKey, reload),
    queryFn: () => call(() => GetTestReview(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

// useTestRequirements loads the requirements this Test covers. It carries an
// optimistic update: TestDetail's applyRequirements handler re-fetches after
// SetTestRequirements and patches this key via queryClient.setQueryData.
export function useTestRequirements(
  profileId: string,
  testKey: string,
  reload: string,
) {
  return useQuery({
    queryKey: keys.testRequirements(profileId, testKey, reload),
    queryFn: () => call(() => GetTestRequirements(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

// useTestContainers loads the Test Sets / Plans / Executions this Test belongs
// to. It carries an optimistic update: TestDetail's deallocateContainer handler
// re-fetches after DeallocateTests and patches this key via setQueryData.
export function useTestContainers(
  profileId: string,
  testKey: string,
  reload: string,
) {
  return useQuery({
    queryKey: keys.testContainers(profileId, testKey, reload),
    queryFn: () => call(() => GetTestContainers(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

// useRequirementCoverage loads the profile-wide requirement coverage list that
// TestDetail uses to populate its requirement picker (read-only here). It is
// profile-scoped, so it is NOT keyed on the test key and caches across test
// switches; `reload` preserves the old load's version/localReloadKey refresh.
export function useRequirementCoverage(profileId: string, reload: string) {
  return useQuery({
    queryKey: keys.requirementCoverage(profileId, reload),
    queryFn: () => call(() => ListRequirementsWithCoverage(profileId)),
    enabled: !!profileId,
  });
}
