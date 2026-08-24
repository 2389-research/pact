// ABOUTME: Tests layered PACT verification over real immutable object files.
// ABOUTME: Covers bytes, signatures, DAG references, trust, and event inspection.
package ledger

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pact/internal/canonical"
	"pact/internal/identity"
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
	if result.OK || result.Objects[commit.ObjectID].Integrity != "invalid" || !strings.Contains(strings.Join(result.Errors, " "), "object digest mismatch") {
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
	if !loose.OK || len(loose.Warnings) != 1 {
		t.Fatalf("loose Verify() = %#v", loose)
	}
	strict, err := Verify(st, true)
	if err != nil {
		t.Fatal(err)
	}
	if strict.OK || len(strict.Errors) != 1 {
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
	if result.OK || result.Counts.Objects != 2 {
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
