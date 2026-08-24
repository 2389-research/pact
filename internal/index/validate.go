// ABOUTME: Validates disposable SQLite indexes through one fixed read-only path.
// ABOUTME: Classifies format, physical, logical, source, and projection failures without repair.
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
	"reflect"
	"strconv"
	"strings"

	"pact/internal/ledger"
)

const (
	readerOptions         = "mode=ro&_query_only=1&_defensive=1&_foreign_keys=1&_busy_timeout=5000"
	maximumSQLiteFileSize = int64(2_147_483_648)
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
	snapshot Snapshot
	meta     map[string]string
	version  int
}

func validateIndex(ctx context.Context, path string, scan ledger.ScanResult) IndexInfo { //nolint:gocyclo // The linear branches preserve the contract's state precedence.
	result := IndexInfo{State: "corrupt", Coverage: coverageNone, Path: new(path), RebuildRequired: true}
	directoryInfo, directoryErr := os.Lstat(filepath.Dir(path))
	if directoryErr != nil || directoryInfo.Mode()&fs.ModeSymlink != 0 || !directoryInfo.IsDir() {
		return result
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return IndexInfo{State: "missing", Coverage: coverageNone, RebuildRequired: true}
	}
	if err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximumSQLiteFileSize {
		return result
	}
	db, err := openIndexReader(path)
	if err != nil {
		return result
	}
	inspection, state := inspectDatabase(ctx, db)
	if closeErr := db.Close(); closeErr != nil && state == "current" {
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
		return result
	}
	// Project has a fixed in-memory API; the bounded scan above already honored ctx.
	expected := Project(scan) //nolint:contextcheck
	if inspection.meta["source_fingerprint"] != scan.SourceFingerprint {
		result.State = "stale"
		return result
	}
	if !reflect.DeepEqual(inspection.snapshot, expected) {
		result.State = "partial-build"
		return result
	}
	result.State = "current"
	result.RebuildRequired = false
	if scan.Completeness.Status == "locally_closed" {
		result.Coverage = "complete"
	} else {
		result.Coverage = "partial"
	}
	return result
}

func inspectDatabase(ctx context.Context, db *sql.DB) (databaseInspection, string) { //nolint:gocyclo // Each gate maps to one required classification boundary.
	inspection := databaseInspection{meta: map[string]string{}}
	if err := db.PingContext(ctx); err != nil {
		return inspection, "corrupt"
	}
	applicationID, err := readPragmaInteger(ctx, db, "application_id")
	if err != nil {
		return inspection, "corrupt"
	}
	version, err := readPragmaInteger(ctx, db, "user_version")
	if err != nil {
		return inspection, "corrupt"
	}
	inspection.version = version
	if applicationID != ApplicationID || version != SchemaVersion {
		return inspection, "incompatible"
	}
	if state := validateSchema(ctx, db); state != "current" {
		return inspection, state
	}
	meta, err := readMetadata(ctx, db)
	if err != nil {
		return inspection, "corrupt"
	}
	inspection.meta = meta
	if meta["format"] != IndexFormat || meta["schema_version"] != strconv.Itoa(SchemaVersion) || meta["schema_digest"] != SchemaDigest() {
		return inspection, "incompatible"
	}
	if meta["limits_contract"] != limitsContract || meta["local_completeness"] != "locally_closed" && meta["local_completeness"] != "incomplete" {
		return inspection, "corrupt"
	}
	if !validDigest(meta["source_fingerprint"]) || !validDigest(meta["logical_digest"]) {
		return inspection, "corrupt"
	}
	if err := validatePragmas(ctx, db); err != nil {
		return inspection, "corrupt"
	}
	snapshot, err := readSnapshotDB(ctx, db)
	if err != nil {
		return inspection, "corrupt"
	}
	inspection.snapshot = snapshot
	if !validStoredCounts(meta, snapshot) {
		return inspection, "corrupt"
	}
	// LogicalDigest has a fixed in-memory API over rows already read with ctx.
	digest, err := LogicalDigest(snapshot) //nolint:contextcheck
	if err != nil || digest != meta["logical_digest"] {
		return inspection, "corrupt"
	}
	if err := representativeReads(ctx, db); err != nil {
		return inspection, "corrupt"
	}
	return inspection, "current"
}

func openIndexReader(path string) (*sql.DB, error) {
	location := (&url.URL{Scheme: "file", Path: path}).String() + "?" + readerOptions
	db, err := sql.Open("sqlite", location)
	if err != nil {
		return nil, fmt.Errorf("open index reader")
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

func readPragmaInteger(ctx context.Context, db *sql.DB, name string) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, "PRAGMA "+name).Scan(&value); err != nil {
		return 0, fmt.Errorf("read index identity")
	}
	return value, nil
}

