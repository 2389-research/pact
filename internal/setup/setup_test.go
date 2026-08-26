// ABOUTME: Exercises setup inspection and resumable application against real owner resources.
// ABOUTME: Proves deterministic plans and publication convergence without exposing private key bytes.
package setup

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"pact/internal/identity"
	"pact/internal/index"
	"pact/internal/ledger"
	"pact/internal/store"

	_ "modernc.org/sqlite"
)

var inspectionTime = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

func TestInspectRejectsInvalidRequestWithoutWrites(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Request)
		ctx    context.Context
	}{
		{name: "missing repository", mutate: func(request *Request) { request.Repo = "" }, ctx: context.Background()},
		{name: "missing namespace", mutate: func(request *Request) { request.Namespace = "" }, ctx: context.Background()},
		{name: "missing actor", mutate: func(request *Request) { request.Actor = "" }, ctx: context.Background()},
		{name: "missing key file", mutate: func(request *Request) { request.KeyFile = "" }, ctx: context.Background()},
		{name: "nil context", mutate: func(_ *Request) {}, ctx: nil},
		{name: "bad namespace", mutate: func(request *Request) { request.Namespace = "not a namespace" }, ctx: context.Background()},
		{name: "unsafe key path", mutate: func(request *Request) { request.KeyFile = filepath.Join(request.Repo, "keys", "alice.key.json") }, ctx: context.Background()},
		{name: "bad actor", mutate: func(request *Request) { request.Actor = strings.Repeat("a", 256) }, ctx: context.Background()},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := setupRequest(t)
			test.mutate(&request)
			before := snapshotSetupPaths(t, request.Repo, request.KeyFile)

			if _, err := Inspect(test.ctx, request); err == nil {
				t.Fatal("Inspect() error = nil, want invalid request refusal")
			}

			after := snapshotSetupPaths(t, request.Repo, request.KeyFile)
			if !reflect.DeepEqual(after, before) {
				t.Fatal("Inspect() changed filesystem after invalid request")
			}
			assertAbsent(t, filepath.Join(request.Repo, ".pact"))
			if request.KeyFile != "" {
				assertAbsent(t, request.KeyFile)
				assertAbsent(t, filepath.Dir(request.KeyFile))
			}
		})
	}
}

