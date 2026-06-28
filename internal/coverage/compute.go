package coverage

import (
	"fmt"

	"xray-test-manager/internal/testrepo"
)

// ValueCoverage annotates one parameter value with its tested state for the
// matrix. RunStatus reuses the shared coverage label (UNCOVERED/NOTRUN/PASSED/
// FAILED) derived from the value's live mapped tests.
type ValueCoverage struct {
	ValueID    string   `json:"valueId"`
	TestKeys   []string `json:"testKeys"` // live mappings only
	Tested     bool     `json:"tested"`
	RunStatus  string   `json:"runStatus"`
	IsRequired bool     `json:"isRequired"`
}

// GroupCoverage is the per-group roll-up. Total/Tested count required values
// only (the 100% denominator); Percent is Tested/Total.
type GroupCoverage struct {
	GroupID string  `json:"groupId"`
	Name    string  `json:"name"`
	Total   int     `json:"total"`
	Tested  int     `json:"tested"`
	Percent float64 `json:"percent"`
}

// CoverageReport is the headline result for one canonical requirement.
type CoverageReport struct {
	CanonicalID  string                   `json:"canonicalId"`
	TotalValues  int                      `json:"totalValues"`
	TestedValues int                      `json:"testedValues"`
	Percent      float64                  `json:"percent"`
	Groups       []GroupCoverage          `json:"groups"`
	Values       map[string]ValueCoverage `json:"values"` // keyed by value id
}

// Gap is a required parameter value with no live mapped test — the named work
// the QA team must fill to reach 100%.
type Gap struct {
	GroupName  string `json:"groupName"`
	ParamName  string `json:"paramName"`
	ValueID    string `json:"valueId"`
	ValueLabel string `json:"valueLabel"`
	ValueKind  string `json:"valueKind"`
	ErrorCode  string `json:"errorCode"`
}

