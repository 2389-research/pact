// ABOUTME: Validates disposable SQLite indexes through one fixed read-only path.
// ABOUTME: Classifies format, physical, logical, source, and projection failures without repair.
package index

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"pact/internal/canonical"
	"pact/internal/ledger"
)

const (
	readerOptions         = "mode=ro&_query_only=1&_defensive=1&_foreign_keys=1&_busy_timeout=5000"
	maximumSQLiteFileSize = int64(2_147_483_648)
)

var (
	closeIndexReader            = func(db *sql.DB) error { return db.Close() }
	afterRepresentativeIndexRow = func() {}
	afterValidatedIndexRow      = func() {}
)

var metadataKeys = []string{
	"format", "limits_contract", "local_completeness", "logical_digest",
	"row_count_checkpoint_frontier", "row_count_checkpoint_schema_refs", "row_count_checkpoints", "row_count_commits",
	"row_count_completeness_blockers", "row_count_event_links", "row_count_event_tags", "row_count_events", "row_count_heads",
	"row_count_index_meta", "row_count_objects", "row_count_parent_edges", "schema_digest", "schema_version",
	"source_count_canonical_bytes", "source_count_checkpoints", "source_count_commits", "source_count_edges", "source_count_events",
	"source_count_objects", "source_fingerprint",
}

type databaseInspection struct {
	meta      map[string]string
	version   int
	divergent bool
}

type tableBounds map[string]uint64

func validateIndex(ctx context.Context, path string, scan ledger.ScanResult) (IndexInfo, error) { //nolint:gocyclo // The linear branches preserve the contract's state precedence.
	result := IndexInfo{State: "corrupt", Coverage: coverageNone, Path: new(path), RebuildRequired: true}
	directoryInfo, directoryErr := os.Lstat(filepath.Dir(path))
	if errors.Is(directoryErr, fs.ErrNotExist) {
		return IndexInfo{State: "missing", Coverage: coverageNone, RebuildRequired: true}, nil
	}
	if directoryErr != nil || directoryInfo.Mode()&fs.ModeSymlink != 0 || !directoryInfo.IsDir() {
		return result, nil //nolint:nilerr // Unsafe or unreadable index paths are corrupt index state.
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if hasIndexSidecar(path) {
			return result, nil
		}
		return IndexInfo{State: "missing", Coverage: coverageNone, RebuildRequired: true}, nil
	}
	if err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximumSQLiteFileSize || hasIndexSidecar(path) {
		return result, nil //nolint:nilerr // Unsafe or unreadable live files are corrupt index state.
	}
	db, err := openIndexReader(path)
	if err != nil {
		return result, nil //nolint:nilerr // An index that cannot open through the fixed reader is corrupt.
	}
	// Project has a fixed in-memory API; the bounded source scan already honored ctx.
	expected := Project(scan) //nolint:contextcheck
	inspection, state, validationErr := inspectDatabase(ctx, db, expected)
	closeErr := closeIndexReader(db)
	if contextErr := contextCause(ctx, validationErr, closeErr); contextErr != nil {
		return result, contextErr
	}
	if validationErr != nil || closeErr != nil {
		state = "corrupt"
	}
	result.State = state
	if inspection.version != 0 {
		result.SchemaVersion = new(inspection.version)
	}
	if fingerprint, found := inspection.meta["source_fingerprint"]; found && validDigest(fingerprint) {
		result.SourceFingerprint = new(fingerprint)
	}
	if digest, found := inspection.meta["logical_digest"]; found && validDigest(digest) {
		result.LogicalDigest = new(digest)
	}
	if state == "corrupt" || state == "incompatible" {
		return result, nil
	}
	if inspection.divergent {
		result.State = "partial-build"
		return result, nil
	}
	if inspection.meta["source_fingerprint"] != scan.SourceFingerprint {
		result.State = "stale"
		return result, nil
	}
	result.State = "current"
	result.RebuildRequired = false
	if scan.Completeness.Status == "locally_closed" {
		result.Coverage = "complete"
	} else {
		result.Coverage = "partial"
	}
	return result, nil
}

func inspectDatabase(ctx context.Context, db *sql.DB, expected Snapshot) (databaseInspection, string, error) { //nolint:gocyclo // Each gate maps to one required classification boundary.
	inspection := databaseInspection{meta: map[string]string{}}
	if err := db.PingContext(ctx); err != nil {
		return inspection, "corrupt", err
	}
	applicationID, err := readPragmaInteger(ctx, db, "application_id")
	if err != nil {
		return inspection, "corrupt", err
	}
	version, err := readPragmaInteger(ctx, db, "user_version")
	if err != nil {
		return inspection, "corrupt", err
	}
	inspection.version = version
	incompatible := applicationID != ApplicationID || version != SchemaVersion
	if err := validatePragmas(ctx, db); err != nil {
		return inspection, "corrupt", err
	}
	if err := validateTextLengthQuery(ctx, db, "SELECT coalesce(max(max(length(name),length(type),coalesce(length(sql),0))),0) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'"); err != nil {
		return inspection, "corrupt", err
	}
	state, err := validateSchema(ctx, db)
	if err != nil {
		return inspection, "corrupt", err
	}
	if state == "corrupt" {
		return inspection, state, nil
	}
	if state == "incompatible" {
		return inspection, "incompatible", nil
	}
	if err := validateTextLengthQuery(ctx, db, "SELECT coalesce(max(max(length(key),length(value))),0) FROM index_meta"); err != nil {
		return inspection, "corrupt", err
	}
	if err := validateRowCountQuery(ctx, db, "index_meta", uint64(len(metadataKeys))); err != nil {
		return inspection, "corrupt", err
	}
	meta, err := readMetadata(ctx, db)
	if err != nil {
		return inspection, "corrupt", err
	}
	inspection.meta = meta
	if meta["format"] != IndexFormat || meta["schema_version"] != strconv.Itoa(SchemaVersion) || meta["schema_digest"] != SchemaDigest() {
		incompatible = true
	}
	if meta["limits_contract"] != limitsContract || meta["local_completeness"] != "locally_closed" && meta["local_completeness"] != "incomplete" {
		return inspection, "corrupt", nil
	}
	if !validDigest(meta["source_fingerprint"]) || !validDigest(meta["logical_digest"]) {
		return inspection, "corrupt", nil
	}
	digest, divergent, counts, err := readSnapshotDB(ctx, db, expected)
	if err != nil {
		return inspection, "corrupt", err
	}
	inspection.divergent = divergent
	if !validStoredCounts(meta, counts) {
		return inspection, "corrupt", nil
	}
	if digest != meta["logical_digest"] {
		return inspection, "corrupt", nil
	}
	if err := representativeReads(ctx, db); err != nil {
		return inspection, "corrupt", err
	}
	if incompatible {
		return inspection, "incompatible", nil
	}
	return inspection, "current", nil
}

func contextCause(ctx context.Context, causes ...error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, cause := range causes {
		if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
			return cause
		}
	}
	return nil
}

func hasIndexSidecar(path string) bool {
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		if _, err := os.Lstat(path + suffix); !errors.Is(err, fs.ErrNotExist) {
			return true
		}
	}
	return false
}

