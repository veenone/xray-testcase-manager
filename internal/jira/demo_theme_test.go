package jira

import "testing"

func TestMakeDemoTestPKCSThemed(t *testing.T) {
	pk := themeFor("demo-pkcs")
	// Feature cycles features[i%len]; C_Sign is index 0.
	tst := makeDemoTest(pk, "PKCS", 0)
	if got := tst.Summary; got == "" || got[:6] != "C_Sign" {
		t.Errorf("pkcs test summary = %q, want it to start with C_Sign", got)
	}
	// Generic is unchanged.
	g := makeDemoTest(genericTheme, "QA", 0)
	if g.Summary == "" {
		t.Error("generic test summary empty")
	}
}

func TestDemoRequirementsPKCSHasFunctionalAndCustomer(t *testing.T) {
	reqs, links := demoRequirements(themeFor("demo-pkcs"), "PKCS")
	var func0, cust0 bool
	for _, r := range reqs {
		if r.ProjectKey == "FUNC" && r.Summary == "C_Sign" {
			func0 = true
		}
		if r.ProjectKey == "CUST-HSM-BANK" {
			cust0 = true
		}
	}
	if !func0 {
		t.Error("pkcs requirements missing the C_Sign functional requirement (FUNC project)")
	}
	if !cust0 {
		t.Error("pkcs requirements missing a CUST-HSM-BANK customer requirement")
	}
	if len(links) == 0 {
		t.Error("pkcs requirements have no test links")
	}
	// Generic requirements unchanged (project PRD).
	gr, _ := demoRequirements(genericTheme, "DEMO")
	if len(gr) == 0 || gr[0].ProjectKey != "PRD" {
		t.Errorf("generic requirements changed: %+v", gr[:1])
	}
}

func TestDemoPreconditionsPKCSThemed(t *testing.T) {
	pre, links, err := demoPreconditionsAndLinks(themeFor("demo-pkcs"), "PKCS")
	if err != nil {
		t.Fatal(err)
	}
	if len(pre) == 0 || pre[0].Summary[:3] != "HSM" {
		t.Errorf("pkcs preconditions not themed: %+v", pre[:1])
	}
	if len(links) == 0 {
		t.Error("pkcs preconditions have no test links")
	}
	// Generic unchanged.
	gp, _, _ := demoPreconditionsAndLinks(genericTheme, "DEMO")
	if len(gp) == 0 || gp[0].Summary != "User account exists" {
		t.Errorf("generic preconditions changed: %+v", gp[:1])
	}
}

func TestThemeForSelectsVariant(t *testing.T) {
	if themeFor("demo").Variant != "" {
		t.Errorf("plain demo should be the generic theme")
	}
	if themeFor("https://jira.example.com").Variant != "" {
		t.Errorf("real URL should be the generic theme")
	}
	pk := themeFor("demo-pkcs")
	if pk.Variant != "pkcs" {
		t.Fatalf("demo-pkcs Variant = %q, want pkcs", pk.Variant)
	}
	if len(pk.Features) != 3 || pk.Features[0] != "C_Sign" {
		t.Errorf("pkcs features = %v, want [C_Sign C_GenerateKeyPair C_Verify]", pk.Features)
	}
	// Generic theme still exposes the original 30 features.
	if len(themeFor("demo").Features) < 20 {
		t.Errorf("generic theme lost its features: %d", len(themeFor("demo").Features))
	}
	// Every PKCS feature maps to a folder category and to preconditions.
	for _, f := range pk.Features {
		if _, ok := pk.FeaturePre[f]; !ok {
			t.Errorf("pkcs feature %q has no preconditions mapping", f)
		}
	}
}
