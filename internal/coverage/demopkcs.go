package coverage

import (
	"database/sql"
	"fmt"
	"strings"
)

// This seeds a complete, internally-consistent PKCS#11 reference dataset for a
// profile — the full end-to-end relation a user can browse:
//
//	functional requirement (FUNC-PKCS11-*)  ← reused by →  customer requirements (CUST-HSM-*)
//	          │                                                      │
//	          ▼                                                      ▼
//	canonical functional requirement ── members ── customer requirements
//	          │                                                      │
//	   versions (2.40 / 3.0) ── member locks ── change request + per-customer decisions
//	          │
//	   parameter model (groups → values) ── value→test mappings ──► test cases (PKCS-*)
//	                                                                      │
//	                                              executions (PKCSEXEC-*) ┘  (run status)
//
// Every test key referenced by a value mapping exists in test_case; every
// canonical member exists in requirement; every mapped test is linked to a
// member requirement (so it shows as a candidate). The Jira-cache rows are
// written via raw SQL (this is seed data, not sync); the coverage layer is built
// through the module's own public methods so it reuses the tested logic.
//
// Idempotent: re-running clears the prior PKCS reference rows first.

// PKCSSeedSummary reports what the reference seed produced.
type PKCSSeedSummary struct {
	Features     int `json:"features"`
	Requirements int `json:"requirements"`
	Tests        int `json:"tests"`
	Versions     int `json:"versions"`
	ChangeReqs   int `json:"changeRequests"`
	Mappings     int `json:"mappings"`
}

type pkcsValue struct {
	label   string
	kind    string // value | errorcode
	errCode string
	gap     bool // intentionally left untested
}

type pkcsGroup struct {
	name string
	vals []pkcsValue
}

type pkcsFeature struct {
	code    string // short tag used in keys: SIG / KG / VER
	fn      string // "C_Sign"
	summary string
	cr      string // change-request title
	groups  []pkcsGroup
	tests   []string // test summaries; keys become PKCS-<code>-<n>
}

func v(label string) pkcsValue  { return pkcsValue{label: label} }
func vg(label string) pkcsValue { return pkcsValue{label: label, gap: true} }
func ec(code string) pkcsValue  { return pkcsValue{label: code, kind: "errorcode", errCode: code} }
func ecg(code string) pkcsValue {
	return pkcsValue{label: code, kind: "errorcode", errCode: code, gap: true}
}

