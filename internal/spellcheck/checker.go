// Package spellcheck flags misspelled words in test-case text. The Checker is
// DB-free and takes an injected dictionary so it is fast and deterministic to
// unit-test; production wiring loads an embedded English wordlist (see
// NewDefaultChecker). Noise tokens common to test content — acronyms, Jira
// keys, URLs, identifiers, numbers — are skipped so real typos stand out.
package spellcheck

import "strings"

// Finding is one misspelled word located in one field of one test.
type Finding struct {
	TestKey     string   `json:"testKey"`
	Field       string   `json:"field"`
	Word        string   `json:"word"`
	Snippet     string   `json:"snippet"`
	Offset      int      `json:"offset"`
	Length      int      `json:"length"`
	Suggestions []string `json:"suggestions"`
}

// TestText is the DB-free input to a scan: one test's checkable fields.
type TestText struct {
	Key               string
	Summary           string
	Description       string
	CucumberScenario  string
	GenericDefinition string
}

// Checker holds the known-word dictionary and the allow-list (domain terms +
// user ignore words). All keys are lowercased.
type Checker struct {
	dict  map[string]struct{}
	allow map[string]struct{}
}

// NewChecker builds a Checker from a lowercased dictionary set and an
// allow-list of words to never flag (case-insensitive).
func NewChecker(dict map[string]struct{}, allow []string) *Checker {
	a := make(map[string]struct{}, len(allow))
	for _, w := range allow {
		w = strings.ToLower(strings.TrimSpace(w))
		if w != "" {
			a[w] = struct{}{}
		}
	}
	return &Checker{dict: dict, allow: a}
}

// CheckText scans one field's text and returns a Finding per unknown word.
// TestKey is left empty for the caller to fill. Suggestions are populated in a
// later step.
func (c *Checker) CheckText(field, text string) []Finding {
	var out []Finding
	n := len(text)
	for i := 0; i < n; {
		if !isWordByte(text[i]) {
			i++
			continue
		}
		start := i
		for i < n && isWordByte(text[i]) {
			i++
		}
		raw := text[start:i]
		core, coff := trimToAlphaCore(raw)
		if core == "" || c.skip(core) {
			continue
		}
		if _, known := c.dict[strings.ToLower(core)]; known {
			continue
		}
		off := start + coff
		out = append(out, Finding{
			Field:   field,
			Word:    core,
			Offset:  off,
			Length:  len(core),
			Snippet: snippet(text, off, len(core)),
		})
	}
	return out
}

// skip reports whether a trimmed token should not be dictionary-checked:
// too short, in the allow-list, contains any non-letter (digit / separator /
// apostrophe → Jira key, URL, snake_case, "v2", contraction), camelCase, or an
// ALL-CAPS acronym.
func (c *Checker) skip(w string) bool {
	if len(w) < 3 {
		return true
	}
	if _, ok := c.allow[strings.ToLower(w)]; ok {
		return true
	}
	hasUpper, hasLower, prevLower := false, false, false
	for i := 0; i < len(w); i++ {
		ch := w[i]
		switch {
		case ch >= 'A' && ch <= 'Z':
			if prevLower {
				return true // lower→upper transition = camelCase
			}
			hasUpper = true
			prevLower = false
		case ch >= 'a' && ch <= 'z':
			hasLower = true
			prevLower = true
		default:
			return true // digit, separator, apostrophe, non-ASCII
		}
	}
	return hasUpper && !hasLower // ALL-CAPS acronym
}

// isWordByte matches characters that form a single "word-ish" run, including
// separators/digits so that keys/URLs/identifiers are captured whole and then
// rejected by skip.
func isWordByte(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		return true
	}
	switch b {
	case '\'', '-', '_', '.', ':', '/', '@':
		return true
	}
	return false
}

// trimToAlphaCore strips leading/trailing non-letter bytes and returns the
// inner span plus its offset within raw. Internal non-letters are preserved so
// skip can reject them.
func trimToAlphaCore(raw string) (string, int) {
	isLetter := func(b byte) bool {
		return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
	}
	lo := 0
	for lo < len(raw) && !isLetter(raw[lo]) {
		lo++
	}
	hi := len(raw)
	for hi > lo && !isLetter(raw[hi-1]) {
		hi--
	}
	if lo >= hi {
		return "", 0
	}
	return raw[lo:hi], lo
}

// snippet returns up to 24 characters of context on each side of the word.
func snippet(text string, off, length int) string {
	const pad = 24
	start := off - pad
	if start < 0 {
		start = 0
	}
	end := off + length + pad
	if end > len(text) {
		end = len(text)
	}
	s := text[start:end]
	if start > 0 {
		s = "…" + s
	}
	if end < len(text) {
		s = s + "…"
	}
	return s
}
