// ABOUTME: Tests deterministic iterative causal scheduling over typed ledger records.
// ABOUTME: Covers synthetic gates, unresolved propagation, cycles, depth, width, and edge bounds.
package ledger

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

func TestCausalGraphUsesSyntheticGatesParentsAndCausedByButNotSupersedes(t *testing.T) {
	commits := map[string]CommitRecord{
		"a": {ID: "a", Namespace: "scope", EventRefs: []string{"event:a"}},
		"b": {ID: "b", Namespace: "scope", Parents: []string{"a"}, EventRefs: []string{"event:b"}},
		"c": {ID: "c", Namespace: "scope", EventRefs: []string{"event:c"}},
	}
	events := map[string]EventRecord{
		"event:a": {Ref: "event:a", CommitID: "a", Namespace: "scope"},
		"event:b": {Ref: "event:b", CommitID: "b", Namespace: "scope", CausedBy: []string{"event:a"}},
		"event:c": {Ref: "event:c", CommitID: "c", Namespace: "scope", Supersedes: []string{"event:b"}},
	}
	result, err := analyzeGraph(context.Background(), commits, events, Phase2Limits)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]uint64{"event:a": 1, "event:b": 4, "event:c": 1}
	if !reflect.DeepEqual(result.Batches, want) || len(result.Unresolved) != 0 || len(result.Errors) != 0 {
		t.Fatalf("analyzeGraph() = %#v, want batches %#v", result, want)
	}
}

func TestCausalGraphPropagatesMissingParentsAndCausedByButNotSupersedes(t *testing.T) {
	commits := map[string]CommitRecord{
		"a": {ID: "a", Namespace: "scope", Parents: []string{"missing"}, EventRefs: []string{"event:a"}},
		"b": {ID: "b", Namespace: "scope", Parents: []string{"a"}, EventRefs: []string{"event:b"}},
		"c": {ID: "c", Namespace: "scope", EventRefs: []string{"event:c"}},
		"d": {ID: "d", Namespace: "scope", EventRefs: []string{"event:d"}},
		"e": {ID: "e", Namespace: "scope", EventRefs: []string{"event:e"}},
	}
	events := map[string]EventRecord{
		"event:a": {Ref: "event:a", CommitID: "a", Namespace: "scope"},
		"event:b": {Ref: "event:b", CommitID: "b", Namespace: "scope"},
		"event:c": {Ref: "event:c", CommitID: "c", Namespace: "scope", CausedBy: []string{"event:missing"}},
		"event:d": {Ref: "event:d", CommitID: "d", Namespace: "scope", CausedBy: []string{"event:c"}},
		"event:e": {Ref: "event:e", CommitID: "e", Namespace: "scope", Supersedes: []string{"event:missing"}},
	}
	result, err := analyzeGraph(context.Background(), commits, events, Phase2Limits)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Unresolved, []string{"event:a", "event:b", "event:c", "event:d"}) {
		t.Fatalf("unresolved = %#v", result.Unresolved)
	}
	if _, found := result.Batches["event:e"]; !found {
		t.Fatalf("missing supersedes target moved ordered event: %#v", result)
	}
}

func TestCausalGraphProcessesWholeSortedFrontierDeterministically(t *testing.T) {
	commits := map[string]CommitRecord{
		"z": {ID: "z", Namespace: "scope", EventRefs: []string{"event:z"}},
		"a": {ID: "a", Namespace: "scope", EventRefs: []string{"event:a"}},
	}
	events := map[string]EventRecord{
		"event:z": {Ref: "event:z", CommitID: "z", Namespace: "scope"},
		"event:a": {Ref: "event:a", CommitID: "a", Namespace: "scope"},
	}
	for range 20 {
		result, err := analyzeGraph(context.Background(), commits, events, Phase2Limits)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Batches, map[string]uint64{"event:a": 1, "event:z": 1}) {
			t.Fatalf("batches = %#v", result.Batches)
		}
	}
}

func TestCausalGraphReportsParentAndCausedByCyclesAndCrossNamespaceEdges(t *testing.T) {
	commits := map[string]CommitRecord{
		"a": {ID: "a", Namespace: "one", Parents: []string{"b"}, EventRefs: []string{"event:a"}},
		"b": {ID: "b", Namespace: "two", Parents: []string{"a"}, EventRefs: []string{"event:b"}},
	}
	events := map[string]EventRecord{
		"event:a": {Ref: "event:a", CommitID: "a", Namespace: "one", CausedBy: []string{"event:b"}},
		"event:b": {Ref: "event:b", CommitID: "b", Namespace: "two", CausedBy: []string{"event:a"}},
	}
	result, err := analyzeGraph(context.Background(), commits, events, Phase2Limits)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) < 3 {
		t.Fatalf("graph errors = %#v, want cycles and cross-namespace failures", result.Errors)
	}
	wantParent := "commit DAG cycle: a -> b -> a"
	wantCausedBy := "caused_by cycle: event:a -> event:b -> event:a"
	if !slices.Contains(result.Errors, wantParent) || !slices.Contains(result.Errors, wantCausedBy) {
		t.Fatalf("graph errors = %#v, want %q and %q", result.Errors, wantParent, wantCausedBy)
	}
}

