package coverage

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// The demo example is deliberately aligned with xtm's built-in demo dataset
// (internal/jira/demo.go): tests are "<feature> <condition>" keyed {PROJ}-{n}
// with feature = demoFeatures[(n-1)%30] and condition = demoConditions[((n-1)/30)%12].
// Feature 0 is "Login", so DEMO-1 = "Login with valid input", DEMO-31 =
// "Login with invalid input", DEMO-61 = "Login from empty state", … Only the
// first 200 tests carry execution run statuses, so the mapped values below favour
// those (n ≤ 200) to show real pass/fail colours; a few higher ones read NOTRUN,
// and two are left as gaps. Requirement PRD-1 is "Login requirement".

// demoVal is one value in the Login example. testNum is the demo test number that
// exercises it ({PROJ}-{testNum}); gap=true leaves it intentionally unmapped.
type demoVal struct {
	label   string
	testNum int
	gap     bool
}

type demoGrp struct {
	name string
	vals []demoVal
}

// demoLogin decomposes the seeded "Login" feature into parameter values, each
// pointing at the actual demo test that exercises that condition. 12 values, 2
// gaps → 10/12 = 83.3%.
func demoLogin() []demoGrp {
	return []demoGrp{
		{"Credentials", []demoVal{
			{"Valid credentials", 1, false},    // DEMO-1  "Login with valid input"
			{"Invalid credentials", 31, false}, // DEMO-31 "Login with invalid input"
			{"Empty credentials", 61, false},   // DEMO-61 "Login from empty state"
			{"Special characters", 121, false}, // DEMO-121 "… with special characters"
		}},
		{"Input boundaries", []demoVal{
			{"Maximum length", 241, false}, // DEMO-241 "… with maximum boundary values" (NOTRUN, n>200)
			{"Minimum length", 0, true},    // gap
		}},
		{"User role", []demoVal{
			{"Admin user", 181, false}, // DEMO-181 "… as an admin user"
			{"Guest user", 211, false}, // DEMO-211 "… as a guest user" (NOTRUN)
		}},
		{"Resilience", []demoVal{
			{"After timeout", 91, false},      // DEMO-91  "… after timeout"
			{"On a slow network", 151, false}, // DEMO-151 "… on a slow network"
			{"After page reload", 301, false}, // DEMO-301 "… after page reload" (NOTRUN)
			{"Concurrent users", 0, true},     // gap
		}},
	}
}

const demoCanonName = "Login (demo example)"

// SeedDemoExample builds the ready-made Login coverage example for a demo
// profile, aligned with the seeded demo tests/requirements: the parameter model
// above, with each non-gap value mapped to the actual demo test that exercises
// it (so run-status colours and the headline % are real), and the "Login
// requirement" (PRD-1) attached as a member so the Reuse tab is populated.
// Idempotent — any prior demo example is replaced. Returns the canonical id.
func (m *Module) SeedDemoExample(profileID string) (string, error) {
	prefix, err := m.testProjectPrefix(profileID)
	if err != nil {
		return "", err
	}
	known, err := m.knownTestKeys(profileID)
	if err != nil {
		return "", err
	}

	// Replace any previous demo example so re-seeding is clean.
	var oldID string
	if err := m.db.QueryRow(
		`SELECT id FROM canonical_requirement WHERE profile_id = ? AND name = ?`,
		profileID, demoCanonName).Scan(&oldID); err == nil && oldID != "" {
		if err := m.DeleteCanonical(profileID, oldID); err != nil {
			return "", fmt.Errorf("clear prior demo example: %w", err)
		}
	}

	cid, err := m.CreateCanonical(profileID, demoCanonName, "Authentication",
		"Demo example aligned with the seeded \"Login\" feature tests (DEMO-1 = \"Login with valid input\", …).")
	if err != nil {
		return "", err
	}
	// Create the initial version so the parameter model is version-rooted.
	vid, err := m.CreateVersion(profileID, cid, "1.0", "stable", "")
	if err != nil {
		return "", fmt.Errorf("create demo version: %w", err)
	}

	tx, err := m.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	now := nowISO()
	for gi, g := range demoLogin() {
		gid := uuid.NewString()
		if _, err := tx.Exec(
			`INSERT INTO coverage_param_group (profile_id, id, canonical_id, version_id, name, sort_order) VALUES (?, ?, '', ?, ?, ?)`,
			profileID, gid, vid, g.name, gi); err != nil {
			return "", err
		}
		pid := uuid.NewString()
		if _, err := tx.Exec(
			`INSERT INTO coverage_parameter (profile_id, id, group_id, name, kind, sort_order) VALUES (?, ?, ?, ?, 'value', 0)`,
			profileID, pid, gid, g.name); err != nil {
			return "", err
		}
		for vi, val := range g.vals {
			vid := uuid.NewString()
			if _, err := tx.Exec(
				`INSERT INTO coverage_param_value
				   (profile_id, id, parameter_id, value_label, value_kind, is_required, sort_order)
				 VALUES (?, ?, ?, ?, 'value', 1, ?)`,
				profileID, vid, pid, val.label, vi); err != nil {
				return "", err
			}
			if val.gap || val.testNum == 0 {
				continue
			}
			testKey := fmt.Sprintf("%s-%d", prefix, val.testNum)
			if !known[testKey] {
				continue // robust to a smaller dataset
			}
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO coverage_value_test (profile_id, value_id, test_key, created_at) VALUES (?, ?, ?, ?)`,
				profileID, vid, testKey, now); err != nil {
				return "", err
			}
		}
	}

	for _, rk := range m.demoMemberRequirements(profileID) {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO canonical_requirement_member (profile_id, canonical_id, requirement_key, added_at) VALUES (?, ?, ?, ?)`,
			profileID, cid, rk, now); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return cid, nil
}

// testProjectPrefix derives the demo test project key (e.g. "DEMO") from any
// synced test key of the form "{PROJ}-{n}".
func (m *Module) testProjectPrefix(profileID string) (string, error) {
	var key string
	err := m.db.QueryRow(
		`SELECT jira_key FROM test_case WHERE profile_id = ? ORDER BY jira_key LIMIT 1`,
		profileID).Scan(&key)
	if err != nil {
		return "", fmt.Errorf("no synced tests found — sync the demo profile first")
	}
	if dash := strings.LastIndex(key, "-"); dash > 0 {
		return key[:dash], nil
	}
	return key, nil
}

// demoMemberRequirements returns the "Login requirement" (PRD-1) plus any other
// requirement whose summary starts with "Login", falling back to the first two
// synced requirements so the Reuse tab is never empty.
func (m *Module) demoMemberRequirements(profileID string) []string {
	out := []string{}
	rows, err := m.db.Query(
		`SELECT jira_key FROM requirement
		  WHERE profile_id = ? AND (jira_key = 'PRD-1' OR summary LIKE 'Login%')
		  ORDER BY jira_key`, profileID)
	if err == nil {
		for rows.Next() {
			var k string
			if rows.Scan(&k) == nil {
				out = append(out, k)
			}
		}
		rows.Close()
	}
	if len(out) > 0 {
		return out
	}
	fallback, _ := m.sampleKeys(profileID, "requirement", "jira_key", 2)
	return fallback
}

// sampleKeys returns up to limit values of column from table for the profile.
func (m *Module) sampleKeys(profileID, table, column string, limit int) ([]string, error) {
	// table/column are package-internal literals, never user input.
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE profile_id = ? ORDER BY %s LIMIT ?`, column, table, column)
	rows, err := m.db.Query(q, profileID, limit)
	if err != nil {
		return nil, fmt.Errorf("sample %s: %w", table, err)
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