// pkcsFeatures defines the three PKCS#11 functions seeded as the reference set:
// C_Sign, C_GenerateKeyPair, C_Verify.
func pkcsFeatures() []pkcsFeature {
	return []pkcsFeature{
		{
			code: "SIG", fn: "C_Sign", summary: "PKCS#11 C_Sign — single-part signature",
			cr: "Add Ed25519 / P-521 signing support",
			groups: []pkcsGroup{
				{"Session", []pkcsValue{v("Valid session"), v("Invalid session"), v("Closed session"), vg("Read-only session")}},
				{"Mechanism", []pkcsValue{v("CKM_RSA_PKCS"), v("CKM_SHA256_RSA_PKCS"), v("CKM_ECDSA"), v("CKM_SHA256_ECDSA"), vg("CKM_ED25519")}},
				{"Data", []pkcsValue{v("1 byte"), v("256 bytes"), v("8 MB"), v("NULL pointer")}},
				{"Output", []pkcsValue{v("Query length"), v("Exact buffer"), v("Undersized buffer")}},
				{"Error paths", []pkcsValue{ec("CKR_OK"), ec("CKR_ARGUMENTS_BAD"), ec("CKR_BUFFER_TOO_SMALL"), ec("CKR_KEY_TYPE_INCONSISTENT"), ecg("CKR_OPERATION_NOT_INITIALIZED")}},
			},
			tests: []string{
				"C_Sign RSA-2048 PKCS#1 valid session",
				"C_Sign SHA256-RSA 256-byte message",
				"C_Sign ECDSA P-256 raw",
				"C_Sign SHA256-ECDSA P-256",
				"C_Sign 8 MB payload (large data)",
				"C_Sign query-length output mode",
				"C_Sign undersized buffer → CKR_BUFFER_TOO_SMALL",
				"C_Sign invalid session → CKR_SESSION_HANDLE_INVALID",
			},
		},
		{
			code: "KG", fn: "C_GenerateKeyPair", summary: "PKCS#11 C_GenerateKeyPair — asymmetric key generation",
			cr: "Add P-521 curve to the key-generation matrix",
			groups: []pkcsGroup{
				{"Session", []pkcsValue{v("Valid R/W session"), vg("Read-only session")}},
				{"Mechanism", []pkcsValue{v("CKM_RSA_PKCS_KEY_PAIR_GEN"), v("CKM_EC_KEY_PAIR_GEN"), vg("CKM_ED25519_KEY_PAIR_GEN")}},
				{"Key template", []pkcsValue{v("RSA-2048"), v("RSA-4096"), v("EC P-256"), vg("EC P-521")}},
				{"Error paths", []pkcsValue{ec("CKR_OK"), ec("CKR_TEMPLATE_INCOMPLETE"), ec("CKR_MECHANISM_INVALID"), ecg("CKR_DEVICE_MEMORY")}},
			},
			tests: []string{
				"C_GenerateKeyPair RSA-2048",
				"C_GenerateKeyPair RSA-4096",
				"C_GenerateKeyPair EC P-256",
				"C_GenerateKeyPair incomplete template → CKR_TEMPLATE_INCOMPLETE",
				"C_GenerateKeyPair unknown mechanism → CKR_MECHANISM_INVALID",
				"C_GenerateKeyPair persistent token keys",
			},
		},
		{
			code: "VER", fn: "C_Verify", summary: "PKCS#11 C_Verify — single-part signature verification",
			cr: "Tighten signature-length range checks",
			groups: []pkcsGroup{
				{"Session", []pkcsValue{v("Valid session"), v("Invalid session")}},
				{"Mechanism", []pkcsValue{v("CKM_RSA_PKCS"), v("CKM_SHA256_RSA_PKCS"), v("CKM_ECDSA"), vg("CKM_SHA256_ECDSA")}},
				{"Signature", []pkcsValue{v("Valid signature"), v("Tampered signature"), vg("Wrong-length signature")}},
				{"Error paths", []pkcsValue{ec("CKR_OK"), ec("CKR_SIGNATURE_INVALID"), ec("CKR_SIGNATURE_LEN_RANGE"), ecg("CKR_DATA_LEN_RANGE")}},
			},
			tests: []string{
				"C_Verify RSA-2048 valid signature",
				"C_Verify SHA256-RSA valid signature",
				"C_Verify ECDSA P-256 valid signature",
				"C_Verify tampered signature → CKR_SIGNATURE_INVALID",
				"C_Verify invalid session → CKR_SESSION_HANDLE_INVALID",
			},
		},
	}
}

// demo customer projects that reuse the functional requirements.
var pkcsCustomers = []struct{ proj, tag, version string }{
	{"CUST-HSM-BANK", "BANK", "2.40"},
	{"CUST-HSM-SAMSU", "SAMSU", "3.0"},
}

