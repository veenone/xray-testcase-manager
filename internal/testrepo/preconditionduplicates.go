package testrepo

import (
	"fmt"
	"sort"
)

// PreconditionDuplicateMember is one Precondition inside a duplicate group.
type PreconditionDuplicateMember struct {
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Type        string `json:"type"`
	Condition   string `json:"condition"`
	Description string `json:"description"`
	TestCount   int    `json:"testCount"`
}

// PreconditionDuplicateGroup is a set of Preconditions (>=2) sharing a
// normalized summary, with a verdict comparing their definition text.
type PreconditionDuplicateGroup struct {
	NormalizedSummary string                        `json:"normalizedSummary"`
	DisplaySummary    string                        `json:"displaySummary"`
	DefinitionVerdict string                        `json:"definitionVerdict"` // "identical" | "differ"
	Members           []PreconditionDuplicateMember `json:"members"`
}

// PreconditionDuplicateReport is the full precondition duplicate scan result.
// It mirrors DuplicateReport, but because preconditions have no object-level
// steps there is no lazy step scan: the definition verdict is computed inline
// from the already-local condition/description text, so there is no "unscanned"
// state and no separate scan pass.
type PreconditionDuplicateReport struct {
	Groups              []PreconditionDuplicateGroup `json:"groups"`
	GroupCount          int                          `json:"groupCount"`
	PreconditionCount   int                          `json:"preconditionCount"`
	DefinitionIdentical int                          `json:"definitionIdentical"`
	DefinitionDiffer    int                          `json:"definitionDiffer"`
	Excluded            int                          `json:"excluded"`
	ScannedAt           string                       `json:"scannedAt"`
}

const (
	defVerdictIdentical = "identical"
	defVerdictDiffer    = "differ"
)

// preconditionDefinition is the comparable definition text of a precondition:
// its Xray condition plus Jira description, each normalized and joined with a
// field separator. Two preconditions with equal definitions have identical
// content.
func preconditionDefinition(condition, description string) string {
	return normalizeText(condition) + "\x1f" + normalizeText(description)
}

// precondDupCandidate is a non-ignored Precondition loaded for grouping.
type precondDupCandidate struct {
	key, summary, ptype, description, condition string
	testCount                                   int
}

// ExcludePreconditionFromDuplicates permanently ignores a Precondition in
// precondition duplicate scans (local only — never sent to Jira). Idempotent.
func (r *Repository) ExcludePreconditionFromDuplicates(profileID, preconditionKey string) error {
	if _, err := r.db.Exec(
		`INSERT INTO precondition_duplicate_ignore (profile_id, precondition_key, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(profile_id, precondition_key) DO NOTHING`,
		profileID, preconditionKey, nowStamp(),
	); err != nil {
		return fmt.Errorf("exclude precondition from duplicates: %w", err)
	}
	return nil
}

// UnexcludePreconditionFromDuplicates restores a previously-excluded Precondition.
func (r *Repository) UnexcludePreconditionFromDuplicates(profileID, preconditionKey string) error {
	if _, err := r.db.Exec(
		`DELETE FROM precondition_duplicate_ignore WHERE profile_id = ? AND precondition_key = ?`,
		profileID, preconditionKey,
	); err != nil {
		return fmt.Errorf("unexclude precondition from duplicates: %w", err)
	}
	return nil
}

// countExcludedPreconditions returns how many Preconditions are excluded.
func (r *Repository) countExcludedPreconditions(profileID string) (int, error) {
	var n int
	if err := r.db.QueryRow(
		`SELECT COUNT(*) FROM precondition_duplicate_ignore WHERE profile_id = ?`, profileID,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("count excluded preconditions: %w", err)
	}
	return n, nil
}

