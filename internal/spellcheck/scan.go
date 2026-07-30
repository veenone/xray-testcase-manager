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
