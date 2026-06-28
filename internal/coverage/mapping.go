package coverage

import (
	"fmt"
)

// CandidateTest is a Test eligible to be mapped to a parameter value: it is
// linked (via test_requirement) to one of the canonical node's member
// requirements. Summary/Status are empty when the test isn't synced locally.
type CandidateTest struct {
	TestKey string `json:"testKey"`
	Summary string `json:"summary"`
	Status  string `json:"status"`
}

// StaleMapping is a value→test mapping whose test no longer exists in the local
// test_case cache (deleted or never synced) — surfaced, never auto-deleted.
type StaleMapping struct {
	ValueID    string `json:"valueId"`
	ValueLabel string `json:"valueLabel"`
	TestKey    string `json:"testKey"`
}

// ListValueTests returns the test keys currently mapped to a parameter value.
func (m *Module) ListValueTests(profileID, valueID string) ([]string, error) {
	rows, err := m.db.Query(
		`SELECT test_key FROM coverage_value_test
		  WHERE profile_id = ? AND value_id = ? ORDER BY test_key`,
		profileID, valueID)
	if err != nil {
		return nil, fmt.Errorf("list value tests: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// SetValueTests replaces the set of tests mapped to a parameter value, in one
// transaction. This is the local "tested" signal — purely local, no Jira write.
func (m *Module) SetValueTests(profileID, valueID string, testKeys []string) error {
	// Guard: the value must exist (a typo'd id would otherwise create orphans).
	var exists int
	if err := m.db.QueryRow(
		`SELECT COUNT(*) FROM coverage_param_value WHERE profile_id = ? AND id = ?`,
		profileID, valueID).Scan(&exists); err != nil {
		return fmt.Errorf("check value: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("parameter value %q not found", valueID)
	}

	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`DELETE FROM coverage_value_test WHERE profile_id = ? AND value_id = ?`,
		profileID, valueID); err != nil {
		return fmt.Errorf("clear mappings: %w", err)
	}
	now := nowISO()
	seen := map[string]bool{}
	for _, k := range testKeys {
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		if _, err := tx.Exec(
			`INSERT INTO coverage_value_test (profile_id, value_id, test_key, created_at)
			 VALUES (?, ?, ?, ?)`,
			profileID, valueID, k, now); err != nil {
			return fmt.Errorf("map test %q: %w", k, err)
		}
	}
	return tx.Commit()
}

// ListCandidateTests returns the Tests linked to any member requirement of the
// canonical node — the pool a user picks from when mapping a value to a test.
// Constraining to linked tests keeps mappings meaningful and limits staleness.
func (m *Module) ListCandidateTests(profileID, canonicalID string) ([]CandidateTest, error) {
	rows, err := m.db.Query(
		`SELECT DISTINCT tr.test_key,
		        COALESCE(tc.summary,''), COALESCE(tc.status,'')
		   FROM test_requirement tr
		   JOIN canonical_requirement_member mm
		     ON mm.profile_id = tr.profile_id AND mm.requirement_key = tr.requirement_key
		   LEFT JOIN test_case tc
		     ON tc.profile_id = tr.profile_id AND tc.jira_key = tr.test_key
		  WHERE tr.profile_id = ? AND mm.canonical_id = ?
		  ORDER BY tr.test_key`,
		profileID, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("list candidate tests: %w", err)
	}
	defer rows.Close()
	out := []CandidateTest{}
	for rows.Next() {
		var c CandidateTest
		if err := rows.Scan(&c.TestKey, &c.Summary, &c.Status); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DetectStaleMappings returns value→test mappings (optionally scoped to one
// canonical node when canonicalID != "") whose test_key is absent from
// test_case, so the UI can badge them. Mappings are kept, never auto-pruned.
func (m *Module) DetectStaleMappings(profileID, canonicalID string) ([]StaleMapping, error) {
	q := `SELECT vt.value_id, pv.value_label, vt.test_key
	        FROM coverage_value_test vt
	        JOIN coverage_param_value pv ON pv.profile_id = vt.profile_id AND pv.id = vt.value_id
	        LEFT JOIN test_case tc ON tc.profile_id = vt.profile_id AND tc.jira_key = vt.test_key
	       WHERE vt.profile_id = ? AND tc.jira_key IS NULL`
	args := []any{profileID}
	if canonicalID != "" {
		q += ` AND pv.parameter_id IN (
		         SELECT p.id FROM coverage_parameter p
		         JOIN coverage_param_group g ON g.profile_id = p.profile_id AND g.id = p.group_id
		         WHERE g.profile_id = ? AND g.canonical_id = ?)`
		args = append(args, profileID, canonicalID)
	}
	q += ` ORDER BY pv.value_label, vt.test_key`
	rows, err := m.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("detect stale mappings: %w", err)
	}
	defer rows.Close()
	out := []StaleMapping{}
	for rows.Next() {
		var s StaleMapping
		if err := rows.Scan(&s.ValueID, &s.ValueLabel, &s.TestKey); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