func TestInspectObservesSetupStateWithoutWrites(t *testing.T) {
	for _, test := range []struct {
		name        string
		mutate      func(*Request)
		prepare     func(*testing.T, Request)
		want        []Action
		wantErr     error
		wantMessage string
		indexState  string
	}{
		{
			name: "fresh",
			want: plannedActions(
				ActionPlanned, ActionPlanned, ActionPlanned, ActionPlanned, ActionPlanned,
			),
		},
		{
			name: "complete",
			prepare: func(t *testing.T, request Request) {
				st, _ := preparedStoreKeyTrust(t, request)
				if _, err := index.New(st).Rebuild(context.Background()); err != nil {
					t.Fatal(err)
				}
			},
			want: plannedActions(
				ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionCurrent,
			),
		},
		{
			name:    "store only",
			prepare: func(t *testing.T, request Request) { mustInitStore(t, request) },
			want: plannedActions(
				ActionExisting, ActionPlanned, ActionPlanned, ActionValid, ActionPlanned,
			),
		},
		{
			name: "store and key",
			prepare: func(t *testing.T, request Request) {
				mustInitStore(t, request)
				mustGenerateKey(t, request, request.Actor)
			},
			want: plannedActions(
				ActionExisting, ActionExisting, ActionPlanned, ActionValid, ActionPlanned,
			),
		},
		{
			name:    "store key and trust at verification boundary",
			prepare: func(t *testing.T, request Request) { preparedStoreKeyTrust(t, request) },
			want: plannedActions(
				ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionPlanned,
			),
		},
		{
			name: "store namespace conflict",
			prepare: func(t *testing.T, request Request) {
				result, err := store.Init(request.Repo, "org/example/other", inspectionTime)
				if err != nil || result.Store == nil {
					t.Fatalf("Init() = (%#v, %v)", result, err)
				}
			},
			wantErr: store.ErrAlreadyInitialized,
		},
		{
			name:    "key actor conflict",
			prepare: func(t *testing.T, request Request) { mustGenerateKey(t, request, "Bob") },
			wantErr: identity.ErrIntegrity,
		},
		{
			name: "malformed key",
			prepare: func(t *testing.T, request Request) {
				mustMkdirAll(t, filepath.Dir(request.KeyFile))
				if err := os.WriteFile(request.KeyFile, []byte("not a PACT key"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantMessage: "unsupported or malformed PACT key file",
		},
		{
			name: "unsafe key mode",
			prepare: func(t *testing.T, request Request) {
				mustGenerateKey(t, request, request.Actor)
				if err := os.Chmod(request.KeyFile, 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: identity.ErrSecretSafety,
		},
		{
			name: "project contained key",
			mutate: func(request *Request) {
				request.KeyFile = filepath.Join(request.Repo, "keys", "alice.key.json")
			},
			wantErr: identity.ErrSecretSafety,
		},
		{
			name: "trust public byte conflict",
			prepare: func(t *testing.T, request Request) {
				st := mustInitStore(t, request)
				key := mustGenerateKey(t, request, request.Actor)
				otherPublic, _, err := ed25519.GenerateKey(nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := st.WriteLocalJSON("trust.json", map[string]any{
					"format": "pact/trust/v1",
					"roots": []any{map[string]any{
						"key_id": key.KeyID, "actor": key.Actor,
						"public_key": base64.RawURLEncoding.EncodeToString(otherPublic),
						"added_at":   inspectionTime.Format(time.RFC3339),
					}},
				}, 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: ledger.ErrIntegrity,
		},
		{
			name: "corrupt canonical state",
			prepare: func(t *testing.T, request Request) {
				st, _ := preparedStoreKeyTrust(t, request)
				if _, _, err := st.PutCanonical(map[string]any{"invalid": true}); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: ledger.ErrIntegrity,
		},
		{
			name:    "missing index",
			prepare: func(t *testing.T, request Request) { preparedStoreKeyTrust(t, request) },
			want: plannedActions(
				ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionPlanned,
			),
		},
		{
			name: "stale index",
			prepare: func(t *testing.T, request Request) {
				st, key := preparedStoreKeyTrust(t, request)
				if _, err := index.New(st).Rebuild(context.Background()); err != nil {
					t.Fatal(err)
				}
				if _, err := ledger.Commit(st, key, ledger.EventBatch{Namespace: request.Namespace, Events: []ledger.Event{{
					LocalID: "stale", Kind: "observation", Type: "setup.stale", Subject: "setup/stale", SchemaRef: "pact:core/setup/v1", Payload: map[string]any{},
				}}}, ledger.CommitOptions{ObservedAt: inspectionTime.Format(time.RFC3339)}); err != nil {
					t.Fatal(err)
				}
			},
			want: plannedActions(
				ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionPlanned,
			),
			indexState: "stale",
		},
		{
			name: "corrupt index",
			prepare: func(t *testing.T, request Request) {
				st, _ := preparedStoreKeyTrust(t, request)
				mustRebuildIndex(t, st)
				mutateIndex(t, st, "UPDATE index_meta SET value='sha256:broken' WHERE key='logical_digest'")
			},
			want: plannedActions(
				ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionPlanned,
			),
			indexState: "corrupt",
		},
		{
			name: "incompatible index",
			prepare: func(t *testing.T, request Request) {
				st, _ := preparedStoreKeyTrust(t, request)
				mustRebuildIndex(t, st)
				mutateIndex(t, st, "PRAGMA application_id=7")
			},
			want: plannedActions(
				ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionPlanned,
			),
			indexState: "incompatible",
		},
		{
			name: "partial build index",
			prepare: func(t *testing.T, request Request) {
				st, _ := preparedStoreKeyTrust(t, request)
				buildPartialIndex(t, st)
			},
			want: plannedActions(
				ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionPlanned,
			),
			indexState: "partial-build",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := setupRequest(t)
			if test.mutate != nil {
				test.mutate(&request)
			}
			if test.prepare != nil {
				test.prepare(t, request)
			}
			if test.indexState != "" {
				st, err := store.Open(request.Repo)
				if err != nil {
					t.Fatal(err)
				}
				status, err := index.New(st).Status(context.Background())
				if err != nil || status.Index.State != test.indexState {
					t.Fatalf("prepared index status = (%#v, %v), want state %q", status.Index, err, test.indexState)
				}
			}
			before := snapshotSetupPaths(t, request.Repo, request.KeyFile)

			plan, err := Inspect(context.Background(), request)
			switch {
			case test.wantErr != nil:
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("Inspect() error = %v, want %v", err, test.wantErr)
				}
			case test.wantMessage != "":
				if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
					t.Fatalf("Inspect() malformed-key diagnostic did not contain %q", test.wantMessage)
				}
			case err != nil:
				t.Fatal(err)
			default:
				if !reflect.DeepEqual(plan.Actions, test.want) {
					t.Fatalf("Inspect() actions = %#v, want %#v", plan.Actions, test.want)
				}
				if plan.Repo == "" || plan.Namespace == "" || plan.Actor == "" || plan.KeyFile == "" {
					t.Fatalf("Inspect() plan lacks resolved request fields: %#v", plan)
				}
			}

			after := snapshotSetupPaths(t, request.Repo, request.KeyFile)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("Inspect() changed observed filesystem state: before=%#v after=%#v", before, after)
			}
		})
	}
}

func TestApplyCreatesThenReusesCompleteSetup(t *testing.T) {
	request := setupRequest(t)

	created, err := Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if want := plannedActions(
		ActionCreated, ActionCreated, ActionCreated, ActionValid, ActionCreated,
	); !reflect.DeepEqual(created.Actions, want) {
		t.Fatalf("first Apply() actions = %#v, want %#v", created.Actions, want)
	}
	assertResolvedResult(t, created, request)
	before := savedSetupBytes(t, created)

	existing, err := Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if want := plannedActions(
		ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionCurrent,
	); !reflect.DeepEqual(existing.Actions, want) {
		t.Fatalf("second Apply() actions = %#v, want %#v", existing.Actions, want)
	}
	assertResolvedResult(t, existing, request)
	after := savedSetupBytes(t, existing)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("rerun changed setup bytes: before=%v after=%v", sortedByteSnapshotPaths(before), sortedByteSnapshotPaths(after))
	}
}

func TestApplyResumesFromEachPersistedBoundary(t *testing.T) {
	for _, test := range []struct {
		name      string
		prepare   func(*testing.T, Request)
		unchanged func(Request) []string
		want      []Action
	}{
		{
			name:    "store",
			prepare: func(t *testing.T, request Request) { mustInitStore(t, request) },
			unchanged: func(request Request) []string {
				return []string{filepath.Join(request.Repo, ".pact", "format.json")}
			},
			want: plannedActions(ActionExisting, ActionCreated, ActionCreated, ActionValid, ActionCreated),
		},
		{
			name: "store and key",
			prepare: func(t *testing.T, request Request) {
				mustInitStore(t, request)
				mustGenerateKey(t, request, request.Actor)
			},
			unchanged: func(request Request) []string {
				return []string{filepath.Join(request.Repo, ".pact", "format.json"), request.KeyFile}
			},
			want: plannedActions(ActionExisting, ActionExisting, ActionCreated, ActionValid, ActionCreated),
		},
		{
			name: "store key and trust",
			// Strict verification has no persisted state, so this is also its resume boundary.
			prepare: func(t *testing.T, request Request) { preparedStoreKeyTrust(t, request) },
			unchanged: func(request Request) []string {
				return []string{filepath.Join(request.Repo, ".pact", "format.json"), request.KeyFile, filepath.Join(request.Repo, ".pact", "trust.json")}
			},
			want: plannedActions(ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionCreated),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := setupRequest(t)
			test.prepare(t, request)
			before := readNamedBytes(t, test.unchanged(request))

			result, err := Apply(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result.Actions, test.want) {
				t.Fatalf("Apply() actions = %#v, want %#v", result.Actions, test.want)
			}
			after := readNamedBytes(t, test.unchanged(request))
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("Apply() changed completed boundary files: %v", sortedByteSnapshotPaths(before))
			}
		})
	}
}

func TestApplyPreservesWinnerBytesOnNamespaceAndActorConflicts(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*Request)
		wantErr error
		want    []Action
	}{
		{
			name:    "namespace",
			mutate:  func(request *Request) { request.Namespace = "org/example/other" },
			wantErr: store.ErrAlreadyInitialized,
			want:    []Action{},
		},
		{
			name:    "actor",
			mutate:  func(request *Request) { request.Actor = "Bob" },
			wantErr: identity.ErrIntegrity,
			want:    plannedActions(ActionExisting, "", "", "", "")[:1],
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			winner := setupRequest(t)
			ready, err := Apply(context.Background(), winner)
			if err != nil {
				t.Fatal(err)
			}
			before := savedSetupBytes(t, ready)
			loser := winner
			test.mutate(&loser)

			partial, err := Apply(context.Background(), loser)
			assertApplyFailure(t, partial, err, test.wantErr, test.want)
			after := savedSetupBytes(t, ready)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("conflicting Apply() changed winner files: %v", sortedByteSnapshotPaths(before))
			}
		})
	}
}

func TestApplyRefusesUnsafeKeyWithoutChangingWinner(t *testing.T) {
	request := setupRequest(t)
	mustInitStore(t, request)
	mustGenerateKey(t, request, request.Actor)
	if err := os.Chmod(request.KeyFile, 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotSetupPaths(t, request.Repo, request.KeyFile)

	partial, err := Apply(context.Background(), request)
	assertApplyFailure(t, partial, err, identity.ErrSecretSafety, []Action{{Name: ActionStore, Status: ActionExisting}})
	after := snapshotSetupPaths(t, request.Repo, request.KeyFile)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("unsafe-key refusal changed winner files")
	}
}

