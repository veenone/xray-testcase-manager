package spellcheck

import "testing"

func TestNewDefaultCheckerFlagsRealTypos(t *testing.T) {
	c := NewDefaultChecker(nil)
	// A real typo is flagged...
	if got := c.CheckText("summary", "recieve"); len(got) != 1 {
		t.Fatalf("'recieve' findings = %d, want 1", len(got))
	}
	// ...a correctly-spelled common word is not...
	if got := c.CheckText("summary", "authentication"); len(got) != 0 {
		t.Errorf("'authentication' flagged: %+v", got)
	}
	// ...and a domain term from the allow-list is not.
	if got := c.CheckText("summary", "euicc pkcs aspice"); len(got) != 0 {
		t.Errorf("domain terms flagged: %+v", got)
	}
}

func TestNewDefaultCheckerHonoursIgnore(t *testing.T) {
	// "widgetized" is not a real English word; ignoring it suppresses the flag.
	base := NewDefaultChecker(nil)
	if got := base.CheckText("summary", "widgetized"); len(got) == 0 {
		t.Skip("wordlist happens to contain the sample word; pick another in review")
	}
	c := NewDefaultChecker([]string{"widgetized"})
	if got := c.CheckText("summary", "widgetized"); len(got) != 0 {
		t.Errorf("ignored word still flagged: %+v", got)
	}
}
