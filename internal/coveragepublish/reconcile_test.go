package coveragepublish

import (
	"context"
	"testing"
)

// assertKeys fails the test unless got (order-independent) equals want.
func assertKeys(t *testing.T, label string, got, want []string) {
	t.Helper()
	g, w := sorted(got), sorted(want)
	if len(g) != len(w) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range g {
		if g[i] != w[i] {
			t.Fatalf("%s = %v, want %v", label, got, want)
		}
	}
}

func TestDetectDrift_NeverPublishedReportsNotPublished(t *testing.T) {
	const p = "profile-1"
	pub, _, cov := newTestPublisher(t, newFakeBackend())

	versionID, groupID, _ := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})

	statuses, err := pub.DetectDrift(p, versionID)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %+v, want 1 entry", statuses)
	}
	gs := statuses[0]
	if gs.GroupID != groupID {
		t.Fatalf("GroupID = %q, want %q", gs.GroupID, groupID)
	}
	if gs.State != NotPublished {
		t.Fatalf("State = %q, want NotPublished", gs.State)
	}
	if gs.ContainerKey != "" {
		t.Fatalf("ContainerKey = %q, want empty", gs.ContainerKey)
	}
	if len(gs.LocalAdded) != 0 || len(gs.LocalRemoved) != 0 || len(gs.RemoteAdded) != 0 || len(gs.RemoteRemoved) != 0 {
		t.Fatalf("expected no diff keys for an unpublished group, got %+v", gs)
	}
}

func TestDetectDrift_InSyncImmediatelyAfterPublish(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS", "ED25519"})
	seedTestCase(t, st, p, "QA-1")
	seedTestCase(t, st, p, "QA-2")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}
	if err := cov.SetValueTests(p, valueIDs[1], []string{"QA-2"}); err != nil {
		t.Fatal(err)
	}

	if _, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID); err != nil {
		t.Fatalf("PublishGroups: %v", err)
	}

	// No pull-sync needed: publish mirrors membership write-through, so R
	// must already equal P here and the state must be InSync, not a false
	// Drift.
	statuses, err := pub.DetectDrift(p, versionID)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %+v, want 1 entry", statuses)
	}
	gs := statuses[0]
	if gs.State != InSync {
		t.Fatalf("State = %q, want InSync (got %+v)", gs.State, gs)
	}
	if gs.ContainerKey == "" {
		t.Fatalf("expected a ContainerKey once published")
	}
	if len(gs.LocalAdded) != 0 || len(gs.LocalRemoved) != 0 || len(gs.RemoteAdded) != 0 || len(gs.RemoteRemoved) != 0 {
		t.Fatalf("expected no diff keys when in sync, got %+v", gs)
	}
}

func TestDetectDrift_LocalChangesWhenModelDivergesFromSnapshot(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS", "ED25519"})
	seedTestCase(t, st, p, "QA-1")
	seedTestCase(t, st, p, "QA-2")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	if _, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID); err != nil {
		t.Fatalf("PublishGroups: %v", err)
	}

	// Local edit after publish, with no republish and no remote change:
	// map QA-2 to the second value.
	if err := cov.SetValueTests(p, valueIDs[1], []string{"QA-2"}); err != nil {
		t.Fatal(err)
	}

	statuses, err := pub.DetectDrift(p, versionID)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	gs := statuses[0]
	if gs.State != LocalChanges {
		t.Fatalf("State = %q, want LocalChanges (got %+v)", gs.State, gs)
	}
	assertKeys(t, "LocalAdded", gs.LocalAdded, []string{"QA-2"})
	assertKeys(t, "LocalRemoved", gs.LocalRemoved, nil)
	assertKeys(t, "RemoteAdded", gs.RemoteAdded, nil)
	assertKeys(t, "RemoteRemoved", gs.RemoteRemoved, nil)
}

