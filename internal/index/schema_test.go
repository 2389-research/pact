// ABOUTME: Verifies the exact disposable SQLite index schema against a real database.
// ABOUTME: Covers schema identity, table shape, constraints, foreign keys, and indexes.
package index

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

type tableShape struct {
	columns []string
	strict  int
	without int
}

type foreignKeyShape struct{ table, from, to string }

func TestCreateSchemaInstallsExactSQLiteV1Schema(t *testing.T) {
	db := openSchemaDatabase(t)

	if err := createSchema(context.Background(), db); err != nil {
		t.Fatalf("createSchema() error = %v", err)
	}

	if got := pragmaInteger(t, db, "application_id"); got != ApplicationID {
		t.Errorf("application_id = %#x, want %#x", got, ApplicationID)
	}
	if got := pragmaInteger(t, db, "user_version"); got != SchemaVersion {
		t.Errorf("user_version = %d, want %d", got, SchemaVersion)
	}
	if got := pragmaInteger(t, db, "foreign_keys"); got != 1 {
		t.Errorf("foreign_keys = %d, want 1", got)
	}

	wantTables := map[string]tableShape{
		"index_meta":             {columns: []string{"key", "value"}, strict: 1, without: 1},
		"objects":                {columns: []string{"object_id", "object_type", "namespace", "body_digest", "actor_key_id", "actor_label", "observed_at", "integrity_state", "structure_state", "authenticity_state", "completeness_state"}, strict: 1, without: 1},
		"commits":                {columns: []string{"commit_id", "event_count"}, strict: 1, without: 1},
		"parent_edges":           {columns: []string{"child_id", "parent_id", "resolved"}, strict: 1, without: 1},
		"events":                 {columns: []string{"event_ref", "commit_id", "local_id", "kind", "event_type", "subject", "schema_ref", "causal_batch", "causal_status"}, strict: 1, without: 1},
		"event_tags":             {columns: []string{"event_ref", "tag"}, strict: 1, without: 1},
		"event_links":            {columns: []string{"source_ref", "relation", "target_ref", "resolved"}, strict: 1, without: 1},
		"checkpoints":            {columns: []string{"checkpoint_id", "scope", "policy_ref", "authority_epoch", "previous_checkpoint"}, strict: 1, without: 1},
		"checkpoint_schema_refs": {columns: []string{"checkpoint_id", "schema_ref"}, strict: 1, without: 1},
		"checkpoint_frontier":    {columns: []string{"checkpoint_id", "namespace", "head_id", "resolved"}, strict: 1, without: 1},
		"heads":                  {columns: []string{"namespace", "commit_id"}, strict: 1, without: 1},
		"completeness_blockers":  {columns: []string{"source_id", "code", "field", "missing_ref"}, strict: 1, without: 1},
	}
	if got := schemaTables(t, db); !reflect.DeepEqual(got, wantTables) {
		t.Errorf("schema tables = %#v, want %#v", got, wantTables)
	}

	wantIndexes := []string{
		"event_links_target_idx", "event_tags_tag_idx", "events_commit_idx", "events_kind_idx",
		"events_order_idx", "events_schema_idx", "events_subject_idx", "events_type_idx",
		"objects_actor_idx", "objects_namespace_idx", "parent_edges_parent_idx",
	}
	if got := schemaObjects(t, db, "index"); !reflect.DeepEqual(got, wantIndexes) {
		t.Errorf("explicit indexes = %v, want %v", got, wantIndexes)
	}
	wantIndexColumns := map[string][]string{
		"objects_namespace_idx":   {"namespace", "object_type", "object_id"},
		"objects_actor_idx":       {"actor_key_id", "object_id"},
		"events_type_idx":         {"event_type", "causal_batch", "event_ref"},
		"events_kind_idx":         {"kind", "causal_batch", "event_ref"},
		"events_subject_idx":      {"subject", "causal_batch", "event_ref"},
		"events_schema_idx":       {"schema_ref", "causal_batch", "event_ref"},
		"events_order_idx":        {"causal_status", "causal_batch", "event_ref"},
		"events_commit_idx":       {"commit_id", "local_id"},
		"event_tags_tag_idx":      {"tag", "event_ref"},
		"event_links_target_idx":  {"target_ref", "relation", "source_ref"},
		"parent_edges_parent_idx": {"parent_id", "child_id"},
	}
	for name, want := range wantIndexColumns {
		if got := indexColumns(t, db, name); !reflect.DeepEqual(got, want) {
			t.Errorf("index %s columns = %v, want %v", name, got, want)
		}
	}
	wantForeignKeys := map[string][]foreignKeyShape{
		"index_meta": {}, "objects": {}, "completeness_blockers": {},
		"commits":                {{table: "objects", from: "commit_id", to: "object_id"}},
		"parent_edges":           {{table: "commits", from: "child_id", to: "commit_id"}},
		"events":                 {{table: "commits", from: "commit_id", to: "commit_id"}},
		"event_tags":             {{table: "events", from: "event_ref", to: "event_ref"}},
		"event_links":            {{table: "events", from: "source_ref", to: "event_ref"}},
		"checkpoints":            {{table: "objects", from: "checkpoint_id", to: "object_id"}},
		"checkpoint_schema_refs": {{table: "checkpoints", from: "checkpoint_id", to: "checkpoint_id"}},
		"checkpoint_frontier":    {{table: "checkpoints", from: "checkpoint_id", to: "checkpoint_id"}},
		"heads":                  {{table: "commits", from: "commit_id", to: "commit_id"}},
	}
	for table, want := range wantForeignKeys {
		if got := tableForeignKeys(t, db, table); !reflect.DeepEqual(got, want) {
			t.Errorf("table %s foreign keys = %#v, want %#v", table, got, want)
		}
	}
	if got := schemaObjects(t, db, "view"); len(got) != 0 {
		t.Errorf("views = %v, want none", got)
	}
	if got := schemaObjects(t, db, "trigger"); len(got) != 0 {
		t.Errorf("triggers = %v, want none", got)
	}
}

