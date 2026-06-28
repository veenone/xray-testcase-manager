// Package coverage is a bounded, local-only sibling module that adds
// parameter-level test coverage and a canonical functional-requirement
// registry on top of the existing test-management store.
//
// It deliberately depends on nothing in testrepo beyond two read-only exported
// helpers (ConsolidatedRunByTest, DeriveCoverage), and testrepo never imports
// this package — Go's no-import-cycle rule keeps the boundary honest. All
// domain state lives in the coverage_* / canonical_* SQLite tables (schema
// v35); nothing here touches Jira, matching the no-admin deployment.
package coverage

import (
	"database/sql"

	"xray-test-manager/internal/store"
	"xray-test-manager/internal/testrepo"
)

// Module is the entry point for the coverage feature. It is held as a single
// field on App and every bound method delegates to it, so the surface stays
// isolated from the core test-management code.
type Module struct {
	db   *sql.DB
	repo *testrepo.Repository
}

// New constructs the module over the same SQLite handle the rest of the app
// uses. repo supplies the shared run-status logic.
func New(s *store.Store, repo *testrepo.Repository) *Module {
	return &Module{db: s.DB(), repo: repo}
}