func TestApplyStopsAtTrustConflictWithoutChangingWinner(t *testing.T) {
	request := setupRequest(t)
	st := mustInitStore(t, request)
	key := mustGenerateKey(t, request, request.Actor)
	otherPublic, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WriteLocalJSON("trust.json", map[string]any{
		"format": "pact/trust/v1",
		"roots": []any{map[string]any{
			"key_id": key.KeyID, "actor": key.Actor,
			"public_key": base64.RawURLEncoding.EncodeToString(otherPublic),
			"added_at":   inspectionTime.Format(time.RFC3339),
		}},
	}, 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotSetupPaths(t, request.Repo, request.KeyFile)

	partial, err := Apply(context.Background(), request)
	assertApplyFailure(t, partial, err, ledger.ErrIntegrity, plannedActions(ActionExisting, ActionExisting, "", "", "")[:2])
	after := snapshotSetupPaths(t, request.Repo, request.KeyFile)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("trust conflict changed winner files")
	}
}

func TestApplyRefusesCorruptStoreWithoutChangingBytes(t *testing.T) {
	request := setupRequest(t)
	mustMkdirAll(t, filepath.Join(request.Repo, ".pact"))
	if err := os.WriteFile(filepath.Join(request.Repo, ".pact", "format.json"), []byte("not a PACT store"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotSetupPaths(t, request.Repo, request.KeyFile)

	partial, err := Apply(context.Background(), request)
	assertApplyFailure(t, partial, err, store.ErrNotInitialized, []Action{})
	after := snapshotSetupPaths(t, request.Repo, request.KeyFile)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("corrupt-store refusal changed winner files")
	}
}

func TestApplyStopsBeforeIndexWhenStrictVerificationFails(t *testing.T) {
	request := setupRequest(t)
	st, _ := preparedStoreKeyTrust(t, request)
	if _, _, err := st.PutCanonical(map[string]any{"invalid": true}); err != nil {
		t.Fatal(err)
	}
	before := snapshotSetupPaths(t, request.Repo, request.KeyFile)

	partial, err := Apply(context.Background(), request)
	assertApplyFailure(t, partial, err, ledger.ErrIntegrity, plannedActions(ActionExisting, ActionExisting, ActionExisting, "", "")[:3])
	after := snapshotSetupPaths(t, request.Repo, request.KeyFile)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("strict-verification refusal changed files or touched the index")
	}
}

func TestApplyPreservesArbitraryIndexRebuildFailure(t *testing.T) {
	request := setupRequest(t)
	preparedStoreKeyTrust(t, request)
	fault := errors.New("injected index rebuild failure")
	owners := defaultOwnerOperations
	owners.rebuildIndex = func(context.Context, *store.Store) (index.RebuildResult, error) {
		return index.RebuildResult{}, fault
	}
	before := snapshotSetupPaths(t, request.Repo, request.KeyFile)

	partial, err := applyWithOwners(context.Background(), request, owners)
	assertApplyFailure(t, partial, err, fault, plannedActions(ActionExisting, ActionExisting, ActionExisting, ActionValid, "")[:4])
	after := snapshotSetupPaths(t, request.Repo, request.KeyFile)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("failed index rebuild changed setup files")
	}
}

func TestApplyPreservesUnsafeLiveIndexWhenRealRebuildFails(t *testing.T) {
	request := setupRequest(t)
	st, _ := preparedStoreKeyTrust(t, request)
	target := filepath.Join(t.TempDir(), "winner.sqlite3")
	if err := os.WriteFile(target, []byte("winner index bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(st.Dir(), "index", "pact-v1.sqlite3")
	if err := os.Symlink(target, live); err != nil {
		t.Fatal(err)
	}
	before := snapshotSetupPaths(t, request.Repo, request.KeyFile)
	targetBefore := readNamedBytes(t, []string{target})

	partial, err := Apply(context.Background(), request)
	var applyErr *ApplyError
	if !errors.As(err, &applyErr) || !strings.Contains(err.Error(), "live index is not a regular non-symlink file") {
		t.Fatalf("Apply() error = %v, want unsafe live-index rebuild refusal", err)
	}
	want := plannedActions(ActionExisting, ActionExisting, ActionExisting, ActionValid, "")[:4]
	if !reflect.DeepEqual(partial.Actions, want) || !reflect.DeepEqual(partial, applyErr.Result) {
		t.Fatalf("Apply() partial result = %#v, want actions %#v", partial, want)
	}
	after := snapshotSetupPaths(t, request.Repo, request.KeyFile)
	if !reflect.DeepEqual(after, before) || !reflect.DeepEqual(readNamedBytes(t, []string{target}), targetBefore) {
		t.Fatal("failed real index rebuild changed unsafe winner bytes")
	}
}

func TestApplyMapsIndexReplacementToRebuilt(t *testing.T) {
	request := setupRequest(t)
	ready, err := Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(request.Repo)
	if err != nil {
		t.Fatal(err)
	}
	key, err := identity.LoadSigningKey(request.KeyFile, st.Root())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Commit(st, key, ledger.EventBatch{Namespace: request.Namespace, Events: []ledger.Event{{
		LocalID: "stale", Kind: "observation", Type: "setup.stale", Subject: "setup/stale", SchemaRef: "pact:core/setup/v1", Payload: map[string]any{},
	}}}, ledger.CommitOptions{ObservedAt: inspectionTime.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	canonicalBefore := snapshotCanonicalBytes(t, st)
	indexBefore := readNamedBytes(t, []string{filepath.Join(st.Dir(), "index", "pact-v1.sqlite3")})

	rebuilt, err := Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	want := plannedActions(ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionRebuilt)
	if !reflect.DeepEqual(rebuilt.Actions, want) {
		t.Fatalf("Apply() actions = %#v, want %#v", rebuilt.Actions, want)
	}
	if after := snapshotCanonicalBytes(t, st); !reflect.DeepEqual(after, canonicalBefore) {
		t.Fatal("index rebuild changed canonical files")
	}
	if after := readNamedBytes(t, []string{filepath.Join(st.Dir(), "index", "pact-v1.sqlite3")}); reflect.DeepEqual(after, indexBefore) {
		t.Fatal("index rebuild did not replace stale live bytes")
	}
	if ready.Store != rebuilt.Store {
		t.Fatalf("rebuilt store = %q, want %q", rebuilt.Store, ready.Store)
	}
}

func TestApplyNeverConvergesArbitraryOwnerErrors(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*testing.T, Request)
		install func(*ownerOperations, error)
		want    []Action
	}{
		{
			name:    "store",
			prepare: func(*testing.T, Request) {},
			install: func(owners *ownerOperations, fault error) {
				owners.initStore = func(string, string, time.Time) (store.InitResult, error) { return store.InitResult{}, fault }
			},
			want: []Action{},
		},
		{
			name:    "key",
			prepare: func(t *testing.T, request Request) { mustInitStore(t, request) },
			install: func(owners *ownerOperations, fault error) {
				owners.generateKey = func(string, string, time.Time) (identity.GenerateResult, error) {
					return identity.GenerateResult{}, fault
				}
			},
			want: []Action{{Name: ActionStore, Status: ActionExisting}},
		},
		{
			name: "trust",
			prepare: func(t *testing.T, request Request) {
				mustInitStore(t, request)
				mustGenerateKey(t, request, request.Actor)
			},
			install: func(owners *ownerOperations, fault error) {
				owners.addRoot = func(*store.Store, *identity.KeyFile, time.Time) (ledger.RootResult, error) {
					return ledger.RootResult{}, fault
				}
			},
			want: plannedActions(ActionExisting, ActionExisting, "", "", "")[:2],
		},
		{
			name:    "verify",
			prepare: func(t *testing.T, request Request) { preparedStoreKeyTrust(t, request) },
			install: func(owners *ownerOperations, fault error) {
				owners.verify = func(context.Context, *store.Store, bool) (ledger.VerifyResult, error) {
					return ledger.VerifyResult{}, fault
				}
			},
			want: plannedActions(ActionExisting, ActionExisting, ActionExisting, "", "")[:3],
		},
		{
			name:    "index status",
			prepare: func(t *testing.T, request Request) { preparedStoreKeyTrust(t, request) },
			install: func(owners *ownerOperations, fault error) {
				owners.indexStatus = func(context.Context, *store.Store) (index.Status, error) { return index.Status{}, fault }
			},
			want: plannedActions(ActionExisting, ActionExisting, ActionExisting, ActionValid, "")[:4],
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := setupRequest(t)
			test.prepare(t, request)
			fault := errors.New("injected arbitrary " + test.name + " failure")
			owners := defaultOwnerOperations
			test.install(&owners, fault)
			before := snapshotSetupPaths(t, request.Repo, request.KeyFile)

			partial, err := applyWithOwners(context.Background(), request, owners)
			assertApplyFailure(t, partial, err, fault, test.want)
			after := snapshotSetupPaths(t, request.Repo, request.KeyFile)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("arbitrary %s error changed setup files", test.name)
			}
		})
	}
}

