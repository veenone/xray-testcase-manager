package coverage

import (
	"fmt"
)

// SeedASPICEReference builds the coverage layer for seven Automotive SPICE
// (ASPICE) processes — the system tier (SYS.2, SYS.5), the software tier
// (SWE.1, SWE.4, SWE.6), and supporting processes (SUP.9, SUP.10) — mapping
// onto synced data that was already written to the store by a demo-aspice
// backend sync:
//
//   - customer/program requirements in project_key CUST-OEM-PLATFORM /
//     CUST-TIER1-ECU / CUST-SAFETY-DOMAIN whose summary starts with the process
//     name
//   - test_case rows whose summary starts with the process name
//   - test_requirement rows linking those tests to the program reqs
//
// It writes only the coverage layer (canonicals, versions, model, mappings,
// member locks, change requests). It never touches requirement / test_case /
// test_container / sync_state rows. Idempotent.
//
// Each process's parameter model follows the ASPICE Process Assessment Model:
// one coverage group per cluster of Base Practices (BPs), with values drawn
// from the BPs and their output work-products. The v/vg/ec/ecg helpers (from
// demopkcs.go) are reused with an ASPICE reading:
//
//   - v()   = a Base Practice / work-product demonstrated by tests
//   - vg()  = a Base Practice that is a coverage gap (an ASPICE weakness)
//   - ec()  = a rating / consistency RULE that must hold (e.g. bidirectional
//             traceability, consistency). These render distinctly from plain
//             practices — the value_kind='errorcode' slot is deliberately
//             repurposed to carry ASPICE rating rules, not error codes.
//   - ecg() = such a rule that is currently an open non-conformance.

// ASPICESeedSummary reports what the ASPICE coverage seed produced / consumed.
type ASPICESeedSummary struct {
	Features     int `json:"features"`
	Requirements int `json:"requirements"` // member reqs found in synced data
	Tests        int `json:"tests"`        // candidate tests found in synced data
	Versions     int `json:"versions"`
	ChangeReqs   int `json:"changeRequests"`
	Mappings     int `json:"mappings"`
}