func openIndexReader(path string) (*sql.DB, error) {
	location := (&url.URL{Scheme: "file", Path: path}).String() + "?" + readerOptions
	db, err := sql.Open("sqlite", location)
	if err != nil {
		return nil, fmt.Errorf("open index reader: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

func readPragmaInteger(ctx context.Context, db *sql.DB, name string) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, "PRAGMA "+name).Scan(&value); err != nil {
		return 0, fmt.Errorf("read index identity: %w", err)
	}
	return value, nil
}

type schemaObject struct{ kind, sql string }

func validateSchema(ctx context.Context, db *sql.DB) (string, error) {
	want, err := schemaObjectMap(ctx, nil)
	if err != nil {
		return "corrupt", err
	}
	got, err := schemaObjectMap(ctx, db)
	if err != nil {
		return "corrupt", err
	}
	for name, object := range got {
		if _, found := want[name]; !found {
			_ = object
			return "corrupt", nil
		}
	}
	if !reflect.DeepEqual(got, want) {
		return "incompatible", nil
	}
	return "current", nil
}

func schemaObjectMap(ctx context.Context, source *sql.DB) (result map[string]schemaObject, err error) {
	db := source
	if db == nil {
		db, err = sql.Open("sqlite", ":memory:")
		if err != nil {
			return nil, err
		}
		defer func() { err = errors.Join(err, db.Close()) }()
		if err := createSchema(ctx, db); err != nil {
			return nil, err
		}
	}
	rows, err := db.QueryContext(ctx, "SELECT name,type,sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	result = make(map[string]schemaObject)
	for rows.Next() {
		var name, kind, statement string
		if err := rows.Scan(&name, &kind, &statement); err != nil {
			return nil, err
		}
		result[name] = schemaObject{kind: kind, sql: normalizeSchemaSQL(statement)}
	}
	return result, rows.Err()
}

func normalizeSchemaSQL(statement string) string { return strings.Join(strings.Fields(statement), " ") }

func readMetadata(ctx context.Context, db *sql.DB) (result map[string]string, err error) {
	rows, err := db.QueryContext(ctx, "SELECT key,value FROM index_meta ORDER BY key")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	result = make(map[string]string)
	keys := make([]string, 0, len(metadataKeys))
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate index metadata")
		}
		result[key] = value
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(keys, metadataKeys) {
		return nil, fmt.Errorf("invalid index metadata key set")
	}
	return result, nil
}

func validatePragmas(ctx context.Context, db *sql.DB) (err error) {
	queryOnly, err := readPragmaInteger(ctx, db, "query_only")
	if err != nil || queryOnly != 1 {
		return fmt.Errorf("index reader is not query-only")
	}
	foreignKeys, err := readPragmaInteger(ctx, db, "foreign_keys")
	if err != nil || foreignKeys != 1 {
		return fmt.Errorf("index reader foreign keys are disabled")
	}
	var quick string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&quick); err != nil || quick != "ok" {
		return fmt.Errorf("index quick check failed")
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("index foreign key check failed")
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	if rows.Next() {
		return fmt.Errorf("index foreign key check failed")
	}
	return rows.Err()
}

type streamTable struct {
	name, query, textLengthQuery string
	maximumRows                  uint64
	scan                         func(*sql.Rows) ([]any, error)
}

func readSnapshotDB(ctx context.Context, db *sql.DB, expected Snapshot) (string, bool, tableBounds, error) { //nolint:funlen,gocognit,gocyclo // Keeping the exact table order together makes digest auditing direct.
	tables := streamingTables()
	expectedTables := logicalTables(expected)
	counts := make(tableBounds, len(tables))
	for _, table := range tables {
		count, err := preflightTable(ctx, db, table)
		if err != nil {
			return "", false, nil, err
		}
		counts[table.name] = count
	}
	edges, err := sourceEdgeCount(ctx, db)
	if err != nil {
		return "", false, nil, err
	}
	counts["source_edges"] = edges
	hash := sha256.New()
	if _, err := io.WriteString(hash, logicalDigestDomain); err != nil {
		return "", false, nil, fmt.Errorf("write logical digest domain: %w", err)
	}
	divergent := false
	for tableIndex, table := range tables {
		if err := writeTableHeader(hash, table.name, counts[table.name]); err != nil {
			return "", false, nil, err
		}
		rowIndex := 0
		err := scanRows(ctx, db, table.query, func(rows *sql.Rows) error {
			actual, err := table.scan(rows)
			if err != nil {
				return err
			}
			afterValidatedIndexRow()
			if err := ctx.Err(); err != nil {
				return err
			}
			if rowIndex >= expectedTables[tableIndex].count || !sameProjectedRow(table.name, actual, expectedTables[tableIndex].row(rowIndex)) {
				divergent = true
			}
			encoded, err := canonical.MarshalContext(ctx, digestRow(table.name, actual))
			if err != nil {
				return fmt.Errorf("canonicalize logical row %s[%d]: %w", table.name, rowIndex, err)
			}
			if err := writeUint64(hash, uint64(len(encoded))); err != nil {
				return err
			}
			if err := writeAll(hash, encoded); err != nil {
				return err
			}
			rowIndex++
			return nil
		})
		if err != nil {
			return "", false, nil, err
		}
		if rowIndex != expectedTables[tableIndex].count {
			divergent = true
		}
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), divergent, counts, nil
}

func streamingTables() []streamTable { //nolint:funlen // SQL and typed scanners stay adjacent so digest column order remains auditable.
	textLimit := func(columns string) string { return "SELECT coalesce(max(" + columns + "),0) FROM %s" }
	return []streamTable{
		{name: "index_meta", query: "SELECT key,value FROM index_meta ORDER BY key", textLengthQuery: fmt.Sprintf(textLimit("length(key)+length(value)"), "index_meta"), maximumRows: uint64(len(metadataKeys)), scan: func(rows *sql.Rows) ([]any, error) {
			var key, value string
			if err := rows.Scan(&key, &value); err != nil {
				return nil, err
			}
			return []any{key, value}, nil
		}},
		{name: "objects", query: "SELECT object_id,object_type,namespace,body_digest,actor_key_id,actor_label,observed_at,integrity_state,structure_state,authenticity_state,completeness_state FROM objects ORDER BY object_id", textLengthQuery: fmt.Sprintf(textLimit("length(object_id)+length(object_type)+length(namespace)+length(body_digest)+length(actor_key_id)+length(actor_label)+length(observed_at)+length(integrity_state)+length(structure_state)+length(authenticity_state)+length(completeness_state)"), "objects"), maximumRows: ledger.Phase2Limits.Objects, scan: scanObjectRow},
		{name: "commits", query: "SELECT commit_id,event_count FROM commits ORDER BY commit_id", textLengthQuery: fmt.Sprintf(textLimit("length(commit_id)"), "commits"), maximumRows: ledger.Phase2Limits.Objects, scan: func(rows *sql.Rows) ([]any, error) {
			var id string
			var count uint64
			if err := rows.Scan(&id, &count); err != nil {
				return nil, err
			}
			return []any{id, count}, nil
		}},
		{name: "parent_edges", query: "SELECT child_id,parent_id,resolved FROM parent_edges ORDER BY child_id,parent_id", textLengthQuery: fmt.Sprintf(textLimit("length(child_id)+length(parent_id)"), "parent_edges"), maximumRows: ledger.Phase2Limits.GraphEdges, scan: scanParentEdgeRow},
		{name: "events", query: "SELECT event_ref,commit_id,local_id,kind,event_type,subject,schema_ref,causal_batch,causal_status FROM events ORDER BY event_ref", textLengthQuery: fmt.Sprintf(textLimit("length(event_ref)+length(commit_id)+length(local_id)+length(kind)+length(event_type)+length(subject)+length(schema_ref)+length(causal_status)"), "events"), maximumRows: ledger.Phase2Limits.Events, scan: scanEventRow},
		{name: "event_tags", query: "SELECT event_ref,tag FROM event_tags ORDER BY event_ref,tag", textLengthQuery: fmt.Sprintf(textLimit("length(event_ref)+length(tag)"), "event_tags"), maximumRows: ledger.Phase2Limits.CanonicalBytes, scan: scanTwoStrings},
		{name: "event_links", query: "SELECT source_ref,relation,target_ref,resolved FROM event_links ORDER BY source_ref,relation,target_ref", textLengthQuery: fmt.Sprintf(textLimit("length(source_ref)+length(relation)+length(target_ref)"), "event_links"), maximumRows: ledger.Phase2Limits.GraphEdges, scan: scanThreeStringsAndInteger},
		{name: "checkpoints", query: "SELECT checkpoint_id,scope,policy_ref,authority_epoch,previous_checkpoint FROM checkpoints ORDER BY checkpoint_id", textLengthQuery: fmt.Sprintf(textLimit("length(checkpoint_id)+length(scope)+length(policy_ref)+coalesce(length(previous_checkpoint),0)"), "checkpoints"), maximumRows: ledger.Phase2Limits.Objects, scan: scanCheckpointRow},
		{name: "checkpoint_schema_refs", query: "SELECT checkpoint_id,schema_ref FROM checkpoint_schema_refs ORDER BY checkpoint_id,schema_ref", textLengthQuery: fmt.Sprintf(textLimit("length(checkpoint_id)+length(schema_ref)"), "checkpoint_schema_refs"), maximumRows: ledger.Phase2Limits.CanonicalBytes, scan: scanTwoStrings},
		{name: "checkpoint_frontier", query: "SELECT checkpoint_id,namespace,head_id,resolved FROM checkpoint_frontier ORDER BY checkpoint_id,namespace,head_id", textLengthQuery: fmt.Sprintf(textLimit("length(checkpoint_id)+length(namespace)+length(head_id)"), "checkpoint_frontier"), maximumRows: ledger.Phase2Limits.GraphEdges, scan: scanThreeStringsAndInteger},
		{name: "heads", query: "SELECT namespace,commit_id FROM heads ORDER BY namespace,commit_id", textLengthQuery: fmt.Sprintf(textLimit("length(namespace)+length(commit_id)"), "heads"), maximumRows: ledger.Phase2Limits.Objects, scan: scanTwoStrings},
		{name: "completeness_blockers", query: "SELECT source_id,code,field,missing_ref FROM completeness_blockers ORDER BY source_id,code,field,missing_ref", textLengthQuery: fmt.Sprintf(textLimit("length(source_id)+length(code)+length(field)+length(missing_ref)"), "completeness_blockers"), maximumRows: ledger.Phase2Limits.GraphEdges, scan: scanFourStrings},
	}
}

func preflightTable(ctx context.Context, db *sql.DB, table streamTable) (uint64, error) {
	if err := validateTextLengthQuery(ctx, db, table.textLengthQuery); err != nil {
		return 0, fmt.Errorf("validate %s text bounds: %w", table.name, err)
	}
	return queryRowCount(ctx, db, table.name, table.maximumRows)
}

func validateTextLengthQuery(ctx context.Context, db *sql.DB, statement string) error {
	var maximum int64
	if err := db.QueryRowContext(ctx, statement).Scan(&maximum); err != nil {
		return fmt.Errorf("read index text length: %w", err)
	}
	return validateTextLength(maximum)
}

func validateTextLength(maximum int64) error {
	if maximum < 0 || uint64(maximum) > ledger.Phase2Limits.ObjectBytes {
		return fmt.Errorf("index text exceeds resource limit")
	}
	return nil
}

func validateRowCount(count, maximum uint64) error {
	if count > maximum {
		return fmt.Errorf("index row count exceeds resource limit")
	}
	return nil
}

func validateRowCountQuery(ctx context.Context, db *sql.DB, table string, maximum uint64) error {
	_, err := queryRowCount(ctx, db, table, maximum)
	return err
}

func queryRowCount(ctx context.Context, db *sql.DB, table string, maximum uint64) (uint64, error) {
	var count uint64
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil { //nolint:gosec // table is selected only from fixed schema names.
		return 0, fmt.Errorf("read index row count: %w", err)
	}
	if err := validateRowCount(count, maximum); err != nil {
		return 0, err
	}
	return count, nil
}

func sourceEdgeCount(ctx context.Context, db *sql.DB) (uint64, error) {
	const statement = `SELECT
		(SELECT count(*) FROM parent_edges)+(SELECT count(*) FROM event_links)+(SELECT count(*) FROM checkpoint_frontier)+
		2*(SELECT count(*) FROM events)+(SELECT count(*) FROM checkpoints WHERE previous_checkpoint IS NOT NULL)`
	var count uint64
	if err := db.QueryRowContext(ctx, statement).Scan(&count); err != nil {
		return 0, fmt.Errorf("read source edge count: %w", err)
	}
	if err := validateRowCount(count, ledger.Phase2Limits.GraphEdges); err != nil {
		return 0, err
	}
	return count, nil
}

func scanObjectRow(rows *sql.Rows) ([]any, error) {
	values := make([]string, 11)
	if err := rows.Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6], &values[7], &values[8], &values[9], &values[10]); err != nil {
		return nil, err
	}
	return []any{values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7], values[8], values[9], values[10]}, nil
}

