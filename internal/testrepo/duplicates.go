package testrepo

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DuplicateMember is one Test inside a duplicate group.
type DuplicateMember struct {
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Status      string `json:"status"`
	FolderID    string `json:"folderId"`
}

// DuplicateGroup is a set of Tests (>=2) sharing a normalized summary, with a
// step-comparison verdict for the group.
type DuplicateGroup struct {
	NormalizedSummary string            `json:"normalizedSummary"`
	DisplaySummary    string            `json:"displaySummary"`
	StepsVerdict      string            `json:"stepsVerdict"` // "identical" | "differ" | "unscanned"
	Members           []DuplicateMember `json:"members"`
}

// DuplicateReport is the full duplicate scan result (FR — duplicate management).
type DuplicateReport struct {
	Groups         []DuplicateGroup `json:"groups"`
	GroupCount     int              `json:"groupCount"`
	TestCount      int              `json:"testCount"`
	StepsIdentical int              `json:"stepsIdentical"`
	StepsDiffer    int              `json:"stepsDiffer"`
	StepsUnscanned int              `json:"stepsUnscanned"`
	Excluded       int              `json:"excluded"`
	ScannedAt      string           `json:"scannedAt"`
}

const (
	stepVerdictIdentical = "identical"
	stepVerdictDiffer    = "differ"
	stepVerdictUnscanned = "unscanned"
)

// normalizeText lowercases, trims, and collapses internal whitespace to single
// spaces — the canonical form for comparing summaries and step text.
func normalizeText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// StepFingerprint is a stable string key for a Test's ordered steps after
// normalizing each field. Two Tests with equal fingerprints have identical step
// content; a Test with no steps fingerprints to "".
func StepFingerprint(steps []Step) string {
	parts := make([]string, len(steps))
	for i, s := range steps {
		parts[i] = normalizeText(s.Action) + "\x1f" + normalizeText(s.Data) + "\x1f" + normalizeText(s.Expected)
	}
	return strings.Join(parts, "\x1e")
}

// nowStamp is the timestamp format used for duplicate bookkeeping rows.
func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

// ExcludeFromDuplicates permanently ignores a Test in duplicate scans (local
// only — never sent to Jira). Idempotent.
func (r *Repository) ExcludeFromDuplicates(profileID, testKey string) error {
	if _, err := r.db.Exec(
		`INSERT INTO duplicate_ignore (profile_id, test_key, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(profile_id, test_key) DO NOTHING`,
		profileID, testKey, nowStamp(),
	); err != nil {
		return fmt.Errorf("exclude from duplicates: %w", err)
	}
	return nil
}

// UnexcludeFromDuplicates restores a previously-excluded Test.
func (r *Repository) UnexcludeFromDuplicates(profileID, testKey string) error {
	if _, err := r.db.Exec(
		`DELETE FROM duplicate_ignore WHERE profile_id = ? AND test_key = ?`,
		profileID, testKey,
	); err != nil {
		return fmt.Errorf("unexclude from duplicates: %w", err)
	}
	return nil
}

// RecordStepScan stores the step fingerprint for a Test so a duplicate scan can
// give a steps verdict and distinguish "scanned, no steps" from "not scanned".
func (r *Repository) RecordStepScan(profileID, testKey string, steps []Step) error {
	if _, err := r.db.Exec(
		`INSERT INTO duplicate_step_scan (profile_id, test_key, fingerprint, scanned_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(profile_id, test_key)
		 DO UPDATE SET fingerprint = excluded.fingerprint, scanned_at = excluded.scanned_at`,
		profileID, testKey, StepFingerprint(steps), nowStamp(),
	); err != nil {
		return fmt.Errorf("record step scan: %w", err)
	}
	return nil
}