// aspiceFeatures defines the seven ASPICE processes seeded as the reference
// set. It reuses the pkcsFeature / pkcsGroup / pkcsValue types and the
// v / vg / ec / ecg helpers from demopkcs.go.
func aspiceFeatures() []pkcsFeature {
	return []pkcsFeature{
		{
			code:    "SRA",
			fn:      "SYS.2 System Requirements Analysis",
			summary: "Automotive SPICE SYS.2 — analyse stakeholder needs into system requirements with verification criteria and traceability",
			cr:      "Add cybersecurity requirements traceability (ISO/SAE 21434)",
			groups: []pkcsGroup{
				{"Requirements specification (BP1–BP2)", []pkcsValue{
					v("System requirements specified"), v("Requirements structured & prioritized"), vg("All requirement attributes complete"),
				}},
				{"Analysis (BP3–BP4)", []pkcsValue{
					v("Feasibility & verifiability analyzed"), v("Operating-environment impact analyzed"), vg("Safety/risk impact linked (ISO 26262)"),
				}},
				{"Verification criteria (BP5)", []pkcsValue{
					v("Verification criteria defined per requirement"), ec("Every requirement has verification criteria"), ecg("Criteria testable & measurable"),
				}},
				{"Traceability & consistency (BP6–BP7)", []pkcsValue{
					ec("Bidirectional: stakeholder ↔ system req"), ec("Bidirectional: system req ↔ system test"), ecg("Consistency ensured"),
				}},
				{"Agreement (BP8)", []pkcsValue{
					v("System requirements baselined"), vg("Communicated to affected parties"),
				}},
			},
		},
		{
			code:    "SQT",
			fn:      "SYS.5 System Qualification Test",
			summary: "Automotive SPICE SYS.5 — qualify the integrated system against system requirements",
			cr:      "Adopt ASPICE 4.0 machine-readable test results",
			groups: []pkcsGroup{
				{"Test strategy (BP1)", []pkcsValue{
					v("Qualification-test strategy defined"), v("Regression strategy defined"), vg("Entry/exit criteria defined"),
				}},
				{"Specification & selection (BP2–BP3)", []pkcsValue{
					v("Test cases specified from system requirements"), v("Test cases selected per strategy"), ec("System requirements covered by tests"),
				}},
				{"Execution (BP4)", []pkcsValue{
					v("Integrated system tested"), ec("Results recorded (pass/fail)"), ecg("Failures raised as problems (SUP.9)"),
				}},
				{"Traceability & consistency (BP5–BP6)", []pkcsValue{
					ec("Bidirectional: system req ↔ test case"), ec("Bidirectional: test case ↔ test result"), ecg("Consistency across trace"),
				}},
				{"Reporting (BP7)", []pkcsValue{
					v("Results summarized"), vg("Communicated to stakeholders"),
				}},
			},
		},
		{
			code:    "SWR",
			fn:      "SWE.1 Software Requirements Analysis",
			summary: "Automotive SPICE SWE.1 — derive software requirements from system requirements with verification criteria and traceability",
			cr:      "Split functional vs non-functional software requirements",
			groups: []pkcsGroup{
				{"SW requirements specification (BP1–BP3)", []pkcsValue{
					v("Software requirements specified"), v("Functional / non-functional structured"), vg("Attributes & priorities complete"),
				}},
				{"Analysis (BP4)", []pkcsValue{
					v("Feasibility & impact analyzed"), vg("Resource & timing constraints captured"),
				}},
				{"Verification criteria (BP5)", []pkcsValue{
					v("Verification criteria per SW requirement"), ec("Every SW requirement has verification criteria"), ecg("Criteria testable"),
				}},
				{"Traceability & consistency (BP6–BP7)", []pkcsValue{
					ec("Bidirectional: system ↔ SW req"), ec("Bidirectional: SW req ↔ SW test"), ecg("Consistency system ↔ SW"),
				}},
				{"Agreement (BP8)", []pkcsValue{
					v("SW requirements baselined"), vg("Communicated to affected parties"),
				}},
			},
		},
		{
			code:    "SUV",
			fn:      "SWE.4 Software Unit Verification",
			summary: "Automotive SPICE SWE.4 — verify software units against detailed design via static and dynamic means",
			cr:      "Require MC/DC coverage for ASIL-D units",
			groups: []pkcsGroup{
				{"Verification strategy (BP1–BP2)", []pkcsValue{
					v("Unit verification strategy incl. regression"), v("Unit verification criteria defined"), vg("Coverage targets set per ASIL"),
				}},
				{"Static verification (BP3)", []pkcsValue{
					v("Code reviews performed"), v("Static analysis run"), ecg("Static-analysis findings resolved"),
				}},
				{"Unit test (BP4)", []pkcsValue{
					v("Software units tested"), ec("Statement/branch coverage achieved"), ecg("MC/DC coverage for ASIL-D"),
				}},
				{"Traceability & consistency (BP5–BP6)", []pkcsValue{
					ec("Bidirectional: detailed design ↔ unit test"), ecg("Consistency ensured"),
				}},
				{"Reporting (BP7)", []pkcsValue{
					v("Results summarized & communicated"),
				}},
			},
		},
		{
			code:    "SWQ",
			fn:      "SWE.6 Software Qualification Test",
			summary: "Automotive SPICE SWE.6 — qualify the integrated software against software requirements",
			cr:      "Adopt ASPICE 4.0 SWE.6 'Software Verification' restructure",
			groups: []pkcsGroup{
				{"Test strategy (BP1)", []pkcsValue{
					v("SW qualification-test strategy defined"), v("Regression strategy defined"), vg("Entry/exit criteria defined"),
				}},
				{"Specification & selection (BP2–BP3)", []pkcsValue{
					v("Test cases from software requirements"), v("Test cases selected per strategy"), ec("Software requirements covered by tests"),
				}},
				{"Execution (BP4)", []pkcsValue{
					v("Integrated software tested"), ec("Results recorded (pass/fail)"), ecg("Failures raised as problems (SUP.9)"),
				}},
				{"Traceability & consistency (BP5–BP6)", []pkcsValue{
					ec("Bidirectional: SW req ↔ test case"), ec("Bidirectional: test case ↔ test result"), ecg("Consistency across trace"),
				}},
				{"Reporting (BP7)", []pkcsValue{
					v("Results summarized"), vg("Communicated to stakeholders"),
				}},
			},
		},
		{
			code:    "PRM",
			fn:      "SUP.9 Problem Resolution Management",
			summary: "Automotive SPICE SUP.9 — identify, analyse and resolve problems; the tool's own defect linking is the evidence",
			cr:      "Add root-cause taxonomy (8D) to problem records",
			groups: []pkcsGroup{
				{"Strategy (BP1)", []pkcsValue{
					v("Problem-resolution strategy defined"), vg("Problem classification scheme"),
				}},
				{"Identification & recording (BP2–BP3)", []pkcsValue{
					v("Problems identified & recorded"), v("Status recorded"), ec("Each problem has a unique record"),
				}},
				{"Root cause & action (BP4–BP5)", []pkcsValue{
					v("Root cause determined"), vg("Structured RCA (8D) applied"), ecg("Corrective actions verified"),
				}},
				{"Traceability & closure (BP6–BP8)", []pkcsValue{
					ec("Bidirectional: problem ↔ affected work-products"), ec("Bidirectional: problem ↔ change request"), ecg("Tracked to closure"),
				}},
				{"Alert & communication (BP7)", []pkcsValue{
					v("Affected parties alerted"), vg("Trend analysis performed"),
				}},
			},
		},
		{
			code:    "CRM",
			fn:      "SUP.10 Change Request Management",
			summary: "Automotive SPICE SUP.10 — record, assess, approve and track change requests; the tool's own change-request feature is the evidence",
			cr:      "Enforce approve-before-implement gate on change requests",
			groups: []pkcsGroup{
				{"Strategy (BP1)", []pkcsValue{
					v("CR management strategy defined"), vg("CR classification scheme"),
				}},
				{"Identification & recording (BP2–BP3)", []pkcsValue{
					v("Change requests identified & recorded"), v("Status recorded"), ec("Each CR has a unique record"),
				}},
				{"Impact analysis & approval (BP4–BP5)", []pkcsValue{
					v("Impact & dependencies analyzed"), ec("Approval obtained before implementation"), ecg("Affected parties agreed"),
				}},
				{"Implementation review & closure (BP6–BP7)", []pkcsValue{
					v("Implementation reviewed"), ecg("Tracked to closure"),
				}},
				{"Traceability (BP8)", []pkcsValue{
					ec("Bidirectional: CR ↔ affected work-products"), ecg("Consistency across CR trace"),
				}},
			},
		},
	}
}

