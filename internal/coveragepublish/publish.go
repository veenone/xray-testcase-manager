// Package coveragepublish publishes local coverage groups
// (internal/coverage's coverage_param_group nodes) into Xray as Test Sets,
// so a coverage group's structure and gaps are visible to people working in
// Jira who never open this app. It is a direct, user-triggered write against
// internal/backend.Backend -- not the pending_change journal -- matching how
// internal/bridge's publish engine (internal/bridge/publish.go) already
// works: this is an explicit action, not a background sync.
//
// Task 3 (drift reconcile) lives in this same package and reuses
// desiredGroupState, the "compute what a group's Test Set should contain and
// say" helper below, so that logic is factored out rather than inlined in
// the publish loop.
package coveragepublish

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/coverage"
	"xray-test-manager/internal/store"
)

// Backend is the publish engine's view of the backend surface it writes
// through: creating a container and keeping its membership and description
// current. A narrow interface -- mirroring internal/bridge/publish.go's
// ExternalRefStore -- so tests can drive the engine against a fake rather
// than a real Jira/Xray connection. Any concrete backend.Backend
// implementation satisfies this with no adapter, since Go interfaces are
// structural (asserted below).
type Backend interface {
	CreateContainer(ctx context.Context, projectKey, kind, summary string) (string, error)
	AddTestsToContainer(ctx context.Context, kind, containerKey string, testKeys []string) error
	RemoveTestsFromContainer(ctx context.Context, kind, containerKey string, testKeys []string) error
	UpdateIssue(ctx context.Context, key string, fields map[string]any) error
}

// var _ Backend = backend.Backend(nil) locks in that the full backend.Backend
// interface keeps satisfying this narrower Backend -- a compile-time guard
// against the two signatures drifting apart.
var _ Backend = backend.Backend(nil)

// GroupResult is the outcome of publishing one coverage group.
type GroupResult struct {
	// GroupID is the coverage_param_group id this result is for.
	GroupID string `json:"groupId"`
	// GroupName is the group's display name, as shown in the coverage tree.
	GroupName string `json:"groupName"`
	// ContainerKey is the Jira key of the Test Set this group publishes to.
	ContainerKey string `json:"containerKey"`
	// Created is true when this run created the Test Set for the first time;
	// false means an existing publication was reused and refreshed.
	Created bool `json:"created"`
	// Added lists the test keys this run added to the Test Set.
	Added []string `json:"added"`
	// Removed lists the test keys this run removed from the Test Set.
	Removed []string `json:"removed"`
	// Error is set when this group's publish failed. A failure here never
	// aborts the run -- every other group under the version still gets its
	// turn (see PublishGroups).
	Error string `json:"error,omitempty"`
}

// Result is the outcome of one PublishGroups run: aggregated counts plus a
// row per group so a caller can render both a summary and a detail table.
type Result struct {
	Created int           `json:"created"`
	Updated int           `json:"updated"`
	Failed  int           `json:"failed"`
	Groups  []GroupResult `json:"groups"`
}

// Publisher runs the coverage publish engine.
type Publisher struct {
	db       *sql.DB
	coverage *coverage.Module
	// Backend is the write surface publishing goes through -- a real
	// backend.Backend in production, a fake in tests.
	Backend Backend
}

// NewPublisher constructs a Publisher from its dependencies. st supplies the
// SQLite handle the publish engine reads its own tables from (directly, for
// coverage_group_publication and test_container_test) and cov supplies the
// coverage module's parameter tree (GetParamModel) so the group/parameter/
// value structure is read through the one place that already owns it.
func NewPublisher(st *store.Store, cov *coverage.Module, be Backend) *Publisher {
	return &Publisher{db: st.DB(), coverage: cov, Backend: be}
}

