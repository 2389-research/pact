// ABOUTME: Tests the bounded canonical ledger scan and source fingerprint contract.
// ABOUTME: Covers completeness blockers, resource limits, cancellation, and safe diagnostics.
package ledger

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"pact/internal/canonical"
	"pact/internal/identity"
	"pact/internal/store"
)

type cancelAfterErrChecks struct {
	context.Context
	checks   int
	cancelAt int
}

func (ctx *cancelAfterErrChecks) Err() error {
	ctx.checks++
	if ctx.checks >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}

func TestSourceFingerprintExactVectors(t *testing.T) {
	tests := []struct {
		ids  []string
		want string
	}{
		{ids: nil, want: "sha256:239a46f0a3514bf5f07eaea4b834fbe96ba805b502c0664d510886dc3a52fe87"},
		{ids: []string{"sha256:" + strings.Repeat("a", 64)}, want: "sha256:4ba133890004ca09640d302e0da6b79eddae779e701cedd87762f70a756d47b0"},
		{ids: []string{"sha256:" + strings.Repeat("b", 64), "sha256:" + strings.Repeat("a", 64)}, want: "sha256:8a35ae53013c58abf1b9f95504494830efc14ab4589d2af32db2a4f65551f013"},
	}
	for _, test := range tests {
		if got := sourceFingerprint(test.ids); got != test.want {
			t.Fatalf("sourceFingerprint(%q) = %q, want %q", test.ids, got, test.want)
		}
	}
}

