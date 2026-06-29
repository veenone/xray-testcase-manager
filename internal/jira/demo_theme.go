package jira

import "strings"

// folderCategory groups demo features into a Test Repository category.
type folderCategory struct {
	Name     string
	Features []string
}

// precondDef is a distinct demo precondition.
type precondDef struct {
	Summary string
	Type    string
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
// "pkcs" for a demo-pkcs URL, "" (generic) otherwise. isDemoURL still gates
// whether demo mode is on at all.
func demoVariant(baseURL string) string {
	u := strings.ToLower(strings.TrimSpace(baseURL))
	if u == "demo-pkcs" || strings.HasPrefix(u, "demo-pkcs:") {
		return "pkcs"
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
	default:
		return feature
	}
}

// themeFor returns the theme for a profile URL.
func themeFor(baseURL string) demoTheme {
	if demoVariant(baseURL) == "pkcs" {
		return pkcsTheme
	}
	return genericTheme
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

// pkcsTheme is the PKCS#11 demo vocabulary: three signing-family functions,
// PKCS-shaped conditions, a signing/key-management folder split, and HSM
// preconditions.
var pkcsTheme = demoTheme{
	Variant:  "pkcs",
	Features: []string{"C_Sign", "C_GenerateKeyPair", "C_Verify"},
	Conditions: []string{
		"with RSA-2048", "with ECDSA P-256", "with SHA-256",
		"on an invalid session", "on a closed session", "with a NULL buffer",
		"expecting CKR_BUFFER_TOO_SMALL", "expecting CKR_ARGUMENTS_BAD",
		"on a read-only session", "with a tampered input",
		"after the matching C_*Init", "without a prior C_*Init",
	},
	Categories: []folderCategory{
		{"Signing", []string{"C_Sign", "C_Verify"}},
		{"Key management", []string{"C_GenerateKeyPair"}},
	},
	Preconditions: []precondDef{
		{"HSM is initialized", "Manual"},
		{"A session is open on the token", "Manual"},
		{"User is logged in to the token", "Manual"},
		{"An RSA-2048 key pair exists", "Manual"},
		{"An EC P-256 key pair exists", "Manual"},
		{"The signing operation was initialized (C_SignInit)", "Manual"},
	},
	FeaturePre: map[string][]int{
		"C_Sign":            {0, 1, 2, 3, 5},
		"C_GenerateKeyPair": {0, 1, 2},
		"C_Verify":          {0, 1, 3, 4},
	},
	// Keep TestCount >= demoLinkedTests (200): the peripheral generic seeders
	// (test runs, bug links, cross-project sub-execution members) index tests up
	// to that cap and would otherwise reference nonexistent PKCS-<n> keys.
	TestCount: 240, // smaller, fully PKCS-flavoured corpus
}
