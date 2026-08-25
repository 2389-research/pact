// ABOUTME: Tests typed index projection from real signed immutable ledger scans.
// ABOUTME: Covers exact rows, missing targets, ordering, metadata, and data exclusion.
package index

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"pact/internal/identity"
	"pact/internal/ledger"
	"pact/internal/store"
)

func TestProjectHonorsMidProjectionAndMidSortCancellation(t *testing.T) {
	scan := ledger.ScanResult{
		Commits: map[string]ledger.CommitRecord{}, Checkpoints: map[string]ledger.CheckpointRecord{},
		Events: map[string]ledger.EventRecord{}, Heads: map[string][]string{}, CausalBatches: map[string]uint64{},
	}
	for index := range 2_048 {
		id := fmt.Sprintf("sha256:%064x", 2_048-index)
		scan.Commits[id] = ledger.CommitRecord{ID: id}
	}
	oldPoll := afterIndexWorkPoll
	t.Cleanup(func() { afterIndexWorkPoll = oldPoll })
	ctx, cancel := context.WithCancel(context.Background())
	polls := 0
	afterIndexWorkPoll = func() {
		polls++
		if polls == 4 {
			cancel()
		}
	}
	if snapshot, err := Project(ctx, scan); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(snapshot, Snapshot{}) {
		t.Fatalf("Project() = (%#v, %v) after %d polls, want zero snapshot and context canceled", snapshot, err, polls)
	}

	snapshot := Snapshot{Objects: make([]ObjectRow, 2_048)}
	for index := range snapshot.Objects {
		snapshot.Objects[index].ObjectID = fmt.Sprintf("%08d", len(snapshot.Objects)-index)
	}
	ctx, cancel = context.WithCancel(context.Background())
	polls = 0
	afterIndexWorkPoll = func() {
		polls++
		if polls == 4 {
			cancel()
		}
	}
	if err := sortSnapshot(ctx, &snapshot); !errors.Is(err, context.Canceled) {
		t.Fatalf("sortSnapshot() error = %v after %d polls, want context canceled", err, polls)
	}
}

func TestProjectionCancellationPropagatesThroughLockedOperations(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	manager := New(fixture.store)
	if _, err := manager.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	oldBefore := beforeIndexProjection
	t.Cleanup(func() { beforeIndexProjection = oldBefore })
	operations := []struct {
		name string
		run  func(context.Context) error
	}{
		{name: "rebuild", run: func(ctx context.Context) error { _, err := manager.Rebuild(ctx); return err }},
		{name: "status", run: func(ctx context.Context) error { _, err := manager.Status(ctx); return err }},
		{name: "log", run: func(ctx context.Context) error {
			_, err := manager.Log(ctx, LogRequest{Namespace: []string{"fixture"}})
			return err
		}},
		{name: "query", run: func(ctx context.Context) error {
			_, err := manager.Query(ctx, QueryRequest{Filters: Filters{Namespace: []string{"fixture"}}})
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			beforeIndexProjection = cancel
			assertDirectContextError(t, operation.run(ctx), context.Canceled)
		})
	}
}

