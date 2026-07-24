package coveragepublish

import (
	"fmt"
	"sort"

	"xray-test-manager/internal/coverage"
)

// ReconcileState is the outcome of comparing one coverage group's local
// model, its last-published snapshot, and its live Jira Test Set membership
// (as of the last pull-sync). See DetectDrift for the full three-way
// comparison this drives.
type ReconcileState string

const (
	// InSync means the local model and the local sync mirror of Jira's Test
	// Set both still match the last-published snapshot: there is nothing to
	// publish and nothing has drifted.
	InSync ReconcileState = "InSync"
	// LocalChanges means the local coverage model changed since the last
	// publish (a test was mapped to or unmapped from a value) but Jira's
	// Test Set still matches the snapshot. Publishing again brings them back
	// in sync with no loss on either side.
	LocalChanges ReconcileState = "LocalChanges"
	// Drift means Jira's Test Set changed since the last publish (someone
	// added or removed a test on the Test Set issue directly) while the
	// local model did not. Republishing would silently overwrite that
	// Jira-side edit -- see DetectDrift's doc comment for why this state is
	// reported and never auto-resolved.
	Drift ReconcileState = "Drift"
	// Conflict means both sides changed since the last publish: the local
	// model diverged from the snapshot and so did Jira's Test Set.
	// Republishing would overwrite the Jira-side edit just as it would for
	// Drift.
	Conflict ReconcileState = "Conflict"
	// NotPublished means this group has never been published: there is no
	// coverage_group_publication row to compare against, so no drift
	// comparison is possible yet.
	NotPublished ReconcileState = "NotPublished"
)

// GroupStatus is one coverage group's reconcile result: its state plus,
// where a comparison was possible, the test keys that changed on each side
// relative to the last-published snapshot. LocalAdded/LocalRemoved and
// RemoteAdded/RemoteRemoved are always reported separately (never merged)
// so a caller can say which side a given test key changed on.
type GroupStatus struct {
	// GroupID is the coverage_param_group id this status is for.
	GroupID string `json:"groupId"`
	// GroupName is the group's display name, as shown in the coverage tree.
	GroupName string `json:"groupName"`
	// ContainerKey is the Jira key of the group's Test Set. Empty when State
	// is NotPublished.
	ContainerKey string `json:"containerKey"`
	// State is this group's reconcile outcome. See the ReconcileState
	// constants for what each value means.
	State ReconcileState `json:"state"`
	// LocalAdded lists test keys the local coverage model gained relative to
	// the published snapshot (mapped to a value after the last publish).
	// Empty when State is NotPublished.
	LocalAdded []string `json:"localAdded"`
	// LocalRemoved lists test keys the local coverage model lost relative to
	// the published snapshot (unmapped from a value after the last
	// publish). Empty when State is NotPublished.
	LocalRemoved []string `json:"localRemoved"`
	// RemoteAdded lists test keys Jira's Test Set gained relative to the
	// published snapshot -- typically a test added to the Test Set issue by
	// hand. Empty when State is NotPublished. See DetectDrift's doc comment:
	// these keys carry no value-level information and this package never
	// applies them back into the coverage model.
	RemoteAdded []string `json:"remoteAdded"`
	// RemoteRemoved lists test keys Jira's Test Set lost relative to the
	// published snapshot. Empty when State is NotPublished.
	RemoteRemoved []string `json:"remoteRemoved"`
}

