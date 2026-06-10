package testrepo

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/store"
)

func newCleanRepo(t *testing.T) *Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "clean.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewRepository(st)
}

// TestCleanSampleContainersRemovesOnlySeeded verifies cleanup deletes the
// seeded sample containers (PROJ-SET/PLAN/EXEC-n) and their links, but leaves
// real synced containers (PROJ-<number>) and their links intact.
func TestCleanSampleContainersRemovesOnlySeeded(t *testing.T) {
	repo := newCleanRepo(t)
	db := repo.db

	containers := []struct{ key, kind string }{
		{"QA-SET-1", "testset"},
		{"QA-PLAN-1", "testplan"},
		{"QA-EXEC-1", "testexec"},
		{"QA-1234", "testset"}, // a REAL container — must survive
	}
	for _, c := range containers {
		if _, err := db.Exec(
			`INSERT INTO test_container (profile_id, jira_key, kind, summary, status) VALUES (?, ?, ?, '', '')`,
			"p1", c.key, c.kind,
		); err != nil {
			t.Fatalf("insert container %s: %v", c.key, err)
		}
		if _, err := db.Exec(
			`INSERT INTO test_container_test (profile_id, container_key, test_key, run_status) VALUES (?, ?, 'QA-T1', '')`,
			"p1", c.key,
		); err != nil {
			t.Fatalf("insert link %s: %v", c.key, err)
		}
	}

	removed, err := repo.CleanSampleContainers("p1", "QA")
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	if removed != 3 {
		t.Fatalf("removed = %d, want 3 (the SET/PLAN/EXEC samples)", removed)
	}

	var containerCount, realCount, linkCount int
	db.QueryRow(`SELECT COUNT(*) FROM test_container WHERE profile_id = 'p1'`).Scan(&containerCount)
	db.QueryRow(`SELECT COUNT(*) FROM test_container WHERE profile_id = 'p1' AND jira_key = 'QA-1234'`).Scan(&realCount)
	db.QueryRow(`SELECT COUNT(*) FROM test_container_test WHERE profile_id = 'p1'`).Scan(&linkCount)
	if containerCount != 1 || realCount != 1 {
		t.Fatalf("containers left = %d (real QA-1234 present = %d), want 1 / 1", containerCount, realCount)
	}
	if linkCount != 1 {
		t.Fatalf("links left = %d, want 1 (only the real container's link)", linkCount)
	}
}
