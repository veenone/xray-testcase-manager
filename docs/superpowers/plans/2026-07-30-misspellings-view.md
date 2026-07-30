# Misspellings View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Misspellings view that scans a profile's synced test cases for spelling errors and lets the user Apply a suggestion or Ignore each word.

**Architecture:** A pure, DB-free `internal/spellcheck` package (tokenizer + injected dictionary + suggester) does the linguistic work. `app.go` orchestrates: it paginates `repo.ListTests`, runs the checker, and returns findings. Corrections are applied server-side by re-reading the field, splicing at the exact offset, and routing through the existing `EditTestField` pending-change pipeline. Ignored words persist in the global settings key-value store.

**Tech Stack:** Go 1.25 (pure-Go, no cgo), `//go:embed` for the wordlist, Wails v2 bindings, React 19 + TypeScript frontend.

## Global Constraints

- Go build must stay **pure-Go (no cgo)** — do not add cgo dependencies.
- **No `Co-Authored-By` / AI-attribution trailer** in any commit message.
- Do **not** commit build-tool churn: `go.mod`/`go.sum` unless a real dependency is added, `frontend/package-lock.json`, `frontend/package.json.md5`.
- Repo files use **CRLF**. Never run `gofmt -w` on an existing file (it rewrites CRLF→LF). For new Go files, write them and verify format with: `tr -d '\r' < FILE | gofmt -d` (expect empty output). New `.go` files may be committed with LF; git's autocrlf handles the rest.
- Keep files under 500 lines. New backend code lives under `internal/`; `app.go` only adapts backend logic to Wails bindings.
- Field names are exactly: `summary`, `description`, `cucumber_scenario`, `generic_definition`.

---

### Task 1: spellcheck package — types, tokenizer, and flagging

**Files:**
- Create: `internal/spellcheck/checker.go`
- Test: `internal/spellcheck/checker_test.go`

**Interfaces:**
- Produces:
  - `type Finding struct { TestKey, Field, Word, Snippet string; Offset, Length int; Suggestions []string }` (JSON tags: `testKey`, `field`, `word`, `snippet`, `offset`, `length`, `suggestions`)
  - `type TestText struct { Key, Summary, Description, CucumberScenario, GenericDefinition string }`
  - `type Checker struct { ... }`
  - `func NewChecker(dict map[string]struct{}, allow []string) *Checker`
  - `func (c *Checker) CheckText(field, text string) []Finding` — findings have `TestKey` empty (caller fills it); `Suggestions` is nil until Task 2.

- [ ] **Step 1: Write the failing test**

Create `internal/spellcheck/checker_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /c/projects/xray-test-manager && go test ./internal/spellcheck/ -run TestCheckText -v`
Expected: FAIL — `undefined: NewChecker` / package has no Go files.

- [ ] **Step 3: Write minimal implementation**

Create `internal/spellcheck/checker.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /c/projects/xray-test-manager && go test ./internal/spellcheck/ -run TestCheckText -v`
Expected: PASS (all three tests).

Also verify formatting: `tr -d '\r' < internal/spellcheck/checker.go | gofmt -d` → empty output.

- [ ] **Step 5: Commit**

```bash
cd /c/projects/xray-test-manager
git add internal/spellcheck/checker.go internal/spellcheck/checker_test.go
git commit -m "feat(spellcheck): tokenizer and unknown-word flagging"
```

---

### Task 2: suggestions (bounded Levenshtein)

**Files:**
- Modify: `internal/spellcheck/checker.go` (add `suggest`, `levenshtein`; call `suggest` in `CheckText`)
- Test: `internal/spellcheck/checker_test.go` (add suggestion test)

**Interfaces:**
- Consumes: `Checker`, `Finding` (Task 1)
- Produces: `func (c *Checker) suggest(lower string) []string` — up to 3 dictionary words within edit distance 2, ranked. `CheckText` now sets `Finding.Suggestions`.

- [ ] **Step 1: Write the failing test**

Add to `internal/spellcheck/checker_test.go`:

```go
func TestSuggestionsRankClosest(t *testing.T) {
	c := testChecker()
	got := c.CheckText("summary", "recieve")
	if len(got) != 1 {
		t.Fatalf("findings = %d, want 1", len(got))
	}
	if len(got[0].Suggestions) == 0 || got[0].Suggestions[0] != "receive" {
		t.Errorf("suggestions = %v, want first = receive", got[0].Suggestions)
	}
}

func TestSuggestionsCappedAtThree(t *testing.T) {
	c := testChecker()
	got := c.CheckText("summary", "passwrd")
	if len(got) != 1 {
		t.Fatalf("findings = %d, want 1", len(got))
	}
	if len(got[0].Suggestions) > 3 {
		t.Errorf("suggestions = %v, want <= 3", got[0].Suggestions)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /c/projects/xray-test-manager && go test ./internal/spellcheck/ -run TestSuggestions -v`
