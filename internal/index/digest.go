// ABOUTME: Hashes normalized index rows with fixed table, count, and row framing.
// ABOUTME: Uses pact-json-v1 arrays in exact DDL column order for logical parity.
package index

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"pact/internal/canonical"
)

const logicalDigestDomain = "PACT-INDEX-LOGICAL-ROWS-V1\x00"

// LogicalDigest identifies every logical row while ignoring its own stored metadata value.
func LogicalDigest(snapshot Snapshot) (string, error) {
	hash := sha256.New()
	if _, err := io.WriteString(hash, logicalDigestDomain); err != nil {
		return "", fmt.Errorf("write logical digest domain: %w", err)
	}
	tables := logicalTables(snapshot)
	for _, table := range tables {
		if err := writeLogicalTable(hash, table); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}

type logicalTable struct {
	name  string
	count int
	row   func(int) []any
}

func logicalTables(snapshot Snapshot) []logicalTable {
	return []logicalTable{
		{name: "index_meta", count: len(snapshot.IndexMeta), row: func(index int) []any {
			row := snapshot.IndexMeta[index]
			value := row.Value
			if row.Key == "logical_digest" {
				value = ""
			}
			return []any{row.Key, value}
		}},
		{name: "objects", count: len(snapshot.Objects), row: func(index int) []any {
			row := snapshot.Objects[index]
			return []any{
				row.ObjectID, row.ObjectType, row.Namespace, row.BodyDigest, row.ActorKeyID, row.ActorLabel, row.ObservedAt,
				row.IntegrityState, row.StructureState, row.AuthenticityState, row.CompletenessState,
			}
		}},
		{name: "commits", count: len(snapshot.Commits), row: func(index int) []any {
			row := snapshot.Commits[index]
			return []any{row.CommitID, row.EventCount}
		}},
		{name: "parent_edges", count: len(snapshot.ParentEdges), row: func(index int) []any {
			row := snapshot.ParentEdges[index]
			return []any{row.ChildID, row.ParentID, row.Resolved}
		}},
		{name: "events", count: len(snapshot.Events), row: func(index int) []any {
			row := snapshot.Events[index]
			return []any{
				row.EventRef, row.CommitID, row.LocalID, row.Kind, row.EventType, row.Subject, row.SchemaRef,
				nullableUint64(row.CausalBatch), row.CausalStatus,
			}
		}},
		{name: "event_tags", count: len(snapshot.EventTags), row: func(index int) []any {
			row := snapshot.EventTags[index]
			return []any{row.EventRef, row.Tag}
		}},
		{name: "event_links", count: len(snapshot.EventLinks), row: func(index int) []any {
			row := snapshot.EventLinks[index]
			return []any{row.SourceRef, row.Relation, row.TargetRef, row.Resolved}
		}},
		{name: "checkpoints", count: len(snapshot.Checkpoints), row: func(index int) []any {
			row := snapshot.Checkpoints[index]
			return []any{row.CheckpointID, row.Scope, row.PolicyRef, row.AuthorityEpoch, nullableString(row.PreviousCheckpoint)}
		}},
		{name: "checkpoint_schema_refs", count: len(snapshot.CheckpointSchemaRefs), row: func(index int) []any {
			row := snapshot.CheckpointSchemaRefs[index]
			return []any{row.CheckpointID, row.SchemaRef}
		}},
		{name: "checkpoint_frontier", count: len(snapshot.CheckpointFrontier), row: func(index int) []any {
			row := snapshot.CheckpointFrontier[index]
			return []any{row.CheckpointID, row.Namespace, row.HeadID, row.Resolved}
		}},
		{name: "heads", count: len(snapshot.Heads), row: func(index int) []any {
			row := snapshot.Heads[index]
			return []any{row.Namespace, row.CommitID}
		}},
		{name: "completeness_blockers", count: len(snapshot.CompletenessBlockers), row: func(index int) []any {
			row := snapshot.CompletenessBlockers[index]
			return []any{row.SourceID, row.Code, row.Field, row.MissingRef}
		}},
	}
}

func nullableUint64(value *uint64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func writeLogicalTable(writer io.Writer, table logicalTable) error {
	// #nosec G115 -- table.count comes from len, so it is nonnegative and fits uint64.
	if err := writeTableHeader(writer, table.name, uint64(table.count)); err != nil {
		return err
	}
	for index := range table.count {
		encoded, err := canonical.Marshal(table.row(index))
		if err != nil {
			return fmt.Errorf("canonicalize logical row %s[%d]: %w", table.name, index, err)
		}
		if err := writeUint64(writer, uint64(len(encoded))); err != nil {
			return fmt.Errorf("frame logical row %s[%d]: %w", table.name, index, err)
		}
		if err := writeAll(writer, encoded); err != nil {
			return fmt.Errorf("write logical row %s[%d]: %w", table.name, index, err)
		}
	}
	return nil
}

func writeTableHeader(writer io.Writer, name string, rowCount uint64) error {
	if len(name) > int(^uint16(0)) {
		return fmt.Errorf("logical table name exceeds uint16 framing")
	}
	var length [2]byte
	// #nosec G115 -- the explicit bound above proves the table-name length fits uint16.
	binary.BigEndian.PutUint16(length[:], uint16(len(name)))
	if err := writeAll(writer, length[:]); err != nil {
		return fmt.Errorf("write logical table name length: %w", err)
	}
	if err := writeAll(writer, []byte(name)); err != nil {
		return fmt.Errorf("write logical table name: %w", err)
	}
	if err := writeUint64(writer, rowCount); err != nil {
		return fmt.Errorf("write logical table row count: %w", err)
	}
	return nil
}

func writeUint64(writer io.Writer, value uint64) error {
	var framed [8]byte
	binary.BigEndian.PutUint64(framed[:], value)
	return writeAll(writer, framed[:])
}

func writeAll(writer io.Writer, value []byte) error {
	written, err := writer.Write(value)
	if err != nil {
		return err
	}
	if written != len(value) {
		return io.ErrShortWrite
	}
	return nil
}
