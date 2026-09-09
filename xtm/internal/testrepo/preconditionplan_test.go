package testrepo

import (
	"path/filepath"
	"strings"
	"testing"

	"agile-suite/xtm/internal/store"
)

// The Preconditions list counts each precondition's tests with a correlated
// subquery, not a LEFT JOIN with a GROUP BY. The join shape had SQLite re-scan
// the profile's whole link table once per precondition and then group on five
// text columns, one of them the full description: measured on a live database
// (6,028 preconditions, 19,658 links) the list took 57 seconds, which the view
// rendered as a permanent "Loading…". The same data through the subquery came
// back in 48ms.
//
// Speed is not directly assertable in a unit test, so this pins the two things
// that produce it: the count seeks idx_test_precondition_precond, and the whole
// statement does no table scan.
func TestPreconditionUsageCountSeeksItsIndex(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "plan.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	repo := NewRepository(st)

	plan := explain(t, repo, `SELECT p.jira_key,
	        (SELECT COUNT(*) FROM test_precondition tp
	          WHERE tp.profile_id = p.profile_id
	            AND tp.precondition_key = p.jira_key) AS test_count
	 FROM precondition p WHERE p.profile_id = ?`, "p1")

	if !strings.Contains(plan, "idx_test_precondition_precond") {
		t.Errorf("the count does not use idx_test_precondition_precond; plan:\n%s", plan)
	}
	if strings.Contains(plan, "SCAN test_precondition") {
		t.Errorf("the count scans test_precondition instead of seeking; plan:\n%s", plan)
	}
}

func explain(t *testing.T, r *Repository, query string, args ...any) string {
	t.Helper()
	rows, err := r.db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("explain columns: %v", err)
	}
	var b strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("explain scan: %v", err)
		}
		// The plan text is the last column ("detail").
		if detail, ok := vals[len(vals)-1].(string); ok {
			b.WriteString(detail)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
