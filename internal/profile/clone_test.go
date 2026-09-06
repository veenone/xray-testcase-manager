package profile_test

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/profile"
	"xray-test-manager/internal/store"
)

func newCloneManager(t *testing.T) *profile.Manager {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return profile.NewManager(st)
}

// TestCreateCarriesTLSSettings pins the fields a cloned profile must not lose.
// A profile created from another one on the same server needs that server's
// certificate settings, or it is created in a state that cannot connect.
func TestCreateCarriesTLSSettings(t *testing.T) {
	m := newCloneManager(t)

	const pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"
	p, err := m.Create("Internal", "https://jira.internal", "PROJ", "", "Bug", "test", "",
		pem, true, "xray")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.CACert != pem {
		t.Errorf("CACert = %q, want the certificate it was created with", p.CACert)
	}
	if !p.AllowUntrustedTLS {
		t.Error("AllowUntrustedTLS = false, want true")
	}

	// And it survives a round trip through the store.
	got, err := m.Get(p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CACert != pem || !got.AllowUntrustedTLS {
		t.Errorf("after reload: CACert=%q AllowUntrustedTLS=%v", got.CACert, got.AllowUntrustedTLS)
	}
}