// loadPreconditionCandidates returns every non-ignored Precondition for a
// profile with its linked-test count.
func (r *Repository) loadPreconditionCandidates(profileID string) ([]precondDupCandidate, error) {
	rows, err := r.db.Query(
		`SELECT p.jira_key, p.summary, p.type, p.description, p.condition,
		        (SELECT COUNT(*) FROM test_precondition tp
		          WHERE tp.profile_id = p.profile_id AND tp.precondition_key = p.jira_key) AS test_count
		   FROM precondition p
		  WHERE p.profile_id = ?
		    AND p.jira_key NOT IN (
		        SELECT precondition_key FROM precondition_duplicate_ignore WHERE profile_id = ?)`,
		profileID, profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("load precondition duplicate candidates: %w", err)
	}
	defer rows.Close()
	out := []precondDupCandidate{}
	for rows.Next() {
		var c precondDupCandidate
		if err := rows.Scan(&c.key, &c.summary, &c.ptype, &c.description, &c.condition, &c.testCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// groupPreconditionCandidates buckets candidates by normalized summary, keeping
// only buckets with >=2 members; each bucket's members are sorted by key for
// determinism. Mirrors groupCandidates for Tests.
func groupPreconditionCandidates(cands []precondDupCandidate) map[string][]precondDupCandidate {
	byNorm := map[string][]precondDupCandidate{}
	for _, c := range cands {
		n := normalizeText(c.summary)
		byNorm[n] = append(byNorm[n], c)
	}
	for n, members := range byNorm {
		if len(members) < 2 {
			delete(byNorm, n)
			continue
		}
		sort.Slice(members, func(i, j int) bool { return members[i].key < members[j].key })
		byNorm[n] = members
	}
	return byNorm
}

// preconditionDefinitionVerdict reports whether all members share identical
// definition text ("identical") or not ("differ"). Unlike Tests there is no
// "unscanned" state: the definition is always locally available.
func preconditionDefinitionVerdict(members []precondDupCandidate) string {
	first, haveFirst := "", false
	for _, m := range members {
		def := preconditionDefinition(m.condition, m.description)
		if !haveFirst {
			first, haveFirst = def, true
		} else if def != first {
			return defVerdictDiffer
		}
	}
	return defVerdictIdentical
}

// ScanPreconditionDuplicates computes the full precondition duplicate report
// for a profile (instant; no Jira). Preconditions are grouped by normalized
// summary (>=2 members), and each group carries a definition verdict comparing
// the members' condition/description text.
func (r *Repository) ScanPreconditionDuplicates(profileID string) (PreconditionDuplicateReport, error) {
	rep := PreconditionDuplicateReport{Groups: []PreconditionDuplicateGroup{}}
	cands, err := r.loadPreconditionCandidates(profileID)
	if err != nil {
		return rep, err
	}
	groups := groupPreconditionCandidates(cands)

	norms := make([]string, 0, len(groups))
	for n := range groups {
		norms = append(norms, n)
	}
	sort.Strings(norms)

	for _, n := range norms {
		members := groups[n]
		verdict := preconditionDefinitionVerdict(members)
		g := PreconditionDuplicateGroup{
			NormalizedSummary: n,
			DisplaySummary:    members[0].summary, // members are key-sorted → deterministic
			DefinitionVerdict: verdict,
			Members:           make([]PreconditionDuplicateMember, len(members)),
		}
		for i, m := range members {
			g.Members[i] = PreconditionDuplicateMember{
				Key:         m.key,
				Summary:     m.summary,
				Type:        m.ptype,
				Condition:   m.condition,
				Description: m.description,
				TestCount:   m.testCount,
			}
		}
		rep.Groups = append(rep.Groups, g)
		rep.PreconditionCount += len(g.Members)
		if verdict == defVerdictIdentical {
			rep.DefinitionIdentical++
		} else {
			rep.DefinitionDiffer++
		}
	}
	rep.GroupCount = len(rep.Groups)
	rep.ScannedAt = nowStamp()
	if rep.Excluded, err = r.countExcludedPreconditions(profileID); err != nil {
		return rep, err
	}
	return rep, nil
}
