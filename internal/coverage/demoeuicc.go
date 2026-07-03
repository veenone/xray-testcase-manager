package coverage

import (
	"fmt"
)

// SeedEUICCReference builds the coverage layer for the seven GSMA RSP procedures
// (Profile Download, Enable Profile, Disable Profile, Delete Profile, eUICC Memory
// Reset, Profile Fall-Back, Profile Enable with Rollback), mapping onto synced data
// that was already written to the store by a demo-euicc backend sync:
//
//   - customer requirements in project_key CUST-MNO-CONSUMER / CUST-IOT-FLEET /
//     CUST-M2M-AUTO whose summary starts with the procedure name
//   - test_case rows whose summary starts with the procedure name
//   - test_requirement rows linking those tests to the customer reqs
//
// It writes only the coverage layer (canonicals, versions, model, mappings,
// member locks, change requests). It never touches requirement / test_case /
// test_container / sync_state rows. Idempotent.

// EUICCSeedSummary reports what the EUICC coverage seed produced / consumed.
type EUICCSeedSummary struct {
	Features     int `json:"features"`
	Requirements int `json:"requirements"` // member reqs found in synced data
	Tests        int `json:"tests"`        // candidate tests found in synced data
	Versions     int `json:"versions"`
	ChangeReqs   int `json:"changeRequests"`
	Mappings     int `json:"mappings"`
}

