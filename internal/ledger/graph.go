// ABOUTME: Builds and analyzes the bounded known causal graph without recursion.
// ABOUTME: Assigns deterministic whole-frontier batches and propagates unresolved state.
package ledger

import (
	"context"
	"fmt"
	"sort"
)

type graphAnalysis struct {
	Batches    map[string]uint64
	Unresolved []string
	Errors     []string
}

type graphNode struct {
	kind     uint8
	id       string
	outgoing []graphEdge
	indegree uint64
	depth    uint64
}

type graphEdge struct {
	target string
	weight uint64
}

const (
	startNode uint8 = iota
	eventNode
	finishNode
)

func analyzeGraph(ctx context.Context, commits map[string]CommitRecord, events map[string]EventRecord, limits Limits) (graphAnalysis, error) {
	if ctx == nil {
		return graphAnalysis{}, fmt.Errorf("context is required")
	}
	if err := ctx.Err(); err != nil {
		return graphAnalysis{}, err
	}
	limits = effectiveLimits(limits)
	result := graphAnalysis{Batches: map[string]uint64{}, Unresolved: []string{}, Errors: []string{}}
	builder := newGraphBuilder(commits, events, limits, &result)
	if err := builder.addCommits(ctx, commits, events); err != nil {
		return graphAnalysis{}, err
	}
	if err := builder.addEvents(commits, events); err != nil {
		return graphAnalysis{}, err
	}
	propagateUnresolved(builder.nodes, builder.unresolved)
	batch, err := kahnBatches(ctx, builder.nodes, builder.unresolved, limits, &result)
	if err != nil {
		return graphAnalysis{}, err
	}
	if batch != uint64(len(builder.nodes)) {
		result.Errors = append(result.Errors, "known causal graph contains a cycle")
	}
	for key, marked := range builder.unresolved {
		if !marked {
			continue
		}
		node := builder.nodes[key]
		if node != nil && node.kind == eventNode {
			result.Unresolved = append(result.Unresolved, node.id)
			delete(result.Batches, node.id)
		}
	}
	sort.Strings(result.Unresolved)
	result.Errors = uniqueSorted(result.Errors)
	return result, nil
}

type graphBuilder struct {
	nodes      map[string]*graphNode
	unresolved map[string]bool
	edges      ScanCounts
	limits     Limits
	result     *graphAnalysis
}

func newGraphBuilder(commits map[string]CommitRecord, events map[string]EventRecord, limits Limits, result *graphAnalysis) *graphBuilder {
	builder := &graphBuilder{nodes: make(map[string]*graphNode, 2*len(commits)+len(events)), unresolved: make(map[string]bool), limits: limits, result: result}
	for _, id := range sortedKeys(commits) {
		builder.nodes[graphNodeKey(startNode, id)] = &graphNode{kind: startNode, id: id}
		builder.nodes[graphNodeKey(finishNode, id)] = &graphNode{kind: finishNode, id: id}
	}
	for _, ref := range sortedKeys(events) {
		builder.nodes[graphNodeKey(eventNode, ref)] = &graphNode{kind: eventNode, id: ref}
	}
	return builder
}

func (builder *graphBuilder) addEdge(source, target string, weight uint64) {
	from, fromFound := builder.nodes[source]
	to, toFound := builder.nodes[target]
	if !fromFound || !toFound {
		return
	}
	from.outgoing = append(from.outgoing, graphEdge{target: target, weight: weight})
	to.indegree++
}

func (builder *graphBuilder) addCommits(ctx context.Context, commits map[string]CommitRecord, events map[string]EventRecord) error {
	for _, id := range sortedKeys(commits) {
		if err := ctx.Err(); err != nil {
			return err
		}
		commit := commits[id]
		if err := addGraphEdges(&builder.edges, builder.limits, uint64(len(commit.Parents))+2*uint64(len(commit.EventRefs))); err != nil {
			return err
		}
		builder.addCommitEvents(id, commit, events)
		builder.addCommitParents(id, commit, commits)
	}
	return nil
}

