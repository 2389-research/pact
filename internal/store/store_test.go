// ABOUTME: Exercises PACT's immutable, content-addressed object store on real filesystems.
// ABOUTME: Covers initialization, collision safety, durability checks, and failure cleanup.
package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"pact/internal/canonical"
)

func TestInitCreatesReferenceLayout(t *testing.T) {
	repo := t.TempDir()
	now := time.Date(2026, 8, 23, 12, 34, 56, 0, time.UTC)
	st, err := Init(repo, "org/example/widget", now)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if got, want := st.Dir(), resolvedPath(t, filepath.Join(repo, ".pact")); got != want {
		t.Fatalf("store directory = %q, want %q", got, want)
	}
	for _, name := range []string{"objects/sha256", "index", "refs", "tmp"} {
		info, err := os.Stat(filepath.Join(repo, ".pact", name))
		if err != nil || !info.IsDir() {
			t.Fatalf("layout directory %q: info=%v err=%v", name, info, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(repo, ".pact", ".gitignore")); err != nil || string(got) != "index/\ntmp/\nrefs/\n" {
		t.Fatalf(".gitignore = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(repo, ".pact", "format.json")); err != nil || !strings.Contains(string(got), "\"format\": \"pact/store/v1\"") || !strings.Contains(string(got), "\"created_at\": \"2026-08-23T12:34:56Z\"") {
		t.Fatalf("format.json = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(repo, ".pact", "trust.json")); err != nil || string(got) != "{\n  \"format\": \"pact/trust/v1\",\n  \"roots\": []\n}\n" {
		t.Fatalf("trust.json = %q, err=%v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".pact.init.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("project init lock artifact exists: err=%v", err)
	}
}

func TestInitRefusesExistingStore(t *testing.T) {
	repo := t.TempDir()
	if _, err := Init(repo, "org/example/widget", time.Now()); err != nil {
		t.Fatalf("first Init() error = %v", err)
	}
	if _, err := Init(repo, "org/example/widget", time.Now()); err == nil {
		t.Fatal("second Init() error = nil, want refusal")
	}
}

func TestInitReturnsPublishedStoreWithBothReleaseErrors(t *testing.T) {
	repo := t.TempDir()
	unlockErr := errors.New("injected unlock failure")
	closeErr := errors.New("injected close failure")
	restore := injectLockReleaseErrors(t, unlockErr, closeErr)

	st, err := Init(repo, "org/example/widget", time.Now())
	if st == nil || !errors.Is(err, unlockErr) || !errors.Is(err, closeErr) {
		t.Fatalf("Init() = (%#v, %v), want published store and both release errors", st, err)
	}
	restore()
	if opened, openErr := Open(repo); openErr != nil || opened.Dir() != st.Dir() {
		t.Fatalf("Open() after release error = (%#v, %v)", opened, openErr)
	}
}

func TestWithMutationLockPreservesOperationAndBothReleaseErrors(t *testing.T) {
	st := testStore(t)
	operationErr := errors.New("injected operation failure")
	unlockErr := errors.New("injected unlock failure")
	closeErr := errors.New("injected close failure")
	injectLockReleaseErrors(t, unlockErr, closeErr)
	calls := 0

	err := st.WithMutationLock(func() error {
		calls++
		return operationErr
	})
	if calls != 1 || !errors.Is(err, operationErr) || !errors.Is(err, unlockErr) || !errors.Is(err, closeErr) {
		t.Fatalf("WithMutationLock() calls=%d error=%v, want operation and both release errors", calls, err)
	}
	successfulCalls := 0
	err = st.WithMutationLock(func() error {
		successfulCalls++
		return nil
	})
	if successfulCalls != 1 || !errors.Is(err, unlockErr) || !errors.Is(err, closeErr) {
		t.Fatalf("successful WithMutationLock() calls=%d error=%v, want one call and both release errors", successfulCalls, err)
	}
}

func injectLockReleaseErrors(t *testing.T, unlockErr, closeErr error) func() {
	t.Helper()
	originalUnlock := unlockLockFile
	originalClose := closeLockFile
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		unlockLockFile = originalUnlock
		closeLockFile = originalClose
	}
	t.Cleanup(restore)
	unlockLockFile = func(lock *os.File) error {
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
			return errors.Join(unlockErr, err)
		}
		return unlockErr
	}
	closeLockFile = func(lock *os.File) error {
		if err := lock.Close(); err != nil {
			return errors.Join(closeErr, err)
		}
		return closeErr
	}
	return restore
}

func TestInitRejectsSymlinkedStore(t *testing.T) {
	repo := t.TempDir()
	escaped := t.TempDir()
	if err := os.Symlink(escaped, filepath.Join(repo, ".pact")); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(repo, "org/example/widget", time.Now()); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Init() error = %v, want symlink refusal", err)
	}
	entries, err := os.ReadDir(escaped)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink target changed: entries=%v err=%v", entries, err)
	}
}

func TestInitAllowsRepositoryReachedThroughSymlink(t *testing.T) {
	repo := t.TempDir()
	link := filepath.Join(t.TempDir(), "repo")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	st, err := Init(link, "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := st.Dir(), resolvedPath(t, filepath.Join(repo, ".pact")); got != want {
		t.Fatalf("store directory = %q, want %q", got, want)
	}
}

func TestConcurrentInitHasOneWinner(t *testing.T) {
	repo := t.TempDir()
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			<-start
			_, err := Init(repo, "org/example/widget", time.Now())
			results <- err
		})
	}
	close(start)
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful Init calls = %d, want 1", successes)
	}
}

