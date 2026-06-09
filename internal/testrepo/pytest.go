package testrepo

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// pyMember is one container Test gathered for scaffold generation.
type pyMember struct {
	Key     string
	Summary string
	Steps   []Step
}

// GeneratePytest builds a Python test scaffold from a Test Set / Plan /
// Execution (FR-7.2): one test per member Test, named from its key, with the
// Test's summary and steps in the docstring and the Xray key carried through so
// the file links back to Xray (FR-7.6). Step bodies come from the cached steps —
// open or refresh a Test to populate them.
//
// style selects the output shape:
//   - "" / "function" — plain pytest, one @pytest.mark.xray test function each.
//   - "unittest"       — a unittest.TestCase subclass, one test method each
//     (runs under both `pytest` and `python -m unittest`).
func (r *Repository) GeneratePytest(profileID, containerKey, style string) (string, error) {
	var kind, summary string
	err := r.db.QueryRow(
		`SELECT kind, summary FROM test_container WHERE profile_id = ? AND jira_key = ?`,
		profileID, containerKey,
	).Scan(&kind, &summary)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("container %s not found", containerKey)
	}
	if err != nil {
		return "", fmt.Errorf("read container: %w", err)
	}

	rows, err := r.db.Query(
		`SELECT t.jira_key, t.summary
		 FROM test_container_test l
		 JOIN test_case t ON t.profile_id = l.profile_id AND t.jira_key = l.test_key
		 WHERE l.profile_id = ? AND l.container_key = ?
		 ORDER BY t.jira_key`,
		profileID, containerKey)
	if err != nil {
		return "", fmt.Errorf("read members: %w", err)
	}
	defer rows.Close()

	members := []pyMember{}
	for rows.Next() {
		var m pyMember
		if err := rows.Scan(&m.Key, &m.Summary); err != nil {
			return "", err
		}
		steps, err := r.ListTestSteps(profileID, m.Key)
		if err != nil {
			return "", err
		}
		m.Steps = steps
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	if style == "unittest" {
		return renderUnittest(containerKey, summary, kind, members), nil
	}
	return renderPytestFunctions(containerKey, summary, kind, members), nil
}

// renderPytestFunctions writes the plain-pytest scaffold: one marked function
// per Test.
func renderPytestFunctions(containerKey, summary, kind string, members []pyMember) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\"\"\"pytest scaffold generated from %s — %s.\n\n", containerKey, summary)
	fmt.Fprintf(&b, "One test per Xray Test in this %s. Fill in the bodies and run with pytest.\n\"\"\"\n",
		containerLabel(kind))
	b.WriteString("import pytest\n\n")

	used := map[string]int{}
	for _, m := range members {
		fn := uniquePyName(used, m.Key)
		fmt.Fprintf(&b, "\n@pytest.mark.xray(%q)\n", m.Key)
		fmt.Fprintf(&b, "def %s():\n", fn)
		b.WriteString(pyDocstring(m.Summary, m.Steps, "    "))
		b.WriteString("    pytest.skip(\"scaffold — implement me\")\n")
	}
	if len(members) == 0 {
		b.WriteString("\n# (no tests in this container yet)\n")
	}
	return b.String()
}

// renderUnittest writes a unittest.TestCase subclass: one test method per Test,
// each carrying its Xray key as a method attribute for traceability. The file
// runs under pytest and `python -m unittest` alike.
func renderUnittest(containerKey, summary, kind string, members []pyMember) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\"\"\"unittest scaffold generated from %s — %s.\n\n", containerKey, summary)
	fmt.Fprintf(&b, "One test method per Xray Test in this %s. Fill in the bodies and run with\n",
		containerLabel(kind))
	b.WriteString("`python -m unittest` or pytest.\n\"\"\"\n")
	b.WriteString("import unittest\n\n\n")
	fmt.Fprintf(&b, "class %s(unittest.TestCase):\n", pyClassName(summary, containerKey))

	used := map[string]int{}
	for _, m := range members {
		fn := uniquePyName(used, m.Key)
		fmt.Fprintf(&b, "\n    def %s(self):\n", fn)
		b.WriteString(pyDocstring(m.Summary, m.Steps, "        "))
		fmt.Fprintf(&b, "        self.xray_key = %q\n", m.Key)
		b.WriteString("        self.skipTest(\"scaffold — implement me\")\n")
	}
	if len(members) == 0 {
		b.WriteString("\n    pass  # (no tests in this container yet)\n")
	}
	b.WriteString("\n\nif __name__ == \"__main__\":\n    unittest.main()\n")
	return b.String()
}

// pyDocstring renders a Test's summary + steps as an indented docstring block.
func pyDocstring(summary string, steps []Step, indent string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\"\"\"%s\n", indent, pyDocLine(summary))
	if len(steps) > 0 {
		fmt.Fprintf(&b, "\n%sSteps:\n", indent)
		for i, s := range steps {
			fmt.Fprintf(&b, "%s%d. %s\n", indent, i+1, pyDocLine(s.Action))
			if strings.TrimSpace(s.Data) != "" {
				fmt.Fprintf(&b, "%s   Data: %s\n", indent, pyDocLine(s.Data))
			}
			if strings.TrimSpace(s.Expected) != "" {
				fmt.Fprintf(&b, "%s   Expected: %s\n", indent, pyDocLine(s.Expected))
			}
		}
	}
	fmt.Fprintf(&b, "%s\"\"\"\n", indent)
	return b.String()
}

// uniquePyName builds a valid, unique Python function name from a Test key.
func uniquePyName(used map[string]int, key string) string {
	var sb strings.Builder
	sb.WriteString("test_")
	for _, r := range strings.ToLower(key) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	base := sb.String()
	used[base]++
	if n := used[base]; n > 1 {
		return fmt.Sprintf("%s_%d", base, n)
	}
	return base
}

// pyClassName builds a valid, CamelCase-ish Python class name from a container
// summary (falling back to its key), prefixed with "Test".
func pyClassName(summary, key string) string {
	src := strings.TrimSpace(summary)
	if src == "" {
		src = key
	}
	var sb strings.Builder
	sb.WriteString("Test")
	upper := true
	for _, r := range src {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			if upper {
				sb.WriteRune(upperRune(r))
				upper = false
			} else {
				sb.WriteRune(r)
			}
		case r >= '0' && r <= '9':
			sb.WriteRune(r)
		default:
			upper = true // word boundary
		}
	}
	out := sb.String()
	if out == "Test" {
		return "TestScaffold"
	}
	return out
}

func upperRune(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}

// pyDocLine collapses a value to a single docstring-safe line.
func pyDocLine(s string) string {
	s = strings.ReplaceAll(s, "\"\"\"", "'''")
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func containerLabel(kind string) string {
	switch kind {
	case "testset":
		return "Test Set"
	case "testplan":
		return "Test Plan"
	case "testexec":
		return "Test Execution"
	}
	return "container"
}
