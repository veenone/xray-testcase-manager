package xray

import (
	"testing"

	"xray-test-manager/internal/backend"
)

// RemoteAhead doesn't touch the wrapped jira.Client, so a zero-value Adapter
// is a valid receiver for these tests.
var remoteAheadAdapter = &Adapter{}

func TestRemoteAheadDetectsLaterTimestamp(t *testing.T) {
	if !remoteAheadAdapter.RemoteAhead(
		backend.VersionToken("2026-05-26T07:00:00.000+0700"),
		backend.VersionToken("2026-05-26T08:00:00.000+0700"),
	) {
		t.Error("later remote should be flagged as ahead")
	}
}

func TestRemoteAheadEqualTimestampsReturnFalse(t *testing.T) {
	s := backend.VersionToken("2026-05-26T07:00:00.000+0700")
	if remoteAheadAdapter.RemoteAhead(s, s) {
		t.Error("equal timestamps must not be ahead — no false conflict")
	}
}

func TestRemoteAheadEarlierRemoteReturnsFalse(t *testing.T) {
	if remoteAheadAdapter.RemoteAhead(
		backend.VersionToken("2026-05-26T07:00:00.000+0700"),
		backend.VersionToken("2026-05-26T06:00:00.000+0700"),
	) {
		t.Error("earlier remote must not be flagged as ahead")
	}
}

func TestRemoteAheadComparesAcrossFormats(t *testing.T) {
	// base in Jira's format; remote in RFC 3339 — both should parse.
	if !remoteAheadAdapter.RemoteAhead(
		backend.VersionToken("2026-05-26T07:00:00.000+0000"),
		backend.VersionToken("2026-05-26T08:00:00Z"),
	) {
		t.Error("should compare across timestamp formats")
	}
}

func TestRemoteAheadUnparseableInputReturnsFalse(t *testing.T) {
	if remoteAheadAdapter.RemoteAhead(
		backend.VersionToken("2026-05-26T07:00:00.000+0700"),
		backend.VersionToken("not a date"),
	) {
		t.Error("unparseable remote must not manufacture a false conflict")
	}
}