func TestInitIgnoresUnlockedStaleLockFile(t *testing.T) {
	repo := t.TempDir()
	lockPath, err := initLockPath(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(repo, "org/example/widget", time.Now()); err != nil {
		t.Fatalf("Init() with stale lock error = %v", err)
	}
}

func TestInitCleansStagingAfterPublishConflictAndRetries(t *testing.T) {
	repo := t.TempDir()
	original := beforePublish
	beforePublish = func(_, destination string) error {
		return os.Mkdir(destination, 0o755)
	}
	t.Cleanup(func() { beforePublish = original })
	if _, err := Init(repo, "org/example/widget", time.Now()); err == nil {
		t.Fatal("Init() error = nil, want publish conflict")
	}
	storePath := filepath.Join(repo, ".pact")
	entries, err := os.ReadDir(storePath)
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial store after failed init: entries=%v err=%v", entries, err)
	}
	staging, err := filepath.Glob(filepath.Join(repo, ".pact.init-*"))
	if err != nil || len(staging) != 0 {
		t.Fatalf("staging paths after failed init: %v err=%v", staging, err)
	}
	if err := os.Remove(storePath); err != nil {
		t.Fatal(err)
	}
	beforePublish = original
	if _, err := Init(repo, "org/example/widget", time.Now()); err != nil {
		t.Fatalf("Init() retry error = %v", err)
	}
}

func TestOpenRejectsNonStrictFormatJSON(t *testing.T) {
	st := testStore(t)
	path := filepath.Join(st.Dir(), "format.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, malformed := range strictJSONMutations(raw) {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, malformed, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(filepath.Dir(st.Dir())); err == nil {
				t.Fatal("Open() error = nil, want strict JSON refusal")
			}
		})
	}
}

func TestPutCanonicalIsIdempotent(t *testing.T) {
	st := testStore(t)
	value := map[string]any{"kind": "test", "count": 1}
	objectID, created, err := st.PutCanonical(value)
	if err != nil || !created {
		t.Fatalf("first PutCanonical() = (%q, %v, %v), want created object", objectID, created, err)
	}
	againID, created, err := st.PutCanonical(value)
	if err != nil || created || againID != objectID {
		t.Fatalf("second PutCanonical() = (%q, %v, %v), want (%q, false, nil)", againID, created, err, objectID)
	}
}

