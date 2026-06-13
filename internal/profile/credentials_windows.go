//go:build windows

package profile

import (
	"fmt"

	"github.com/danieljoos/wincred"
)

// credentialPrefix namespaces this app's entries in Windows Credential Manager.
const credentialPrefix = "xray-test-manager:"

// windowsCredentialStore backs CredentialStore with Windows Credential Manager,
// whose entries are DPAPI-protected per user account.
type windowsCredentialStore struct{}

// NewCredentialStore returns the OS-native credential store (Windows).
func NewCredentialStore() CredentialStore {
	return &windowsCredentialStore{}
}

func (w *windowsCredentialStore) Save(profileID, secret string) error {
	cred := wincred.NewGenericCredential(credentialPrefix + profileID)
	cred.CredentialBlob = []byte(secret)
	if err := cred.Write(); err != nil {
		return fmt.Errorf("store credential: %w", err)
	}
	return nil
}

func (w *windowsCredentialStore) Load(profileID string) (string, error) {
	cred, err := wincred.GetGenericCredential(credentialPrefix + profileID)
	if err != nil {
		return "", fmt.Errorf("load credential: %w", err)
	}
	return string(cred.CredentialBlob), nil
}

func (w *windowsCredentialStore) Delete(profileID string) error {
	cred, err := wincred.GetGenericCredential(credentialPrefix + profileID)
	if err != nil {
		return fmt.Errorf("find credential to delete: %w", err)
	}
	if err := cred.Delete(); err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	return nil
}
