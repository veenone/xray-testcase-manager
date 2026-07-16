package kiwi

import (
	"strings"

	"xray-test-manager/internal/backend"
)

// kiwiUser mirrors the subset of Kiwi's User RPC output that
// Adapter.TestConnection needs: username, first_name, last_name, email
// (standard Django User fields). Spec §3.1.
type kiwiUser struct {
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

// toUser maps a kiwiUser to the neutral backend.User, falling back to the
// username when no first/last name is set.
func toUser(u kiwiUser) *backend.User {
	display := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if display == "" {
		display = u.Username
	}
	return &backend.User{
		Name:        u.Username,
		DisplayName: display,
		Email:       u.Email,
	}
}
