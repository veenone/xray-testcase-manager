package coverage

import (
	"math"
	"testing"
)

func TestProjectCoverageAndSankey(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	db := st.DB()
	db.Exec(`INSERT INTO requirement (profile_id, jira_key, project_key, summary) VALUES (?,?,?,?)`, p, "CUST-A-1", "CUST-A", "a")
	db.Exec(`INSERT INTO requirement (profile_id, jira_key, project_key, summary) VALUES (?,?,?,?)`, p, "CUST-B-1", "CUST-B", "b")
	// two live tests for mapping (jira_id is NOT NULL in the schema)
	db.Exec(`INSERT INTO test_case (profile_id, jira_key, jira_id, summary) VALUES (?,?,?,?)`, p, "T-1", "t1", "t1")
	db.Exec(`INSERT INTO test_case (profile_id, jira_key, jira_id, summary) VALUES (?,?,?,?)`, p, "T-2", "t2", "t2")

	cid, _ := m.CreateCanonical(p, "C_Sign", "PKCS11", "")
	m.SetMembers(p, cid, []string{"CUST-A-1", "CUST-B-1"})
	vid, _ := m.CreateVersion(p, cid, "2.40", "stable", "")
	gid, _ := m.UpsertNode(p, NodeEdit{Kind: "group", VersionID: vid, Name: "G"})
	pid, _ := m.UpsertNode(p, NodeEdit{Kind: "parameter", GroupID: gid, Name: "P"})
	v1, _ := m.UpsertNode(p, NodeEdit{Kind: "value", ParameterID: pid, Name: "v1", IsRequired: true})
	v2, _ := m.UpsertNode(p, NodeEdit{Kind: "value", ParameterID: pid, Name: "v2", IsRequired: true})
	_, _ = m.UpsertNode(p, NodeEdit{Kind: "value", ParameterID: pid, Name: "v3", IsRequired: true}) // gap
	m.SetValueTests(p, v1, []string{"T-1"})
	m.SetValueTests(p, v2, []string{"T-2"})

	rows, err := m.ProjectCoverage(p)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]ProjectCoverageRow{}
	for _, r := range rows {
		byKey[r.ProjectKey] = r
	}
	for _, k := range []string{"CUST-A", "CUST-B"} {
		r := byKey[k]
		if r.TotalValues != 3 || r.CoveredValues != 2 || r.FunctionsReused != 1 {
			t.Errorf("%s: %+v, want total 3 covered 2 functions 1", k, r)
		}
		if math.Abs(r.Percent-66.6667) > 0.1 {
			t.Errorf("%s percent = %v", k, r.Percent)
		}
	}

	s, err := m.ProjectRelationSankey(p)
	if err != nil {
		t.Fatal(err)
	}
	var cov, gap int
	for _, l := range s.Links {
		if l.Source == "fn:"+cid && l.Target == "out:covered" {
			cov = l.Value
		}
		if l.Source == "fn:"+cid && l.Target == "out:gap" {
			gap = l.Value
		}
	}
	if cov != 2 || gap != 1 {
		t.Errorf("sankey fn→outcome covered=%d gap=%d, want 2/1", cov, gap)
	}
}
