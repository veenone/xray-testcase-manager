package spellcheck

import "testing"

func testChecker() *Checker {
	dict := map[string]struct{}{
		"the": {}, "user": {}, "can": {}, "reset": {}, "password": {},
		"receive": {}, "separate": {}, "authentication": {}, "and": {}, "given": {},
	}
	return NewChecker(dict, []string{"euicc", "pkcs"})
}

func TestCheckTextFlagsUnknownWords(t *testing.T) {
	c := testChecker()
	got := c.CheckText("summary", "The user can recieve a passwrd")
	if len(got) != 2 {
		t.Fatalf("findings = %d (%+v), want 2", len(got), got)
	}
	if got[0].Word != "recieve" || got[0].Field != "summary" {
		t.Errorf("first finding = %+v, want word=recieve field=summary", got[0])
	}
	// Offset must point at the exact byte position of the word.
	text := "The user can recieve a passwrd"
	if text[got[0].Offset:got[0].Offset+got[0].Length] != "recieve" {
		t.Errorf("offset/length %d/%d does not slice to 'recieve'", got[0].Offset, got[0].Length)
	}
}

func TestCheckTextSkipsNoise(t *testing.T) {
	c := testChecker()
	// ALL-CAPS acronym, Jira key, URL, digit-word, camelCase, snake_case,
	// short word, and an allow-listed domain term must all be skipped.
	text := "PKCS RSP DEMO-TC-12 http://x.co v2 camelCase snake_case an euICC"
	got := c.CheckText("description", text)
	for _, f := range got {
		t.Errorf("unexpected finding for noise token: %q", f.Word)
	}
}

func TestCheckTextKnownWordsClean(t *testing.T) {
	c := testChecker()
	if got := c.CheckText("summary", "The user can reset password"); len(got) != 0 {
		t.Fatalf("clean text produced findings: %+v", got)
	}
}
