package coverage

import (
	"fmt"
	"sort"

	"xray-test-manager/internal/testrepo"
)

// ProjectCoverageRow is one row in the per-project coverage summary table
// (Coverage Map view, left panel).
type ProjectCoverageRow struct {
	ProjectKey       string  `json:"projectKey"`
	Role             string  `json:"role"`
	Label            string  `json:"label"`
	RequirementCount int     `json:"requirementCount"`
	FunctionsReused  int     `json:"functionsReused"`
	CoveredValues    int     `json:"coveredValues"`
	TotalValues      int     `json:"totalValues"`
	Percent          float64 `json:"percent"`
}

// stableVersionID returns the first "stable" version ID for a canonical, or
// the first version ID when none are stable, or "" when no versions exist.
func (m *Module) stableVersionID(profileID, canonicalID string) (string, error) {
	versions, err := m.ListVersions(profileID, canonicalID)
	if err != nil {
		return "", err
	}
	for _, v := range versions {
		if v.Status == "stable" {
			return v.ID, nil
		}
	}
	if len(versions) > 0 {
		return versions[0].ID, nil
	}
	return "", nil
}

// ProjectCoverage computes a per-project coverage summary across all projects
// returned by ListProjects for the profile.
func (m *Module) ProjectCoverage(profileID string) ([]ProjectCoverageRow, error) {
	projects, err := m.ListProjects(profileID)
	if err != nil {
		return nil, err
	}

	var out []ProjectCoverageRow
	for _, proj := range projects {
		row := ProjectCoverageRow{
			ProjectKey: proj.ProjectKey,
			Role:       proj.Role,
			Label:      proj.Label,
		}
		switch proj.Role {
		case "source":
			if err := m.aggregateSourceProject(profileID, &row); err != nil {
				return nil, fmt.Errorf("project %q: %w", proj.ProjectKey, err)
			}
		default: // "customer"
			if err := m.aggregateCustomerProject(profileID, proj.ProjectKey, &row); err != nil {
				return nil, fmt.Errorf("project %q: %w", proj.ProjectKey, err)
			}
		}
		if row.TotalValues > 0 {
			row.Percent = float64(row.CoveredValues) / float64(row.TotalValues) * 100
		}
		out = append(out, row)
	}
	return out, nil
}

// aggregateCustomerProject fills a ProjectCoverageRow for a customer-role
// project: members are requirements whose project_key matches, one version per
// canonical (member's accepted_version_id when set, else stableVersionID).
func (m *Module) aggregateCustomerProject(profileID, projectKey string, row *ProjectCoverageRow) error {
	dbRows, err := m.db.Query(
		`SELECT mm.canonical_id, COALESCE(mm.accepted_version_id,'')
		   FROM canonical_requirement_member mm
		   JOIN requirement r ON r.profile_id = mm.profile_id AND r.jira_key = mm.requirement_key
		  WHERE mm.profile_id = ? AND r.project_key = ?
		  ORDER BY mm.requirement_key`,
		profileID, projectKey)
	if err != nil {
		return err
	}
	defer dbRows.Close()

	// first version seen per canonical (first member wins on version choice)
	canonicalVersion := map[string]string{}
	memberCount := 0

	for dbRows.Next() {
		var canonicalID, versionID string
		if err := dbRows.Scan(&canonicalID, &versionID); err != nil {
			return err
		}
		memberCount++
		if _, seen := canonicalVersion[canonicalID]; !seen {
			canonicalVersion[canonicalID] = versionID
		}
	}
	if err := dbRows.Err(); err != nil {
		return err
	}

	row.RequirementCount = memberCount
	row.FunctionsReused = len(canonicalVersion)

	for canonicalID, ver := range canonicalVersion {
		if ver == "" {
			stable, err := m.stableVersionID(profileID, canonicalID)
			if err != nil {
				return err
			}
			ver = stable
		}
		if ver == "" {
			continue // no version → skip
		}
		report, err := m.ComputeCoverage(profileID, ver)
		if err != nil {
			return err
		}
		row.CoveredValues += report.TestedValues
		row.TotalValues += report.TotalValues
	}
	return nil
}

// aggregateSourceProject fills a ProjectCoverageRow for a source-role project:
// every canonical in the profile is counted once, using its stable version.
func (m *Module) aggregateSourceProject(profileID string, row *ProjectCoverageRow) error {
	dbRows, err := m.db.Query(
		`SELECT id FROM canonical_requirement WHERE profile_id = ?`, profileID)
	if err != nil {
		return err
	}
	defer dbRows.Close()

	var canonicalIDs []string
	for dbRows.Next() {
		var id string
		if err := dbRows.Scan(&id); err != nil {
			return err
		}
		canonicalIDs = append(canonicalIDs, id)
	}
	if err := dbRows.Err(); err != nil {
		return err
	}

	count := len(canonicalIDs)
	row.RequirementCount = count
	row.FunctionsReused = count

	for _, cid := range canonicalIDs {
		ver, err := m.stableVersionID(profileID, cid)
		if err != nil {
			return err
		}
		if ver == "" {
			continue
		}
		report, err := m.ComputeCoverage(profileID, ver)
		if err != nil {
			return err
		}
		row.CoveredValues += report.TestedValues
		row.TotalValues += report.TotalValues
	}
	return nil
}