// euiccFeatures defines the seven GSMA RSP procedures seeded as the EUICC
// reference set. It reuses the pkcsFeature / pkcsGroup / pkcsValue types and
// the v / vg / ec / ecg helpers from demopkcs.go.
func euiccFeatures() []pkcsFeature {
	return []pkcsFeature{
		{
			code:    "DLD",
			fn:      "Profile Download",
			summary: "GSMA RSP Profile Download — activation and delivery over ES9+/ES8+",
			cr:      "Add eIM-triggered bulk download (SGP.32)",
			groups: []pkcsGroup{
				{"Activation / trigger", []pkcsValue{
					v("Activation Code (LPA)"), v("SM-DS event"), v("eIM-triggered (SGP.32)"), vg("QR scan on no-UI device"),
				}},
				{"Bound profile package (BPP)", []pkcsValue{
					v("valid BPP over ES9+/ES8+"), v("segmented BPP"), ec("BPP integrity check failed"),
				}},
				{"eUICC memory", []pkcsValue{
					v("sufficient space"), ec("insufficient memory"),
				}},
				{"Profile metadata", []pkcsValue{
					v("operator profile"), v("provisioning/bootstrap profile"), vg("test profile"),
				}},
				{"Result / errors", []pkcsValue{
					ec("OK (installed)"), ec("EID mismatch"), ec("download retry exhausted"), ecg("undefined error"),
				}},
			},
		},
		{
			code:    "ENA",
			fn:      "Enable Profile",
			summary: "GSMA RSP Enable Profile — ISD-P state transition to Enabled",
			cr:      "Add atomic enable-with-refresh (SGP.22 v3)",
			groups: []pkcsGroup{
				{"Target profile state (ISD-P)", []pkcsValue{
					v("Disabled (valid)"), v("another profile Enabled"), vg("no profile installed"), ec("ICCID not found"),
				}},
				{"Profile identifier", []pkcsValue{
					v("ICCID"), v("ISD-P AID"), ec("identifier invalid"),
				}},
				{"Refresh handling (SGP.22 LPA)", []pkcsValue{
					v("refreshFlag=true (REFRESH)"), v("refreshFlag=false"), vg("error during REFRESH"),
				}},
				{"Policy rules POL1 (SGP.02)", []pkcsValue{
					v("no rules"), v(`"disable of Enabled not allowed"`), ec("policy violation"),
				}},
				{"Result / errors", []pkcsValue{
					ec("OK (enabled)"), ec("not in Disabled state"), ec("catBusy / refresh failed"), ecg("undefined error"),
				}},
			},
		},
		{
			code:    "DIS",
			fn:      "Disable Profile",
			summary: "GSMA RSP Disable Profile — ISD-P state transition to Disabled",
			cr:      "Auto-enable fall-back on disable (SGP.32)",
			groups: []pkcsGroup{
				{"Target profile state", []pkcsValue{
					v("Enabled (valid)"), vg("already Disabled"), ec("ICCID not found"),
				}},
				{"Refresh handling", []pkcsValue{
					v("refreshFlag=true"), v("refreshFlag=false"),
				}},
				{"Policy rules POL1", []pkcsValue{
					v("no rules"), v(`"disable not allowed (POL1)" → blocked`), ec("policy violation"),
				}},
				{"Fall-back interaction", []pkcsValue{
					v("no fall-back configured"), v("fall-back profile becomes Enabled"), vg("fall-back missing"),
				}},
				{"Result / errors", []pkcsValue{
					ec("OK (disabled)"), ec("not in Enabled state"), ec("disable not allowed (POL1)"), ecg("undefined error"),
				}},
			},
		},
		{
			code:    "DEL",
			fn:      "Delete Profile",
			summary: "GSMA RSP Delete Profile — permanent removal of ISD-P",
			cr:      "Require disable-before-delete confirmation",
			groups: []pkcsGroup{
				{"Target profile state", []pkcsValue{
					v("Disabled (valid)"), ec(`Enabled → "must disable first"`), ec("ICCID not found"),
				}},
				{"Policy rules POL1", []pkcsValue{
					v("deletion allowed"), v(`"deletion not allowed (POL1)"`), ec("policy violation"),
				}},
				{"Profile type", []pkcsValue{
					v("operator profile"), vg("provisioning profile (protected)"),
				}},
				{"Result / errors", []pkcsValue{
					ec("OK (deleted)"), ec("not in Disabled state"), ec("deletion not allowed (POL1)"), ecg("undefined error"),
				}},
			},
		},
		{
			code:    "RST",
			fn:      "eUICC Memory Reset",
			summary: "GSMA RSP eUICC Memory Reset — scoped deletion of profiles from eUICC",
			cr:      "Add scoped reset keeping provisioning profile",
			groups: []pkcsGroup{
				{"Reset scope", []pkcsValue{
					v("delete operational profiles"), v("reset to factory (keep provisioning)"), vg("delete provisioning profiles"), ec("invalid scope"),
				}},
				{"Profiles present", []pkcsValue{
					v("operational profiles only"), v("includes Enabled profile"), vg("fall-back profile present"),
				}},
				{"Authorization", []pkcsValue{
					v("authorized (LPA confirm)"), ec("unauthorized"),
				}},
				{"Result / errors", []pkcsValue{
					ec("OK (reset)"), ec("nothing to reset"), ec("unauthorized"), ecg("undefined error"),
				}},
			},
		},
		{
			code:    "FBK",
			fn:      "Profile Fall-Back",
			summary: "GSMA RSP Profile Fall-Back — automatic switch to fall-back profile on bearer failure",
			cr:      "Define fall-back for satellite IoT bearer",
			groups: []pkcsGroup{
				{"Fall-Back attribute", []pkcsValue{
					v("set on one profile"), v("not set"), vg("set on multiple"),
				}},
				{"Trigger condition", []pkcsValue{
					v("connectivity loss (no network attach)"), v("SM-DS unreachable"), ec("no fall-back profile present"),
				}},
				{"IoT bearer", []pkcsValue{
					v("NB-IoT"), v("LTE-M"), vg("satellite"),
				}},
				{"Result / errors", []pkcsValue{
					ec("OK (fall-back enabled)"), ec("no fall-back profile"), ec("fall-back already active"), ecg("undefined error"),
				}},
			},
		},
		{
			code:    "RBK",
			fn:      "Profile Enable with Rollback",
			summary: "GSMA RSP Profile Enable with Rollback — enable with automatic revert on post-enable failure",
			cr:      "Bound rollback window for IoT (SGP.32)",
			groups: []pkcsGroup{
				{"Enable target", []pkcsValue{
					v("valid Disabled profile"), v("profile failing post-enable network check"),
				}},
				{"Rollback trigger", []pkcsValue{
					v("REFRESH failed"), v("no network after enable"), ec("eUICC busy"),
				}},
				{"Previous profile state", []pkcsValue{
					v("previously-Enabled retained"), vg("no previous profile"),
				}},
				{"Result / errors", []pkcsValue{
					ec("OK (rolled back)"), ec("rollback failed"), ec("no previous profile"), ecg("undefined error"),
				}},
			},
		},
	}
}

