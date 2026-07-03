package coverage

import "strings"

// ProjectConfig is one in-scope Jira project for the coverage module.
type ProjectConfig struct {
	ProjectKey string `json:"projectKey"`
	Role       string `json:"role"`  // source | customer
	Label      string `json:"label"`
	SortOrder  int    `json:"sortOrder"`
}

// ListProjects returns the configured in-scope projects ordered by sort_order.
// When none are configured it derives a default from the requirements that are
// canonical members (so existing data yields an editable starting point); the
// derived list is NOT persisted.
func (m *Module) ListProjects(profileID string) ([]ProjectConfig, error) {
	rows, err := m.db.Query(
		`SELECT project_key, role, label, sort_order FROM coverage_project
		 WHERE profile_id = ? ORDER BY sort_order, project_key`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectConfig
	for rows.Next() {
		var p ProjectConfig
		if err := rows.Scan(&p.ProjectKey, &p.Role, &p.Label, &p.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return m.seedProjectsFromMembers(profileID)
	}
	return out, nil
}

// seedProjectsFromMembers derives an in-scope project list from the distinct
// project_key of requirements that are canonical members. All derived rows get
// role "customer"; the user re-labels/roles them in the UI.
func (m *Module) seedProjectsFromMembers(profileID string) ([]ProjectConfig, error) {
	rows, err := m.db.Query(
		`SELECT DISTINCT r.project_key FROM canonical_requirement_member mm
		   JOIN requirement r ON r.profile_id = mm.profile_id AND r.jira_key = mm.requirement_key
		  WHERE mm.profile_id = ? AND r.project_key <> '' ORDER BY r.project_key`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectConfig
	i := 0
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, ProjectConfig{ProjectKey: key, Role: "customer", Label: key, SortOrder: i})
		i++
	}
	return out, rows.Err()
}

// SetProjects replaces the in-scope project list for the profile in one tx.
// Input is validated at the boundary: blank project keys (after TrimSpace) are
// silently skipped, and duplicate keys are deduped (last write wins). The
// persisted sort_order is the compacted index over the surviving rows.
func (m *Module) SetProjects(profileID string, projects []ProjectConfig) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM coverage_project WHERE profile_id = ?`, profileID); err != nil {
		return err
	}

	// Validate: skip blank keys (UI seeds rows with projectKey:""), dedupe
	// by key keeping the last occurrence (last write wins).
	seen := map[string]ProjectConfig{}
	order := make([]string, 0, len(projects))
	for _, p := range projects {
		key := strings.TrimSpace(p.ProjectKey)
		if key == "" {
			continue
		}
		p.ProjectKey = key
		if _, exists := seen[key]; !exists {
			order = append(order, key)
		}
		seen[key] = p
	}

	for i, key := range order {
		p := seen[key]
		role := p.Role
		if role != "source" {
			role = "customer"
		}
		if _, err := tx.Exec(
			`INSERT INTO coverage_project (profile_id, project_key, role, label, sort_order)
			 VALUES (?,?,?,?,?)`, profileID, key, role, p.Label, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}
