package kiwi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// Authenticator establishes credentials on a Client before authenticated
// calls are made. Two implementations exist behind this seam:
//   - sessionLogin — the default (spec §1.2 Option A). Calls Auth.login and
//     seeds the session cookie.
//   - bearerToken — implemented but NOT the default (spec §1.2 Option B).
//     Stock Kiwi has no token/Bearer auth middleware; this is kept for a
//     deployment that adds one (OQ-1 in the spec).
type Authenticator interface {
	Authenticate(ctx context.Context, c *Client) error
}

// sessionLogin authenticates via Auth.login(username, password), which
// returns the Django session id as a plain string (spec §1.2, §9.1a). It
// seeds the Client's cookiejar with that session id so it is replayed as a
// cookie on every subsequent request — real Kiwi also sets Set-Cookie on
// the login response itself, but seeding explicitly keeps auth correct even
// against a test double that only echoes the JSON-RPC result.
type sessionLogin struct {
	user string
	pass string
}

func (s *sessionLogin) Authenticate(ctx context.Context, c *Client) error {
	var sessionID string
	if err := c.call(ctx, "Auth.login", []any{s.user, s.pass}, &sessionID); err != nil {
		return err
	}
	if sessionID == "" || c.http.Jar == nil {
		return nil
	}
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return err
	}
	c.http.Jar.SetCookies(u, []*http.Cookie{{Name: "sessionid", Value: sessionID, Path: "/"}})
	return nil
}

// bearerToken sends a static header (e.g. "Authorization: Bearer <token>")
// on every request instead of establishing a session. Spec §1.2 Option B —
// NOT usable against stock Kiwi (no token-auth middleware exists there);
// kept behind the Authenticator seam for a deployment that adds one. It is
// never the default returned by NewClient.
type bearerToken struct {
	token  string // the opaque credential
	header string // header name; defaults to "Authorization"
	prefix string // value prefix, e.g. "Bearer "
}

func (b *bearerToken) Authenticate(ctx context.Context, c *Client) error {
	header := b.header
	if header == "" {
		header = "Authorization"
	}
	c.authHeaderName = header
	c.authHeaderValue = b.prefix + b.token
	return nil
}

// splitCredential splits a "username:password" secret on the FIRST colon,
// so a password containing colons is preserved intact. Spec
// p4_1-brief.md "Resolved decisions".
func splitCredential(secret string) (user, pass string) {
	idx := strings.Index(secret, ":")
	if idx < 0 {
		return secret, ""
	}
	return secret[:idx], secret[idx+1:]
}