func TestApplyReturnsStoreReleaseErrorAfterValidatedCollision(t *testing.T) {
	request := setupRequest(t)
	mustInitStore(t, request)
	releaseFault := errors.New("injected store lock release failure")
	owners := defaultOwnerOperations
	owners.initStore = func(string, string, time.Time) (store.InitResult, error) {
		return store.InitResult{Status: store.InitConflict}, &store.LockError{
			Operation: store.ErrAlreadyInitialized,
			Release:   releaseFault,
		}
	}
	before := snapshotSetupPaths(t, request.Repo, request.KeyFile)

	partial, err := applyWithOwners(context.Background(), request, owners)
	assertApplyFailure(t, partial, err, releaseFault, []Action{{Name: ActionStore, Status: ActionExisting}})
	var lockErr *store.LockError
	if !errors.As(err, &lockErr) || !errors.Is(err, store.ErrAlreadyInitialized) {
		t.Fatalf("Apply() error = %v, want original store collision LockError", err)
	}
	after := snapshotSetupPaths(t, request.Repo, request.KeyFile)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("store collision plus release failure changed setup files")
	}
}

func TestApplyReturnsJoinedKeyErrorAfterValidatedCollision(t *testing.T) {
	request := setupRequest(t)
	mustInitStore(t, request)
	mustGenerateKey(t, request, request.Actor)
	cleanupFault := errors.New("injected key cleanup failure")
	owners := defaultOwnerOperations
	owners.generateKey = func(string, string, time.Time) (identity.GenerateResult, error) {
		collision := &os.LinkError{Op: "link", Old: "temporary", New: request.KeyFile, Err: fs.ErrExist}
		return identity.GenerateResult{Status: identity.GenerateConflict}, errors.Join(collision, cleanupFault)
	}
	before := snapshotSetupPaths(t, request.Repo, request.KeyFile)

	partial, err := applyWithOwners(context.Background(), request, owners)
	want := plannedActions(ActionExisting, ActionExisting, "", "", "")[:2]
	assertApplyFailure(t, partial, err, cleanupFault, want)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("Apply() error = %v, want original key collision cause", err)
	}
	after := snapshotSetupPaths(t, request.Repo, request.KeyFile)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("key collision plus cleanup failure changed setup files")
	}
}

func TestApplyRejectsConflictStatusWithoutOwnerError(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*testing.T, Request)
		install func(*ownerOperations)
		want    []Action
	}{
		{
			name:    "store",
			prepare: func(t *testing.T, request Request) { mustInitStore(t, request) },
			install: func(owners *ownerOperations) {
				owners.initStore = func(string, string, time.Time) (store.InitResult, error) {
					return store.InitResult{Status: store.InitConflict}, nil
				}
			},
			want: []Action{},
		},
		{
			name: "key",
			prepare: func(t *testing.T, request Request) {
				mustInitStore(t, request)
				mustGenerateKey(t, request, request.Actor)
			},
			install: func(owners *ownerOperations) {
				owners.generateKey = func(string, string, time.Time) (identity.GenerateResult, error) {
					return identity.GenerateResult{Status: identity.GenerateConflict}, nil
				}
			},
			want: []Action{{Name: ActionStore, Status: ActionExisting}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := setupRequest(t)
			test.prepare(t, request)
			owners := defaultOwnerOperations
			test.install(&owners)
			before := snapshotSetupPaths(t, request.Repo, request.KeyFile)

			partial, err := applyWithOwners(context.Background(), request, owners)
			var applyErr *ApplyError
			if !errors.As(err, &applyErr) || !errors.Is(err, errInvalidOwnerOutcome) || !strings.Contains(err.Error(), "invalid owner outcome") ||
				!strings.Contains(err.Error(), test.name) {
				t.Fatalf("Apply() error = %v, want specific invalid %s owner outcome", err, test.name)
			}
			if !reflect.DeepEqual(partial, applyErr.Result) || !reflect.DeepEqual(partial.Actions, test.want) {
				t.Fatalf("Apply() partial result = %#v, want actions %#v", partial, test.want)
			}
			after := snapshotSetupPaths(t, request.Repo, request.KeyFile)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("nil-error %s conflict changed setup files", test.name)
			}
		})
	}
}

func TestApplyReportsPublishedStoreBeforePostRenameFailure(t *testing.T) {
	request := setupRequest(t)
	fault := errors.New("injected store post-rename failure")
	owners := defaultOwnerOperations
	owners.initStore = func(repo, namespace string, now time.Time) (store.InitResult, error) {
		result, err := defaultOwnerOperations.initStore(repo, namespace, now)
		if err == nil && result.Status == store.InitCreated {
			return result, fault
		}
		return result, err
	}

	partial, err := applyWithOwners(context.Background(), request, owners)
	assertApplyFailure(t, partial, err, fault, []Action{{Name: ActionStore, Status: ActionCreated}})
	opened, openErr := store.Open(request.Repo)
	if openErr != nil {
		t.Fatal(openErr)
	}
	if namespace, namespaceErr := opened.DefaultNamespace(); namespaceErr != nil || namespace != request.Namespace {
		t.Fatalf("published store namespace = (%q, %v), want %q", namespace, namespaceErr, request.Namespace)
	}
	formatPath := filepath.Join(opened.Dir(), "format.json")
	before := readNamedBytes(t, []string{formatPath})

	ready, err := Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if want := plannedActions(ActionExisting, ActionCreated, ActionCreated, ActionValid, ActionCreated); !reflect.DeepEqual(ready.Actions, want) {
		t.Fatalf("rerun actions = %#v, want %#v", ready.Actions, want)
	}
	after := readNamedBytes(t, []string{formatPath})
	if !reflect.DeepEqual(after, before) {
		t.Fatal("clean rerun changed published store format bytes")
	}
}

