// ABOUTME: Exercises index state classification through real SQLite files and signed source scans.
// ABOUTME: Proves status is read-only and separates replica coverage from derived database damage.
package index

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pact/internal/identity"
	"pact/internal/ledger"
	"pact/internal/store"
)

func TestValidateRejectsConnectionWithoutFixedReaderPragmas(t *testing.T) {
	_, path, snapshot := managerFixture(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close fixture database: %v", err)
		}
	}()
	if _, state, _ := inspectDatabase(context.Background(), db, snapshot); state != "corrupt" {
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

func TestStatusInvalidSourceRetainsStableCode(t *testing.T) {
	st, err := store.Init(t.TempDir(), "fixture/invalid-status", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutCanonical(map[string]any{"invalid": true}); err != nil {
		t.Fatal(err)
	}
	_, err = New(st).Status(context.Background())
	if err == nil {
		t.Fatal("Status() succeeded with invalid source")
	}
	assertQueryErrorCode(t, err, "source_invalid")
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

func TestStatusClassificationPrecedence(t *testing.T) {
	t.Run("corrupt beats incompatible", func(t *testing.T) {
		st, path, _ := managerFixture(t)
		mutateSQLiteFixture(t, path, "PRAGMA application_id=7")
		mutateSQLiteFixture(t, path, "UPDATE index_meta SET value='sha256:broken' WHERE key='logical_digest'")
		status, err := New(st).Status(context.Background())
		if err != nil || status.Index.State != "corrupt" {
			t.Fatalf("status = %#v, error = %v; want corrupt", status, err)
		}
	})

	t.Run("same-source row divergence is partial-build", func(t *testing.T) {
		st, path, snapshot := managerFixture(t)
		snapshot.Objects[0].ActorLabel = "divergent"
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
		if err != nil || status.Index.State != "partial-build" {
			t.Fatalf("status = %#v, error = %v; want partial-build", status, err)
		}
	})

	t.Run("corrupt metadata beats safely queryable incompatible schema", func(t *testing.T) {
		st, path, _ := managerFixture(t)
		mutateSQLiteFixture(t, path, "DROP INDEX events_type_idx")
		mutateSQLiteFixture(t, path, "UPDATE index_meta SET value='sha256:broken' WHERE key='logical_digest'")
		status, err := New(st).Status(context.Background())
		if err != nil || status.Index.State != "corrupt" {
			t.Fatalf("status = %#v, error = %v; want corrupt", status, err)
		}
	})
}

func TestStatusClassifiesValidSourceShrinkAsStale(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	st, err := store.Init(t.TempDir(), "fixture/shrink", now)
	if err != nil {
		t.Fatal(err)
	}
	seed := make([]byte, ed25519.SeedSize)
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)
	keyID, err := identity.KeyID(public)
	if err != nil {
		t.Fatal(err)
	}
	key := &identity.KeyFile{Actor: "Shrink Fixture", KeyID: keyID, Public: public, Private: private, CreatedAt: now}
	if _, err := ledger.AddRoot(st, key, now); err != nil {
		t.Fatal(err)
	}
	commitFixture(t, st, key, "fixture/shrink/one", []string{}, now, fixtureEvent("one", "observation", nil, nil, nil))
	removed := commitFixture(t, st, key, "fixture/shrink/two", []string{}, now, fixtureEvent("two", "observation", nil, nil, nil))
	manager := New(st)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(removed.Path); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Index.State != "stale" {
		t.Fatalf("source-shrink state = %q, want stale", status.Index.State)
	}
}

func TestDeclaredStoredCountsEnforceFixedBoundsAndRelationships(t *testing.T) {
	valid := map[string]string{
		"row_count_index_meta": "25", "row_count_objects": "2", "row_count_commits": "1",
		"row_count_parent_edges": "0", "row_count_events": "1", "row_count_event_tags": "1",
		"row_count_event_links": "0", "row_count_checkpoints": "1", "row_count_checkpoint_schema_refs": "1",
		"row_count_checkpoint_frontier": "0", "row_count_heads": "1", "row_count_completeness_blockers": "0",
		"source_count_objects": "2", "source_count_commits": "1", "source_count_checkpoints": "1",
		"source_count_events": "1", "source_count_edges": "2", "source_count_canonical_bytes": "100",
	}
	if _, ok := declaredStoredCounts(valid); !ok {
		t.Fatal("valid declared counts rejected")
	}
	tests := []struct{ key, value string }{
		{key: "source_count_objects", value: "100001"},
		{key: "row_count_events", value: "250001"},
		{key: "source_count_edges", value: "1000001"},
		{key: "source_count_canonical_bytes", value: "1073741825"},
		{key: "row_count_event_tags", value: "101"},
		{key: "row_count_checkpoint_schema_refs", value: "101"},
		{key: "row_count_commits", value: "2"},
		{key: "row_count_index_meta", value: "24"},
	}
	for _, test := range tests {
		t.Run(test.key+"="+test.value, func(t *testing.T) {
			meta := make(map[string]string, len(valid))
			maps.Copy(meta, valid)
			meta[test.key] = test.value
			if _, ok := declaredStoredCounts(meta); ok {
				t.Fatalf("unsafe declared counts accepted: %s=%s", test.key, test.value)
			}
		})
	}
	hostile := []struct {
		name      string
		overrides map[string]string
	}{
		{name: "commit addition operand", overrides: map[string]string{"row_count_commits": "18446744073709551615", "source_count_commits": "18446744073709551615"}},
		{name: "checkpoint addition operand", overrides: map[string]string{"row_count_checkpoints": "18446744073709551615", "source_count_checkpoints": "18446744073709551615"}},
		{name: "parent edge addition operand", overrides: map[string]string{"row_count_parent_edges": "18446744073709551615", "source_count_edges": "1"}},
		{name: "event link addition operand", overrides: map[string]string{"row_count_event_links": "18446744073709551615", "source_count_edges": "1"}},
		{name: "frontier addition operand", overrides: map[string]string{"row_count_checkpoint_frontier": "18446744073709551615", "source_count_edges": "1"}},
	}
	for _, test := range hostile {
		t.Run(test.name, func(t *testing.T) {
			meta := make(map[string]string, len(valid))
			maps.Copy(meta, valid)
			maps.Copy(meta, test.overrides)
			if _, ok := declaredStoredCounts(meta); ok {
				t.Fatalf("overflowing declared counts accepted: %#v", test.overrides)
			}
		})
	}
}

func TestStatusValidationDamageMatrix(t *testing.T) {
	tests := []struct {
		name, want string
		mutate     func(*testing.T, string, Snapshot)
	}{
		{name: "metadata key missing", want: "corrupt", mutate: func(t *testing.T, path string, _ Snapshot) {
			mutateSQLiteFixture(t, path, "DELETE FROM index_meta WHERE key='limits_contract'")
		}},
		{name: "metadata numeric malformed", want: "corrupt", mutate: func(t *testing.T, path string, _ Snapshot) {
			mutateSQLiteFixture(t, path, "UPDATE index_meta SET value='many' WHERE key='row_count_events'")
		}},
		{name: "metadata format unsupported", want: "incompatible", mutate: func(t *testing.T, path string, expected Snapshot) {
			mutateSQLiteFixture(t, path, "UPDATE index_meta SET value='pact/sqlite-index/v2' WHERE key='format'")
			refreshFixtureLogicalDigest(t, path, expected)
		}},
		{name: "metadata schema digest unsupported", want: "incompatible", mutate: func(t *testing.T, path string, expected Snapshot) {
			mutateSQLiteFixture(t, path, "UPDATE index_meta SET value=? WHERE key='schema_digest'", "sha256:"+strings.Repeat("a", 64))
			refreshFixtureLogicalDigest(t, path, expected)
		}},
		{name: "required index missing", want: "incompatible", mutate: func(t *testing.T, path string, _ Snapshot) {
			mutateSQLiteFixture(t, path, "DROP INDEX events_type_idx")
		}},
		{name: "required column missing", want: "incompatible", mutate: func(t *testing.T, path string, _ Snapshot) {
			writeDamagedSchemaFixture(t, path, strings.Replace(schemaDDL, "  actor_label TEXT NOT NULL,\n", "", 1))
		}},
		{name: "required check changed", want: "incompatible", mutate: func(t *testing.T, path string, _ Snapshot) {
			writeDamagedSchemaFixture(t, path, strings.Replace(schemaDDL, "('commit','checkpoint')", "('commit','checkpoint','blob')", 1))
		}},
		{name: "unauthorized table", want: "corrupt", mutate: func(t *testing.T, path string, _ Snapshot) {
			mutateSQLiteFixture(t, path, "CREATE TABLE extra(value TEXT) STRICT")
		}},
		{name: "unauthorized trigger", want: "corrupt", mutate: func(t *testing.T, path string, _ Snapshot) {
			mutateSQLiteFixture(t, path, "CREATE TRIGGER extra_trigger AFTER UPDATE ON index_meta BEGIN SELECT 1; END")
		}},
		{name: "typed row changed", want: "corrupt", mutate: func(t *testing.T, path string, _ Snapshot) {
			mutateSQLiteFixture(t, path, "UPDATE objects SET actor_label='changed' WHERE object_id=(SELECT object_id FROM objects LIMIT 1)")
		}},
		{name: "stored row count changed", want: "corrupt", mutate: func(t *testing.T, path string, _ Snapshot) {
			mutateSQLiteFixture(t, path, "UPDATE index_meta SET value='999' WHERE key='row_count_objects'")
		}},
		{name: "foreign key violation", want: "corrupt", mutate: func(t *testing.T, path string, _ Snapshot) {
			mutateSQLiteFixture(t, path, "DELETE FROM objects WHERE object_id=(SELECT commit_id FROM commits LIMIT 1)")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, path, expected := managerFixture(t)
			test.mutate(t, path, expected)
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

func TestStatusMissingIndexDirectoryIsMissing(t *testing.T) {
	st, err := store.Init(t.TempDir(), "fixture/status", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(st.Dir(), "index")); err != nil {
		t.Fatal(err)
	}
	status, err := New(st).Status(context.Background())
	if err != nil || status.Index.State != "missing" {
		t.Fatalf("status = %#v, error = %v", status, err)
	}
}

func TestStatusRejectsLiveSidecars(t *testing.T) {
	st, path, _ := managerFixture(t)
	if err := os.WriteFile(path+"-wal", []byte("leftover"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := New(st).Status(context.Background())
	if err != nil || status.Index.State != "corrupt" {
		t.Fatalf("status = %#v, error = %v", status, err)
	}
}

func TestValidationPropagatesCancellationDuringRepresentativeIteration(t *testing.T) {
	st, _, _ := managerFixture(t)
	resetValidationSeams(t)
	ctx, cancel := context.WithCancel(context.Background())
	afterRepresentativeIndexRow = cancel
	_, err := New(st).Status(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Status() error = %v, want context canceled", err)
	}
}

func TestValidationPropagatesCancellationDuringStreamedRowValidation(t *testing.T) {
	st, _, _ := managerFixture(t)
	resetValidationSeams(t)
	ctx, cancel := context.WithCancel(context.Background())
	afterValidatedIndexRow = cancel
	_, err := New(st).Status(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Status() error = %v, want context canceled", err)
	}
}

func TestStatusClassifiesRealQuickCheckDamageCorrupt(t *testing.T) {
	st, path, _ := managerFixture(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var rootPage, pageSize int64
	if err := db.QueryRow("SELECT rootpage FROM sqlite_schema WHERE name='objects'").Scan(&rootPage); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteAt(make([]byte, 128), (rootPage-1)*pageSize)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("damage fixture: write=%v close=%v", writeErr, closeErr)
	}
	damaged, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var quick string
	quickErr := damaged.QueryRow("PRAGMA quick_check").Scan(&quick)
	if closeErr := damaged.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if quickErr == nil && quick == "ok" {
		t.Fatal("fixture damage did not fail SQLite quick_check")
	}
	status, err := New(st).Status(context.Background())
	if err != nil || status.Index.State != "corrupt" {
		t.Fatalf("status = %#v, error = %v", status, err)
	}
}

func TestReaderCloseFailureClassifiesCorrupt(t *testing.T) {
	st, _, _ := managerFixture(t)
	resetValidationSeams(t)
	fault := errors.New("reader close failed")
	closeIndexReader = func(db *sql.DB) error { return errors.Join(db.Close(), fault) }
	status, err := New(st).Status(context.Background())
	if err != nil || status.Index.State != "corrupt" {
		t.Fatalf("status = %#v, error = %v", status, err)
	}
}

func TestValidationPreflightRejectsExcessRowsAtAdmissionSentinel(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE hostile_rows(value TEXT); INSERT INTO hostile_rows VALUES('a'),('b'),('c')"); err != nil {
		t.Fatal(err)
	}
	table := streamTable{name: "hostile_rows", textLengthQuery: "SELECT coalesce(max(octet_length(value)),0) FROM hostile_rows", maximumRows: 2}
	if _, err := preflightTable(context.Background(), db, table); err == nil {
		t.Fatal("excess SQL row count passed its fixed bound")
	}
}

func TestValidationPreflightUsesStoredTextBytes(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE multibyte(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	value := strings.Repeat("é", int(ledger.Phase2Limits.ObjectBytes/2+1))
	if _, err := db.Exec("INSERT INTO multibyte VALUES(?)", value); err != nil {
		t.Fatal(err)
	}
	var characters, bytes int64
	if err := db.QueryRow("SELECT length(value),octet_length(value) FROM multibyte").Scan(&characters, &bytes); err != nil {
		t.Fatalf("modernc/sqlite v1.57.0 octet_length support: %v", err)
	}
	if characters >= bytes || bytes <= int64(ledger.Phase2Limits.ObjectBytes) {
		t.Fatalf("length=%d octet_length=%d, want multibyte byte overflow", characters, bytes)
	}
	table := streamTable{name: "multibyte", textLengthQuery: "SELECT coalesce(max(octet_length(value)),0) FROM multibyte", maximumRows: 1}
	if _, err := preflightTable(context.Background(), db, table); err == nil {
		t.Fatal("stored multibyte TEXT passed the byte limit")
	}
	for _, table := range streamingTables(tableBounds{}) {
		withoutOctets := strings.ReplaceAll(table.textLengthQuery, "octet_length(", "")
		if strings.Contains(withoutOctets, "length(") {
			t.Fatalf("%s bound query uses character length: %s", table.name, table.textLengthQuery)
		}
	}
}

func TestStatusHandlesSparse900MiBDatabaseWithoutMaterializingIt(t *testing.T) {
	st, path, _ := managerFixture(t)
	if err := os.Truncate(path, int64(900*1024*1024)); err != nil {
		t.Fatal(err)
	}
	status, err := New(st).Status(context.Background())
	if err != nil || status.Index.State != "current" {
		t.Fatalf("status = %#v, error = %v", status, err)
	}
}

func resetValidationSeams(t *testing.T) {
	t.Helper()
	oldClose, oldAfterRepresentative, oldAfterValidated := closeIndexReader, afterRepresentativeIndexRow, afterValidatedIndexRow
	t.Cleanup(func() {
		closeIndexReader, afterRepresentativeIndexRow, afterValidatedIndexRow = oldClose, oldAfterRepresentative, oldAfterValidated
	})
	closeIndexReader = func(db *sql.DB) error { return db.Close() }
	afterRepresentativeIndexRow = func() {}
	afterValidatedIndexRow = func() {}
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

func TestStatusClassifiesSelfConsistentRowsBeyondCurrentSourceAsPartialBuild(t *testing.T) {
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
	st, path, expected := managerFixture(t)
	mutateSQLiteFixture(t, path, "UPDATE index_meta SET value=? WHERE key='source_fingerprint'", "sha256:"+strings.Repeat("7", 64))
	refreshFixtureLogicalDigest(t, path, expected)
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

func refreshFixtureLogicalDigest(t *testing.T, path string, expected Snapshot) {
	t.Helper()
	db, err := openIndexReader(path)
	if err != nil {
		t.Fatal(err)
	}
	meta, readErr := readMetadata(context.Background(), db)
	if readErr != nil {
		db.Close()
		t.Fatal(readErr)
	}
	declared, ok := declaredStoredCounts(meta)
	if !ok {
		db.Close()
		t.Fatal("fixture metadata counts are invalid")
	}
	digest, _, _, readErr := readSnapshotDB(context.Background(), db, expected, declared)
	closeErr := db.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if digest == "" {
		t.Fatal("streamed fixture digest is empty")
	}
	mutateSQLiteFixture(t, path, "UPDATE index_meta SET value=? WHERE key='logical_digest'", digest)
}