// PublishGroups publishes every coverage_param_group under versionID into a
// Test Set inside projectKey. One group's failure is recorded in its
// GroupResult and does not stop the run.
func (p *Publisher) PublishGroups(ctx context.Context, profileID, projectKey, versionID string) (Result, error) {
	result := Result{Groups: []GroupResult{}}

	canonicalName, versionName, err := p.versionInfo(profileID, versionID)
	if err != nil {
		return result, fmt.Errorf("read canonical/version names: %w", err)
	}

	model, err := p.coverage.GetParamModel(profileID, versionID)
	if err != nil {
		return result, fmt.Errorf("read parameter model: %w", err)
	}

	for _, group := range model.Groups {
		gr := p.publishOne(ctx, profileID, projectKey, canonicalName, versionName, group)
		switch {
		case gr.Error != "":
			result.Failed++
		case gr.Created:
			result.Created++
		default:
			result.Updated++
		}
		result.Groups = append(result.Groups, gr)
	}

	return result, nil
}

// publishOne handles exactly one coverage group. It never returns a Go
// error -- every failure path is recorded on the returned GroupResult's
// Error field instead, which is what gives PublishGroups its per-group
// isolation.
func (p *Publisher) publishOne(ctx context.Context, profileID, projectKey, canonicalName, versionName string, group coverage.ParamGroup) GroupResult {
	gr := GroupResult{GroupID: group.ID, GroupName: group.Name}

	rows, desired, err := p.desiredGroupState(profileID, group)
	if err != nil {
		gr.Error = fmt.Sprintf("compute desired test set: %v", err)
		return gr
	}

	containerKey, _, existed, err := p.publication(profileID, group.ID)
	if err != nil {
		gr.Error = fmt.Sprintf("read publication record: %v", err)
		return gr
	}

	if !existed {
		title := fmt.Sprintf("%s %s - %s", canonicalName, versionName, group.Name)
		key, err := p.Backend.CreateContainer(ctx, projectKey, backend.KindTestSet, title)
		if err != nil {
			gr.Error = fmt.Sprintf("create test set: %v", err)
			return gr
		}
		containerKey = key
		gr.Created = true

		// Write the publication row IMMEDIATELY -- before the membership
		// calls and before the description write -- so a re-run after a
		// failure below finds this row and reuses the Test Set instead of
		// creating a second one. This is the same resumability lesson
		// internal/bridge/publish.go's step-3 comment documents for
		// external_ref: "target test exists" and "publication recorded"
		// must never be allowed to diverge.
		if err := p.putPublication(profileID, group.ID, containerKey, nil); err != nil {
			gr.ContainerKey = containerKey
			gr.Error = fmt.Sprintf("test set %s was created but recording the publication row failed: %v (a retry will create a DUPLICATE Test Set)", containerKey, err)
			return gr
		}
	}
	gr.ContainerKey = containerKey

	current, err := p.currentMembers(profileID, containerKey)
	if err != nil {
		gr.Error = fmt.Sprintf("read current membership: %v", err)
		return gr
	}
	add, remove := diffMembership(current, desired)

	if len(add) > 0 {
		if err := p.Backend.AddTestsToContainer(ctx, backend.KindTestSet, containerKey, add); err != nil {
			gr.Error = fmt.Sprintf("add tests to test set: %v", err)
			return gr
		}
		// Mirror the confirmed add locally so a second PublishGroups run --
		// with no pull-sync in between -- sees the up-to-date membership
		// instead of re-issuing the same add (this is what makes publish
		// genuinely idempotent, not just idempotent-after-a-sync).
		if err := p.mirrorMembersAdded(profileID, containerKey, add); err != nil {
			gr.Error = fmt.Sprintf("update local membership mirror after add: %v", err)
			return gr
		}
	}
	if len(remove) > 0 {
		if err := p.Backend.RemoveTestsFromContainer(ctx, backend.KindTestSet, containerKey, remove); err != nil {
			gr.Error = fmt.Sprintf("remove tests from test set: %v", err)
			return gr
		}
		if err := p.mirrorMembersRemoved(profileID, containerKey, remove); err != nil {
			gr.Error = fmt.Sprintf("update local membership mirror after remove: %v", err)
			return gr
		}
	}
	gr.Added = add
	gr.Removed = remove

	description := renderDescription(canonicalName, versionName, group.Name, rows)
	if err := p.Backend.UpdateIssue(ctx, containerKey, map[string]any{"description": description}); err != nil {
		gr.Error = fmt.Sprintf("update test set description: %v", err)
		return gr
	}

	if err := p.putPublication(profileID, group.ID, containerKey, desired); err != nil {
		gr.Error = fmt.Sprintf("record publication snapshot: %v", err)
		return gr
	}

	return gr
}

