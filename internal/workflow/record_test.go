// ABOUTME: Verifies cooperative recording against real keys, canonical storage, and indexes.
// ABOUTME: Exercises raw-claim conflicts, cancellation, evidence gates, and lock path safety.
package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"pact/internal/canonical"
	"pact/internal/development"
	"pact/internal/identity"
	"pact/internal/ledger"
	"pact/internal/setup"
	"pact/internal/store"
)

func fixture(t *testing.T) (RecordRequest, *store.Store) {
	t.Helper()
	request := RecordRequest{Repo: t.TempDir(), KeyFile: filepath.Join(t.TempDir(), "key"), Input: development.RecordInput{OperationID: "operation", Namespace: "org/example/record", Subject: "work", Source: development.Source{Repository: "reported", Revision: strings.Repeat("a", 40)}, Events: []development.InputEvent{{LocalID: "check", Type: development.CheckExecuted, Payload: map[string]any{"name": "check", "command": []any{"never-run"}, "outcome": "passed", "exit_code": 0, "evidence_indexes": []any{0}}, Evidence: []map[string]any{{"ref": "proof", "digest": canonical.Digest([]byte("proof")), "media_type": "text/plain", "role": "primary"}}, CausedBy: []string{}, Supersedes: []string{}}}}}
	if _, err := setup.Apply(t.Context(), setup.Request{Repo: request.Repo, KeyFile: request.KeyFile, Namespace: request.Input.Namespace, Actor: "Fixture", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(request.Repo, "proof"), []byte("proof"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(request.Repo)
	if err != nil {
		t.Fatal(err)
	}
	return request, st
}

func TestRecordRefusesEvidenceBeforeAdmission(t *testing.T) {
	for _, state := range []string{"changed", "missing"} {
		t.Run(state, func(t *testing.T) {
			request, st := fixture(t)
			if state == "changed" {
				os.WriteFile(filepath.Join(request.Repo, "proof"), []byte("changed"), 0o600)
			} else {
				os.Remove(filepath.Join(request.Repo, "proof"))
			}
			result, err := Record(t.Context(), request)
			if !errors.Is(err, ErrEvidence) || result.ObjectID != "" {
				t.Fatalf("%+v %v", result, err)
			}
			files, _ := st.ObjectFiles()
			if len(files) != 0 {
				t.Fatal("admitted mismatching evidence")
			}
		})
	}
}

func TestRecordRejectsForgedRawClaimAndDuplicateClaims(t *testing.T) {
	for _, different := range []bool{true, false} {
		t.Run(map[bool]string{true: "different-facts", false: "duplicate"}[different], func(t *testing.T) {
			request, st := fixture(t)
			first, err := Record(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			batch, err := development.ValidateInput(t.Context(), request.Input)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := digestValue(t.Context(), batchFacts(batch))
			if err != nil {
				t.Fatal(err)
			}
			batch.Metadata = map[string]any{"workflow": map[string]any{"profile": development.Profile, "operation_id": request.Input.OperationID, "request_digest": digest}}
			if different {
				batch.Events[0].Payload["outcome"] = "invented"
			}
			key, err := identity.LoadSigningKey(request.KeyFile, st.Root())
			if err != nil {
				t.Fatal(err)
			}
			raw, err := ledger.Commit(st, key, batch, ledger.CommitOptions{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := Record(t.Context(), request)
			if !errors.Is(err, ErrConflict) || result.Admission != "conflict" || len(result.ConflictingObjectIDs) != 2 {
				t.Fatalf("%+v %v", result, err)
			}
			for _, id := range []string{first.ObjectID, raw.ObjectID} {
				found := false
				for _, got := range result.ConflictingObjectIDs {
					found = found || got == id
				}
				if !found {
					t.Fatalf("omitted %s", id)
				}
			}
		})
	}
}

func TestRecordSingleRawCopiedDigestIsNotExactRetry(t *testing.T) {
	request, st := fixture(t)
	batch, _ := development.ValidateInput(t.Context(), request.Input)
	digest, _ := digestValue(t.Context(), batchFacts(batch))
	batch.Metadata = map[string]any{"workflow": map[string]any{"profile": development.Profile, "operation_id": request.Input.OperationID, "request_digest": digest}}
	batch.Events[0].Payload["name"] = "other"
	key, _ := identity.LoadSigningKey(request.KeyFile, st.Root())
	raw, err := ledger.Commit(st, key, batch, ledger.CommitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Record(t.Context(), request)
	if !errors.Is(err, ErrConflict) || !reflect.DeepEqual(result.ConflictingObjectIDs, []string{raw.ObjectID}) {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestRecordCancellationWaitingForOperationLock(t *testing.T) {
	request, st := fixture(t)
	key, _ := identity.LoadSigningKey(request.KeyFile, st.Root())
	lock, err := operationLock(t.Context(), st, request.Input.Namespace, key.KeyID, request.Input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	ctx, cancel := context.WithCancel(t.Context())
	completed := make(chan error, 1)
	go func() { _, err := Record(ctx, request); completed <- err }()
	select {
	case err := <-completed:
		t.Fatalf("lock did not block: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-completed:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation blocked")
	}
	files, _ := st.ObjectFiles()
	if len(files) != 0 {
		t.Fatal("canceled request appended")
	}
}

func TestRecordCancellationAfterAdmissionPreservesIdentity(t *testing.T) {
	request, st := fixture(t)
	first, err := Record(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	pending, err := refreshIndex(ctx, st, first)
	if !errors.Is(err, context.Canceled) || pending.ObjectID != first.ObjectID || pending.IndexState != "pending" {
		t.Fatalf("%+v %v", pending, err)
	}
	recovered, err := Record(t.Context(), request)
	if err != nil || recovered.ObjectID != first.ObjectID || recovered.Admission != "existing" {
		t.Fatalf("%+v %v", recovered, err)
	}
}

func TestRecordRejectsSymlinkedLockDirectory(t *testing.T) {
	request, st := fixture(t)
	os.RemoveAll(filepath.Join(st.Dir(), "tmp"))
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(st.Dir(), "tmp")); err != nil {
		t.Fatal(err)
	}
	if _, err := Record(t.Context(), request); err == nil {
		t.Fatal("accepted symlink")
	}
	files, _ := os.ReadDir(outside)
	if len(files) != 0 {
		t.Fatal("wrote outside store")
	}
}

func TestRecordValidationPrecedesKeyAndCanonicalAccess(t *testing.T) {
	request, _ := fixture(t)
	request.Input.Events[0].Payload["outcome"] = "partial"
	request.KeyFile = "does-not-exist"
	result, err := Record(t.Context(), request)
	var validation *development.ValidationError
	if !errors.As(err, &validation) || result.SchemaValidation != "invalid" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestRecordCanceledValidationRemainsUnchecked(t *testing.T) {
	for _, test := range []struct {
		name string
		ctx  func(context.Context) (context.Context, context.CancelFunc)
		want error
	}{
		{"canceled", func(parent context.Context) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(parent)
			cancel()
			return ctx, cancel
		}, context.Canceled},
		{"deadline", func(parent context.Context) (context.Context, context.CancelFunc) {
			return context.WithDeadline(parent, time.Now().Add(-time.Second))
		}, context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := test.ctx(t.Context())
			defer cancel()
			result, err := Record(ctx, RecordRequest{})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if result.SchemaValidation != "unchecked" || result.ObjectID != "" {
				t.Fatalf("uncompleted validation claimed a result: %+v", result)
			}
		})
	}
}

func TestRecordIndexPendingDoesNotReportPreAdmissionFingerprint(t *testing.T) {
	request, st := fixture(t)
	os.RemoveAll(filepath.Join(st.Dir(), "index"))
	if err := os.WriteFile(filepath.Join(st.Dir(), "index"), []byte("obstruction"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Record(t.Context(), request)
	if err == nil || result.Admission != "created" || result.ObjectID == "" || result.IndexState != "pending" {
		t.Fatalf("%+v %v", result, err)
	}
	if result.SourceFingerprint != "" {
		t.Fatalf("pending creation reports pre-admission fingerprint: %s", result.SourceFingerprint)
	}
}

func TestRecordRetryReportsEvidenceLimitWithoutLosingAdmission(t *testing.T) {
	request, _ := fixture(t)
	first, err := Record(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(request.Repo, "proof"), os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(16<<20 + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	retry, err := Record(t.Context(), request)
	if err != nil || retry.ObjectID != first.ObjectID || retry.Admission != "existing" || retry.EvidenceState != "limit_exceeded" {
		t.Fatalf("%+v %v", retry, err)
	}
}

func TestRecordOperationScopeAndNoImplicitTrust(t *testing.T) {
	request, st := fixture(t)
	first, err := Record(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	rootsBefore, err := st.ReadLocal("trust.json")
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(t.TempDir(), "second.key")
	if _, err := identity.GenerateKeyFile(keyFile, "Second", time.Now()); err != nil {
		t.Fatal(err)
	}
	request.KeyFile = keyFile
	second, err := Record(t.Context(), request)
	if err != nil || second.Admission != "created" || second.ObjectID == first.ObjectID || second.Authorization == "authorized" {
		t.Fatalf("%+v %v", second, err)
	}
	request.Input.Namespace = "org/example/other"
	third, err := Record(t.Context(), request)
	if err != nil || third.Admission != "created" || third.ObjectID == second.ObjectID {
		t.Fatalf("%+v %v", third, err)
	}
	rootsAfter, err := st.ReadLocal("trust.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rootsBefore, rootsAfter) {
		t.Fatal("recorder installed trust")
	}
	files, err := st.ObjectFiles()
	if err != nil || len(files) != 3 {
		t.Fatalf("scope counts: %d %v", len(files), err)
	}
}

func TestRecordRejectsSymlinkedOperationLock(t *testing.T) {
	request, st := fixture(t)
	key, _ := identity.LoadSigningKey(request.KeyFile, st.Root())
	digest, err := digestValue(t.Context(), []any{request.Input.Namespace, key.KeyID, development.Profile, request.Input.OperationID})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(st.Dir(), "tmp", "record-"+strings.TrimPrefix(digest, "sha256:")+".lock")
	if err := os.Symlink(outside, name); err != nil {
		t.Fatal(err)
	}
	if _, err := Record(t.Context(), request); err == nil {
		t.Fatal("accepted symlinked lock")
	}
	raw, _ := os.ReadFile(outside)
	if string(raw) != "preserve" {
		t.Fatal("outside lock target changed")
	}
}
