package store

import "testing"

func TestTestCaseBodyColumnsExist(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cols := map[string]bool{}
	rows, err := st.DB().Query(`PRAGMA table_info(test_case)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	for _, want := range []string{"cucumber_scenario", "cucumber_type", "generic_definition"} {
		if !cols[want] {
			t.Errorf("test_case missing column %q", want)
		}
	}
}