// desiredGroupState computes what one coverage group's Test Set should
// contain and say: the per-value coverage rows (for rendering) and the
// sorted union of their live mapped test keys (for diffing membership).
// Unexported and factored out here specifically because Task 3's
// drift-reconcile job reuses this same computation verbatim.
func (p *Publisher) desiredGroupState(profileID string, group coverage.ParamGroup) ([]valueRow, []string, error) {
	live, err := p.liveTestsByGroup(profileID, group.ID)
	if err != nil {
		return nil, nil, err
	}

	var rows []valueRow
	desiredSet := map[string]struct{}{}
	for _, param := range group.Parameters {
		for _, v := range param.Values {
			keys := append([]string(nil), live[v.ID]...)
			sort.Strings(keys)
			rows = append(rows, valueRow{Label: v.ValueLabel, Kind: v.ValueKind, TestKeys: keys})
			for _, k := range keys {
				desiredSet[k] = struct{}{}
			}
		}
	}

	desired := make([]string, 0, len(desiredSet))
	for k := range desiredSet {
		desired = append(desired, k)
	}
	sort.Strings(desired)
	return rows, desired, nil
}

// liveTestsByGroup returns, per value id under groupID, the mapped test keys
// that still exist in test_case (stale mappings excluded) -- the same
// liveness join internal/coverage/compute.go's liveTestsByValue performs,
// scoped to one group instead of a whole version.
func (p *Publisher) liveTestsByGroup(profileID, groupID string) (map[string][]string, error) {
	rows, err := p.db.Query(
		`SELECT vt.value_id, vt.test_key
		   FROM coverage_value_test vt
		   JOIN coverage_param_value pv ON pv.profile_id = vt.profile_id AND pv.id = vt.value_id
		   JOIN coverage_parameter p   ON p.profile_id = pv.profile_id AND p.id = pv.parameter_id
		   JOIN test_case tc          ON tc.profile_id = vt.profile_id AND tc.jira_key = vt.test_key
		  WHERE vt.profile_id = ? AND p.group_id = ?`,
		profileID, groupID)
	if err != nil {
		return nil, fmt.Errorf("read live mappings: %w", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var valueID, testKey string
		if err := rows.Scan(&valueID, &testKey); err != nil {
			return nil, err
		}
		out[valueID] = append(out[valueID], testKey)
	}
	return out, rows.Err()
}

// currentMembers returns the test keys currently in containerKey per the
// local sync mirror of Xray membership (test_container_test) -- not a live
// backend read.
func (p *Publisher) currentMembers(profileID, containerKey string) ([]string, error) {
	rows, err := p.db.Query(
		`SELECT test_key FROM test_container_test WHERE profile_id = ? AND container_key = ? ORDER BY test_key`,
		profileID, containerKey)
	if err != nil {
		return nil, fmt.Errorf("read current membership: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// mirrorMembersAdded records, in the local test_container_test mirror, the
// test keys just confirmed added to containerKey via a successful
// AddTestsToContainer call. Only call this after that call has succeeded --
// this keeps the local mirror truthful about what Xray actually has, which
// is what lets a re-run of PublishGroups (with no pull-sync in between) see
// the real membership instead of re-emitting the same add. Test Sets carry
// no run status, so run_status/run_defects/run_comment stay at their column
// defaults (empty string).
func (p *Publisher) mirrorMembersAdded(profileID, containerKey string, testKeys []string) error {
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("begin membership mirror tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT INTO test_container_test (profile_id, container_key, test_key)
		 VALUES (?, ?, ?)
		 ON CONFLICT(profile_id, container_key, test_key) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("prepare membership mirror insert: %w", err)
	}
	defer stmt.Close()

	for _, k := range testKeys {
		if _, err := stmt.Exec(profileID, containerKey, k); err != nil {
			return fmt.Errorf("mirror added test %s: %w", k, err)
		}
	}
	return tx.Commit()
}

// mirrorMembersRemoved deletes, from the local test_container_test mirror,
// the test keys just confirmed removed from containerKey via a successful
// RemoveTestsFromContainer call. See mirrorMembersAdded for why this matters.
func (p *Publisher) mirrorMembersRemoved(profileID, containerKey string, testKeys []string) error {
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("begin membership mirror tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`DELETE FROM test_container_test WHERE profile_id = ? AND container_key = ? AND test_key = ?`)
	if err != nil {
		return fmt.Errorf("prepare membership mirror delete: %w", err)
	}
	defer stmt.Close()

	for _, k := range testKeys {
		if _, err := stmt.Exec(profileID, containerKey, k); err != nil {
			return fmt.Errorf("mirror removed test %s: %w", k, err)
		}
	}
	return tx.Commit()
}

// diffMembership compares a container's current membership against the
// desired set and returns the delta only. Both slices are expected sorted;
// the returned add/remove slices preserve that order.
func diffMembership(current, desired []string) (add, remove []string) {
	curSet := make(map[string]struct{}, len(current))
	for _, k := range current {
		curSet[k] = struct{}{}
	}
	desSet := make(map[string]struct{}, len(desired))
	for _, k := range desired {
		desSet[k] = struct{}{}
	}
	for _, k := range desired {
		if _, ok := curSet[k]; !ok {
			add = append(add, k)
		}
	}
	for _, k := range current {
		if _, ok := desSet[k]; !ok {
			remove = append(remove, k)
		}
	}
	return add, remove
}

// versionInfo resolves the display names needed for a Test Set's title and
// description footer: the canonical requirement's name and this version's
// name. GetParamModel does not carry either (ParamModel is scoped to the
// parameter tree only), so this reads canonical_version joined to
// canonical_requirement directly.
func (p *Publisher) versionInfo(profileID, versionID string) (canonicalName, versionName string, err error) {
	err = p.db.QueryRow(
		`SELECT cr.name, cv.name
		   FROM canonical_version cv
		   JOIN canonical_requirement cr ON cr.profile_id = cv.profile_id AND cr.id = cv.canonical_id
		  WHERE cv.profile_id = ? AND cv.id = ?`,
		profileID, versionID).Scan(&canonicalName, &versionName)
	if err == sql.ErrNoRows {
		return "", "", fmt.Errorf("version %s not found", versionID)
	}
	if err != nil {
		return "", "", err
	}
	return canonicalName, versionName, nil
}

// publication returns the existing coverage_group_publication row for a
// group, if any. publishedTests is the previously published snapshot,
// decoded from the newline-joined column (see splitPublishedTests) -- not
// used by PublishGroups' own diffing (which reads test_container_test
// instead), but returned here for Task 3's drift-reconcile job to reuse.
func (p *Publisher) publication(profileID, groupID string) (containerKey string, publishedTests []string, ok bool, err error) {
	var raw string
	err = p.db.QueryRow(
		`SELECT container_key, published_tests FROM coverage_group_publication
		  WHERE profile_id = ? AND group_id = ?`,
		profileID, groupID).Scan(&containerKey, &raw)
	if err == sql.ErrNoRows {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	return containerKey, splitPublishedTests(raw), true, nil
}

// putPublication upserts the publication row for profileID/groupID,
// recording the Test Set key and the exact set of test keys just published.
// testKeys is expected pre-sorted (desiredGroupState already sorts it) so
// the stored snapshot is deterministic.
func (p *Publisher) putPublication(profileID, groupID, containerKey string, testKeys []string) error {
	_, err := p.db.Exec(
		`INSERT INTO coverage_group_publication (profile_id, group_id, container_key, published_tests, published_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(profile_id, group_id) DO UPDATE SET
		   container_key = excluded.container_key,
		   published_tests = excluded.published_tests,
		   published_at = excluded.published_at`,
		profileID, groupID, containerKey, joinPublishedTests(testKeys), nowISO())
	return err
}

// nowISO returns an ISO-8601 UTC timestamp, matching the format used
// elsewhere in the store (e.g. internal/coverage's canonical_requirement
// timestamps).
func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }
