//go:build !windows

package profile

import (
	"fmt"

	"github.com/zalando/go-keyring"
)

// keyringService is the service name this app's secrets are stored under in the
// macOS Keychain / Linux Secret Service (visible in Keychain Access on macOS).
const keyringService = "Xray Test Manager"

// keyringCredentialStore backs CredentialStore with the OS-native secret store
// on non-Windows platforms — the macOS Keychain or the Linux Secret Service —
// via zalando/go-keyring. It needs no cgo (macOS shells out to the `security`
// binary; Linux talks to the Secret Service over D-Bus).
type keyringCredentialStore struct{}

// NewCredentialStore returns the OS-native credential store (macOS / Linux).
func NewCredentialStore() CredentialStore {
	return &keyringCredentialStore{}
}

func (k *keyringCredentialStore) Save(profileID, secret string) error {
	if err := keyring.Set(keyringService, profileID, secret); err != nil {
		return fmt.Errorf("store credential: %w", err)
	}
	return nil
}

func (k *keyringCredentialStore) Load(profileID string) (string, error) {
	secret, err := keyring.Get(keyringService, profileID)
	if err != nil {
		return "", fmt.Errorf("load credential: %w", err)
	}
	return secret, nil
}

func (k *keyringCredentialStore) Delete(profileID string) error {
	if err := keyring.Delete(keyringService, profileID); err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	return nil
}
