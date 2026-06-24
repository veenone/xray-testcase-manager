package main

import (
	"errors"
	"strings"
	"testing"
)

// TestRecoverToError verifies that recoverToError catches a panic and sets the
// named error return to a message that includes the panic value.
func TestRecoverToError(t *testing.T) {
	var err error
	func() {
		defer recoverToError("X", &err)
		panic("boom")
	}()
	if err == nil {
		t.Fatal("expected a recovered error, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error to contain %q, got %q", "boom", err.Error())
	}
}

// TestRecoverToErrorDoesNotOverwrite verifies that recoverToError leaves an
// already-set error unchanged (only sets it when nil at recovery time).
func TestRecoverToErrorDoesNotOverwrite(t *testing.T) {
	sentinel := errors.New("sentinel error")
	var err error
	err = sentinel
	func() {
		defer recoverToError("Y", &err)
		panic("ignored")
	}()
	if err != sentinel {
		t.Fatalf("recoverToError overwrote a pre-existing error; got %v", err)
	}
}

// TestRecoverToErrorNilPointer verifies that passing a nil errp does not itself
// panic (safe no-op).
func TestRecoverToErrorNilPointer(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("recoverToError with nil errp caused a secondary panic: %v", r)
		}
	}()
	func() {
		defer recoverToError("Z", nil)
		panic("nilptrtest")
	}()
}
