package kiwi

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// contentHash computes the RemoteVersion token: a SHA-256 hex digest over the
// TestCase ROW fields alone (summary, text, resolved case_status name,
// resolved priority value, and history_date). It is computable from a single
// TestCase.filter row with NO extra RPC, so the pull path and the
// RemoteVersion path — which both read the same row — always produce an
// identical token for the same remote state.
//
// history_date (P4.6, live-verified) is Kiwi's per-object last-modified stamp
// (django-simple-history): it advances on every save, so including it makes
// the token change on any edit. Tags/components are DELIBERATELY excluded:
// they are not on the TestCase row (fetched separately via Tag/Component
// filter), so requiring them would force an extra RPC in RemoteVersion, and
// they are display-only rather than part of the version identity.
//
// We hash the RESOLVED display values (CaseStatusName/PriorityValue) rather
// than the raw FK ids: a bare id has no meaning to the hub or the
// conflict-detection UI, and hashing the id could miss a rename (same id, new
// label) while flagging a spurious change on a harmless renumbering. Fields
// are joined with a unit-separator byte (0x1F) rather than an illustrative
// "|" so a literal delimiter inside a value cannot collide.
func contentHash(tc kiwiTestCase) string {
	const sep = "\x1f"
	parts := []string{
		tc.Summary,
		tc.Text,
		tc.CaseStatusName,
		tc.PriorityValue,
		tc.HistoryDate,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, sep)))
	return hex.EncodeToString(sum[:])
}
