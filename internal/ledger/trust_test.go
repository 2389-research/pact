// ABOUTME: Tests out-of-band local trust bootstrap backed by real store configuration.
// ABOUTME: Ensures root additions are idempotent and conflicting key bytes fail closed.
package ledger

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pact/internal/identity"
	"pact/internal/store"
)

func TestRootsContextHonorsCancellationDuringCanonicalWork(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	roots := make([]any, 512)
	for index := range roots {
		roots[index] = map[string]any{
			"key_id": key.KeyID, "actor": "Alice", "public_key": base64.RawURLEncoding.EncodeToString(key.Public),
			"added_at": "2026-08-23T12:00:00Z",
		}
	}
	if err := st.WriteLocalJSON("trust.json", map[string]any{"format": trustFormat, "roots": roots}, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := &cancelAfterErrChecks{Context: context.Background(), cancelAt: 20}
	if _, err := RootsContext(ctx, st); !errors.Is(err, context.Canceled) {
		t.Fatalf("RootsContext() error = %v after %d checks, want context canceled", err, ctx.checks)
	}
}

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

func TestAddRootRefusesTrustFileWithoutExplicitRoots(t *testing.T) {
	st, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key, err := identity.GenerateKeyFile(filepath.Join(t.TempDir(), "alice.key.json"), "Alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(st.Dir(), "trust.json")
	before := []byte("{\"format\":\"pact/trust/v1\"}\n")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddRoot(st, key, time.Now()); err == nil {
		t.Fatal("AddRoot() error = nil, want missing roots refusal")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("trust file changed after refusal: %q, err=%v", after, err)
	}
}

func TestRootsRejectsNonStrictTrustJSON(t *testing.T) {
	st, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(st.Dir(), "trust.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, malformed := range map[string][]byte{
		"duplicate":     bytes.Replace(raw, []byte(`"format": "pact/trust/v1"`), []byte(`"format": "bad", "format": "pact/trust/v1"`), 1),
		"nfc collision": append([]byte("{\"e\\u0301\":1,\"é\":2,"), raw[1:]...),
		"BOM":           append([]byte{0xef, 0xbb, 0xbf}, raw...),
		"trailing":      append(append([]byte{}, raw...), []byte("{}")...),
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, malformed, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Roots(st); err == nil {
				t.Fatal("Roots() error = nil, want strict JSON refusal")
			}
		})
	}
}
