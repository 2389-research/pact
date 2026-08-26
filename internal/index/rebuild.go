// ABOUTME: Builds one validated SQLite snapshot and publishes it with a same-directory atomic rename.
// ABOUTME: Holds the store mutation lock across scan, durability, publication, and unchanged-source proof.
package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"pact/internal/ledger"
	"pact/internal/store"
)

const writerOptions = "_foreign_keys=1&_journal_mode=DELETE&_synchronous=FULL&_busy_timeout=5000"

var (
	commitIndexTransaction    = func(transaction *sql.Tx) error { return transaction.Commit() }
	closeIndexWriter          = func(db *sql.DB) error { return db.Close() }
	syncIndexFile             = syncRegularIndexFile
	renameIndexFile           = os.Rename
	syncIndexDirectory        = syncRealIndexDirectory
	beforeBuiltValidation     = func(string) error { return nil }
	beforePublishedValidation = func(string) error { return nil }
	beforeIndexWrite          = func() error { return nil }
	execPreparedIndexRow      = func(ctx context.Context, prepared *sql.Stmt, values ...any) (sql.Result, error) {
		return prepared.ExecContext(ctx, values...)
	}
)

// Rebuild creates and validates a disposable index, then atomically publishes it.
func (m *Manager) Rebuild(ctx context.Context) (result RebuildResult, err error) {
	if m == nil || m.store == nil || ctx == nil {
		return result, fmt.Errorf("index rebuild requires a store and context")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	err = m.store.WithMutationLock(func() error {
		var rebuildErr error
		result, rebuildErr = m.rebuildLocked(ctx)
		return rebuildErr
	})
	return result, err
}

func (m *Manager) rebuildLocked(ctx context.Context) (result RebuildResult, err error) { //nolint:gocyclo,gocognit,funlen // The linear branches are the ordered durability protocol.
	indexDirectory := filepath.Join(m.store.Root(), ".pact", "index")
	livePath := filepath.Join(indexDirectory, liveIndexName)
	if err := validateIndexPaths(m.store.Dir(), indexDirectory, livePath); err != nil {
		return result, fmt.Errorf("validate index paths: %w", err)
	}
	if err := cleanStaleBuilds(indexDirectory); err != nil {
		return result, fmt.Errorf("clean stale index builds: %w", err)
	}
	scan, err := ledger.Scan(ctx, m.store, ledger.ScanOptions{Limits: ledger.Phase2Limits})
	if err != nil {
		if contextError(err) {
			return result, err
		}
		if errors.Is(err, ledger.ErrIntegrity) || errors.Is(err, store.ErrIntegrity) {
			return result, fmt.Errorf("scan rebuild source: %w", &QueryError{Code: "source_invalid"})
		}
		return result, fmt.Errorf("scan rebuild source: %w", err)
	}
	if !scan.Verification.OK {
		return result, fmt.Errorf("scan rebuild source: %w", &QueryError{Code: "source_invalid"})
	}
	_, liveStatErr := os.Lstat(livePath)
	replaced := liveStatErr == nil
	if liveStatErr != nil && !errors.Is(liveStatErr, fs.ErrNotExist) {
		return result, fmt.Errorf("inspect live index: %w", liveStatErr)
	}
	tempFile, err := os.CreateTemp(indexDirectory, ".build-*.sqlite3")
	if err != nil {
		return result, fmt.Errorf("create index build file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			if cleanupErr := cleanupCurrentBuild(tempPath); cleanupErr != nil {
				err = errors.Join(err, cleanupErr)
			}
		}
	}()
	if err := tempFile.Chmod(0o600); err != nil {
		return result, fmt.Errorf("set index build mode: %w", errors.Join(err, tempFile.Close()))
	}
	if err := tempFile.Close(); err != nil {
		return result, fmt.Errorf("close new index build file: %w", err)
	}
	if err := validateBuildFile(tempPath); err != nil {
		return result, err
	}
	snapshot, err := Project(ctx, scan)
	if err != nil {
		if contextError(err) {
			return result, err
		}
		return result, fmt.Errorf("project rebuild source: %w", err)
	}
	if err := writeSnapshot(ctx, tempPath, snapshot); err != nil {
		return result, err
	}
	if err := beforeBuiltValidation(tempPath); err != nil {
		return result, fmt.Errorf("prepare built-index validation: %w", err)
	}
	if err := refuseSidecars(tempPath); err != nil {
		return result, err
	}
	info, err := validateIndex(ctx, tempPath, scan)
	if err != nil {
		if contextError(err) {
			return result, err
		}
		return result, fmt.Errorf("validate built index: %w", err)
	}
	if info.State != "current" {
		return result, fmt.Errorf("validate built index: %w", &QueryError{Code: "index_" + strings.ReplaceAll(info.State, "-", "_")})
	}
	if err := syncIndexFile(tempPath); err != nil {
		return result, fmt.Errorf("sync built index: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := validateBuildFile(tempPath); err != nil {
		return result, err
	}
	if err := refuseSidecars(tempPath); err != nil {
		return result, err
	}
	if err := refuseSidecars(livePath); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := renameIndexFile(tempPath, livePath); err != nil {
		return result, fmt.Errorf("publish index: %w: %w", &QueryError{Code: "index_publication_failed"}, err)
	}
	cleanupTemp = false
	result.Created = !replaced
	result.Replaced = replaced
	if err := syncIndexDirectory(indexDirectory); err != nil {
		return result, fmt.Errorf("sync published index directory: %w: %w", &QueryError{Code: "index_publication_failed"}, err)
	}
	if err := beforePublishedValidation(livePath); err != nil {
		return result, fmt.Errorf("prepare published-index validation: %w", err)
	}
	info, err = validateIndex(ctx, livePath, scan)
	if err != nil {
		if contextError(err) {
			return result, err
		}
		return result, fmt.Errorf("validate published index: %w", err)
	}
	if info.State != "current" {
		return result, fmt.Errorf("validate published index: %w", &QueryError{Code: "index_" + strings.ReplaceAll(info.State, "-", "_")})
	}
	if err := proveCanonicalSourceUnchanged(ctx, m.store, scan.SourceFingerprint); err != nil {
		return result, fmt.Errorf("validate canonical source after rebuild: %w", err)
	}
	status := Status{Index: info, Replica: replicaInfo(scan), Counts: sourceCounts(scan.Counts), Limits: LimitsInfo{Profile: ledger.LimitsProfile, Status: "within_limits"}}
	result.Status = status
	return result, nil
}

func proveCanonicalSourceUnchanged(ctx context.Context, st *store.Store, sourceFingerprint string) error {
	after, err := ledger.Scan(ctx, st, ledger.ScanOptions{Limits: ledger.Phase2Limits})
	if err != nil {
		if contextError(err) {
			return err
		}
		return fmt.Errorf("%w: %w", &QueryError{Code: "source_changed"}, err)
	}
	if !after.Verification.OK || after.SourceFingerprint != sourceFingerprint {
		return &QueryError{Code: "source_changed"}
	}
	return nil
}

func validateIndexPaths(storeDirectory, indexDirectory, livePath string) error {
	storeInfo, err := os.Lstat(storeDirectory)
	if err != nil {
		return err
	}
	if storeInfo.Mode()&fs.ModeSymlink != 0 || !storeInfo.IsDir() {
		return fmt.Errorf("store directory is not a real directory")
	}
	directory, err := os.Lstat(indexDirectory)
	if err != nil {
		return err
	}
	if directory.Mode()&fs.ModeSymlink != 0 || !directory.IsDir() {
		return fmt.Errorf("index directory is not a real directory")
	}
	info, err := os.Lstat(livePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("live index is not a regular non-symlink file")
	}
	return nil
}

func cleanStaleBuilds(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !isBuildName(entry.Name()) {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing unsafe stale index build")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func isBuildName(name string) bool {
	if !strings.HasPrefix(name, ".build-") || !strings.HasSuffix(name, ".sqlite3") {
		return false
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(name, ".build-"), ".sqlite3")
	if middle == "" {
		return false
	}
	for _, character := range middle {
		if !isASCIIAlphaNumeric(character) {
			return false
		}
	}
	return true
}

func validateBuildFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect index build file: %w", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return fmt.Errorf("index build file is not regular non-symlink mode 0600")
	}
	// #nosec G115 -- the preceding negative-size branch guards the conversion.
	if info.Size() < 0 || uint64(info.Size()) > ledger.Phase2Limits.SQLiteBytes {
		return fmt.Errorf("index build exceeds resource limit")
	}
	return nil
}

