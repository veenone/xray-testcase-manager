// keys is the single source of truth for query keys, so a mutation's
// invalidation target can never drift from the read it must invalidate.
//
// `params` is typed as Record<string, unknown> rather than the real
// `TestQuery` from "../api": TestQuery declares its filter fields as
// required, but callers (including tests) legitimately query with a partial
// filter set, so a structural key type is the honest fit here.
export const keys = {
  tests: (profileId: string, params: Record<string, unknown>) =>
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
};
