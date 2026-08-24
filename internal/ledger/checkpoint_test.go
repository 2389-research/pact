// ABOUTME: Tests trusted-root checkpoint creation over real signed commit history.
// ABOUTME: Covers canonical frontiers, strict preflight checks, chaining, and inspection.
package ledger

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"pact/internal/canonical"
	"pact/internal/identity"
	"pact/internal/store"
)

const (
	testPolicyRef = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testSchemaA   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testSchemaB   = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func TestCheckpointSelectsCanonicalScopedFrontierAndSchemaRefs(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	if _, err := AddRoot(st, key, time.Now()); err != nil {
		t.Fatal(err)
	}
	exact := commitInNamespace(t, st, key, "scope", "exact")
	childFirst := commitInNamespace(t, st, key, "scope/child", "child-first")
	childSecond := commitInNamespace(t, st, key, "scope/child", "child-second")
	outside := commitInNamespace(t, st, key, "scope-other", "outside")

	result, err := Checkpoint(st, key, CheckpointOptions{
		Scope:          "scope",
		PolicyRef:      testPolicyRef,
		AuthorityEpoch: "epoch-1",
		SchemaRefs:     []string{testSchemaB, testSchemaA, testSchemaB},
		Purpose:        "release cut",
		ObservedAt:     "2026-08-23T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []CheckpointFrontier{
		{Namespace: "scope", Heads: []string{exact.ObjectID}},
		{Namespace: "scope/child", Heads: []string{childSecond.ObjectID}},
	}
	if !equalFrontier(result.Frontier, want) {
		t.Fatalf("frontier = %#v, want %#v", result.Frontier, want)
	}
	if !equalStrings(result.SchemaRefs, []string{testSchemaA, testSchemaB}) {
		t.Fatalf("schema refs = %#v", result.SchemaRefs)
	}
	if result.Authorization != "authorized" || result.Integrity != "valid" || result.Authenticity != "valid" {
		t.Fatalf("Checkpoint() = %#v", result)
	}
	if result.ObjectID == exact.ObjectID || result.ObjectID == childFirst.ObjectID || result.ObjectID == outside.ObjectID {
		t.Fatalf("checkpoint ID collided with commit: %#v", result)
	}
	shown, err := Show(st, result.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if shown.Kind != "checkpoint" || shown.Integrity != "valid" || shown.Authenticity != "valid" {
		t.Fatalf("Show(checkpoint) = %#v", shown)
	}
	body := shown.Object["body"].(map[string]any)
	metadata := body["metadata"].(map[string]any)
	if body["scope"] != "scope" || metadata["purpose"] != "release cut" || metadata["producer"] != "pact-reference-cli/0.1.0" {
		t.Fatalf("checkpoint body = %#v", body)
	}
}

func TestCheckpointWireVector(t *testing.T) {
	fixedTime := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	st, err := store.Init(t.TempDir(), "scope", fixedTime)
	if err != nil {
		t.Fatal(err)
	}
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index)
	}
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)
	keyID, err := identity.KeyID(public)
	if err != nil {
		t.Fatal(err)
	}
	key := &identity.KeyFile{Actor: "Vector Signer", KeyID: keyID, Public: public, Private: private, CreatedAt: fixedTime}
	if _, err := AddRoot(st, key, fixedTime); err != nil {
		t.Fatal(err)
	}
	commitInNamespace(t, st, key, "scope", "vector")
	result, err := Checkpoint(st, key, CheckpointOptions{
		Scope: "scope", PolicyRef: testPolicyRef, AuthorityEpoch: "epoch-1",
		SchemaRefs: []string{testSchemaB, testSchemaA}, Purpose: "wire vector",
		ObservedAt: "2026-08-23T12:01:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := st.Get(result.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := canonical.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	object := parsed.(map[string]any)
	bodyDigest := object["body_digest"].(string)
	signatureText := object["signature"].(map[string]any)["value"].(string)
	const wantBodyDigest = "sha256:fc13630aefeace3701c90a56e03eeb1a8045e724073f49ca9df2d16103c5b76b"
	const wantSignature = "KPwyvoHdZprhgx9Lnq9YCRW7z5VRMLOehwHZsPcDaTqx1rSPl7uG5de0N_Q1U9cpzMFw9P7p96EEqq8L6rM3AQ"
	const wantObjectID = "sha256:93b573cd8fa5ac44c16a1f060c16ff052d57ac4b2aca2e3e6915fc032a53cda5"
	if bodyDigest != wantBodyDigest || signatureText != wantSignature || result.ObjectID != wantObjectID {
		t.Fatalf("checkpoint wire vector:\nbody_digest=%q\nsignature=%q\nobject_id=%q", bodyDigest, signatureText, result.ObjectID)
	}
	signature, err := base64.RawURLEncoding.DecodeString(signatureText)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.VerifyBody(object["body"], bodyDigest, public, signature); err != nil {
		t.Fatalf("wire signature: %v", err)
	}
	if canonical.Digest(raw) != result.ObjectID {
		t.Fatalf("wire object digest = %s, want %s", canonical.Digest(raw), result.ObjectID)
	}
}

func TestCheckpointRequiresTrustedRootBeforePersistence(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commitInNamespace(t, st, key, "scope", "event")
	before := objectCount(t, st)
	_, err := Checkpoint(st, key, validCheckpointOptions())
	if err == nil || !strings.Contains(err.Error(), "trusted root") {
		t.Fatalf("Checkpoint() error = %v", err)
	}
	if got := objectCount(t, st); got != before {
		t.Fatalf("object count = %d, want %d", got, before)
	}
}

func TestCheckpointAdmitsExactObjectLimit(t *testing.T) {
	purpose := checkpointPurposeForObjectBytes(t, Phase2Limits.ObjectBytes)
	st, key := checkpointLimitFixture(t)
	before := objectCount(t, st)
	result, err := Checkpoint(st, key, checkpointOptionsWithPurpose(purpose))
	if err != nil {
		t.Fatalf("Checkpoint() error = %v", err)
	}
	if got := objectCount(t, st); got != before+1 {
		t.Fatalf("object count = %d, want %d", got, before+1)
	}
	raw, err := st.Get(result.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if got := uint64(len(raw)); got != Phase2Limits.ObjectBytes {
		t.Fatalf("checkpoint bytes = %d, want %d", got, Phase2Limits.ObjectBytes)
	}
}

func TestCheckpointRejectsFirstObjectByteOverLimitBeforePersistence(t *testing.T) {
	purpose := checkpointPurposeForObjectBytes(t, Phase2Limits.ObjectBytes+1)
	st, key := checkpointLimitFixture(t)
	before := objectCount(t, st)
	if _, err := Checkpoint(st, key, checkpointOptionsWithPurpose(purpose)); err == nil {
		t.Fatal("Checkpoint() error = nil, want object byte limit")
	} else {
		assertLimitError(t, err, "object_bytes", Phase2Limits.ObjectBytes)
	}
	if got := objectCount(t, st); got != before {
		t.Fatalf("object count after over-limit checkpoint = %d, want %d", got, before)
	}
}

func TestCheckpointRejectsEmptyScopeBadPreviousAndMalformedRefsBeforePersistence(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	if _, err := AddRoot(st, key, time.Now()); err != nil {
		t.Fatal(err)
	}
	commitInNamespace(t, st, key, "outside", "event")
	before := objectCount(t, st)
	for _, options := range []CheckpointOptions{
		validCheckpointOptions(),
		{Scope: "outside", PolicyRef: testPolicyRef, AuthorityEpoch: "epoch-1", PreviousCheckpoint: testSchemaA, ObservedAt: "2026-08-23T12:00:00Z"},
		{Scope: "outside", PolicyRef: "bad", AuthorityEpoch: "epoch-1", ObservedAt: "2026-08-23T12:00:00Z"},
		{Scope: "outside", PolicyRef: testPolicyRef, AuthorityEpoch: "", ObservedAt: "2026-08-23T12:00:00Z"},
	} {
		if _, err := Checkpoint(st, key, options); err == nil {
			t.Fatalf("Checkpoint(%#v) error = nil", options)
		}
		if got := objectCount(t, st); got != before {
			t.Fatalf("object count after rejected checkpoint = %d, want %d", got, before)
		}
	}
}

func TestCheckpointChainsOnlyToValidCheckpoint(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	if _, err := AddRoot(st, key, time.Now()); err != nil {
		t.Fatal(err)
	}
	commitInNamespace(t, st, key, "scope", "event")
	first, err := Checkpoint(st, key, validCheckpointOptions())
	if err != nil {
		t.Fatal(err)
	}
	options := validCheckpointOptions()
	options.PreviousCheckpoint = first.ObjectID
	options.ObservedAt = "2026-08-23T12:01:00Z"
	second, err := Checkpoint(st, key, options)
	if err != nil {
		t.Fatal(err)
	}
	if second.PreviousCheckpoint != first.ObjectID {
		t.Fatalf("previous = %q, want %q", second.PreviousCheckpoint, first.ObjectID)
	}
}

func TestCheckpointRefusesInvalidOrIncompleteStrictStore(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *store.Store)
	}{
		{name: "invalid object", mutate: func(t *testing.T, st *store.Store) {
			if _, _, err := st.PutCanonical(map[string]any{"bad": true}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unresolved reference", mutate: func(t *testing.T, st *store.Store) {
			// The fixture commit is added by the setup below.
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			st, key := ledgerStoreAndKey(t)
			if _, err := AddRoot(st, key, time.Now()); err != nil {
				t.Fatal(err)
			}
			if test.name == "unresolved reference" {
				batch := mustBatch(t, "event")
				batch.Events[0].Supersedes = []string{"pact:event:sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd#gone"}
				if _, err := Commit(st, key, batch, CommitOptions{Namespace: "scope", ObservedAt: "2026-08-23T12:00:00Z"}); err != nil {
					t.Fatal(err)
				}
			} else {
				commitInNamespace(t, st, key, "scope", "event")
			}
			test.mutate(t, st)
			before := objectCount(t, st)
			if _, err := Checkpoint(st, key, validCheckpointOptions()); !errors.Is(err, ErrIntegrity) {
				t.Fatalf("Checkpoint() error = %v", err)
			}
			if got := objectCount(t, st); got != before {
				t.Fatalf("object count = %d, want %d", got, before)
			}
		})
	}
}

func TestVerifyCheckpointTreatsFrontierAsHistoricalCut(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	if _, err := AddRoot(st, key, time.Now()); err != nil {
		t.Fatal(err)
	}
	first := commitInNamespace(t, st, key, "scope", "first")
	checkpoint, err := Checkpoint(st, key, validCheckpointOptions())
	if err != nil {
		t.Fatal(err)
	}
	heads, err := Heads(st, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(heads["scope"], []string{first.ObjectID}) {
		t.Fatalf("heads after checkpoint = %#v", heads)
	}
	second := commitInNamespace(t, st, key, "scope", "second")
	heads, err = Heads(st, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(heads["scope"], []string{second.ObjectID}) {
		t.Fatalf("heads after post-checkpoint commit = %#v", heads)
	}
	verified, err := Verify(st, true)
	if err != nil {
		t.Fatal(err)
	}
	if !verified.OK || verified.Counts.Checkpoints != 1 {
		t.Fatalf("Verify() = %#v", verified)
	}
	object := verified.Objects[checkpoint.ObjectID]
	if object.Type != "checkpoint" || object.Namespace != "scope" {
		t.Fatalf("checkpoint verification = %#v", object)
	}
	shown, err := Show(st, checkpoint.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	displayedHeads := shown.Object["body"].(map[string]any)["frontier"].([]any)[0].(map[string]any)["heads"].([]any)
	if len(displayedHeads) != 1 || displayedHeads[0] != first.ObjectID {
		t.Fatalf("historical frontier = %#v", displayedHeads)
	}
}

func validCheckpointOptions() CheckpointOptions {
	return CheckpointOptions{Scope: "scope", PolicyRef: testPolicyRef, AuthorityEpoch: "epoch-1", ObservedAt: "2026-08-23T12:00:00Z"}
}

func checkpointOptionsWithPurpose(purpose string) CheckpointOptions {
	options := validCheckpointOptions()
	options.Purpose = purpose
	return options
}

func checkpointLimitFixture(t *testing.T) (*store.Store, *identity.KeyFile) {
	t.Helper()
	st, key := ledgerStoreAndKey(t)
	if _, err := AddRoot(st, key, time.Now()); err != nil {
		t.Fatal(err)
	}
	commitInNamespace(t, st, key, "scope", "event")
	return st, key
}

func checkpointPurposeForObjectBytes(t *testing.T, maximum uint64) string {
	t.Helper()
	st, key := checkpointLimitFixture(t)
	result, err := Checkpoint(st, key, checkpointOptionsWithPurpose("x"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := st.Get(result.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	base := uint64(len(raw))
	if base > maximum {
		t.Fatalf("one-byte checkpoint = %d, exceeds requested size %d", base, maximum)
	}
	return strings.Repeat("x", int(maximum-base+1))
}

func commitInNamespace(t *testing.T, st *store.Store, key *identity.KeyFile, namespace, localID string) CommitResult {
	t.Helper()
	result, err := Commit(st, key, mustBatch(t, localID), CommitOptions{Namespace: namespace, ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func objectCount(t *testing.T, st *store.Store) int {
	t.Helper()
	files, err := st.ObjectFiles()
	if err != nil {
		t.Fatal(err)
	}
	return len(files)
}

func equalFrontier(got, want []CheckpointFrontier) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index].Namespace != want[index].Namespace || !equalStrings(got[index].Heads, want[index].Heads) {
			return false
		}
	}
	return true
}
