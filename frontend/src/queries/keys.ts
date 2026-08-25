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
  pending: (profileId: string) => [profileId, "pending"] as const,
  folders: (profileId: string) => [profileId, "folders"] as const,
};
