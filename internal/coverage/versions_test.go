package coverage

import "testing"

func TestVersionCRUDAndClone(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	cid, err := m.CreateCanonical(p, "C_Sign", "", "")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := m.CreateVersion(p, cid, "2.40", "stable", "")
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	// Build a small model under v1: group→param→value, with a mapping.
	gid, _ := m.UpsertNode(p, NodeEdit{Kind: "group", VersionID: v1, Name: "Mechanism"})
	pid, _ := m.UpsertNode(p, NodeEdit{Kind: "parameter", GroupID: gid, Name: "pMech"})
	vid, _ := m.UpsertNode(p, NodeEdit{Kind: "value", ParameterID: pid, Name: "RSA_PKCS", IsRequired: true})
	seedTest(t, st, p, "QA-1", "PASS", "")
	if err := m.SetValueTests(p, vid, []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	// Clone v1 -> v2: full model + mapping copied, source untouched, new ids.
	v2, err := m.CloneVersion(p, v1, "3.0", "beta")
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	m1, _ := m.GetParamModel(p, v1)
	m2, _ := m.GetParamModel(p, v2)
	if len(m2.Groups) != 1 || len(m2.Groups[0].Parameters) != 1 || len(m2.Groups[0].Parameters[0].Values) != 1 {
		t.Fatalf("clone model shape wrong: %+v", m2)
	}
	if m2.Groups[0].ID == m1.Groups[0].ID {
		t.Error("cloned group must have a new id")
	}
	// Cloned value carries the mapping.
	clonedVID := m2.Groups[0].Parameters[0].Values[0].ID
	keys, _ := m.ListValueTests(p, clonedVID)
	if len(keys) != 1 || keys[0] != "QA-1" {
		t.Errorf("cloned mapping = %v, want [QA-1]", keys)
	}

	vers, _ := m.ListVersions(p, cid)
	if len(vers) != 2 {
		t.Fatalf("versions = %d, want 2", len(vers))
	}

	// Member lock.
	if _, err := st.DB().Exec(`INSERT INTO requirement (profile_id, jira_key, project_key) VALUES (?, 'R-1', 'CUST')`, p); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMembers(p, cid, []string{"R-1"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMemberVersion(p, cid, "R-1", v2); err != nil {
		t.Fatalf("set member version: %v", err)
	}

	// DeleteVersion cascades.
	if err := m.DeleteVersion(p, v1); err != nil {
		t.Fatalf("delete version: %v", err)
	}
	if vers, _ := m.ListVersions(p, cid); len(vers) != 1 {
		t.Errorf("after delete, versions = %d, want 1", len(vers))
	}
	var groups int
	st.DB().QueryRow(`SELECT COUNT(*) FROM coverage_param_group WHERE profile_id=? AND version_id=?`, p, v1).Scan(&groups)
	if groups != 0 {
		t.Errorf("deleted version still has %d groups", groups)
	}
}
