package coverage

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CanonicalRequirement is a local "functional requirement" node that groups
// equivalent requirement issues spread across customer projects. It owns a
// parameter-coverage model so coverage is defined once and reused (PRD Topic 1).
type CanonicalRequirement struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	MemberCount int    `json:"memberCount"`
}

// ReuseRow answers "which customer projects reuse this functional requirement?"
// — one row per member requirement issue, carrying its project so the UI can
// group by customer/project.
type ReuseRow struct {
	CanonicalID       string `json:"canonicalId"`
	RequirementKey    string `json:"requirementKey"`
	ProjectKey        string `json:"projectKey"`
	Summary           string `json:"summary"`
	Status            string `json:"status"`
	AcceptedVersionID string `json:"acceptedVersionId"`
}

// nowISO returns an ISO-8601 UTC timestamp, matching the format used across the
// store (e.g. profile.created_at, test_run timestamps).
func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

// ListCanonical returns the profile's canonical requirements with their member
// counts, ordered by name.
func (m *Module) ListCanonical(profileID string) ([]CanonicalRequirement, error) {
	rows, err := m.db.Query(
		`SELECT cr.id, cr.name, cr.category, cr.description, cr.created_at, cr.updated_at,
		        (SELECT COUNT(*) FROM canonical_requirement_member mm
		           WHERE mm.profile_id = cr.profile_id AND mm.canonical_id = cr.id) AS member_count
		   FROM canonical_requirement cr
		  WHERE cr.profile_id = ?
		  ORDER BY cr.name COLLATE NOCASE`,
		profileID)
	if err != nil {
		return nil, fmt.Errorf("list canonical requirements: %w", err)
	}
	defer rows.Close()
	out := []CanonicalRequirement{}
	for rows.Next() {
		var c CanonicalRequirement
		if err := rows.Scan(&c.ID, &c.Name, &c.Category, &c.Description,
			&c.CreatedAt, &c.UpdatedAt, &c.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateCanonical inserts a new canonical requirement and returns its id.
func (m *Module) CreateCanonical(profileID, name, category, description string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	id := uuid.NewString()
	now := nowISO()
	if _, err := m.db.Exec(
		`INSERT INTO canonical_requirement
		   (profile_id, id, name, category, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		profileID, id, name, category, description, now, now,
	); err != nil {
		return "", fmt.Errorf("create canonical requirement: %w", err)
	}
	return id, nil
}

// RenameCanonical updates a canonical requirement's editable fields.
func (m *Module) RenameCanonical(profileID, id, name, category, description string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	res, err := m.db.Exec(
		`UPDATE canonical_requirement
		    SET name = ?, category = ?, description = ?, updated_at = ?
		  WHERE profile_id = ? AND id = ?`,
		name, category, description, nowISO(), profileID, id)
	if err != nil {
		return fmt.Errorf("rename canonical requirement: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("canonical requirement %q not found", id)
	}
	return nil
}

// DeleteCanonical removes a canonical requirement and its entire parameter model
// (versions → groups → parameters → values → mappings) plus its memberships, in
// one transaction. Deleting a canonical is the only cascade in the module.
func (m *Module) DeleteCanonical(profileID, id string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// --- Cascade via canonical_version (version-rooted groups, current schema) ---
	if _, err := tx.Exec(
		`DELETE FROM coverage_value_test WHERE profile_id=? AND value_id IN (
		    SELECT v.id FROM coverage_param_value v
		    JOIN coverage_parameter p ON p.profile_id=v.profile_id AND p.id=v.parameter_id
		    JOIN coverage_param_group g ON g.profile_id=p.profile_id AND g.id=p.group_id
		    JOIN canonical_version cv ON cv.profile_id=g.profile_id AND cv.id=g.version_id
		    WHERE g.profile_id=? AND cv.canonical_id=?)`,
		profileID, profileID, id); err != nil {
		return fmt.Errorf("delete version mappings: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM coverage_param_value WHERE profile_id=? AND parameter_id IN (
		    SELECT p.id FROM coverage_parameter p
		    JOIN coverage_param_group g ON g.profile_id=p.profile_id AND g.id=p.group_id
		    JOIN canonical_version cv ON cv.profile_id=g.profile_id AND cv.id=g.version_id
		    WHERE g.profile_id=? AND cv.canonical_id=?)`,
		profileID, profileID, id); err != nil {
		return fmt.Errorf("delete version values: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM coverage_parameter WHERE profile_id=? AND group_id IN (
		    SELECT g.id FROM coverage_param_group g
		    JOIN canonical_version cv ON cv.profile_id=g.profile_id AND cv.id=g.version_id
		    WHERE g.profile_id=? AND cv.canonical_id=?)`,
		profileID, profileID, id); err != nil {
		return fmt.Errorf("delete version parameters: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM coverage_param_group WHERE profile_id=? AND version_id IN (
		    SELECT id FROM canonical_version WHERE profile_id=? AND canonical_id=?)`,
		profileID, profileID, id); err != nil {
		return fmt.Errorf("delete version groups: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM canonical_version WHERE profile_id=? AND canonical_id=?`,
		profileID, id); err != nil {
		return fmt.Errorf("delete versions: %w", err)
	}

	// --- Legacy: groups with canonical_id set (backward compat with pre-v36 data) ---
	if _, err := tx.Exec(
		`DELETE FROM coverage_value_test WHERE profile_id=? AND value_id IN (
		    SELECT v.id FROM coverage_param_value v
		    JOIN coverage_parameter p ON p.profile_id=v.profile_id AND p.id=v.parameter_id
		    JOIN coverage_param_group g ON g.profile_id=p.profile_id AND g.id=p.group_id
		    WHERE g.profile_id=? AND g.canonical_id=?)`,
		profileID, profileID, id); err != nil {
		return fmt.Errorf("delete mappings: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM coverage_param_value WHERE profile_id=? AND parameter_id IN (
		    SELECT p.id FROM coverage_parameter p
		    JOIN coverage_param_group g ON g.profile_id=p.profile_id AND g.id=p.group_id
		    WHERE g.profile_id=? AND g.canonical_id=?)`,
		profileID, profileID, id); err != nil {
		return fmt.Errorf("delete values: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM coverage_parameter WHERE profile_id=? AND group_id IN (
		    SELECT id FROM coverage_param_group WHERE profile_id=? AND canonical_id=?)`,
		profileID, profileID, id); err != nil {
		return fmt.Errorf("delete parameters: %w", err)
	}
	// --- Change requests and their member decisions ---
	if _, err := tx.Exec(
		`DELETE FROM cr_member_decision WHERE profile_id=? AND cr_id IN (
		    SELECT id FROM change_request WHERE profile_id=? AND canonical_id=?)`,
		profileID, profileID, id); err != nil {
		return fmt.Errorf("delete cr decisions: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM change_request WHERE profile_id=? AND canonical_id=?`,
		profileID, id); err != nil {
		return fmt.Errorf("delete change requests: %w", err)
	}

	for _, q := range []string{
		`DELETE FROM coverage_param_group WHERE profile_id=? AND canonical_id=?`,
		`DELETE FROM canonical_requirement_member WHERE profile_id=? AND canonical_id=?`,
		`DELETE FROM canonical_requirement WHERE profile_id=? AND id=?`,
	} {
		if _, err := tx.Exec(q, profileID, id); err != nil {
			return fmt.Errorf("delete canonical: %w", err)
		}
	}
	return tx.Commit()
}

// SetMembers replaces the canonical's membership set with the given requirement
// keys, in one transaction. Idempotent — re-sending the same set is a no-op
// beyond timestamps.
func (m *Module) SetMembers(profileID, canonicalID string, requirementKeys []string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`DELETE FROM canonical_requirement_member WHERE profile_id = ? AND canonical_id = ?`,
		profileID, canonicalID); err != nil {
		return fmt.Errorf("clear members: %w", err)
	}
	now := nowISO()
	seen := map[string]bool{}
	for _, key := range requirementKeys {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if _, err := tx.Exec(
			`INSERT INTO canonical_requirement_member
			   (profile_id, canonical_id, requirement_key, added_at)
			 VALUES (?, ?, ?, ?)`,
			profileID, canonicalID, key, now); err != nil {
			return fmt.Errorf("add member %q: %w", key, err)
		}
	}
	return tx.Commit()
}

// ListReuse returns the member requirements of a canonical node joined to their
// synced requirement rows, so the UI can show which projects/customers reuse it.
// Members whose requirement isn't synced yet still appear (LEFT JOIN), with
// empty project/summary.
func (m *Module) ListReuse(profileID, canonicalID string) ([]ReuseRow, error) {
	rows, err := m.db.Query(
		`SELECT mm.canonical_id, mm.requirement_key,
		        COALESCE(r.project_key,''), COALESCE(r.summary,''), COALESCE(r.status,''),
		        COALESCE(mm.accepted_version_id,'')
		   FROM canonical_requirement_member mm
		   LEFT JOIN requirement r
		     ON r.profile_id = mm.profile_id AND r.jira_key = mm.requirement_key
		  WHERE mm.profile_id = ? AND mm.canonical_id = ?
		  ORDER BY r.project_key COLLATE NOCASE, mm.requirement_key`,
		profileID, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("list reuse: %w", err)
	}
	defer rows.Close()
	out := []ReuseRow{}
	for rows.Next() {
		var rr ReuseRow
		if err := rows.Scan(&rr.CanonicalID, &rr.RequirementKey,
			&rr.ProjectKey, &rr.Summary, &rr.Status, &rr.AcceptedVersionID); err != nil {
			return nil, err
		}
		out = append(out, rr)
	}
	return out, rows.Err()
}
