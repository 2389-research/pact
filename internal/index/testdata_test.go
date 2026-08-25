// ABOUTME: Builds real SQLite index fixtures for manager integration tests.
// ABOUTME: Keeps signed-ledger and database mutation setup out of behavior assertions.
package index

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"pact/internal/ledger"
	"pact/internal/store"

	_ "modernc.org/sqlite"
)

func writeDamagedSchemaFixture(t *testing.T, path, ddl string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ddl); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA application_id=1346454356; PRAGMA user_version=1"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func managerFixture(t *testing.T) (*store.Store, string, Snapshot) {
	t.Helper()
	fixture := signedPartialScanFixture(t)
	path := filepath.Join(fixture.store.Dir(), "index", "pact-v1.sqlite3")
	snapshot := mustProject(t, fixture.scan)
	writeSnapshotFixture(t, path, snapshot)
	return fixture.store, path, snapshot
}

func writeSnapshotFixture(t *testing.T, path string, snapshot Snapshot) {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_foreign_keys=1&_journal_mode=DELETE&_synchronous=FULL&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	if err := createSchema(context.Background(), db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	insert := func(statement string, rows ...[]any) {
		t.Helper()
		prepared, prepareErr := tx.Prepare(statement)
		if prepareErr != nil {
			t.Fatal(prepareErr)
		}
		defer prepared.Close()
		for _, row := range rows {
			if _, execErr := prepared.Exec(row...); execErr != nil {
				t.Fatal(execErr)
			}
		}
	}
	insert("INSERT INTO index_meta(key,value) VALUES(?,?)", mapRows(snapshot.IndexMeta, func(row IndexMetaRow) []any { return []any{row.Key, row.Value} })...)
	insert("INSERT INTO objects VALUES(?,?,?,?,?,?,?,?,?,?,?)", mapRows(snapshot.Objects, func(row ObjectRow) []any {
		return []any{row.ObjectID, row.ObjectType, row.Namespace, row.BodyDigest, row.ActorKeyID, row.ActorLabel, row.ObservedAt, row.IntegrityState, row.StructureState, row.AuthenticityState, row.CompletenessState}
	})...)
	insert("INSERT INTO commits VALUES(?,?)", mapRows(snapshot.Commits, func(row CommitRow) []any { return []any{row.CommitID, row.EventCount} })...)
	insert("INSERT INTO parent_edges VALUES(?,?,?)", mapRows(snapshot.ParentEdges, func(row ParentEdgeRow) []any { return []any{row.ChildID, row.ParentID, row.Resolved} })...)
	insert("INSERT INTO events VALUES(?,?,?,?,?,?,?,?,?)", mapRows(snapshot.Events, func(row EventRow) []any {
		return []any{row.EventRef, row.CommitID, row.LocalID, row.Kind, row.EventType, row.Subject, row.SchemaRef, nullableUint64(row.CausalBatch), row.CausalStatus}
	})...)
	insert("INSERT INTO event_tags VALUES(?,?)", mapRows(snapshot.EventTags, func(row EventTagRow) []any { return []any{row.EventRef, row.Tag} })...)
	insert("INSERT INTO event_links VALUES(?,?,?,?)", mapRows(snapshot.EventLinks, func(row EventLinkRow) []any { return []any{row.SourceRef, row.Relation, row.TargetRef, row.Resolved} })...)
	insert("INSERT INTO checkpoints VALUES(?,?,?,?,?)", mapRows(snapshot.Checkpoints, func(row CheckpointRow) []any {
		return []any{row.CheckpointID, row.Scope, row.PolicyRef, row.AuthorityEpoch, nullableString(row.PreviousCheckpoint)}
	})...)
	insert("INSERT INTO checkpoint_schema_refs VALUES(?,?)", mapRows(snapshot.CheckpointSchemaRefs, func(row CheckpointSchemaRefRow) []any { return []any{row.CheckpointID, row.SchemaRef} })...)
	insert("INSERT INTO checkpoint_frontier VALUES(?,?,?,?)", mapRows(snapshot.CheckpointFrontier, func(row CheckpointFrontierRow) []any {
		return []any{row.CheckpointID, row.Namespace, row.HeadID, row.Resolved}
	})...)
	insert("INSERT INTO heads VALUES(?,?)", mapRows(snapshot.Heads, func(row HeadRow) []any { return []any{row.Namespace, row.CommitID} })...)
	insert("INSERT INTO completeness_blockers VALUES(?,?,?,?)", mapRows(snapshot.CompletenessBlockers, func(row CompletenessBlockerRow) []any {
		return []any{row.SourceID, row.Code, row.Field, row.MissingRef}
	})...)
	if err := tx.Commit(); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func mustProject(t *testing.T, scan ledger.ScanResult) Snapshot {
	t.Helper()
	snapshot, err := Project(context.Background(), scan)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func mapRows[Row any](rows []Row, convert func(Row) []any) [][]any {
	result := make([][]any, len(rows))
	for index, row := range rows {
		result[index] = convert(row)
	}
	return result
}

func mutateSQLiteFixture(t *testing.T, path, statement string, arguments ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(statement, arguments...); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func indexPath(st interface{ Dir() string }) string {
	return filepath.Join(st.Dir(), "index", liveIndexName)
}
