package coverage

import (
	"fmt"

	"github.com/google/uuid"
)

type ChangeRequest struct {
	ID              string `json:"id"`
	CRKey           string `json:"crKey"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	TargetVersionID string `json:"targetVersionId"`
	Risk            string `json:"risk"`
	Description     string `json:"description"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type CRDecision struct {
	RequirementKey string `json:"requirementKey"`
	ProjectKey     string `json:"projectKey"`
	Decision       string `json:"decision"`
	Note           string `json:"note"`
}

type CRImpactResult struct {
	CR           ChangeRequest `json:"cr"`
	Decisions    []CRDecision  `json:"decisions"`
	CanAccept    int           `json:"canAccept"`
	CannotAccept int           `json:"cannotAccept"`
	Pending      int           `json:"pending"`
}

func (m *Module) ListChangeRequests(profileID, canonicalID string) ([]ChangeRequest, error) {
	rows, err := m.db.Query(
		`SELECT id, cr_key, title, status, target_version_id, risk, description, created_at, updated_at
		   FROM change_request WHERE profile_id=? AND canonical_id=? ORDER BY created_at DESC`,
		profileID, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("list change requests: %w", err)
	}
	defer rows.Close()
	out := []ChangeRequest{}
	for rows.Next() {
		var c ChangeRequest
		if err := rows.Scan(&c.ID, &c.CRKey, &c.Title, &c.Status, &c.TargetVersionID, &c.Risk, &c.Description, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (m *Module) CreateChangeRequest(profileID, canonicalID, crKey, title, status, targetVersionID, risk, description string) (string, error) {
	if title == "" {
		return "", fmt.Errorf("title is required")
	}
	if status == "" {
		status = "proposed"
	}
	if risk == "" {
		risk = "low"
	}
	id := uuid.NewString()
	now := nowISO()
	if _, err := m.db.Exec(
		`INSERT INTO change_request (profile_id,id,canonical_id,cr_key,title,status,target_version_id,risk,description,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		profileID, id, canonicalID, crKey, title, status, targetVersionID, risk, description, now, now); err != nil {
		return "", fmt.Errorf("create change request: %w", err)
	}
	return id, nil
}

func (m *Module) UpdateChangeRequest(profileID, id, crKey, title, status, targetVersionID, risk, description string) error {
	if title == "" {
		return fmt.Errorf("title is required")
	}
	res, err := m.db.Exec(
		`UPDATE change_request SET cr_key=?, title=?, status=?, target_version_id=?, risk=?, description=?, updated_at=?
		  WHERE profile_id=? AND id=?`,
		crKey, title, status, targetVersionID, risk, description, nowISO(), profileID, id)
	if err != nil {
		return fmt.Errorf("update change request: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("change request %q not found", id)
	}
	return nil
}

func (m *Module) DeleteChangeRequest(profileID, id string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM cr_member_decision WHERE profile_id=? AND cr_id=?`, profileID, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM change_request WHERE profile_id=? AND id=?`, profileID, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *Module) SetCRDecision(profileID, crID, requirementKey, decision, note string) error {
	if decision == "" {
		decision = "pending"
	}
	_, err := m.db.Exec(
		`INSERT INTO cr_member_decision (profile_id, cr_id, requirement_key, decision, note, updated_at)
		 VALUES (?,?,?,?,?,?)
		 ON CONFLICT(profile_id, cr_id, requirement_key) DO UPDATE SET decision=excluded.decision, note=excluded.note, updated_at=excluded.updated_at`,
		profileID, crID, requirementKey, decision, note, nowISO())
	if err != nil {
		return fmt.Errorf("set CR decision: %w", err)
	}
	return nil
}

// CRImpact returns the CR plus, for every member of its canonical, that member's
// decision (defaulting to pending), with tallies.
func (m *Module) CRImpact(profileID, crID string) (CRImpactResult, error) {
	var res CRImpactResult
	var canonicalID string
	c := &res.CR
	if err := m.db.QueryRow(
		`SELECT id, canonical_id, cr_key, title, status, target_version_id, risk, description, created_at, updated_at
		   FROM change_request WHERE profile_id=? AND id=?`, profileID, crID).
		Scan(&c.ID, &canonicalID, &c.CRKey, &c.Title, &c.Status, &c.TargetVersionID, &c.Risk, &c.Description, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return res, fmt.Errorf("change request not found: %w", err)
	}
	rows, err := m.db.Query(
		`SELECT mm.requirement_key, COALESCE(r.project_key,''), COALESCE(d.decision,'pending'), COALESCE(d.note,'')
		   FROM canonical_requirement_member mm
		   LEFT JOIN requirement r ON r.profile_id=mm.profile_id AND r.jira_key=mm.requirement_key
		   LEFT JOIN cr_member_decision d ON d.profile_id=mm.profile_id AND d.cr_id=? AND d.requirement_key=mm.requirement_key
		  WHERE mm.profile_id=? AND mm.canonical_id=?
		  ORDER BY r.project_key, mm.requirement_key`,
		crID, profileID, canonicalID)
	if err != nil {
		return res, fmt.Errorf("read decisions: %w", err)
	}
	defer rows.Close()
	res.Decisions = []CRDecision{}
	for rows.Next() {
		var d CRDecision
		if err := rows.Scan(&d.RequirementKey, &d.ProjectKey, &d.Decision, &d.Note); err != nil {
			return res, err
		}
		switch d.Decision {
		case "can_accept":
			res.CanAccept++
		case "cannot_accept":
			res.CannotAccept++
		default:
			res.Pending++
		}
		res.Decisions = append(res.Decisions, d)
	}
	return res, rows.Err()
}