func scanParentEdgeRow(rows *sql.Rows) ([]any, error) {
	var child, parent string
	var resolved int64
	if err := rows.Scan(&child, &parent, &resolved); err != nil {
		return nil, err
	}
	return []any{child, parent, resolved}, nil
}

func scanEventRow(rows *sql.Rows) ([]any, error) {
	var eventRef, commitID, localID, kind, eventType, subject, schemaRef, causalStatus string
	var batch sql.NullInt64
	if err := rows.Scan(&eventRef, &commitID, &localID, &kind, &eventType, &subject, &schemaRef, &batch, &causalStatus); err != nil {
		return nil, err
	}
	var causalBatch any
	if batch.Valid {
		if batch.Int64 < 0 {
			return nil, fmt.Errorf("negative causal batch")
		}
		causalBatch = uint64(batch.Int64)
	}
	return []any{eventRef, commitID, localID, kind, eventType, subject, schemaRef, causalBatch, causalStatus}, nil
}

func scanTwoStrings(rows *sql.Rows) ([]any, error) {
	var first, second string
	if err := rows.Scan(&first, &second); err != nil {
		return nil, err
	}
	return []any{first, second}, nil
}

func scanThreeStringsAndInteger(rows *sql.Rows) ([]any, error) {
	var first, second, third string
	var value int64
	if err := rows.Scan(&first, &second, &third, &value); err != nil {
		return nil, err
	}
	return []any{first, second, third, value}, nil
}

