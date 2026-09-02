import { useQuery } from "@tanstack/react-query";
import { GetTestSummaries } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// useTestSummaries loads the current summary of each selected Test for the
// bulk-rename preview (-354). One fetch per selection; the preview then
// recomputes locally on every keystroke with no further I/O.
//
// No placeholderData: showing a previous selection's summaries under a new
// selection would preview renames against the wrong tests.
export function useTestSummaries(profileId: string, testKeys: string[]) {
  return useQuery({
    queryKey: keys.testSummaries(profileId, testKeys),
    queryFn: () => call(() => GetTestSummaries(profileId, testKeys)),
    enabled: !!profileId && testKeys.length > 0,
  });
}
