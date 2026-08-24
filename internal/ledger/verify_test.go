// ABOUTME: Tests layered PACT verification over real immutable object files.
// ABOUTME: Covers bytes, signatures, DAG references, trust, and event inspection.
package ledger

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

func TestVerifyPreservesPartialResultWhenTrustEvaluationFails(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitOne(t, st, key, "a", nil)
	if err := os.WriteFile(filepath.Join(st.Dir(), "trust.json"), []byte(`{"format":"wrong","roots":[]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Verify(st, true)
	if err != nil {
		t.Fatalf("Verify() error = %v, want partial result", err)
	}
	authorityError := "authority evaluation failed: ledger store failure: malformed local trust file"
	if result.OK || result.Counts.Objects != 1 || result.Counts.Commits != 1 || result.Counts.Events != 1 {
		t.Fatalf("Verify() counts = %#v, ok=%v", result.Counts, result.OK)
	}
	if object := result.Objects[commit.ObjectID]; !object.Valid() {
		t.Fatalf("verified object = %#v", object)
	}
	if heads := result.Heads["org/example/widget"]; len(heads) != 1 || heads[0] != commit.ObjectID {
		t.Fatalf("heads = %#v", result.Heads)
	}
	if !sort.StringsAreSorted(result.Errors) || len(result.Errors) != 1 || result.Errors[0] != authorityError {
		t.Fatalf("errors = %#v, want sorted authority error", result.Errors)
	}
	if len(result.Integrity.Errors) != 0 || len(result.Structure.Errors) != 0 || len(result.Authenticity.Errors) != 0 || len(result.DAG.Errors) != 0 || len(result.References.Errors) != 0 {
		t.Fatalf("verification layers = %#v", result)
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

func TestShowReturnsTypedIntegrityDetailsForCorruptCanonicalBytes(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitOne(t, st, key, "a", nil)
	files, err := st.ObjectFiles()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[0].Path, []byte(`{"bad":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Show(st, commit.ObjectID)
	var showError *ShowError
	if !errors.Is(err, ErrIntegrity) || !errors.As(err, &showError) {
		t.Fatalf("Show() error = %v, want typed integrity details", err)
	}
	if showError.Result.Identifier != commit.ObjectID || showError.Result.Integrity != "invalid" || !strings.Contains(strings.Join(showError.Result.Errors, " "), "digest mismatch") {
		t.Fatalf("ShowError result = %#v", showError.Result)
	}
}

func TestShowReturnsTypedIntegrityDetailsForUnparseableCanonicalBytes(t *testing.T) {
	st, _ := ledgerStoreAndKey(t)
	raw := []byte(`{"unterminated":`)
	id := canonical.Digest(raw)
	hexID := strings.TrimPrefix(id, "sha256:")
	path := filepath.Join(st.Dir(), "objects", "sha256", hexID[:2], hexID[2:]+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Show(st, id)
	var showError *ShowError
	if !errors.Is(err, ErrIntegrity) || !errors.As(err, &showError) || showError.Result.Integrity != "invalid" || len(showError.Result.Errors) == 0 {
		t.Fatalf("Show() error = %#v, details = %#v", err, showError)
	}
}

func TestShowReturnsTypedIntegrityDetailsForUnreadableCanonicalBytes(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitOne(t, st, key, "a", nil)
	files, err := st.ObjectFiles()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(files[0].Path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(files[0].Path, 0o644) })
	_, err = Show(st, commit.ObjectID)
	var showError *ShowError
	if !errors.Is(err, ErrIntegrity) || !errors.As(err, &showError) || !strings.Contains(strings.Join(showError.Result.Errors, " "), "cannot read object") {
		t.Fatalf("Show() error = %#v, details = %#v", err, showError)
	}
}

func TestShowAllowsInspectionWhenOnlySignatureAuthenticityFails(t *testing.T) {
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
	object["signature"].(map[string]any)["value"] = base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	id, _, err := st.PutCanonical(object)
	if err != nil {
		t.Fatal(err)
	}
	shown, err := Show(st, id)
	if err != nil || shown.Object == nil || shown.Integrity != "valid" || shown.Authenticity != "invalid" {
		t.Fatalf("Show() = (%#v, %v), want parsed object with invalid authenticity", shown, err)
	}
}

func TestShowMissingObjectUsesTypedDependencyError(t *testing.T) {
	st, _ := ledgerStoreAndKey(t)
	_, err := Show(st, "sha256:"+strings.Repeat("a", 64))
	if !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("Show() error = %v, want missing dependency", err)
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

func TestVerifyMarksCanonicalNonObjectStructureInvalid(t *testing.T) {
	st, _ := ledgerStoreAndKey(t)
	if _, _, err := st.PutCanonical([]any{"not", "an", "object"}); err != nil {
		t.Fatal(err)
	}
	result, err := Verify(st, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range result.Objects {
		if object.Structure != "invalid" || result.Counts.Structure != 1 || len(result.Structure.Errors) != 1 {
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

func TestVerifyRejectsCheckpointWithMissingOrWrongNamespaceHeads(t *testing.T) {
	for _, test := range []struct {
		name      string
		namespace string
		head      func(CommitResult) string
		want      string
	}{
		{name: "missing head", namespace: "scope", head: func(CommitResult) string {
			return "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		}, want: "missing checkpoint head"},
		{name: "wrong namespace", namespace: "scope/other", head: func(commit CommitResult) string { return commit.ObjectID }, want: "namespace mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			st, key := ledgerStoreAndKey(t)
			commit := commitInNamespace(t, st, key, "scope", "event")
			checkpointID := putSignedCheckpointForVerify(t, st, key, test.namespace, test.head(commit), "")
			result, err := Verify(st, true)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(result.References.Errors, " ")
			if result.OK || !strings.Contains(joined, checkpointID) || !strings.Contains(joined, test.want) {
				t.Fatalf("Verify() = %#v", result)
			}
		})
	}
}

func TestVerifyRejectsUnavailablePreviousCheckpoint(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitInNamespace(t, st, key, "scope", "event")
	missing := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	checkpointID := putSignedCheckpointForVerify(t, st, key, "scope", commit.ObjectID, missing)
	loose, err := Verify(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if !loose.OK || len(loose.References.Errors) != 0 || !strings.Contains(strings.Join(loose.References.Warnings, " "), checkpointID+": previous checkpoint is unavailable") {
		t.Fatalf("loose Verify() = %#v", loose)
	}
	result, err := Verify(st, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || !strings.Contains(strings.Join(result.References.Errors, " "), checkpointID+": previous checkpoint is unavailable") {
		t.Fatalf("Verify() = %#v", result)
	}
}

func TestVerifyRejectsNoncanonicalCheckpointBody(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitInNamespace(t, st, key, "scope", "event")
	body := checkpointBodyFixture(key, "scope", commit.ObjectID, "")
	body["schema_refs"] = []any{testSchemaA, testSchemaA}
	digest, signature, err := identity.SignBody(body, key.Private)
	if err != nil {
		t.Fatal(err)
	}
	object := map[string]any{"format": checkpointFormat, "body": body, "body_digest": digest, "signature": map[string]any{"algorithm": "ed25519", "key_id": key.KeyID, "public_key": base64.RawURLEncoding.EncodeToString(key.Public), "value": base64.RawURLEncoding.EncodeToString(signature)}}
	if _, _, err := st.PutCanonical(object); err != nil {
		t.Fatal(err)
	}
	result, err := Verify(st, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Counts.Structure != 1 || !strings.Contains(strings.Join(result.Structure.Errors, " "), "schema_refs are not canonical") {
		t.Fatalf("Verify() = %#v", result)
	}
}

func TestVerifyRejectsCheckpointFrontierOutsideBodyScope(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitInNamespace(t, st, key, "outside", "event")
	putSignedCheckpointForVerify(t, st, key, "outside", commit.ObjectID, "")
	result, err := Verify(st, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Counts.Structure != 1 || !strings.Contains(strings.Join(result.Structure.Errors, " "), `checkpoint namespace "outside" is outside scope "scope"`) {
		t.Fatalf("Verify() = %#v", result)
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

func putSignedCheckpointForVerify(t *testing.T, st *store.Store, key *identity.KeyFile, namespace, head, previous string) string {
	t.Helper()
	body := checkpointBodyFixture(key, namespace, head, previous)
	digest, signature, err := identity.SignBody(body, key.Private)
	if err != nil {
		t.Fatal(err)
	}
	object := map[string]any{"format": checkpointFormat, "body": body, "body_digest": digest, "signature": map[string]any{"algorithm": "ed25519", "key_id": key.KeyID, "public_key": base64.RawURLEncoding.EncodeToString(key.Public), "value": base64.RawURLEncoding.EncodeToString(signature)}}
	id, _, err := st.PutCanonical(object)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func checkpointBodyFixture(key *identity.KeyFile, namespace, head, previous string) map[string]any {
	var previousValue any
	if previous != "" {
		previousValue = previous
	}
	return map[string]any{
		"scope": "scope", "frontier": []any{map[string]any{"namespace": namespace, "heads": []any{head}}},
		"policy_ref": testPolicyRef, "schema_refs": []any{}, "authority_epoch": "epoch-1",
		"previous_checkpoint": previousValue, "actor": map[string]any{"key_id": key.KeyID, "label": key.Actor},
		"observed_at": "2026-08-23T12:00:00Z", "metadata": map[string]any{},
	}
}

func TestVerifyGraphDiagnosticsPublishStableIterativeCycleWitnesses(t *testing.T) {
	commits := map[string]CommitRecord{
		"a": {ID: "a", Namespace: "scope", Parents: []string{"b"}, EventRefs: []string{"event:a"}},
		"b": {ID: "b", Namespace: "scope", Parents: []string{"a"}, EventRefs: []string{"event:b"}},
	}
	events := map[string]EventRecord{
		"event:a": {Ref: "event:a", CommitID: "a", Namespace: "scope", CausedBy: []string{"event:b"}},
		"event:b": {Ref: "event:b", CommitID: "b", Namespace: "scope", CausedBy: []string{"event:a"}},
	}
	graph, err := analyzeGraph(context.Background(), commits, events, Phase2Limits)
	if err != nil {
		t.Fatal(err)
	}
	result := VerifyResult{diagnosticLimits: Phase2Limits}
	applyGraphVerification(&result, graph)
	want := []string{
		"caused_by cycle: event:a -> event:b -> event:a",
		"commit DAG cycle: a -> b -> a",
	}
	if !reflect.DeepEqual(result.DAG.Errors, want) || !reflect.DeepEqual(result.Errors, want) || result.Counts.DAG != 2 {
		t.Fatalf("Verify graph diagnostics = %#v", result)
	}
}

func TestVerifyReportsStableCausedByCycleFromSignedCommit(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	batch, err := NormalizeEventBatch(map[string]any{"events": []any{
		eventInput("a", []any{}, []any{"local:b"}),
		eventInput("b", []any{}, []any{"local:a"}),
	}})
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
	want := "caused_by cycle: " + EventRef(commit.ObjectID, "a") + " -> " + EventRef(commit.ObjectID, "b") + " -> " + EventRef(commit.ObjectID, "a")
	if result.OK || !slices.Contains(result.DAG.Errors, want) || result.Counts.DAG != 1 {
		t.Fatalf("Verify() DAG = %#v, want %q", result.DAG, want)
	}
}
