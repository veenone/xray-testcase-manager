package jira

import "testing"

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
