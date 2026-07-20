// Feature flags for capabilities that are built but intentionally hidden in the
// UI. Flip a flag to surface the feature again — no other code change needed.

import { useEffect, useState } from "react";
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

// invalidationListeners lets mounted useCapabilities hooks know a cache entry
// was dropped so they can re-fetch even though their profileId (their only
// effect dependency) hasn't changed -- e.g. editing an existing profile's or
// connection's Backend in place. Listeners re-run fetchCapabilities for
// whatever profileId they're currently bound to; the cache lookup itself is
// what makes that cheap for ids the invalidation didn't touch.
const invalidationListeners = new Set<() => void>();

// fetchCapabilities returns the given profile's backend capabilities, caching
// the result per profile id. Falls back to defaultCapabilities (rather than
// throwing) on error, since capability gating must never be the reason a
// profile fails to load.
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

// invalidateCapabilities drops the cached capabilities for a profile/
// connection id and tells every mounted useCapabilities hook to re-fetch.
// Call this after any save that can change a profile or connection's
// backend (UpdateProfile, UpdateConnection) -- otherwise the UI keeps
// gating on the old backend's capabilities until an app restart, since the
// cache is keyed on an id that doesn't change across the edit.
export function invalidateCapabilities(profileId: string): void {
  capabilitiesCache.delete(profileId);
  invalidationListeners.forEach((notify) => notify());
}

// useCapabilities is the single access point components use to gate
// backend-specific UI. It fetches the active profile's capabilities via
// fetchCapabilities (cached, so multiple components mounting for the same
// profile only fetch once) and returns defaultCapabilities synchronously
// while that fetch is in flight, so nothing flashes as "unsupported" before
// the real answer arrives. Because defaultCapabilities is the full Xray set,
// an Xray profile never has anything gated off, before or after the fetch
// resolves.
export function useCapabilities(profileId: string): Capabilities {
  const [caps, setCaps] = useState<Capabilities>(defaultCapabilities);
  // Bumped by invalidateCapabilities so the fetch effect below re-runs even
  // when profileId itself hasn't changed (an in-place backend switch).
  const [invalidation, setInvalidation] = useState(0);

  useEffect(() => {
    const notify = () => setInvalidation((v) => v + 1);
    invalidationListeners.add(notify);
    return () => {
      invalidationListeners.delete(notify);
    };
  }, []);

  useEffect(() => {
    if (!profileId) {
      setCaps(defaultCapabilities);
      return;
    }
    let cancelled = false;
    // Reset to the permissive default for the new profile while its real
    // capabilities load, rather than carrying over the previous profile's.
    setCaps(defaultCapabilities);
    fetchCapabilities(profileId).then((c) => {
      if (!cancelled) setCaps(c);
    });
    return () => {
      cancelled = true;
    };
  }, [profileId, invalidation]);

  return caps;
}
