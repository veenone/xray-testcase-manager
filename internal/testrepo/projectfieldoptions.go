package testrepo

import "fmt"

// ReplaceProjectFieldOptions atomically replaces the cached field options for
// a (profile, project, field) triple. field must be "component" or
// "fixversion". A nil or empty values slice clears the existing options.
func (r *Repository) ReplaceProjectFieldOptions(profileID, projectKey, field string, values []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`DELETE FROM project_field_option
		 WHERE profile_id = ? AND project_key = ? AND field = ?`,
		profileID, projectKey, field,
	); err != nil {
		return fmt.Errorf("clear project field options: %w", err)
	}
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO project_field_option
			   (profile_id, project_key, field, value) VALUES (?, ?, ?, ?)`,
			profileID, projectKey, field, v,
		); err != nil {
			return fmt.Errorf("insert project field option %q: %w", v, err)
		}
	}
	return tx.Commit()
}

// ListProjectFieldOptions returns the cached field option values for a
// (profile, project, field) triple, sorted alphabetically. Returns an empty
// (non-nil) slice when nothing is cached.
func (r *Repository) ListProjectFieldOptions(profileID, projectKey, field string) ([]string, error) {
	rows, err := r.db.Query(
		`SELECT value FROM project_field_option
		 WHERE profile_id = ? AND project_key = ? AND field = ?
		 ORDER BY value`,
		profileID, projectKey, field,
	)
	if err != nil {
		return nil, fmt.Errorf("list project field options: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if out == nil {
		out = []string{}
	}
	return out, rows.Err()
}