type schemaObject struct{ kind, sql string }

func validateSchema(ctx context.Context, db *sql.DB) string {
	want, err := schemaObjectMap(ctx, nil)
	if err != nil {
		return "corrupt"
	}
	got, err := schemaObjectMap(ctx, db)
	if err != nil {
		return "corrupt"
	}
	for name, object := range got {
		if _, found := want[name]; !found {
			_ = object
			return "corrupt"
		}
	}
	if !reflect.DeepEqual(got, want) {
		return "incompatible"
	}
	return "current"
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

func readSnapshotDB(ctx context.Context, db *sql.DB) (Snapshot, error) { //nolint:funlen,gocognit,gocyclo // Keeping the exact table order together makes digest auditing direct.
	var snapshot Snapshot
	if err := scanRows(ctx, db, "SELECT key,value FROM index_meta ORDER BY key", func(rows *sql.Rows) error {
		var row IndexMetaRow
		if err := rows.Scan(&row.Key, &row.Value); err != nil {
			return err
		}
		snapshot.IndexMeta = append(snapshot.IndexMeta, row)
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	if err := scanRows(ctx, db, "SELECT object_id,object_type,namespace,body_digest,actor_key_id,actor_label,observed_at,integrity_state,structure_state,authenticity_state,completeness_state FROM objects ORDER BY object_id", func(rows *sql.Rows) error {
		var row ObjectRow
		if err := rows.Scan(&row.ObjectID, &row.ObjectType, &row.Namespace, &row.BodyDigest, &row.ActorKeyID, &row.ActorLabel, &row.ObservedAt, &row.IntegrityState, &row.StructureState, &row.AuthenticityState, &row.CompletenessState); err != nil {
			return err
		}
		snapshot.Objects = append(snapshot.Objects, row)
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	if err := scanRows(ctx, db, "SELECT commit_id,event_count FROM commits ORDER BY commit_id", func(rows *sql.Rows) error {
		var row CommitRow
		if err := rows.Scan(&row.CommitID, &row.EventCount); err != nil {
			return err
		}
		snapshot.Commits = append(snapshot.Commits, row)
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	if err := scanRows(ctx, db, "SELECT child_id,parent_id,resolved FROM parent_edges ORDER BY child_id,parent_id", func(rows *sql.Rows) error {
		var row ParentEdgeRow
		if err := rows.Scan(&row.ChildID, &row.ParentID, &row.Resolved); err != nil {
			return err
		}
		snapshot.ParentEdges = append(snapshot.ParentEdges, row)
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	if err := scanRows(ctx, db, "SELECT event_ref,commit_id,local_id,kind,event_type,subject,schema_ref,causal_batch,causal_status FROM events ORDER BY event_ref", func(rows *sql.Rows) error {
		var row EventRow
		var batch sql.NullInt64
		if err := rows.Scan(&row.EventRef, &row.CommitID, &row.LocalID, &row.Kind, &row.EventType, &row.Subject, &row.SchemaRef, &batch, &row.CausalStatus); err != nil {
			return err
		}
		if batch.Valid {
			if batch.Int64 < 0 {
				return fmt.Errorf("negative causal batch")
			}
			value := uint64(batch.Int64)
			row.CausalBatch = &value
		}
		snapshot.Events = append(snapshot.Events, row)
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	if err := scanRows(ctx, db, "SELECT event_ref,tag FROM event_tags ORDER BY event_ref,tag", func(rows *sql.Rows) error {
		var row EventTagRow
		if err := rows.Scan(&row.EventRef, &row.Tag); err != nil {
			return err
		}
		snapshot.EventTags = append(snapshot.EventTags, row)
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	if err := scanRows(ctx, db, "SELECT source_ref,relation,target_ref,resolved FROM event_links ORDER BY source_ref,relation,target_ref", func(rows *sql.Rows) error {
		var row EventLinkRow
		if err := rows.Scan(&row.SourceRef, &row.Relation, &row.TargetRef, &row.Resolved); err != nil {
			return err
		}
		snapshot.EventLinks = append(snapshot.EventLinks, row)
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	if err := scanRows(ctx, db, "SELECT checkpoint_id,scope,policy_ref,authority_epoch,previous_checkpoint FROM checkpoints ORDER BY checkpoint_id", func(rows *sql.Rows) error {
		var row CheckpointRow
		var previous sql.NullString
		if err := rows.Scan(&row.CheckpointID, &row.Scope, &row.PolicyRef, &row.AuthorityEpoch, &previous); err != nil {
			return err
		}
		if previous.Valid {
			row.PreviousCheckpoint = &previous.String
		}
		snapshot.Checkpoints = append(snapshot.Checkpoints, row)
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	if err := scanRows(ctx, db, "SELECT checkpoint_id,schema_ref FROM checkpoint_schema_refs ORDER BY checkpoint_id,schema_ref", func(rows *sql.Rows) error {
		var row CheckpointSchemaRefRow
		if err := rows.Scan(&row.CheckpointID, &row.SchemaRef); err != nil {
			return err
		}
		snapshot.CheckpointSchemaRefs = append(snapshot.CheckpointSchemaRefs, row)
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	if err := scanRows(ctx, db, "SELECT checkpoint_id,namespace,head_id,resolved FROM checkpoint_frontier ORDER BY checkpoint_id,namespace,head_id", func(rows *sql.Rows) error {
		var row CheckpointFrontierRow
		if err := rows.Scan(&row.CheckpointID, &row.Namespace, &row.HeadID, &row.Resolved); err != nil {
			return err
		}
		snapshot.CheckpointFrontier = append(snapshot.CheckpointFrontier, row)
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	if err := scanRows(ctx, db, "SELECT namespace,commit_id FROM heads ORDER BY namespace,commit_id", func(rows *sql.Rows) error {
		var row HeadRow
		if err := rows.Scan(&row.Namespace, &row.CommitID); err != nil {
			return err
		}
		snapshot.Heads = append(snapshot.Heads, row)
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	if err := scanRows(ctx, db, "SELECT source_id,code,field,missing_ref FROM completeness_blockers ORDER BY source_id,code,field,missing_ref", func(rows *sql.Rows) error {
		var row CompletenessBlockerRow
		if err := rows.Scan(&row.SourceID, &row.Code, &row.Field, &row.MissingRef); err != nil {
			return err
		}
		snapshot.CompletenessBlockers = append(snapshot.CompletenessBlockers, row)
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func scanRows(ctx context.Context, db *sql.DB, statement string, scan func(*sql.Rows) error) (err error) {
	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		return fmt.Errorf("read index rows")
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return fmt.Errorf("read typed index row")
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read index rows")
	}
	return nil
}

func validStoredCounts(meta map[string]string, snapshot Snapshot) bool {
	counts := map[string]uint64{
		"row_count_index_meta": uint64(len(snapshot.IndexMeta)), "row_count_objects": uint64(len(snapshot.Objects)),
		"row_count_commits": uint64(len(snapshot.Commits)), "row_count_parent_edges": uint64(len(snapshot.ParentEdges)),
		"row_count_events": uint64(len(snapshot.Events)), "row_count_event_tags": uint64(len(snapshot.EventTags)),
		"row_count_event_links": uint64(len(snapshot.EventLinks)), "row_count_checkpoints": uint64(len(snapshot.Checkpoints)),
		"row_count_checkpoint_schema_refs": uint64(len(snapshot.CheckpointSchemaRefs)), "row_count_checkpoint_frontier": uint64(len(snapshot.CheckpointFrontier)),
		"row_count_heads": uint64(len(snapshot.Heads)), "row_count_completeness_blockers": uint64(len(snapshot.CompletenessBlockers)),
		"source_count_objects": uint64(len(snapshot.Objects)), "source_count_commits": uint64(len(snapshot.Commits)),
		"source_count_checkpoints": uint64(len(snapshot.Checkpoints)), "source_count_events": uint64(len(snapshot.Events)),
	}
	edges := uint64(len(snapshot.ParentEdges) + len(snapshot.EventLinks) + len(snapshot.CheckpointFrontier))
	edges += 2 * uint64(len(snapshot.Events))
	for _, row := range snapshot.Checkpoints {
		if row.PreviousCheckpoint != nil {
			edges++
		}
	}
	counts["source_count_edges"] = edges
	for key, want := range counts {
		got, err := strconv.ParseUint(meta[key], 10, 64)
		if err != nil || got != want {
			return false
		}
	}
	_, err := strconv.ParseUint(meta["source_count_canonical_bytes"], 10, 64)
	return err == nil
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
			var value string
			if err := rows.Scan(&value); err != nil {
				if closeErr := rows.Close(); closeErr != nil {
					return fmt.Errorf("representative index read and close failed")
				}
				return fmt.Errorf("representative index read failed")
			}
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
