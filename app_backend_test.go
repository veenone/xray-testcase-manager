package main

import (
	"testing"

	"xray-test-manager/internal/backend/kiwi"
	"xray-test-manager/internal/backend/xray"
)

// TestNewBackendRouting verifies newBackend routes on backendType (a
// Profile.Backend value) and on a kiwi-demo URL, matching the factory's
// contract (P6.1a): a "kiwi" backend type always routes to kiwi.New; a
// kiwi-demo URL routes to kiwi.New regardless of backendType (so existing
// demo profiles with a blank/"xray" Backend field keep working); anything
// else routes to xray.New.
func TestNewBackendRouting(t *testing.T) {
	if _, ok := newBackend("kiwi", "https://kiwi.example.com", "user:pass", "", false).(*kiwi.Adapter); !ok {
		t.Error(`newBackend("kiwi", real URL) did not return a *kiwi.Adapter`)
	}

	if _, ok := newBackend("xray", "https://jira.example.com", "tok", "", false).(*xray.Adapter); !ok {
		t.Error(`newBackend("xray", real URL) did not return a *xray.Adapter`)
	}
	if _, ok := newBackend("", "https://jira.example.com", "tok", "", false).(*xray.Adapter); !ok {
		t.Error(`newBackend("", real URL) did not return a *xray.Adapter`)
	}

	if _, ok := newBackend("", "kiwi-demo", "tok", "", false).(*kiwi.Adapter); !ok {
		t.Error(`newBackend("", "kiwi-demo") did not return a *kiwi.Adapter`)
	}
	if _, ok := newBackend("xray", "kiwi-demo", "tok", "", false).(*kiwi.Adapter); !ok {
		t.Error(`newBackend("xray", "kiwi-demo") did not return a *kiwi.Adapter`)
	}
}
