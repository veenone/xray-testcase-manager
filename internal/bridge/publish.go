// Publish engine — Phase 6 bridge task B5. Publishes a workspace's hub TESTS
// (fields + steps + status) into a target backend connection via the neutral
// write path (internal/backend.Backend), applying the B4 Mapping and
// recording internal/store's external_ref table so the run is resumable and
// dual-publish (a local test carrying an identity in more than one backend at
// once) falls out for free. Containers/preconditions/requirements/links are a
// later task (B5b) — this file only ever reads and writes TESTS. The SOURCE
// connection is never read from or written to by this file: everything comes
// from the local hub cache (HubReader) and every write goes to Target.
package bridge

import (
	"context"
	"fmt"
	"strings"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/store"
)

// entityTypeTest is the external_ref.entity_type value for Test rows —
// matches the literal 'test' the v41 backfill already writes for the source
// (xray) connection (store.go).
const entityTypeTest = "test"

// HubTest is the neutral shape of one hub test the publish engine reads —
// a narrow projection of testrepo.TestCase's fields onto this package so
// internal/bridge does not need to import internal/testrepo. *App's
// HubReader adapter (bridge_publish.go) does the field-for-field conversion
// at the boundary, the same pattern app.go's toJiraTransitions/
// toJiraBugCreateFields already use for backend.* -> jira.*.
type HubTest struct {
	Key         string
	Summary     string
	Description string
	Status      string
	Priority    string
	Labels      []string
	Components  []string
}

// HubStep is the neutral shape of one hub test step (testrepo.Step's
// action/data/expected fields; XrayID/Index/CalledTestKey are irrelevant to
// publish, so they are not carried over).
type HubStep struct {
	Action   string
	Data     string
	Expected string
}

// HubReader is the publish engine's read-only view of the local hub dataset.
// *App wires this to internal/testrepo.Repository; PublishTests's tests use a
// fake so the engine's logic (resumability, mapping, failure isolation) can
// be exercised without a real store.
type HubReader interface {
	// ListHubTests returns every cached hub test for a workspace (TESTS only
	// — no containers/preconditions/requirements). Order is not significant.
	ListHubTests(workspaceID string) ([]HubTest, error)
	// ListHubSteps returns the cached steps for one hub test, in the same
	// order the source captured them.
	ListHubSteps(workspaceID, testKey string) ([]HubStep, error)
}

// ExternalRefStore is the publish engine's view of the external_ref identity
// table (store.go's ExternalRef/PutExternalRef). A narrow interface — rather
// than taking *store.Store directly — so publish.go's dependency is exactly
// the two methods it calls; *store.Store satisfies it with no adapter needed
// (asserted below).
type ExternalRefStore interface {
	ExternalRef(workspaceID, entityType, localID, connection string) (externalKey string, ok bool, err error)
	PutExternalRef(workspaceID, entityType, localID, connection, externalKey, versionToken string) error
}

// var _ ExternalRefStore = (*store.Store)(nil) locks in that *store.Store
// keeps satisfying ExternalRefStore — a compile-time guard against the two
// signatures drifting apart.
var _ ExternalRefStore = (*store.Store)(nil)

// PublishedTest records one hub test successfully created in the target
// during this run: LocalKey is the hub identity, TargetKey the key/id the
// target backend assigned.
type PublishedTest struct {
	LocalKey  string `json:"localKey"`
	TargetKey string `json:"targetKey"`
}

// PublishFailure is one hub test whose publish failed for a non-skip reason.
// The row is NOT recorded in external_ref, so it remains eligible for a
// future PublishTests run to retry.
type PublishFailure struct {
	LocalKey string `json:"localKey"`
	Error    string `json:"error"`
}

// PublishResult reports the outcome of a PublishTests run, mirroring the
// shape of syncer.CommitResult: disjoint per-test buckets the UI (B6) renders
// directly.
type PublishResult struct {
	// Created is every hub test newly published to the target this run.
	Created []PublishedTest `json:"created"`
	// AlreadyPublished lists hub test keys skipped because an
	// external_ref(target) row already existed — resumability: re-running
	// PublishTests after a partial or full previous run creates nothing for
	// these.
	AlreadyPublished []string `json:"alreadyPublished"`
	// Failed is every hub test whose publish attempt errored. One test's
	// failure never aborts the run — the loop always continues to the next
	// test.
	Failed []PublishFailure `json:"failed"`
}

