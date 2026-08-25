// ABOUTME: Projects verified immutable ledger scans into typed, primary-key-sorted index rows.
// ABOUTME: Excludes canonical payloads, evidence, signing material, paths, trust, and private data.
package index

import (
	"context"
	"fmt"
	"strconv"

	"pact/internal/ledger"
)

const limitsContract = "pact/resource-limits/phase2-v1"

var beforeIndexProjection = func() {}

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
func Project(ctx context.Context, scan ledger.ScanResult) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, fmt.Errorf("context is required")
	}
	beforeIndexProjection()
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	capacities, err := countProjectionRows(ctx, scan)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, err := allocateSnapshot(ctx, capacities)
	if err != nil {
		return Snapshot{}, err
	}
	if err := projectCommits(ctx, scan, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := projectCheckpoints(ctx, scan, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := projectEvents(ctx, scan, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := projectHeads(ctx, scan, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := projectBlockers(ctx, scan, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := sortSnapshot(ctx, &snapshot); err != nil {
		return Snapshot{}, err
	}
	snapshot.IndexMeta, err = metadataRows(ctx, scan, snapshot)
	if err != nil {
		return Snapshot{}, err
	}
	digest, err := LogicalDigest(ctx, snapshot)
	if err != nil {
		return Snapshot{}, err
	}
	for index := range snapshot.IndexMeta {
		if err := pollIndexContext(ctx, index); err != nil {
			return Snapshot{}, err
		}
		if snapshot.IndexMeta[index].Key == "logical_digest" {
			snapshot.IndexMeta[index].Value = digest
			break
		}
	}
	return snapshot, nil
}

type projectionRowCounts struct {
	objects, commits, parentEdges, events, eventTags, eventLinks int
	checkpoints, checkpointSchemaRefs, checkpointFrontier        int
	heads, blockers                                              int
}

func countProjectionRows(ctx context.Context, scan ledger.ScanResult) (projectionRowCounts, error) { //nolint:gocognit // Each independent row family needs its own checked count.
	counts := projectionRowCounts{commits: len(scan.Commits), events: len(scan.Events), checkpoints: len(scan.Checkpoints), blockers: len(scan.Completeness.Blockers)}
	var err error
	if counts.objects, err = addProjectionCount(len(scan.Commits), len(scan.Checkpoints)); err != nil {
		return projectionRowCounts{}, err
	}
	work := 0
	for _, commit := range scan.Commits {
		if err := pollIndexContext(ctx, work); err != nil {
			return projectionRowCounts{}, err
		}
		work++
		if counts.parentEdges, err = addProjectionCount(counts.parentEdges, len(commit.Parents)); err != nil {
			return projectionRowCounts{}, err
		}
	}
	for _, checkpoint := range scan.Checkpoints {
		if err := pollIndexContext(ctx, work); err != nil {
			return projectionRowCounts{}, err
		}
		work++
		if counts.checkpointSchemaRefs, err = addProjectionCount(counts.checkpointSchemaRefs, len(checkpoint.SchemaRefs)); err != nil {
			return projectionRowCounts{}, err
		}
		for _, frontier := range checkpoint.Frontier {
			if err := pollIndexContext(ctx, work); err != nil {
				return projectionRowCounts{}, err
			}
			work++
			if counts.checkpointFrontier, err = addProjectionCount(counts.checkpointFrontier, len(frontier.Heads)); err != nil {
				return projectionRowCounts{}, err
			}
		}
	}
	for _, event := range scan.Events {
		if err := pollIndexContext(ctx, work); err != nil {
			return projectionRowCounts{}, err
		}
		work++
		if counts.eventTags, err = addProjectionCount(counts.eventTags, len(event.Tags)); err != nil {
			return projectionRowCounts{}, err
		}
		links, err := addProjectionCount(len(event.CausedBy), len(event.Supersedes))
		if err != nil {
			return projectionRowCounts{}, err
		}
		if counts.eventLinks, err = addProjectionCount(counts.eventLinks, links); err != nil {
			return projectionRowCounts{}, err
		}
	}
	for _, commits := range scan.Heads {
		if err := pollIndexContext(ctx, work); err != nil {
			return projectionRowCounts{}, err
		}
		work++
		if counts.heads, err = addProjectionCount(counts.heads, len(commits)); err != nil {
			return projectionRowCounts{}, err
		}
	}
	return counts, ctx.Err()
}

func addProjectionCount(left, right int) (int, error) {
	maximum := int(^uint(0) >> 1)
	if left < 0 || right < 0 || right > maximum-left {
		return 0, fmt.Errorf("index projection row count exceeds int")
	}
	return left + right, nil
}

func allocateSnapshot(ctx context.Context, counts projectionRowCounts) (Snapshot, error) {
	var result Snapshot
	var err error
	if result.Objects, err = allocateProjectionRows[ObjectRow](ctx, counts.objects); err != nil {
		return Snapshot{}, err
	}
	if result.Commits, err = allocateProjectionRows[CommitRow](ctx, counts.commits); err != nil {
		return Snapshot{}, err
	}
	if result.ParentEdges, err = allocateProjectionRows[ParentEdgeRow](ctx, counts.parentEdges); err != nil {
		return Snapshot{}, err
	}
	if result.Events, err = allocateProjectionRows[EventRow](ctx, counts.events); err != nil {
		return Snapshot{}, err
	}
	if result.EventTags, err = allocateProjectionRows[EventTagRow](ctx, counts.eventTags); err != nil {
		return Snapshot{}, err
	}
	if result.EventLinks, err = allocateProjectionRows[EventLinkRow](ctx, counts.eventLinks); err != nil {
		return Snapshot{}, err
	}
	if result.Checkpoints, err = allocateProjectionRows[CheckpointRow](ctx, counts.checkpoints); err != nil {
		return Snapshot{}, err
	}
	if result.CheckpointSchemaRefs, err = allocateProjectionRows[CheckpointSchemaRefRow](ctx, counts.checkpointSchemaRefs); err != nil {
		return Snapshot{}, err
	}
	if result.CheckpointFrontier, err = allocateProjectionRows[CheckpointFrontierRow](ctx, counts.checkpointFrontier); err != nil {
		return Snapshot{}, err
	}
	if result.Heads, err = allocateProjectionRows[HeadRow](ctx, counts.heads); err != nil {
		return Snapshot{}, err
	}
	if result.CompletenessBlockers, err = allocateProjectionRows[CompletenessBlockerRow](ctx, counts.blockers); err != nil {
		return Snapshot{}, err
	}
	return result, ctx.Err()
}

func allocateProjectionRows[Row any](ctx context.Context, capacity int) ([]Row, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return make([]Row, 0, capacity), nil
}

func projectCommits(ctx context.Context, scan ledger.ScanResult, snapshot *Snapshot) error {
	work := 0
	for _, commit := range scan.Commits {
		if err := pollIndexContext(ctx, work); err != nil {
			return err
		}
		work++
		snapshot.Objects = append(snapshot.Objects, ObjectRow{
			ObjectID: commit.ID, ObjectType: "commit", Namespace: commit.Namespace, BodyDigest: commit.BodyDigest,
			ActorKeyID: commit.ActorID, ActorLabel: commit.ActorLabel, ObservedAt: commit.ObservedAt,
			IntegrityState: commit.Integrity, StructureState: commit.Structure, AuthenticityState: commit.Authenticity, CompletenessState: commit.Completeness,
		})
		snapshot.Commits = append(snapshot.Commits, CommitRow{CommitID: commit.ID, EventCount: uint64(len(commit.EventRefs))})
		for _, parent := range commit.Parents {
			if err := pollIndexContext(ctx, work); err != nil {
				return err
			}
			work++
			snapshot.ParentEdges = append(snapshot.ParentEdges, ParentEdgeRow{ChildID: commit.ID, ParentID: parent, Resolved: resolved(scan.Commits, parent)})
		}
	}
	return ctx.Err()
}

func projectCheckpoints(ctx context.Context, scan ledger.ScanResult, snapshot *Snapshot) error {
	work := 0
	for _, checkpoint := range scan.Checkpoints {
		if err := pollIndexContext(ctx, work); err != nil {
			return err
		}
		work++
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
			if err := pollIndexContext(ctx, work); err != nil {
				return err
			}
			work++
			snapshot.CheckpointSchemaRefs = append(snapshot.CheckpointSchemaRefs, CheckpointSchemaRefRow{CheckpointID: checkpoint.ID, SchemaRef: schemaRef})
		}
		for _, frontier := range checkpoint.Frontier {
			for _, head := range frontier.Heads {
				if err := pollIndexContext(ctx, work); err != nil {
					return err
				}
				work++
				snapshot.CheckpointFrontier = append(snapshot.CheckpointFrontier, CheckpointFrontierRow{
					CheckpointID: checkpoint.ID, Namespace: frontier.Namespace, HeadID: head, Resolved: resolved(scan.Commits, head),
				})
			}
		}
	}
	return ctx.Err()
}

func projectEvents(ctx context.Context, scan ledger.ScanResult, snapshot *Snapshot) error {
	work := 0
	for _, event := range scan.Events {
		if err := pollIndexContext(ctx, work); err != nil {
			return err
		}
		work++
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
			if err := pollIndexContext(ctx, work); err != nil {
				return err
			}
			work++
			snapshot.EventTags = append(snapshot.EventTags, EventTagRow{EventRef: event.Ref, Tag: tag})
		}
		for _, target := range event.CausedBy {
			if err := pollIndexContext(ctx, work); err != nil {
				return err
			}
			work++
			snapshot.EventLinks = append(snapshot.EventLinks, EventLinkRow{SourceRef: event.Ref, Relation: "caused_by", TargetRef: target, Resolved: resolved(scan.Events, target)})
		}
		for _, target := range event.Supersedes {
			if err := pollIndexContext(ctx, work); err != nil {
				return err
			}
			work++
			snapshot.EventLinks = append(snapshot.EventLinks, EventLinkRow{SourceRef: event.Ref, Relation: "supersedes", TargetRef: target, Resolved: resolved(scan.Events, target)})
		}
	}
	return ctx.Err()
}

func projectHeads(ctx context.Context, scan ledger.ScanResult, snapshot *Snapshot) error {
	work := 0
	for namespace, commits := range scan.Heads {
		for _, commit := range commits {
			if err := pollIndexContext(ctx, work); err != nil {
				return err
			}
			work++
			snapshot.Heads = append(snapshot.Heads, HeadRow{Namespace: namespace, CommitID: commit})
		}
	}
	return ctx.Err()
}

func projectBlockers(ctx context.Context, scan ledger.ScanResult, snapshot *Snapshot) error {
	for index, blocker := range scan.Completeness.Blockers {
		if err := pollIndexContext(ctx, index); err != nil {
			return err
		}
		snapshot.CompletenessBlockers = append(snapshot.CompletenessBlockers, CompletenessBlockerRow{
			SourceID: blocker.SourceID, Code: blocker.Code, Field: blocker.Field, MissingRef: blocker.MissingRef,
		})
	}
	return ctx.Err()
}

func resolved[Value any](values map[string]Value, key string) int64 {
	if _, found := values[key]; found {
		return 1
	}
	return 0
}

func sortSnapshot(ctx context.Context, snapshot *Snapshot) error { //nolint:gocognit,gocyclo // Every fixed table needs the same cancellable primary-key sort gate.
	if err := sortIndexRowsContext(ctx, snapshot.Objects, func(leftRow, rightRow ObjectRow) bool { return leftRow.ObjectID < rightRow.ObjectID }); err != nil {
		return err
	}
	if err := sortIndexRowsContext(ctx, snapshot.Commits, func(leftRow, rightRow CommitRow) bool { return leftRow.CommitID < rightRow.CommitID }); err != nil {
		return err
	}
	if err := sortIndexRowsContext(ctx, snapshot.ParentEdges, func(leftRow, rightRow ParentEdgeRow) bool {
		return leftRow.ChildID < rightRow.ChildID || leftRow.ChildID == rightRow.ChildID && leftRow.ParentID < rightRow.ParentID
	}); err != nil {
		return err
	}
	if err := sortIndexRowsContext(ctx, snapshot.Events, func(leftRow, rightRow EventRow) bool { return leftRow.EventRef < rightRow.EventRef }); err != nil {
		return err
	}
	if err := sortIndexRowsContext(ctx, snapshot.EventTags, func(leftRow, rightRow EventTagRow) bool {
		return leftRow.EventRef < rightRow.EventRef || leftRow.EventRef == rightRow.EventRef && leftRow.Tag < rightRow.Tag
	}); err != nil {
		return err
	}
	if err := sortIndexRowsContext(ctx, snapshot.EventLinks, func(leftRow, rightRow EventLinkRow) bool {
		if leftRow.SourceRef != rightRow.SourceRef {
			return leftRow.SourceRef < rightRow.SourceRef
		}
		if leftRow.Relation != rightRow.Relation {
			return leftRow.Relation < rightRow.Relation
		}
		return leftRow.TargetRef < rightRow.TargetRef
	}); err != nil {
		return err
	}
	if err := sortIndexRowsContext(ctx, snapshot.Checkpoints, func(leftRow, rightRow CheckpointRow) bool {
		return leftRow.CheckpointID < rightRow.CheckpointID
	}); err != nil {
		return err
	}
	if err := sortIndexRowsContext(ctx, snapshot.CheckpointSchemaRefs, func(leftRow, rightRow CheckpointSchemaRefRow) bool {
		return leftRow.CheckpointID < rightRow.CheckpointID || leftRow.CheckpointID == rightRow.CheckpointID && leftRow.SchemaRef < rightRow.SchemaRef
	}); err != nil {
		return err
	}
	if err := sortIndexRowsContext(ctx, snapshot.CheckpointFrontier, func(leftRow, rightRow CheckpointFrontierRow) bool {
		if leftRow.CheckpointID != rightRow.CheckpointID {
			return leftRow.CheckpointID < rightRow.CheckpointID
		}
		if leftRow.Namespace != rightRow.Namespace {
			return leftRow.Namespace < rightRow.Namespace
		}
		return leftRow.HeadID < rightRow.HeadID
	}); err != nil {
		return err
	}
	if err := sortIndexRowsContext(ctx, snapshot.Heads, func(leftRow, rightRow HeadRow) bool {
		return leftRow.Namespace < rightRow.Namespace || leftRow.Namespace == rightRow.Namespace && leftRow.CommitID < rightRow.CommitID
	}); err != nil {
		return err
	}
	if err := sortIndexRowsContext(ctx, snapshot.CompletenessBlockers, func(leftRow, rightRow CompletenessBlockerRow) bool {
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
	}); err != nil {
		return err
	}
	return ctx.Err()
}

func metadataRows(ctx context.Context, scan ledger.ScanResult, snapshot Snapshot) ([]IndexMetaRow, error) {
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
	work := 0
	for key, value := range values {
		if err := pollIndexContext(ctx, work); err != nil {
			return nil, err
		}
		work++
		rows = append(rows, IndexMetaRow{Key: key, Value: value})
	}
	if err := sortIndexRowsContext(ctx, rows, func(left, right IndexMetaRow) bool { return left.Key < right.Key }); err != nil {
		return nil, err
	}
	return rows, nil
}
