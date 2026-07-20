package main

import (
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"xray-test-manager/internal/bridge"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

// --- Bridge publish engine (Phase 6 task B5) --------------------------------
//
// PublishToTarget is the App-layer entry point for bridge.Publisher: it wires
// the target connection's backend + project key, the saved/default mapping
// (B4's GetBridgeMapping), the local hub cache (appHubReader, below), and the
// store's external_ref accessors together, then runs the resumable,
// TESTS-only publish (B5). Containers/preconditions/requirements/links are
// not published here — that is task B5b; the source connection is never read
// from or written to.

// PublishToTarget publishes workspaceID's hub tests that have not already
// been published to targetConnectionID into that connection's backend,
// applying the saved (or freshly-computed default) bridge mapping for
// (workspaceID, sourceConnectionID, targetConnectionID). Progress is emitted
// on the "bridge:publish-progress" Wails event (mirroring the sync engine's
// "sync:progress" convention) so the bridge wizard (B6) can show a live
// done/total count.
func (a *App) PublishToTarget(workspaceID, sourceConnectionID, targetConnectionID string) (bridge.PublishResult, error) {
	if err := a.requireStore(); err != nil {
		return bridge.PublishResult{}, err
	}

	target, err := a.backendForConnection(targetConnectionID)
	if err != nil {
		return bridge.PublishResult{}, fmt.Errorf("load target connection: %w", err)
	}
	targetConn, err := a.connections.Get(targetConnectionID)
	if err != nil {
		return bridge.PublishResult{}, fmt.Errorf("load target connection: %w", err)
	}
	mapping, err := a.GetBridgeMapping(workspaceID, sourceConnectionID, targetConnectionID)
	if err != nil {
		return bridge.PublishResult{}, fmt.Errorf("load bridge mapping: %w", err)
	}

	pub := bridge.NewPublisher(target, targetConn.ProjectKey, appHubReader{repo: a.repo}, a.store, mapping)

	onProgress := func(done, total int) {
		runtime.EventsEmit(a.ctx, "bridge:publish-progress", syncer.Progress{
			Fetched: done,
			Total:   total,
			Stage:   "Publishing tests",
		})
	}
	defer runtime.EventsEmit(a.ctx, "bridge:publish-progress", syncer.Progress{Done: true})

	return pub.PublishTests(a.ctx, workspaceID, sourceConnectionID, targetConnectionID, onProgress)
}

// appHubReader adapts *testrepo.Repository to bridge.HubReader: it hides
// testrepo.ListTests's page-at-a-time paging (capped at 500 rows/call, see
// testrepo.go) behind a single "every hub test" read, and narrows
// testrepo.TestCase/Step down to bridge's neutral HubTest/HubStep shapes —
// the same boundary-conversion pattern app.go's toJiraTransitions/
// toJiraBugCreateFields already use for backend.* -> jira.*.
type appHubReader struct {
	repo *testrepo.Repository
}

var _ bridge.HubReader = appHubReader{}

// hubPageSize matches testrepo.ListTests's own page cap (a Limit above 500
// is clamped back down to 100), so each page request asks for the largest
// page ListTests will actually honor.
const hubPageSize = 500

func (h appHubReader) ListHubTests(workspaceID string) ([]bridge.HubTest, error) {
	var out []bridge.HubTest
	offset := 0
	for {
		page, err := h.repo.ListTests(workspaceID, testrepo.Query{
			SortBy: "key",
			Limit:  hubPageSize,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		for _, t := range page.Tests {
			out = append(out, bridge.HubTest{
				Key:         t.Key,
				Summary:     t.Summary,
				Description: t.Description,
				Status:      t.Status,
				Priority:    t.Priority,
				Labels:      t.Labels,
				Components:  t.Components,
			})
		}
		offset += len(page.Tests)
		if len(page.Tests) == 0 || offset >= page.Total {
			break
		}
	}
	return out, nil
}

func (h appHubReader) ListHubSteps(workspaceID, testKey string) ([]bridge.HubStep, error) {
	steps, err := h.repo.ListTestSteps(workspaceID, testKey)
	if err != nil {
		return nil, err
	}
	out := make([]bridge.HubStep, len(steps))
	for i, s := range steps {
		out[i] = bridge.HubStep{Action: s.Action, Data: s.Data, Expected: s.Expected}
	}
	return out, nil
}