// countExcluded returns how many Tests are excluded for a profile.
func (r *Repository) countExcluded(profileID string) (int, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM duplicate_ignore WHERE profile_id = ?`, profileID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count excluded: %w", err)
	}
	return n, nil
}

// stepScans returns the fingerprint per scanned Test plus the most recent scan
// timestamp for the profile.
func (r *Repository) stepScans(profileID string) (map[string]string, string, error) {
	rows, err := r.db.Query(
		`SELECT test_key, fingerprint, scanned_at FROM duplicate_step_scan WHERE profile_id = ?`,
		profileID,
	)
	if err != nil {
		return nil, "", fmt.Errorf("read step scans: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	latest := ""
	for rows.Next() {
		var key, fp, at string
		if err := rows.Scan(&key, &fp, &at); err != nil {
			return nil, "", err
		}
		out[key] = fp
		if at > latest {
			latest = at
		}
	}
	return out, latest, rows.Err()
}

// dupCandidate is a non-ignored Test loaded for grouping.
type dupCandidate struct {
	key, summary, description, status, folder string
}

// loadCandidates returns every non-ignored Test for a profile, optionally only
// those whose normalized summary equals `onlyNorm` (empty = all).
func (r *Repository) loadCandidates(profileID, onlyNorm string) ([]dupCandidate, error) {
	rows, err := r.db.Query(
		`SELECT jira_key, summary, description, status, folder_id FROM test_case
		 WHERE profile_id = ?
		   AND jira_key NOT IN (SELECT test_key FROM duplicate_ignore WHERE profile_id = ?)`,
		profileID, profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("load duplicate candidates: %w", err)
	}
	defer rows.Close()
	out := []dupCandidate{}
	for rows.Next() {
		var c dupCandidate
		if err := rows.Scan(&c.key, &c.summary, &c.description, &c.status, &c.folder); err != nil {
			return nil, err
		}
		if onlyNorm == "" || normalizeText(c.summary) == onlyNorm {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// groupCandidates buckets candidates by normalized summary, keeping only buckets
// with >=2 members; each bucket's members are sorted by key for determinism.
func groupCandidates(cands []dupCandidate) map[string][]dupCandidate {
	byNorm := map[string][]dupCandidate{}
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

// verdictFor computes a group's steps verdict from the scanned-fingerprint map.
func verdictFor(members []dupCandidate, fps map[string]string) string {
	first, haveFirst, allSame := "", false, true
	for _, m := range members {
		fp, ok := fps[m.key]
		if !ok {
			return stepVerdictUnscanned
		}
		if !haveFirst {
			first, haveFirst = fp, true
		} else if fp != first {
			allSame = false
		}
	}
	if allSame {
		return stepVerdictIdentical
	}
	return stepVerdictDiffer
}

// buildGroup assembles a DuplicateGroup from sorted members + verdict.
func buildGroup(norm string, members []dupCandidate, fps map[string]string) DuplicateGroup {
	g := DuplicateGroup{
		NormalizedSummary: norm,
		DisplaySummary:    members[0].summary, // members are key-sorted → deterministic
		StepsVerdict:      verdictFor(members, fps),
		Members:           make([]DuplicateMember, len(members)),
	}
	for i, m := range members {
		g.Members[i] = DuplicateMember{Key: m.key, Summary: m.summary, Description: m.description, Status: m.status, FolderID: m.folder}
	}
	return g
}

// ScanDuplicates computes the full duplicate report for a profile (instant; no
// Jira). Step verdicts come from previously recorded scans.
func (r *Repository) ScanDuplicates(profileID string) (DuplicateReport, error) {
	rep := DuplicateReport{Groups: []DuplicateGroup{}}
	cands, err := r.loadCandidates(profileID, "")
	if err != nil {
		return rep, err
	}
	fps, scannedAt, err := r.stepScans(profileID)
	if err != nil {
		return rep, err
	}
	groups := groupCandidates(cands)

	norms := make([]string, 0, len(groups))
	for n := range groups {
		norms = append(norms, n)
	}
	sort.Strings(norms)

	for _, n := range norms {
		g := buildGroup(n, groups[n], fps)
		rep.Groups = append(rep.Groups, g)
		rep.TestCount += len(g.Members)
		switch g.StepsVerdict {
		case stepVerdictIdentical:
			rep.StepsIdentical++
		case stepVerdictDiffer:
			rep.StepsDiffer++
		default:
			rep.StepsUnscanned++
		}
	}
	rep.GroupCount = len(rep.Groups)
	rep.ScannedAt = scannedAt
	if rep.Excluded, err = r.countExcluded(profileID); err != nil {
		return rep, err
	}
	return rep, nil
}

// ScanDuplicateGroup recomputes one group (by normalized summary). Returns an
// empty group (NormalizedSummary == "") if fewer than 2 members remain.
func (r *Repository) ScanDuplicateGroup(profileID, normalizedSummary string) (DuplicateGroup, error) {
	cands, err := r.loadCandidates(profileID, normalizedSummary)
	if err != nil {
		return DuplicateGroup{}, err
	}
	if len(cands) < 2 {
		return DuplicateGroup{}, nil
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].key < cands[j].key })
	fps, _, err := r.stepScans(profileID)
	if err != nil {
		return DuplicateGroup{}, err
	}
	return buildGroup(normalizedSummary, cands, fps), nil
}

// ScanAllDuplicateSteps walks every duplicate group whose steps verdict is still
// unscanned, fetches each member's steps via fetch, and records the fingerprint
// so the group gains an identical/differ verdict. fetch force-pulls fresh steps
// (matching the per-group scan) because steps load lazily: a member that was
// never opened has an empty local cache, which would otherwise fingerprint to ""
// and wrongly report the group as identical. progress(done, total) is invoked
// after each scanned group (total is the count of unscanned groups). It returns
// the number of groups scanned. A member whose fetch fails is skipped best-effort
// (like the per-group action), and per-group errors are swallowed so one bad
// group does not abort the whole run.
func (r *Repository) ScanAllDuplicateSteps(profileID string, fetch func(key string) ([]Step, error), progress func(done, total int)) (int, error) {
	// Load every candidate once and group in memory; loadCandidates per group
	// would re-query the whole candidate set for each group (N+1).
	cands, err := r.loadCandidates(profileID, "")
	if err != nil {
		return 0, err
	}
	groups := groupCandidates(cands)
	fps, _, err := r.stepScans(profileID)
	if err != nil {
		return 0, err
	}

	// Filter to groups that still need a step scan, in deterministic order.
	norms := make([]string, 0, len(groups))
	for n := range groups {
		norms = append(norms, n)
	}
	sort.Strings(norms)
	todo := make([]string, 0, len(norms))
	for _, n := range norms {
		// groupCandidates already drops groups with <2 members.
		if verdictFor(groups[n], fps) == stepVerdictUnscanned {
			todo = append(todo, n)
		}
	}

	total := len(todo)
	scanned := 0
	for _, n := range todo {
		if err := r.scanGroupSteps(profileID, n, fetch); err != nil {
			// Best-effort: skip groups we can't scan; they stay unscanned.
			continue
		}
		scanned++
		if progress != nil {
			progress(scanned, total)
		}
	}
	return scanned, nil
}

// scanGroupSteps fetches and records the step fingerprint of every member of one
// group. fetch force-pulls fresh steps (the App layer wires it to
// GetTestSteps(..., true)); a member whose fetch fails is skipped best-effort so
// one unreachable Test does not abort the group.
func (r *Repository) scanGroupSteps(profileID, normalizedSummary string, fetch func(key string) ([]Step, error)) error {
	keys, err := r.DuplicateGroupMemberKeys(profileID, normalizedSummary)
	if err != nil {
		return err
	}
	for _, key := range keys {
		steps, err := fetch(key)
		if err != nil {
			// Best-effort: skip members we can't fetch; they stay unscanned.
			continue
		}
		if err := r.RecordStepScan(profileID, key, steps); err != nil {
			return err
		}
	}
	return nil
}

// DuplicateGroupMemberKeys returns the non-ignored member keys of a group.
func (r *Repository) DuplicateGroupMemberKeys(profileID, normalizedSummary string) ([]string, error) {
	cands, err := r.loadCandidates(profileID, normalizedSummary)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(cands))
	for i, c := range cands {
		keys[i] = c.key
	}
	sort.Strings(keys)
	return keys, nil
}
