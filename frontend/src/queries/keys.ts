// keys is the single source of truth for query keys, so a mutation's
// invalidation target can never drift from the read it must invalidate.
//
// `tests` is generic over its params object rather than pinned to the real
// `TestQuery` from "../api": TestQuery declares its filter fields as required,
// but callers (including tests) legitimately query with a partial filter set,
// so accepting any object shape is the honest fit here.
export const keys = {
  tests: <T extends object>(profileId: string, params: T) =>
    [profileId, "tests", params] as const,
  // The Test detail keys are stable (no reload counter). [profileId, "test",
  // key] is the shared prefix, so one invalidateQueries on `test` refetches the
  // base read plus every section below. TestDetail translates its parent-driven
  // reload signal (version/localReloadKey) into exactly that invalidation.
  test: (profileId: string, key: string) =>
    [profileId, "test", key] as const,
  testMeta: (profileId: string, key: string) =>
    [profileId, "test", key, "meta"] as const,
  testRunHistory: (profileId: string, key: string) =>
    [profileId, "test", key, "runHistory"] as const,
  testBugs: (profileId: string, key: string) =>
    [profileId, "test", key, "bugs"] as const,
  testReview: (profileId: string, key: string) =>
    [profileId, "test", key, "review"] as const,
  // This Test's container memberships (Test Sets/Plans/Executions it belongs
  // to). Test-scoped, distinct from the profile-wide `containers` list below.
  testContainers: (profileId: string, key: string) =>
    [profileId, "test", key, "containers"] as const,
  // This Test's covered requirements. Test-scoped, distinct from the
  // profile-wide `requirements` coverage list.
  testRequirements: (profileId: string, key: string) =>
    [profileId, "test", key, "requirements"] as const,
  // This Test's linked preconditions. Test-scoped.
  testPreconditions: (profileId: string, key: string) =>
    [profileId, "test", key, "preconditions"] as const,
  // The profile-wide precondition pool TestDetail's picker draws from.
  // Profile-scoped (caches across test switches); shares the
  // [profileId, "preconditions"] prefix with the PreconditionsView list.
  preconditionPool: (profileId: string) =>
    [profileId, "preconditions", "pool"] as const,
  // TestDetail's copy of the requirement-coverage list. Profile-scoped (not
  // test-scoped), so it caches across test switches. Shares the
  // [profileId, "requirements"] prefix with useRequirements; the "coverage"
  // segment keeps it a distinct entry from that view's refreshKey-bridged key.
  requirementCoverage: (profileId: string) =>
    [profileId, "requirements", "coverage"] as const,
  pending: (profileId: string) => [profileId, "pending"] as const,
  syncState: (profileId: string) => [profileId, "syncState"] as const,
  folders: (profileId: string) => [profileId, "folders"] as const,
  components: (profileId: string) => [profileId, "components"] as const,
  preconditions: (profileId: string) => [profileId, "preconditions"] as const,
  // The tests linked to one precondition (PreconditionsView's "Used by" list).
  // Nested under the "preconditions" prefix so invalidateProfileData refreshes
  // it along with the list.
  preconditionTests: (profileId: string, key: string) =>
    [profileId, "preconditions", "tests", key] as const,
  requirements: (profileId: string) => [profileId, "requirements"] as const,
  testCalls: (profileId: string) => [profileId, "testCalls"] as const,
  duplicates: (profileId: string) => [profileId, "duplicates"] as const,
  stats: (profileId: string) => [profileId, "stats"] as const,
  canonicalRequirements: (profileId: string) =>
    [profileId, "canonicalRequirements"] as const,
  containers: (profileId: string) => [profileId, "containers"] as const,
};
