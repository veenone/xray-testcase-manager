package coverage

import "fmt"

type VersionShare struct {
	VersionID   string `json:"versionId"`
	VersionName string `json:"versionName"`
	Status      string `json:"status"`
	MemberCount int    `json:"memberCount"`
}

// VersionDistribution counts members locked to each version of a canonical.
// Members with no lock are reported under an empty version ("Unassigned").
func (m *Module) VersionDistribution(profileID, canonicalID string) ([]VersionShare, error) {
	rows, err := m.db.Query(
		`SELECT v.id, v.name, v.status,
		        (SELECT COUNT(*) FROM canonical_requirement_member mm
		           WHERE mm.profile_id=v.profile_id AND mm.canonical_id=v.canonical_id AND mm.accepted_version_id=v.id) AS members
		   FROM canonical_version v
		  WHERE v.profile_id=? AND v.canonical_id=? ORDER BY v.sort_order, v.name`,
		profileID, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("version distribution: %w", err)
	}
	defer rows.Close()
	out := []VersionShare{}
	for rows.Next() {
		var s VersionShare
		if err := rows.Scan(&s.VersionID, &s.VersionName, &s.Status, &s.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var unassigned int
	m.db.QueryRow(
		`SELECT COUNT(*) FROM canonical_requirement_member WHERE profile_id=? AND canonical_id=? AND accepted_version_id=''`,
		profileID, canonicalID).Scan(&unassigned)
	if unassigned > 0 {
		out = append(out, VersionShare{VersionName: "Unassigned", MemberCount: unassigned})
	}
	return out, nil
}

type CRShare struct {
	CRID         string `json:"crId"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	CanAccept    int    `json:"canAccept"`
	CannotAccept int    `json:"cannotAccept"`
	Pending      int    `json:"pending"`
}

// CRAdoption summarises each CR of a canonical: accept/cannot/pending across all
// the canonical's members (pending = members without an explicit decision).
func (m *Module) CRAdoption(profileID, canonicalID string) ([]CRShare, error) {
	crs, err := m.ListChangeRequests(profileID, canonicalID)
	if err != nil {
		return nil, err
	}
	var memberCount int
	m.db.QueryRow(
		`SELECT COUNT(*) FROM canonical_requirement_member WHERE profile_id=? AND canonical_id=?`,
		profileID, canonicalID).Scan(&memberCount)
	out := []CRShare{}
	for _, cr := range crs {
		s := CRShare{CRID: cr.ID, Title: cr.Title, Status: cr.Status}
		rows, err := m.db.Query(
			`SELECT decision, COUNT(*) FROM cr_member_decision WHERE profile_id=? AND cr_id=? GROUP BY decision`,
			profileID, cr.ID)
		if err != nil {
			return nil, fmt.Errorf("CR adoption: %w", err)
		}
		decided := 0
		for rows.Next() {
			var d string
			var n int
			if err := rows.Scan(&d, &n); err != nil {
				rows.Close()
				return nil, err
			}
			switch d {
			case "can_accept":
				s.CanAccept = n
				decided += n
			case "cannot_accept":
				s.CannotAccept = n
				decided += n
			}
		}
		rows.Close()
		s.Pending = memberCount - decided
		if s.Pending < 0 {
			s.Pending = 0
		}
		out = append(out, s)
	}
	return out, nil
}