func writerDSN(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String() + "?" + writerOptions
}

func writeSnapshot(ctx context.Context, path string, snapshot Snapshot) (err error) {
	db, err := sql.Open("sqlite", writerDSN(path))
	if err != nil {
		return fmt.Errorf("open index writer")
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, db.Close())
		}
	}()
	transaction, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin index build transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	if err := createSchemaInTransaction(ctx, transaction); err != nil {
		return fmt.Errorf("install index schema: %w", err)
	}
	if err := beforeIndexWrite(); err != nil {
		return fmt.Errorf("prepare index row write: %w", err)
	}
	if err := insertSnapshot(ctx, transaction, snapshot); err != nil {
		return err
	}
	if err := commitIndexTransaction(transaction); err != nil {
		return fmt.Errorf("commit index build: %w", err)
	}
	committed = true
	if err := closeIndexWriter(db); err != nil {
		closed = true
		return fmt.Errorf("close index writer: %w", err)
	}
	closed = true
	return nil
}

func insertSnapshot(ctx context.Context, transaction *sql.Tx, snapshot Snapshot) error {
	if err := insertRows(ctx, transaction, "INSERT INTO index_meta(key,value) VALUES(?,?)", len(snapshot.IndexMeta), func(index int) []any { row := snapshot.IndexMeta[index]; return []any{row.Key, row.Value} }); err != nil {
		return err
	}
	if err := insertRows(ctx, transaction, "INSERT INTO objects VALUES(?,?,?,?,?,?,?,?,?,?,?)", len(snapshot.Objects), func(index int) []any {
		row := snapshot.Objects[index]
		return []any{row.ObjectID, row.ObjectType, row.Namespace, row.BodyDigest, row.ActorKeyID, row.ActorLabel, row.ObservedAt, row.IntegrityState, row.StructureState, row.AuthenticityState, row.CompletenessState}
	}); err != nil {
		return err
	}
	if err := insertRows(ctx, transaction, "INSERT INTO commits VALUES(?,?)", len(snapshot.Commits), func(index int) []any { row := snapshot.Commits[index]; return []any{row.CommitID, row.EventCount} }); err != nil {
		return err
	}
	if err := insertRows(ctx, transaction, "INSERT INTO parent_edges VALUES(?,?,?)", len(snapshot.ParentEdges), func(index int) []any {
		row := snapshot.ParentEdges[index]
		return []any{row.ChildID, row.ParentID, row.Resolved}
	}); err != nil {
		return err
	}
	if err := insertRows(ctx, transaction, "INSERT INTO events VALUES(?,?,?,?,?,?,?,?,?)", len(snapshot.Events), func(index int) []any {
		row := snapshot.Events[index]
		return []any{row.EventRef, row.CommitID, row.LocalID, row.Kind, row.EventType, row.Subject, row.SchemaRef, nullableUint64(row.CausalBatch), row.CausalStatus}
	}); err != nil {
		return err
	}
	if err := insertRows(ctx, transaction, "INSERT INTO event_tags VALUES(?,?)", len(snapshot.EventTags), func(index int) []any { row := snapshot.EventTags[index]; return []any{row.EventRef, row.Tag} }); err != nil {
		return err
	}
	if err := insertRows(ctx, transaction, "INSERT INTO event_links VALUES(?,?,?,?)", len(snapshot.EventLinks), func(index int) []any {
		row := snapshot.EventLinks[index]
		return []any{row.SourceRef, row.Relation, row.TargetRef, row.Resolved}
	}); err != nil {
		return err
	}
	if err := insertRows(ctx, transaction, "INSERT INTO checkpoints VALUES(?,?,?,?,?)", len(snapshot.Checkpoints), func(index int) []any {
		row := snapshot.Checkpoints[index]
		return []any{row.CheckpointID, row.Scope, row.PolicyRef, row.AuthorityEpoch, nullableString(row.PreviousCheckpoint)}
	}); err != nil {
		return err
	}
	if err := insertRows(ctx, transaction, "INSERT INTO checkpoint_schema_refs VALUES(?,?)", len(snapshot.CheckpointSchemaRefs), func(index int) []any {
		row := snapshot.CheckpointSchemaRefs[index]
		return []any{row.CheckpointID, row.SchemaRef}
	}); err != nil {
		return err
	}
	if err := insertRows(ctx, transaction, "INSERT INTO checkpoint_frontier VALUES(?,?,?,?)", len(snapshot.CheckpointFrontier), func(index int) []any {
		row := snapshot.CheckpointFrontier[index]
		return []any{row.CheckpointID, row.Namespace, row.HeadID, row.Resolved}
	}); err != nil {
		return err
	}
	if err := insertRows(ctx, transaction, "INSERT INTO heads VALUES(?,?)", len(snapshot.Heads), func(index int) []any { row := snapshot.Heads[index]; return []any{row.Namespace, row.CommitID} }); err != nil {
		return err
	}
	return insertRows(ctx, transaction, "INSERT INTO completeness_blockers VALUES(?,?,?,?)", len(snapshot.CompletenessBlockers), func(index int) []any {
		row := snapshot.CompletenessBlockers[index]
		return []any{row.SourceID, row.Code, row.Field, row.MissingRef}
	})
}