// ProjectRelationSankey builds a project → canonical-function → coverage Sankey
// for the Coverage Map view. Layer 0 = customer projects, layer 1 = canonicals,
// layer 2 = covered/gap outcome nodes. Only nodes with ≥1 link are included.
func (m *Module) ProjectRelationSankey(profileID string) (testrepo.Sankey, error) {
	out := testrepo.Sankey{Nodes: []testrepo.SankeyNode{}, Links: []testrepo.SankeyLink{}}

	projects, err := m.ListProjects(profileID)
	if err != nil {
		return out, err
	}

	// Preload canonical names for fn-node labels.
	nameRows, err := m.db.Query(
		`SELECT id, name FROM canonical_requirement WHERE profile_id = ?`, profileID)
	if err != nil {
		return out, err
	}
	defer nameRows.Close()
	canonicalName := map[string]string{}
	for nameRows.Next() {
		var id, name string
		if err := nameRows.Scan(&id, &name); err != nil {
			return out, err
		}
		canonicalName[id] = name
	}
	if err := nameRows.Err(); err != nil {
		return out, err
	}

	// nodeValue, nodeLabel, nodeLayer accumulate per node.
	nodeValue := map[string]int{}
	nodeLabel := map[string]string{}
	nodeLayer := map[string]int{}

	type sankeyLink struct{ source, target string; value int }
	var links []sankeyLink

	// Layer-0 → layer-1 links: one per (customer project, canonical) pair.
	for _, proj := range projects {
		if proj.Role != "customer" {
			continue
		}
		projID := "proj:" + proj.ProjectKey

		cntRows, err := m.db.Query(
			`SELECT mm.canonical_id, COUNT(*)
			   FROM canonical_requirement_member mm
			   JOIN requirement r ON r.profile_id = mm.profile_id AND r.jira_key = mm.requirement_key
			  WHERE mm.profile_id = ? AND r.project_key = ?
			  GROUP BY mm.canonical_id`,
			profileID, proj.ProjectKey)
		if err != nil {
			return out, err
		}
		for cntRows.Next() {
			var canonicalID string
			var count int
			if err := cntRows.Scan(&canonicalID, &count); err != nil {
				cntRows.Close()
				return out, err
			}
			ver, err := m.stableVersionID(profileID, canonicalID)
			if err != nil {
				cntRows.Close()
				return out, err
			}
			if ver == "" {
				continue // no version → skip canonical in Sankey
			}
			fnID := "fn:" + canonicalID
			links = append(links, sankeyLink{source: projID, target: fnID, value: count})
			nodeValue[projID] += count
			nodeValue[fnID] += count
			nodeLabel[projID] = proj.Label
			nodeLabel[fnID] = canonicalName[canonicalID]
			nodeLayer[projID] = 0
			nodeLayer[fnID] = 1
		}
		if err := cntRows.Err(); err != nil {
			cntRows.Close()
			return out, err
		}
		if err := cntRows.Close(); err != nil {
			return out, err
		}
	}

	// Layer-1 → layer-2 links: fn → covered/gap using stableVersionID.
	// Note: the Sankey intentionally uses the function's stable version here, not
	// a per-project accepted_version_id. A shared canonical node can't carry
	// per-project version locks, so the Sankey flow may differ from the panel %
	// shown in ProjectCoverage when a customer is locked to a non-stable version.
	// Iterate over a sorted slice so output is deterministic.
	fnIDs := make([]string, 0)
	for id := range nodeLabel {
		if nodeLayer[id] == 1 {
			fnIDs = append(fnIDs, id)
		}
	}
	sort.Strings(fnIDs)

	for _, fnID := range fnIDs {
		canonicalID := fnID[3:] // strip "fn:"
		ver, err := m.stableVersionID(profileID, canonicalID)
		if err != nil {
			return out, err
		}
		if ver == "" {
			continue
		}
		report, err := m.ComputeCoverage(profileID, ver)
		if err != nil {
			return out, err
		}

		covered := report.TestedValues
		gap := report.TotalValues - report.TestedValues

		links = append(links,
			sankeyLink{source: fnID, target: "out:covered", value: covered},
			sankeyLink{source: fnID, target: "out:gap", value: gap},
		)
		nodeValue[fnID] += covered + gap
		nodeValue["out:covered"] += covered
		nodeValue["out:gap"] += gap
		nodeLabel["out:covered"] = "Covered"
		nodeLabel["out:gap"] = "Gap"
		nodeLayer["out:covered"] = 2
		nodeLayer["out:gap"] = 2
	}

	// Build node list (only nodes that ended up with a label).
	for id, lbl := range nodeLabel {
		out.Nodes = append(out.Nodes, testrepo.SankeyNode{
			ID:    id,
			Label: lbl,
			Layer: nodeLayer[id],
			Value: nodeValue[id],
		})
	}
	sort.Slice(out.Nodes, func(i, j int) bool {
		if out.Nodes[i].Layer != out.Nodes[j].Layer {
			return out.Nodes[i].Layer < out.Nodes[j].Layer
		}
		return out.Nodes[i].ID < out.Nodes[j].ID
	})

	// Build link list.
	for _, l := range links {
		out.Links = append(out.Links, testrepo.SankeyLink{
			Source: l.source,
			Target: l.target,
			Value:  l.value,
		})
	}
	sort.Slice(out.Links, func(i, j int) bool {
		if out.Links[i].Source != out.Links[j].Source {
			return out.Links[i].Source < out.Links[j].Source
		}
		return out.Links[i].Target < out.Links[j].Target
	})

	return out, nil
}