func TestApplyReportsPublishedKeyBeforePostLinkFailure(t *testing.T) {
	request := setupRequest(t)
	mustInitStore(t, request)
	fault := errors.New("injected key post-link failure")
	owners := defaultOwnerOperations
	owners.generateKey = func(path, actor string, now time.Time) (identity.GenerateResult, error) {
		result, err := defaultOwnerOperations.generateKey(path, actor, now)
		if err == nil && result.Status == identity.GenerateCreated {
			return result, fault
		}
		return result, err
	}

	partial, err := applyWithOwners(context.Background(), request, owners)
	assertApplyFailure(t, partial, err, fault, plannedActions(ActionExisting, ActionCreated, "", "", "")[:2])
	key, loadErr := identity.LoadSigningKey(request.KeyFile, request.Repo)
	if loadErr != nil || key.Actor != request.Actor || key.KeyID != partial.KeyID {
		t.Fatalf("LoadSigningKey() after publication failure: actor=%q key_id_matches=%t err=%v", keyActor(key), key != nil && key.KeyID == partial.KeyID, loadErr)
	}
	before := readNamedBytes(t, []string{request.KeyFile})

	ready, err := Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if want := plannedActions(ActionExisting, ActionExisting, ActionCreated, ActionValid, ActionCreated); !reflect.DeepEqual(ready.Actions, want) {
		t.Fatalf("rerun actions = %#v, want %#v", ready.Actions, want)
	}
	after := readNamedBytes(t, []string{request.KeyFile})
	if !reflect.DeepEqual(after, before) {
		t.Fatal("clean rerun changed published key bytes")
	}
}

func TestApplyReportsPublishedTrustBeforePostRenameFailure(t *testing.T) {
	request := setupRequest(t)
	st := mustInitStore(t, request)
	key := mustGenerateKey(t, request, request.Actor)
	fault := errors.New("injected trust post-rename failure")
	owners := defaultOwnerOperations
	owners.addRoot = func(st *store.Store, key *identity.KeyFile, now time.Time) (ledger.RootResult, error) {
		result, err := defaultOwnerOperations.addRoot(st, key, now)
		if err == nil && result.Status == ledger.RootCreated {
			return result, fault
		}
		return result, err
	}

	partial, err := applyWithOwners(context.Background(), request, owners)
	assertApplyFailure(t, partial, err, fault, plannedActions(ActionExisting, ActionExisting, ActionCreated, "", "")[:3])
	roots, rootsErr := ledger.Roots(st)
	root, found := roots[key.KeyID]
	if rootsErr != nil || !found || root.Actor != request.Actor {
		t.Fatalf("Roots() after publication failure: found=%t actor=%q err=%v", found, root.Actor, rootsErr)
	}
	trustPath := filepath.Join(st.Dir(), "trust.json")
	before := readNamedBytes(t, []string{trustPath})

	ready, err := Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if want := plannedActions(ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionCreated); !reflect.DeepEqual(ready.Actions, want) {
		t.Fatalf("rerun actions = %#v, want %#v", ready.Actions, want)
	}
	after := readNamedBytes(t, []string{trustPath})
	if !reflect.DeepEqual(after, before) {
		t.Fatal("clean rerun changed published trust bytes")
	}
}

func TestApplyReportsPublishedIndexBeforePostRenameFailure(t *testing.T) {
	request := setupRequest(t)
	st, _ := preparedStoreKeyTrust(t, request)
	fault := errors.New("injected index post-rename failure")
	owners := defaultOwnerOperations
	owners.rebuildIndex = func(ctx context.Context, st *store.Store) (index.RebuildResult, error) {
		result, err := defaultOwnerOperations.rebuildIndex(ctx, st)
		if err == nil && (result.Created || result.Replaced) {
			return result, fault
		}
		return result, err
	}

	partial, err := applyWithOwners(context.Background(), request, owners)
	assertApplyFailure(t, partial, err, fault, plannedActions(ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionCreated))
	status, statusErr := index.New(st).Status(context.Background())
	if statusErr != nil || status.Index.State != "current" {
		t.Fatalf("Status() after publication failure = (%q, %v), want current", status.Index.State, statusErr)
	}
	indexPath := filepath.Join(st.Dir(), "index", "pact-v1.sqlite3")
	before := readNamedBytes(t, []string{indexPath})

	ready, err := Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if want := plannedActions(ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionCurrent); !reflect.DeepEqual(ready.Actions, want) {
		t.Fatalf("rerun actions = %#v, want %#v", ready.Actions, want)
	}
	after := readNamedBytes(t, []string{indexPath})
	if !reflect.DeepEqual(after, before) {
		t.Fatal("clean rerun changed published index bytes")
	}
}

func TestApplyConcurrentIdenticalRequestsConverge(t *testing.T) {
	canonical, alias, request := concurrentSetupPaths(t)
	request.Repo = canonical
	aliasRequest := request
	aliasRequest.Repo = alias
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	owners := ownerOperationsWithBarriers(ctx, true)

	outcomes := runConcurrentApplies(ctx, owners, request, aliasRequest)
	for index, outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("Apply() outcome %d error = %v", index, outcome.err)
		}
		if outcome.result.Status != "ready" {
			t.Fatalf("Apply() outcome %d status = %q, want ready", index, outcome.result.Status)
		}
	}
	assertCombinedStatuses(t, outcomes, ActionStore, ActionCreated, ActionExisting)
	assertCombinedStatuses(t, outcomes, ActionKey, ActionCreated, ActionExisting)
	assertCombinedStatuses(t, outcomes, ActionTrust, ActionCreated, ActionExisting)
	assertCombinedStatuses(t, outcomes, ActionVerify, ActionValid, ActionValid)
	assertCombinedStatuses(t, outcomes, ActionIndex, ActionCreated, ActionRebuilt)
	assertConcurrentResultSequences(t, outcomes)

	before := assertValidSetupAndReadBytes(t, request)
	rerun, err := Apply(context.Background(), aliasRequest)
	if err != nil {
		t.Fatal(err)
	}
	if want := plannedActions(ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionCurrent); !reflect.DeepEqual(rerun.Actions, want) {
		t.Fatalf("converged rerun actions = %#v, want %#v", rerun.Actions, want)
	}
	after := assertValidSetupAndReadBytes(t, request)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("converged rerun changed final files: %v", sortedByteSnapshotPaths(before))
	}
}

