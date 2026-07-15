// Feature flags for capabilities that are built but intentionally hidden in the
// UI. Flip a flag to surface the feature again — no other code change needed.

import { GetCapabilities, type Capabilities } from "./api";

// Test review / sign-off (verdict + reviewer + note). The backend, local store,
// commit path (a Jira comment, Phase 7), the Browse review filter and the
// requirement sign-off audit export all remain; this flag only gates the
// user-facing entry points. As a standalone tool XTM can't enforce a review
// workflow, so the surface is hidden until a team process makes it useful.
export const REVIEW_ENABLED = false;

// defaultCapabilities is the full/permissive capability set — every backend
// feature enabled, the Xray container kinds and models — used as the fallback
// before a profile's real capabilities have loaded. Every profile is Xray
// today, and Xray reports this exact set (see xray.Adapter.Capabilities), so
// components consuming capabilities before the fetch resolves see the same
// behavior as after it resolves: nothing gates OFF prematurely.
export const defaultCapabilities: Capabilities = {
  name: "xray",
  idStyle: "opaque",
  supportsJqlScope: true,
  stepModel: "objects",
  supportsTestTypes: true,
  supportsFolders: true,
  supportsPreconditionObjects: true,
  supportsRequirementObjects: true,
  supportsIssueLinkTypes: true,
  supportsEnvironments: true,
  supportsContainers: true,
  containerKinds: ["testset", "testplan", "testexec"],
  supportsTestRuns: true,
  statusModel: "workflow",
  supportsWorkflowTransitions: true,
  supportsBugCreation: true,
  supportsBugLinks: true,
  supportsTags: false,
};

// capabilitiesCache holds the last-fetched Capabilities per profile id, so
// repeated calls (e.g. multiple components mounting) don't refetch.
const capabilitiesCache = new Map<string, Capabilities>();

// fetchCapabilities returns the given profile's backend capabilities, caching
// the result per profile id. Falls back to defaultCapabilities (rather than
// throwing) on error, since capability gating must never be the reason a
// profile fails to load. No component consumes this yet -- it is plumbing for
// later phases that gate Xray-only UI once a non-Xray backend exists.
export async function fetchCapabilities(profileId: string): Promise<Capabilities> {
  const cached = capabilitiesCache.get(profileId);
  if (cached) return cached;
  try {
    const caps = await GetCapabilities(profileId);
    capabilitiesCache.set(profileId, caps);
    return caps;
  } catch {
    return defaultCapabilities;
  }
}
