// ABOUTME: Tests out-of-band local trust bootstrap backed by real store configuration.
// ABOUTME: Ensures root additions are idempotent and conflicting key bytes fail closed.
package ledger

import (
	"crypto/ed25519"
	"testing"
	"time"

	"pact/internal/identity"
	"pact/internal/store"
)

func TestAddRootIsIdempotent(t *testing.T) {
	st, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key, err := identity.GenerateKeyFile(t.TempDir()+"/alice.key.json", "Alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	created, err := AddRoot(st, key, time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC))
	if err != nil || !created {
		t.Fatalf("first AddRoot() = (%v, %v), want (true, nil)", created, err)
	}
	created, err = AddRoot(st, key, time.Now())
	if err != nil || created {
		t.Fatalf("second AddRoot() = (%v, %v), want (false, nil)", created, err)
	}
	roots, err := Roots(st)
	if err != nil || roots[key.KeyID].Actor != "Alice" {
		t.Fatalf("Roots() = (%#v, %v)", roots, err)
	}
}

func TestAddRootRejectsConflictingPublicBytes(t *testing.T) {
	st, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key, err := identity.GenerateKeyFile(t.TempDir()+"/alice.key.json", "Alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddRoot(st, key, time.Now()); err != nil {
		t.Fatal(err)
	}
	conflict := &identity.KeyFile{Actor: "Mallory", KeyID: key.KeyID, Public: make(ed25519.PublicKey, ed25519.PublicKeySize)}
	if _, err := AddRoot(st, conflict, time.Now()); err == nil {
		t.Fatal("AddRoot() error = nil, want conflicting public bytes refusal")
	}
}
