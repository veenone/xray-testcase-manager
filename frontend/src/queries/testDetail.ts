import { useQuery } from "@tanstack/react-query";
import {
  GetTest,
  GetTestBugs,
  GetTestContainers,
  GetTestMeta,
  GetTestPreconditions,
  GetTestRequirements,
  GetTestReview,
  GetTestRunHistory,
  ListAllPreconditions,
  ListRequirementsWithCoverage,
} from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// The Test detail panel's per-section queries (audit A3, Phase 2b–2g). Each key
// is stable: parent-driven reloads are handled by TestDetail invalidating the
// [profileId, "test", key] prefix (and the two profile-scoped pools), not by a
// counter folded into the key (Phase 4a).

// useTest is the panel's primary read — the `test` itself. Unlike meta/history
// it DOES carry optimistic updates (field edit, folder move, status
// transition), which TestDetail applies via queryClient.setQueryData on this
// same key.
export function useTest(profileId: string, testKey: string) {
  return useQuery({
    queryKey: keys.test(profileId, testKey),
    queryFn: () => call(() => GetTest(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

export function useTestMeta(profileId: string, testKey: string) {
  return useQuery({
    queryKey: keys.testMeta(profileId, testKey),
    queryFn: () => call(() => GetTestMeta(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

export function useTestRunHistory(profileId: string, testKey: string) {
  return useQuery({
    queryKey: keys.testRunHistory(profileId, testKey),
    queryFn: () => call(() => GetTestRunHistory(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

// useTestBugs loads the Jira bugs linked to this Test (read-only display). It is
// disabled for not-yet-created "NEW-" tests, matching the old load's skipBugs
// guard (a placeholder Test has no remote bugs to fetch).
export function useTestBugs(profileId: string, testKey: string) {
  return useQuery({
    queryKey: keys.testBugs(profileId, testKey),
    queryFn: () => call(() => GetTestBugs(profileId, testKey)),
    enabled: !!profileId && !!testKey && !testKey.startsWith("NEW-"),
  });
}

// useTestReview loads this Test's review verdict. It carries an optimistic
// update: TestDetail's setVerdict handler re-fetches after SetTestReview and
// patches this key via queryClient.setQueryData.
export function useTestReview(profileId: string, testKey: string) {
  return useQuery({
    queryKey: keys.testReview(profileId, testKey),
    queryFn: () => call(() => GetTestReview(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

// useTestRequirements loads the requirements this Test covers. It carries an
// optimistic update: TestDetail's applyRequirements handler re-fetches after
// SetTestRequirements and patches this key via queryClient.setQueryData.
export function useTestRequirements(profileId: string, testKey: string) {
  return useQuery({
    queryKey: keys.testRequirements(profileId, testKey),
    queryFn: () => call(() => GetTestRequirements(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

// useTestPreconditions loads the preconditions linked to this Test. The queryFn
// uses the cached read (forceRefresh=false), matching the old panel load; the
// "Re-fetch from Jira" button (refreshPreconditions, forceRefresh=true) stays
// imperative and patches this key via setQueryData. applyPreconditions likewise
// patches after SetTestPreconditions.
export function useTestPreconditions(profileId: string, testKey: string) {
  return useQuery({
    queryKey: keys.testPreconditions(profileId, testKey),
    queryFn: () => call(() => GetTestPreconditions(profileId, testKey, false)),
    enabled: !!profileId && !!testKey,
  });
}

// useAllPreconditions loads the profile-wide precondition pool TestDetail's
// picker draws from. Profile-scoped (not keyed on the test), so it caches across
// test switches.
export function useAllPreconditions(profileId: string) {
  return useQuery({
    queryKey: keys.preconditionPool(profileId),
    queryFn: () => call(() => ListAllPreconditions(profileId)),
    enabled: !!profileId,
  });
}

// useTestContainers loads the Test Sets / Plans / Executions this Test belongs
// to. It carries an optimistic update: TestDetail's deallocateContainer handler
// re-fetches after DeallocateTests and patches this key via setQueryData.
export function useTestContainers(profileId: string, testKey: string) {
  return useQuery({
    queryKey: keys.testContainers(profileId, testKey),
    queryFn: () => call(() => GetTestContainers(profileId, testKey)),
    enabled: !!profileId && !!testKey,
  });
}

// useRequirementCoverage loads the profile-wide requirement coverage list that
// TestDetail uses to populate its requirement picker (read-only here). It is
// profile-scoped, so it is NOT keyed on the test key and caches across test
// switches.
export function useRequirementCoverage(profileId: string) {
  return useQuery({
    queryKey: keys.requirementCoverage(profileId),
    queryFn: () => call(() => ListRequirementsWithCoverage(profileId)),
    enabled: !!profileId,
  });
}
