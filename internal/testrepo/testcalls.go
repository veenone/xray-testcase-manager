package testrepo

import "fmt"

// TestCallLink is one "call test" relationship (FR-2.5, #2): a step in CallerKey
// calls CalledKey. Both ends are resolved to their summaries; CalledExists is
// false when the called test isn't in the local cache — a dangling or
// cross-project call worth flagging.
type TestCallLink struct {
	CallerKey     string `json:"callerKey"`
	CallerSummary string `json:"callerSummary"`
	CalledKey     string `json:"calledKey"`
	CalledSummary string `json:"calledSummary"`
	CalledExists  bool   `json:"calledExists"`
	StepIndex     int    `json:"stepIndex"`
}

// ListTestCallLinks returns every call-test relationship derived from the cached
// test steps — one per step whose called_test_key is set — ordered by caller
// then step position. It's a read-only projection of the step cache, so it
// tracks local edits and committed steps without a Jira round trip.
func (r *Repository) ListTestCallLinks(profileID string) ([]TestCallLink, error) {
	rows, err := r.db.Query(
		`SELECT s.test_key,
		        COALESCE(caller.summary, ''),
		        s.called_test_key,
		        COALESCE(called.summary, ''),
		        CASE WHEN called.jira_key IS NULL THEN 0 ELSE 1 END,
		        s.idx
		   FROM test_step s
		   JOIN test_case caller
		     ON caller.profile_id = s.profile_id AND caller.jira_key = s.test_key
		   LEFT JOIN test_case called
		     ON called.profile_id = s.profile_id AND called.jira_key = s.called_test_key
		  WHERE s.profile_id = ? AND s.called_test_key <> ''
		  ORDER BY s.test_key, s.idx`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("list test call links: %w", err)
	}
	defer rows.Close()

	out := []TestCallLink{}
	for rows.Next() {
		var (
			l      TestCallLink
			exists int
		)
		if err := rows.Scan(
			&l.CallerKey, &l.CallerSummary, &l.CalledKey,
			&l.CalledSummary, &exists, &l.StepIndex,
		); err != nil {
			return nil, err
		}
		l.CalledExists = exists != 0
		out = append(out, l)
	}
	return out, rows.Err()
}