Expected: FAIL — `Suggestions` is nil, `got[0].Suggestions[0]` panics or empty.

- [ ] **Step 3: Write minimal implementation**

In `internal/spellcheck/checker.go`, add `"sort"` to the import block, set suggestions in `CheckText` (replace the `out = append(...)` Finding literal so it includes `Suggestions`):

```go
		out = append(out, Finding{
			Field:       field,
			Word:        core,
			Offset:      off,
			Length:      len(core),
			Snippet:     snippet(text, off, len(core)),
			Suggestions: c.suggest(strings.ToLower(core)),
		})
```

Then append these functions:

```go
// suggest returns up to three dictionary words within edit distance 2 of w
// (lowercased), ranked by distance then by closeness in length then
// alphabetically. A same-first-letter prefilter keeps the scan cheap on a
// large dictionary; typos that change the first letter are not suggested for
// (an acceptable trade-off for speed).
func (c *Checker) suggest(w string) []string {
	if w == "" {
		return nil
	}
	type cand struct {
		word string
		dist int
	}
	var cands []cand
	for dw := range c.dict {
		if dw == "" || dw[0] != w[0] {
			continue
		}
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
	return out
}

// levenshtein computes the edit distance between a and b, returning early with
// max+1 once the minimum of a row exceeds max.
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /c/projects/xray-test-manager && go test ./internal/spellcheck/ -v`
Expected: PASS (all tests, including Task 1's).

Verify format: `tr -d '\r' < internal/spellcheck/checker.go | gofmt -d` → empty.

- [ ] **Step 5: Commit**

```bash
cd /c/projects/xray-test-manager
git add internal/spellcheck/checker.go internal/spellcheck/checker_test.go
git commit -m "feat(spellcheck): ranked Levenshtein suggestions"
```

---

### Task 3: ScanTests over multiple fields

**Files:**
- Create: `internal/spellcheck/scan.go`
- Test: `internal/spellcheck/scan_test.go`

**Interfaces:**
- Consumes: `Checker`, `TestText`, `Finding` (Tasks 1–2)
- Produces: `func ScanTests(tests []TestText, c *Checker) []Finding` — flattens findings across the four fields of every test, stamping `TestKey`.

- [ ] **Step 1: Write the failing test**

Create `internal/spellcheck/scan_test.go`:

```go
package spellcheck

import "testing"

func TestScanTestsStampsKeyAndCoversFields(t *testing.T) {
	c := testChecker()
	tests := []TestText{
		{Key: "T-1", Summary: "recieve", Description: "the user"},
		{Key: "T-2", GenericDefinition: "passwrd", CucumberScenario: "Given the user"},
	}
	got := ScanTests(tests, c)
	if len(got) != 2 {
		t.Fatalf("findings = %d (%+v), want 2", len(got), got)
	}
	byKey := map[string]Finding{}
	for _, f := range got {
		byKey[f.TestKey] = f
	}
	if byKey["T-1"].Field != "summary" || byKey["T-1"].Word != "recieve" {
		t.Errorf("T-1 finding = %+v", byKey["T-1"])
	}
	if byKey["T-2"].Field != "generic_definition" || byKey["T-2"].Word != "passwrd" {
		t.Errorf("T-2 finding = %+v", byKey["T-2"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /c/projects/xray-test-manager && go test ./internal/spellcheck/ -run TestScanTests -v`
Expected: FAIL — `undefined: ScanTests`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/spellcheck/scan.go`:

```go
package spellcheck

// ScanTests runs the checker over the four checkable fields of every test and
// returns all findings, each stamped with its TestKey.
func ScanTests(tests []TestText, c *Checker) []Finding {
	var out []Finding
	for _, tt := range tests {
		fields := []struct{ name, text string }{
			{"summary", tt.Summary},
			{"description", tt.Description},
			{"cucumber_scenario", tt.CucumberScenario},
			{"generic_definition", tt.GenericDefinition},
		}
		for _, f := range fields {
			for _, finding := range c.CheckText(f.name, f.text) {
				finding.TestKey = tt.Key
				out = append(out, finding)
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /c/projects/xray-test-manager && go test ./internal/spellcheck/ -v`
Expected: PASS.

Verify format: `tr -d '\r' < internal/spellcheck/scan.go | gofmt -d` → empty.

- [ ] **Step 5: Commit**

```bash
cd /c/projects/xray-test-manager
git add internal/spellcheck/scan.go internal/spellcheck/scan_test.go
git commit -m "feat(spellcheck): ScanTests across test-case fields"
```

---

### Task 4: embedded wordlist, domain allow-list, and NewDefaultChecker

**Files:**
- Create: `internal/spellcheck/words_en.txt` (downloaded wordlist, MIT)
- Create: `internal/spellcheck/allowlist.go`
- Modify: `internal/spellcheck/checker.go` (add embed + `NewDefaultChecker`)
- Test: `internal/spellcheck/default_test.go`

**Interfaces:**
- Consumes: `Checker`, `NewChecker` (Task 1)
- Produces:
  - `var domainAllowList []string`
  - `func NewDefaultChecker(ignore []string) *Checker` — loads the embedded English wordlist plus `domainAllowList` plus the caller's `ignore` words.

- [ ] **Step 1: Obtain the wordlist**

Download the MIT-licensed English wordlist (dwyl/english-words, `words_alpha.txt`, ~370k lowercase alpha words) and save it as the embed target:

```bash
cd /c/projects/xray-test-manager
curl -fsSL https://raw.githubusercontent.com/dwyl/english-words/master/words_alpha.txt -o internal/spellcheck/words_en.txt
# Sanity: a few thousand+ lines, all lowercase alpha
wc -l internal/spellcheck/words_en.txt
head -3 internal/spellcheck/words_en.txt
```

Expected: a large line count (hundreds of thousands); words like `a`, `aa`, `aaa`. (If the URL is unavailable, substitute any permissively-licensed newline-delimited English wordlist saved to the same path.)

- [ ] **Step 2: Write the failing test**

Create `internal/spellcheck/default_test.go`:

```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd /c/projects/xray-test-manager && go test ./internal/spellcheck/ -run TestNewDefaultChecker -v`
Expected: FAIL — `undefined: NewDefaultChecker`.

- [ ] **Step 4: Write minimal implementation**

Create `internal/spellcheck/allowlist.go`:

```go
package spellcheck

// domainAllowList holds recurring domain vocabulary that a general English
// dictionary flags but which is correct in this product's test content:
// product/spec names, standards acronyms rendered in lowercase, and Gherkin
// keywords. Add terms here (lowercase) as they surface; user-specific terms
// belong in the per-user ignore list instead.
var domainAllowList = []string{
	// product & spec names
	"xray", "euicc", "esim", "pkcs", "cryptoki", "aspice",
	// standards / telecom acronyms (lowercased forms)
	"rsp", "mno", "gsma", "sgp", "isd", "kiwi", "tcms",
	// Gherkin keywords
	"given", "when", "then", "scenario", "feature", "background", "outline", "examples",
}
```

In `internal/spellcheck/checker.go`, add the embed. At the top of the file, change the import block to include `_ "embed"` and add the embed directive + loader. Add after the imports:

```go
import (
	_ "embed"
	"sort"
	"strings"
)

//go:embed words_en.txt
var wordsEN string

// NewDefaultChecker builds a Checker from the embedded English wordlist plus
// the domain allow-list and any user ignore words.
func NewDefaultChecker(ignore []string) *Checker {
	dict := make(map[string]struct{}, 400000)
	for _, line := range strings.Split(wordsEN, "\n") {
		w := strings.ToLower(strings.TrimSpace(line))
		if w != "" {
			dict[w] = struct{}{}
		}
	}
	allow := make([]string, 0, len(domainAllowList)+len(ignore))
	allow = append(allow, domainAllowList...)
	allow = append(allow, ignore...)
	return NewChecker(dict, allow)
}
```

(Note: the existing import block from Tasks 1–2 already imports `sort` and `strings`; merge — do not duplicate imports. The only additions are `_ "embed"` and the `//go:embed` + `NewDefaultChecker` below the imports.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /c/projects/xray-test-manager && go test ./internal/spellcheck/ -v`
Expected: PASS.

Verify format: `tr -d '\r' < internal/spellcheck/checker.go | gofmt -d && tr -d '\r' < internal/spellcheck/allowlist.go | gofmt -d` → empty.

- [ ] **Step 6: Commit**

```bash
cd /c/projects/xray-test-manager
git add internal/spellcheck/words_en.txt internal/spellcheck/allowlist.go internal/spellcheck/checker.go internal/spellcheck/default_test.go
git commit -m "feat(spellcheck): embedded English dictionary and domain allow-list"
```

---

### Task 5: settings — persist user ignore words

**Files:**
- Modify: `internal/settings/settings.go`
- Test: `internal/settings/settings_test.go`

**Interfaces:**
- Produces:
  - `func (m *Manager) GetIgnoreWords() ([]string, error)`
  - `func (m *Manager) AddIgnoreWord(word string) error` — lowercases, de-dupes, persists as a JSON array under `spellcheck_ignore_words`.

- [ ] **Step 1: Write the failing test**

Add to `internal/settings/settings_test.go` (reuse the file's existing store-open helper pattern; if none, open inline as shown):

```go
func TestIgnoreWordsRoundTrip(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	m := NewManager(st)

	if words, err := m.GetIgnoreWords(); err != nil || len(words) != 0 {
		t.Fatalf("initial GetIgnoreWords = %v, %v; want empty", words, err)
	}
	if err := m.AddIgnoreWord("  EUICC "); err != nil {
		t.Fatalf("AddIgnoreWord: %v", err)
	}
	if err := m.AddIgnoreWord("euicc"); err != nil { // duplicate (post-normalise)
		t.Fatalf("AddIgnoreWord dup: %v", err)
	}
	if err := m.AddIgnoreWord("widgetized"); err != nil {
		t.Fatalf("AddIgnoreWord 2: %v", err)
	}
	words, err := m.GetIgnoreWords()
	if err != nil {
		t.Fatalf("GetIgnoreWords: %v", err)
	}
	if len(words) != 2 || words[0] != "euicc" || words[1] != "widgetized" {
		t.Errorf("words = %v, want [euicc widgetized]", words)
	}
}
```

Ensure the test file imports `path/filepath` and `xray-test-manager/internal/store` (add if missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /c/projects/xray-test-manager && go test ./internal/settings/ -run TestIgnoreWords -v`
Expected: FAIL — `m.GetIgnoreWords undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/settings/settings.go`:

Add to the import block: `"encoding/json"` and `"strings"`.

Add the key constant to the `const (...)` block:

```go
	keySpellcheckIgnore = "spellcheck_ignore_words"
```

Add the two methods (e.g. after `SetRequirementLinkType`):

```go
// GetIgnoreWords returns the user's persisted spellcheck ignore list
// (lowercased words), empty when none are set.
func (m *Manager) GetIgnoreWords() ([]string, error) {
	raw, err := m.value(keySpellcheckIgnore)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}
	var words []string
	if err := json.Unmarshal([]byte(raw), &words); err != nil {
		return nil, fmt.Errorf("parse ignore words: %w", err)
	}
	return words, nil
}

// AddIgnoreWord adds a word (lowercased, trimmed) to the ignore list. No-op for
// blank input or a word already present.
func (m *Manager) AddIgnoreWord(word string) error {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return nil
	}
	words, err := m.GetIgnoreWords()
	if err != nil {
		return err
	}
	for _, w := range words {
		if w == word {
			return nil
		}
	}
	words = append(words, word)
	b, err := json.Marshal(words)
	if err != nil {
		return fmt.Errorf("encode ignore words: %w", err)
	}
	return m.setValue(keySpellcheckIgnore, string(b))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /c/projects/xray-test-manager && go test ./internal/settings/ -v`
Expected: PASS.

Verify format: `tr -d '\r' < internal/settings/settings.go | gofmt -d` → empty (surgically fix alignment only if the tool flags added lines; never `gofmt -w`).

- [ ] **Step 5: Commit**

```bash
cd /c/projects/xray-test-manager
git add internal/settings/settings.go internal/settings/settings_test.go
git commit -m "feat(settings): persist spellcheck ignore words"
```

---

### Task 6: App bindings — ListMisspellings, ApplyCorrection, AddIgnoreWord

**Files:**
- Create: `app_spellcheck.go`
- Test: `app_spellcheck_test.go`

**Interfaces:**
- Consumes: `spellcheck.NewDefaultChecker`, `spellcheck.ScanTests`, `spellcheck.TestText`, `spellcheck.Finding` (Tasks 1–4); `settings.Manager.GetIgnoreWords/AddIgnoreWord` (Task 5); `testrepo.Repository.ListTests`, `.GetTest`, `.EditTestField`; `App` fields `repo`, `settings`; `recoverToError`, `requireStore`.
- Produces:
  - `func (a *App) ListMisspellings(profileID string) ([]spellcheck.Finding, error)`
  - `func (a *App) ApplyCorrection(profileID, testKey, field, word string, offset, length int, replacement string) error`
  - `func (a *App) AddIgnoreWord(word string) error`

- [ ] **Step 1: Write the failing test**

Create `app_spellcheck_test.go` in the repo root (package `main`). It seeds two tests with planted typos via the real `CreateTest` path, scans, applies a correction, and ignores a word:

```go
package main

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/settings"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/testrepo"
)

func newSpellApp(t *testing.T) *App {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "spell.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a := NewApp()
	a.store = st
	a.repo = testrepo.NewRepository(st)
	a.settings = settings.NewManager(st)
	return a
}

func TestListMisspellingsFindsPlantedTypos(t *testing.T) {
	a := newSpellApp(t)
	if _, err := a.repo.CreateTest("p1", testrepo.TestDraft{
		Summary:     "User can recieve a token",
		Description: "The system must authenticate the user",
	}); err != nil {
		t.Fatalf("CreateTest 1: %v", err)
	}
	if _, err := a.repo.CreateTest("p1", testrepo.TestDraft{
		Summary: "Clean title with no typos here",
	}); err != nil {
		t.Fatalf("CreateTest 2: %v", err)
	}

	findings, err := a.ListMisspellings("p1")
	if err != nil {
		t.Fatalf("ListMisspellings: %v", err)
	}
	var found bool
	for _, f := range findings {
		if f.Word == "recieve" && f.Field == "summary" {
			found = true
		}
		if f.Word == "typos" {
			t.Errorf("'typos' should be a real word, not flagged")
		}
	}
	if !found {
		t.Fatalf("did not flag 'recieve'; findings = %+v", findings)
	}
}

func TestApplyCorrectionSplicesAndQueues(t *testing.T) {
	a := newSpellApp(t)
	key, err := a.repo.CreateTest("p1", testrepo.TestDraft{Summary: "User can recieve a token"})
	if err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	// Locate the finding to get its exact offset/length.
	findings, err := a.ListMisspellings("p1")
	if err != nil {
		t.Fatalf("ListMisspellings: %v", err)
	}
	var target *struct {
		off, length int
		word, field string
	}
	for _, f := range findings {
		if f.TestKey == key && f.Word == "recieve" {
			target = &struct {
				off, length int
				word, field string
			}{f.Offset, f.Length, f.Word, f.Field}
		}
	}
	if target == nil {
		t.Fatalf("no 'recieve' finding for %s", key)
	}
	if err := a.ApplyCorrection("p1", key, target.field, target.word, target.off, target.length, "receive"); err != nil {
		t.Fatalf("ApplyCorrection: %v", err)
	}
	tc, err := a.repo.GetTest("p1", key)
	if err != nil {
		t.Fatalf("GetTest: %v", err)
	}
	if tc.Summary != "User can receive a token" {
		t.Errorf("summary = %q, want corrected", tc.Summary)
	}
	// A stale offset (wrong word) must be rejected, not written.
	if err := a.ApplyCorrection("p1", key, "summary", "recieve", target.off, target.length, "receive"); err == nil {
		t.Errorf("stale correction was accepted; want error")
	}
}

func TestAddIgnoreWordSuppressesFinding(t *testing.T) {
	a := newSpellApp(t)
	if _, err := a.repo.CreateTest("p1", testrepo.TestDraft{Summary: "Check the euicc profile"}); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	// "euicc" is already in the domain allow-list, so pick a word that is not.
	if _, err := a.repo.CreateTest("p1", testrepo.TestDraft{Summary: "Check the frobnicator profile"}); err != nil {
		t.Fatalf("CreateTest 2: %v", err)
	}
	before, _ := a.ListMisspellings("p1")
	var hadFrob bool
	for _, f := range before {
		if f.Word == "frobnicator" {
			hadFrob = true
		}
	}
	if !hadFrob {
		t.Skip("sample word unexpectedly in dictionary; pick another in review")
	}
	if err := a.AddIgnoreWord("frobnicator"); err != nil {
		t.Fatalf("AddIgnoreWord: %v", err)
	}
	after, _ := a.ListMisspellings("p1")
	for _, f := range after {
		if f.Word == "frobnicator" {
			t.Errorf("ignored word still flagged")
		}
	}
}
```

The helper wires `App` directly (mirroring `initStore`) so the test needs no GUI/Wails context. The import list is exactly `path/filepath`, `testing`, `xray-test-manager/internal/settings`, `xray-test-manager/internal/store`, `xray-test-manager/internal/testrepo`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /c/projects/xray-test-manager && go test . -run 'TestListMisspellings|TestApplyCorrection|TestAddIgnoreWord' -v`
Expected: FAIL — `a.ListMisspellings undefined` (and the other two).

- [ ] **Step 3: Write minimal implementation**

Create `app_spellcheck.go` in the repo root:

```go
package main

import (
	"fmt"

	"xray-test-manager/internal/spellcheck"
	"xray-test-manager/internal/testrepo"
)

// ListMisspellings scans every synced test in the profile for spelling errors
// across summary, description, and the Cucumber/Generic bodies. It reads only
// the local store (works fully offline) and folds the user's ignore list into
// the checker.
func (a *App) ListMisspellings(profileID string) (findings []spellcheck.Finding, err error) {
	defer recoverToError("ListMisspellings", &err)
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	ignore, err := a.settings.GetIgnoreWords()
	if err != nil {
		return nil, err
	}
	checker := spellcheck.NewDefaultChecker(ignore)

	var texts []spellcheck.TestText
	offset := 0
	for {
		page, err := a.repo.ListTests(profileID, testrepo.Query{Limit: 500, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, tc := range page.Tests {
			texts = append(texts, spellcheck.TestText{
				Key:               tc.Key,
				Summary:           tc.Summary,
				Description:       tc.Description,
				CucumberScenario:  tc.CucumberScenario,
				GenericDefinition: tc.GenericDefinition,
			})
		}
		offset += len(page.Tests)
		if len(page.Tests) == 0 || offset >= page.Total {
			break
		}
	}
	return spellcheck.ScanTests(texts, checker), nil
}

// ApplyCorrection replaces the flagged word at [offset,offset+length) in the
// given field with replacement, then routes the edit through the existing
// pending-change pipeline. The word argument is validated against the current
// text so a stale finding (edited since the scan) is rejected rather than
// corrupting the field.
func (a *App) ApplyCorrection(profileID, testKey, field, word string, offset, length int, replacement string) (err error) {
	defer recoverToError("ApplyCorrection", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	tc, err := a.repo.GetTest(profileID, testKey)
	if err != nil {
		return err
	}
	var cur string
	switch field {
	case "summary":
		cur = tc.Summary
	case "description":
		cur = tc.Description
	case "cucumber_scenario":
		cur = tc.CucumberScenario
	case "generic_definition":
		cur = tc.GenericDefinition
	default:
		return fmt.Errorf("field %q is not spellcheck-correctable", field)
	}
	if offset < 0 || length <= 0 || offset+length > len(cur) || cur[offset:offset+length] != word {
		return fmt.Errorf("correction is stale — please re-scan")
	}
	newValue := cur[:offset] + replacement + cur[offset+length:]
	return a.repo.EditTestField(profileID, testKey, field, newValue)
}

// AddIgnoreWord adds a word to the global spellcheck ignore list so future
// scans skip it across all profiles.
func (a *App) AddIgnoreWord(word string) (err error) {
	defer recoverToError("AddIgnoreWord", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.settings.AddIgnoreWord(word)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /c/projects/xray-test-manager && go test . -run 'TestListMisspellings|TestApplyCorrection|TestAddIgnoreWord' -v`
Expected: PASS.

Verify format: `tr -d '\r' < app_spellcheck.go | gofmt -d` → empty.

- [ ] **Step 5: Commit**

```bash
cd /c/projects/xray-test-manager
git add app_spellcheck.go app_spellcheck_test.go
git commit -m "feat(app): ListMisspellings, ApplyCorrection, AddIgnoreWord bindings"
```

---

### Task 7: Wails bindings + frontend view

**Files:**
- Regenerate: `frontend/wailsjs/go/main/App.js`, `App.d.ts`, `frontend/wailsjs/go/models.ts`
- Modify: `frontend/src/api.ts`
- Create: `frontend/src/components/MisspellingsView.tsx`
- Modify: `frontend/src/App.tsx`

**Interfaces:**
- Consumes: `App.ListMisspellings`, `App.ApplyCorrection`, `App.AddIgnoreWord` (Task 6); the existing `errMsg` in `api.ts`.
- Produces: a `"misspellings"` view reachable from the top-nav.

- [ ] **Step 1: Regenerate Wails bindings**

Run from the repo root:

```bash
cd /c/projects/xray-test-manager && "$HOME/go/bin/wails" generate module
```

Expected: `App.js`/`App.d.ts` gain `ListMisspellings`, `ApplyCorrection`, `AddIgnoreWord`; `models.ts` gains `spellcheck.Finding`. Confirm:

```bash
grep -n "ListMisspellings\|ApplyCorrection\|AddIgnoreWord" frontend/wailsjs/go/main/App.d.ts
grep -n "class Finding" frontend/wailsjs/go/models.ts
```

- [ ] **Step 2: Re-export the bindings in api.ts**

In `frontend/src/api.ts`, add the three function names to the big `export { ... } from "../wailsjs/go/main/App"` re-export block (alongside e.g. `ListPreconditionsWithUsage`):

```ts
  ListMisspellings,
  ApplyCorrection,
  AddIgnoreWord,
```

- [ ] **Step 3: Create the view component**

Create `frontend/src/components/MisspellingsView.tsx`:

```tsx
import { useState } from "react";
import { ListMisspellings, ApplyCorrection, AddIgnoreWord, errMsg } from "../api";

interface Finding {
  testKey: string;
  field: string;
  word: string;
  snippet: string;
  offset: number;
  length: number;
  suggestions: string[];
}

interface Props {
  profileId: string;
  refreshKey: number;
  onChanged: () => void;
}

const FIELD_LABEL: Record<string, string> = {
  summary: "Summary",
  description: "Description",
  cucumber_scenario: "Gherkin",
  generic_definition: "Definition",
};

export default function MisspellingsView({ profileId, onChanged }: Props) {
  const [findings, setFindings] = useState<Finding[]>([]);
  const [scanned, setScanned] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function scan() {
    if (!profileId) return;
    setLoading(true);
    setError("");
    try {
      const result = (await ListMisspellings(profileId)) as unknown as Finding[];
      setFindings(result ?? []);
      setScanned(true);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setLoading(false);
    }
  }

  async function apply(f: Finding, replacement: string) {
    try {
      await ApplyCorrection(profileId, f.testKey, f.field, f.word, f.offset, f.length, replacement);
      setFindings((prev) => prev.filter((x) => x !== f));
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function ignore(f: Finding) {
    try {
      await AddIgnoreWord(f.word);
      const lower = f.word.toLowerCase();
      setFindings((prev) => prev.filter((x) => x.word.toLowerCase() !== lower));
    } catch (e) {
      setError(errMsg(e));
    }
  }

  return (
    <div className="misspellings-view">
      <div className="toolbar">
        <button onClick={scan} disabled={loading || !profileId}>
          {loading ? "Scanning…" : "Scan for typos"}
        </button>
        {scanned && !loading && (
          <span className="muted">
            {findings.length} {findings.length === 1 ? "issue" : "issues"} found
          </span>
        )}
      </div>

      {error && <div className="error">{error}</div>}

      {scanned && findings.length === 0 && !loading && !error && (
        <p className="muted">No spelling issues found.</p>
      )}

      {findings.length > 0 && (
        <table className="board-table">
          <thead>
            <tr>
              <th>Test</th>
              <th>Field</th>
              <th>Word</th>
              <th>Context</th>
              <th>Suggestions</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {findings.map((f, i) => (
              <tr key={`${f.testKey}-${f.field}-${f.offset}-${i}`}>
                <td>{f.testKey}</td>
                <td>{FIELD_LABEL[f.field] ?? f.field}</td>
                <td className="typo-word">{f.word}</td>
                <td className="typo-snippet">{f.snippet}</td>
                <td>
                  {f.suggestions.length === 0 && <span className="muted">—</span>}
                  {f.suggestions.map((s) => (
                    <button key={s} className="suggestion-chip" onClick={() => apply(f, s)}>
                      {s}
                    </button>
                  ))}
                </td>
                <td>
                  <button className="link-btn" onClick={() => ignore(f)}>
                    Ignore
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Wire the view into App.tsx**

Make four edits in `frontend/src/App.tsx` (locate by pattern, not fixed line numbers):

1. Import near the other view-component imports:
```tsx
import MisspellingsView from "./components/MisspellingsView";
```
2. Add `"misspellings"` to the `view` state union. Find the `useState<...>("browse")` declaration and add the literal:
```tsx
  // ...existing union members... | "coverage" | "misspellings"
```
3. Add a nav tab button inside the `<nav className="view-tabs ...">` block, mirroring an existing tab:
```tsx
          <button
            className={view === "misspellings" ? "view-tab-active" : ""}
            onClick={() => setView("misspellings")}
          >
            Misspellings
          </button>
```
4. Add a render branch alongside the other `view === "..."` branches:
```tsx
        {view === "misspellings" && (
          <main className="content">
            <MisspellingsView profileId={activeId} refreshKey={refreshKey} onChanged={onChanged} />
          </main>
        )}
```
Use the exact `refreshKey`/`onChanged` names already passed to sibling views (e.g. `PreconditionsView`) in that file — match them verbatim.

- [ ] **Step 5: Verify the frontend compiles and builds**

Run:

```bash
cd /c/projects/xray-test-manager/frontend && npx tsc --noEmit && npm run build
```

Expected: no type errors; build succeeds. (If `errMsg`, `refreshKey`, or `onChanged` names differ from those used here, adjust to match the real ones in `api.ts` / `App.tsx`.)

- [ ] **Step 6: Commit**

```bash
cd /c/projects/xray-test-manager
git add frontend/wailsjs frontend/src/api.ts frontend/src/components/MisspellingsView.tsx frontend/src/App.tsx
git commit -m "feat(frontend): Misspellings view with scan, apply, and ignore"
```

(Do NOT `git add` `frontend/package-lock.json` or `frontend/package.json.md5` if the build touched them.)

---

### Task 8: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Backend build, vet, and full test suite**

```bash
cd /c/projects/xray-test-manager
go build ./...
go vet ./internal/spellcheck/ ./internal/settings/ .
go test ./... -count=1
```

Expected: build clean; vet clean; all packages PASS (including `internal/spellcheck`, `internal/settings`, and root `.`).

- [ ] **Step 2: Confirm no stray build churn is staged**

```bash
cd /c/projects/xray-test-manager && git status --short
```

Expected: clean working tree. If `go.mod`/`go.sum`/`package-lock.json`/`package.json.md5` show as modified with no real dependency change, restore them: `git checkout -- go.mod go.sum frontend/package-lock.json frontend/package.json.md5`.

- [ ] **Step 3: Demo-mode manual smoke (optional but recommended)**

Have the user (or a dev terminal) run `wails dev`, create a profile with Jira URL `demo`, Sync, open the **Misspellings** tab, click **Scan for typos**, confirm findings render with suggestion chips, click a suggestion (verify the finding disappears and a pending change is queued), and click **Ignore** on another (verify it disappears and stays gone on re-scan).

- [ ] **Step 4: Final commit (only if Step 2 required restoring files)**

No code changes expected here; this step is a checkpoint, not a commit.

---

## Self-Review

**Spec coverage:**
- Report + inline fix → Task 7 (view with Apply) + Task 6 (`ApplyCorrection`). ✓
- Field scope (summary/description/cucumber/generic) → Task 3 `ScanTests` + Task 6 field switch. ✓
- Go engine + embedded wordlist → Tasks 1–4. ✓
- 3-layer noise control: heuristics (Task 1 `skip`), domain allow-list (Task 4), user ignore (Task 5 + Task 6). ✓
- Server-side Apply via `EditTestField` → Task 6. ✓
- Global ignore persistence → Task 5. ✓
- On-demand scan button → Task 7 view. ✓
- Error handling: `requireStore`/`recoverToError` (Task 6), stale-offset guard (Task 6), empty result not error (Task 6 loop). ✓
- Testing: pure checker (Tasks 1–2), ScanTests (Task 3), default checker (Task 4), settings (Task 5), app integration (Task 6). ✓
- Non-goals (steps/custom-fields/grammar) → not implemented, as intended. ✓

**Placeholder scan:** No TBD/TODO/"implement later" remain. Every code step shows complete code; every field-name and signature is spelled out. The one download step (Task 4 wordlist) names an exact URL and a documented fallback.

**Type consistency:** `Finding`/`TestText`/`Checker`/`NewChecker`/`NewDefaultChecker`/`ScanTests`/`CheckText` names and signatures match across Tasks 1–4 and their consumption in Task 6. `ApplyCorrection` signature (`profileID, testKey, field, word string, offset, length int, replacement string`) is identical in the Task 6 test, implementation, and the Task 7 view call. Field-name strings (`summary`/`description`/`cucumber_scenario`/`generic_definition`) match `editableFields` and the `ScanTests` field list.
