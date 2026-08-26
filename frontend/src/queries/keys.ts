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
  pending: (profileId: string) => [profileId, "pending"] as const,
  folders: (profileId: string) => [profileId, "folders"] as const,
};