// euiccCustomers lists the three demo customer projects with their version lock
// and CR decision posture, representing three GSMA specs.
var euiccCustomers = []struct {
	proj     string
	version  string
	decision string
	note     string
}{
	{
		"CUST-MNO-CONSUMER", "3.0", "can_accept",
		"Smartphones and wearables, SGP.22 MNO subscription profiles via LPA.",
	},
	{
		"CUST-IOT-FLEET", "3.0", "can_accept",
		"NB-IoT/LTE-M device fleet via eIM, SGP.32.",
	},
	{
		"CUST-M2M-AUTO", "2.4", "cannot_accept",
		"Automotive/metering M2M via SM-SR, SGP.02; legacy M2M, locked for type approval.",
	},
}

// SeedEUICCReference builds the coverage layer for the seven GSMA RSP procedures,
// mapping onto synced demo-euicc data. Idempotent.
func (m *Module) SeedEUICCReference(profileID string) (EUICCSeedSummary, error) {
	var sum EUICCSeedSummary
	feats := euiccFeatures()

	// Remove any prior EUICC canonicals (and their version/CR/model cascade).
	if err := m.deletePriorEUICCCanonicals(profileID); err != nil {
		return sum, fmt.Errorf("clear prior EUICC canonicals: %w", err)
	}

	for _, f := range feats {
		if err := m.seedEUICCCoverage(profileID, f, &sum); err != nil {
			return sum, fmt.Errorf("seed coverage for %s: %w", f.fn, err)
		}
		sum.Features++
	}
	return sum, nil
}

// seedEUICCCoverage builds one RSP procedure's canonical requirement, versions,
// model, mappings, member locks, and change request, mapping onto synced data.
func (m *Module) seedEUICCCoverage(profileID string, f pkcsFeature, sum *EUICCSeedSummary) error {
	cid, err := m.CreateCanonical(profileID, f.fn, "EUICC", f.summary)
	if err != nil {
		return err
	}

	// Step 1: Members = synced customer reqs whose summary starts with f.fn.
	rows, err := m.db.Query(
		`SELECT jira_key, project_key FROM requirement
		  WHERE profile_id=? AND project_key IN ('CUST-MNO-CONSUMER','CUST-IOT-FLEET','CUST-M2M-AUTO')
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

	// Step 3: Build version 2.4 with the parameter model; map non-gap values
	// round-robin to the candidate tests. If the pool is empty, the model is
	// created but all mappings are skipped (coverage 0%).
	v24, err := m.CreateVersion(profileID, cid, "2.4", "stable", "Stable release locked by conservative customers.")
	if err != nil {
		return err
	}
	ti := 0
	for gi, g := range f.groups {
		gid, err := m.UpsertNode(profileID, NodeEdit{Kind: "group", VersionID: v24, Name: g.name, SortOrder: gi})
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

	// Step 4: Clone to 3.0 (beta).
	v30, err := m.CloneVersion(profileID, v24, "3.0", "beta")
	if err != nil {
		return err
	}
	sum.Versions += 2

	// Build per-project lookups for version lock and CR decision.
	custVerByProj := make(map[string]string, len(euiccCustomers))
	custDecByProj := make(map[string]struct{ dec, note string }, len(euiccCustomers))
	for _, c := range euiccCustomers {
		var verID string
		switch c.version {
		case "2.4":
			verID = v24
		case "3.0":
			verID = v30
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

	// Change request targeting 3.0.
	crID, err := m.CreateChangeRequest(profileID, cid, "CHG-EUICC-"+f.code, f.cr, "approved", v30, "low",
		"Introduced in 3.0; customers opt in per their risk posture.")
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

// deletePriorEUICCCanonicals removes any existing canonicals whose name matches
// a seeded EUICC procedure (so a re-seed does not duplicate).
func (m *Module) deletePriorEUICCCanonicals(profileID string) error {
	names := make([]string, 0)
	for _, f := range euiccFeatures() {
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
