// ABOUTME: Exercises atomic disposable-index rebuilds with real files, SQLite, and signed ledger scans.
// ABOUTME: Verifies publication boundaries, cleanup safety, partial replicas, and the shared mutation lock.
package index

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"pact/internal/ledger"
	"pact/internal/store"
)

func TestRebuildIgnoresDeepRefPathOutsideCanonicalSource(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	deepRef := filepath.Join(fixture.store.Dir(), "refs", strings.Repeat("a", 200), strings.Repeat("b", 200), strings.Repeat("c", 120), "ignored")
	if err := os.MkdirAll(filepath.Dir(deepRef), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deepRef, []byte("ignored ref data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	protectedBefore := captureProtectedFiles(t, fixture.store.Dir())
	result, err := New(fixture.store).Rebuild(context.Background())
	if err != nil {
		t.Fatalf("Rebuild() refused ignored %d-byte ref path: %v", len(deepRef), err)
	}
	if result.Index.State != "current" {
		t.Fatalf("Rebuild() state = %q, want current", result.Index.State)
	}
	if protectedAfter := captureProtectedFiles(t, fixture.store.Dir()); !reflect.DeepEqual(protectedAfter, protectedBefore) {
		t.Fatal("rebuild changed canonical, trust, ref, or checkpoint bytes")
	}
}

func TestCanonicalSourceProofDetectsObjectChange(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	if err := os.WriteFile(fixture.child.Path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := proveCanonicalSourceUnchanged(context.Background(), fixture.store, fixture.scan.SourceFingerprint)
	assertQueryErrorCode(t, err, "source_changed")
}

func TestRebuildCreatesThenReplacesValidatedIndex(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	live := filepath.Join(fixture.store.Dir(), "index", liveIndexName)
	protectedBefore := captureProtectedFiles(t, fixture.store.Dir())

	first, err := manager.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("first Rebuild() error = %v", err)
	}
	if !first.Created || first.Replaced || first.Index.State != "current" || first.Index.Coverage != "partial" {
		t.Fatalf("first result = %#v", first)
	}
	firstBytes, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(live)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
		t.Fatalf("live mode = %v, want regular 0600", info.Mode())
	}
	assertNoIndexSidecars(t, live)

	second, err := manager.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("second Rebuild() error = %v", err)
	}
	if second.Created || !second.Replaced || second.Index.LogicalDigest == nil || first.Index.LogicalDigest == nil || *second.Index.LogicalDigest != *first.Index.LogicalDigest {
		t.Fatalf("second result = %#v, first = %#v", second, first)
	}
	secondBytes, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstBytes) == 0 || len(secondBytes) == 0 {
		t.Fatal("published SQLite file is empty")
	}
	if protectedAfter := captureProtectedFiles(t, fixture.store.Dir()); !reflect.DeepEqual(protectedAfter, protectedBefore) {
		t.Fatal("rebuild changed canonical, trust, ref, or checkpoint bytes")
	}
}

