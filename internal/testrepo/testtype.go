package testrepo

import (
	"fmt"
	"strings"
)

// Conversion between the three test-type bodies. All transforms are
// best-effort and lossy by nature: they exist to pre-fill an EMPTY target body
// when a test's type changes, giving the user a reviewable starting point. The
// source body is never modified by these pure functions.

var gherkinKeywords = []string{"Given ", "When ", "Then ", "And ", "But ", "* "}

// StepsToGherkin renders manual steps as a Gherkin scenario skeleton.
func StepsToGherkin(summary string, steps []Step, scenarioType string) string {
	if scenarioType == "" {
		scenarioType = "Scenario"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# generated from %d manual steps — review before commit\n", len(steps))
	fmt.Fprintf(&b, "%s: %s\n", scenarioType, strings.TrimSpace(summary))
	for _, s := range steps {
		if a := strings.TrimSpace(s.Action); a != "" {
			fmt.Fprintf(&b, "  When %s\n", a)
		}
		if d := strings.TrimSpace(s.Data); d != "" {
			fmt.Fprintf(&b, "  And %s\n", d)
		}
		if e := strings.TrimSpace(s.Expected); e != "" {
			fmt.Fprintf(&b, "  Then %s\n", e)
		}
	}
	return b.String()
}

// StepsToDefinition flattens manual steps to a numbered plain-text definition.
func StepsToDefinition(steps []Step) string {
	var b strings.Builder
	for i, s := range steps {
		fmt.Fprintf(&b, "%d. %s", i+1, strings.TrimSpace(s.Action))
		if d := strings.TrimSpace(s.Data); d != "" {
			fmt.Fprintf(&b, " — Data: %s", d)
		}
		if e := strings.TrimSpace(s.Expected); e != "" {
			fmt.Fprintf(&b, " — Expected: %s", e)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// GherkinToSteps parses scenario lines into manual steps: each Given/When/Then/
// And/But line becomes a step action (keyword stripped). Headers, comments, and
// blanks are skipped.
func GherkinToSteps(scenario string) []Step {
	var steps []Step
	for _, raw := range strings.Split(scenario, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "Scenario") || strings.HasPrefix(line, "Feature") ||
			strings.HasPrefix(line, "Background") || strings.HasPrefix(line, "Examples") {
			continue
		}
		for _, kw := range gherkinKeywords {
			if strings.HasPrefix(line, kw) {
				line = strings.TrimSpace(strings.TrimPrefix(line, kw))
				break
			}
		}
		steps = append(steps, Step{Action: line})
	}
	return steps
}

// GherkinToDefinition uses the raw scenario text as the generic definition.
func GherkinToDefinition(scenario string) string {
	return strings.TrimSpace(scenario)
}

// DefinitionToSteps turns each non-blank line of a definition into a step action.
func DefinitionToSteps(definition string) []Step {
	var steps []Step
	for _, raw := range strings.Split(definition, "\n") {
		if line := strings.TrimSpace(raw); line != "" {
			steps = append(steps, Step{Action: line})
		}
	}
	return steps
}

// DefinitionToGherkin wraps a definition as a scenario with the definition as a
// Given line.
func DefinitionToGherkin(summary, definition string) string {
	return fmt.Sprintf("Scenario: %s\n  Given %s\n",
		strings.TrimSpace(summary), strings.TrimSpace(definition))
}