func TestCausalDepthAllows4096AndRejects4097SignedEdges(t *testing.T) {
	limits := Phase2Limits
	limits.FrontierNodes = 10_000
	for _, test := range []struct {
		depth   int
		wantErr bool
	}{
		{depth: 4_096},
		{depth: 4_097, wantErr: true},
	} {
		t.Run(fmt.Sprint(test.depth), func(t *testing.T) {
			commits, events := causedByChain(test.depth)
			_, err := analyzeGraph(context.Background(), commits, events, limits)
			if test.wantErr {
				assertLimitError(t, err, "causal_depth", 4_096)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGraphFrontierAllows4096AndRejects4097Nodes(t *testing.T) {
	for _, count := range []int{4_096, 4_097} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			commits := make(map[string]CommitRecord, count)
			events := make(map[string]EventRecord, count)
			for index := range count {
				id := fmt.Sprintf("c%05d", index)
				ref := "event:" + id
				commits[id] = CommitRecord{ID: id, Namespace: "scope", EventRefs: []string{ref}}
				events[ref] = EventRecord{Ref: ref, CommitID: id, Namespace: "scope"}
			}
			_, err := analyzeGraph(context.Background(), commits, events, Phase2Limits)
			if count == 4_097 {
				assertLimitError(t, err, "frontier_nodes", 4_096)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGraphEdgeCounterAllowsMillionAndRejectsFirstExcess(t *testing.T) {
	counts := edgeCategoryCounts{parents: 200_000, eventGates: 300_000, causedBy: 100_000, supersedes: 100_000, checkpointHeads: 200_000, previousCheckpoints: 100_000}
	if err := counts.validate(Phase2Limits, "object"); err != nil {
		t.Fatal(err)
	}
	if counts.total() != 1_000_000 {
		t.Fatalf("edges = %d", counts.total())
	}
	counts.previousCheckpoints++
	assertLimitError(t, counts.validate(Phase2Limits, "object"), "graph_edges", 1_000_000)
}

func TestCausalGraphHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := analyzeGraph(ctx, nil, nil, Phase2Limits)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("analyzeGraph() error = %v", err)
	}
}

func TestCausalGraphHonorsCancellationDuringInnerWork(t *testing.T) {
	commits, events := causedByChain(300)
	ctx, cancel := context.WithCancel(context.Background())
	polls := 0
	original := afterLedgerWorkPoll
	afterLedgerWorkPoll = func() {
		polls++
		if polls == 4 {
			cancel()
		}
	}
	t.Cleanup(func() { afterLedgerWorkPoll = original })
	_, err := analyzeGraph(ctx, commits, events, Phase2Limits)
	if !errors.Is(err, context.Canceled) || polls < 4 {
		t.Fatalf("analyzeGraph() = %v after %d polls, want mid-work cancellation", err, polls)
	}
}

func TestUnresolvedPropagationHonorsCancellationWithinLargeOutgoingSet(t *testing.T) {
	nodes := map[string]*graphNode{"source": {id: "source"}}
	for index := range 300 {
		key := fmt.Sprintf("target:%03d", index)
		nodes[key] = &graphNode{id: key}
		nodes["source"].outgoing = append(nodes["source"].outgoing, graphEdge{target: key})
	}
	ctx, cancel := context.WithCancel(context.Background())
	polls := 0
	original := afterLedgerWorkPoll
	afterLedgerWorkPoll = func() {
		polls++
		if polls == 2 {
			cancel()
		}
	}
	t.Cleanup(func() { afterLedgerWorkPoll = original })
	unresolved := map[string]bool{"source": true}
	err := propagateUnresolved(ctx, nodes, unresolved)
	if !errors.Is(err, context.Canceled) || polls < 2 || len(unresolved) == len(nodes) {
		t.Fatalf("propagateUnresolved() = %v after %d polls and %d marks, want mid-edge cancellation", err, polls, len(unresolved))
	}
}

func TestCausalGraphReportsEveryStableParentCycleWitness(t *testing.T) {
	commits := map[string]CommitRecord{
		"a": {ID: "a", Parents: []string{"b"}},
		"b": {ID: "b", Parents: []string{"a"}},
		"c": {ID: "c", Parents: []string{"d"}},
		"d": {ID: "d", Parents: []string{"c"}},
	}
	result, err := analyzeGraph(context.Background(), commits, nil, Phase2Limits)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"commit DAG cycle: a -> b -> a", "commit DAG cycle: c -> d -> c"}
	if !reflect.DeepEqual(result.Errors, want) || result.ErrorCount != 2 {
		t.Fatalf("cycle diagnostics = %#v, count %d", result.Errors, result.ErrorCount)
	}
}

func causedByChain(depth int) (map[string]CommitRecord, map[string]EventRecord) {
	refs := make([]string, depth+1)
	events := make(map[string]EventRecord, depth+1)
	for index := range refs {
		refs[index] = fmt.Sprintf("event:%05d", index)
		event := EventRecord{Ref: refs[index], CommitID: "commit", Namespace: "scope"}
		if index > 0 {
			event.CausedBy = []string{refs[index-1]}
		}
		events[refs[index]] = event
	}
	commits := map[string]CommitRecord{"commit": {ID: "commit", Namespace: "scope", EventRefs: refs}}
	return commits, events
}