func captureProtectedFiles(t *testing.T, storeDirectory string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte)
	for _, relative := range []string{"objects", "refs", "trust.json"} {
		root := filepath.Join(storeDirectory, relative)
		info, err := os.Lstat(root)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().IsRegular() {
			result[relative] = mustReadFile(t, root)
			continue
		}
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			name, err := filepath.Rel(storeDirectory, path)
			if err != nil {
				return err
			}
			result[name] = mustReadFile(t, path)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRebuildRefusesInvalidSourceAndCancellation(t *testing.T) {
	st, err := store.Init(t.TempDir(), "fixture/rebuild", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutCanonical(map[string]any{"bad": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(st).Rebuild(context.Background()); err == nil {
		t.Fatal("Rebuild() succeeded with invalid source")
	} else {
		assertQueryErrorCode(t, err, "source_invalid")
	}
	if _, err := os.Lstat(filepath.Join(st.Dir(), "index", liveIndexName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("live file after source refusal: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(st).Rebuild(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Rebuild() error = %v", err)
	}
}

func TestRebuildCleansOnlySafeStaleTemps(t *testing.T) {
	st, err := store.Init(t.TempDir(), "fixture/rebuild", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	indexDir := filepath.Join(st.Dir(), "index")
	stale := filepath.Join(indexDir, ".build-12345.sqlite3")
	unrelated := filepath.Join(indexDir, ".build-not-a-db")
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(st).Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale temp still exists: %v", err)
	}
	if _, err := os.Lstat(unrelated); err != nil {
		t.Fatalf("unrelated file changed: %v", err)
	}
}

func TestRebuildRefusesSymlinkedTempWithoutFollowingIt(t *testing.T) {
	st, err := store.Init(t.TempDir(), "fixture/rebuild", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("do not touch"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(st.Dir(), "index", ".build-67890.sqlite3")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := New(st).Rebuild(context.Background()); err == nil {
		t.Fatal("Rebuild() succeeded with symlinked stale temp")
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "do not touch" {
		t.Fatalf("symlink target changed: %q, %v", raw, err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink candidate changed: %v, %v", info, err)
	}
}

func TestRebuildRefusesSymlinkedStoreBeforeTempCleanup(t *testing.T) {
	st, err := store.Init(t.TempDir(), "fixture/rebuild", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	realStore := filepath.Join(st.Root(), ".pact-real")
	if err := os.Rename(st.Dir(), realStore); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realStore, st.Dir()); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(realStore, "index", ".build-12345.sqlite3")
	if err := os.WriteFile(stale, []byte("must remain"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(st).Rebuild(context.Background()); err == nil {
		t.Fatal("Rebuild() succeeded through a symlinked store directory")
	}
	if raw, err := os.ReadFile(stale); err != nil || string(raw) != "must remain" {
		t.Fatalf("unsafe path cleanup changed stale temp: %q, %v", raw, err)
	}
}

func TestRebuildPrePublicationFaultsPreserveOldBytes(t *testing.T) {
	tests := []struct {
		name    string
		install func(error)
	}{
		{name: "writer", install: func(fault error) { beforeIndexWrite = func() error { return fault } }},
		{name: "commit", install: func(fault error) { commitIndexTransaction = func(*sql.Tx) error { return fault } }},
		{name: "close", install: func(fault error) { closeIndexWriter = func(db *sql.DB) error { return errors.Join(db.Close(), fault) } }},
		{name: "file sync", install: func(fault error) { syncIndexFile = func(string) error { return fault } }},
		{name: "built validation", install: func(fault error) { beforeBuiltValidation = func(string) error { return fault } }},
		{name: "rename", install: func(fault error) { renameIndexFile = func(string, string) error { return fault } }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := signedPartialScanFixture(t)
			manager := New(fixture.store)
			if _, err := manager.Rebuild(context.Background()); err != nil {
				t.Fatal(err)
			}
			live := filepath.Join(fixture.store.Dir(), "index", liveIndexName)
			oldBytes, err := os.ReadFile(live)
			if err != nil {
				t.Fatal(err)
			}
			resetIndexFailureSeams(t)
			fault := errors.New("injected " + test.name + " failure")
			test.install(fault)
			if _, err := manager.Rebuild(context.Background()); !errors.Is(err, fault) {
				t.Fatalf("Rebuild() error = %v, want injected fault", err)
			}
			gotBytes, err := os.ReadFile(live)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotBytes, oldBytes) {
				t.Fatal("pre-publication fault changed old live bytes")
			}
		})
	}
}

func TestRebuildFirstBuildFaultLeavesNoLiveFile(t *testing.T) {
	st, err := store.Init(t.TempDir(), "fixture/rebuild", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	resetIndexFailureSeams(t)
	fault := errors.New("injected rename failure")
	renameIndexFile = func(string, string) error { return fault }
	if _, err := New(st).Rebuild(context.Background()); !errors.Is(err, fault) {
		t.Fatalf("Rebuild() error = %v, want injected fault", err)
	}
	if _, err := os.Lstat(filepath.Join(st.Dir(), "index", liveIndexName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first failed build left live file: %v", err)
	}
}

func TestRebuildRefusesOversizeBuiltDatabaseBeforeRename(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(fixture.store.Dir(), "index", liveIndexName)
	oldBytes, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	resetIndexFailureSeams(t)
	beforeBuiltValidation = func(path string) error { return os.Truncate(path, int64(ledger.Phase2Limits.SQLiteBytes)+1) }
	if _, err := manager.Rebuild(context.Background()); err == nil {
		t.Fatal("Rebuild() succeeded with oversize built database")
	}
	gotBytes, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBytes, oldBytes) {
		t.Fatal("oversize pre-publication failure changed old live bytes")
	}
}

func TestRebuildRefusesSidecarBeforePublication(t *testing.T) {
	st, err := store.Init(t.TempDir(), "fixture/rebuild", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	resetIndexFailureSeams(t)
	beforeBuiltValidation = func(path string) error { return os.WriteFile(path+"-wal", []byte("unexpected"), 0o600) }
	if _, err := New(st).Rebuild(context.Background()); err == nil {
		t.Fatal("Rebuild() succeeded with a temp sidecar")
	}
	if _, err := os.Lstat(filepath.Join(st.Dir(), "index", liveIndexName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("live file after sidecar refusal: %v", err)
	}
}

func TestRebuildRefusesLiveSidecarBeforeRename(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(fixture.store.Dir(), "index", liveIndexName)
	oldBytes := mustReadFile(t, live)
	if err := os.WriteFile(live+"-shm", []byte("leftover"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Rebuild(context.Background()); err == nil {
		t.Fatal("Rebuild() succeeded over a live sidecar")
	}
	if got := mustReadFile(t, live); !bytes.Equal(got, oldBytes) {
		t.Fatal("live sidecar refusal changed old live bytes")
	}
}

func TestRebuildPropagatesCancellationDuringPreparedExec(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	resetIndexFailureSeams(t)
	ctx, cancel := context.WithCancel(context.Background())
	execPreparedIndexRow = func(ctx context.Context, statement *sql.Stmt, arguments ...any) (sql.Result, error) {
		cancel()
		return statement.ExecContext(ctx, arguments...)
	}
	if _, err := New(fixture.store).Rebuild(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Rebuild() error = %v, want context canceled", err)
	}
}

func TestRebuildPreparedExecFailurePreservesCauseAndOldBytes(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(fixture.store.Dir(), "index", liveIndexName)
	oldBytes := mustReadFile(t, live)
	resetIndexFailureSeams(t)
	fault := errors.New("prepared execution failed")
	execPreparedIndexRow = func(context.Context, *sql.Stmt, ...any) (sql.Result, error) { return nil, fault }
	if _, err := manager.Rebuild(context.Background()); !errors.Is(err, fault) {
		t.Fatalf("Rebuild() error = %v, want prepared execution cause", err)
	}
	if got := mustReadFile(t, live); !bytes.Equal(got, oldBytes) {
		t.Fatal("prepared execution failure changed old live bytes")
	}
}

func TestIndexRowWriteErrorsHideDriverTextAndPreserveCause(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Run("actual prepare failure", func(t *testing.T) {
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		err = insertRows(context.Background(), tx, "INSERT SQL DSN", 0, func(int) []any { return nil })
		assertSafeIndexWriteError(t, err)
		if errors.Unwrap(err) == nil {
			t.Fatal("prepare failure lost its modernc cause")
		}
	})

	t.Run("custom execution failure", func(t *testing.T) {
		resetIndexFailureSeams(t)
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		fault := errors.New("INSERT secret SQL from file:dsn?token=private")
		execPreparedIndexRow = func(context.Context, *sql.Stmt, ...any) (sql.Result, error) { return nil, fault }
		err = insertRows(context.Background(), tx, "SELECT ?", 1, func(int) []any { return []any{"safe"} })
		assertSafeIndexWriteError(t, err)
		if !errors.Is(err, fault) {
			t.Fatalf("insertRows() error = %v, want preserved cause", err)
		}
	})

	t.Run("context remains exact", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		assertDirectContextError(t, insertRows(ctx, tx, "SELECT ?", 1, func(int) []any { return []any{"safe"} }), context.Canceled)
	})

	t.Run("deadline remains exact", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
		defer cancel()
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		assertDirectContextError(t, insertRows(ctx, tx, "SELECT ?", 1, func(int) []any { return []any{"safe"} }), context.DeadlineExceeded)
	})
}

func assertDirectContextError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("insertRows() error = %#v, want %v", got, want)
	}
	if _, wrapped := got.(interface{ Unwrap() error }); wrapped {
		t.Fatalf("insertRows() error = %#v, want direct context sentinel", got)
	}
	if _, joined := got.(interface{ Unwrap() []error }); joined {
		t.Fatalf("insertRows() error = %#v, want direct context sentinel", got)
	}
}

func assertSafeIndexWriteError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("insertRows() error = nil")
	}
	upper := strings.ToUpper(err.Error())
	for _, fragment := range []string{"INSERT", " SQL", "DSN"} {
		if strings.Contains(upper, fragment) {
			t.Fatalf("insertRows() error leaked %q: %q", fragment, err)
		}
	}
}

func TestRebuildCancellationAfterSyncPreventsRename(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(fixture.store.Dir(), "index", liveIndexName)
	oldBytes := mustReadFile(t, live)
	resetIndexFailureSeams(t)
	ctx, cancel := context.WithCancel(context.Background())
	syncIndexFile = func(path string) error {
		err := syncRegularIndexFile(path)
		cancel()
		return err
	}
	if _, err := manager.Rebuild(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Rebuild() error = %v, want context canceled", err)
	}
	if got := mustReadFile(t, live); !bytes.Equal(got, oldBytes) {
		t.Fatal("post-sync cancellation changed old live bytes")
	}
}

func TestRebuildReportsPostPublicationFaults(t *testing.T) {
	tests := []struct {
		name    string
		install func(error)
	}{
		{name: "directory sync", install: func(fault error) { syncIndexDirectory = func(string) error { return fault } }},
		{name: "published reopen", install: func(fault error) { beforePublishedValidation = func(string) error { return fault } }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, err := store.Init(t.TempDir(), "fixture/rebuild", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			resetIndexFailureSeams(t)
			fault := errors.New("injected " + test.name + " failure")
			test.install(fault)
			if _, err := New(st).Rebuild(context.Background()); !errors.Is(err, fault) {
				t.Fatalf("Rebuild() error = %v, want injected fault", err)
			} else if test.name == "directory sync" {
				assertQueryErrorCode(t, err, "index_publication_failed")
			}
			live := filepath.Join(st.Dir(), "index", liveIndexName)
			if info, err := os.Lstat(live); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("post-publication fault did not leave published file: %v, %v", info, err)
			}
		})
	}
}

func TestConcurrentCanonicalPublicationWaitsForFullRebuildLock(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	resetIndexFailureSeams(t)
	renameReached := make(chan struct{})
	releaseRename := make(chan struct{})
	renameIndexFile = func(oldPath, newPath string) error {
		close(renameReached)
		<-releaseRename
		return os.Rename(oldPath, newPath)
	}
	rebuildDone := make(chan error, 1)
	go func() { _, err := New(fixture.store).Rebuild(context.Background()); rebuildDone <- err }()
	<-renameReached
	commitDone := make(chan error, 1)
	go func() {
		_, err := ledger.Commit(fixture.store, fixture.key, ledger.EventBatch{Namespace: "fixture/concurrent", Events: []ledger.Event{fixtureEvent("concurrent", "action", nil, nil, nil)}}, ledger.CommitOptions{Namespace: "fixture/concurrent", ObservedAt: "2026-08-24T13:00:00Z"})
		commitDone <- err
	}()
	select {
	case err := <-commitDone:
		t.Fatalf("canonical publication did not wait for rebuild lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseRename)
	if err := <-rebuildDone; err != nil {
		t.Fatal(err)
	}
	if err := <-commitDone; err != nil {
		t.Fatal(err)
	}
	status, err := New(fixture.store).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Index.State != "stale" {
		t.Fatalf("post-commit state = %q, want stale", status.Index.State)
	}
}

func assertNoIndexSidecars(t *testing.T, path string) {
	t.Helper()
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		if _, err := os.Lstat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("sidecar %s exists or cannot be inspected: %v", suffix, err)
		}
	}
}

func resetIndexFailureSeams(t *testing.T) {
	t.Helper()
	oldCommit, oldClose := commitIndexTransaction, closeIndexWriter
	oldSyncFile, oldRename, oldSyncDirectory := syncIndexFile, renameIndexFile, syncIndexDirectory
	oldBeforeBuilt, oldBeforePublished := beforeBuiltValidation, beforePublishedValidation
	oldBeforeWrite := beforeIndexWrite
	oldExec := execPreparedIndexRow
	t.Cleanup(func() {
		commitIndexTransaction, closeIndexWriter = oldCommit, oldClose
		syncIndexFile, renameIndexFile, syncIndexDirectory = oldSyncFile, oldRename, oldSyncDirectory
		beforeBuiltValidation, beforePublishedValidation = oldBeforeBuilt, oldBeforePublished
		beforeIndexWrite = oldBeforeWrite
		execPreparedIndexRow = oldExec
	})
	commitIndexTransaction = func(tx *sql.Tx) error { return tx.Commit() }
	closeIndexWriter = func(db *sql.DB) error { return db.Close() }
	syncIndexFile = syncRegularIndexFile
	renameIndexFile = os.Rename
	syncIndexDirectory = syncRealIndexDirectory
	beforeBuiltValidation = func(string) error { return nil }
	beforePublishedValidation = func(string) error { return nil }
	beforeIndexWrite = func() error { return nil }
	execPreparedIndexRow = func(ctx context.Context, statement *sql.Stmt, arguments ...any) (sql.Result, error) {
		return statement.ExecContext(ctx, arguments...)
	}
}
