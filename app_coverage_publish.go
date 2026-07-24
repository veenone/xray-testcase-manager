package main

import (
	"fmt"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/coveragepublish"
)

// Bound methods for publishing coverage groups into the backend as Test Sets
// and reading their publish/drift status (internal/coveragepublish). Both
// methods are thin adapters -- requireStore(), resolve the backend, gate on
// capability, delegate -- matching app_coverage.go's style for the rest of
// the coverage module.

// requireContainerCapability resolves profileID's backend and checks it can
// hold a Test Set container: coverage group publishing writes one Test Set
// per group, so a backend without backend.KindTestSet in its Capabilities
// (Kiwi has TestPlan/TestRun but no Test Set) cannot support the feature at
// all. Returns backend.ErrUnsupported (wrapped) rather than attempting the
// operation, so the caller degrades cleanly instead of erroring deep inside
// the publish engine.
func (a *App) requireContainerCapability(profileID string) (backend.Backend, error) {
	b, err := a.backendFor(profileID)
	if err != nil {
		return nil, err
	}
	caps := b.Capabilities()
	if !caps.SupportsContainers || !containsContainerKind(caps.ContainerKinds, backend.KindTestSet) {
		return nil, fmt.Errorf("coverage publish: %w", backend.ErrUnsupported)
	}
	return b, nil
}

// containsContainerKind reports whether kind is present in kinds.
func containsContainerKind(kinds []string, kind string) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// PublishCoverageGroups publishes every coverage group under versionID into a
// Test Set in the profile's configured Jira project (profile.ProjectKey --
// the same project CreateContainerAndAllocate creates a container in),
// creating or refreshing each group's Test Set membership and rendered
// description. One group's failure is recorded on its GroupResult rather
// than aborting the run (coveragepublish.PublishGroups). Returns
// backend.ErrUnsupported when the profile's backend has no Test Set
// container kind (e.g. a Kiwi connection).
func (a *App) PublishCoverageGroups(profileID, versionID string) (result coveragepublish.Result, err error) {
	defer recoverToError("PublishCoverageGroups", &err)
	var empty coveragepublish.Result
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	b, err := a.requireContainerCapability(profileID)
	if err != nil {
		return empty, err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return empty, err
	}
	pub := coveragepublish.NewPublisher(a.store, a.cov, b)
	return pub.PublishGroups(a.ctx, profileID, p.ProjectKey, versionID)
}

// GetCoveragePublishStatus returns the publish/drift status of every coverage
// group under versionID (NotPublished / InSync / LocalChanges / Drift /
// Conflict) via a read-only comparison that makes no backend calls itself
// (coveragepublish.DetectDrift). It still resolves the backend and
// capability-gates like PublishCoverageGroups so the frontend can tell
// whether to show the publish affordance at all.
func (a *App) GetCoveragePublishStatus(profileID, versionID string) ([]coveragepublish.GroupStatus, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	b, err := a.requireContainerCapability(profileID)
	if err != nil {
		return nil, err
	}
	pub := coveragepublish.NewPublisher(a.store, a.cov, b)
	return pub.DetectDrift(profileID, versionID)
}