// SeedPKCSReference seeds the complete PKCS#11 reference dataset into the
// profile and returns a summary. Idempotent.
func (m *Module) SeedPKCSReference(profileID string) (PKCSSeedSummary, error) {
	var sum PKCSSeedSummary
	feats := pkcsFeatures()
	now := nowISO()

	// Remove any prior PKCS canonicals (and their version/CR/model cascade) so a
	// re-seed is clean — the Jira-cache rows are cleared inside the Phase 1 tx.
	if err := m.deletePriorPKCSCanonicals(profileID); err != nil {
		return sum, fmt.Errorf("clear prior PKCS canonicals: %w", err)
	}

	// ── Phase 1: Jira-cache rows (folders, requirements, tests, links, runs) ──
	tx, err := m.db.Begin()
	if err != nil {
		return sum, err
	}
	if err := clearPKCSReference(tx, profileID); err != nil {
		tx.Rollback()
		return sum, fmt.Errorf("clear prior PKCS data: %w", err)
	}

	totalTests := 0
	for fi, f := range feats {
		// Folder per feature for nice Browse grouping.
		folderID := "FOLD-PKCS-" + f.code
		if _, err := tx.Exec(
			`INSERT INTO test_folder (profile_id, id, parent_id, name) VALUES (?,?,'',?)`,
			profileID, folderID, f.fn); err != nil {
			tx.Rollback()
			return sum, err
		}

		// Functional requirement + per-customer requirements.
		funcKey := "FUNC-PKCS11-" + f.code
		if _, err := tx.Exec(
			`INSERT INTO requirement (profile_id, jira_key, project_key, issue_type, summary, status, updated_at)
			 VALUES (?,?,?,?,?,?,?)`,
			profileID, funcKey, "FUNC", "Requirement", f.summary, "Approved", now); err != nil {
			tx.Rollback()
			return sum, err
		}
		sum.Requirements++
		for _, c := range pkcsCustomers {
			custKey := c.proj + "-" + f.code
			if _, err := tx.Exec(
				`INSERT INTO requirement (profile_id, jira_key, project_key, issue_type, summary, status, updated_at)
				 VALUES (?,?,?,?,?,?,?)`,
				profileID, custKey, c.proj, "Story", f.fn+" — "+c.tag+" customer requirement", "In Progress", now); err != nil {
				tx.Rollback()
				return sum, err
			}
			sum.Requirements++
		}

		// Tests + execution with run statuses + requirement links.
		execKey := "PKCSEXEC-" + f.code
		if _, err := tx.Exec(
			`INSERT INTO test_container (profile_id, jira_key, kind, summary, status) VALUES (?,?,'testexec',?,'Open')`,
			profileID, execKey, f.fn+" regression cycle"); err != nil {
			tx.Rollback()
			return sum, err
		}
		for ti, summary := range f.tests {
			testKey := fmt.Sprintf("PKCS-%s-%d", f.code, ti+1)
			if _, err := tx.Exec(
				`INSERT INTO test_case (profile_id, jira_key, jira_id, summary, status, priority, folder_id, components, updated_at)
				 VALUES (?,?,?,?,?,?,?,?,?)`,
				profileID, testKey, fmt.Sprintf("%d%02d", fi+1, ti+1), summary, "Approved", "Medium", folderID, "PKCS11", now); err != nil {
				tx.Rollback()
				return sum, err
			}
			totalTests++
			// Link each test to both customer requirements of this feature, so it
			// is a candidate for the canonical (whose members are those reqs).
			for _, c := range pkcsCustomers {
				if _, err := tx.Exec(
					`INSERT INTO test_requirement (profile_id, test_key, requirement_key, link_id) VALUES (?,?,?,?)`,
					profileID, testKey, c.proj+"-"+f.code, fmt.Sprintf("L-%s-%d-%s", f.code, ti+1, c.tag)); err != nil {
					tx.Rollback()
					return sum, err
				}
			}
			// Run status: mostly pass, every 4th fails, every 5th not-run.
			run := "PASS"
			switch {
			case (ti+1)%5 == 0:
				run = ""
			case (ti+1)%4 == 0:
				run = "FAIL"
			}
			if run != "" {
				if _, err := tx.Exec(
					`INSERT INTO test_container_test (profile_id, container_key, test_key, run_status) VALUES (?,?,?,?)`,
					profileID, execKey, testKey, run); err != nil {
					tx.Rollback()
					return sum, err
				}
			}
		}
	}

	// Mark the profile as synced so the UI shows the dataset.
	if _, err := tx.Exec(
		`INSERT INTO sync_state (profile_id, last_synced_at, test_count) VALUES (?,?,?)
		 ON CONFLICT(profile_id) DO UPDATE SET last_synced_at=excluded.last_synced_at, test_count=excluded.test_count`,
		profileID, now, totalTests); err != nil {
		tx.Rollback()
		return sum, err
	}
	if err := tx.Commit(); err != nil {
		return sum, err
	}
	sum.Tests = totalTests

	// ── Phase 2: coverage layer, via the module's tested public methods ──
	for _, f := range feats {
		if err := m.seedPKCSCoverage(profileID, f, &sum); err != nil {
			return sum, fmt.Errorf("seed coverage for %s: %w", f.fn, err)
		}
		sum.Features++
	}
	return sum, nil
}