// DetectDrift performs a read-only, per-group three-way comparison for every
// coverage group under versionID:
//
//   - L (local)    the group's current desired test set, freshly computed
//     from the coverage model -- the same computation PublishGroups uses to
//     decide what a Test Set should contain (desiredGroupState).
//   - P (snapshot) the published_tests recorded by the last successful
//     publish (coverage_group_publication).
//   - R (remote)   the local sync mirror of what Jira's Test Set currently
//     contains (test_container_test), populated by the last pull-sync.
//
// It makes no backend/Jira calls and performs no writes -- it is a pure read
// over already-synced local data, so its accuracy is only as fresh as the
// last pull-sync. A group with no publication row is reported with state
// NotPublished rather than being skipped or treated as an error, so a caller
// can show every group in the version.
//
// HONEST LIMIT: a test added to a Test Set in Jira carries no value-level
// information -- Xray has no way to say which coverage parameter value a
// manually added test covers. DetectDrift therefore only detects and
// reports Drift/Conflict and the RemoteAdded/RemoteRemoved keys; it never
// guesses a value and never writes anything back into the coverage model.
// This package intentionally has no "accept remote" or "resolve" helper.
// Resolving a Drift or Conflict is left to the caller: either republish
// (which overwrites Jira's Test Set membership with the local model) or
// assign the remotely added test to a specific value by hand, e.g. via
// frontend/src/components/CoverageTestPicker.tsx.
func (p *Publisher) DetectDrift(profileID, versionID string) ([]GroupStatus, error) {
	model, err := p.coverage.GetParamModel(profileID, versionID)
	if err != nil {
		return nil, fmt.Errorf("read parameter model: %w", err)
	}

	statuses := make([]GroupStatus, 0, len(model.Groups))
	for _, group := range model.Groups {
		gs, err := p.detectGroupDrift(profileID, group)
		if err != nil {
			return nil, fmt.Errorf("group %s: %w", group.ID, err)
		}
		statuses = append(statuses, gs)
	}
	return statuses, nil
}

// detectGroupDrift runs DetectDrift's three-way comparison for a single
// group.
func (p *Publisher) detectGroupDrift(profileID string, group coverage.ParamGroup) (GroupStatus, error) {
	gs := GroupStatus{GroupID: group.ID, GroupName: group.Name}

	containerKey, published, existed, err := p.publication(profileID, group.ID)
	if err != nil {
		return gs, fmt.Errorf("read publication record: %w", err)
	}
	if !existed {
		gs.State = NotPublished
		return gs, nil
	}
	gs.ContainerKey = containerKey

	_, local, err := p.desiredGroupState(profileID, group)
	if err != nil {
		return gs, fmt.Errorf("compute desired test set: %w", err)
	}

	remote, err := p.currentMembers(profileID, containerKey)
	if err != nil {
		return gs, fmt.Errorf("read current membership: %w", err)
	}

	// published, local, and remote are each expected pre-sorted by their
	// producers (joinPublishedTests always stores a sorted snapshot,
	// desiredGroupState sorts its result, and currentMembers' query orders
	// by test_key), but sort defensively so a future change to any of those
	// producers -- or a hand-edited row -- can never make the comparison's
	// output order flap between runs.
	publishedSorted := sortedCopy(published)
	localSorted := sortedCopy(local)
	remoteSorted := sortedCopy(remote)

	gs.LocalAdded, gs.LocalRemoved = diffMembership(publishedSorted, localSorted)
	gs.RemoteAdded, gs.RemoteRemoved = diffMembership(publishedSorted, remoteSorted)

	localChanged := len(gs.LocalAdded) > 0 || len(gs.LocalRemoved) > 0
	remoteChanged := len(gs.RemoteAdded) > 0 || len(gs.RemoteRemoved) > 0

	switch {
	case !localChanged && !remoteChanged:
		gs.State = InSync
	case localChanged && !remoteChanged:
		gs.State = LocalChanges
	case !localChanged && remoteChanged:
		gs.State = Drift
	default:
		gs.State = Conflict
	}

	return gs, nil
}

// sortedCopy returns a sorted copy of ss, leaving ss itself untouched. A nil
// input returns nil rather than an empty slice, matching splitPublishedTests
// and desiredGroupState's own nil-for-empty convention.
func sortedCopy(ss []string) []string {
	if ss == nil {
		return nil
	}
	out := append([]string(nil), ss...)
	sort.Strings(out)
	return out
}
