// ABOUTME: Projects verified immutable ledger scans into typed, primary-key-sorted index rows.
// ABOUTME: Excludes canonical payloads, evidence, signing material, paths, trust, and private data.
package index

import (
	"sort"
	"strconv"

	"pact/internal/ledger"
)

const limitsContract = "pact/resource-limits/phase2-v1"

type IndexMetaRow struct{ Key, Value string }

type ObjectRow struct {
	ObjectID, ObjectType, Namespace, BodyDigest, ActorKeyID, ActorLabel, ObservedAt string
	IntegrityState, StructureState, AuthenticityState, CompletenessState            string
}

type CommitRow struct {
	CommitID   string
	EventCount uint64
}

type ParentEdgeRow struct {
	ChildID, ParentID string
	Resolved          int64
}

type EventRow struct {
	EventRef, CommitID, LocalID, Kind, EventType, Subject, SchemaRef string
	CausalBatch                                                      *uint64
	CausalStatus                                                     string
}

type EventTagRow struct{ EventRef, Tag string }

type EventLinkRow struct {
	SourceRef, Relation, TargetRef string
	Resolved                       int64
}

type CheckpointRow struct {
	CheckpointID, Scope, PolicyRef, AuthorityEpoch string
	PreviousCheckpoint                             *string
}

type CheckpointSchemaRefRow struct{ CheckpointID, SchemaRef string }

type CheckpointFrontierRow struct {
	CheckpointID, Namespace, HeadID string
	Resolved                        int64
}

type HeadRow struct{ Namespace, CommitID string }

type CompletenessBlockerRow struct{ SourceID, Code, Field, MissingRef string }

// Snapshot is the complete logical row set for one disposable index build.
type Snapshot struct {
	IndexMeta            []IndexMetaRow
	Objects              []ObjectRow
	Commits              []CommitRow
	ParentEdges          []ParentEdgeRow
	Events               []EventRow
	EventTags            []EventTagRow
	EventLinks           []EventLinkRow
	Checkpoints          []CheckpointRow
	CheckpointSchemaRefs []CheckpointSchemaRefRow
	CheckpointFrontier   []CheckpointFrontierRow
	Heads                []HeadRow
	CompletenessBlockers []CompletenessBlockerRow
}

// Project converts one published immutable ledger scan into normalized index rows.
func Project(scan ledger.ScanResult) Snapshot {
	snapshot := Snapshot{}
	projectCommits(scan, &snapshot)
	projectCheckpoints(scan, &snapshot)
	projectEvents(scan, &snapshot)
	projectHeads(scan, &snapshot)
	projectBlockers(scan, &snapshot)
	sortSnapshot(&snapshot)
	snapshot.IndexMeta = metadataRows(scan, snapshot)
	digest, err := LogicalDigest(snapshot)
	if err != nil {
		panic("index projection produced invalid logical rows: " + err.Error())
	}
	for index := range snapshot.IndexMeta {
		if snapshot.IndexMeta[index].Key == "logical_digest" {
			snapshot.IndexMeta[index].Value = digest
			break
		}
	}
	return snapshot
}

func projectCommits(scan ledger.ScanResult, snapshot *Snapshot) {
	for _, commit := range scan.Commits {
		snapshot.Objects = append(snapshot.Objects, ObjectRow{
			ObjectID: commit.ID, ObjectType: "commit", Namespace: commit.Namespace, BodyDigest: commit.BodyDigest,
			ActorKeyID: commit.ActorID, ActorLabel: commit.ActorLabel, ObservedAt: commit.ObservedAt,
			IntegrityState: commit.Integrity, StructureState: commit.Structure, AuthenticityState: commit.Authenticity, CompletenessState: commit.Completeness,
		})
		snapshot.Commits = append(snapshot.Commits, CommitRow{CommitID: commit.ID, EventCount: uint64(len(commit.EventRefs))})
		for _, parent := range commit.Parents {
			snapshot.ParentEdges = append(snapshot.ParentEdges, ParentEdgeRow{ChildID: commit.ID, ParentID: parent, Resolved: resolved(scan.Commits, parent)})
		}
	}
}

