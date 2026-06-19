package testrepo

import (
	"testing"
)

// hasPendingContainerEnv reports whether a coalesced container_env pending
// change exists for the given container key.
func hasPendingContainerEnv(t *testing.T, r *Repository, profileID, containerKey string) bool {
	t.Helper()
	var n int
	if err := r.db.QueryRow(
		`SELECT COUNT(*) FROM pending_change
		 WHERE profile_id = ? AND entity_type = 'container_env' AND entity_key = ?`,
		profileID, containerKey,
	).Scan(&n); err != nil {
		t.Fatalf("count container_env pending: %v", err)
	}
	return n > 0
}

func containsAll(haystack, needles []string) bool {
	set := map[string]bool{}
	for _, h := range haystack {
		set[h] = true
	}
	for _, n := range needles {
		if !set[n] {
			return false
		}
	}
	return true
}

func TestSetContainerEnvironmentsAndFilter(t *testing.T) {
	r := newTestRepo(t)
	const p = "p1"

	if err := r.UpsertContainers(p, []Container{
		{Key: "EX-1", Kind: "testexec", Summary: "Cycle 1", Status: "Open"},
		{Key: "EX-2", Kind: "testexec", Summary: "Cycle 2", Status: "Open"},
	}); err != nil {
		t.Fatalf("UpsertContainers: %v", err)
	}

	if err := r.SetContainerEnvironments(p, "EX-1", []string{"Staging", "Chrome"}); err != nil {
		t.Fatalf("SetContainerEnvironments: %v", err)
	}

	// The container list/board for EX-1 returns the environments.
	got, err := r.ListContainers(p, "testexec")
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	byKey := map[string]Container{}
	for _, c := range got {
		byKey[c.Key] = c
	}
	if !containsAll(byKey["EX-1"].Environments, []string{"Staging", "Chrome"}) {
		t.Errorf("EX-1 environments = %v, want Staging+Chrome", byKey["EX-1"].Environments)
	}
	if len(byKey["EX-2"].Environments) != 0 {
		t.Errorf("EX-2 should have no environments, got %v", byKey["EX-2"].Environments)
	}

	// The environment filter returns only EX-1.
	filtered, err := r.ListContainersQuery(p, ContainerQuery{Kind: "testexec", Environment: "Staging"})
	if err != nil {
		t.Fatalf("ListContainersQuery: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Key != "EX-1" {
		t.Fatalf("filter Staging = %v, want only EX-1", keysOf(filtered))
	}

	// A coalesced pending_change with entity_type container_env exists.
	if !hasPendingContainerEnv(t, r, p, "EX-1") {
		t.Errorf("expected a container_env pending change for EX-1")
	}
}

// TestSetContainerEnvironmentsFilterAvoidsSubstringCollision verifies the JSON
// quoted-token match does not let "Prod" match "Production".
func TestSetContainerEnvironmentsFilterAvoidsSubstringCollision(t *testing.T) {
	r := newTestRepo(t)
	const p = "p1"
	if err := r.UpsertContainers(p, []Container{
		{Key: "EX-1", Kind: "testexec", Summary: "One", Status: "Open"},
		{Key: "EX-2", Kind: "testexec", Summary: "Two", Status: "Open"},
	}); err != nil {
		t.Fatalf("UpsertContainers: %v", err)
	}
	if err := r.SetContainerEnvironments(p, "EX-1", []string{"Production"}); err != nil {
		t.Fatalf("SetContainerEnvironments EX-1: %v", err)
	}
	if err := r.SetContainerEnvironments(p, "EX-2", []string{"Prod"}); err != nil {
		t.Fatalf("SetContainerEnvironments EX-2: %v", err)
	}
	filtered, err := r.ListContainersQuery(p, ContainerQuery{Kind: "testexec", Environment: "Prod"})
	if err != nil {
		t.Fatalf("ListContainersQuery: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Key != "EX-2" {
		t.Fatalf("filter Prod = %v, want only EX-2 (no Production collision)", keysOf(filtered))
	}
}

func TestBulkEditContainersEnv(t *testing.T) {
	r := newTestRepo(t)
	const p = "p1"
	if err := r.UpsertContainers(p, []Container{
		{Key: "EX-1", Kind: "testexec", Summary: "One", Status: "Open"},
		{Key: "EX-2", Kind: "testexec", Summary: "Two", Status: "Open"},
	}); err != nil {
		t.Fatalf("UpsertContainers: %v", err)
	}

	// set_env across two.
	res, err := r.BulkEditContainers(p, []string{"EX-1", "EX-2"}, BulkEdit{Operation: "set_env", Value: "Staging"})
	if err != nil {
		t.Fatalf("BulkEditContainers set_env: %v", err)
	}
	if len(res.Succeeded) != 2 || len(res.Failed) != 0 {
		t.Fatalf("set_env result = %+v, want 2 succeeded", res)
	}
	assertEnv(t, r, p, "EX-1", []string{"Staging"})
	assertEnv(t, r, p, "EX-2", []string{"Staging"})

	// add_env Chrome to both.
	if _, err := r.BulkEditContainers(p, []string{"EX-1", "EX-2"}, BulkEdit{Operation: "add_env", Value: "Chrome"}); err != nil {
		t.Fatalf("BulkEditContainers add_env: %v", err)
	}
	assertEnv(t, r, p, "EX-1", []string{"Chrome", "Staging"})
	assertEnv(t, r, p, "EX-2", []string{"Chrome", "Staging"})

	// remove_env Staging from EX-1 only.
	if _, err := r.BulkEditContainers(p, []string{"EX-1"}, BulkEdit{Operation: "remove_env", Value: "Staging"}); err != nil {
		t.Fatalf("BulkEditContainers remove_env: %v", err)
	}
	assertEnv(t, r, p, "EX-1", []string{"Chrome"})
	assertEnv(t, r, p, "EX-2", []string{"Chrome", "Staging"})
}

func assertEnv(t *testing.T, r *Repository, profileID, key string, want []string) {
	t.Helper()
	got, err := r.ContainerEnvironments(profileID, key)
	if err != nil {
		t.Fatalf("ContainerEnvironments %s: %v", key, err)
	}
	if len(got) != len(want) {
		t.Fatalf("%s env = %v, want %v", key, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s env = %v, want %v", key, got, want)
		}
	}
}

func keysOf(cs []Container) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Key
	}
	return out
}