// liveTestsByValue returns, per value id under the canonical node, the mapped
// test keys that still exist in test_case (stale mappings excluded).
func (m *Module) liveTestsByValue(profileID, canonicalID string) (map[string][]string, error) {
	rows, err := m.db.Query(
		`SELECT vt.value_id, vt.test_key
		   FROM coverage_value_test vt
		   JOIN coverage_param_value pv ON pv.profile_id = vt.profile_id AND pv.id = vt.value_id
		   JOIN coverage_parameter p   ON p.profile_id = pv.profile_id AND p.id = pv.parameter_id
		   JOIN coverage_param_group g ON g.profile_id = p.profile_id AND g.id = p.group_id
		   JOIN test_case tc           ON tc.profile_id = vt.profile_id AND tc.jira_key = vt.test_key
		  WHERE vt.profile_id = ? AND g.canonical_id = ?`,
		profileID, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("read live mappings: %w", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var valueID, testKey string
		if err := rows.Scan(&valueID, &testKey); err != nil {
			return nil, err
		}
		out[valueID] = append(out[valueID], testKey)
	}
	return out, rows.Err()
}

// ComputeCoverage produces the per-group and overall coverage for a canonical
// requirement. A required value is "tested" when it has ≥1 live mapped test;
// each value's run status is derived from those tests via the shared
// testrepo logic, so "is this test passing?" has one source of truth.
func (m *Module) ComputeCoverage(profileID, canonicalID string) (CoverageReport, error) {
	report := CoverageReport{
		CanonicalID: canonicalID,
		Groups:      []GroupCoverage{},
		Values:      map[string]ValueCoverage{},
	}

	live, err := m.liveTestsByValue(profileID, canonicalID)
	if err != nil {
		return report, err
	}
	runByTest, err := m.repo.ConsolidatedRunByTest(profileID)
	if err != nil {
		return report, fmt.Errorf("consolidated run status: %w", err)
	}

	rows, err := m.db.Query(
		`SELECT g.id, g.name, g.sort_order, pv.id, pv.is_required
		   FROM coverage_param_value pv
		   JOIN coverage_parameter p   ON p.profile_id = pv.profile_id AND p.id = pv.parameter_id
		   JOIN coverage_param_group g ON g.profile_id = p.profile_id AND g.id = p.group_id
		  WHERE pv.profile_id = ? AND g.canonical_id = ?
		  ORDER BY g.sort_order, g.name COLLATE NOCASE`,
		profileID, canonicalID)
	if err != nil {
		return report, fmt.Errorf("read values for coverage: %w", err)
	}
	defer rows.Close()

	groupIdx := map[string]int{}
	for rows.Next() {
		var groupID, groupName, valueID string
		var sortOrder, required int
		if err := rows.Scan(&groupID, &groupName, &sortOrder, &valueID, &required); err != nil {
			return report, err
		}

		testKeys := live[valueID]
		if testKeys == nil {
			testKeys = []string{}
		}
		statuses := make([]string, 0, len(testKeys))
		for _, k := range testKeys {
			statuses = append(statuses, runByTest[k])
		}
		isRequired := required != 0
		tested := len(testKeys) > 0
		report.Values[valueID] = ValueCoverage{
			ValueID:    valueID,
			TestKeys:   testKeys,
			Tested:     tested,
			RunStatus:  testrepo.DeriveCoverage(statuses, len(testKeys)),
			IsRequired: isRequired,
		}

		// Group + overall totals count required values only.
		gi, ok := groupIdx[groupID]
		if !ok {
			gi = len(report.Groups)
			groupIdx[groupID] = gi
			report.Groups = append(report.Groups, GroupCoverage{GroupID: groupID, Name: groupName})
		}
		if isRequired {
			report.Groups[gi].Total++
			report.TotalValues++
			if tested {
				report.Groups[gi].Tested++
				report.TestedValues++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return report, err
	}

	for i := range report.Groups {
		report.Groups[i].Percent = pct(report.Groups[i].Tested, report.Groups[i].Total)
	}
	report.Percent = pct(report.TestedValues, report.TotalValues)
	return report, nil
}

// ListGaps returns the required values that have no live mapped test, with
// enough context (group, parameter, error code) to act on.
func (m *Module) ListGaps(profileID, canonicalID string) ([]Gap, error) {
	rows, err := m.db.Query(
		`SELECT g.name, p.name, pv.id, pv.value_label, pv.value_kind, pv.error_code
		   FROM coverage_param_value pv
		   JOIN coverage_parameter p   ON p.profile_id = pv.profile_id AND p.id = pv.parameter_id
		   JOIN coverage_param_group g ON g.profile_id = p.profile_id AND g.id = p.group_id
		  WHERE pv.profile_id = ? AND g.canonical_id = ? AND pv.is_required = 1
		    AND NOT EXISTS (
		      SELECT 1 FROM coverage_value_test vt
		      JOIN test_case tc ON tc.profile_id = vt.profile_id AND tc.jira_key = vt.test_key
		      WHERE vt.profile_id = pv.profile_id AND vt.value_id = pv.id)
		  ORDER BY g.sort_order, g.name, p.sort_order, pv.sort_order, pv.value_label`,
		profileID, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("list gaps: %w", err)
	}
	defer rows.Close()
	out := []Gap{}
	for rows.Next() {
		var gap Gap
		if err := rows.Scan(&gap.GroupName, &gap.ParamName, &gap.ValueID,
			&gap.ValueLabel, &gap.ValueKind, &gap.ErrorCode); err != nil {
			return nil, err
		}
		out = append(out, gap)
	}
	return out, rows.Err()
}

// pct returns tested/total as a percentage rounded to one decimal, or 100 when
// there is nothing required to cover (an empty model is vacuously complete).
func pct(tested, total int) float64 {
	if total == 0 {
		return 100
	}
	v := float64(tested) / float64(total) * 100
	// round to 1 decimal
	return float64(int(v*10+0.5)) / 10
}