func scanCheckpointRow(rows *sql.Rows) ([]any, error) {
	var checkpointID, scope, policyRef, authorityEpoch string
	var previous sql.NullString
	if err := rows.Scan(&checkpointID, &scope, &policyRef, &authorityEpoch, &previous); err != nil {
		return nil, err
	}
	var previousValue any
	if previous.Valid {
		previousValue = previous.String
	}
	return []any{checkpointID, scope, policyRef, authorityEpoch, previousValue}, nil
}

func scanFourStrings(rows *sql.Rows) ([]any, error) {
	var first, second, third, fourth string
	if err := rows.Scan(&first, &second, &third, &fourth); err != nil {
		return nil, err
	}
	return []any{first, second, third, fourth}, nil
}

func digestRow(table string, row []any) []any {
	if table == "index_meta" && len(row) == 2 && row[0] == "logical_digest" {
		return []any{row[0], ""}
	}
	return row
}

func sameProjectedRow(table string, actual, expected []any) bool {
	if table == "index_meta" && len(actual) == 2 && len(expected) == 2 && actual[0] == expected[0] {
		if actual[0] == "source_fingerprint" || actual[0] == "logical_digest" {
			return true
		}
	}
	return reflect.DeepEqual(actual, expected)
}

func scanRows(ctx context.Context, db *sql.DB, statement string, scan func(*sql.Rows) error) (err error) {
	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		return fmt.Errorf("read index rows: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return fmt.Errorf("read typed index row: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read index rows: %w", err)
	}
	return nil
}

