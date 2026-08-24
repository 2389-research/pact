// ABOUTME: Tests PACT signed-commit admission, parent selection, and head computation.
// ABOUTME: Uses real stores and Ed25519 keys so persisted bytes remain the contract.
package ledger

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"pact/internal/canonical"
	"pact/internal/identity"
	"pact/internal/store"
)

func TestCommitSignsNormalizedBatchAndExpandsLocalRefsInResult(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	batch, err := NormalizeEventBatch(map[string]any{"events": []any{eventInput("b", []any{}, []any{"local:a"}), eventInput("a", []any{}, []any{})}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Commit(st, key, batch, CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Namespace != "org/example/widget" || len(result.EventRefs) != 2 || result.EventRefs[0] != EventRef(result.ObjectID, "a") {
		t.Fatalf("Commit() = %#v", result)
	}
	raw, err := st.Get(result.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	value, err := canonical.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	object := value.(map[string]any)
	if got := object["body"].(map[string]any)["metadata"].(map[string]any)["producer"]; got != "pact-reference-cli/0.1.0" {
		t.Fatalf("producer = %#v", got)
	}
	events := object["body"].(map[string]any)["events"].([]any)
	if got := events[1].(map[string]any)["caused_by"].([]any)[0]; got != "local:a" {
		t.Fatalf("stored caused_by = %#v, want local ref", got)
	}
}

func TestCommitDefaultsToAllExactNamespaceHeadsAndAllowsExplicitFork(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	first := commitOne(t, st, key, "a", nil)
	second := commitOne(t, st, key, "b", []string{first.ObjectID})
	fork := commitOne(t, st, key, "c", []string{first.ObjectID})
	merge := commitOne(t, st, key, "d", nil)
	want := []string{fork.ObjectID, second.ObjectID}
	sort.Strings(want)
	if got := merge.Parents; !equalStrings(got, want) {
		t.Fatalf("default parents = %q, want %q", got, want)
	}
	heads, err := Heads(st, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := heads["org/example/widget"]; !equalStrings(got, []string{merge.ObjectID}) {
		t.Fatalf("heads = %#v", heads)
	}
}

func TestCommitRejectsUnavailableOrCrossNamespaceParents(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	batch, err := NormalizeEventBatch(map[string]any{"events": []any{eventInput("a", []any{}, []any{})}})
	if err != nil {
		t.Fatal(err)
	}
	missing := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := Commit(st, key, batch, CommitOptions{Parents: []string{missing}, ObservedAt: "2026-08-23T12:00:00Z"}); err == nil {
		t.Fatal("Commit() missing parent error = nil")
	}
	other, err := Commit(st, key, batch, CommitOptions{Namespace: "other/widget", ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(st, key, batch, CommitOptions{Parents: []string{other.ObjectID}, ObservedAt: "2026-08-23T12:00:00Z"}); err == nil {
		t.Fatal("Commit() cross namespace parent error = nil")
	}
}

func TestCommitPreflightRejectsMutatedBatchBeforePersistence(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	batch, err := NormalizeEventBatch(map[string]any{"events": []any{eventInput("a", []any{}, []any{})}})
	if err != nil {
		t.Fatal(err)
	}
	batch.Events = nil
	if _, err := Commit(st, key, batch, CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"}); err == nil {
		t.Fatal("Commit() error = nil")
	}
	files, err := st.ObjectFiles()
	if err != nil || len(files) != 0 {
		t.Fatalf("objects after rejected write = %#v, %v", files, err)
	}
	batch, err = NormalizeEventBatch(map[string]any{"events": []any{eventInput("a", []any{}, []any{})}})
	if err != nil {
		t.Fatal(err)
	}
	batch.Events[0].CausedBy = []string{"local:gone"}
	if _, err := Commit(st, key, batch, CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"}); err == nil {
		t.Fatal("Commit() local-ref error = nil")
	}
	if files, err := st.ObjectFiles(); err != nil || len(files) != 0 {
		t.Fatalf("objects after invalid ref = %#v, %v", files, err)
	}
	if _, err := Commit(st, key, mustBatch(t, "ok"), CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"}); err != nil {
		t.Fatalf("next valid Commit() = %v", err)
	}
}

func TestCommitPreflightRejectsInvalidKeyAndDeduplicatesExplicitParents(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	bad := *key
	bad.Actor = strings.Repeat("界", 256)
	if _, err := Commit(st, &bad, mustBatch(t, "a"), CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"}); err == nil {
		t.Fatal("Commit() actor error = nil")
	}
	if files, err := st.ObjectFiles(); err != nil || len(files) != 0 {
		t.Fatalf("objects after bad key = %#v, %v", files, err)
	}
	first := commitOne(t, st, key, "first", nil)
	result, err := Commit(st, key, mustBatch(t, "second"), CommitOptions{Parents: []string{first.ObjectID, first.ObjectID}, ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil || !equalStrings(result.Parents, []string{first.ObjectID}) {
		t.Fatalf("Commit() = %#v, %v", result, err)
	}
}

func TestHeadsAndCommitRefuseInvalidCanonicalHistory(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	if _, _, err := st.PutCanonical(map[string]any{"bad": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Heads(st, ""); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("Heads() error = %v", err)
	}
	if _, err := Commit(st, key, mustBatch(t, "a"), CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"}); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("Commit() error = %v", err)
	}
}

func mustBatch(t *testing.T, localID string) EventBatch {
	t.Helper()
	batch, err := NormalizeEventBatch(map[string]any{"events": []any{eventInput(localID, []any{}, []any{})}})
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func ledgerStoreAndKey(t *testing.T) (*store.Store, *identity.KeyFile) {
	t.Helper()
	st, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key, err := identity.GenerateKeyFile(t.TempDir()+"/alice.key.json", "Alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return st, key
}

func commitOne(t *testing.T, st *store.Store, key *identity.KeyFile, localID string, parents []string) CommitResult {
	t.Helper()
	batch, err := NormalizeEventBatch(map[string]any{"events": []any{eventInput(localID, []any{}, []any{})}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Commit(st, key, batch, CommitOptions{Parents: parents, ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
