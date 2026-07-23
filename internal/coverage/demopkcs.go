package coverage

import (
	"fmt"
	"strings"
)

// SeedPKCSReference builds the coverage layer for the six PKCS#11 reference
// features — the signing family (C_Sign, C_GenerateKeyPair, C_Verify) and the
// key-management family (C_WrapKey, C_UnwrapKey, C_DeriveKey) — mapping onto
// synced data that was already written to the store by a demo-pkcs backend sync:
//
//   - customer requirements in project_key CUST-HSM-BANK / CUST-HSM-SAMSU
//     whose summary starts with the feature function name (e.g. "C_Sign …")
//   - test_case rows whose summary starts with the function name
//   - test_requirement rows linking those tests to the customer reqs
//
// It writes only the coverage layer (canonicals, versions, model, mappings,
// member locks, change requests). It never touches requirement / test_case /
// test_container / sync_state rows. Idempotent.

// PKCSSeedSummary reports what the coverage seed produced / consumed.
type PKCSSeedSummary struct {
	Features     int `json:"features"`
	Requirements int `json:"requirements"` // member reqs found in synced data
	Tests        int `json:"tests"`        // candidate tests found in synced data
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
	summary string // used as canonical description
	cr      string // change-request title
	groups  []pkcsGroup
}

func v(label string) pkcsValue  { return pkcsValue{label: label} }
func vg(label string) pkcsValue { return pkcsValue{label: label, gap: true} }
func ec(code string) pkcsValue  { return pkcsValue{label: code, kind: "errorcode", errCode: code} }
func ecg(code string) pkcsValue {
	return pkcsValue{label: code, kind: "errorcode", errCode: code, gap: true}
}

