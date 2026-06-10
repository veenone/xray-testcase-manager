package testrepo

import "fmt"

// profileScopedTables lists every table whose rows belong to a single profile,
// so a profile delete can remove all of its cached data (FR-5.3).
var profileScopedTables = []string{
	"sync_state",
	"test_folder",
	"test_case",
	"precondition",
	"test_precondition",
	"pending_change",
	"audit_log",
	"test_step",
	"test_container",
	"test_container_test",
	"saved_view",
	"custom_field",
	"test_custom_field",
	"sync_log",
	"test_review",
}

// PurgeProfile deletes every cached row belonging to a profile (FR-5.3) so its
// data doesn't linger after the profile is removed.
func (r *Repository) PurgeProfile(profileID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, table := range profileScopedTables {
		if _, err := tx.Exec(
			fmt.Sprintf("DELETE FROM %s WHERE profile_id = ?", table), profileID,
		); err != nil {
			return fmt.Errorf("purge %s: %w", table, err)
		}
	}
	return tx.Commit()
}

// CleanSampleContainers deletes the sample Test Sets / Plans / Executions that
// SeedSampleContainers created — their keys are "<projectKey>-SET-n",
// "-PLAN-n", "-EXEC-n", which real Jira issue keys ("<projectKey>-<number>")
// never match — plus their Test links. Real synced data is left untouched.
// Returns the number of containers removed.
func (r *Repository) CleanSampleContainers(profileID, projectKey string) (int, error) {
	if projectKey == "" {
		projectKey = "SAMPLE"
	}
	tx, err := r.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	removed := 0
	for _, infix := range []string{"-SET-%", "-PLAN-%", "-EXEC-%"} {
		pattern := projectKey + infix
		if _, err := tx.Exec(
			`DELETE FROM test_container_test WHERE profile_id = ? AND container_key LIKE ?`,
			profileID, pattern,
		); err != nil {
			return 0, fmt.Errorf("clean sample links: %w", err)
		}
		res, err := tx.Exec(
			`DELETE FROM test_container WHERE profile_id = ? AND jira_key LIKE ?`,
			profileID, pattern,
		)
		if err != nil {
			return 0, fmt.Errorf("clean sample containers: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			removed += int(n)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit clean sample data: %w", err)
	}
	return removed, nil
}
