package coverage

import "testing"

func TestChangeRequestLifecycleAndImpact(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	cid, _ := m.CreateCanonical(p, "Login", "", "")
	v2, _ := m.CreateVersion(p, cid, "2.0", "beta", "")

	// Two member requirements.
	for _, k := range []string{"BANK-1", "SAMSU-1"} {
		st.DB().Exec(`INSERT INTO requirement (profile_id, jira_key, project_key) VALUES (?, ?, 'CUST')`, p, k)
	}
	m.SetMembers(p, cid, []string{"BANK-1", "SAMSU-1"})

	crID, err := m.CreateChangeRequest(p, cid, "CHG-1", "Add OAuth", "approved", v2, "low", "")
	if err != nil {
		t.Fatalf("create CR: %v", err)
	}
	if list, _ := m.ListChangeRequests(p, cid); len(list) != 1 || list[0].Title != "Add OAuth" {
		t.Fatalf("list CRs = %+v", list)
	}

	if err := m.SetCRDecision(p, crID, "BANK-1", "cannot_accept", "breaks API"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetCRDecision(p, crID, "SAMSU-1", "can_accept", ""); err != nil {
		t.Fatal(err)
	}

	imp, err := m.CRImpact(p, crID)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if imp.CanAccept != 1 || imp.CannotAccept != 1 || imp.Pending != 0 {
		t.Errorf("tallies = %d/%d/%d, want 1/1/0", imp.CanAccept, imp.CannotAccept, imp.Pending)
	}
	if len(imp.Decisions) != 2 {
		t.Errorf("decisions = %d, want 2 (one per member)", len(imp.Decisions))
	}

	if err := m.DeleteChangeRequest(p, crID); err != nil {
		t.Fatal(err)
	}
	if list, _ := m.ListChangeRequests(p, cid); len(list) != 0 {
		t.Errorf("after delete, CRs = %d, want 0", len(list))
	}
}
