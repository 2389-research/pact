// ABOUTME: Exercises index state classification through real SQLite files and signed source scans.
// ABOUTME: Proves status is read-only and separates replica coverage from derived database damage.
package index

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pact/internal/ledger"
	"pact/internal/store"
)

func TestValidateRejectsConnectionWithoutFixedReaderPragmas(t *testing.T) {
	_, path, _ := managerFixture(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close fixture database: %v", err)
		}
	}()
	if _, state := inspectDatabase(context.Background(), db); state != "corrupt" {
		t.Fatalf("state = %q, want corrupt without fixed query-only reader settings", state)
	}
}

func TestStatusClassifiesCurrentCompleteReplica(t *testing.T) {
	st, err := store.Init(t.TempDir(), "fixture/status", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	scan, err := ledger.Scan(context.Background(), st, ledger.ScanOptions{Limits: ledger.Phase2Limits})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(st.Dir(), "index", "pact-v1.sqlite3")
	writeSnapshotFixture(t, path, Project(scan))
	status, err := New(st).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Index.State != "current" || status.Index.Coverage != "complete" || status.Replica.Completeness != "locally_closed" {
		t.Fatalf("status = %#v", status)
	}
}

func TestStatusClassifiesMissingWithoutMutatingIndexDirectory(t *testing.T) {
	st, err := store.Init(t.TempDir(), "fixture/status", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	temp := filepath.Join(st.Dir(), "index", ".build-leftover.sqlite3")
	if err := os.WriteFile(temp, []byte("left alone"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := New(st).Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Index.State != "missing" || status.Index.Coverage != "unavailable" || !status.Index.RebuildRequired {
		t.Fatalf("missing status = %#v", status)
	}
	if status.Index.Path != nil || status.Index.SchemaVersion != nil || status.Index.SourceFingerprint != nil || status.Index.LogicalDigest != nil {
		t.Fatalf("missing optional values = %#v, want nil", status.Index)
	}
	if status.Replica.Completeness != "locally_closed" || status.Limits.Status != "within_limits" {
		t.Fatalf("source status = %#v", status)
	}
	if _, err := os.Stat(temp); err != nil {
		t.Fatalf("Status removed stale temp: %v", err)
	}
}

func TestStatusClassifiesCurrentPartialReplica(t *testing.T) {
	st, path, snapshot := managerFixture(t)
	status, err := New(st).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Index.State != "current" || status.Index.Coverage != "partial" || status.Index.RebuildRequired {
		t.Fatalf("index status = %#v", status.Index)
	}
	if status.Index.Path == nil || *status.Index.Path != path || status.Index.SchemaVersion == nil || *status.Index.SchemaVersion != SchemaVersion {
		t.Fatalf("index identity = %#v", status.Index)
	}
	if status.Index.LogicalDigest == nil || *status.Index.LogicalDigest != metadataValue(snapshot, "logical_digest") {
		t.Fatalf("logical digest = %#v", status.Index.LogicalDigest)
	}
	if status.Replica.Completeness != "incomplete" || len(status.Replica.Blockers) == 0 {
		t.Fatalf("replica = %#v", status.Replica)
	}
}

func TestStatusClassifiesRecognizedDatabaseStates(t *testing.T) {
	tests := []struct {
		name, want string
		mutate     func(*testing.T, string, Snapshot)
	}{
		{name: "corrupt metadata digest", want: "corrupt", mutate: func(t *testing.T, path string, _ Snapshot) {
			mutateSQLiteFixture(t, path, "UPDATE index_meta SET value='sha256:broken' WHERE key='logical_digest'")
		}},
		{name: "incompatible application ID", want: "incompatible", mutate: func(t *testing.T, path string, _ Snapshot) {
			mutateSQLiteFixture(t, path, "PRAGMA application_id=7")
		}},
		{name: "incompatible user version", want: "incompatible", mutate: func(t *testing.T, path string, _ Snapshot) {
			mutateSQLiteFixture(t, path, "PRAGMA user_version=2")
		}},
		{name: "unauthorized view", want: "corrupt", mutate: func(t *testing.T, path string, _ Snapshot) {
			mutateSQLiteFixture(t, path, "CREATE VIEW leak AS SELECT value FROM index_meta")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, path, snapshot := managerFixture(t)
			test.mutate(t, path, snapshot)
			status, err := New(st).Status(context.Background())
			if err != nil {
				t.Fatalf("Status() error = %v", err)
			}
			if status.Index.State != test.want || status.Index.Coverage != "unavailable" || !status.Index.RebuildRequired {
				t.Fatalf("status = %#v, want state %q", status.Index, test.want)
			}
		})
	}
}

func TestStatusValidationDamageMatrix(t *testing.T) {
	tests := []struct {
		name, want string
		mutate     func(*testing.T, string)
	}{
		{name: "metadata key missing", want: "corrupt", mutate: func(t *testing.T, path string) {
			mutateSQLiteFixture(t, path, "DELETE FROM index_meta WHERE key='limits_contract'")
		}},
		{name: "metadata numeric malformed", want: "corrupt", mutate: func(t *testing.T, path string) {
			mutateSQLiteFixture(t, path, "UPDATE index_meta SET value='many' WHERE key='row_count_events'")
		}},
		{name: "metadata format unsupported", want: "incompatible", mutate: func(t *testing.T, path string) {
			mutateSQLiteFixture(t, path, "UPDATE index_meta SET value='pact/sqlite-index/v2' WHERE key='format'")
		}},
		{name: "metadata schema digest unsupported", want: "incompatible", mutate: func(t *testing.T, path string) {
			mutateSQLiteFixture(t, path, "UPDATE index_meta SET value=? WHERE key='schema_digest'", "sha256:"+strings.Repeat("a", 64))
		}},
		{name: "required index missing", want: "incompatible", mutate: func(t *testing.T, path string) {
			mutateSQLiteFixture(t, path, "DROP INDEX events_type_idx")
		}},
		{name: "required column missing", want: "incompatible", mutate: func(t *testing.T, path string) {
			writeDamagedSchemaFixture(t, path, strings.Replace(schemaDDL, "  actor_label TEXT NOT NULL,\n", "", 1))
		}},
		{name: "required check changed", want: "incompatible", mutate: func(t *testing.T, path string) {
			writeDamagedSchemaFixture(t, path, strings.Replace(schemaDDL, "('commit','checkpoint')", "('commit','checkpoint','blob')", 1))
		}},
		{name: "unauthorized table", want: "corrupt", mutate: func(t *testing.T, path string) {
			mutateSQLiteFixture(t, path, "CREATE TABLE extra(value TEXT) STRICT")
		}},
		{name: "unauthorized trigger", want: "corrupt", mutate: func(t *testing.T, path string) {
			mutateSQLiteFixture(t, path, "CREATE TRIGGER extra_trigger AFTER UPDATE ON index_meta BEGIN SELECT 1; END")
		}},
		{name: "typed row changed", want: "corrupt", mutate: func(t *testing.T, path string) {
			mutateSQLiteFixture(t, path, "UPDATE objects SET actor_label='changed' WHERE object_id=(SELECT object_id FROM objects LIMIT 1)")
		}},
		{name: "stored row count changed", want: "corrupt", mutate: func(t *testing.T, path string) {
			mutateSQLiteFixture(t, path, "UPDATE index_meta SET value='999' WHERE key='row_count_objects'")
		}},
		{name: "foreign key violation", want: "corrupt", mutate: func(t *testing.T, path string) {
			mutateSQLiteFixture(t, path, "DELETE FROM objects WHERE object_id=(SELECT commit_id FROM commits LIMIT 1)")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, path, _ := managerFixture(t)
			test.mutate(t, path)
			status, err := New(st).Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if status.Index.State != test.want {
				t.Fatalf("state = %q, want %q", status.Index.State, test.want)
			}
		})
	}
}

func TestStatusClassifiesUnsafeAndOversizeFilesAsCorrupt(t *testing.T) {
	t.Run("live symlink", func(t *testing.T) {
		st, path, _ := managerFixture(t)
		target := filepath.Join(t.TempDir(), "target.sqlite3")
		if err := os.Rename(path, target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		status, err := New(st).Status(context.Background())
		if err != nil || status.Index.State != "corrupt" {
			t.Fatalf("status = %#v, error = %v", status, err)
		}
	})

	t.Run("sparse oversize file", func(t *testing.T) {
		st, path, _ := managerFixture(t)
		if err := os.Truncate(path, int64(ledger.Phase2Limits.SQLiteBytes)+1); err != nil {
			t.Fatal(err)
		}
		status, err := New(st).Status(context.Background())
		if err != nil || status.Index.State != "corrupt" {
			t.Fatalf("status = %#v, error = %v", status, err)
		}
	})

	t.Run("damaged SQLite header", func(t *testing.T) {
		st, path, _ := managerFixture(t)
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteAt([]byte("not sqlite"), 0); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		status, err := New(st).Status(context.Background())
		if err != nil || status.Index.State != "corrupt" {
			t.Fatalf("status = %#v, error = %v", status, err)
		}
	})
}

func TestStatusClassifiesPartialBuildBeforeStale(t *testing.T) {
	st, path, snapshot := managerFixture(t)
	rogueID := "sha256:" + strings.Repeat("9", 64)
	rogueRef := rogueID + "#rogue"
	snapshot.Objects = append(snapshot.Objects, ObjectRow{
		ObjectID: rogueID, ObjectType: "commit", Namespace: "fixture/rogue", BodyDigest: rogueID,
		ActorKeyID: "ed25519:" + strings.Repeat("8", 64), ActorLabel: "Rogue", ObservedAt: "2026-08-24T12:00:00Z",
		IntegrityState: "valid", StructureState: "valid", AuthenticityState: "valid", CompletenessState: "complete",
	})
	snapshot.Commits = append(snapshot.Commits, CommitRow{CommitID: rogueID, EventCount: 1})
	batch := uint64(0)
	snapshot.Events = append(snapshot.Events, EventRow{EventRef: rogueRef, CommitID: rogueID, LocalID: "rogue", Kind: "action", EventType: "fixture.rogue", Subject: "fixture/rogue", SchemaRef: "pact:core/fixture/v1", CausalBatch: &batch, CausalStatus: "ordered"})
	snapshot.Heads = append(snapshot.Heads, HeadRow{Namespace: "fixture/rogue", CommitID: rogueID})
	sortSnapshot(&snapshot)
	setFixtureMetadata(snapshot.IndexMeta, "source_count_objects", "7")
	setFixtureMetadata(snapshot.IndexMeta, "source_count_commits", "5")
	setFixtureMetadata(snapshot.IndexMeta, "source_count_events", "6")
	setFixtureMetadata(snapshot.IndexMeta, "source_count_edges", "21")
	setFixtureMetadata(snapshot.IndexMeta, "row_count_objects", "7")
	setFixtureMetadata(snapshot.IndexMeta, "row_count_commits", "5")
	setFixtureMetadata(snapshot.IndexMeta, "row_count_events", "6")
	setFixtureMetadata(snapshot.IndexMeta, "row_count_heads", "4")
	digest, err := LogicalDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	setFixtureMetadata(snapshot.IndexMeta, "logical_digest", digest)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeSnapshotFixture(t, path, snapshot)
	status, err := New(st).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Index.State != "partial-build" {
		t.Fatalf("state = %q, want partial-build", status.Index.State)
	}
}

func TestStatusClassifiesStaleWhenRowsRemainInternallyValid(t *testing.T) {
	st, path, _ := managerFixture(t)
	mutateSQLiteFixture(t, path, "UPDATE index_meta SET value=? WHERE key='source_fingerprint'", "sha256:"+strings.Repeat("7", 64))
	refreshFixtureLogicalDigest(t, path)
	status, err := New(st).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Index.State != "stale" {
		t.Fatalf("state = %q, want stale", status.Index.State)
	}
}

func setFixtureMetadata(rows []IndexMetaRow, key, value string) {
	for index := range rows {
		if rows[index].Key == key {
			rows[index].Value = value
			return
		}
	}
}

func refreshFixtureLogicalDigest(t *testing.T, path string) {
	t.Helper()
	snapshot := readSnapshotFixture(t, path)
	digest, err := LogicalDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	mutateSQLiteFixture(t, path, "UPDATE index_meta SET value=? WHERE key='logical_digest'", digest)
}