func insertRows(ctx context.Context, transaction *sql.Tx, statement string, count int, values func(int) []any) (err error) {
	prepared, err := transaction.PrepareContext(ctx, statement)
	if err != nil {
		return safeIndexWriteError("prepare index row write", err)
	}
	defer func() {
		if closeErr := prepared.Close(); closeErr != nil {
			err = errors.Join(err, safeIndexWriteError("close index row write", closeErr))
		}
	}()
	for index := range count {
		if _, err := execPreparedIndexRow(ctx, prepared, values(index)...); err != nil {
			return safeIndexWriteError("write index row", err)
		}
	}
	return nil
}

type indexWriteError struct {
	message string
	cause   error
}

func (err *indexWriteError) Error() string { return err.message }

func (err *indexWriteError) Unwrap() error { return err.cause }

func safeIndexWriteError(message string, cause error) error {
	if errors.Is(cause, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return &indexWriteError{message: message, cause: cause}
}

func refuseSidecars(path string) error {
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect index sidecar: %w", err)
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing unsafe index sidecar")
		}
		return fmt.Errorf("refusing unexpected index sidecar")
	}
	return nil
}

func cleanupCurrentBuild(path string) error {
	candidates := []string{path, path + "-journal", path + "-wal", path + "-shm"}
	present := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		info, err := os.Lstat(candidate)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing unsafe index build cleanup")
		}
		present = append(present, candidate)
	}
	for _, candidate := range present {
		if err := os.Remove(candidate); err != nil {
			return err
		}
	}
	return nil
}

func isASCIIAlphaNumeric(character rune) bool {
	return character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
}

func syncRegularIndexFile(path string) (err error) {
	// #nosec G304 -- path is the fixed live index or an Lstat-checked same-directory build file.
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	return file.Sync()
}

func syncRealIndexDirectory(path string) (err error) {
	// #nosec G304 -- path is the fixed Lstat-checked index directory.
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, directory.Close()) }()
	if err := directory.Sync(); err != nil && !errors.Is(err, fs.ErrInvalid) {
		return err
	}
	return nil
}