func projectCheckpoints(scan ledger.ScanResult, snapshot *Snapshot) {
	for _, checkpoint := range scan.Checkpoints {
		snapshot.Objects = append(snapshot.Objects, ObjectRow{
			ObjectID: checkpoint.ID, ObjectType: "checkpoint", Namespace: checkpoint.Scope, BodyDigest: checkpoint.BodyDigest,
			ActorKeyID: checkpoint.ActorID, ActorLabel: checkpoint.ActorLabel, ObservedAt: checkpoint.ObservedAt,
			IntegrityState: checkpoint.Integrity, StructureState: checkpoint.Structure, AuthenticityState: checkpoint.Authenticity, CompletenessState: checkpoint.Completeness,
		})
		row := CheckpointRow{CheckpointID: checkpoint.ID, Scope: checkpoint.Scope, PolicyRef: checkpoint.PolicyRef, AuthorityEpoch: checkpoint.AuthorityEpoch}
		if checkpoint.PreviousCheckpoint != "" {
			previous := checkpoint.PreviousCheckpoint
			row.PreviousCheckpoint = &previous
		}
		snapshot.Checkpoints = append(snapshot.Checkpoints, row)
		for _, schemaRef := range checkpoint.SchemaRefs {
			snapshot.CheckpointSchemaRefs = append(snapshot.CheckpointSchemaRefs, CheckpointSchemaRefRow{CheckpointID: checkpoint.ID, SchemaRef: schemaRef})
		}
		for _, frontier := range checkpoint.Frontier {
			for _, head := range frontier.Heads {
				snapshot.CheckpointFrontier = append(snapshot.CheckpointFrontier, CheckpointFrontierRow{
					CheckpointID: checkpoint.ID, Namespace: frontier.Namespace, HeadID: head, Resolved: resolved(scan.Commits, head),
				})
			}
		}
	}
}

func projectEvents(scan ledger.ScanResult, snapshot *Snapshot) {
	for _, event := range scan.Events {
		row := EventRow{
			EventRef: event.Ref, CommitID: event.CommitID, LocalID: event.LocalID, Kind: event.Kind,
			EventType: event.Type, Subject: event.Subject, SchemaRef: event.SchemaRef, CausalStatus: "unresolved",
		}
		if batch, found := scan.CausalBatches[event.Ref]; found {
			batchCopy := batch
			row.CausalBatch = &batchCopy
			row.CausalStatus = "ordered"
		}
		snapshot.Events = append(snapshot.Events, row)
		for _, tag := range event.Tags {
			snapshot.EventTags = append(snapshot.EventTags, EventTagRow{EventRef: event.Ref, Tag: tag})
		}
		for _, target := range event.CausedBy {
			snapshot.EventLinks = append(snapshot.EventLinks, EventLinkRow{SourceRef: event.Ref, Relation: "caused_by", TargetRef: target, Resolved: resolved(scan.Events, target)})
		}
		for _, target := range event.Supersedes {
			snapshot.EventLinks = append(snapshot.EventLinks, EventLinkRow{SourceRef: event.Ref, Relation: "supersedes", TargetRef: target, Resolved: resolved(scan.Events, target)})
		}
	}
}

func projectHeads(scan ledger.ScanResult, snapshot *Snapshot) {
	for namespace, commits := range scan.Heads {
		for _, commit := range commits {
			snapshot.Heads = append(snapshot.Heads, HeadRow{Namespace: namespace, CommitID: commit})
		}
	}
}

func projectBlockers(scan ledger.ScanResult, snapshot *Snapshot) {
	for _, blocker := range scan.Completeness.Blockers {
		snapshot.CompletenessBlockers = append(snapshot.CompletenessBlockers, CompletenessBlockerRow{
			SourceID: blocker.SourceID, Code: blocker.Code, Field: blocker.Field, MissingRef: blocker.MissingRef,
		})
	}
}

func resolved[Value any](values map[string]Value, key string) int64 {
	if _, found := values[key]; found {
		return 1
	}
	return 0
}