func validStoredCounts(meta map[string]string, bounds tableBounds) bool {
	counts := map[string]uint64{
		"row_count_index_meta": bounds["index_meta"], "row_count_objects": bounds["objects"],
		"row_count_commits": bounds["commits"], "row_count_parent_edges": bounds["parent_edges"],
		"row_count_events": bounds["events"], "row_count_event_tags": bounds["event_tags"],
		"row_count_event_links": bounds["event_links"], "row_count_checkpoints": bounds["checkpoints"],
		"row_count_checkpoint_schema_refs": bounds["checkpoint_schema_refs"], "row_count_checkpoint_frontier": bounds["checkpoint_frontier"],
		"row_count_heads": bounds["heads"], "row_count_completeness_blockers": bounds["completeness_blockers"],
		"source_count_objects": bounds["objects"], "source_count_commits": bounds["commits"],
		"source_count_checkpoints": bounds["checkpoints"], "source_count_events": bounds["events"],
		"source_count_edges": bounds["source_edges"],
	}
	for key, want := range counts {
		got, err := strconv.ParseUint(meta[key], 10, 64)
		if err != nil || got != want {
			return false
		}
	}
	canonicalBytes, err := strconv.ParseUint(meta["source_count_canonical_bytes"], 10, 64)
	return err == nil && canonicalBytes <= ledger.Phase2Limits.CanonicalBytes
}

func representativeReads(ctx context.Context, db *sql.DB) error {
	queries := []string{
		"SELECT object_id FROM objects ORDER BY object_id LIMIT 1",
		"SELECT event_ref FROM events WHERE causal_status='ordered' ORDER BY causal_batch,event_ref LIMIT 1",
		"SELECT source_id FROM completeness_blockers ORDER BY source_id,code,field,missing_ref LIMIT 1",
	}
	for _, query := range queries {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("representative index read failed")
		}
		for rows.Next() {
			afterRepresentativeIndexRow()
			if err := ctx.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			var value string
			if err := rows.Scan(&value); err != nil {
				if closeErr := rows.Close(); closeErr != nil {
					return fmt.Errorf("representative index read and close failed")
				}
				return fmt.Errorf("representative index read failed")
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("representative index read failed: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("representative index read failed")
		}
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[7:] {
		if !isLowerHex(character) {
			return false
		}
	}
	return true
}

func isLowerHex(character rune) bool {
	return character >= '0' && character <= '9' || character >= 'a' && character <= 'f'
}