func TestDetectDrift_DriftWhenJiraSideTestSetDivergesFromSnapshot(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	seedTestCase(t, st, p, "QA-1")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	first, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("PublishGroups: %v", err)
	}
	containerKey := first.Groups[0].ContainerKey

	// Simulate a pull-sync picking up a manual edit on the Test Set issue in
	// Jira: someone added QA-9 directly. The local coverage model is
	// untouched.
	mirrorMembership(t, st, p, containerKey, []string{"QA-1", "QA-9"})

	statuses, err := pub.DetectDrift(p, versionID)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	gs := statuses[0]
	if gs.State != Drift {
		t.Fatalf("State = %q, want Drift (got %+v)", gs.State, gs)
	}
	assertKeys(t, "LocalAdded", gs.LocalAdded, nil)
	assertKeys(t, "LocalRemoved", gs.LocalRemoved, nil)
	assertKeys(t, "RemoteAdded", gs.RemoteAdded, []string{"QA-9"})
	assertKeys(t, "RemoteRemoved", gs.RemoteRemoved, nil)
}

func TestDetectDrift_ConflictWhenBothSidesDivergeFromSnapshot(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS", "ED25519"})
	seedTestCase(t, st, p, "QA-1")
	seedTestCase(t, st, p, "QA-2")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	first, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("PublishGroups: %v", err)
	}
	containerKey := first.Groups[0].ContainerKey

	// Local model moves (QA-2 mapped, unpublished)...
	if err := cov.SetValueTests(p, valueIDs[1], []string{"QA-2"}); err != nil {
		t.Fatal(err)
	}
	// ...and Jira's Test Set independently moves too (QA-9 added by hand,
	// picked up by a pull-sync).
	mirrorMembership(t, st, p, containerKey, []string{"QA-1", "QA-9"})

	statuses, err := pub.DetectDrift(p, versionID)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	gs := statuses[0]
	if gs.State != Conflict {
		t.Fatalf("State = %q, want Conflict (got %+v)", gs.State, gs)
	}
	assertKeys(t, "LocalAdded", gs.LocalAdded, []string{"QA-2"})
	assertKeys(t, "LocalRemoved", gs.LocalRemoved, nil)
	assertKeys(t, "RemoteAdded", gs.RemoteAdded, []string{"QA-9"})
	assertKeys(t, "RemoteRemoved", gs.RemoteRemoved, nil)
}

// TestDetectDrift_ReturnsEveryGroupIncludingUnpublishedOnes proves a version
// with a mix of published and never-published groups gets a status for
// each -- the caller can show every group, not just the ones with a
// publication row.
func TestDetectDrift_ReturnsEveryGroupIncludingUnpublishedOnes(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, publishedGroupID, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	// Second group deliberately left unpublished: PublishGroups publishes
	// every group under a version, so simulate a partial-publish state by
	// hand-seeding the publication row for only the first group instead of
	// calling PublishGroups on the whole version.
	unpublishedGroupID, _ := addGroupToVersion(t, cov, p, versionID, "Session", 1, []string{"Valid"})
	seedTestCase(t, st, p, "QA-1")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	const containerKey = "PROJ-TS1"
	if err := pub.putPublication(p, publishedGroupID, containerKey, []string{"QA-1"}); err != nil {
		t.Fatalf("putPublication: %v", err)
	}
	mirrorMembership(t, st, p, containerKey, []string{"QA-1"})

	statuses, err := pub.DetectDrift(p, versionID)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("statuses = %+v, want 2 entries", statuses)
	}

	byID := map[string]GroupStatus{}
	for _, gs := range statuses {
		byID[gs.GroupID] = gs
	}
	if got := byID[publishedGroupID]; got.State != InSync {
		t.Fatalf("published group state = %q, want InSync (got %+v)", got.State, got)
	}
	if got := byID[unpublishedGroupID]; got.State != NotPublished {
		t.Fatalf("unpublished group state = %q, want NotPublished (got %+v)", got.State, got)
	}
}