// seedPKCSCoverage builds one feature's canonical requirement, versions, model,
// mappings, member locks, and change request.
func (m *Module) seedPKCSCoverage(profileID string, f pkcsFeature, sum *PKCSSeedSummary) error {
	cid, err := m.CreateCanonical(profileID, f.fn, "PKCS11", f.summary)
	if err != nil {
		return err
	}
	// Members = the per-customer requirements.
	members := make([]string, 0, len(pkcsCustomers))
	for _, c := range pkcsCustomers {
		members = append(members, c.proj+"-"+f.code)
	}
	if err := m.SetMembers(profileID, cid, members); err != nil {
		return err
	}

	// Primary version 2.40 holds the model; 3.0 is a clone (beta).
	v240, err := m.CreateVersion(profileID, cid, "2.40", "stable", "Stable release locked by conservative customers.")
	if err != nil {
		return err
	}

	// Build groups → one synthetic parameter per group → values; map non-gap
	// values round-robin to this feature's tests.
	testKeys := make([]string, len(f.tests))
	for i := range f.tests {
		testKeys[i] = fmt.Sprintf("PKCS-%s-%d", f.code, i+1)
	}
	ti := 0
	for gi, g := range f.groups {
		gid, err := m.UpsertNode(profileID, NodeEdit{Kind: "group", VersionID: v240, Name: g.name, SortOrder: gi})
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

	v30, err := m.CloneVersion(profileID, v240, "3.0", "beta")
	if err != nil {
		return err
	}
	sum.Versions += 2

	// Member version locks (BANK→2.40, SAMSU→3.0) per pkcsCustomers.
	verByTag := map[string]string{"2.40": v240, "3.0": v30}
	for _, c := range pkcsCustomers {
		if vid, ok := verByTag[c.version]; ok {
			if err := m.SetMemberVersion(profileID, cid, c.proj+"-"+f.code, vid); err != nil {
				return err
			}
		}
	}

	// Change request targeting 3.0 with per-customer decisions.
	crID, err := m.CreateChangeRequest(profileID, cid, "CHG-PKCS-"+f.code, f.cr, "approved", v30, "low",
		"Introduced in 3.0; customers opt in per their risk posture.")
	if err != nil {
		return err
	}
	sum.ChangeReqs++
	// BANK (conservative) cannot accept; SAMSU (early adopter) can.
	_ = m.SetCRDecision(profileID, crID, "CUST-HSM-BANK-"+f.code, "cannot_accept", "Locked to 2.40 for compliance.")
	_ = m.SetCRDecision(profileID, crID, "CUST-HSM-SAMSU-"+f.code, "can_accept", "Wants the new mechanisms.")
	return nil
}

// clearPKCSReference removes any prior PKCS reference rows for the profile so a
// re-seed is clean. Canonicals (and their version/CR/model cascade) are removed
// via DeleteCanonical after this, in the caller — here we clear the Jira-cache
// rows by their reference key prefixes.
func clearPKCSReference(tx *sql.Tx, profileID string) error {
	stmts := []struct {
		q    string
		args []any
	}{
		{`DELETE FROM test_container_test WHERE profile_id=? AND container_key LIKE 'PKCSEXEC-%'`, []any{profileID}},
		{`DELETE FROM test_container WHERE profile_id=? AND jira_key LIKE 'PKCSEXEC-%'`, []any{profileID}},
		{`DELETE FROM test_requirement WHERE profile_id=? AND test_key LIKE 'PKCS-%'`, []any{profileID}},
		{`DELETE FROM test_case WHERE profile_id=? AND jira_key LIKE 'PKCS-%'`, []any{profileID}},
		{`DELETE FROM test_folder WHERE profile_id=? AND id LIKE 'FOLD-PKCS-%'`, []any{profileID}},
		{`DELETE FROM requirement WHERE profile_id=? AND (jira_key LIKE 'FUNC-PKCS11-%' OR jira_key LIKE 'CUST-HSM-%')`, []any{profileID}},
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s.q, s.args...); err != nil {
			return err
		}
	}
	return nil
}

// deletePriorPKCSCanonicals removes any existing canonicals whose name matches a
// seeded feature (so a re-seed doesn't duplicate). Called before Phase 2.
func (m *Module) deletePriorPKCSCanonicals(profileID string) error {
	names := make([]string, 0)
	for _, f := range pkcsFeatures() {
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

func placeholders(n int) string {
	if n <= 0 {
		return "''"
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
