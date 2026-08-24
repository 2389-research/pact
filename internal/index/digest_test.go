// ABOUTME: Tests deterministic logical-row digest framing and fixed vectors.
// ABOUTME: Covers self-hash substitution, value sensitivity, and framing errors.
package index

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"pact/internal/ledger"
)

func TestLogicalDigestFixedVectors(t *testing.T) {
	tests := []struct {
		name     string
		snapshot func() Snapshot
		want     string
	}{
		{
			name:     "empty scan",
			snapshot: func() Snapshot { return Project(emptyScanFixture(t)) },
			want:     "sha256:03f03e8a64cabe2cf76aad5d5f0ba934f63efc7aac6b03526922bc8207b3d481",
		},
		{
			name:     "signed partial replica",
			snapshot: func() Snapshot { return Project(signedPartialScanFixture(t).scan) },
			want:     "sha256:5996aa26d41a908d1a0a06993fa6b406cd6506ea7cfe9b3320dfd879b80b21a1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := test.snapshot()
			got, err := LogicalDigest(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("LogicalDigest() = %q, want %q", got, test.want)
			}
			if independent := independentlyDerivedDigest(t, snapshot); independent != test.want {
				t.Fatalf("independent logical digest = %q, want %q", independent, test.want)
			}
			if stored := metadataValue(snapshot, "logical_digest"); stored != got {
				t.Fatalf("stored logical digest = %q, computed %q", stored, got)
			}
		})
	}
}

func independentlyDerivedDigest(t *testing.T, snapshot Snapshot) string {
	t.Helper()
	tableNames := []string{
		"index_meta", "objects", "commits", "parent_edges", "events", "event_tags", "event_links",
		"checkpoints", "checkpoint_schema_refs", "checkpoint_frontier", "heads", "completeness_blockers",
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("PACT-INDEX-LOGICAL-ROWS-V1\x00"))
	snapshotValue := reflect.ValueOf(snapshot)
	for tableIndex, tableName := range tableNames {
		rows := snapshotValue.Field(tableIndex)
		var nameLength [2]byte
		binary.BigEndian.PutUint16(nameLength[:], uint16(len(tableName)))
		_, _ = hash.Write(nameLength[:])
		_, _ = hash.Write([]byte(tableName))
		var count [8]byte
		binary.BigEndian.PutUint64(count[:], uint64(rows.Len()))
		_, _ = hash.Write(count[:])
		for rowIndex := range rows.Len() {
			row := rows.Index(rowIndex)
			values := make([]any, row.NumField())
			for columnIndex := range row.NumField() {
				column := row.Field(columnIndex)
				if column.Kind() == reflect.Pointer {
					if !column.IsNil() {
						values[columnIndex] = column.Elem().Interface()
					}
				} else {
					values[columnIndex] = column.Interface()
				}
			}
			if tableName == "index_meta" && values[0] == "logical_digest" {
				values[1] = ""
			}
			encoded, err := json.Marshal(values)
			if err != nil {
				t.Fatal(err)
			}
			binary.BigEndian.PutUint64(count[:], uint64(len(encoded)))
			_, _ = hash.Write(count[:])
			_, _ = hash.Write(encoded)
		}
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil))
}

func TestLogicalDigestChangesWithOneLogicalValue(t *testing.T) {
	snapshot := Project(signedPartialScanFixture(t).scan)
	before, err := LogicalDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	changed := snapshot
	changed.Objects = append([]ObjectRow(nil), snapshot.Objects...)
	changed.Objects[0].ActorLabel += " changed"
	after, err := LogicalDigest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatalf("logical digest did not change after one row value changed: %q", before)
	}
}

func TestLogicalDigestRejectsInvalidCanonicalRowValue(t *testing.T) {
	snapshot := Project(ledger.ScanResult{})
	snapshot.IndexMeta = append([]IndexMetaRow(nil), snapshot.IndexMeta...)
	snapshot.IndexMeta[0].Value = string([]byte{0xff})
	if _, err := LogicalDigest(snapshot); err == nil {
		t.Fatal("LogicalDigest() error = nil, want invalid UTF-8 rejection")
	}
}

func TestLogicalDigestRejectsTableNameFramingOverflow(t *testing.T) {
	if err := writeTableHeader(io.Discard, strings.Repeat("x", 1<<16), 0); err == nil {
		t.Fatal("writeTableHeader() error = nil, want uint16 framing overflow")
	}
}