// aspiceCustomers lists the three demo program projects with their PAM-version
// lock and CR decision posture, representing three automotive assessment scopes.
var aspiceCustomers = []struct {
	proj     string
	version  string
	decision string
	note     string
}{
	{
		"CUST-OEM-PLATFORM", "4.0", "can_accept",
		"OEM platform program targeting Capability Level 3; adopting Automotive SPICE 4.0.",
	},
	{
		"CUST-TIER1-ECU", "3.1", "cannot_accept",
		"Tier-1 ECU supplier at CL2; scope frozen for an upcoming assessment (ASPICE 3.1).",
	},
	{
		"CUST-SAFETY-DOMAIN", "4.0", "can_accept",
		"Safety-relevant domain (ISO 26262 ASIL-D); CL3 + cybersecurity, ASPICE 4.0.",
	},
}

// SeedASPICEReference builds the coverage layer for the seven ASPICE processes,
// mapping onto synced demo-aspice data. Idempotent.
func (m *Module) SeedASPICEReference(profileID string) (ASPICESeedSummary, error) {
	var sum ASPICESeedSummary
	feats := aspiceFeatures()

	// Remove any prior ASPICE canonicals (and their version/CR/model cascade).
	if err := m.deletePriorASPICECanonicals(profileID); err != nil {
		return sum, fmt.Errorf("clear prior ASPICE canonicals: %w", err)
	}

	for _, f := range feats {
		if err := m.seedASPICECoverage(profileID, f, &sum); err != nil {
			return sum, fmt.Errorf("seed coverage for %s: %w", f.fn, err)
		}
		sum.Features++
	}
	return sum, nil
}

