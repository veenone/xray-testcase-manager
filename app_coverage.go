package main

import (
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"xray-test-manager/internal/coverage"
	"xray-test-manager/internal/testrepo"
)

// Bound methods for the coverage module (parameter-level test coverage +
// canonical functional-requirement reuse). Every method is a thin delegator to
// a.cov so the module stays isolated from the core test-management surface in
// app.go. Reads guard with requireStore(); mutators add recoverToError.

// --- Coverage: project config (PRD Topic 1) ---

// ListCoverageProjects returns the in-scope projects for the profile's coverage
// module. When none are configured it derives a default from canonical members.
func (a *App) ListCoverageProjects(profileID string) ([]coverage.ProjectConfig, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.ListProjects(profileID)
}

// SetCoverageProjects replaces the in-scope project list for the profile.
func (a *App) SetCoverageProjects(profileID string, projects []coverage.ProjectConfig) (err error) {
	defer recoverToError("SetCoverageProjects", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.SetProjects(profileID, projects)
}

// --- Coverage: canonical requirement registry (PRD Topic 1) ---

// ListCanonicalRequirements returns the profile's canonical functional
// requirements with member counts.
func (a *App) ListCanonicalRequirements(profileID string) ([]coverage.CanonicalRequirement, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.ListCanonical(profileID)
}

// CreateCanonicalRequirement creates a canonical node and returns its id.
func (a *App) CreateCanonicalRequirement(profileID, name, category, description string) (id string, err error) {
	defer recoverToError("CreateCanonicalRequirement", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	return a.cov.CreateCanonical(profileID, name, category, description)
}

// RenameCanonicalRequirement updates a canonical node's editable fields.
func (a *App) RenameCanonicalRequirement(profileID, id, name, category, description string) (err error) {
	defer recoverToError("RenameCanonicalRequirement", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.RenameCanonical(profileID, id, name, category, description)
}

// DeleteCanonicalRequirement removes a canonical node and its whole parameter
// model and memberships.
func (a *App) DeleteCanonicalRequirement(profileID, id string) (err error) {
	defer recoverToError("DeleteCanonicalRequirement", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.DeleteCanonical(profileID, id)
}

// SetCanonicalMembers replaces the requirement issues grouped under a canonical
// node.
func (a *App) SetCanonicalMembers(profileID, canonicalID string, requirementKeys []string) (err error) {
	defer recoverToError("SetCanonicalMembers", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.SetMembers(profileID, canonicalID, requirementKeys)
}

// ListCanonicalReuse returns the member requirements of a canonical node with
// their project/customer, answering "who reuses this functional requirement?".
func (a *App) ListCanonicalReuse(profileID, canonicalID string) ([]coverage.ReuseRow, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.ListReuse(profileID, canonicalID)
}

// --- Coverage: parameter model (PRD Topic 3) ---

// GetParamModel returns the full parameter tree (groups → parameters → values)
// for a version of a canonical requirement.
func (a *App) GetParamModel(profileID, versionID string) (coverage.ParamModel, error) {
	if err := a.requireStore(); err != nil {
		return coverage.ParamModel{Groups: []coverage.ParamGroup{}}, err
	}
	return a.cov.GetParamModel(profileID, versionID)
}

// UpsertCoverageNode inserts or updates one node (group, parameter, or value)
// of the parameter model and returns its id. A single entry point keeps the
// bound surface small.
func (a *App) UpsertCoverageNode(profileID string, node coverage.NodeEdit) (id string, err error) {
	defer recoverToError("UpsertCoverageNode", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	return a.cov.UpsertNode(profileID, node)
}

// DeleteCoverageNode removes a node (and everything beneath it) from the model.
func (a *App) DeleteCoverageNode(profileID, kind, id string) (err error) {
	defer recoverToError("DeleteCoverageNode", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.DeleteNode(profileID, kind, id)
}

// --- Coverage: value↔test mapping & computation (PRD Topic 3) ---

// GetCoverageReport computes per-group and overall parameter coverage for a
// version of a canonical requirement, plus a per-value tested/run-status
// annotation for the matrix.
func (a *App) GetCoverageReport(profileID, versionID string) (coverage.CoverageReport, error) {
	if err := a.requireStore(); err != nil {
		return coverage.CoverageReport{Groups: []coverage.GroupCoverage{}, Values: map[string]coverage.ValueCoverage{}}, err
	}
	return a.cov.ComputeCoverage(profileID, versionID)
}

// ListCoverageGaps returns the required values with no live mapped test — the
// named work to reach 100%.
func (a *App) ListCoverageGaps(profileID, versionID string) ([]coverage.Gap, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.ListGaps(profileID, versionID)
}

// ListCoverageCandidateTests returns the Tests eligible to be mapped to a value
// (those linked to a member requirement of the canonical node).
func (a *App) ListCoverageCandidateTests(profileID, canonicalID string) ([]coverage.CandidateTest, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.ListCandidateTests(profileID, canonicalID)
}

// ListValueTests returns the test keys currently mapped to a parameter value.
func (a *App) ListValueTests(profileID, valueID string) ([]string, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.ListValueTests(profileID, valueID)
}

// SetValueTests replaces the set of Tests mapped to a parameter value (the
// local "tested" signal).
func (a *App) SetValueTests(profileID, valueID string, testKeys []string) (err error) {
	defer recoverToError("SetValueTests", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.SetValueTests(profileID, valueID, testKeys)
}

// DetectStaleCoverageMappings returns value→test mappings whose test no longer
// exists locally, scoped to a version when versionID != "" (empty scans the
// whole profile).
func (a *App) DetectStaleCoverageMappings(profileID, versionID string) ([]coverage.StaleMapping, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.DetectStaleMappings(profileID, versionID)
}

// --- Coverage: import / export (PRD Topic 3) ---

// ImportCoverageTemplate prompts for an .xlsx parameter-extraction workbook and
// loads it into the version's model (replacing any existing model). A
// zero-value summary is returned when the dialog is cancelled.
func (a *App) ImportCoverageTemplate(profileID, versionID string) (summary coverage.ImportSummary, err error) {
	defer recoverToError("ImportCoverageTemplate", &err)
	if err := a.requireStore(); err != nil {
		return coverage.ImportSummary{}, err
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Import parameter-coverage template",
		Filters: []runtime.FileFilter{{DisplayName: "Excel Workbook", Pattern: "*.xlsx"}},
	})
	if err != nil {
		return coverage.ImportSummary{}, fmt.Errorf("open dialog: %w", err)
	}
	if path == "" {
		return coverage.ImportSummary{Warnings: []string{}}, nil // cancelled
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return coverage.ImportSummary{}, fmt.Errorf("read file: %w", err)
	}
	return a.cov.ImportCoverageTemplate(profileID, versionID, data)
}

// ExportCoverageReport prompts for a save location and writes the version's
// coverage report as a styled .xlsx. Returns the path written, or "" when
// cancelled.
func (a *App) ExportCoverageReport(profileID, versionID string) (path string, err error) {
	defer recoverToError("ExportCoverageReport", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	data, err := a.cov.ExportReport(profileID, versionID)
	if err != nil {
		return "", err
	}
	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export coverage report",
		DefaultFilename: "coverage-report.xlsx",
		Filters:         []runtime.FileFilter{{DisplayName: "Excel Workbook", Pattern: "*.xlsx"}},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if dest == "" {
		return "", nil // cancelled
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return dest, nil
}

// DownloadCoverageTemplate prompts for a save location and writes a blank,
// ready-to-fill parameter-extraction workbook whose format matches the importer.
// Returns the path written, or "" when cancelled.
func (a *App) DownloadCoverageTemplate() (path string, err error) {
	defer recoverToError("DownloadCoverageTemplate", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	data, err := coverage.GenerateTemplateWorkbook()
	if err != nil {
		return "", err
	}
	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Download coverage template",
		DefaultFilename: "parameter-coverage-template.xlsx",
		Filters:         []runtime.FileFilter{{DisplayName: "Excel Workbook", Pattern: "*.xlsx"}},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if dest == "" {
		return "", nil // cancelled
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return dest, nil
}

// SeedPKCS11Reference seeds a complete, cross-linked PKCS#11 reference dataset
// (requirements + tests + executions + the full coverage/versioning layer for
// C_Sign, C_GenerateKeyPair, C_Verify) into the profile — for use on a throwaway
// "demo-pkcs" profile as an end-to-end data reference. Idempotent.
func (a *App) SeedPKCS11Reference(profileID string) (summary coverage.PKCSSeedSummary, err error) {
	defer recoverToError("SeedPKCS11Reference", &err)
	if err := a.requireStore(); err != nil {
		return coverage.PKCSSeedSummary{}, err
	}
	return a.cov.SeedPKCSReference(profileID)
}

// SeedEUICCReference seeds the GSMA eUICC/RSP reference dataset (the seven RSP
// procedures with their coverage/versioning layer mapped onto the synced
// CUST-MNO-CONSUMER / CUST-IOT-FLEET / CUST-M2M-AUTO requirements + tests) into
// the profile — for use on a throwaway "demo-euicc" profile. Idempotent.
func (a *App) SeedEUICCReference(profileID string) (summary coverage.EUICCSeedSummary, err error) {
	defer recoverToError("SeedEUICCReference", &err)
	if err := a.requireStore(); err != nil {
		return coverage.EUICCSeedSummary{}, err
	}
	return a.cov.SeedEUICCReference(profileID)
}

// SeedEUICCASPICEAssessment overlays the seven Automotive SPICE processes as
// coverage canonicals judged against the synced demo-euicc dataset (a curated
// in-app ASPICE gap assessment). For use on a throwaway "demo-euicc" profile.
// Idempotent.
func (a *App) SeedEUICCASPICEAssessment(profileID string) (summary coverage.ASPICEAssessmentSummary, err error) {
	defer recoverToError("SeedEUICCASPICEAssessment", &err)
	if err := a.requireStore(); err != nil {
		return coverage.ASPICEAssessmentSummary{}, err
	}
	return a.cov.SeedEUICCASPICEAssessment(profileID)
}

// SeedASPICEReference seeds the Automotive SPICE reference dataset (seven ASPICE
// processes — SYS.2, SYS.5, SWE.1, SWE.4, SWE.6, SUP.9, SUP.10 — with their
// coverage/versioning layer mapped onto the synced CUST-OEM-PLATFORM /
// CUST-TIER1-ECU / CUST-SAFETY-DOMAIN requirements + tests) into the profile —
// for use on a throwaway "demo-aspice" profile. Idempotent.
func (a *App) SeedASPICEReference(profileID string) (summary coverage.ASPICESeedSummary, err error) {
	defer recoverToError("SeedASPICEReference", &err)
	if err := a.requireStore(); err != nil {
		return coverage.ASPICESeedSummary{}, err
	}
	return a.cov.SeedASPICEReference(profileID)
}

// SeedDemoCoverageExample builds the built-in PKCS#11 C_Sign example for the
// profile (intended for demo mode): a full parameter model mapped to the
// profile's synced tests, landing at the PRD's 35/41 = 85.4%. Returns the new
// canonical requirement's id.
func (a *App) SeedDemoCoverageExample(profileID string) (id string, err error) {
	defer recoverToError("SeedDemoCoverageExample", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	return a.cov.SeedDemoExample(profileID)
}

// --- Coverage: versions (PRD Topic 2) ---

func (a *App) ListVersions(profileID, canonicalID string) ([]coverage.Version, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.ListVersions(profileID, canonicalID)
}

func (a *App) CreateVersion(profileID, canonicalID, name, status, notes string) (id string, err error) {
	defer recoverToError("CreateVersion", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	return a.cov.CreateVersion(profileID, canonicalID, name, status, notes)
}

func (a *App) CloneVersion(profileID, sourceVersionID, name, status string) (id string, err error) {
	defer recoverToError("CloneVersion", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	return a.cov.CloneVersion(profileID, sourceVersionID, name, status)
}

func (a *App) RenameVersion(profileID, id, name, status, notes string) (err error) {
	defer recoverToError("RenameVersion", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.RenameVersion(profileID, id, name, status, notes)
}

func (a *App) SetVersionStatus(profileID, id, status string) (err error) {
	defer recoverToError("SetVersionStatus", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.SetVersionStatus(profileID, id, status)
}

func (a *App) DeleteVersion(profileID, id string) (err error) {
	defer recoverToError("DeleteVersion", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.DeleteVersion(profileID, id)
}

func (a *App) SetMemberVersion(profileID, canonicalID, requirementKey, versionID string) (err error) {
	defer recoverToError("SetMemberVersion", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.SetMemberVersion(profileID, canonicalID, requirementKey, versionID)
}

// --- Coverage: change requests (PRD Topic 2) ---

func (a *App) ListChangeRequests(profileID, canonicalID string) ([]coverage.ChangeRequest, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.ListChangeRequests(profileID, canonicalID)
}

func (a *App) CreateChangeRequest(profileID, canonicalID, crKey, title, status, targetVersionID, risk, description string) (id string, err error) {
	defer recoverToError("CreateChangeRequest", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	return a.cov.CreateChangeRequest(profileID, canonicalID, crKey, title, status, targetVersionID, risk, description)
}

func (a *App) UpdateChangeRequest(profileID, id, crKey, title, status, targetVersionID, risk, description string) (err error) {
	defer recoverToError("UpdateChangeRequest", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.UpdateChangeRequest(profileID, id, crKey, title, status, targetVersionID, risk, description)
}

func (a *App) DeleteChangeRequest(profileID, id string) (err error) {
	defer recoverToError("DeleteChangeRequest", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.DeleteChangeRequest(profileID, id)
}

func (a *App) SetCRDecision(profileID, crID, requirementKey, decision, note string) (err error) {
	defer recoverToError("SetCRDecision", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.SetCRDecision(profileID, crID, requirementKey, decision, note)
}

// --- Coverage: dashboards (PRD Topic 2) ---

func (a *App) GetVersionDistribution(profileID, canonicalID string) ([]coverage.VersionShare, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.VersionDistribution(profileID, canonicalID)
}

func (a *App) GetCRAdoption(profileID, canonicalID string) ([]coverage.CRShare, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.CRAdoption(profileID, canonicalID)
}

func (a *App) GetCRImpact(profileID, crID string) (coverage.CRImpactResult, error) {
	if err := a.requireStore(); err != nil {
		return coverage.CRImpactResult{Decisions: []coverage.CRDecision{}}, err
	}
	return a.cov.CRImpact(profileID, crID)
}

// --- Coverage: project-level map (PRD Coverage Map view) ---

// GetCoverageProjectStatus returns per-project coverage rows for the Coverage
// Map view's left panel.
func (a *App) GetCoverageProjectStatus(profileID string) ([]coverage.ProjectCoverageRow, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.ProjectCoverage(profileID)
}

// GetCoverageRelationSankey returns the project → canonical-function → coverage
// Sankey for the Coverage Map view's right panel.
func (a *App) GetCoverageRelationSankey(profileID string) (testrepo.Sankey, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.Sankey{}, err
	}
	return a.cov.ProjectRelationSankey(profileID)
}
