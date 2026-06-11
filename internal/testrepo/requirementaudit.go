package testrepo

import (
	"fmt"
)

// requirementAuditHeader is the column order for the requirement coverage /
// sign-off audit export: one row per (requirement, covering test) pair, plus a
// single test-less row for each uncovered requirement. It joins the derived
// coverage with each covering Test's run result and review sign-off so the
// sheet stands alone as an audit record.
var requirementAuditHeader = []string{
	"Requirement", "Project", "Type", "Requirement Summary", "Requirement Status",
	"Coverage", "Test", "Test Summary", "Test Status", "Run Result",
	"Review Verdict", "Reviewer", "Reviewed At",
}

// ExportRequirementAudit renders the requirement coverage / sign-off audit as a
// CSV or XLSX file's bytes. format is "csv" or "xlsx". Requirements are listed
// in the same order as the coverage view; each covering Test contributes a row
// carrying its run result and review verdict, and an uncovered requirement
// yields a single row with empty Test columns.
func (r *Repository) ExportRequirementAudit(profileID, format string) ([]byte, error) {
	reqs, err := r.ListRequirementsWithCoverage(profileID)
	if err != nil {
		return nil, err
	}
	reviews, err := r.allReviews(profileID)
	if err != nil {
		return nil, err
	}

	rows := make([][]string, 0, len(reqs)+1)
	rows = append(rows, requirementAuditHeader)
	for _, req := range reqs {
		tests, err := r.ListTestsForRequirement(profileID, req.Key)
		if err != nil {
			return nil, err
		}
		if len(tests) == 0 {
			rows = append(rows, []string{
				req.Key, req.ProjectKey, req.IssueType, req.Summary, req.Status,
				req.Coverage, "", "", "", "", "", "", "",
			})
			continue
		}
		for _, t := range tests {
			rv := reviews[t.Key]
			rows = append(rows, []string{
				req.Key, req.ProjectKey, req.IssueType, req.Summary, req.Status,
				req.Coverage, t.Key, t.Summary, t.Status, t.RunStatus,
				rv.Verdict, rv.Reviewer, rv.ReviewedAt,
			})
		}
	}

	if format == "xlsx" {
		return writeXLSX(rows)
	}
	return writeCSV(rows)
}

// allReviews reads every Test review for a profile into a map keyed by Test key,
// so the audit export joins reviews in one query instead of one per covering
// Test.
func (r *Repository) allReviews(profileID string) (map[string]Review, error) {
	rows, err := r.db.Query(
		`SELECT test_key, verdict, reviewer, note, reviewed_at
		 FROM test_review WHERE profile_id = ?`, profileID)
	if err != nil {
		return nil, fmt.Errorf("read reviews: %w", err)
	}
	defer rows.Close()
	out := map[string]Review{}
	for rows.Next() {
		var key string
		var rv Review
		if err := rows.Scan(&key, &rv.Verdict, &rv.Reviewer, &rv.Note, &rv.ReviewedAt); err != nil {
			return nil, err
		}
		out[key] = rv
	}
	return out, rows.Err()
}