func (builder *graphBuilder) addCommitEvents(id string, commit CommitRecord, events map[string]EventRecord) {
	start := graphNodeKey(startNode, id)
	finish := graphNodeKey(finishNode, id)
	for _, ref := range commit.EventRefs {
		if _, found := events[ref]; !found {
			builder.result.Errors = append(builder.result.Errors, fmt.Sprintf("%s: canonical event record is unavailable", ref))
			continue
		}
		event := graphNodeKey(eventNode, ref)
		builder.addEdge(start, event, 0)
		builder.addEdge(event, finish, 0)
	}
}

func (builder *graphBuilder) addCommitParents(id string, commit CommitRecord, commits map[string]CommitRecord) {
	start := graphNodeKey(startNode, id)
	for _, parent := range commit.Parents {
		if _, found := commits[parent]; !found {
			builder.unresolved[start] = true
			continue
		}
		builder.addEdge(graphNodeKey(finishNode, parent), start, 1)
	}
}

func (builder *graphBuilder) addEvents(commits map[string]CommitRecord, events map[string]EventRecord) error {
	for _, ref := range sortedKeys(events) {
		event := events[ref]
		if err := addGraphEdges(&builder.edges, builder.limits, uint64(len(event.CausedBy))+uint64(len(event.Supersedes))); err != nil {
			return err
		}
		if _, found := commits[event.CommitID]; !found {
			builder.result.Errors = append(builder.result.Errors, fmt.Sprintf("%s: owning commit %s is unavailable", ref, event.CommitID))
		}
		builder.addEventDependencies(ref, event, events)
	}
	return nil
}

func (builder *graphBuilder) addEventDependencies(ref string, event EventRecord, events map[string]EventRecord) {
	source := graphNodeKey(eventNode, ref)
	for _, dependency := range event.CausedBy {
		target, found := events[dependency]
		if !found {
			builder.unresolved[source] = true
			continue
		}
		if target.Namespace != event.Namespace {
			builder.result.Errors = append(builder.result.Errors, fmt.Sprintf("%s: caused_by %s crosses namespace %q to %q", ref, dependency, target.Namespace, event.Namespace))
		}
		builder.addEdge(graphNodeKey(eventNode, dependency), source, 1)
	}
}

func propagateUnresolved(nodes map[string]*graphNode, unresolved map[string]bool) {
	queue := make([]string, 0, len(unresolved))
	for key := range unresolved {
		queue = append(queue, key)
	}
	sort.Strings(queue)
	for index := 0; index < len(queue); index++ {
		key := queue[index]
		for _, edge := range nodes[key].outgoing {
			if !unresolved[edge.target] {
				unresolved[edge.target] = true
				queue = append(queue, edge.target)
			}
		}
	}
}

func kahnBatches(ctx context.Context, nodes map[string]*graphNode, unresolved map[string]bool, limits Limits, result *graphAnalysis) (uint64, error) {
	frontier := make([]string, 0)
	for key, node := range nodes {
		if node.indegree == 0 {
			frontier = append(frontier, key)
		}
	}
	processed := uint64(0)
	round := uint64(0)
	for len(frontier) != 0 {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if uint64(len(frontier)) > limits.FrontierNodes {
			return 0, limitError("frontier_nodes", limits.FrontierNodes)
		}
		sort.Slice(frontier, func(i, j int) bool { return graphNodeLess(nodes[frontier[i]], nodes[frontier[j]]) })
		next := make([]string, 0)
		for _, key := range frontier {
			node := nodes[key]
			processed++
			if node.kind == eventNode && !unresolved[key] {
				result.Batches[node.id] = round
			}
			for _, edge := range node.outgoing {
				target := nodes[edge.target]
				candidate := node.depth + edge.weight
				if candidate > limits.CausalDepth {
					return 0, limitError("causal_depth", limits.CausalDepth)
				}
				if candidate > target.depth {
					target.depth = candidate
				}
				target.indegree--
				if target.indegree == 0 {
					next = append(next, edge.target)
				}
			}
		}
		frontier = next
		round++
	}
	return processed, nil
}

func graphNodeKey(kind uint8, id string) string { return fmt.Sprintf("%d:%s", kind, id) }

func graphNodeLess(left, right *graphNode) bool {
	if left.kind != right.kind {
		return left.kind < right.kind
	}
	return left.id < right.id
}
