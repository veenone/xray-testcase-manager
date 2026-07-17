package syncer

import (
	"testing"

	"xray-test-manager/internal/jira"
)

// TestMapRunRowsMapsComment verifies mapRunRows carries the Xray-synced
// tr.Comment through to TestRunRow.Comment (T7 — it previously mapped
// Defects but dropped Comment).
func TestMapRunRowsMapsComment(t *testing.T) {
	runs := []jira.TestRun{
		{TestKey: "QA-1", Status: "FAIL", Comment: "Reproduced failure; logged BUG-100 for follow-up."},
		{TestKey: "QA-2", Status: "PASS"},
	}
	rows := mapRunRows(runs, "QA-TE-1", nil)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Comment != "Reproduced failure; logged BUG-100 for follow-up." {
		t.Errorf("rows[0].Comment = %q, want the synced comment", rows[0].Comment)
	}
	if rows[1].Comment != "" {
		t.Errorf("rows[1].Comment = %q, want empty (no comment set)", rows[1].Comment)
	}
}
