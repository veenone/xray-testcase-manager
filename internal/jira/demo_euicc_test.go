package jira

import (
	"strings"
	"testing"
)

func TestDemoVariantEUICC(t *testing.T) {
	if got := demoVariant("demo-euicc"); got != "euicc" {
		t.Errorf("demoVariant(%q) = %q, want %q", "demo-euicc", got, "euicc")
	}
	if got := demoVariant("demo-euicc:extra"); got != "euicc" {
		t.Errorf("demoVariant(%q) = %q, want %q", "demo-euicc:extra", got, "euicc")
	}
	if got := demoVariant("DEMO-EUICC"); got != "euicc" {
		t.Errorf("demoVariant(%q) = %q, want %q (case-insensitive)", "DEMO-EUICC", got, "euicc")
	}
	// Other demo URLs must be unaffected.
	if got := demoVariant("demo-pkcs"); got != "pkcs" {
		t.Errorf("demoVariant(%q) = %q, want pkcs", "demo-pkcs", got)
	}
	if got := demoVariant("demo"); got != "" {
		t.Errorf("demoVariant(%q) = %q, want \"\" (generic)", "demo", got)
	}
}

func TestThemeForEUICC(t *testing.T) {
	theme := themeFor("demo-euicc")
	if theme.Variant != "euicc" {
		t.Fatalf("themeFor(%q).Variant = %q, want %q", "demo-euicc", theme.Variant, "euicc")
	}
	if len(theme.Features) != 7 {
		t.Fatalf("euiccTheme has %d features, want 7: %v", len(theme.Features), theme.Features)
	}
	wantFeatures := []string{
		"Profile Download",
		"Enable Profile",
		"Disable Profile",
		"Delete Profile",
		"eUICC Memory Reset",
		"Profile Fall-Back",
		"Profile Enable with Rollback",
	}
	for i, f := range wantFeatures {
		if theme.Features[i] != f {
			t.Errorf("euiccTheme.Features[%d] = %q, want %q", i, theme.Features[i], f)
		}
	}
	if theme.TestCount < 200 {
		t.Errorf("euiccTheme.TestCount = %d, must be >= 200 (demoLinkedTests cap)", theme.TestCount)
	}
	// Every feature must have a FeaturePre entry.
	for _, f := range theme.Features {
		if _, ok := theme.FeaturePre[f]; !ok {
			t.Errorf("euiccTheme.FeaturePre missing entry for feature %q", f)
		}
	}
	// All preconditions must be Manual.
	for _, p := range theme.Preconditions {
		if p.Type != "Manual" {
			t.Errorf("precondition %q has Type=%q, want Manual", p.Summary, p.Type)
		}
	}
	// Categories must cover all 7 features exactly once.
	covered := make(map[string]bool, len(theme.Features))
	for _, cat := range theme.Categories {
		for _, f := range cat.Features {
			if covered[f] {
				t.Errorf("feature %q appears in more than one category", f)
			}
			covered[f] = true
		}
	}
	for _, f := range theme.Features {
		if !covered[f] {
			t.Errorf("feature %q not found in any category", f)
		}
	}
	// pkcs and generic themes must be unchanged.
	if themeFor("demo-pkcs").Variant != "pkcs" {
		t.Error("pkcs theme changed by euicc addition")
	}
	if themeFor("demo").Variant != "" {
		t.Error("generic theme changed by euicc addition")
	}
}

func TestEUIccCode(t *testing.T) {
	cases := []struct {
		feature string
		want    string
	}{
		{"Profile Download", "DLD"},
		{"Enable Profile", "ENA"},
		{"Disable Profile", "DIS"},
		{"Delete Profile", "DEL"},
		{"eUICC Memory Reset", "RST"},
		{"Profile Fall-Back", "FBK"},
		{"Profile Enable with Rollback", "RBK"},
		{"Unknown", "Unknown"},
	}
	for _, tc := range cases {
		if got := euiccCode(tc.feature); got != tc.want {
			t.Errorf("euiccCode(%q) = %q, want %q", tc.feature, got, tc.want)
		}
	}
}