// Publisher runs the bridge's publish engine: resumable, TESTS-only,
// one-way publish of a workspace's hub tests into TargetProjectKey inside
// Target's backend, via Target's neutral write path. It never touches the
// source backend — Hub is the local cache, not a live connection.
type Publisher struct {
	// Target is the backend.Backend the tests are published into.
	Target backend.Backend
	// TargetProjectKey is the target connection's project key / product name
	// — CreateTest's projectKey argument.
	TargetProjectKey string
	// Hub reads the workspace's cached tests + steps (never the source
	// connection directly).
	Hub HubReader
	// Refs is the external_ref accessor used both to test resumability
	// (skip already-published tests) and to record newly published ones.
	Refs ExternalRefStore
	// Mapping is the B4 status/step/field mapping applied to every test this
	// Publisher creates.
	Mapping Mapping
}

// NewPublisher constructs a Publisher from its dependencies.
func NewPublisher(target backend.Backend, targetProjectKey string, hub HubReader, refs ExternalRefStore, mapping Mapping) *Publisher {
	return &Publisher{
		Target:           target,
		TargetProjectKey: targetProjectKey,
		Hub:              hub,
		Refs:             refs,
		Mapping:          mapping,
	}
}

// PublishTests publishes every hub test in workspaceID that has no
// external_ref(targetConnection) row yet into the target backend, applying
// p.Mapping's status map and step mode. sourceConnection identifies which
// connection's data is being read from the hub (currently informational
// only — HubReader is not itself connection-scoped, since a workspace's hub
// cache today only ever holds one connection's pulled data; it is threaded
// through so a future multi-source hub read does not need a signature
// change) and is never read from or written to directly: publish only ever
// touches the local hub cache and the target backend.
//
// For each hub test:
//  1. Skip (record AlreadyPublished) if external_ref(workspace, "test",
//     key, targetConnection) already exists.
//  2. Otherwise CreateTest with the mapped priority/labels/components, then
//     CreateTestStep per p.Mapping.StepMode (one joined step for "flatten",
//     one call per source step for "passthrough").
//  3. If the mapped status differs and the target is settable (not
//     workflow-driven), write it via FieldsForJira + UpdateIssue — the same
//     settable-status pattern syncer/commit.go uses (~545).
//  4. Record external_ref(workspace, "test", key, targetConnection,
//     targetKey).
//
// A failure at any step for one test is recorded in Failed and the loop
// continues — one bad test never aborts the run. onProgress (nilable) is
// called once per test, after it is fully handled (skipped, created, or
// failed).
func (p *Publisher) PublishTests(ctx context.Context, workspaceID, sourceConnection, targetConnection string, onProgress func(done, total int)) (PublishResult, error) {
	_ = sourceConnection // see doc comment: informational only, source is never touched.

	result := PublishResult{
		Created:          []PublishedTest{},
		AlreadyPublished: []string{},
		Failed:           []PublishFailure{},
	}

	tests, err := p.Hub.ListHubTests(workspaceID)
	if err != nil {
		return result, fmt.Errorf("list hub tests: %w", err)
	}

	caps := p.Target.Capabilities()
	total := len(tests)

	for i, t := range tests {
		p.publishOne(ctx, workspaceID, targetConnection, t, caps, &result)
		if onProgress != nil {
			onProgress(i+1, total)
		}
	}

	return result, nil
}

