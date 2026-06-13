package profile

// CredentialStore persists per-profile secrets (a PAT or password) in the
// operating system's native secret store — never in the database, never in
// plaintext, never in logs (FR-8.3, NFR-3). The concrete store is chosen at
// build time per platform: Windows Credential Manager on Windows, the macOS
// Keychain or the Linux Secret Service elsewhere (see the credentials_*.go
// files). NewCredentialStore is defined in each platform file.
type CredentialStore interface {
	// Save stores the secret for a profile.
	Save(profileID, secret string) error
	// Load retrieves the secret for a profile.
	Load(profileID string) (string, error)
	// Delete removes the secret for a profile.
	Delete(profileID string) error
}