func TestApplyConcurrentConflictingRequestsPreserveWinner(t *testing.T) {
	for _, conflict := range []string{"namespace", "actor"} {
		t.Run(conflict, func(t *testing.T) {
			canonical, alias, first := concurrentSetupPaths(t)
			first.Repo = canonical
			second := first
			second.Repo = alias
			wantErr := store.ErrAlreadyInitialized
			if conflict == "namespace" {
				second.Namespace = "org/example/other"
			} else {
				second.Actor = "Bob"
				wantErr = identity.ErrIntegrity
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			owners := ownerOperationsWithConflictBarriers(ctx, conflict)

			outcomes := runConcurrentApplies(ctx, owners, first, second)
			winner, loser := splitConcurrentOutcomes(t, outcomes, wantErr)
			assertConflictResultSequences(t, conflict, winner.result, loser.result)
			winnerRequest := first
			if winner.result.Actor == second.Actor && winner.result.Namespace == second.Namespace {
				winnerRequest = second
			}
			before := assertValidSetupAndReadBytes(t, winnerRequest)

			rerun, err := Apply(context.Background(), winnerRequest)
			if err != nil {
				t.Fatal(err)
			}
			if want := plannedActions(ActionExisting, ActionExisting, ActionExisting, ActionValid, ActionCurrent); !reflect.DeepEqual(rerun.Actions, want) {
				t.Fatalf("winner rerun actions = %#v, want %#v", rerun.Actions, want)
			}
			after := assertValidSetupAndReadBytes(t, winnerRequest)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("conflicting rerun changed winner files: %v", sortedByteSnapshotPaths(before))
			}
		})
	}
}

func TestInspectNormalizesActorAndUsesStoreResolvedRoot(t *testing.T) {
	request := setupRequest(t)
	request.Actor = "  A\u0301lice\t"
	st := mustInitStore(t, request)
	alias := filepath.Join(t.TempDir(), "repo-alias")
	if err := os.Symlink(request.Repo, alias); err != nil {
		t.Fatal(err)
	}
	request.Repo = alias

	plan, err := Inspect(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Repo != st.Root() || plan.Actor != "Álice" {
		t.Fatalf("Inspect() plan = %#v, want resolved repo and normalized actor", plan)
	}
	if again, err := Inspect(context.Background(), request); err != nil || !reflect.DeepEqual(again, plan) {
		t.Fatalf("second Inspect() = (%#v, %v), want deterministic plan", again, err)
	}
}

func setupRequest(t *testing.T) Request {
	t.Helper()
	base := t.TempDir()
	return Request{
		Repo:      filepath.Join(base, "repo"),
		Namespace: "org/example/widget",
		Actor:     "Alice",
		KeyFile:   filepath.Join(base, "keys", "alice.key.json"),
		Now:       inspectionTime,
	}
}

func plannedActions(storeStatus, keyStatus, trustStatus, verifyStatus, indexStatus ActionStatus) []Action {
	return []Action{
		{Name: ActionStore, Status: storeStatus},
		{Name: ActionKey, Status: keyStatus},
		{Name: ActionTrust, Status: trustStatus},
		{Name: ActionVerify, Status: verifyStatus},
		{Name: ActionIndex, Status: indexStatus},
	}
}

func mustInitStore(t *testing.T, request Request) *store.Store {
	t.Helper()
	mustMkdirAll(t, request.Repo)
	result, err := store.Init(request.Repo, request.Namespace, inspectionTime)
	if err != nil || result.Store == nil {
		t.Fatalf("Init() = (%#v, %v)", result, err)
	}
	return result.Store
}

func mustGenerateKey(t *testing.T, request Request, actor string) *identity.KeyFile {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(request.KeyFile))
	result, err := identity.GenerateKeyFile(request.KeyFile, actor, inspectionTime)
	if err != nil || result.Key == nil {
		t.Fatalf("GenerateKeyFile() status=%q key_present=%t err=%v", result.Status, result.Key != nil, err)
	}
	return result.Key
}

func preparedStoreKeyTrust(t *testing.T, request Request) (*store.Store, *identity.KeyFile) {
	t.Helper()
	st := mustInitStore(t, request)
	key := mustGenerateKey(t, request, request.Actor)
	if _, err := ledger.AddRoot(st, key, inspectionTime); err != nil {
		t.Fatal(err)
	}
	return st, key
}

func mustRebuildIndex(t *testing.T, st *store.Store) {
	t.Helper()
	if _, err := index.New(st).Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func mutateIndex(t *testing.T, st *store.Store, statement string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(st.Dir(), "index", "pact-v1.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close index mutation database: %v", err)
		}
	}()
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func buildPartialIndex(t *testing.T, st *store.Store) {
	t.Helper()
	mustRebuildIndex(t, st)
	scan, err := ledger.Scan(context.Background(), st, ledger.ScanOptions{Limits: ledger.Phase2Limits})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := index.Project(context.Background(), scan)
	if err != nil {
		t.Fatal(err)
	}
	rogueID := "sha256:" + strings.Repeat("9", 64)
	rogueRef := rogueID + "#rogue"
	batch := uint64(0)
	snapshot.Objects = append(snapshot.Objects, index.ObjectRow{ObjectID: rogueID, ObjectType: "commit", Namespace: "setup/rogue", BodyDigest: rogueID, ActorKeyID: "ed25519:" + strings.Repeat("8", 64), ActorLabel: "Rogue", ObservedAt: inspectionTime.Format(time.RFC3339), IntegrityState: "valid", StructureState: "valid", AuthenticityState: "valid", CompletenessState: "complete"})
	snapshot.Commits = append(snapshot.Commits, index.CommitRow{CommitID: rogueID, EventCount: 1})
	snapshot.Events = append(snapshot.Events, index.EventRow{EventRef: rogueRef, CommitID: rogueID, LocalID: "rogue", Kind: "action", EventType: "setup.rogue", Subject: "setup/rogue", SchemaRef: "pact:core/setup/v1", CausalBatch: &batch, CausalStatus: "ordered"})
	snapshot.Heads = append(snapshot.Heads, index.HeadRow{Namespace: "setup/rogue", CommitID: rogueID})
	for key, value := range map[string]string{"source_count_objects": "1", "source_count_commits": "1", "source_count_events": "1", "source_count_edges": "2", "row_count_objects": "1", "row_count_commits": "1", "row_count_events": "1", "row_count_heads": "1"} {
		setSnapshotMeta(snapshot.IndexMeta, key, value)
	}
	digest, err := index.LogicalDigest(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	setSnapshotMeta(snapshot.IndexMeta, "logical_digest", digest)

	db, err := sql.Open("sqlite", filepath.Join(st.Dir(), "index", "pact-v1.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close partial-build database: %v", err)
		}
	}()
	if _, err := db.Exec("INSERT INTO objects VALUES(?,?,?,?,?,?,?,?,?,?,?)", rogueID, "commit", "setup/rogue", rogueID, "ed25519:"+strings.Repeat("8", 64), "Rogue", inspectionTime.Format(time.RFC3339), "valid", "valid", "valid", "complete"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO commits VALUES(?,?)", rogueID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO events VALUES(?,?,?,?,?,?,?,?,?)", rogueRef, rogueID, "rogue", "action", "setup.rogue", "setup/rogue", "pact:core/setup/v1", 0, "ordered"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO heads VALUES(?,?)", "setup/rogue", rogueID); err != nil {
		t.Fatal(err)
	}
	for _, row := range snapshot.IndexMeta {
		if _, err := db.Exec("UPDATE index_meta SET value=? WHERE key=?", row.Value, row.Key); err != nil {
			t.Fatal(err)
		}
	}
}

