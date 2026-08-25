import { useQuery } from "@tanstack/react-query";
import { ListPendingChanges } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// usePendingChanges loads the active profile's pending-change journal. Replaces
// the App-level useState + manual reloadPending loader (audit A3): freshness now
// comes from invalidating keys.pending(profileId) after a mutation.
export function usePendingChanges(profileId: string) {
  return useQuery({
    queryKey: keys.pending(profileId),
    queryFn: () => call(() => ListPendingChanges(profileId)),
    enabled: !!profileId,
  });
}
