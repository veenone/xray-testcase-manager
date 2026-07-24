package coveragepublish

import (
	"strings"
	"testing"
)

func TestRenderDescription_WikiMarkupWithGapRow(t *testing.T) {
	rows := []valueRow{
		{Label: "CKM_RSA_PKCS", Kind: "value", TestKeys: []string{"TEST-11"}},
		{Label: "CKM_ED25519", Kind: "value", TestKeys: nil},
	}
	out := renderDescription("C_Sign", "1.0", "Mechanism", rows)

	want := []string{
		"||Value||Kind||Covered by||",
		"|CKM_RSA_PKCS|value|TEST-11|",
		"|CKM_ED25519|value|(none - gap)|",
	}
	for _, line := range want {
		if !containsLine(out, line) {
			t.Errorf("rendered description missing line %q\n--- got ---\n%s", line, out)
		}
	}
	if !strings.Contains(out, "C_Sign") || !strings.Contains(out, "1.0") {
		t.Errorf("footer should name the canonical requirement and version, got:\n%s", out)
	}
}

func TestRenderDescription_MultipleTestsJoined(t *testing.T) {
	rows := []valueRow{
		{Label: "CKM_AES_CBC", Kind: "value", TestKeys: []string{"TEST-1", "TEST-2"}},
	}
	out := renderDescription("C_Encrypt", "2.0", "Cipher", rows)
	if !containsLine(out, "|CKM_AES_CBC|value|TEST-1, TEST-2|") {
		t.Errorf("expected joined test keys, got:\n%s", out)
	}
}

func TestSplitPublishedTests(t *testing.T) {
	if got := splitPublishedTests(""); len(got) != 0 {
		t.Errorf("splitPublishedTests(\"\") = %v, want empty (no phantom entry)", got)
	}
	got := splitPublishedTests("QA-1\nQA-2")
	if len(got) != 2 || got[0] != "QA-1" || got[1] != "QA-2" {
		t.Errorf("splitPublishedTests(QA-1\\nQA-2) = %v, want [QA-1 QA-2]", got)
	}
}

func TestJoinSplitPublishedTests_RoundTrip(t *testing.T) {
	if joinPublishedTests(nil) != "" {
		t.Errorf("joinPublishedTests(nil) = %q, want empty string", joinPublishedTests(nil))
	}
	if got := splitPublishedTests(joinPublishedTests(nil)); len(got) != 0 {
		t.Errorf("round trip of empty set = %v, want empty", got)
	}
	keys := []string{"QA-1", "QA-2", "QA-3"}
	if got := splitPublishedTests(joinPublishedTests(keys)); len(got) != 3 {
		t.Errorf("round trip = %v, want %v", got, keys)
	}
}

func containsLine(text, line string) bool {
	for _, l := range strings.Split(text, "\n") {
		if l == line {
			return true
		}
	}
	return false
}