func TestBoundedSortingStopsBeforeScanningCanceledCollections(t *testing.T) {
	values := make(map[string]int, 4_096)
	for index := range 4_096 {
		values[fmt.Sprintf("%08d", index)] = index
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sortedKeysContext(ctx, values); !errors.Is(err, context.Canceled) {
		t.Fatalf("sortedKeysContext() error = %v, want context canceled", err)
	}
	ids := make([]string, 4_096)
	for index := range ids {
		ids[index] = fmt.Sprintf("sha256:%064x", index)
	}
	if _, err := sourceFingerprintContext(ctx, ids); !errors.Is(err, context.Canceled) {
		t.Fatalf("sourceFingerprintContext() error = %v, want context canceled", err)
	}
}

func TestLedgerMergeSortChecksCancellationBeforeFullBufferAllocation(t *testing.T) {
	values := make([]string, 512)
	for index := range values {
		values[index] = fmt.Sprintf("%08d", len(values)-index)
	}
	ctx, cancel := context.WithCancel(context.Background())
	originalBefore := beforeLedgerMergeBufferAllocation
	originalAfter := afterLedgerMergeBufferAllocation
	allocated := false
	beforeLedgerMergeBufferAllocation = cancel
	afterLedgerMergeBufferAllocation = func() { allocated = true }
	t.Cleanup(func() {
		beforeLedgerMergeBufferAllocation = originalBefore
		afterLedgerMergeBufferAllocation = originalAfter
	})
	if _, err := sortOwnedStringsContext(ctx, values); !errors.Is(err, context.Canceled) || allocated {
		t.Fatalf("sortOwnedStringsContext() error = %v, allocated = %t; want pre-allocation cancellation", err, allocated)
	}
}

func TestCheckpointProjectionCloneHonorsMidLoopCancellation(t *testing.T) {
	frontier := make([]CheckpointFrontier, 256)
	for index := range frontier {
		frontier[index] = CheckpointFrontier{Namespace: fmt.Sprintf("scope/%03d", index), Heads: []string{fmt.Sprintf("head-%03d", index)}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	original := afterLedgerWorkPoll
	polls := 0
	afterLedgerWorkPoll = func() {
		polls++
		if polls == 2 {
			cancel()
		}
	}
	t.Cleanup(func() { afterLedgerWorkPoll = original })
	if _, err := cloneCheckpointFrontierContext(ctx, frontier); !errors.Is(err, context.Canceled) {
		t.Fatalf("cloneCheckpointFrontierContext() error = %v, want context canceled", err)
	}
}

func TestStoredCommitProjectionHonorsMidLoopCancellation(t *testing.T) {
	events := make([]any, 256)
	for index := range events {
		events[index] = eventInput(fmt.Sprintf("event-%03d", index), []any{}, []any{})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelOnSecondLedgerPoll(t, cancel)
	if _, err := storedCommitFromObjectContext(ctx, projectionCommitObject(events)); !errors.Is(err, context.Canceled) {
		t.Fatalf("storedCommitFromObjectContext() error = %v, want context canceled", err)
	}
}

func TestStoredCheckpointProjectionHonorsNestedCancellation(t *testing.T) {
	heads := make([]any, 256)
	for index := range heads {
		heads[index] = fmt.Sprintf("sha256:%064x", index)
	}
	object := map[string]any{"body": map[string]any{"frontier": []any{map[string]any{"namespace": "scope", "heads": heads}}, "previous_checkpoint": nil}}
	ctx, cancel := context.WithCancel(context.Background())
	cancelOnSecondLedgerPoll(t, cancel)
	if _, err := storedCheckpointFromObjectContext(ctx, object); !errors.Is(err, context.Canceled) {
		t.Fatalf("storedCheckpointFromObjectContext() error = %v, want context canceled", err)
	}
}

func TestEventRecordProjectionHonorsMidLoopCancellation(t *testing.T) {
	storedEvents := make([]storedEvent, 256)
	for index := range storedEvents {
		object := eventInput(fmt.Sprintf("event-%03d", index), []any{}, []any{})
		storedEvents[index] = storedEvent{localID: object["local_id"].(string), object: object}
	}
	verification := ObjectVerification{object: projectionCommitObject(nil)}
	ctx, cancel := context.WithCancel(context.Background())
	cancelOnSecondLedgerPoll(t, cancel)
	if _, _, err := recordsForCommitContext(ctx, "commit", verification, storedCommit{namespace: "scope", events: storedEvents}); !errors.Is(err, context.Canceled) {
		t.Fatalf("recordsForCommitContext() error = %v, want context canceled", err)
	}
}

func TestEventRecordPublicationHonorsMidLoopCancellation(t *testing.T) {
	source := make(map[string]EventRecord, 256)
	for index := range 256 {
		ref := fmt.Sprintf("event-%03d", index)
		source[ref] = EventRecord{Ref: ref}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelOnSecondLedgerPoll(t, cancel)
	if err := copyEventRecordsContext(ctx, map[string]EventRecord{}, source); !errors.Is(err, context.Canceled) {
		t.Fatalf("copyEventRecordsContext() error = %v, want context canceled", err)
	}
}

func TestFinalHeadCloneHonorsNestedCancellation(t *testing.T) {
	heads := make([]string, 256)
	for index := range heads {
		heads[index] = fmt.Sprintf("head-%03d", index)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelOnSecondLedgerPoll(t, cancel)
	if _, err := cloneStringMapContext(ctx, map[string][]string{"scope": heads}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cloneStringMapContext() error = %v, want context canceled", err)
	}
}

func TestResolveCommitProjectionHonorsMidLoopCancellation(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	batch := mustBatch(t, "event-000")
	batch.Events = make([]Event, 256)
	for index := range batch.Events {
		batch.Events[index] = mustBatch(t, fmt.Sprintf("event-%03d", index)).Events[0]
	}
	commit, err := Commit(st, key, batch, CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	original := beforeResolveCommitProjection
	beforeResolveCommitProjection = func() { cancelOnSecondLedgerPoll(t, cancel) }
	t.Cleanup(func() { beforeResolveCommitProjection = original })
	if _, err := ResolveCommit(ctx, st, commit.ObjectID, Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ResolveCommit() error = %v, want context canceled during projection", err)
	}
}

func TestResolveCommitPropagatesCancellationDuringCanonicalParse(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	batch := mustBatch(t, "event")
	batch.Events[0].Payload["padding"] = strings.Repeat("x", 4_096)
	commit, err := Commit(st, key, batch, CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := st.Get(commit.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	readProbe := &cancelAfterErrChecks{Context: context.Background(), cancelAt: 1 << 30}
	if _, err := st.GetBoundedContext(readProbe, commit.ObjectID, Phase2Limits.ObjectBytes); err != nil {
		t.Fatal(err)
	}
	digestProbe := &cancelAfterErrChecks{Context: context.Background(), cancelAt: 1 << 30}
	if _, err := canonical.DigestContext(digestProbe, raw); err != nil {
		t.Fatal(err)
	}
	// ResolveCommit polls once around the store read. The next two polls enter
	// ParseContext and cancel in its first UTF-8 validation chunk.
	ctx := &cancelAfterErrChecks{
		Context:  context.Background(),
		cancelAt: 1 + readProbe.checks + 1 + digestProbe.checks + 2,
	}
	if _, err := ResolveCommit(ctx, st, commit.ObjectID, Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ResolveCommit() error = %v after %d checks, want context canceled during canonical parse", err, ctx.checks)
	}
}

func TestDigestMismatchFallbackReadHonorsMidReadCancellation(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitOne(t, st, key, "event", nil)
	files, err := st.ObjectFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].ID != commit.ObjectID {
		t.Fatalf("object files = %#v, want commit %s", files, commit.ObjectID)
	}
	raw := bytes.Repeat([]byte{'x'}, int(Phase2Limits.ObjectBytes-1))
	if err := os.WriteFile(files[0].Path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := &cancelAfterErrChecks{Context: context.Background(), cancelAt: 1 << 30}
	original := beforeDigestMismatchFallbackRead
	beforeDigestMismatchFallbackRead = func() {
		ctx.checks = 0
		ctx.cancelAt = 2
	}
	t.Cleanup(func() { beforeDigestMismatchFallbackRead = original })
	_, _, _, err = readScannedObject(ctx, st, files[0], Phase2Limits, Phase2Limits.CanonicalBytes, &scanResourceCounts{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("readScannedObject() error = %v, want context canceled during digest-mismatch fallback read", err)
	}
	if ctx.checks != 2 {
		t.Fatalf("fallback read context checks = %d, want cancellation on the second bounded read", ctx.checks)
	}
}

func TestCanonicalConfirmationHonorsCancellationInsideMarshal(t *testing.T) {
	items := make([]any, 512)
	for index := range items {
		items[index] = map[string]any{"index": int64(index), "text": fmt.Sprintf("value-%03d", index)}
	}
	object := map[string]any{"items": items}
	raw, err := canonical.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &cancelAfterErrChecks{Context: context.Background(), cancelAt: 16}
	if _, err := confirmCanonicalBytesContext(ctx, ObjectVerification{object: object}, raw); !errors.Is(err, context.Canceled) {
		t.Fatalf("confirmCanonicalBytesContext() error = %v after %d checks, want context canceled", err, ctx.checks)
	}
}

func cancelOnSecondLedgerPoll(t *testing.T, cancel context.CancelFunc) {
	t.Helper()
	original := afterLedgerWorkPoll
	polls := 0
	afterLedgerWorkPoll = func() {
		polls++
		if polls == 2 {
			cancel()
		}
	}
	t.Cleanup(func() { afterLedgerWorkPoll = original })
}

func projectionCommitObject(events []any) map[string]any {
	return map[string]any{
		"body": map[string]any{
			"namespace": "scope", "parents": []any{}, "events": events, "observed_at": "2026-08-23T12:00:00Z",
			"actor": map[string]any{"key_id": "actor", "label": "Actor"},
		},
		"body_digest": "sha256:" + strings.Repeat("a", 64),
		"signature":   map[string]any{"public_key": "key"},
	}
}

func TestScanEmptyReplica(t *testing.T) {
	st, _ := ledgerStoreAndKey(t)
	result, err := Scan(context.Background(), st, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Objects) != 0 || len(result.Commits) != 0 || len(result.Events) != 0 || len(result.Checkpoints) != 0 {
		t.Fatalf("Scan() records = %#v", result)
	}
	if result.SourceFingerprint != "sha256:239a46f0a3514bf5f07eaea4b834fbe96ba805b502c0664d510886dc3a52fe87" {
		t.Fatalf("source fingerprint = %q", result.SourceFingerprint)
	}
	if !reflect.DeepEqual(result.Completeness, Completeness{Scope: "local_object_set", Status: "locally_closed", GlobalCompleteness: "unknown", Blockers: []Blocker{}}) {
		t.Fatalf("completeness = %#v", result.Completeness)
	}
	if !result.Verification.OK {
		t.Fatalf("verification = %#v", result.Verification)
	}
}

func TestScanCompleteReplicaReturnsTypedImmutableRecords(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	batch := mustBatch(t, "event")
	batch.Events[0].Tags = []string{"alpha", "beta"}
	commit, err := Commit(st, key, batch, CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), st, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	record := result.Commits[commit.ObjectID]
	event := result.Events[EventRef(commit.ObjectID, "event")]
	if record.ID != commit.ObjectID || record.Namespace != "org/example/widget" || record.ActorID != key.KeyID || len(record.EventRefs) != 1 {
		t.Fatalf("commit record = %#v", record)
	}
	if event.Ref != EventRef(commit.ObjectID, "event") || event.CommitID != commit.ObjectID || !equalStrings(event.Tags, []string{"alpha", "beta"}) {
		t.Fatalf("event record = %#v", event)
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[CommitRecord](), reflect.TypeFor[EventRecord](), reflect.TypeFor[CheckpointRecord]()} {
		for field := range typ.Fields() {
			if field.Type.Kind() == reflect.Map {
				t.Fatalf("%s exposes mutable map field %s", typ, field.Name)
			}
		}
	}
	record.EventRefs[0] = "mutated"
	if result.Events[EventRef(commit.ObjectID, "event")].Ref != EventRef(commit.ObjectID, "event") {
		t.Fatal("published record slices alias another scan record")
	}
}

func TestScanPreservesParentsForImplicitCommitChain(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	first := commitOne(t, st, key, "first", nil)
	second := commitOne(t, st, key, "second", nil)
	result, err := Scan(context.Background(), st, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Commits[second.ObjectID].Parents, []string{first.ObjectID}) {
		t.Fatalf("second parents = %#v, want %s", result.Commits[second.ObjectID].Parents, first.ObjectID)
	}
}

func TestScanCompletenessBlockersAreSortedUniqueAndStrictOnlyFailsBlockers(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	missingObject := "sha256:" + strings.Repeat("a", 64)
	missingEvent := EventRef("sha256:"+strings.Repeat("b", 64), "gone")
	missingParentCommit := putSignedCommitForVerify(t, st, key, "org/example/widget", []string{missingObject})
	batch := mustBatch(t, "source")
	batch.Events[0].CausedBy = []string{missingEvent}
	batch.Events[0].Supersedes = []string{missingEvent}
	eventCommit, err := Commit(st, key, batch, CommitOptions{Parents: []string{}, ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	checkpointID := putSignedCheckpointForVerify(t, st, key, "scope", "sha256:"+strings.Repeat("c", 64), "sha256:"+strings.Repeat("d", 64))

	loose, err := Scan(context.Background(), st, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !loose.Verification.OK || loose.Completeness.Status != "incomplete" {
		t.Fatalf("loose Scan() = %#v", loose)
	}
	want := []Blocker{
		{Code: "missing_checkpoint_head", SourceID: checkpointID, Field: "frontier.heads", MissingRef: "sha256:" + strings.Repeat("c", 64)},
		{Code: "missing_event_reference", SourceID: EventRef(eventCommit.ObjectID, "source"), Field: "caused_by", MissingRef: missingEvent},
		{Code: "missing_event_reference", SourceID: EventRef(eventCommit.ObjectID, "source"), Field: "supersedes", MissingRef: missingEvent},
		{Code: "missing_parent", SourceID: missingParentCommit, Field: "parents", MissingRef: missingObject},
		{Code: "missing_previous_checkpoint", SourceID: checkpointID, Field: "previous_checkpoint", MissingRef: "sha256:" + strings.Repeat("d", 64)},
	}
	if !reflect.DeepEqual(loose.Completeness.Blockers, want) {
		t.Fatalf("blockers = %#v, want %#v", loose.Completeness.Blockers, want)
	}
	if !sort.SliceIsSorted(loose.Completeness.Blockers, func(i, j int) bool {
		left, right := loose.Completeness.Blockers[i], loose.Completeness.Blockers[j]
		return blockerKey(left) < blockerKey(right)
	}) {
		t.Fatalf("blockers are not sorted: %#v", loose.Completeness.Blockers)
	}

	strict, err := Scan(context.Background(), st, ScanOptions{Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	if strict.Verification.OK || len(strict.Verification.DAG.Errors) != 1 || len(strict.Verification.References.Errors) != 4 {
		t.Fatalf("strict Scan() = %#v", strict.Verification)
	}
}

func TestRecordCompletenessPropagatesAcrossPresentDependencyClosure(t *testing.T) {
	result := ScanResult{
		Commits: map[string]CommitRecord{
			"a": {ID: "a", Completeness: "complete"},
			"b": {ID: "b", Parents: []string{"a"}, Completeness: "complete"},
			"c": {ID: "c", Completeness: "complete"},
			"d": {ID: "d", Completeness: "complete"},
			"e": {ID: "e", Parents: []string{"d"}, Completeness: "complete"},
			"f": {ID: "f", Completeness: "complete"},
			"g": {ID: "g", Completeness: "complete"},
		},
		Events: map[string]EventRecord{
			"event:b": {Ref: "event:b", CommitID: "b"},
			"event:c": {Ref: "event:c", CommitID: "c", CausedBy: []string{"event:b"}},
			"event:d": {Ref: "event:d", CommitID: "d", Supersedes: []string{"missing"}},
			"event:f": {Ref: "event:f", CommitID: "f", CausedBy: []string{"missing"}},
			"event:g": {Ref: "event:g", CommitID: "g", Supersedes: []string{"event:f"}},
		},
		Checkpoints: map[string]CheckpointRecord{
			"cp1": {ID: "cp1", Frontier: []CheckpointFrontier{{Heads: []string{"b"}}}, Completeness: "complete"},
			"cp2": {ID: "cp2", PreviousCheckpoint: "cp1", Completeness: "complete"},
		},
		Completeness: Completeness{Blockers: []Blocker{
			{Code: "missing_parent", SourceID: "a"},
			{Code: "missing_event_reference", SourceID: "event:d", Field: "supersedes"},
			{Code: "missing_event_reference", SourceID: "event:f", Field: "caused_by"},
		}},
	}
	if err := applyRecordCompleteness(context.Background(), &result); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		if result.Commits[id].Completeness != "partial" {
			t.Fatalf("commit %s completeness = %q", id, result.Commits[id].Completeness)
		}
	}
	if result.Commits["f"].Completeness != "partial" || result.Commits["g"].Completeness != "complete" {
		t.Fatalf("supersedes closure = f:%q g:%q, want partial/complete", result.Commits["f"].Completeness, result.Commits["g"].Completeness)
	}
	for _, id := range []string{"cp1", "cp2"} {
		if result.Checkpoints[id].Completeness != "partial" {
			t.Fatalf("checkpoint %s completeness = %q", id, result.Checkpoints[id].Completeness)
		}
	}
}

func TestScanEnforcesObjectByteEventEdgeAndTotalBounds(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	batch, err := NormalizeEventBatch(map[string]any{"events": []any{eventInput("a", []any{}, []any{}), eventInput("b", []any{}, []any{})}})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := Commit(st, key, batch, CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := st.Get(commit.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		limits   Limits
		resource string
		maximum  uint64
	}{
		{name: "object bytes", limits: Limits{ObjectBytes: uint64(len(raw) - 1)}, resource: "object_bytes", maximum: uint64(len(raw) - 1)},
		{name: "canonical bytes", limits: Limits{CanonicalBytes: uint64(len(raw) - 1)}, resource: "canonical_bytes", maximum: uint64(len(raw) - 1)},
		{name: "events", limits: Limits{Events: 1}, resource: "events", maximum: 1},
		{name: "edges", limits: Limits{GraphEdges: 3}, resource: "graph_edges", maximum: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Scan(context.Background(), st, ScanOptions{Limits: test.limits})
			assertLimitError(t, err, test.resource, test.maximum)
		})
	}
	commitOne(t, st, key, "c", nil)
	_, err = Scan(context.Background(), st, ScanOptions{Limits: Limits{Objects: 1}})
	assertLimitError(t, err, "objects", 1)
}

func TestEffectiveScanLimitsCanReduceButNeverRaisePhaseTwoCaps(t *testing.T) {
	overrides := Limits{
		ObjectBytes:       Phase2Limits.ObjectBytes + 1,
		Objects:           1,
		Events:            Phase2Limits.Events + 1,
		GraphEdges:        7,
		DiagnosticSamples: Phase2Limits.DiagnosticSamples + 1,
	}
	got := effectiveLimits(overrides)
	if got.ObjectBytes != Phase2Limits.ObjectBytes || got.Events != Phase2Limits.Events || got.DiagnosticSamples != Phase2Limits.DiagnosticSamples {
		t.Fatalf("raised effective limits = %#v", got)
	}
	if got.Objects != 1 || got.GraphEdges != 7 {
		t.Fatalf("reduced effective limits = %#v", got)
	}
}

func TestScanResourceAccountingRejectsRawCommitCountsBeforeProjection(t *testing.T) {
	limits := Phase2Limits
	limits.ParentsPerCommit = 1
	object := map[string]any{
		"format": commitFormat,
		"body":   map[string]any{"parents": []any{"one", "two"}, "events": []any{}},
	}
	counts := scanResourceCounts{}
	_, err := counts.preflightParsedObject(context.Background(), "commit-id", object, limits)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Resource != "parents_per_commit" || limit.Maximum != 1 || limit.ObjectID != "commit-id" {
		t.Fatalf("limit = %#v", limit)
	}
	if counts.events != 0 || counts.edges.total() != 0 || counts.rawEvents != 0 || counts.rawEdges.total() != 0 {
		t.Fatalf("accounting changed after rejected raw object: %#v", counts)
	}
}

func TestScanPreflightsRawCanonicalArraysBeforeStructuralValidation(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	events := make([]any, 1_025)
	for index := range events {
		events[index] = map[string]any{"local_id": "duplicate"}
	}
	commitID := putRawSignedObjectForScan(t, st, key, commitFormat, map[string]any{
		"namespace": "org/example/widget", "parents": []any{}, "events": events,
	})
	_, err := Scan(context.Background(), st, ScanOptions{})
	assertObjectLimit(t, err, "events_per_commit", 1_024, commitID)
	_, err = ResolveCommit(context.Background(), st, commitID, Limits{})
	assertObjectLimit(t, err, "events_per_commit", 1_024, commitID)

	st, key = ledgerStoreAndKey(t)
	heads := make([]any, 9)
	for index := range heads {
		heads[index] = index
	}
	checkpointID := putRawSignedObjectForScan(t, st, key, checkpointFormat, map[string]any{
		"frontier": []any{map[string]any{"heads": heads}},
	})
	_, err = Scan(context.Background(), st, ScanOptions{Limits: Limits{GraphEdges: 8}})
	assertObjectLimit(t, err, "graph_edges", 8, checkpointID)
}

func putRawSignedObjectForScan(t *testing.T, st *store.Store, key *identity.KeyFile, format string, body map[string]any) string {
	t.Helper()
	digest, signature, err := identity.SignBody(body, key.Private)
	if err != nil {
		t.Fatal(err)
	}
	object := map[string]any{
		"format": format, "body": body, "body_digest": digest,
		"signature": map[string]any{"algorithm": "ed25519", "key_id": key.KeyID, "public_key": base64.RawURLEncoding.EncodeToString(key.Public), "value": base64.RawURLEncoding.EncodeToString(signature)},
	}
	id, _, err := st.PutCanonical(object)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func assertObjectLimit(t *testing.T, err error, resource string, maximum uint64, objectID string) {
	t.Helper()
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Resource != resource || limit.Maximum != maximum || limit.ObjectID != objectID {
		t.Fatalf("error = %#v, want %s limit %d at %s", err, resource, maximum, objectID)
	}
}

func TestScanEdgeAccountingIncludesEveryContractCategory(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	parent := commitOne(t, st, key, "parent", nil)
	batch := mustBatch(t, "source")
	batch.Events[0].CausedBy = []string{parent.EventRefs[0]}
	batch.Events[0].Supersedes = []string{parent.EventRefs[0]}
	source, err := Commit(st, key, batch, CommitOptions{Parents: []string{parent.ObjectID}, ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	putSignedCheckpointForVerify(t, st, key, "scope", source.ObjectID, "sha256:"+strings.Repeat("f", 64))
	result, err := Scan(context.Background(), st, ScanOptions{Limits: Limits{GraphEdges: 9}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts.Edges != 9 {
		t.Fatalf("Scan() edges = %d, want 9", result.Counts.Edges)
	}
	_, err = Scan(context.Background(), st, ScanOptions{Limits: Limits{GraphEdges: 8}})
	assertLimitError(t, err, "graph_edges", 8)
}

func TestScanProcessesRecordLimitsInObjectIDOrder(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	first, err := Commit(st, key, mustBatch(t, "first"), CommitOptions{Parents: []string{}, ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Commit(st, key, mustBatch(t, "second"), CommitOptions{Parents: []string{}, ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	want := max(first.ObjectID, second.ObjectID)
	for range 30 {
		_, err := Scan(context.Background(), st, ScanOptions{Limits: Limits{Events: 1}})
		var limit *LimitError
		if !errors.As(err, &limit) || limit.ObjectID != want {
			t.Fatalf("Scan() limit = %#v, want deterministic object %s", limit, want)
		}
	}
}

func TestScanHonorsCanceledContext(t *testing.T) {
	st, _ := ledgerStoreAndKey(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, st, ScanOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan() error = %v, want context cancellation", err)
	}
}

func TestScanBoundsDiagnosticSamplesAndText(t *testing.T) {
	st, _ := ledgerStoreAndKey(t)
	for index := range 3 {
		if _, _, err := st.PutCanonical(map[string]any{"format": strings.Repeat(string(rune('a'+index)), 200)}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Scan(context.Background(), st, ScanOptions{Limits: Limits{DiagnosticSamples: 1, DiagnosticTextBytes: 80}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verification.DiagnosticsTruncated || len(result.Verification.Errors) != 1 || len(result.Verification.Structure.Errors) != 1 {
		t.Fatalf("diagnostics = %#v", result.Verification)
	}
	if len(result.Verification.Errors[0]) > 80 || len(result.Verification.Structure.Errors[0]) > 80 {
		t.Fatalf("diagnostic text exceeds bound: %#v", result.Verification)
	}
}

func TestUnsupportedKeySampleOmissionMarksVerificationDiagnosticsTruncated(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitOne(t, st, key, "source", nil)
	raw, err := st.Get(commit.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := canonical.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	object := parsed.(map[string]any)
	alphabet := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for index := range 101 {
		name := string(alphabet[index%len(alphabet)])
		if index >= len(alphabet) {
			name = "a" + name
		}
		object[name] = index
	}
	objectID, _, err := st.PutCanonical(object)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), st, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	verification := result.Objects[objectID]
	if verification.Structure != "invalid" || !result.Verification.DiagnosticsTruncated {
		t.Fatalf("verification = %#v, diagnostics_truncated = %t", verification, result.Verification.DiagnosticsTruncated)
	}
	if len(verification.Errors) != 1 || len(verification.Errors[0]) > 512 {
		t.Fatalf("bounded validation errors = %#v", verification.Errors)
	}
}

func TestCanonicalAttackerKeyTruncationReachesVerification(t *testing.T) {
	st, _ := ledgerStoreAndKey(t)
	key := strings.Repeat("line\n\x1b\"\\é", 200)
	quotedRaw, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	quoted := string(quotedRaw)
	raw := []byte("{" + quoted + ":1," + quoted + ":2}")
	id := canonical.Digest(raw)
	hexID := strings.TrimPrefix(id, "sha256:")
	path := filepath.Join(st.Dir(), "objects", "sha256", hexID[:2], hexID[2:]+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o444); err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), st, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	verification := result.Objects[id]
	if !result.Verification.DiagnosticsTruncated || len(verification.Errors) != 1 {
		t.Fatalf("verification = %#v, diagnostics_truncated = %t", verification, result.Verification.DiagnosticsTruncated)
	}
	message := verification.Errors[0]
	if len(message) > 512 || !utf8.ValidString(message) || strings.ContainsAny(message, "\n\x1b") {
		t.Fatalf("canonical diagnostic = %q", message)
	}
}

func TestDiagnosticCollectorBoundsAtAppendAndPreservesUTF8(t *testing.T) {
	result := VerifyResult{diagnosticLimits: Limits{DiagnosticSamples: 1, DiagnosticTextBytes: 3}}
	appendVerificationDiagnostic(&result, &result.Errors, "éé")
	appendVerificationDiagnostic(&result, &result.Errors, "discarded")
	if !result.DiagnosticsTruncated || !reflect.DeepEqual(result.Errors, []string{"é"}) {
		t.Fatalf("bounded diagnostics = %#v", result.Errors)
	}

	exact := VerifyResult{diagnosticLimits: Limits{DiagnosticSamples: 1, DiagnosticTextBytes: 4}}
	appendVerificationDiagnostic(&exact, &exact.Errors, "éé")
	if exact.DiagnosticsTruncated || !reflect.DeepEqual(exact.Errors, []string{"éé"}) {
		t.Fatalf("exact UTF-8 diagnostic = %#v, truncated %t", exact.Errors, exact.DiagnosticsTruncated)
	}

	firstByte := VerifyResult{diagnosticLimits: Limits{DiagnosticSamples: 1, DiagnosticTextBytes: 1}}
	appendVerificationDiagnostic(&firstByte, &firstByte.Errors, "é")
	if !firstByte.DiagnosticsTruncated || !reflect.DeepEqual(firstByte.Errors, []string{""}) {
		t.Fatalf("first-byte-over diagnostic = %#v", firstByte.Errors)
	}
}

func TestVerifyCountsObjectsCanonicalOnce(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commitOne(t, st, key, "one", nil)
	result, err := Verify(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts.Objects != 1 {
		t.Fatalf("Verify() counts.objects = %d, want 1", result.Counts.Objects)
	}
}

func TestVerifyPublishesCompletenessAndLimitsFromScan(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	batch := mustBatch(t, "source")
	batch.Events[0].Supersedes = []string{EventRef("sha256:"+strings.Repeat("e", 64), "gone")}
	if _, err := Commit(st, key, batch, CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	result, err := Verify(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Completeness.Status != "incomplete" || result.Limits != (LimitsStatus{Profile: LimitsProfile, Status: "within_limits"}) {
		t.Fatalf("Verify() = %#v", result)
	}
}

func TestResolveCommitUsesBoundedDirectCanonicalLookup(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	commit := commitOne(t, st, key, "one", nil)
	record, err := ResolveCommit(context.Background(), st, commit.ObjectID, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != commit.ObjectID || !equalStrings(record.EventRefs, commit.EventRefs) {
		t.Fatalf("ResolveCommit() = %#v", record)
	}
	_, err = ResolveCommit(context.Background(), st, "sha256:"+strings.Repeat("f", 64), Limits{})
	if !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("ResolveCommit() missing error = %v", err)
	}
}

func TestResolveCommitReportsTheLimitingEventResource(t *testing.T) {
	st, key := ledgerStoreAndKey(t)
	batch, err := NormalizeEventBatch(map[string]any{"events": []any{eventInput("a", []any{}, []any{}), eventInput("b", []any{}, []any{})}})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := Commit(st, key, batch, CommitOptions{ObservedAt: "2026-08-23T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveCommit(context.Background(), st, commit.ObjectID, Limits{Events: 1})
	assertLimitError(t, err, "events", 1)
}

func TestShowDirectLookupRejectsOversizeCanonicalObject(t *testing.T) {
	st, _ := ledgerStoreAndKey(t)
	id, _, err := st.PutCanonical(map[string]any{"oversize": strings.Repeat("x", int(Phase2Limits.ObjectBytes))})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Show(st, id)
	assertLimitError(t, err, "object_bytes", Phase2Limits.ObjectBytes)
}

func blockerKey(blocker Blocker) string {
	return blocker.Code + "\x00" + blocker.SourceID + "\x00" + blocker.Field + "\x00" + blocker.MissingRef
}
