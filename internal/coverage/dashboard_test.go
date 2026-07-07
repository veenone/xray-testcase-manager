package coverage

import "testing"

func TestDashboards(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	cid, _ := m.CreateCanonical(p, "Login", "", "")
	v1, _ := m.CreateVersion(p, cid, "1.0", "stable", "")
	v2, _ := m.CreateVersion(p, cid, "2.0", "beta", "")
	for _, k := range []string{"BANK-1", "SAMSU-1", "TELCO-1"} {
		st.DB().Exec(`INSERT INTO requirement (profile_id, jira_key, project_key) VALUES (?, ?, 'CUST')`, p, k)
	}
	m.SetMembers(p, cid, []string{"BANK-1", "SAMSU-1", "TELCO-1"})
	m.SetMemberVersion(p, cid, "BANK-1", v1)
	m.SetMemberVersion(p, cid, "SAMSU-1", v2)
	m.SetMemberVersion(p, cid, "TELCO-1", v2)

	dist, err := m.VersionDistribution(p, cid)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, d := range dist {
		got[d.VersionName] = d.MemberCount
	}
	if got["1.0"] != 1 || got["2.0"] != 2 {
		t.Errorf("distribution = %v, want 1.0:1 2.0:2", got)
	}

	crID, _ := m.CreateChangeRequest(p, cid, "CHG-1", "Add OAuth", "approved", v2, "low", "")
	m.SetCRDecision(p, crID, "SAMSU-1", "can_accept", "")
	adopt, err := m.CRAdoption(p, cid)
	if err != nil {
		t.Fatal(err)
	}
	if len(adopt) != 1 || adopt[0].CanAccept != 1 || adopt[0].Pending != 2 {
		t.Errorf("adoption = %+v, want 1 CR with 1 can-accept / 2 pending", adopt)
	}
}