// seedASPICECoverage builds one ASPICE process's canonical requirement,
// versions, model, mappings, member locks, and change request, mapping onto
// synced data.
func (m *Module) seedASPICECoverage(profileID string, f pkcsFeature, sum *ASPICESeedSummary) error {
	cid, err := m.CreateCanonical(profileID, f.fn, "ASPICE", f.summary)
	if err != nil {
		return err
	}

	// Step 1: Members = synced program reqs whose summary starts with f.fn.
	rows, err := m.db.Query(
		`SELECT jira_key, project_key FROM requirement
		  WHERE profile_id=? AND project_key IN ('CUST-OEM-PLATFORM','CUST-TIER1-ECU','CUST-SAFETY-DOMAIN')
		    AND summary LIKE ? ORDER BY jira_key`,
		profileID, f.fn+"%")
	if err != nil {
		return fmt.Errorf("query members for %s: %w", f.fn, err)
	}
	var memberKeys []string
	var memberProjs []string
	for rows.Next() {
		var key, proj string
		if err := rows.Scan(&key, &proj); err != nil {
			rows.Close()
			return err
		}
		memberKeys = append(memberKeys, key)
		memberProjs = append(memberProjs, proj)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if err := m.SetMembers(profileID, cid, memberKeys); err != nil {
		return err
	}
	sum.Requirements += len(memberKeys)

	// Step 2: Candidate tests = synced test_case rows whose summary starts with f.fn.
	rows, err = m.db.Query(
		`SELECT jira_key FROM test_case WHERE profile_id=? AND summary LIKE ? ORDER BY jira_key`,
		profileID, f.fn+"%")
	if err != nil {
		return fmt.Errorf("query tests for %s: %w", f.fn, err)
	}
	var testKeys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return err
		}
		testKeys = append(testKeys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	sum.Tests += len(testKeys)

	// Step 3: Build version 3.1 with the parameter model; map non-gap values
	// round-robin to the candidate tests. If the pool is empty, the model is
	// created but all mappings are skipped (coverage 0%).
	v31, err := m.CreateVersion(profileID, cid, "3.1", "stable", "Automotive SPICE 3.1 process reference model (VDA scope).")
	if err != nil {
		return err
	}
	ti := 0
	for gi, g := range f.groups {
		gid, err := m.UpsertNode(profileID, NodeEdit{Kind: "group", VersionID: v31, Name: g.name, SortOrder: gi})
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
			if val.gap || len(testKeys) == 0 {
				continue
			}
			tk := testKeys[ti%len(testKeys)]
			ti++
			if err := m.SetValueTests(profileID, vid, []string{tk}); err != nil {
				return err
			}
			sum.Mappings++
		}
	}

	// Step 4: Clone to 4.0 (beta).
	v40, err := m.CloneVersion(profileID, v31, "4.0", "beta")
	if err != nil {
		return err
	}
	sum.Versions += 2

	// Build per-project lookups for version lock and CR decision.
	custVerByProj := make(map[string]string, len(aspiceCustomers))
	custDecByProj := make(map[string]struct{ dec, note string }, len(aspiceCustomers))
	for _, c := range aspiceCustomers {
		var verID string
		switch c.version {
		case "3.1":
			verID = v31
		case "4.0":
			verID = v40
		}
		custVerByProj[c.proj] = verID
		custDecByProj[c.proj] = struct{ dec, note string }{c.decision, c.note}
	}

	// Member version locks.
	for i, key := range memberKeys {
		proj := memberProjs[i]
		if vid, ok := custVerByProj[proj]; ok {
			if err := m.SetMemberVersion(profileID, cid, key, vid); err != nil {
				return err
			}
		}
	}

	// Change request targeting 4.0.
	crID, err := m.CreateChangeRequest(profileID, cid, "CHG-ASPICE-"+f.code, f.cr, "approved", v40, "low",
		"Introduced in 4.0; programs opt in per their assessment posture.")
	if err != nil {
		return err
	}
	sum.ChangeReqs++

	// Per-customer CR decisions, derived from the members found in the DB.
	for i, key := range memberKeys {
		proj := memberProjs[i]
		if d, ok := custDecByProj[proj]; ok {
			_ = m.SetCRDecision(profileID, crID, key, d.dec, d.note)
		}
	}
	return nil
}

// deletePriorASPICECanonicals removes any existing canonicals whose name
// matches a seeded ASPICE process (so a re-seed does not duplicate).
func (m *Module) deletePriorASPICECanonicals(profileID string) error {
	names := make([]string, 0)
	for _, f := range aspiceFeatures() {
		names = append(names, f.fn)
	}
	rows, err := m.db.Query(
		`SELECT id FROM canonical_requirement WHERE profile_id=? AND name IN (`+placeholders(len(names))+`)`,
		append([]any{profileID}, toAny(names)...)...)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if err := m.DeleteCanonical(profileID, id); err != nil {
			return err
		}
	}
	return nil
}