func TestObjectFilesEnumeratesCanonicalObjectPaths(t *testing.T) {
	st := testStore(t)
	first, _, err := st.PutCanonical(map[string]any{"kind": "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := st.PutCanonical(map[string]any{"kind": "second"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := st.ObjectFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].ID >= files[1].ID || (files[0].ID != first && files[1].ID != first) || (files[0].ID != second && files[1].ID != second) {
		t.Fatalf("ObjectFiles() = %#v, want sorted canonical objects %q and %q", files, first, second)
	}
}

func TestWithReadLockBlocksCanonicalPublication(t *testing.T) {
	st := testStore(t)
	helper := startStoreLockHelper(t, st, "publish")
	if err := st.WithReadLock(func() error {
		waitForStoreLockHelper(t, helper.started)
		assertStoreLockHelperBlocked(t, helper.acquired)
		return nil
	}); err != nil {
		t.Fatalf("WithReadLock() error = %v", err)
	}
	if err := helper.command.Wait(); err != nil {
		t.Fatalf("canonical publication helper = %v", err)
	}
}

func TestMutationLockBlocksReadLock(t *testing.T) {
	st := testStore(t)
	helper := startStoreLockHelper(t, st, "read")
	if err := st.WithMutationLock(func() error {
		waitForStoreLockHelper(t, helper.started)
		assertStoreLockHelperBlocked(t, helper.acquired)
		return nil
	}); err != nil {
		t.Fatalf("WithMutationLock() error = %v", err)
	}
	if err := helper.command.Wait(); err != nil {
		t.Fatalf("read lock helper = %v", err)
	}
}

func TestObjectFilesBoundedStopsAtFirstExcess(t *testing.T) {
	st := testStore(t)
	for _, value := range []string{"first", "second"} {
		if _, _, err := st.PutCanonical(map[string]any{"kind": value}); err != nil {
			t.Fatal(err)
		}
	}
	if files, err := st.ObjectFilesBounded(1); err == nil || files != nil {
		t.Fatalf("ObjectFilesBounded() = (%#v, %v), want no partial result and limit error", files, err)
	} else {
		var limit *ObjectCountLimitError
		if !errors.As(err, &limit) || limit.Maximum != 1 {
			t.Fatalf("ObjectFilesBounded() error = %#v, want typed maximum 1", err)
		}
	}
}

func TestObjectFilesBoundedContextHonorsCancellationDuringEnumeration(t *testing.T) {
	st := testStore(t)
	shard := filepath.Join(st.Dir(), "objects", "sha256", "aa")
	if err := os.MkdirAll(shard, 0o755); err != nil {
		t.Fatal(err)
	}
	for index := range objectDirectoryBatchSize + 1 {
		if err := os.WriteFile(filepath.Join(shard, fmt.Sprintf("%062x.json", index)), []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	original := afterObjectDirectoryBatch
	afterObjectDirectoryBatch = func() { cancel() }
	t.Cleanup(func() { afterObjectDirectoryBatch = original })
	files, err := st.ObjectFilesBoundedContext(ctx, 100)
	if !errors.Is(err, context.Canceled) || files != nil {
		t.Fatalf("ObjectFilesBoundedContext() = (%#v, %v), want cancellation", files, err)
	}
}

func TestGetBoundedRejectsOversizeBeforeRead(t *testing.T) {
	st := testStore(t)
	raw := []byte(`{"kind":"` + strings.Repeat("x", 1024) + `"}`)
	objectID := canonical.Digest(raw)
	path := objectFile(st, objectID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := st.GetBounded(objectID, uint64(len(raw)-1)); err == nil || got != nil {
		t.Fatalf("GetBounded() = (%q, %v), want no bytes and limit error", got, err)
	} else {
		var limit *ObjectByteLimitError
		if !errors.As(err, &limit) || limit.Maximum != uint64(len(raw)-1) {
			t.Fatalf("GetBounded() error = %#v, want typed byte limit", err)
		}
	}
}

func TestGetBoundedMarksDigestMismatch(t *testing.T) {
	st := testStore(t)
	raw := []byte(`{"kind":"stable"}`)
	objectID := canonical.Digest(raw)
	path := objectFile(st, objectID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"kind":"changed"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetBounded(objectID, 1_000); !errors.Is(err, ErrIntegrity) || !errors.Is(err, ErrObjectDigestMismatch) {
		t.Fatalf("GetBounded() error = %v, want integrity and digest sentinels", err)
	}
}

func TestGetBoundedRejectsGrowthAfterStat(t *testing.T) {
	st := testStore(t)
	raw := []byte(`{"kind":"stable"}`)
	objectID := canonical.Digest(raw)
	path := objectFile(st, objectID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	original := afterGetBoundedStat
	afterGetBoundedStat = func(grownPath string) error {
		file, err := os.OpenFile(grownPath, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = file.Write([]byte("x"))
		return err
	}
	t.Cleanup(func() { afterGetBoundedStat = original })
	if got, err := st.GetBounded(objectID, uint64(len(raw))); err == nil || got != nil {
		t.Fatalf("GetBounded() after growth = (%q, %v), want no bytes and limit error", got, err)
	}
}

func TestObjectFilesBoundedStopsWithinLargeShard(t *testing.T) {
	st := testStore(t)
	shard := filepath.Join(st.Dir(), "objects", "sha256", "aa")
	if err := os.MkdirAll(shard, 0o755); err != nil {
		t.Fatal(err)
	}
	for index := range 65 {
		name := fmt.Sprintf("%062x.json", index)
		if err := os.WriteFile(filepath.Join(shard, name), []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if objectDirectoryBatchSize >= 65 {
		t.Fatalf("object directory batch size = %d, want bounded chunks smaller than the shard", objectDirectoryBatchSize)
	}
	if files, err := st.ObjectFilesBounded(1); err == nil || files != nil {
		t.Fatalf("ObjectFilesBounded() = (%#v, %v), want no partial result at the second object", files, err)
	}
}

func TestWithMutationLockRetainsPhaseOneErrorWording(t *testing.T) {
	if err := (*Store)(nil).WithMutationLock(func() error { return nil }); err == nil || err.Error() != "store and mutation operation are required" {
		t.Fatalf("nil store WithMutationLock() error = %v", err)
	}
	st := &Store{repo: "\x00"}
	if err := st.WithMutationLock(func() error { return nil }); err == nil || !strings.HasPrefix(err.Error(), "lock store mutation: ") {
		t.Fatalf("WithMutationLock() lock error = %v", err)
	}
}

func TestObjectFilesRejectsSymlinkedShard(t *testing.T) {
	st := testStore(t)
	escaped := t.TempDir()
	if err := os.Symlink(escaped, filepath.Join(st.Dir(), "objects", "sha256", "aa")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ObjectFiles(); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ObjectFiles() error = %v, want symlink refusal", err)
	}
}

func TestPutCanonicalRejectsSymlinkedObjectDirectory(t *testing.T) {
	st := testStore(t)
	objects := filepath.Join(st.Dir(), "objects")
	escaped := t.TempDir()
	if err := os.RemoveAll(objects); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(escaped, objects); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutCanonical(map[string]any{"kind": "test"}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("PutCanonical() error = %v, want symlink refusal", err)
	}
	entries, err := os.ReadDir(escaped)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink target changed: entries=%v err=%v", entries, err)
	}
}

func TestPutCanonicalRecreatesDisposableTempDirectory(t *testing.T) {
	st := testStore(t)
	firstID, _, err := st.PutCanonical(map[string]any{"kind": "first"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(st.Dir(), "tmp")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutCanonical(map[string]any{"kind": "second"}); err != nil {
		t.Fatalf("PutCanonical() after tmp removal error = %v", err)
	}
	if _, err := st.Get(firstID); err != nil {
		t.Fatalf("Get(first object) after tmp removal error = %v", err)
	}
}

func TestPutCanonicalRefusesMissingImmutableObjectTree(t *testing.T) {
	st := testStore(t)
	objects := filepath.Join(st.Dir(), "objects")
	if err := os.RemoveAll(objects); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutCanonical(map[string]any{"kind": "test"}); err == nil {
		t.Fatal("PutCanonical() error = nil, want missing immutable tree refusal")
	}
	if _, err := os.Stat(objects); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("immutable object tree was recreated: err=%v", err)
	}
}

func TestPutCanonicalRefusesDifferentBytesAtSameDigestPath(t *testing.T) {
	st := testStore(t)
	value := map[string]any{"kind": "test"}
	raw, err := canonical.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	objectID := canonical.Digest(raw)
	path := objectFile(st, objectID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"kind":"tampered"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutCanonical(value); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("PutCanonical() error = %v, want collision refusal", err)
	}
}

func TestPutCanonicalVerifiesPersistedHash(t *testing.T) {
	st := testStore(t)
	original := afterLink
	afterLink = func(path string) error {
		return os.WriteFile(path, []byte(`{"kind":"tampered"}`), 0o644)
	}
	t.Cleanup(func() { afterLink = original })
	if _, _, err := st.PutCanonical(map[string]any{"kind": "test"}); err == nil || !strings.Contains(err.Error(), "post-write") {
		t.Fatalf("PutCanonical() error = %v, want post-write verification failure", err)
	}
	if _, err := os.Stat(objectFile(st, canonical.Digest([]byte(`{"kind":"test"}`)))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canonical object remains after failed digest check: err=%v", err)
	}
}

func TestPutCanonicalRollsBackEveryPostLinkFailureAndSyncsRemoval(t *testing.T) {
	value := map[string]any{"kind": "test"}
	raw, err := canonical.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		inject func(*testing.T, string)
	}{
		{name: "shard sync", inject: func(t *testing.T, _ string) {
			original := syncDirectoryFile
			calls := 0
			syncDirectoryFile = func(path string) error {
				calls++
				if calls == 1 {
					return errors.New("injected shard sync failure")
				}
				return original(path)
			}
			t.Cleanup(func() { syncDirectoryFile = original })
		}},
		{name: "post-link hook", inject: func(t *testing.T, _ string) {
			original := afterLink
			afterLink = func(string) error { return errors.New("injected post-link hook failure") }
			t.Cleanup(func() { afterLink = original })
		}},
		{name: "readback", inject: func(t *testing.T, destination string) {
			original := readCanonicalFile
			readCanonicalFile = func(path string) ([]byte, error) {
				if path == destination {
					return nil, errors.New("injected readback failure")
				}
				return original(path)
			}
			t.Cleanup(func() { readCanonicalFile = original })
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			st := testStore(t)
			path := objectFile(st, canonical.Digest(raw))
			test.inject(t, path)
			if _, _, err := st.PutCanonical(value); err == nil {
				t.Fatal("PutCanonical() error = nil, want injected failure")
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("canonical link remains after failure: err=%v", err)
			}
		})
	}
}

func TestPutCanonicalSerializesRollbackAgainstIdenticalAdmission(t *testing.T) {
	st := testStore(t)
	value := map[string]any{"kind": "test"}
	linked := make(chan struct{})
	release := make(chan struct{})
	original := afterLink
	calls := 0
	afterLink = func(string) error {
		calls++
		if calls == 1 {
			close(linked)
			<-release
			return errors.New("injected first admission failure")
		}
		return nil
	}
	t.Cleanup(func() { afterLink = original })
	type result struct {
		created bool
		err     error
	}
	results := make(chan result, 2)
	go func() {
		_, created, err := st.PutCanonical(value)
		results <- result{created: created, err: err}
	}()
	<-linked
	go func() {
		_, created, err := st.PutCanonical(value)
		results <- result{created: created, err: err}
	}()
	close(release)
	admissions := []result{<-results, <-results}
	failures, successes := 0, 0
	for _, admission := range admissions {
		if admission.err != nil {
			failures++
		} else if admission.created {
			successes++
		}
	}
	if failures != 1 || successes != 1 {
		t.Fatalf("admissions = %#v, want one failure and one created success", admissions)
	}
	if _, err := st.Get(canonical.Digest([]byte(`{"kind":"test"}`))); err != nil {
		t.Fatalf("successful concurrent object missing: %v", err)
	}
}

func TestStoreMutationLockSerializesAndIgnoresStaleFile(t *testing.T) {
	st := testStore(t)
	lockPath, err := mutationLockPath(st.repo)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(lockPath) == filepath.Clean(os.TempDir()) {
		t.Fatalf("mutation lock is a predictable file directly under the shared temp directory: %s", lockPath)
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 2)
	go func() {
		done <- st.WithMutationLock(func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	secondEntered := make(chan struct{})
	go func() {
		done <- st.WithMutationLock(func() error {
			close(secondEntered)
			return nil
		})
	}()
	select {
	case <-secondEntered:
		t.Fatal("second mutation entered before first released")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPutCanonicalLeavesNoObjectAfterPreLinkFailure(t *testing.T) {
	st := testStore(t)
	original := linkFile
	linkFile = func(_, _ string) error { return errors.New("injected pre-link failure") }
	t.Cleanup(func() { linkFile = original })
	value := map[string]any{"kind": "test"}
	raw, err := canonical.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutCanonical(value); err == nil || !strings.Contains(err.Error(), "injected pre-link") {
		t.Fatalf("PutCanonical() error = %v, want injected failure", err)
	}
	if _, err := os.Stat(objectFile(st, canonical.Digest(raw))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canonical object exists after failed admission: err=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(st.Dir(), "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("tmp contents after failed admission = %v, err=%v", entries, err)
	}
}

func TestGetRejectsTamperedObject(t *testing.T) {
	st := testStore(t)
	objectID, _, err := st.PutCanonical(map[string]any{"kind": "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectFile(st, objectID), []byte(`{"kind":"tampered"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(objectID); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("Get() error = %v, want digest mismatch", err)
	}
}

func TestStoreLockHelperProcess(t *testing.T) {
	if os.Getenv("PACT_STORE_LOCK_HELPER") == "" {
		return
	}
	st, err := Open(os.Getenv("PACT_STORE_LOCK_REPO"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("PACT_STORE_LOCK_STARTED"), []byte("started"), 0o600); err != nil {
		t.Fatal(err)
	}
	acquired := func() error {
		return os.WriteFile(os.Getenv("PACT_STORE_LOCK_ACQUIRED"), []byte("acquired"), 0o600)
	}
	switch os.Getenv("PACT_STORE_LOCK_HELPER") {
	case "publish":
		_, _, err = st.PutCanonical(map[string]any{"kind": "helper"})
		if err == nil {
			err = acquired()
		}
	case "read":
		err = st.WithReadLock(acquired)
	default:
		t.Fatalf("unknown store lock helper mode %q", os.Getenv("PACT_STORE_LOCK_HELPER"))
	}
	if err != nil {
		t.Fatal(err)
	}
}

type storeLockHelper struct {
	command  *exec.Cmd
	started  string
	acquired string
}

func startStoreLockHelper(t *testing.T, st *Store, mode string) storeLockHelper {
	t.Helper()
	directory := t.TempDir()
	helper := storeLockHelper{
		command:  exec.Command(os.Args[0], "-test.run=^TestStoreLockHelperProcess$"),
		started:  filepath.Join(directory, "started"),
		acquired: filepath.Join(directory, "acquired"),
	}
	helper.command.Env = append(os.Environ(), "PACT_STORE_LOCK_HELPER="+mode, "PACT_STORE_LOCK_REPO="+st.Root(), "PACT_STORE_LOCK_STARTED="+helper.started, "PACT_STORE_LOCK_ACQUIRED="+helper.acquired)
	if err := helper.command.Start(); err != nil {
		t.Fatal(err)
	}
	return helper
}

func waitForStoreLockHelper(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("store lock helper did not start: %s", path)
}

func assertStoreLockHelperBlocked(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("store lock helper acquired lock before release: %s", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Init(t.TempDir(), "org/example/widget", time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func objectFile(st *Store, objectID string) string {
	hexDigest := strings.TrimPrefix(objectID, "sha256:")
	return filepath.Join(st.Dir(), "objects", "sha256", hexDigest[:2], hexDigest[2:]+".json")
}

func strictJSONMutations(raw []byte) map[string][]byte {
	return map[string][]byte{
		"duplicate":     bytes.Replace(raw, []byte(`"format": "pact/store/v1"`), []byte(`"format": "bad", "format": "pact/store/v1"`), 1),
		"nfc collision": append([]byte("{\"e\\u0301\":1,\"é\":2,"), raw[1:]...),
		"BOM":           append([]byte{0xef, 0xbb, 0xbf}, raw...),
		"trailing":      append(append([]byte{}, raw...), []byte("{}")...),
	}
}

func resolvedPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