const (
	fixturePolicyRef = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fixtureSchemaA   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fixtureSchemaB   = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

type signedFixture struct {
	scan                  ledger.ScanResult
	store                 *store.Store
	key                   *identity.KeyFile
	presentParent         ledger.CommitResult
	missingParent         ledger.CommitResult
	child                 ledger.CommitResult
	chainCommit           ledger.CommitResult
	supersedesCommit      ledger.CommitResult
	missingHeadCommit     ledger.CommitResult
	missingHeadCheckpoint ledger.CheckpointResult
	missingPrevious       ledger.CheckpointResult
	latestCheckpoint      ledger.CheckpointResult
	missingSupersedes     string
}

func TestProjectSignedPartialReplicaExactRows(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	snapshot := mustProject(t, fixture.scan)

	wantObjects := []ObjectRow{
		objectRowForCommit(fixture.scan.Commits[fixture.presentParent.ObjectID]),
		objectRowForCommit(fixture.scan.Commits[fixture.child.ObjectID]),
		objectRowForCommit(fixture.scan.Commits[fixture.chainCommit.ObjectID]),
		objectRowForCommit(fixture.scan.Commits[fixture.supersedesCommit.ObjectID]),
		objectRowForCheckpoint(fixture.scan.Checkpoints[fixture.missingHeadCheckpoint.ObjectID]),
		objectRowForCheckpoint(fixture.scan.Checkpoints[fixture.latestCheckpoint.ObjectID]),
	}
	sort.Slice(wantObjects, func(left, right int) bool { return wantObjects[left].ObjectID < wantObjects[right].ObjectID })
	assertEqual(t, "objects", snapshot.Objects, wantObjects)

	wantCommits := []CommitRow{
		{CommitID: fixture.presentParent.ObjectID, EventCount: 1},
		{CommitID: fixture.child.ObjectID, EventCount: 2},
		{CommitID: fixture.chainCommit.ObjectID, EventCount: 1},
		{CommitID: fixture.supersedesCommit.ObjectID, EventCount: 1},
	}
	sort.Slice(wantCommits, func(left, right int) bool { return wantCommits[left].CommitID < wantCommits[right].CommitID })
	assertEqual(t, "commits", snapshot.Commits, wantCommits)

	wantParents := []ParentEdgeRow{
		{ChildID: fixture.child.ObjectID, ParentID: fixture.presentParent.ObjectID, Resolved: 1},
		{ChildID: fixture.child.ObjectID, ParentID: fixture.missingParent.ObjectID, Resolved: 0},
	}
	sort.Slice(wantParents, func(left, right int) bool {
		if wantParents[left].ChildID != wantParents[right].ChildID {
			return wantParents[left].ChildID < wantParents[right].ChildID
		}
		return wantParents[left].ParentID < wantParents[right].ParentID
	})
	assertEqual(t, "parent edges", snapshot.ParentEdges, wantParents)

	wantEvents := make([]EventRow, 0, len(fixture.scan.Events))
	for _, ref := range []string{
		fixture.presentParent.EventRefs[0], fixture.child.EventRefs[0], fixture.child.EventRefs[1], fixture.chainCommit.EventRefs[0], fixture.supersedesCommit.EventRefs[0],
	} {
		event := fixture.scan.Events[ref]
		batch, ordered := fixture.scan.CausalBatches[ref]
		var batchPointer *uint64
		status := "unresolved"
		if ordered {
			batchCopy := batch
			batchPointer = &batchCopy
			status = "ordered"
		}
		wantEvents = append(wantEvents, EventRow{
			EventRef: ref, CommitID: event.CommitID, LocalID: event.LocalID, Kind: event.Kind,
			EventType: event.Type, Subject: event.Subject, SchemaRef: event.SchemaRef,
			CausalBatch: batchPointer, CausalStatus: status,
		})
	}
	sort.Slice(wantEvents, func(left, right int) bool { return wantEvents[left].EventRef < wantEvents[right].EventRef })
	assertEqual(t, "events", snapshot.Events, wantEvents)
	for _, row := range snapshot.Events {
		switch row.EventRef {
		case fixture.child.EventRefs[0]:
			if row.CausalStatus != "unresolved" || row.CausalBatch != nil {
				t.Errorf("missing caused-by event causal state = %#v, want unresolved with null batch", row)
			}
		case fixture.supersedesCommit.EventRefs[0]:
			if row.CausalStatus != "ordered" || row.CausalBatch == nil {
				t.Errorf("missing supersedes event causal state = %#v, want ordered with non-null batch", row)
			}
		}
	}

	wantTags := []EventTagRow{
		{EventRef: fixture.presentParent.EventRefs[0], Tag: "origin"},
		{EventRef: fixture.child.EventRefs[0], Tag: "alpha"},
		{EventRef: fixture.child.EventRefs[0], Tag: "zulu"},
		{EventRef: fixture.child.EventRefs[1], Tag: "beta"},
	}
	sort.Slice(wantTags, func(left, right int) bool {
		if wantTags[left].EventRef != wantTags[right].EventRef {
			return wantTags[left].EventRef < wantTags[right].EventRef
		}
		return wantTags[left].Tag < wantTags[right].Tag
	})
	assertEqual(t, "event tags", snapshot.EventTags, wantTags)

	wantLinks := []EventLinkRow{
		{SourceRef: fixture.child.EventRefs[0], Relation: "caused_by", TargetRef: fixture.missingParent.EventRefs[0], Resolved: 0},
		{SourceRef: fixture.child.EventRefs[0], Relation: "supersedes", TargetRef: fixture.presentParent.EventRefs[0], Resolved: 1},
		{SourceRef: fixture.child.EventRefs[1], Relation: "caused_by", TargetRef: fixture.presentParent.EventRefs[0], Resolved: 1},
		{SourceRef: fixture.supersedesCommit.EventRefs[0], Relation: "supersedes", TargetRef: fixture.missingSupersedes, Resolved: 0},
	}
	sort.Slice(wantLinks, func(left, right int) bool {
		if wantLinks[left].SourceRef != wantLinks[right].SourceRef {
			return wantLinks[left].SourceRef < wantLinks[right].SourceRef
		}
		if wantLinks[left].Relation != wantLinks[right].Relation {
			return wantLinks[left].Relation < wantLinks[right].Relation
		}
		return wantLinks[left].TargetRef < wantLinks[right].TargetRef
	})
	assertEqual(t, "event links", snapshot.EventLinks, wantLinks)

	missingHead := fixture.scan.Checkpoints[fixture.missingHeadCheckpoint.ObjectID]
	latest := fixture.scan.Checkpoints[fixture.latestCheckpoint.ObjectID]
	previous := fixture.missingPrevious.ObjectID
	wantCheckpoints := []CheckpointRow{
		{CheckpointID: missingHead.ID, Scope: missingHead.Scope, PolicyRef: missingHead.PolicyRef, AuthorityEpoch: missingHead.AuthorityEpoch},
		{CheckpointID: latest.ID, Scope: latest.Scope, PolicyRef: latest.PolicyRef, AuthorityEpoch: latest.AuthorityEpoch, PreviousCheckpoint: &previous},
	}
	sort.Slice(wantCheckpoints, func(left, right int) bool {
		return wantCheckpoints[left].CheckpointID < wantCheckpoints[right].CheckpointID
	})
	assertEqual(t, "checkpoints", snapshot.Checkpoints, wantCheckpoints)

	wantSchemaRefs := []CheckpointSchemaRefRow{
		{CheckpointID: missingHead.ID, SchemaRef: fixtureSchemaA},
		{CheckpointID: missingHead.ID, SchemaRef: fixtureSchemaB},
		{CheckpointID: latest.ID, SchemaRef: fixtureSchemaA},
		{CheckpointID: latest.ID, SchemaRef: fixtureSchemaB},
	}
	sort.Slice(wantSchemaRefs, func(left, right int) bool {
		if wantSchemaRefs[left].CheckpointID != wantSchemaRefs[right].CheckpointID {
			return wantSchemaRefs[left].CheckpointID < wantSchemaRefs[right].CheckpointID
		}
		return wantSchemaRefs[left].SchemaRef < wantSchemaRefs[right].SchemaRef
	})
	assertEqual(t, "checkpoint schema refs", snapshot.CheckpointSchemaRefs, wantSchemaRefs)

	wantFrontier := []CheckpointFrontierRow{
		{CheckpointID: missingHead.ID, Namespace: "fixture/missing-head", HeadID: fixture.missingHeadCommit.ObjectID, Resolved: 0},
		{CheckpointID: latest.ID, Namespace: "fixture/chain", HeadID: fixture.chainCommit.ObjectID, Resolved: 1},
	}
	sort.Slice(wantFrontier, func(left, right int) bool {
		if wantFrontier[left].CheckpointID != wantFrontier[right].CheckpointID {
			return wantFrontier[left].CheckpointID < wantFrontier[right].CheckpointID
		}
		if wantFrontier[left].Namespace != wantFrontier[right].Namespace {
			return wantFrontier[left].Namespace < wantFrontier[right].Namespace
		}
		return wantFrontier[left].HeadID < wantFrontier[right].HeadID
	})
	assertEqual(t, "checkpoint frontier", snapshot.CheckpointFrontier, wantFrontier)

	wantHeads := []HeadRow{
		{Namespace: "fixture/chain", CommitID: fixture.chainCommit.ObjectID},
		{Namespace: "fixture/projection", CommitID: fixture.child.ObjectID},
		{Namespace: "fixture/supersedes", CommitID: fixture.supersedesCommit.ObjectID},
	}
	assertEqual(t, "heads", snapshot.Heads, wantHeads)

	wantBlockers := make([]CompletenessBlockerRow, len(fixture.scan.Completeness.Blockers))
	for index, blocker := range fixture.scan.Completeness.Blockers {
		wantBlockers[index] = CompletenessBlockerRow{SourceID: blocker.SourceID, Code: blocker.Code, Field: blocker.Field, MissingRef: blocker.MissingRef}
	}
	sort.Slice(wantBlockers, func(left, right int) bool {
		if wantBlockers[left].SourceID != wantBlockers[right].SourceID {
			return wantBlockers[left].SourceID < wantBlockers[right].SourceID
		}
		if wantBlockers[left].Code != wantBlockers[right].Code {
			return wantBlockers[left].Code < wantBlockers[right].Code
		}
		if wantBlockers[left].Field != wantBlockers[right].Field {
			return wantBlockers[left].Field < wantBlockers[right].Field
		}
		return wantBlockers[left].MissingRef < wantBlockers[right].MissingRef
	})
	assertEqual(t, "completeness blockers", snapshot.CompletenessBlockers, wantBlockers)

	wantMeta := expectedMetadata(fixture.scan, snapshot)
	assertEqual(t, "index metadata", snapshot.IndexMeta, wantMeta)
}

func TestProjectIsIndependentOfSourceMapInsertionOrder(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	first := mustProject(t, fixture.scan)
	shuffled := shuffleScanMaps(fixture.scan)
	second := mustProject(t, shuffled)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("projection changed after source map reinsertion\nfirst:  %#v\nsecond: %#v", first, second)
	}
	firstDigest, err := LogicalDigest(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := LogicalDigest(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest != secondDigest {
		t.Fatalf("digest changed after source map reinsertion: %q != %q", firstDigest, secondDigest)
	}
}

func TestProjectEmptyAndPartialReplicaMetadata(t *testing.T) {
	empty := mustProject(t, emptyScanFixture(t))
	if len(empty.IndexMeta) != 25 {
		t.Fatalf("empty metadata rows = %d, want 25", len(empty.IndexMeta))
	}
	if got := metadataValue(empty, "row_count_index_meta"); got != "25" {
		t.Fatalf("row_count_index_meta = %q, want 25", got)
	}
	for _, key := range []string{"source_count_objects", "source_count_commits", "source_count_checkpoints", "source_count_events", "source_count_edges", "source_count_canonical_bytes", "row_count_objects", "row_count_events", "row_count_completeness_blockers"} {
		if got := metadataValue(empty, key); got != "0" {
			t.Errorf("%s = %q, want 0", key, got)
		}
	}

	fixture := signedPartialScanFixture(t)
	partial := mustProject(t, fixture.scan)
	if got := metadataValue(partial, "local_completeness"); got != "incomplete" {
		t.Fatalf("local_completeness = %q, want incomplete", got)
	}
	codes := map[string]bool{}
	for _, blocker := range partial.CompletenessBlockers {
		codes[blocker.Code] = true
	}
	for _, code := range []string{"missing_parent", "missing_event_reference", "missing_checkpoint_head", "missing_previous_checkpoint"} {
		if !codes[code] {
			t.Errorf("missing blocker class %q in %#v", code, partial.CompletenessBlockers)
		}
	}
}

func emptyScanFixture(t *testing.T) ledger.ScanResult {
	t.Helper()
	st, err := store.Init(t.TempDir(), "fixture/empty", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	scan, err := ledger.Scan(context.Background(), st, ledger.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return scan
}

func TestSnapshotForbiddenCanonicalAndPrivateFields(t *testing.T) {
	forbiddenTypes := []reflect.Type{reflect.TypeFor[ledger.ObjectVerification](), reflect.TypeFor[identity.KeyFile]()}
	forbiddenNames := []string{"payload", "evidence", "signature", "publickey", "private", "trust", "canonicalpath", "canonicalbytes", "path"}
	var inspect func(reflect.Type, string)
	inspect = func(kind reflect.Type, location string) {
		if kind.Kind() == reflect.Pointer || kind.Kind() == reflect.Slice || kind.Kind() == reflect.Array {
			inspect(kind.Elem(), location)
			return
		}
		if kind.Kind() == reflect.Map {
			t.Errorf("%s uses forbidden map type %s", location, kind)
			return
		}
		for _, forbidden := range forbiddenTypes {
			if kind == forbidden {
				t.Errorf("%s uses forbidden source type %s", location, kind)
			}
		}
		if kind.Kind() != reflect.Struct {
			return
		}
		for field := range kind.Fields() {
			normalized := strings.ToLower(strings.ReplaceAll(field.Name, "_", ""))
			for _, forbidden := range forbiddenNames {
				if normalized == forbidden {
					t.Errorf("%s.%s is a forbidden snapshot field", location, field.Name)
				}
			}
			inspect(field.Type, location+"."+field.Name)
		}
	}
	inspect(reflect.TypeFor[Snapshot](), "Snapshot")
}

func signedPartialScanFixture(t *testing.T) signedFixture {
	t.Helper()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	st, err := store.Init(t.TempDir(), "fixture/default", now)
	if err != nil {
		t.Fatal(err)
	}
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index)
	}
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)
	keyID, err := identity.KeyID(public)
	if err != nil {
		t.Fatal(err)
	}
	key := &identity.KeyFile{Actor: "Index Fixture", KeyID: keyID, Public: public, Private: private, CreatedAt: now}
	if _, err := ledger.AddRoot(st, key, now); err != nil {
		t.Fatal(err)
	}

	present := commitFixture(t, st, key, "fixture/projection", nil, now, fixtureEvent("source", "observation", []string{"origin"}, nil, nil))
	missing := commitFixture(t, st, key, "fixture/projection", []string{}, now, fixtureEvent("gone", "assertion", nil, nil, nil))
	missingHeadCommit := commitFixture(t, st, key, "fixture/missing-head", []string{}, now, fixtureEvent("head", "control", nil, nil, nil))
	missingHeadCheckpoint, err := ledger.Checkpoint(st, key, ledger.CheckpointOptions{
		Scope: "fixture/missing-head", PolicyRef: fixturePolicyRef, AuthorityEpoch: "epoch-1",
		SchemaRefs: []string{fixtureSchemaB, fixtureSchemaA}, ObservedAt: now.Add(2 * time.Minute).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	chainCommit := commitFixture(t, st, key, "fixture/chain", []string{}, now, fixtureEvent("chain", "control", nil, nil, nil))
	missingPrevious, err := ledger.Checkpoint(st, key, ledger.CheckpointOptions{
		Scope: "fixture/chain", PolicyRef: fixturePolicyRef, AuthorityEpoch: "epoch-1",
		SchemaRefs: []string{fixtureSchemaA}, ObservedAt: now.Add(3 * time.Minute).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := ledger.Checkpoint(st, key, ledger.CheckpointOptions{
		Scope: "fixture/chain", PolicyRef: fixturePolicyRef, AuthorityEpoch: "epoch-2", PreviousCheckpoint: missingPrevious.ObjectID,
		SchemaRefs: []string{fixtureSchemaB, fixtureSchemaA}, ObservedAt: now.Add(4 * time.Minute).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	missingSupersedes := ledger.EventRef("sha256:"+strings.Repeat("d", 64), "missing")
	supersedesCommit := commitFixture(t, st, key, "fixture/supersedes", []string{}, now,
		fixtureEvent("supersedes", "decision", nil, nil, []string{missingSupersedes}),
	)
	child := commitFixture(t, st, key, "fixture/projection", []string{present.ObjectID, missing.ObjectID}, now.Add(time.Minute),
		fixtureEvent("alpha", "action", []string{"zulu", "alpha"}, []string{missing.EventRefs[0]}, []string{present.EventRefs[0]}),
		fixtureEvent("beta", "decision", []string{"beta"}, []string{present.EventRefs[0]}, nil),
	)
	for _, objectPath := range []string{missing.Path, missingHeadCommit.Path, missingPrevious.Path} {
		if err := os.Remove(objectPath); err != nil {
			t.Fatal(err)
		}
	}
	scan, err := ledger.Scan(context.Background(), st, ledger.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return signedFixture{
		scan: scan, store: st, key: key, presentParent: present, missingParent: missing, child: child,
		chainCommit: chainCommit, supersedesCommit: supersedesCommit, missingHeadCommit: missingHeadCommit, missingHeadCheckpoint: missingHeadCheckpoint,
		missingPrevious: missingPrevious, latestCheckpoint: latest, missingSupersedes: missingSupersedes,
	}
}

func fixtureEvent(localID, kind string, tags, causedBy, supersedes []string) ledger.Event {
	return ledger.Event{
		LocalID: localID, Kind: kind, Type: "fixture." + localID, Subject: "fixture/subject", SchemaRef: "pact:core/fixture/v1",
		Payload: map[string]any{"excluded": localID}, Evidence: []map[string]any{{
			"ref": "https://example.invalid/" + localID, "digest": "sha256:" + strings.Repeat("e", 64),
			"media_type": "text/plain", "role": "supporting",
		}},
		Tags: tags, CausedBy: causedBy, Supersedes: supersedes,
	}
}

func commitFixture(t *testing.T, st *store.Store, key *identity.KeyFile, namespace string, parents []string, observed time.Time, events ...ledger.Event) ledger.CommitResult {
	t.Helper()
	result, err := ledger.Commit(st, key, ledger.EventBatch{Namespace: namespace, Events: events}, ledger.CommitOptions{
		Namespace: namespace, Parents: parents, ObservedAt: observed.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func objectRowForCommit(record ledger.CommitRecord) ObjectRow {
	return ObjectRow{
		ObjectID: record.ID, ObjectType: "commit", Namespace: record.Namespace, BodyDigest: record.BodyDigest,
		ActorKeyID: record.ActorID, ActorLabel: record.ActorLabel, ObservedAt: record.ObservedAt,
		IntegrityState: record.Integrity, StructureState: record.Structure, AuthenticityState: record.Authenticity, CompletenessState: record.Completeness,
	}
}

func objectRowForCheckpoint(record ledger.CheckpointRecord) ObjectRow {
	return ObjectRow{
		ObjectID: record.ID, ObjectType: "checkpoint", Namespace: record.Scope, BodyDigest: record.BodyDigest,
		ActorKeyID: record.ActorID, ActorLabel: record.ActorLabel, ObservedAt: record.ObservedAt,
		IntegrityState: record.Integrity, StructureState: record.Structure, AuthenticityState: record.Authenticity, CompletenessState: record.Completeness,
	}
}

func expectedMetadata(scan ledger.ScanResult, snapshot Snapshot) []IndexMetaRow {
	values := map[string]string{
		"format": IndexFormat, "schema_version": strconv.Itoa(SchemaVersion), "schema_digest": SchemaDigest(),
		"source_fingerprint": scan.SourceFingerprint, "logical_digest": metadataValue(snapshot, "logical_digest"),
		"limits_contract":      "pact/resource-limits/phase2-v1",
		"source_count_objects": strconv.FormatUint(scan.Counts.Objects, 10), "source_count_commits": strconv.FormatUint(scan.Counts.Commits, 10),
		"source_count_checkpoints": strconv.FormatUint(scan.Counts.Checkpoints, 10), "source_count_events": strconv.FormatUint(scan.Counts.Events, 10),
		"source_count_edges": strconv.FormatUint(scan.Counts.Edges, 10), "source_count_canonical_bytes": strconv.FormatUint(scan.Counts.CanonicalBytes, 10),
		"row_count_index_meta": "25", "row_count_objects": strconv.Itoa(len(snapshot.Objects)), "row_count_commits": strconv.Itoa(len(snapshot.Commits)),
		"row_count_parent_edges": strconv.Itoa(len(snapshot.ParentEdges)), "row_count_events": strconv.Itoa(len(snapshot.Events)),
		"row_count_event_tags": strconv.Itoa(len(snapshot.EventTags)), "row_count_event_links": strconv.Itoa(len(snapshot.EventLinks)),
		"row_count_checkpoints": strconv.Itoa(len(snapshot.Checkpoints)), "row_count_checkpoint_schema_refs": strconv.Itoa(len(snapshot.CheckpointSchemaRefs)),
		"row_count_checkpoint_frontier": strconv.Itoa(len(snapshot.CheckpointFrontier)), "row_count_heads": strconv.Itoa(len(snapshot.Heads)),
		"row_count_completeness_blockers": strconv.Itoa(len(snapshot.CompletenessBlockers)), "local_completeness": scan.Completeness.Status,
	}
	rows := make([]IndexMetaRow, 0, len(values))
	for key, value := range values {
		rows = append(rows, IndexMetaRow{Key: key, Value: value})
	}
	sort.Slice(rows, func(left, right int) bool { return rows[left].Key < rows[right].Key })
	return rows
}

func metadataValue(snapshot Snapshot, key string) string {
	for _, row := range snapshot.IndexMeta {
		if row.Key == key {
			return row.Value
		}
	}
	return ""
}

func shuffleScanMaps(scan ledger.ScanResult) ledger.ScanResult {
	result := scan
	result.Objects = reverseMap(scan.Objects)
	result.Commits = reverseMap(scan.Commits)
	result.Checkpoints = reverseMap(scan.Checkpoints)
	result.Events = reverseMap(scan.Events)
	result.Heads = reverseMap(scan.Heads)
	result.CausalBatches = reverseMap(scan.CausalBatches)
	return result
}

func reverseMap[Value any](source map[string]Value) map[string]Value {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	result := make(map[string]Value, len(source))
	for _, key := range keys {
		result[key] = source[key]
	}
	return result
}

func assertEqual(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s =\n%#v\nwant\n%#v", name, got, want)
	}
}

func TestSignedFixtureHasExpectedSourceCounts(t *testing.T) {
	fixture := signedPartialScanFixture(t)
	want := ledger.ScanCounts{Objects: 6, Commits: 4, Checkpoints: 2, Events: 5, Edges: 19}
	if got := fixture.scan.Counts; got.Objects != want.Objects || got.Commits != want.Commits || got.Checkpoints != want.Checkpoints || got.Events != want.Events || got.Edges != want.Edges {
		t.Fatalf("scan counts = %s, want %s (canonical bytes vary by fixture encoding)", fmt.Sprint(got), fmt.Sprint(want))
	}
}
