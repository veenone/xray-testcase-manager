package coverage

import "testing"

// TestDeleteCanonicalCRCleanup verifies that DeleteCanonical removes change_request
// rows and their cr_member_decision rows (cascade gap fix).
func TestDeleteCanonicalCRCleanup(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"

	cid, err := m.CreateCanonical(p, "Login", "", "")
	if err != nil {
		t.Fatalf("create canonical: %v", err)
	}
	v1, err := m.CreateVersion(p, cid, "1.0", "stable", "")
	if err != nil {
		t.Fatalf("create version: %v", err)
	}

	// Seed and add a member.
	st.DB().Exec(`INSERT INTO requirement (profile_id, jira_key, project_key) VALUES (?, 'REQ-1', 'CUST')`, p)
	if err := m.SetMembers(p, cid, []string{"REQ-1"}); err != nil {
		t.Fatalf("set members: %v", err)
	}

	// Create a change request pointing at v1.
	crID, err := m.CreateChangeRequest(p, cid, "CHG-1", "Add OAuth", "approved", v1, "low", "")
	if err != nil {
		t.Fatalf("create CR: %v", err)
	}

	// Record a decision by the member.
	if err := m.SetCRDecision(p, crID, "REQ-1", "can_accept", "looks good"); err != nil {
		t.Fatalf("set CR decision: %v", err)
	}

	// Pre-condition: rows exist.
	var crCount, decCount int
	st.DB().QueryRow(`SELECT COUNT(*) FROM change_request WHERE profile_id=?`, p).Scan(&crCount)
	if crCount != 1 {
		t.Fatalf("pre-condition: expected 1 change_request, got %d", crCount)
	}
	st.DB().QueryRow(`SELECT COUNT(*) FROM cr_member_decision WHERE profile_id=?`, p).Scan(&decCount)
	if decCount != 1 {
		t.Fatalf("pre-condition: expected 1 cr_member_decision, got %d", decCount)
	}

	// Act: delete the canonical.
	if err := m.DeleteCanonical(p, cid); err != nil {
		t.Fatalf("delete canonical: %v", err)
	}

	// Assert: change_request rows are gone.
	st.DB().QueryRow(`SELECT COUNT(*) FROM change_request WHERE profile_id=?`, p).Scan(&crCount)
	if crCount != 0 {
		t.Errorf("after DeleteCanonical: expected 0 change_request rows, got %d", crCount)
	}

	// Assert: cr_member_decision rows are gone.
	st.DB().QueryRow(`SELECT COUNT(*) FROM cr_member_decision WHERE profile_id=?`, p).Scan(&decCount)
	if decCount != 0 {
		t.Errorf("after DeleteCanonical: expected 0 cr_member_decision rows, got %d", decCount)
	}
}

// TestDeleteVersionClearsMemberLocks verifies that DeleteVersion resets
// accepted_version_id on members locked to the deleted version (cascade gap fix).
func TestDeleteVersionClearsMemberLocks(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"

	cid, err := m.CreateCanonical(p, "Auth", "", "")
	if err != nil {
		t.Fatalf("create canonical: %v", err)
	}
	v1, err := m.CreateVersion(p, cid, "1.0", "stable", "")
	if err != nil {
		t.Fatalf("create version: %v", err)
	}

	// Seed and add a member, then lock it to v1.
	st.DB().Exec(`INSERT INTO requirement (profile_id, jira_key, project_key) VALUES (?, 'REQ-1', 'CUST')`, p)
	if err := m.SetMembers(p, cid, []string{"REQ-1"}); err != nil {
		t.Fatalf("set members: %v", err)
	}
	if err := m.SetMemberVersion(p, cid, "REQ-1", v1); err != nil {
		t.Fatalf("set member version: %v", err)
	}

	// Pre-condition: member is locked to v1.
	var acceptedVersionID string
	st.DB().QueryRow(
		`SELECT accepted_version_id FROM canonical_requirement_member WHERE profile_id=? AND canonical_id=? AND requirement_key='REQ-1'`,
		p, cid).Scan(&acceptedVersionID)
	if acceptedVersionID != v1 {
		t.Fatalf("pre-condition: member accepted_version_id = %q, want %q", acceptedVersionID, v1)
	}

	// Pre-condition: VersionDistribution shows member counted under v1.
	dist, err := m.VersionDistribution(p, cid)
	if err != nil {
		t.Fatalf("version distribution pre: %v", err)
	}
	found := false
	for _, d := range dist {
		if d.VersionID == v1 && d.MemberCount == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("pre-condition: expected v1 to have 1 member in VersionDistribution, got %+v", dist)
	}

	// Act: delete v1.
	if err := m.DeleteVersion(p, v1); err != nil {
		t.Fatalf("delete version: %v", err)
	}

	// Assert: member's accepted_version_id is now empty string.
	acceptedVersionID = "NOT_CLEARED"
	st.DB().QueryRow(
		`SELECT accepted_version_id FROM canonical_requirement_member WHERE profile_id=? AND canonical_id=? AND requirement_key='REQ-1'`,
		p, cid).Scan(&acceptedVersionID)
	if acceptedVersionID != "" {
		t.Errorf("after DeleteVersion: member accepted_version_id = %q, want empty string", acceptedVersionID)
	}

	// Assert: VersionDistribution no longer shows v1 (deleted) and the member
	// appears in the Unassigned bucket.
	dist, err = m.VersionDistribution(p, cid)
	if err != nil {
		t.Fatalf("version distribution post: %v", err)
	}
	for _, d := range dist {
		if d.VersionID == v1 {
			t.Errorf("after DeleteVersion: v1 still appears in VersionDistribution with %d members", d.MemberCount)
		}
	}
	unassigned := 0
	for _, d := range dist {
		if d.VersionName == "Unassigned" {
			unassigned = d.MemberCount
		}
	}
	if unassigned != 1 {
		t.Errorf("after DeleteVersion: expected 1 unassigned member, got %d (dist=%+v)", unassigned, dist)
	}
}