// pkcsFeatures defines the six PKCS#11 functions seeded as the reference set:
// the signing family (C_Sign, C_GenerateKeyPair, C_Verify) and the
// key-management family (C_WrapKey, C_UnwrapKey, C_DeriveKey). Each feature's
// parameter model follows the function's OASIS Cryptoki argument signature —
// one group per argument, with values drawn from the mechanisms, attribute
// templates, and CKR_* return codes that argument can produce. Values marked
// via vg()/ecg() are intentional gaps (spec-permitted paths left untested).
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
		},
		{
			code: "KG", fn: "C_GenerateKeyPair", summary: "PKCS#11 C_GenerateKeyPair — asymmetric key-pair generation (8-argument signature)",
			cr: "Add P-521 curve to the key-generation matrix",
			groups: []pkcsGroup{
				{"hSession (session handle)", []pkcsValue{v("Valid R/W session"), vg("Read-only session"), ecg("CKR_SESSION_HANDLE_INVALID")}},
				{"pMechanism (mechanism)", []pkcsValue{v("CKM_RSA_PKCS_KEY_PAIR_GEN"), v("CKM_EC_KEY_PAIR_GEN"), vg("CKM_ED25519_KEY_PAIR_GEN"), ec("CKR_MECHANISM_INVALID")}},
				{"pPublicKeyTemplate (public template)", []pkcsValue{v("CKA_MODULUS_BITS 2048"), v("CKA_PUBLIC_EXPONENT 0x10001"), v("CKA_EC_PARAMS P-256"), vg("CKA_EC_PARAMS P-521"), ec("CKR_TEMPLATE_INCOMPLETE")}},
				{"ulPublicKeyAttributeCount (public attr count)", []pkcsValue{v("Matches template length"), ecg("CKR_ARGUMENTS_BAD (zero count)")}},
				{"pPrivateKeyTemplate (private template)", []pkcsValue{v("CKA_SIGN = true"), v("CKA_SENSITIVE = true"), v("CKA_EXTRACTABLE = false"), vg("CKA_TOKEN = true (persistent)"), ec("CKR_TEMPLATE_INCONSISTENT")}},
				{"ulPrivateKeyAttributeCount (private attr count)", []pkcsValue{v("Matches template length"), ecg("CKR_ARGUMENTS_BAD (zero count)")}},
				{"phPublicKey (public handle out)", []pkcsValue{v("Valid pointer receives handle"), ec("CKR_ARGUMENTS_BAD (NULL_PTR)")}},
				{"phPrivateKey (private handle out)", []pkcsValue{v("Valid pointer receives handle"), ec("CKR_ARGUMENTS_BAD (NULL_PTR)")}},
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
		},
		{
			code: "WRP", fn: "C_WrapKey", summary: "PKCS#11 C_WrapKey — export a key encrypted under a wrapping key (6-argument signature)",
			cr: "Add AES-GCM key wrapping (CKM_AES_GCM) to the wrap matrix",
			groups: []pkcsGroup{
				{"hSession (session handle)", []pkcsValue{v("Valid R/W session"), v("Invalid session"), ecg("CKR_SESSION_HANDLE_INVALID")}},
				{"pMechanism (wrapping mechanism)", []pkcsValue{v("CKM_AES_KEY_WRAP"), v("CKM_AES_KEY_WRAP_PAD"), v("CKM_RSA_PKCS_OAEP"), vg("CKM_AES_GCM"), ec("CKR_MECHANISM_INVALID")}},
				{"hWrappingKey (wrapping key handle)", []pkcsValue{v("AES key, CKA_WRAP=true"), v("RSA public key, CKA_WRAP=true"), ec("CKR_WRAPPING_KEY_HANDLE_INVALID"), ecg("CKR_WRAPPING_KEY_TYPE_INCONSISTENT")}},
				{"hKey (key to wrap)", []pkcsValue{v("CKA_EXTRACTABLE=true"), ec("CKR_KEY_UNEXTRACTABLE"), ecg("CKR_KEY_NOT_WRAPPABLE")}},
				{"pWrappedKey / pulWrappedKeyLen (output)", []pkcsValue{v("Query length (NULL_PTR buffer)"), v("Exact buffer"), ec("CKR_BUFFER_TOO_SMALL")}},
				{"Error paths", []pkcsValue{ec("CKR_OK"), ec("CKR_WRAPPING_KEY_SIZE_RANGE"), ecg("CKR_ARGUMENTS_BAD")}},
			},
		},
		{
			code: "UNW", fn: "C_UnwrapKey", summary: "PKCS#11 C_UnwrapKey — import a wrapped key blob as a new key object (8-argument signature)",
			cr: "Add AES-GCM unwrapping to the import matrix",
			groups: []pkcsGroup{
				{"hSession (session handle)", []pkcsValue{v("Valid R/W session"), vg("Read-only session"), ecg("CKR_SESSION_HANDLE_INVALID")}},
				{"pMechanism (unwrapping mechanism)", []pkcsValue{v("CKM_AES_KEY_WRAP"), v("CKM_RSA_PKCS_OAEP"), vg("CKM_AES_GCM"), ec("CKR_MECHANISM_INVALID")}},
				{"hUnwrappingKey (unwrapping key handle)", []pkcsValue{v("AES key, CKA_UNWRAP=true"), v("RSA private key, CKA_UNWRAP=true"), ec("CKR_UNWRAPPING_KEY_HANDLE_INVALID"), ecg("CKR_UNWRAPPING_KEY_TYPE_INCONSISTENT")}},
				{"pWrappedKey / ulWrappedKeyLen (wrapped blob)", []pkcsValue{v("Valid wrapped blob"), ec("CKR_WRAPPED_KEY_INVALID"), ecg("CKR_WRAPPED_KEY_LEN_RANGE")}},
				{"pTemplate / ulAttributeCount (new-key template)", []pkcsValue{v("CKA_CLASS=CKO_SECRET_KEY"), v("CKA_KEY_TYPE=CKK_AES"), vg("CKA_TOKEN=true (persistent)"), ec("CKR_TEMPLATE_INCONSISTENT")}},
				{"phKey (new key handle out)", []pkcsValue{v("Valid pointer receives handle"), ec("CKR_ARGUMENTS_BAD (NULL_PTR)")}},
				{"Error paths", []pkcsValue{ec("CKR_OK"), ec("CKR_ATTRIBUTE_VALUE_INVALID"), ecg("CKR_USER_NOT_LOGGED_IN")}},
			},
		},
		{
			code: "DRV", fn: "C_DeriveKey", summary: "PKCS#11 C_DeriveKey — derive a new key from a base key (6-argument signature)",
			cr: "Add SP800-108 KDF (CKM_SP800_108_COUNTER_KDF) to the derivation matrix",
			groups: []pkcsGroup{
				{"hSession (session handle)", []pkcsValue{v("Valid R/W session"), v("Invalid session"), ecg("CKR_SESSION_HANDLE_INVALID")}},
				{"pMechanism (derivation mechanism)", []pkcsValue{v("CKM_ECDH1_DERIVE"), v("CKM_DH_PKCS_DERIVE"), v("CKM_TLS12_KEY_AND_MAC_DERIVE"), vg("CKM_SP800_108_COUNTER_KDF"), ec("CKR_MECHANISM_INVALID"), ecg("CKR_MECHANISM_PARAM_INVALID")}},
				{"hBaseKey (base key handle)", []pkcsValue{v("EC private key, CKA_DERIVE=true"), v("DH private key"), ec("CKR_KEY_HANDLE_INVALID"), ecg("CKR_KEY_TYPE_INCONSISTENT")}},
				{"pTemplate / ulAttributeCount (derived-key template)", []pkcsValue{v("CKA_CLASS=CKO_SECRET_KEY"), v("CKA_KEY_TYPE=CKK_AES"), v("CKA_VALUE_LEN=32"), vg("CKA_SENSITIVE=true"), ec("CKR_TEMPLATE_INCOMPLETE")}},
				{"phKey (derived key handle out)", []pkcsValue{v("Valid pointer receives handle"), ec("CKR_ARGUMENTS_BAD (NULL_PTR)")}},
				{"Error paths", []pkcsValue{ec("CKR_OK"), ecg("CKR_DOMAIN_PARAMS_INVALID")}},
			},
		},
	}
}

