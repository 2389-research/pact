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
	result, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st := result.Store
	generated, err := identity.GenerateKeyFile(t.TempDir()+"/alice.key.json", "Alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key := generated.Key
	rootResult, err := AddRoot(st, key, time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC))
	if err != nil || rootResult.Status != RootCreated || rootResult.Root.KeyID != key.KeyID {
		t.Fatalf("first AddRoot() = (%#v, %v), want created root", rootResult, err)
	}
	existingKey := *key
	existingKey.Actor = "Changed label does not change public identity"
	rootResult, err = AddRoot(st, &existingKey, time.Now())
	if err != nil || rootResult.Status != RootExisting || rootResult.Root.Actor != "Alice" {
		t.Fatalf("second AddRoot() = (%#v, %v), want existing stored root", rootResult, err)
	}
	roots, err := Roots(st)
	if err != nil || roots[key.KeyID].Actor != "Alice" {
		t.Fatalf("Roots() = (%#v, %v)", roots, err)
	}
}

func TestAddRootRejectsConflictingPublicBytes(t *testing.T) {
	result, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st := result.Store
	generated, err := identity.GenerateKeyFile(t.TempDir()+"/alice.key.json", "Alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key := generated.Key
	if _, err := AddRoot(st, key, time.Now()); err != nil {
		t.Fatal(err)
	}
	conflict := &identity.KeyFile{Actor: "Mallory", KeyID: key.KeyID, Public: make(ed25519.PublicKey, ed25519.PublicKeySize)}
	if rootResult, err := AddRoot(st, conflict, time.Now()); err == nil || !errors.Is(err, ErrIntegrity) || rootResult.Status != "" {
		t.Fatalf("AddRoot(conflict) = (%#v, %v), want integrity error without status", rootResult, err)
	}
}

func TestAddRootPreservesCreatedResultThroughOperationAndLockReleaseFailure(t *testing.T) {
	result, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st := result.Store
	generated, err := identity.GenerateKeyFile(filepath.Join(t.TempDir(), "alice.key.json"), "Alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	operationFault := errors.New("injected post-rename sync failure")
	releaseFault := errors.New("injected lock release failure")
	oldWrite := writeTrustJSON
	oldLock := withTrustMutationLock
	writeTrustJSON = func(st *store.Store, name string, value any, mode os.FileMode) error {
		if err := st.WriteLocalJSON(name, value, mode); err != nil {
			return err
		}
		return publishedTrustWriteError{err: operationFault}
	}
	withTrustMutationLock = func(st *store.Store, operation func() error) error {
		return &store.LockError{Operation: operation(), Release: releaseFault}
	}
	t.Cleanup(func() {
		writeTrustJSON = oldWrite
		withTrustMutationLock = oldLock
	})

	rootResult, err := AddRoot(st, generated.Key, time.Now())
	if rootResult.Status != RootCreated || rootResult.Root.KeyID != generated.Key.KeyID || !errors.Is(err, operationFault) || !errors.Is(err, releaseFault) {
		t.Fatalf("AddRoot() = (%#v, %v), want created root and both errors", rootResult, err)
	}
	writeTrustJSON = oldWrite
	withTrustMutationLock = oldLock
	roots, rootsErr := Roots(st)
	if rootsErr != nil || roots[generated.Key.KeyID].PublicKey != base64.RawURLEncoding.EncodeToString(generated.Key.Public) {
		t.Fatalf("Roots() after published error = (%#v, %v)", roots, rootsErr)
	}
}

type publishedTrustWriteError struct{ err error }

func (err publishedTrustWriteError) Error() string              { return err.err.Error() }
func (err publishedTrustWriteError) Unwrap() error              { return err.err }
func (err publishedTrustWriteError) ReplacementPublished() bool { return true }

func TestAddRootRefusesTrustFileWithoutExplicitRoots(t *testing.T) {
	result, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st := result.Store
	generated, err := identity.GenerateKeyFile(filepath.Join(t.TempDir(), "alice.key.json"), "Alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key := generated.Key
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
	result, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st := result.Store
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
