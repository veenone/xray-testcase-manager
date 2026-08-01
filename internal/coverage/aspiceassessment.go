package coverage

import "fmt"

// SeedEUICCASPICEAssessment overlays the seven Automotive SPICE processes as
// coverage canonicals judged against a synced demo-euicc dataset, producing an
// in-app ASPICE gap assessment. It reuses the ASPICE Base-Practice catalog from
// aspiceFeatures() (demoaspice.go) but decides covered-vs-gap from the curated
// euiccASPICESatisfied verdict below — not the catalog's generic gap markers.
//
// Satisfied Base Practices are mapped to a real eUICC test_case (round-robin
// over the eUICC test pool) as clickable evidence; the rest are left unmapped
// and surface as coverage gaps. Members are all eUICC CUST-* requirements —
// each ASPICE process is assessed against the whole eUICC requirement set.
//
// It writes only coverage-layer rows (canonicals, one version each, model,
// mappings, members). It never touches requirement / test_case / sync_state
// rows. Idempotent: prior ASPICE canonicals are cleared first.

// ASPICEAssessmentSummary reports what the assessment produced / consumed.
type ASPICEAssessmentSummary struct {
	Processes int `json:"processes"`
	Members   int `json:"members"`
	Tests     int `json:"tests"`    // eUICC candidate tests found
	Mappings  int `json:"mappings"` // satisfied Base Practices given evidence
	Gaps      int `json:"gaps"`     // unmapped required values
}

// euiccASPICESatisfied is the curated verdict: per ASPICE process code, the
// Base-Practice value labels the demo-euicc dataset satisfies. Everything not
// listed is treated as a gap. Each label MUST match a value in that process's
// aspiceFeatures() model (guarded by TestSeedEUICCASPICEAssessment).
//
// Rationale per process (see the eUICC-vs-ASPICE analysis):
//   - SYS.2 / SYS.5: eUICC has system requirements, a coverage/verification
//     model, req<->test traceability, executions and change/version control —
//     but no verification-criteria attribute, no stakeholder tier, no
//     consistency reporting, no per-case result work product.
//   - SWE.1 / SWE.6: no distinct software-requirement or SW-qualification tier.
//   - SWE.4: no unit-verification level at all (0%).
//   - SUP.9: defect linking gives partial problem management.
//   - SUP.10: change requests + per-customer decisions + versions make this the
//     strongest area.
var euiccASPICESatisfied = map[string][]string{
	"SRA": { // SYS.2 System Requirements Analysis
		"System requirements specified",
		"Requirements structured & prioritized",
		"Feasibility & verifiability analyzed",
		"Operating-environment impact analyzed",
		"Verification criteria defined per requirement",
		"Every requirement has verification criteria",
		"Bidirectional: system req ↔ system test",
		"System requirements baselined",
	},
	"SQT": { // SYS.5 System Qualification Test
		"Test cases specified from system requirements",
		"Test cases selected per strategy",
		"System requirements covered by tests",
		"Integrated system tested",
		"Results recorded (pass/fail)",
		"Failures raised as problems (SUP.9)",
		"Bidirectional: system req ↔ test case",
		"Results summarized",
	},
	"SWR": { // SWE.1 Software Requirements Analysis
		"Software requirements specified",
		"Bidirectional: system ↔ SW req",
	},
	"SUV": {}, // SWE.4 Software Unit Verification — no unit tests in the dataset
	"SWQ": { // SWE.6 Software Qualification Test
		"Integrated software tested",
		"Results recorded (pass/fail)",
	},
	"PRM": { // SUP.9 Problem Resolution Management
		"Problems identified & recorded",
		"Status recorded",
		"Each problem has a unique record",
		"Root cause determined",
		"Bidirectional: problem ↔ affected work-products",
		"Bidirectional: problem ↔ change request",
	},
	"CRM": { // SUP.10 Change Request Management
		"CR management strategy defined",
		"Change requests identified & recorded",
		"Status recorded",
		"Each CR has a unique record",
		"Impact & dependencies analyzed",
		"Approval obtained before implementation",
		"Implementation reviewed",
		"Affected parties agreed",
		"Bidirectional: CR ↔ affected work-products",
	},
}