func sortSnapshot(snapshot *Snapshot) {
	sort.Slice(snapshot.Objects, func(left, right int) bool { return snapshot.Objects[left].ObjectID < snapshot.Objects[right].ObjectID })
	sort.Slice(snapshot.Commits, func(left, right int) bool { return snapshot.Commits[left].CommitID < snapshot.Commits[right].CommitID })
	sort.Slice(snapshot.ParentEdges, func(left, right int) bool {
		leftRow, rightRow := snapshot.ParentEdges[left], snapshot.ParentEdges[right]
		return leftRow.ChildID < rightRow.ChildID || leftRow.ChildID == rightRow.ChildID && leftRow.ParentID < rightRow.ParentID
	})
	sort.Slice(snapshot.Events, func(left, right int) bool { return snapshot.Events[left].EventRef < snapshot.Events[right].EventRef })
	sort.Slice(snapshot.EventTags, func(left, right int) bool {
		leftRow, rightRow := snapshot.EventTags[left], snapshot.EventTags[right]
		return leftRow.EventRef < rightRow.EventRef || leftRow.EventRef == rightRow.EventRef && leftRow.Tag < rightRow.Tag
	})
	sort.Slice(snapshot.EventLinks, func(left, right int) bool {
		leftRow, rightRow := snapshot.EventLinks[left], snapshot.EventLinks[right]
		if leftRow.SourceRef != rightRow.SourceRef {
			return leftRow.SourceRef < rightRow.SourceRef
		}
		if leftRow.Relation != rightRow.Relation {
			return leftRow.Relation < rightRow.Relation
		}
		return leftRow.TargetRef < rightRow.TargetRef
	})
	sort.Slice(snapshot.Checkpoints, func(left, right int) bool {
		return snapshot.Checkpoints[left].CheckpointID < snapshot.Checkpoints[right].CheckpointID
	})
	sort.Slice(snapshot.CheckpointSchemaRefs, func(left, right int) bool {
		leftRow, rightRow := snapshot.CheckpointSchemaRefs[left], snapshot.CheckpointSchemaRefs[right]
		return leftRow.CheckpointID < rightRow.CheckpointID || leftRow.CheckpointID == rightRow.CheckpointID && leftRow.SchemaRef < rightRow.SchemaRef
	})
	sort.Slice(snapshot.CheckpointFrontier, func(left, right int) bool {
		leftRow, rightRow := snapshot.CheckpointFrontier[left], snapshot.CheckpointFrontier[right]
		if leftRow.CheckpointID != rightRow.CheckpointID {
			return leftRow.CheckpointID < rightRow.CheckpointID
		}
		if leftRow.Namespace != rightRow.Namespace {
			return leftRow.Namespace < rightRow.Namespace
		}
		return leftRow.HeadID < rightRow.HeadID
	})
	sort.Slice(snapshot.Heads, func(left, right int) bool {
		leftRow, rightRow := snapshot.Heads[left], snapshot.Heads[right]
		return leftRow.Namespace < rightRow.Namespace || leftRow.Namespace == rightRow.Namespace && leftRow.CommitID < rightRow.CommitID
	})
	sort.Slice(snapshot.CompletenessBlockers, func(left, right int) bool {
		leftRow, rightRow := snapshot.CompletenessBlockers[left], snapshot.CompletenessBlockers[right]
		if leftRow.SourceID != rightRow.SourceID {
			return leftRow.SourceID < rightRow.SourceID
		}
		if leftRow.Code != rightRow.Code {
			return leftRow.Code < rightRow.Code
		}
		if leftRow.Field != rightRow.Field {
			return leftRow.Field < rightRow.Field
		}
		return leftRow.MissingRef < rightRow.MissingRef
	})
}

func metadataRows(scan ledger.ScanResult, snapshot Snapshot) []IndexMetaRow {
	values := map[string]string{
		"format": IndexFormat, "schema_version": strconv.Itoa(SchemaVersion), "schema_digest": SchemaDigest(),
		"source_fingerprint": scan.SourceFingerprint, "logical_digest": "", "limits_contract": limitsContract,
		"source_count_objects":         strconv.FormatUint(scan.Counts.Objects, 10),
		"source_count_commits":         strconv.FormatUint(scan.Counts.Commits, 10),
		"source_count_checkpoints":     strconv.FormatUint(scan.Counts.Checkpoints, 10),
		"source_count_events":          strconv.FormatUint(scan.Counts.Events, 10),
		"source_count_edges":           strconv.FormatUint(scan.Counts.Edges, 10),
		"source_count_canonical_bytes": strconv.FormatUint(scan.Counts.CanonicalBytes, 10),
		"row_count_index_meta":         "25", "row_count_objects": strconv.Itoa(len(snapshot.Objects)),
		"row_count_commits": strconv.Itoa(len(snapshot.Commits)), "row_count_parent_edges": strconv.Itoa(len(snapshot.ParentEdges)),
		"row_count_events": strconv.Itoa(len(snapshot.Events)), "row_count_event_tags": strconv.Itoa(len(snapshot.EventTags)),
		"row_count_event_links": strconv.Itoa(len(snapshot.EventLinks)), "row_count_checkpoints": strconv.Itoa(len(snapshot.Checkpoints)),
		"row_count_checkpoint_schema_refs": strconv.Itoa(len(snapshot.CheckpointSchemaRefs)),
		"row_count_checkpoint_frontier":    strconv.Itoa(len(snapshot.CheckpointFrontier)), "row_count_heads": strconv.Itoa(len(snapshot.Heads)),
		"row_count_completeness_blockers": strconv.Itoa(len(snapshot.CompletenessBlockers)), "local_completeness": scan.Completeness.Status,
	}
	rows := make([]IndexMetaRow, 0, len(values))
	for key, value := range values {
		rows = append(rows, IndexMetaRow{Key: key, Value: value})
	}
	sort.Slice(rows, func(left, right int) bool { return rows[left].Key < rows[right].Key })
	return rows
}
