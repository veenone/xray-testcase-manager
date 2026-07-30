// Package spellcheck flags misspelled words in test-case text. The Checker is
// DB-free and takes an injected dictionary so it is fast and deterministic to
// unit-test; production wiring loads an embedded English wordlist (see
// NewDefaultChecker). Noise tokens common to test content — acronyms, Jira
// keys, URLs, identifiers, numbers — are skipped so real typos stand out.
package spellcheck

import (
	_ "embed"
	"sort"
	"strings"
	"sync"
)

//go:embed words_en.txt
var wordsEN string

// baseDict is the embedded English wordlist parsed once and shared read-only
// across scans, so NewDefaultChecker does not rebuild the ~370k-entry map on
// every scan.
var (
	baseDictOnce sync.Once
	baseDict     map[string]struct{}
)

func loadBaseDict() map[string]struct{} {
	baseDictOnce.Do(func() {
		d := make(map[string]struct{}, 400000)
		for _, line := range strings.Split(wordsEN, "\n") {
			w := strings.ToLower(strings.TrimSpace(line))
			if w != "" {
				d[w] = struct{}{}
			}
		}
		baseDict = d
	})
	return baseDict
}

// NewDefaultChecker builds a Checker from the embedded English wordlist plus
// the domain allow-list and any user ignore words.
func NewDefaultChecker(ignore []string) *Checker {
	allow := make([]string, 0, len(domainAllowList)+len(ignore))
	allow = append(allow, domainAllowList...)
	allow = append(allow, ignore...)
	return NewChecker(loadBaseDict(), allow)
}

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
// user ignore words). All keys are lowercased. byFirst indexes the dictionary
// by first letter so suggest() only scans same-initial candidates, and sugCache
// memoizes suggestions per distinct word so a repeated typo is computed once
// per scan. A Checker is not safe for concurrent CheckText calls (sugCache is
// mutated); scans are single-goroutine.
type Checker struct {
	dict     map[string]struct{}
	allow    map[string]struct{}
	byFirst  map[byte][]string
	sugCache map[string][]string
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
	byFirst := make(map[byte][]string, 32)
	for w := range dict {
		if w != "" {
			byFirst[w[0]] = append(byFirst[w[0]], w)
		}
	}
	return &Checker{
		dict:     dict,
		allow:    a,
		byFirst:  byFirst,
		sugCache: make(map[string][]string),
	}
}

// CheckText scans one field's text and returns a Finding per unknown word,
// including its suggestions (memoized per distinct word via the Checker's
// cache, so a repeated typo is computed once per scan). TestKey is left empty
// for the caller to fill.
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
			Field:       field,
			Word:        core,
			Offset:      off,
			Length:      len(core),
			Snippet:     snippet(text, off, len(core)),
			Suggestions: c.suggest(strings.ToLower(core)),
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

// suggest returns up to three dictionary words within edit distance 2 of w
// (lowercased), ranked by distance then by closeness in length then
// alphabetically. Candidates come from the first-letter index (typos that
// change the first letter are not suggested for, an acceptable trade-off for
// speed) and the result is memoized per word, so a repeated typo across many
// tests costs one computation per scan.
func (c *Checker) suggest(w string) []string {
	if w == "" {
		return nil
	}
	if s, ok := c.sugCache[w]; ok {
		return s
	}
	type cand struct {
		word string
		dist int
	}
	var cands []cand
	// Only same-first-letter candidates can be within distance while keeping the
	// first letter; the index makes this a small bucket instead of the whole
	// dictionary.
	for _, dw := range c.byFirst[w[0]] {
		if abs(len(dw)-len(w)) > 2 {
			continue
		}
		if d := levenshtein(w, dw, 2); d <= 2 {
			cands = append(cands, cand{dw, d})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		li, lj := abs(len(cands[i].word)-len(w)), abs(len(cands[j].word)-len(w))
		if li != lj {
			return li < lj
		}
		return cands[i].word < cands[j].word
	})
	out := make([]string, 0, 3)
	for i := 0; i < len(cands) && i < 3; i++ {
		out = append(out, cands[i].word)
	}
	c.sugCache[w] = out
	return out
}

// levenshtein computes the edit distance between a and b, returning the true
// edit distance when it is <= max, and max+1 otherwise; it never returns a
// value greater than max+1.
func levenshtein(a, b string, max int) int {
	la, lb := len(a), len(b)
	if abs(la-lb) > max {
		return max + 1
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		if rowMin > max {
			return max + 1
		}
		prev, curr = curr, prev
	}
	if prev[lb] > max {
		return max + 1
	}
	return prev[lb]
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
