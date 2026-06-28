package coverage

import (
	"fmt"

	"github.com/google/uuid"
)

// Version is one release line of a functional requirement; the parameter model
// (and therefore coverage) is measured per version.
type Version struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Notes     string `json:"notes"`
	SortOrder int    `json:"sortOrder"`
	CreatedAt string `json:"createdAt"`
}

func (m *Module) ListVersions(profileID, canonicalID string) ([]Version, error) {
	rows, err := m.db.Query(
		`SELECT id, name, status, notes, sort_order, created_at FROM canonical_version
		  WHERE profile_id = ? AND canonical_id = ? ORDER BY sort_order, name`,
		profileID, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.ID, &v.Name, &v.Status, &v.Notes, &v.SortOrder, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (m *Module) CreateVersion(profileID, canonicalID, name, status, notes string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("version name is required")
	}
	if status == "" {
		status = "planning"
	}
	var n int
	m.db.QueryRow(`SELECT COUNT(*) FROM canonical_version WHERE profile_id=? AND canonical_id=?`, profileID, canonicalID).Scan(&n)
	id := uuid.NewString()
	if _, err := m.db.Exec(
		`INSERT INTO canonical_version (profile_id, id, canonical_id, name, status, notes, sort_order, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		profileID, id, canonicalID, name, status, notes, n, nowISO()); err != nil {
		return "", fmt.Errorf("create version: %w", err)
	}
	return id, nil
}

func (m *Module) RenameVersion(profileID, id, name, status, notes string) error {
	if name == "" {
		return fmt.Errorf("version name is required")
	}
	res, err := m.db.Exec(
		`UPDATE canonical_version SET name=?, status=?, notes=? WHERE profile_id=? AND id=?`,
		name, status, notes, profileID, id)
	if err != nil {
		return fmt.Errorf("rename version: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("version %q not found", id)
	}
	return nil
}

func (m *Module) SetVersionStatus(profileID, id, status string) error {
	_, err := m.db.Exec(`UPDATE canonical_version SET status=? WHERE profile_id=? AND id=?`, status, profileID, id)
	return err
}

// DeleteVersion removes a version and the entire parameter model beneath it.
func (m *Module) DeleteVersion(profileID, id string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmts := []string{
		`DELETE FROM coverage_value_test WHERE profile_id=? AND value_id IN (
			SELECT v.id FROM coverage_param_value v
			JOIN coverage_parameter p ON p.profile_id=v.profile_id AND p.id=v.parameter_id
			JOIN coverage_param_group g ON g.profile_id=p.profile_id AND g.id=p.group_id
			WHERE g.profile_id=? AND g.version_id=?)`,
		`DELETE FROM coverage_param_value WHERE profile_id=? AND parameter_id IN (
			SELECT p.id FROM coverage_parameter p
			JOIN coverage_param_group g ON g.profile_id=p.profile_id AND g.id=p.group_id
			WHERE g.profile_id=? AND g.version_id=?)`,
		`DELETE FROM coverage_parameter WHERE profile_id=? AND group_id IN (
			SELECT id FROM coverage_param_group WHERE profile_id=? AND version_id=?)`,
		`DELETE FROM coverage_param_group WHERE profile_id=? AND version_id=?`,
	}
	for i, q := range stmts {
		args := []any{profileID, profileID, id}
		if i == len(stmts)-1 {
			args = []any{profileID, id}
		}
		if _, err := tx.Exec(q, args...); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM canonical_version WHERE profile_id=? AND id=?`, profileID, id); err != nil {
		return err
	}
	return tx.Commit()
}

// CloneVersion deep-copies a source version's groups→parameters→values and their
// test mappings into a new version of the same canonical.
func (m *Module) CloneVersion(profileID, sourceVersionID, name, status string) (string, error) {
	var canonicalID string
	if err := m.db.QueryRow(
		`SELECT canonical_id FROM canonical_version WHERE profile_id=? AND id=?`,
		profileID, sourceVersionID).Scan(&canonicalID); err != nil {
		return "", fmt.Errorf("source version not found: %w", err)
	}
	newVer, err := m.CreateVersion(profileID, canonicalID, name, status, "")
	if err != nil {
		return "", err
	}
	model, err := m.GetParamModel(profileID, sourceVersionID)
	if err != nil {
		return "", err
	}
	tx, err := m.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := nowISO()
	for _, g := range model.Groups {
		gid := uuid.NewString()
		if _, err := tx.Exec(
			`INSERT INTO coverage_param_group (profile_id,id,canonical_id,version_id,name,sort_order) VALUES (?,?,'',?,?,?)`,
			profileID, gid, newVer, g.Name, g.SortOrder); err != nil {
			return "", err
		}
		for _, par := range g.Parameters {
			pid := uuid.NewString()
			if _, err := tx.Exec(
				`INSERT INTO coverage_parameter (profile_id,id,group_id,name,kind,description,sort_order) VALUES (?,?,?,?,?,?,?)`,
				profileID, pid, gid, par.Name, par.Kind, par.Description, par.SortOrder); err != nil {
				return "", err
			}
			for _, v := range par.Values {
				vid := uuid.NewString()
				req := 0
				if v.IsRequired {
					req = 1
				}
				if _, err := tx.Exec(
					`INSERT INTO coverage_param_value (profile_id,id,parameter_id,value_label,value_kind,error_code,is_required,notes,sort_order)
					 VALUES (?,?,?,?,?,?,?,?,?)`,
					profileID, vid, pid, v.ValueLabel, v.ValueKind, v.ErrorCode, req, v.Notes, v.SortOrder); err != nil {
					return "", err
				}
				// Copy mappings from the source value.
				if _, err := tx.Exec(
					`INSERT OR IGNORE INTO coverage_value_test (profile_id,value_id,test_key,created_at)
					 SELECT profile_id, ?, test_key, ? FROM coverage_value_test WHERE profile_id=? AND value_id=?`,
					vid, now, profileID, v.ID); err != nil {
					return "", err
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return newVer, nil
}

// SetMemberVersion records which version a member (customer requirement) is on.
func (m *Module) SetMemberVersion(profileID, canonicalID, requirementKey, versionID string) error {
	res, err := m.db.Exec(
		`UPDATE canonical_requirement_member SET accepted_version_id=? WHERE profile_id=? AND canonical_id=? AND requirement_key=?`,
		versionID, profileID, canonicalID, requirementKey)
	if err != nil {
		return fmt.Errorf("set member version: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("member %q not found under canonical %q", requirementKey, canonicalID)
	}
	return nil
}
