package testrepo

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/store"
)

// Tests sort by the key's trailing number numerically, not lexically: QA-2 must
// come before QA-10 before QA-100, never the "-1, -10, -100" string order
// (RND_P_4TFINT_05-202 / -205).
func TestListTestsSortsKeysNumerically(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := NewRepository(st)

	keys := []string{"QA-1", "QA-2", "QA-10", "QA-21", "QA-100"}
	var seed []TestCase
	for _, k := range keys {
		seed = append(seed, TestCase{Key: k, ID: k})
	}
	if err := repo.UpsertTests("p1", seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	asc, err := repo.ListTests("p1", Query{SortBy: "key", Limit: 100})
	if err != nil {
		t.Fatalf("list asc: %v", err)
	}
	got := make([]string, len(asc.Tests))
	for i, tc := range asc.Tests {
		got[i] = tc.Key
	}
	want := []string{"QA-1", "QA-2", "QA-10", "QA-21", "QA-100"}
	if !equalStrings(got, want) {
		t.Errorf("ascending key sort = %v, want %v", got, want)
	}

	desc, err := repo.ListTests("p1", Query{SortBy: "key", Desc: true, Limit: 100})
	if err != nil {
		t.Fatalf("list desc: %v", err)
	}
	gotD := make([]string, len(desc.Tests))
	for i, tc := range desc.Tests {
		gotD[i] = tc.Key
	}
	wantD := []string{"QA-100", "QA-21", "QA-10", "QA-2", "QA-1"}
	if !equalStrings(gotD, wantD) {
		t.Errorf("descending key sort = %v, want %v", gotD, wantD)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
