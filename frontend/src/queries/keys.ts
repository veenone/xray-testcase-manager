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
  test: (profileId: string, key: string) =>
    [profileId, "test", key] as const,
  // `reload` folds TestDetail's version + localReloadKey counters into the key
  // as the migration bridge (a bump refetches). Phase 4 replaces it with
  // targeted invalidation of [profileId, "test", key].
  testMeta: (profileId: string, key: string, reload: string) =>
    [profileId, "test", key, "meta", reload] as const,
  testRunHistory: (profileId: string, key: string, reload: string) =>
    [profileId, "test", key, "runHistory", reload] as const,
  pending: (profileId: string) => [profileId, "pending"] as const,
  folders: (profileId: string) => [profileId, "folders"] as const,
  preconditions: (profileId: string) => [profileId, "preconditions"] as const,
  requirements: (profileId: string) => [profileId, "requirements"] as const,
  testCalls: (profileId: string) => [profileId, "testCalls"] as const,
  duplicates: (profileId: string) => [profileId, "duplicates"] as const,
  stats: (profileId: string) => [profileId, "stats"] as const,
  canonicalRequirements: (profileId: string) =>
    [profileId, "canonicalRequirements"] as const,
};
