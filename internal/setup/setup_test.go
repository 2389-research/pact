// ABOUTME: Exercises read-only setup observation against real PACT stores, keys, and indexes.
// ABOUTME: Proves inspection plans deterministically without exposing private key bytes.
package setup_test

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
	"testing"
	"time"

	"pact/internal/identity"
	"pact/internal/index"
	"pact/internal/ledger"
	"pact/internal/setup"
	"pact/internal/store"

	_ "modernc.org/sqlite"
)

var inspectionTime = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

func TestInspectRejectsInvalidRequestWithoutWrites(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*setup.Request)
		ctx    context.Context
	}{
		{name: "missing repository", mutate: func(request *setup.Request) { request.Repo = "" }, ctx: context.Background()},
		{name: "missing namespace", mutate: func(request *setup.Request) { request.Namespace = "" }, ctx: context.Background()},
		{name: "missing actor", mutate: func(request *setup.Request) { request.Actor = "" }, ctx: context.Background()},
		{name: "missing key file", mutate: func(request *setup.Request) { request.KeyFile = "" }, ctx: context.Background()},
		{name: "nil context", mutate: func(_ *setup.Request) {}, ctx: nil},
		{name: "bad namespace", mutate: func(request *setup.Request) { request.Namespace = "not a namespace" }, ctx: context.Background()},
		{name: "unsafe key path", mutate: func(request *setup.Request) { request.KeyFile = filepath.Join(request.Repo, "keys", "alice.key.json") }, ctx: context.Background()},
		{name: "bad actor", mutate: func(request *setup.Request) { request.Actor = strings.Repeat("a", 256) }, ctx: context.Background()},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := setupRequest(t)
			test.mutate(&request)
			before := snapshotSetupPaths(t, request.Repo, request.KeyFile)

			if _, err := setup.Inspect(test.ctx, request); err == nil {
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
		mutate      func(*setup.Request)
		prepare     func(*testing.T, setup.Request)
		want        []setup.Action
		wantErr     error
		wantFailure bool
		wantMessage string
		indexState  string
	}{
		{
			name: "fresh",
			want: plannedActions(
				setup.ActionPlanned, setup.ActionPlanned, setup.ActionPlanned, setup.ActionPlanned, setup.ActionPlanned,
			),
		},
		{
			name: "complete",
			prepare: func(t *testing.T, request setup.Request) {
				st, _ := preparedStoreKeyTrust(t, request)
				if _, err := index.New(st).Rebuild(context.Background()); err != nil {
					t.Fatal(err)
				}
			},
			want: plannedActions(
				setup.ActionExisting, setup.ActionExisting, setup.ActionExisting, setup.ActionValid, setup.ActionCurrent,
			),
		},
		{
			name:    "store only",
			prepare: func(t *testing.T, request setup.Request) { mustInitStore(t, request) },
			want: plannedActions(
				setup.ActionExisting, setup.ActionPlanned, setup.ActionPlanned, setup.ActionValid, setup.ActionPlanned,
			),
		},
		{
			name: "store and key",
			prepare: func(t *testing.T, request setup.Request) {
				mustInitStore(t, request)
				mustGenerateKey(t, request, request.Actor)
			},
			want: plannedActions(
				setup.ActionExisting, setup.ActionExisting, setup.ActionPlanned, setup.ActionValid, setup.ActionPlanned,
			),
		},
		{
			name:    "store key and trust",
			prepare: func(t *testing.T, request setup.Request) { preparedStoreKeyTrust(t, request) },
			want: plannedActions(
				setup.ActionExisting, setup.ActionExisting, setup.ActionExisting, setup.ActionValid, setup.ActionPlanned,
			),
		},
		{
			name:    "store key trust and strict verification",
			prepare: func(t *testing.T, request setup.Request) { preparedStoreKeyTrust(t, request) },
			want: plannedActions(
				setup.ActionExisting, setup.ActionExisting, setup.ActionExisting, setup.ActionValid, setup.ActionPlanned,
			),
		},
		{
			name: "store namespace conflict",
			prepare: func(t *testing.T, request setup.Request) {
				result, err := store.Init(request.Repo, "org/example/other", inspectionTime)
				if err != nil || result.Store == nil {
					t.Fatalf("Init() = (%#v, %v)", result, err)
				}
			},
			wantErr: store.ErrAlreadyInitialized,
		},
		{
			name:    "key actor conflict",
			prepare: func(t *testing.T, request setup.Request) { mustGenerateKey(t, request, "Bob") },
			wantErr: identity.ErrIntegrity,
		},
		{
			name: "malformed key",
			prepare: func(t *testing.T, request setup.Request) {
				mustMkdirAll(t, filepath.Dir(request.KeyFile))
				if err := os.WriteFile(request.KeyFile, []byte("not a PACT key"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantMessage: "unsupported or malformed PACT key file",
		},
		{
			name: "unsafe key mode",
			prepare: func(t *testing.T, request setup.Request) {
				mustGenerateKey(t, request, request.Actor)
				if err := os.Chmod(request.KeyFile, 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: identity.ErrSecretSafety,
		},
		{
			name: "project contained key",
			mutate: func(request *setup.Request) {
				request.KeyFile = filepath.Join(request.Repo, "keys", "alice.key.json")
			},
			wantErr: identity.ErrSecretSafety,
		},
		{
			name: "trust public byte conflict",
			prepare: func(t *testing.T, request setup.Request) {
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
			prepare: func(t *testing.T, request setup.Request) {
				st, _ := preparedStoreKeyTrust(t, request)
				if _, _, err := st.PutCanonical(map[string]any{"invalid": true}); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: ledger.ErrIntegrity,
		},
		{
			name:    "missing index",
			prepare: func(t *testing.T, request setup.Request) { preparedStoreKeyTrust(t, request) },
			want: plannedActions(
				setup.ActionExisting, setup.ActionExisting, setup.ActionExisting, setup.ActionValid, setup.ActionPlanned,
			),
		},
		{
			name: "stale index",
			prepare: func(t *testing.T, request setup.Request) {
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
				setup.ActionExisting, setup.ActionExisting, setup.ActionExisting, setup.ActionValid, setup.ActionPlanned,
			),
			indexState: "stale",
		},
		{
			name: "corrupt index",
			prepare: func(t *testing.T, request setup.Request) {
				st, _ := preparedStoreKeyTrust(t, request)
				mustRebuildIndex(t, st)
				mutateIndex(t, st, "UPDATE index_meta SET value='sha256:broken' WHERE key='logical_digest'")
			},
			want: plannedActions(
				setup.ActionExisting, setup.ActionExisting, setup.ActionExisting, setup.ActionValid, setup.ActionPlanned,
			),
			indexState: "corrupt",
		},
		{
			name: "incompatible index",
			prepare: func(t *testing.T, request setup.Request) {
				st, _ := preparedStoreKeyTrust(t, request)
				mustRebuildIndex(t, st)
				mutateIndex(t, st, "PRAGMA application_id=7")
			},
			want: plannedActions(
				setup.ActionExisting, setup.ActionExisting, setup.ActionExisting, setup.ActionValid, setup.ActionPlanned,
			),
			indexState: "incompatible",
		},
		{
			name: "partial build index",
			prepare: func(t *testing.T, request setup.Request) {
				st, _ := preparedStoreKeyTrust(t, request)
				buildPartialIndex(t, st)
			},
			want: plannedActions(
				setup.ActionExisting, setup.ActionExisting, setup.ActionExisting, setup.ActionValid, setup.ActionPlanned,
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

			plan, err := setup.Inspect(context.Background(), request)
			switch {
			case test.wantErr != nil:
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("Inspect() error = %v, want %v", err, test.wantErr)
				}
			case test.wantFailure:
				if err == nil {
					t.Fatal("Inspect() error = nil, want refusal")
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

func TestInspectNormalizesActorAndUsesStoreResolvedRoot(t *testing.T) {
	request := setupRequest(t)
	request.Actor = "  A\u0301lice\t"
	st := mustInitStore(t, request)
	alias := filepath.Join(t.TempDir(), "repo-alias")
	if err := os.Symlink(request.Repo, alias); err != nil {
		t.Fatal(err)
	}
	request.Repo = alias

	plan, err := setup.Inspect(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Repo != st.Root() || plan.Actor != "Álice" {
		t.Fatalf("Inspect() plan = %#v, want resolved repo and normalized actor", plan)
	}
	if again, err := setup.Inspect(context.Background(), request); err != nil || !reflect.DeepEqual(again, plan) {
		t.Fatalf("second Inspect() = (%#v, %v), want deterministic plan", again, err)
	}
}

func setupRequest(t *testing.T) setup.Request {
	t.Helper()
	base := t.TempDir()
	return setup.Request{
		Repo:      filepath.Join(base, "repo"),
		Namespace: "org/example/widget",
		Actor:     "Alice",
		KeyFile:   filepath.Join(base, "keys", "alice.key.json"),
		Now:       inspectionTime,
	}
}

func plannedActions(storeStatus, keyStatus, trustStatus, verifyStatus, indexStatus setup.ActionStatus) []setup.Action {
	return []setup.Action{
		{Name: setup.ActionStore, Status: storeStatus},
		{Name: setup.ActionKey, Status: keyStatus},
		{Name: setup.ActionTrust, Status: trustStatus},
		{Name: setup.ActionVerify, Status: verifyStatus},
		{Name: setup.ActionIndex, Status: indexStatus},
	}
}

func mustInitStore(t *testing.T, request setup.Request) *store.Store {
	t.Helper()
	mustMkdirAll(t, request.Repo)
	result, err := store.Init(request.Repo, request.Namespace, inspectionTime)
	if err != nil || result.Store == nil {
		t.Fatalf("Init() = (%#v, %v)", result, err)
	}
	return result.Store
}

func mustGenerateKey(t *testing.T, request setup.Request, actor string) *identity.KeyFile {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(request.KeyFile))
	result, err := identity.GenerateKeyFile(request.KeyFile, actor, inspectionTime)
	if err != nil || result.Key == nil {
		t.Fatalf("GenerateKeyFile() status=%q key_present=%t err=%v", result.Status, result.Key != nil, err)
	}
	return result.Key
}

func preparedStoreKeyTrust(t *testing.T, request setup.Request) (*store.Store, *identity.KeyFile) {
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