// pkcsCustomers lists the two demo customer projects with their version lock and
// CR decision posture.
var pkcsCustomers = []struct {
	proj     string
	version  string
	decision string
	note     string
}{
	{"CUST-HSM-BANK", "2.40", "cannot_accept", "Locked to 2.40 for compliance."},
	{"CUST-HSM-SAMSU", "3.0", "can_accept", "Wants the new mechanisms."},
}

// SeedPKCSReference builds the coverage layer for the three PKCS#11 reference
// features, mapping onto synced demo-pkcs data. Idempotent.
func (m *Module) SeedPKCSReference(profileID string) (PKCSSeedSummary, error) {
	var sum PKCSSeedSummary
	feats := pkcsFeatures()

	// Remove any prior PKCS canonicals (and their version/CR/model cascade).
	if err := m.deletePriorPKCSCanonicals(profileID); err != nil {
		return sum, fmt.Errorf("clear prior PKCS canonicals: %w", err)
	}

	for _, f := range feats {
		if err := m.seedPKCSCoverage(profileID, f, &sum); err != nil {
			return sum, fmt.Errorf("seed coverage for %s: %w", f.fn, err)
		}
		sum.Features++
	}
	return sum, nil
}

// seedPKCSCoverage builds one feature's canonical requirement, versions, model,
// mappings, member locks, and change request, mapping onto synced data.
func (m *Module) seedPKCSCoverage(profileID string, f pkcsFeature, sum *PKCSSeedSummary) error {
	cid, err := m.CreateCanonical(profileID, f.fn, "PKCS11", f.summary)
	if err != nil {
		return err
	}

	// Step 1: Members = synced customer reqs whose summary starts with f.fn.
	rows, err := m.db.Query(
		`SELECT jira_key, project_key FROM requirement
		  WHERE profile_id=? AND project_key IN ('CUST-HSM-BANK','CUST-HSM-SAMSU')
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

	// Step 3: Build version 2.40 with the parameter model; map non-gap values
	// round-robin to the candidate tests. If the pool is empty, the model is
	// created but all mappings are skipped (coverage 0%).
	v240, err := m.CreateVersion(profileID, cid, "2.40", "stable", "Stable release locked by conservative customers.")
	if err != nil {
		return err
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

	// Step 4: Clone to 3.0 (beta).
	v30, err := m.CloneVersion(profileID, v240, "3.0", "beta")
	if err != nil {
		return err
	}
	sum.Versions += 2

	// Build per-project lookups for version lock and CR decision.
	custVerByProj := make(map[string]string, len(pkcsCustomers))
	custDecByProj := make(map[string]struct{ dec, note string }, len(pkcsCustomers))
	for _, c := range pkcsCustomers {
		var verID string
		switch c.version {
		case "2.40":
			verID = v240
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
	crID, err := m.CreateChangeRequest(profileID, cid, "CHG-PKCS-"+f.code, f.cr, "approved", v30, "low",
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

// deletePriorPKCSCanonicals removes any existing canonicals whose name matches a
// seeded feature (so a re-seed doesn't duplicate).
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
