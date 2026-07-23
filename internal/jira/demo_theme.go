package jira

import "strings"

// folderCategory groups demo features into a Test Repository category.
type folderCategory struct {
	Name     string
	Features []string
}

// precondDef is a distinct demo precondition.
type precondDef struct {
	Summary   string
	Type      string
	Condition string
}

// demoTheme bundles the vocabulary that gives a demo dataset its flavour. The
// structural generators stay the same; only this vocabulary changes per variant.
type demoTheme struct {
	Variant       string
	Features      []string
	Conditions    []string
	Categories    []folderCategory
	Preconditions []precondDef
	FeaturePre    map[string][]int
	TestCount     int
}

// demoVariant returns the demo dataset variant selected by a profile URL:
// "pkcs" for a demo-pkcs URL, "euicc" for a demo-euicc URL, "" (generic)
// otherwise. isDemoURL still gates whether demo mode is on at all.
func demoVariant(baseURL string) string {
	u := strings.ToLower(strings.TrimSpace(baseURL))
	if u == "demo-pkcs" || strings.HasPrefix(u, "demo-pkcs:") {
		return "pkcs"
	}
	if u == "demo-euicc" || strings.HasPrefix(u, "demo-euicc:") {
		return "euicc"
	}
	return ""
}

// pkcsCode maps a PKCS#11 feature name to a short code used in requirement
// keys (e.g. "C_Sign" → "SIG"). Unknown features fall back to the feature
// name itself so the mapping is always defined.
func pkcsCode(feature string) string {
	switch feature {
	case "C_Sign":
		return "SIG"
	case "C_GenerateKeyPair":
		return "KG"
	case "C_Verify":
		return "VER"
	case "C_WrapKey":
		return "WRP"
	case "C_UnwrapKey":
		return "UNW"
	case "C_DeriveKey":
		return "DRV"
	default:
		return feature
	}
}

// euiccCode maps a GSMA RSP procedure name to a short code used in requirement
// keys (e.g. "Profile Download" -> "DLD"). Unknown procedures fall back to the
// procedure name itself so the mapping is always defined.
func euiccCode(feature string) string {
	switch feature {
	case "Profile Download":
		return "DLD"
	case "Enable Profile":
		return "ENA"
	case "Disable Profile":
		return "DIS"
	case "Delete Profile":
		return "DEL"
	case "eUICC Memory Reset":
		return "RST"
	case "Profile Fall-Back":
		return "FBK"
	case "Profile Enable with Rollback":
		return "RBK"
	default:
		return feature
	}
}

// themeFor returns the theme for a profile URL.
func themeFor(baseURL string) demoTheme {
	switch demoVariant(baseURL) {
	case "pkcs":
		return pkcsTheme
	case "euicc":
		return euiccTheme
	default:
		return genericTheme
	}
}

// euiccTheme is the GSMA eUICC / RSP demo vocabulary: seven canonical RSP
// procedures, telco/IoT-flavoured conditions, lifecycle folder groupings, and
// RSP-relevant preconditions.
var euiccTheme = demoTheme{
	Variant: "euicc",
	Features: []string{
		"Profile Download",
		"Enable Profile",
		"Disable Profile",
		"Delete Profile",
		"eUICC Memory Reset",
		"Profile Fall-Back",
		"Profile Enable with Rollback",
	},
	Conditions: []string{
		"on a Disabled profile",
		"on the Enabled profile",
		"on an unknown ICCID",
		"with POL1 rules",
		"over ES9+",
		"via eIM bulk command",
		"on an NB-IoT device",
		"on a no-UI IoT device",
		"after connectivity loss",
		"with the Fall-Back Attribute set",
		"during network attach",
		"expecting rollback to the previous profile",
	},
	Categories: []folderCategory{
		{"Profile lifecycle", []string{
			"Profile Download",
			"Enable Profile",
			"Disable Profile",
			"Delete Profile",
		}},
		{"eUICC management", []string{
			"eUICC Memory Reset",
		}},
		{"Resilience (fall-back & rollback)", []string{
			"Profile Fall-Back",
			"Profile Enable with Rollback",
		}},
	},
	Preconditions: []precondDef{
		{"eUICC provisioned with EID and bootstrap profile", "Manual", ""},
		{"Device attached to NB-IoT network", "Manual", ""},
		{"An RSP session is established with SM-DP+", "Manual", ""},
		{"eIM is configured and associated (SGP.32)", "Manual", ""},
		{"SM-SR is associated (SGP.02)", "Manual", ""},
		{"A profile is installed and Disabled", "Manual", ""},
		{"The Enabled profile has the Fall-Back Attribute", "Manual", ""},
		{"Two profiles installed (one Enabled, one Disabled)", "Manual", ""},
	},
	FeaturePre: map[string][]int{
		"Profile Download":            {0, 1, 2, 3, 4},
		"Enable Profile":              {0, 2, 5, 7},
		"Disable Profile":             {0, 2, 5, 7},
		"Delete Profile":              {0, 2, 5},
		"eUICC Memory Reset":          {0, 2},
		"Profile Fall-Back":           {0, 1, 6, 7},
		"Profile Enable with Rollback": {0, 2, 5, 7},
	},
	// Keep TestCount >= demoLinkedTests (200): the peripheral generic seeders
	// (test runs, bug links, cross-project sub-execution members) index tests up
	// to that cap and would otherwise reference nonexistent EUICC-<n> keys.
	TestCount: 240, // smaller, fully eUICC-flavoured corpus
}

