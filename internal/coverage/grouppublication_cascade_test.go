package coverage

import "testing"

// seedPublication inserts a coverage_group_publication row for groupID, as if
// the group had already been published to Xray as a Test Set.
func seedPublication(t *testing.T, m *Module, profileID, groupID, containerKey string) {
	t.Helper()
	if _, err := m.db.Exec(
		`INSERT INTO coverage_group_publication
		   (profile_id, group_id, container_key, published_tests, published_at)
		 VALUES (?, ?, ?, 'QA-1', '2026-07-24T00:00:00Z')`,
		profileID, groupID, containerKey); err != nil {
		t.Fatalf("seed coverage_group_publication: %v", err)
	}
}

// TestDeleteNodeGroupCascadesPublication verifies DeleteNode's "group" branch
// also removes any coverage_group_publication row for the deleted group, so a
// republish of a differently-scoped group never inherits a stale record.
func TestDeleteNodeGroupCascadesPublication(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"

	cid, err := m.CreateCanonical(p, "C", "", "")
	if err != nil {
		t.Fatal(err)
	}
	vid, err := m.CreateVersion(p, cid, "1.0", "stable", "")
	if err != nil {
		t.Fatal(err)
	}
	gid, err := m.UpsertNode(p, NodeEdit{Kind: "group", VersionID: vid, Name: "G"})
	if err != nil {
		t.Fatal(err)
	}
	seedPublication(t, m, p, gid, "DEMO-TE-1")

	if got := countRows(t, m, `SELECT COUNT(*) FROM coverage_group_publication WHERE profile_id=? AND group_id=?`, p, gid); got != 1 {
		t.Fatalf("pre-condition: publication row missing (%d)", got)
	}

	if err := m.DeleteNode(p, "group", gid); err != nil {
		t.Fatalf("delete group: %v", err)
	}

	if got := countRows(t, m, `SELECT COUNT(*) FROM coverage_group_publication WHERE profile_id=? AND group_id=?`, p, gid); got != 0 {
		t.Errorf("publication row not cascaded on group delete (%d)", got)
	}
}

// TestDeleteCanonicalCascadesPublication verifies DeleteCanonical removes
// coverage_group_publication rows for groups reached via both cascade paths:
// version-rooted groups (current schema, groups joined through
// canonical_version) and legacy groups (pre-Topic-2 data with canonical_id set
// directly on coverage_param_group and no version_id).
func TestDeleteCanonicalCascadesPublication(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"

	cid, err := m.CreateCanonical(p, "C", "", "")
	if err != nil {
		t.Fatal(err)
	}
	vid, err := m.CreateVersion(p, cid, "1.0", "stable", "")
	if err != nil {
		t.Fatal(err)
	}

	// Version-rooted group, created through the normal API.
	versionedGID, err := m.UpsertNode(p, NodeEdit{Kind: "group", VersionID: vid, Name: "G-versioned"})
	if err != nil {
		t.Fatal(err)
	}
	seedPublication(t, m, p, versionedGID, "DEMO-TE-1")

	// Legacy group: canonical_id set directly, no version_id, mirroring
	// pre-Topic-2 data that predates canonical_version.
	const legacyGID = "legacy-group-1"
	if _, err := m.db.Exec(
		`INSERT INTO coverage_param_group (profile_id, id, canonical_id, version_id, name, sort_order)
		 VALUES (?, ?, ?, '', 'G-legacy', 0)`,
		p, legacyGID, cid); err != nil {
		t.Fatalf("seed legacy group: %v", err)
	}
	seedPublication(t, m, p, legacyGID, "DEMO-TE-2")

	if got := countRows(t, m, `SELECT COUNT(*) FROM coverage_group_publication WHERE profile_id=?`, p); got != 2 {
		t.Fatalf("pre-condition: expected 2 publication rows, got %d", got)
	}

	if err := m.DeleteCanonical(p, cid); err != nil {
		t.Fatalf("delete canonical: %v", err)
	}

	if got := countRows(t, m, `SELECT COUNT(*) FROM coverage_group_publication WHERE profile_id=?`, p); got != 0 {
		t.Errorf("publication rows not cascaded on canonical delete, %d remain", got)
	}
}
