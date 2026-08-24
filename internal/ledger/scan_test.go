// ABOUTME: Tests the bounded canonical ledger scan and source fingerprint contract.
// ABOUTME: Covers completeness blockers, resource limits, cancellation, and safe diagnostics.
package ledger

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
)

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