// publishOne handles exactly one hub test, appending to result and never
// returning an error itself — every failure path is recorded in
// result.Failed instead, which is what gives PublishTests its per-test
// isolation.
func (p *Publisher) publishOne(ctx context.Context, workspaceID, targetConnection string, t HubTest, caps backend.Capabilities, result *PublishResult) {
	if _, ok, err := p.Refs.ExternalRef(workspaceID, entityTypeTest, t.Key, targetConnection); err != nil {
		result.Failed = append(result.Failed, PublishFailure{LocalKey: t.Key, Error: fmt.Sprintf("check existing publish: %v", err)})
		return
	} else if ok {
		result.AlreadyPublished = append(result.AlreadyPublished, t.Key)
		return
	}

	targetKey, err := p.Target.CreateTest(ctx, p.TargetProjectKey, t.Summary, t.Description, t.Priority, t.Labels, t.Components)
	if err != nil {
		result.Failed = append(result.Failed, PublishFailure{LocalKey: t.Key, Error: fmt.Sprintf("create test: %v", err)})
		return
	}

	steps, err := p.Hub.ListHubSteps(workspaceID, t.Key)
	if err != nil {
		result.Failed = append(result.Failed, PublishFailure{LocalKey: t.Key, Error: fmt.Sprintf("read hub steps: %v", err)})
		return
	}
	if err := p.publishSteps(ctx, targetKey, steps); err != nil {
		result.Failed = append(result.Failed, PublishFailure{LocalKey: t.Key, Error: fmt.Sprintf("create step: %v", err)})
		return
	}

	if mappedStatus, ok := p.Mapping.StatusMap[t.Status]; ok && mappedStatus != "" {
		if caps.SupportsWorkflowTransitions {
			// Creating-with-status on a workflow-driven target would need
			// resolving a valid transition off whatever status CreateTest
			// left the new issue in, which is out of scope for B5 (the brief
			// explicitly defers this) — the test is published without a
			// status write; the target's default status stands.
		} else {
			fields := p.Target.FieldsForJira(map[string]string{"status": mappedStatus})
			if err := p.Target.UpdateIssue(ctx, targetKey, fields); err != nil {
				result.Failed = append(result.Failed, PublishFailure{LocalKey: t.Key, Error: fmt.Sprintf("set status: %v", err)})
				return
			}
		}
	}

	if err := p.Refs.PutExternalRef(workspaceID, entityTypeTest, t.Key, targetConnection, targetKey, ""); err != nil {
		result.Failed = append(result.Failed, PublishFailure{LocalKey: t.Key, Error: fmt.Sprintf("record external ref: %v", err)})
		return
	}

	result.Created = append(result.Created, PublishedTest{LocalKey: t.Key, TargetKey: targetKey})
}

// publishSteps writes steps to targetKey per p.Mapping.StepMode: "flatten"
// joins every hub step into a single inline-text action (one CreateTestStep
// call, for a target whose StepModel is "inline-text" — e.g. Kiwi);
// anything else ("passthrough") calls CreateTestStep once per hub step,
// preserving order. No steps is a no-op either way.
func (p *Publisher) publishSteps(ctx context.Context, targetKey string, steps []HubStep) error {
	if len(steps) == 0 {
		return nil
	}
	if p.Mapping.StepMode == StepModeFlatten {
		_, err := p.Target.CreateTestStep(ctx, targetKey, joinFlattenedSteps(steps), "", "")
		return err
	}
	for _, s := range steps {
		if _, err := p.Target.CreateTestStep(ctx, targetKey, s.Action, s.Data, s.Expected); err != nil {
			return err
		}
	}
	return nil
}

// joinFlattenedSteps combines multiple hub steps into the single inline-text
// action a StepMode=="flatten" target expects — the inverse of the Kiwi
// adapter's read-side flattenSteps (internal/backend/kiwi/convert.go), which
// wraps a target's whole text field back into one neutral Step verbatim.
// Each source step becomes an "Action: .. / Data: .. / Expected: .." block
// (blank lines omitted for fields the step didn't use), blocks separated by
// a blank line — a close-to-reversible layout: a future read-side improvement
// could split on blank lines and re-parse the three "Label: value" lines back
// into per-step action/data/expected, though building that parser is outside
// B5's scope (join direction only).
func joinFlattenedSteps(steps []HubStep) string {
	blocks := make([]string, 0, len(steps))
	for _, s := range steps {
		var lines []string
		if s.Action != "" {
			lines = append(lines, "Action: "+s.Action)
		}
		if s.Data != "" {
			lines = append(lines, "Data: "+s.Data)
		}
		if s.Expected != "" {
			lines = append(lines, "Expected: "+s.Expected)
		}
		if len(lines) > 0 {
			blocks = append(blocks, strings.Join(lines, "\n"))
		}
	}
	return strings.Join(blocks, "\n\n")
}