// genericTheme is the original web-app demo vocabulary (references the existing
// package vars so the generic dataset is unchanged).
var genericTheme = demoTheme{
	Variant:       "",
	Features:      demoFeatures,
	Conditions:    demoConditions,
	Categories:    demoFolderCategories,
	Preconditions: preconditionDefs,
	FeaturePre:    featurePreconditions,
	TestCount:     demoTestCount,
}

// pkcsTheme is the PKCS#11 demo vocabulary: six Cryptoki functions spanning the
// signing family (C_Sign, C_GenerateKeyPair, C_Verify) and the key-management
// family (C_WrapKey, C_UnwrapKey, C_DeriveKey), PKCS-shaped conditions, a
// signing / key-management / key-transport folder split, and HSM preconditions.
var pkcsTheme = demoTheme{
	Variant:  "pkcs",
	Features: []string{"C_Sign", "C_GenerateKeyPair", "C_Verify", "C_WrapKey", "C_UnwrapKey", "C_DeriveKey"},
	Conditions: []string{
		"with RSA-2048", "with ECDSA P-256", "with SHA-256",
		"on an invalid session", "on a closed session", "with a NULL buffer",
		"expecting CKR_BUFFER_TOO_SMALL", "expecting CKR_ARGUMENTS_BAD",
		"on a read-only session", "with a tampered input",
		"after the matching C_*Init", "without a prior C_*Init",
	},
	Categories: []folderCategory{
		{"Signing", []string{"C_Sign", "C_Verify"}},
		{"Key management", []string{"C_GenerateKeyPair", "C_DeriveKey"}},
		{"Key transport", []string{"C_WrapKey", "C_UnwrapKey"}},
	},
	Preconditions: []precondDef{
		{"HSM is initialized", "Manual", ""},
		{"A session is open on the token", "Manual", ""},
		{"User is logged in to the token", "Manual", ""},
		{"An RSA-2048 key pair exists", "Manual", ""},
		{"An EC P-256 key pair exists", "Manual", ""},
		{"The signing operation was initialized (C_SignInit)", "Manual", ""},
		{"An AES-256 key exists on the token (CKA_WRAP / CKA_UNWRAP / CKA_DERIVE)", "Manual", ""},
	},
	FeaturePre: map[string][]int{
		"C_Sign":            {0, 1, 2, 3, 5},
		"C_GenerateKeyPair": {0, 1, 2},
		"C_Verify":          {0, 1, 3, 4},
		"C_WrapKey":         {0, 1, 2, 6},
		"C_UnwrapKey":       {0, 1, 2, 6},
		"C_DeriveKey":       {0, 1, 2, 4, 6},
	},
	// Keep TestCount >= demoLinkedTests (200): the peripheral generic seeders
	// (test runs, bug links, cross-project sub-execution members) index tests up
	// to that cap and would otherwise reference nonexistent PKCS-<n> keys.
	TestCount: 240, // smaller, fully PKCS-flavoured corpus
}
