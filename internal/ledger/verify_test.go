// ABOUTME: Tests layered PACT verification over real immutable object files.
// ABOUTME: Covers bytes, signatures, DAG references, trust, and event inspection.
package ledger

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"pact/internal/canonical"
	"pact/internal/identity"
	"pact/internal/store"
)

func TestVerifySeparatesAuthorizationFromAuthenticityAndShowExpandsEvent(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	batch, err := NormalizeEventBatch(map[string]any{"events": []any{eventInput("a", []any{}, []any{})}})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := Commit(st, key, batch, CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Verify(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Counts.Indeterminate != 1 || result.Counts.Authorized != 0 {
		t.Fatalf("Verify() = %#v", result)
	}
	if auth := result.Authorization[commit.ObjectID]; len(auth.Chain) != 0 || auth.LeaseStatus != "not_applicable" || auth.Depth != 0 {
		t.Fatalf("unknown authorization = %#v", auth)
	}
	if _, err := AddRoot(st, key, time.Now()); err != nil {
		t.Fatal(err)
	}
	result, err = Verify(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Counts.Authorized != 1 {
		t.Fatalf("trusted Verify() = %#v", result)
	}
	if auth := result.Authorization[commit.ObjectID]; !equalStrings(auth.Chain, []string{key.KeyID}) || auth.LeaseStatus != "not_applicable" || auth.Depth != 0 {
		t.Fatalf("root authorization = %#v", auth)
	}
	shown, err := Show(st, EventRef(commit.ObjectID, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if shown.Kind != "event" || shown.Event["local_id"] != "a" || shown.Integrity != "valid" || shown.Authenticity != "valid" {
		t.Fatalf("Show() = %#v", shown)
	}
}

func TestVerifyReportsCanonicalPathDigestMismatch(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitOne(t, st, key, "a", nil)
	files, err := st.ObjectFiles()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[0].Path, []byte(`{"bad":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Verify(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Objects[commit.ObjectID].Integrity != "invalid" || len(result.Integrity.Errors) != 1 || len(result.Structure.Errors) != 0 || len(result.Authenticity.Errors) != 0 || !strings.Contains(strings.Join(result.Errors, " "), "object digest mismatch") {
		t.Fatalf("Verify() = %#v", result)
	}
}

func TestVerifyStrictEscalatesMissingExternalReferences(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	missing := "pact:event:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa#old"
	batch, err := NormalizeEventBatch(map[string]any{"events": []any{eventInput("a", []any{}, []any{})}})
	if err != nil {
		t.Fatal(err)
	}
	batch.Events[0].Supersedes = []string{missing}
	if _, err := Commit(st, key, batch, CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	loose, err := Verify(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if !loose.OK || len(loose.Warnings) != 1 || len(loose.References.Warnings) != 1 || len(loose.DAG.Errors) != 0 {
		t.Fatalf("loose Verify() = %#v", loose)
	}
	strict, err := Verify(st, true)
	if err != nil {
		t.Fatal(err)
	}
	if strict.OK || len(strict.Errors) != 1 || len(strict.References.Errors) != 1 || len(strict.Integrity.Errors) != 0 {
		t.Fatalf("strict Verify() = %#v", strict)
	}
}

func TestVerifyRejectsSignatureSubstitution(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitOne(t, st, key, "a", nil)
	other, err := identity.GenerateKeyFile(filepath.Join(t.TempDir(), "other.key.json"), "Other", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := st.Get(commit.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := canonical.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	value := parsed.(map[string]any)
	value["signature"].(map[string]any)["public_key"] = base64.RawURLEncoding.EncodeToString(other.Public)
	if _, _, err := st.PutCanonical(value); err != nil {
		t.Fatal(err)
	}
	result, err := Verify(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Counts.Objects != 2 || len(result.Authenticity.Errors) != 1 || len(result.Structure.Errors) != 0 {
		t.Fatalf("Verify() = %#v", result)
	}
}

func TestVerifyReportsMalformedSignatureFieldsWithoutPanicking(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitOne(t, st, key, "a", nil)
	raw, err := st.Get(commit.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := canonical.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	object := parsed.(map[string]any)
	object["body_digest"] = int64(1)
	if _, _, err := st.PutCanonical(object); err != nil {
		t.Fatal(err)
	}
	result, err := Verify(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || !strings.Contains(strings.Join(result.Errors, " "), "body digest is malformed") {
		t.Fatalf("Verify() = %#v", result)
	}
}

func TestVerifyReportsActorSignatureMismatch(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitOne(t, st, key, "a", nil)
	other, err := identity.GenerateKeyFile(filepath.Join(t.TempDir(), "other.key.json"), "Other", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := st.Get(commit.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := canonical.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	object := parsed.(map[string]any)
	object["body"].(map[string]any)["actor"].(map[string]any)["key_id"] = other.KeyID
	if _, _, err := st.PutCanonical(object); err != nil {
		t.Fatal(err)
	}
	result, err := Verify(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(result.Authenticity.Errors, " "), "body actor key ID") {
		t.Fatalf("Verify() = %#v", result)
	}
}

func TestCorrectionCommitLeavesOriginalObjectBytesImmutable(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	first := commitOne(t, st, key, "a", nil)
	before, err := st.Get(first.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	batch := mustBatch(t, "correction")
	batch.Events[0].Supersedes = []string{EventRef(first.ObjectID, "a")}
	if _, err := Commit(st, key, batch, CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	after, err := st.Get(first.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("correction changed original immutable bytes")
	}
}

func TestVerifyMarksFailedCommitStructureInvalid(t *testing.T) {
	st, _ := ledgerStoreAndKey(t)
	if _, _, err := st.PutCanonical(map[string]any{"format": commitFormat, "body": map[string]any{}, "body_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "signature": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	result, err := Verify(st, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range result.Objects {
		if object.Structure != "invalid" || len(result.Structure.Errors) != 1 {
			t.Fatalf("verification = %#v", result)
		}
	}
}

func TestVerifyReportsMissingAndCrossNamespaceParentsInDAGLayer(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	missing := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	missingID := putSignedCommitForVerify(t, st, key, "org/example/widget", []string{missing})
	other := commitOne(t, st, key, "other", nil)
	crossID := putSignedCommitForVerify(t, st, key, "different/widget", []string{other.ObjectID})
	result, err := Verify(st, true)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(result.DAG.Errors, " ")
	if !strings.Contains(joined, missingID+": missing or invalid parent") || !strings.Contains(joined, crossID+": parent "+other.ObjectID+" belongs to different namespace") {
		t.Fatalf("DAG = %#v", result.DAG)
	}
	if !sort.StringsAreSorted(result.DAG.Errors) || !sort.StringsAreSorted(result.Errors) {
		t.Fatalf("verification layers are not sorted: %#v", result)
	}
}

func TestCommitCyclesDetectsCycleFixture(t *testing.T) {
	commits := map[string]storedCommit{
		"a": {body: map[string]any{"parents": []any{"b"}}},
		"b": {body: map[string]any{"parents": []any{"a"}}},
	}
	cycles := commitCycles(commits)
	if len(cycles) != 1 || !equalStrings(cycles[0], []string{"a", "b", "a"}) {
		t.Fatalf("commitCycles() = %#v", cycles)
	}
}

func putSignedCommitForVerify(t *testing.T, st *store.Store, key *identity.KeyFile, namespace string, parents []string) string {
	t.Helper()
	body := map[string]any{"namespace": namespace, "parents": stringsToAny(parents), "actor": map[string]any{"key_id": key.KeyID, "label": key.Actor}, "authority": map[string]any{"delegation_ref": nil, "epoch": nil, "lease_ref": nil}, "observed_at": "2026-08-23T12:00:00Z", "metadata": map[string]any{}, "events": batchEvents(mustBatch(t, "fixture").Events)}
	digest, signature, err := identity.SignBody(body, key.Private)
	if err != nil {
		t.Fatal(err)
	}
	object := map[string]any{"format": commitFormat, "body": body, "body_digest": digest, "signature": map[string]any{"algorithm": "ed25519", "key_id": key.KeyID, "public_key": base64.RawURLEncoding.EncodeToString(key.Public), "value": base64.RawURLEncoding.EncodeToString(signature)}}
	id, _, err := st.PutCanonical(object)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