func TestDemoRequirementsEUICC(t *testing.T) {
	theme := themeFor("demo-euicc")
	reqs, links := demoRequirements(theme, "EUICC")

	// Expect: 7 features x 4 reqs each (1 FUNC + 3 CUST) = 28.
	if len(reqs) != 28 {
		t.Errorf("got %d requirements, want 28 (7 features x 4)", len(reqs))
	}

	// Collect by project key.
	byProject := make(map[string][]Requirement)
	for _, r := range reqs {
		byProject[r.ProjectKey] = append(byProject[r.ProjectKey], r)
	}

	// 7 functional requirements in FUNC project.
	if len(byProject["FUNC"]) != 7 {
		t.Errorf("FUNC project: got %d requirements, want 7", len(byProject["FUNC"]))
	}
	for _, r := range byProject["FUNC"] {
		if !strings.HasPrefix(r.Key, "FUNC-EUICC-") {
			t.Errorf("FUNC requirement key %q does not start with FUNC-EUICC-", r.Key)
		}
		if r.IssueType != "Requirement" {
			t.Errorf("FUNC requirement %q IssueType = %q, want Requirement", r.Key, r.IssueType)
		}
		if r.Status != "Approved" {
			t.Errorf("FUNC requirement %q Status = %q, want Approved", r.Key, r.Status)
		}
		// Summary must equal the feature name exactly (starts with = starts with for exact match).
		found := false
		for _, f := range theme.Features {
			if r.Summary == f {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("FUNC requirement %q summary %q does not match any feature name", r.Key, r.Summary)
		}
	}

	// 7 customer requirements per customer project.
	for _, proj := range []string{"CUST-MNO-CONSUMER", "CUST-IOT-FLEET", "CUST-M2M-AUTO"} {
		custReqs := byProject[proj]
		if len(custReqs) != 7 {
			t.Errorf("%s: got %d requirements, want 7", proj, len(custReqs))
		}
		for _, r := range custReqs {
			// Summary must start with a feature name.
			startsWithFeature := false
			for _, f := range theme.Features {
				if strings.HasPrefix(r.Summary, f) {
					startsWithFeature = true
					break
				}
			}
			if !startsWithFeature {
				t.Errorf("%s requirement %q summary %q does not start with a feature name",
					proj, r.Key, r.Summary)
			}
			if r.IssueType != "Story" {
				t.Errorf("%s requirement %q IssueType = %q, want Story", proj, r.Key, r.IssueType)
			}
			if r.Status != "In Progress" {
				t.Errorf("%s requirement %q Status = %q, want In Progress", proj, r.Key, r.Status)
			}
		}
	}

	// Links must point to EUICC-* test keys.
	if len(links) == 0 {
		t.Error("demoRequirements(euicc) produced no test links")
	}
	for _, l := range links {
		if !strings.HasPrefix(l.TestKey, "EUICC-") {
			t.Errorf("link TestKey %q does not start with EUICC-", l.TestKey)
		}
		if l.RequirementKey == "" {
			t.Error("link has empty RequirementKey")
		}
	}

	// pkcs and generic datasets must be unchanged.
	pkReqs, _ := demoRequirements(themeFor("demo-pkcs"), "PKCS")
	for _, r := range pkReqs {
		if r.ProjectKey == "FUNC" && !strings.HasPrefix(r.Key, "FUNC-PKCS11-") {
			t.Errorf("pkcs FUNC requirement key %q changed", r.Key)
		}
	}
	gReqs, _ := demoRequirements(themeFor("demo"), "DEMO")
	if len(gReqs) == 0 || gReqs[0].ProjectKey != "PRD" {
		t.Error("generic requirements changed")
	}
}

func TestDemoStepsForKeyEUICC(t *testing.T) {
	theme := themeFor("demo-euicc")
	steps := demoStepsForKey(theme, "EUICC-1")
	if len(steps) != 4 {
		t.Fatalf("euicc steps for EUICC-1: got %d steps, want 4", len(steps))
	}
	// First step must mention RSP session.
	if !strings.Contains(steps[0].Action, "RSP") {
		t.Errorf("euicc step 1 action = %q, want it to mention RSP session", steps[0].Action)
	}
	// Second step must mention the feature (Profile Download is index 0).
	if !strings.Contains(steps[1].Action, "Profile Download") {
		t.Errorf("euicc step 2 action = %q, want it to invoke Profile Download", steps[1].Action)
	}
}

func TestMakeDemoTestEUICC(t *testing.T) {
	theme := themeFor("demo-euicc")
	for _, i := range []int{0, 1, 2, 3, 4, 5, 6, 7} {
		tst := makeDemoTest(theme, "EUICC", i)
		feature := theme.Features[i%len(theme.Features)]
		if !strings.HasPrefix(tst.Summary, feature) {
			t.Errorf("makeDemoTest euicc index %d: summary %q does not start with feature %q",
				i, tst.Summary, feature)
		}
	}
}
