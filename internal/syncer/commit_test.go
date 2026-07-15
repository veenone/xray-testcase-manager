package syncer

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

func TestOldestBaseVersionPicksEarliest(t *testing.T) {
	changes := []testrepo.PendingChange{
		{BaseVersion: "2026-05-26T08:00:00.000+0700"},
		{BaseVersion: "2026-05-26T07:00:00.000+0700"},
		{BaseVersion: "2026-05-26T09:00:00.000+0700"},
	}

	got := oldestBaseVersion(changes)

	if got != "2026-05-26T07:00:00.000+0700" {
		t.Errorf("got %q, want earliest base_version", got)
	}
}

func TestOldestBaseVersionSkipsEmptyValues(t *testing.T) {
	changes := []testrepo.PendingChange{
		{BaseVersion: ""},
		{BaseVersion: "2026-05-26T07:00:00.000+0700"},
	}

	got := oldestBaseVersion(changes)

	if got != "2026-05-26T07:00:00.000+0700" {
		t.Errorf("got %q, want the non-empty base", got)
	}
}

func TestOldestBaseVersionEmptyInputReturnsEmpty(t *testing.T) {
	if got := oldestBaseVersion(nil); got != "" {
		t.Errorf("empty input should yield empty, got %q", got)
	}
}