func setSnapshotMeta(rows []index.IndexMetaRow, key, value string) {
	for position := range rows {
		if rows[position].Key == key {
			rows[position].Value = value
			return
		}
	}
}

func assertResolvedResult(t *testing.T, result Result, request Request) {
	t.Helper()
	wantRepo, err := filepath.Abs(request.Repo)
	if err != nil {
		t.Fatal(err)
	}
	wantRepo, err = filepath.EvalSymlinks(wantRepo)
	if err != nil {
		t.Fatal(err)
	}
	wantKey, err := filepath.Abs(request.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ready" || result.Repo != wantRepo || result.Store != filepath.Join(wantRepo, ".pact") ||
		result.Namespace != request.Namespace || result.Actor != request.Actor || result.KeyFile != wantKey || result.KeyID == "" {
		t.Fatalf("Apply() result lacks resolved setup identity: status=%q repo=%q store=%q namespace=%q actor=%q key_file=%q key_id_present=%t",
			result.Status, result.Repo, result.Store, result.Namespace, result.Actor, result.KeyFile, result.KeyID != "")
	}
}

type applyOutcome struct {
	request Request
	result  Result
	err     error
}

type applyBarrier struct {
	mu      sync.Mutex
	want    int
	reached int
	release chan struct{}
}

func newApplyBarrier() *applyBarrier {
	return &applyBarrier{want: 2, release: make(chan struct{})}
}

func (barrier *applyBarrier) wait(ctx context.Context) error {
	barrier.mu.Lock()
	barrier.reached++
	if barrier.reached == barrier.want {
		close(barrier.release)
	}
	barrier.mu.Unlock()
	select {
	case <-barrier.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func ownerOperationsWithBarriers(ctx context.Context, includeIndex bool) ownerOperations {
	storeBarrier := newApplyBarrier()
	keyBarrier := newApplyBarrier()
	trustBarrier := newApplyBarrier()
	statusBarrier := newApplyBarrier()
	indexBarrier := newApplyBarrier()
	owners := defaultOwnerOperations
	owners.initStore = func(repo, namespace string, now time.Time) (store.InitResult, error) {
		result, err := defaultOwnerOperations.initStore(repo, namespace, now)
		return result, joinBarrierError(err, storeBarrier.wait(ctx))
	}
	owners.generateKey = func(path, actor string, now time.Time) (identity.GenerateResult, error) {
		result, err := defaultOwnerOperations.generateKey(path, actor, now)
		return result, joinBarrierError(err, keyBarrier.wait(ctx))
	}
	owners.addRoot = func(st *store.Store, key *identity.KeyFile, now time.Time) (ledger.RootResult, error) {
		result, err := defaultOwnerOperations.addRoot(st, key, now)
		return result, joinBarrierError(err, trustBarrier.wait(ctx))
	}
	if includeIndex {
		owners.indexStatus = func(callCtx context.Context, st *store.Store) (index.Status, error) {
			result, err := defaultOwnerOperations.indexStatus(callCtx, st)
			return result, joinBarrierError(err, statusBarrier.wait(ctx))
		}
		owners.rebuildIndex = func(callCtx context.Context, st *store.Store) (index.RebuildResult, error) {
			result, err := defaultOwnerOperations.rebuildIndex(callCtx, st)
			return result, joinBarrierError(err, indexBarrier.wait(ctx))
		}
	}
	return owners
}

func ownerOperationsWithConflictBarriers(ctx context.Context, conflict string) ownerOperations {
	storeBarrier := newApplyBarrier()
	owners := defaultOwnerOperations
	owners.initStore = func(repo, namespace string, now time.Time) (store.InitResult, error) {
		result, err := defaultOwnerOperations.initStore(repo, namespace, now)
		return result, joinBarrierError(err, storeBarrier.wait(ctx))
	}
	if conflict == "actor" {
		keyBarrier := newApplyBarrier()
		owners.generateKey = func(path, actor string, now time.Time) (identity.GenerateResult, error) {
			result, err := defaultOwnerOperations.generateKey(path, actor, now)
			return result, joinBarrierError(err, keyBarrier.wait(ctx))
		}
	}
	return owners
}

func joinBarrierError(operationErr, barrierErr error) error {
	if barrierErr == nil {
		return operationErr
	}
	if operationErr == nil {
		return barrierErr
	}
	return errors.Join(operationErr, barrierErr)
}

func runConcurrentApplies(ctx context.Context, owners ownerOperations, requests ...Request) []applyOutcome {
	outcomeChannel := make(chan applyOutcome, len(requests))
	var calls sync.WaitGroup
	calls.Add(len(requests))
	for _, request := range requests {
		go func(request Request) {
			defer calls.Done()
			result, err := applyWithOwners(ctx, request, owners)
			outcomeChannel <- applyOutcome{request: request, result: result, err: err}
		}(request)
	}
	calls.Wait()
	close(outcomeChannel)
	outcomes := make([]applyOutcome, 0, len(requests))
	for outcome := range outcomeChannel {
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func concurrentSetupPaths(t *testing.T) (string, string, Request) {
	t.Helper()
	base := t.TempDir()
	canonical := filepath.Join(base, "repo")
	mustMkdirAll(t, canonical)
	alias := filepath.Join(base, "repo-alias")
	if err := os.Symlink(canonical, alias); err != nil {
		t.Fatal(err)
	}
	request := Request{
		Namespace: "org/example/widget",
		Actor:     "Alice",
		KeyFile:   filepath.Join(base, "keys", "alice.key.json"),
		Now:       inspectionTime,
	}
	return canonical, alias, request
}

func assertCombinedStatuses(t *testing.T, outcomes []applyOutcome, name ActionName, want ...ActionStatus) {
	t.Helper()
	got := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		found := false
		for _, action := range outcome.result.Actions {
			if action.Name == name {
				got = append(got, string(action.Status))
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("outcome lacks %s action: %#v", name, outcome.result.Actions)
		}
	}
	wantStrings := make([]string, len(want))
	for index, status := range want {
		wantStrings[index] = string(status)
	}
	sort.Strings(got)
	sort.Strings(wantStrings)
	if !reflect.DeepEqual(got, wantStrings) {
		t.Fatalf("combined %s statuses = %v, want %v", name, got, wantStrings)
	}
}

func assertConcurrentResultSequences(t *testing.T, outcomes []applyOutcome) {
	t.Helper()
	wantNames := []ActionName{ActionStore, ActionKey, ActionTrust, ActionVerify, ActionIndex}
	for index, outcome := range outcomes {
		if len(outcome.result.Actions) != len(wantNames) {
			t.Fatalf("outcome %d actions = %#v, want five", index, outcome.result.Actions)
		}
		for actionIndex, wantName := range wantNames {
			if outcome.result.Actions[actionIndex].Name != wantName {
				t.Fatalf("outcome %d action %d = %q, want %q", index, actionIndex, outcome.result.Actions[actionIndex].Name, wantName)
			}
		}
	}
}

func splitConcurrentOutcomes(t *testing.T, outcomes []applyOutcome, wantErr error) (applyOutcome, applyOutcome) {
	t.Helper()
	if len(outcomes) != 2 {
		t.Fatalf("outcomes = %d, want 2", len(outcomes))
	}
	var winner, loser *applyOutcome
	for index := range outcomes {
		outcome := &outcomes[index]
		if outcome.err == nil {
			winner = outcome
			continue
		}
		var applyErr *ApplyError
		if !errors.As(outcome.err, &applyErr) || !errors.Is(outcome.err, wantErr) {
			t.Fatalf("losing Apply() error = %v, want typed conflict %v", outcome.err, wantErr)
		}
		loser = outcome
	}
	if winner == nil || loser == nil {
		t.Fatalf("concurrent outcomes = %#v, want one success and one conflict", outcomes)
	}
	return *winner, *loser
}

func assertConflictResultSequences(t *testing.T, conflict string, winner, loser Result) {
	t.Helper()
	if conflict == "namespace" {
		wantWinner := plannedActions(ActionCreated, ActionCreated, ActionCreated, ActionValid, ActionCreated)
		if !reflect.DeepEqual(winner.Actions, wantWinner) || len(loser.Actions) != 0 {
			t.Fatalf("namespace conflict sequences = winner %#v loser %#v", winner.Actions, loser.Actions)
		}
		return
	}
	if len(winner.Actions) != 5 || len(loser.Actions) != 1 ||
		winner.Actions[0].Name != ActionStore || loser.Actions[0].Name != ActionStore ||
		!isCreatorAccepterPair(winner.Actions[0].Status, loser.Actions[0].Status) ||
		!reflect.DeepEqual(winner.Actions[1:], plannedActions("", ActionCreated, ActionCreated, ActionValid, ActionCreated)[1:]) {
		t.Fatalf("actor conflict sequences = winner %#v loser %#v", winner.Actions, loser.Actions)
	}
}

func isCreatorAccepterPair(first, second ActionStatus) bool {
	return first == ActionCreated && second == ActionExisting || first == ActionExisting && second == ActionCreated
}

func assertValidSetupAndReadBytes(t *testing.T, request Request) map[string][]byte {
	t.Helper()
	st, err := store.Open(request.Repo)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := st.DefaultNamespace()
	if err != nil || namespace != request.Namespace {
		t.Fatalf("winner namespace = (%q, %v), want %q", namespace, err, request.Namespace)
	}
	key, err := identity.LoadSigningKey(request.KeyFile, st.Root())
	if err != nil || key == nil || key.Actor != request.Actor {
		t.Fatalf("winner key actor = (%q, %v), want %q", keyActor(key), err, request.Actor)
	}
	roots, err := ledger.Roots(st)
	root, found := roots[key.KeyID]
	if err != nil || !found || root.PublicKey != base64.RawURLEncoding.EncodeToString(key.Public) {
		t.Fatalf("winner trust validation = found=%t public_matches=%t err=%v", found, found && root.PublicKey == base64.RawURLEncoding.EncodeToString(key.Public), err)
	}
	verification, err := ledger.VerifyContext(context.Background(), st, true)
	if err != nil || !verification.OK {
		t.Fatalf("winner strict verification = (ok=%t, %v)", verification.OK, err)
	}
	status, err := index.New(st).Status(context.Background())
	if err != nil || status.Index.State != "current" {
		t.Fatalf("winner index status = (%q, %v), want current", status.Index.State, err)
	}
	return savedSetupBytes(t, Result{Store: st.Dir(), KeyFile: request.KeyFile})
}

func assertApplyFailure(t *testing.T, result Result, err, cause error, want []Action) {
	t.Helper()
	var applyErr *ApplyError
	if !errors.As(err, &applyErr) || !errors.Is(err, cause) {
		t.Fatalf("Apply() error = %v, want ApplyError preserving %v", err, cause)
	}
	if !reflect.DeepEqual(result, applyErr.Result) {
		t.Fatal("Apply() returned result differs from ApplyError.Result")
	}
	if !reflect.DeepEqual(result.Actions, want) {
		t.Fatalf("Apply() partial actions = %#v, want %#v", result.Actions, want)
	}
}

func keyActor(key *identity.KeyFile) string {
	if key == nil {
		return ""
	}
	return key.Actor
}

func savedSetupBytes(t *testing.T, result Result) map[string][]byte {
	t.Helper()
	paths := []string{
		result.KeyFile,
		filepath.Join(result.Store, "format.json"),
		filepath.Join(result.Store, "trust.json"),
		filepath.Join(result.Store, "index", "pact-v1.sqlite3"),
	}
	objects := filepath.Join(result.Store, "objects")
	if err := filepath.WalkDir(objects, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	snapshot := make(map[string][]byte, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read saved setup file %q: %v", path, err)
		}
		snapshot[path] = raw
	}
	return snapshot
}

func readNamedBytes(t *testing.T, paths []string) map[string][]byte {
	t.Helper()
	snapshot := make(map[string][]byte, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read setup file %q: %v", path, err)
		}
		snapshot[path] = raw
	}
	return snapshot
}

func snapshotCanonicalBytes(t *testing.T, st *store.Store) map[string][]byte {
	t.Helper()
	var paths []string
	if err := filepath.WalkDir(filepath.Join(st.Dir(), "objects"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return readNamedBytes(t, paths)
}

func sortedByteSnapshotPaths(snapshot map[string][]byte) []string {
	paths := make([]string, 0, len(snapshot))
	for path := range snapshot {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

type filesystemSnapshot struct {
	Path string
	Mode fs.FileMode
	Size int64
	Hash [sha256.Size]byte
}

func snapshotSetupPaths(t *testing.T, repo, keyFile string) []filesystemSnapshot {
	t.Helper()
	paths := []string{repo, filepath.Dir(keyFile)}
	seen := make(map[string]bool)
	var snapshot []filesystemSnapshot
	for _, root := range paths {
		if seen[root] {
			continue
		}
		seen[root] = true
		if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			item := filesystemSnapshot{Path: path, Mode: info.Mode(), Size: info.Size()}
			if info.Mode().IsRegular() {
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				item.Hash = sha256.Sum256(raw)
			}
			snapshot = append(snapshot, item)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Slice(snapshot, func(i, j int) bool { return snapshot[i].Path < snapshot[j].Path })
	return snapshot
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("path %q exists or could not be read: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