func indexColumns(t *testing.T, db *sql.DB, index string) []string {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf("PRAGMA index_info(%q)", index))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var sequence, columnID int
		var name string
		if err := rows.Scan(&sequence, &columnID, &name); err != nil {
			t.Fatal(err)
		}
		result = append(result, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func tableForeignKeys(t *testing.T, db *sql.DB, table string) []foreignKeyShape {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf("PRAGMA foreign_key_list(%q)", table))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := []foreignKeyShape{}
	for rows.Next() {
		var id, sequence int
		var targetTable, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &sequence, &targetTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatal(err)
		}
		if onUpdate != "NO ACTION" || onDelete != "NO ACTION" || match != "NONE" {
			t.Errorf("table %s foreign key actions = (%s, %s, %s), want NO ACTION/NO ACTION/NONE", table, onUpdate, onDelete, match)
		}
		result = append(result, foreignKeyShape{table: targetTable, from: from, to: to})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCreateSchemaEnforcesChecksAndForeignKeys(t *testing.T) {
	db := openSchemaDatabase(t)
	if err := createSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	assertExecFails(t, db, "INSERT INTO objects VALUES ('bad','blob','ns','digest','key','label','time','valid','valid','valid','complete')", "object type check")
	assertExecFails(t, db, "INSERT INTO commits VALUES ('missing',1)", "commit object foreign key")
	assertExecFails(t, db, "INSERT INTO events VALUES ('ref','missing','local','action','type','subject','schema',NULL,'ordered')", "event causal check")
	assertExecFails(t, db, "INSERT INTO event_links VALUES ('missing','related_to','target',0)", "event relation check")
	assertExecFails(t, db, "INSERT INTO completeness_blockers VALUES ('source','unknown','field','missing')", "blocker code check")
}

func TestSchemaDigestFixedVector(t *testing.T) {
	const want = "sha256:47a6275d425e9ff9158b544d52b125544817b34f06818c5e629151e81a8a833c"
	if got := SchemaDigest(); got != want {
		t.Fatalf("SchemaDigest() = %q, want %q", got, want)
	}
}

func openSchemaDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "index.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	return db
}

func pragmaInteger(t *testing.T, db *sql.DB, name string) int {
	t.Helper()
	var value int
	if err := db.QueryRow("PRAGMA " + name).Scan(&value); err != nil {
		t.Fatalf("read PRAGMA %s: %v", name, err)
	}
	return value
}

func schemaTables(t *testing.T, db *sql.DB) map[string]tableShape {
	t.Helper()
	rows, err := db.Query("PRAGMA table_list")
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]tableShape)
	for rows.Next() {
		var schema, name, kind string
		var columns, without, strict int
		if err := rows.Scan(&schema, &name, &kind, &columns, &without, &strict); err != nil {
			t.Fatal(err)
		}
		if schema != "main" || kind != "table" || strings.HasPrefix(name, "sqlite_") {
			continue
		}
		result[name] = tableShape{strict: strict, without: without}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for name, shape := range result {
		shape.columns = tableColumns(t, db, name)
		result[name] = shape
	}
	return result
}

func tableColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		result = append(result, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func schemaObjects(t *testing.T, db *sql.DB, kind string) []string {
	t.Helper()
	rows, err := db.Query("SELECT name FROM sqlite_schema WHERE type = ? AND name NOT LIKE 'sqlite_%' ORDER BY name", kind)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		result = append(result, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(result)
	return result
}

func assertExecFails(t *testing.T, db *sql.DB, statement, name string) {
	t.Helper()
	if _, err := db.Exec(statement); err == nil {
		t.Errorf("%s succeeded, want constraint error", name)
	}
}
