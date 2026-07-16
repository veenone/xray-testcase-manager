package kiwi

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// contentHash computes the P4.2 RemoteVersion token: a SHA-256 hex digest
// over the fields spec §5's content-hash option calls "salient" (summary,
// text, case_status, priority, sorted tags, sorted components).
//
// Deviation from the spec's literal field list, documented: the spec names
// the raw FK fields (`case_status`, `priority`); we hash the RESOLVED
// display values instead (CaseStatusName/PriorityValue, i.e. the same
// case_status__name/priority__value already surfaced on backend.Test). A
// bare FK id has no meaning to the hub or the conflict-detection UI, and
// hashing the id could miss a rename (a status keeping its id but changing
// its label) while flagging a spurious change on a harmless renumbering.
// Hashing the resolved values keeps "the hash changed" aligned with "what
// the user would see changed" — which is what RemoteAhead's conflict check
// (base != remote => ahead) actually needs to protect.
//
// Fields are joined with a unit-separator byte (0x1F) rather than the
// spec's illustrative "|" so a literal pipe inside a summary/text value
// cannot collide with the delimiter.
func contentHash(tc kiwiTestCase) string {
	const sep = "\x1f"

	tags := append([]string(nil), tc.Tag...)
	sort.Strings(tags)
	comps := append([]string(nil), tc.Component...)
	sort.Strings(comps)

	parts := []string{
		tc.Summary,
		tc.Text,
		tc.CaseStatusName,
		tc.PriorityValue,
		strings.Join(tags, sep),
		strings.Join(comps, sep),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, sep)))
	return hex.EncodeToString(sum[:])
}