// SeedEUICCASPICEAssessment builds the ASPICE assessment overlay for a synced
// demo-euicc profile. Idempotent.
func (m *Module) SeedEUICCASPICEAssessment(profileID string) (ASPICEAssessmentSummary, error) {
	var sum ASPICEAssessmentSummary

	// Clear any prior ASPICE canonicals (reuses the demoaspice.go helper, which
	// matches on aspiceFeatures() process names).
	if err := m.deletePriorASPICECanonicals(profileID); err != nil {
		return sum, fmt.Errorf("clear prior ASPICE canonicals: %w", err)
	}

	// Members = all eUICC CUST-* requirements (whole requirement set).
	memberKeys, err := m.queryKeys(
		`SELECT jira_key FROM requirement
		  WHERE profile_id=? AND project_key IN ('CUST-MNO-CONSUMER','CUST-IOT-FLEET','CUST-M2M-AUTO')
		  ORDER BY jira_key`, profileID)
	if err != nil {
		return sum, fmt.Errorf("query eUICC members: %w", err)
	}

	// Evidence pool = all eUICC test cases.
	testKeys, err := m.queryKeys(
		`SELECT jira_key FROM test_case WHERE profile_id=? ORDER BY jira_key`, profileID)
	if err != nil {
		return sum, fmt.Errorf("query eUICC tests: %w", err)
	}
	sum.Tests = len(testKeys)

	ti := 0
	for _, f := range aspiceFeatures() {
		satisfied := make(map[string]bool, len(euiccASPICESatisfied[f.code]))
		for _, label := range euiccASPICESatisfied[f.code] {
			satisfied[label] = true
		}
		if err := m.assessOneProcess(profileID, f, satisfied, memberKeys, testKeys, &ti, &sum); err != nil {
			return sum, fmt.Errorf("assess %s: %w", f.fn, err)
		}
		sum.Processes++
	}
	return sum, nil
}

// assessOneProcess seeds one ASPICE process's canonical, version, model, members
// and evidence mappings for the eUICC assessment.
func (m *Module) assessOneProcess(profileID string, f pkcsFeature, satisfied map[string]bool,
	memberKeys, testKeys []string, ti *int, sum *ASPICEAssessmentSummary) error {

	cid, err := m.CreateCanonical(profileID, f.fn, "ASPICE", f.summary)
	if err != nil {
		return err
	}
	if err := m.SetMembers(profileID, cid, memberKeys); err != nil {
		return err
	}
	sum.Members += len(memberKeys)

	ver, err := m.CreateVersion(profileID, cid, "ASPICE 3.1 assessment", "assessment",
		"eUICC dataset assessed against Automotive SPICE 3.1 (VDA scope).")
	if err != nil {
		return err
	}

	for gi, g := range f.groups {
		gid, err := m.UpsertNode(profileID, NodeEdit{Kind: "group", VersionID: ver, Name: g.name, SortOrder: gi})
		if err != nil {
			return err
		}
		pid, err := m.UpsertNode(profileID, NodeEdit{Kind: "parameter", GroupID: gid, Name: g.name})
		if err != nil {
			return err
		}
		for vi, val := range g.vals {
			kind := val.kind
			if kind == "" {
				kind = "value"
			}
			vid, err := m.UpsertNode(profileID, NodeEdit{
				Kind: "value", ParameterID: pid, Name: val.label,
				ValueKind: kind, ErrorCode: val.errCode, IsRequired: true, SortOrder: vi,
			})
			if err != nil {
				return err
			}
			// Curated verdict: satisfied Base Practices get eUICC test evidence;
			// the rest are gaps. (The catalog's own val.gap marker is ignored.)
			// If the eUICC test pool is empty (profile not synced), even a
			// satisfied practice falls through to the gap branch — the whole
			// assessment then reads 0%, the intended "sync first" degradation.
			if satisfied[val.label] && len(testKeys) > 0 {
				tk := testKeys[*ti%len(testKeys)]
				*ti++
				if err := m.SetValueTests(profileID, vid, []string{tk}); err != nil {
					return err
				}
				sum.Mappings++
			} else {
				sum.Gaps++
			}
		}
	}
	return nil
}

// queryKeys runs a single-column string query and collects the results.
func (m *Module) queryKeys(query, profileID string) ([]string, error) {
	rows, err := m.db.Query(query, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
