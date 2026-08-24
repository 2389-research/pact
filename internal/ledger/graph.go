// ABOUTME: Builds and analyzes the bounded known causal graph without recursion.
// ABOUTME: Assigns deterministic whole-frontier batches and propagates unresolved state.
package ledger

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type graphAnalysis struct {
	Batches              map[string]uint64
	Unresolved           []string
	Errors               []string
	ErrorCount           uint64
	DiagnosticsTruncated bool
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
	builder, err := newGraphBuilder(ctx, commits, events, limits, &result)
	if err != nil {
		return graphAnalysis{}, err
	}
	if err := builder.addCommits(ctx, commits, events); err != nil {
		return graphAnalysis{}, err
	}
	if err := builder.addEvents(ctx, commits, events); err != nil {
		return graphAnalysis{}, err
	}
	if err := propagateUnresolved(ctx, builder.nodes, builder.unresolved); err != nil {
		return graphAnalysis{}, err
	}
	commitGraph, err := commitDependencyGraph(ctx, commits)
	if err != nil {
		return graphAnalysis{}, err
	}
	cycles, err := deterministicCycles(ctx, commitGraph)
	if err != nil {
		return graphAnalysis{}, err
	}
	cycleReported := false
	for _, cycle := range cycles {
		appendGraphError(&result, limits, "commit DAG cycle: "+joinCycle(cycle))
		cycleReported = true
	}
	eventGraph, err := eventDependencyGraph(ctx, events)
	if err != nil {
		return graphAnalysis{}, err
	}
	cycles, err = deterministicCycles(ctx, eventGraph)
	if err != nil {
		return graphAnalysis{}, err
	}
	for _, cycle := range cycles {
		appendGraphError(&result, limits, "caused_by cycle: "+joinCycle(cycle))
		cycleReported = true
	}
	batch, err := kahnBatches(ctx, builder.nodes, builder.unresolved, limits, &result)
	if err != nil {
		return graphAnalysis{}, err
	}
	if batch != uint64(len(builder.nodes)) && !cycleReported {
		appendGraphError(&result, limits, "known causal graph contains a cycle")
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
	limits     Limits
	result     *graphAnalysis
}

func newGraphBuilder(ctx context.Context, commits map[string]CommitRecord, events map[string]EventRecord, limits Limits, result *graphAnalysis) (*graphBuilder, error) {
	builder := &graphBuilder{nodes: make(map[string]*graphNode, 2*len(commits)+len(events)), unresolved: make(map[string]bool), limits: limits, result: result}
	work := 0
	for _, id := range sortedKeys(commits) {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		work++
		builder.nodes[graphNodeKey(startNode, id)] = &graphNode{kind: startNode, id: id}
		builder.nodes[graphNodeKey(finishNode, id)] = &graphNode{kind: finishNode, id: id}
	}
	for _, ref := range sortedKeys(events) {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		work++
		builder.nodes[graphNodeKey(eventNode, ref)] = &graphNode{kind: eventNode, id: ref}
	}
	return builder, nil
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
	for index, id := range sortedKeys(commits) {
		if err := pollContext(ctx, index); err != nil {
			return err
		}
		commit := commits[id]
		if err := builder.addCommitEvents(ctx, id, commit, events); err != nil {
			return err
		}
		if err := builder.addCommitParents(ctx, id, commit, commits); err != nil {
			return err
		}
	}
	return nil
}

func (builder *graphBuilder) addCommitEvents(ctx context.Context, id string, commit CommitRecord, events map[string]EventRecord) error {
	start := graphNodeKey(startNode, id)
	finish := graphNodeKey(finishNode, id)
	for index, ref := range commit.EventRefs {
		if err := pollContext(ctx, index); err != nil {
			return err
		}
		if _, found := events[ref]; !found {
			appendGraphError(builder.result, builder.limits, fmt.Sprintf("%s: canonical event record is unavailable", ref))
			continue
		}
		event := graphNodeKey(eventNode, ref)
		builder.addEdge(start, event, 0)
		builder.addEdge(event, finish, 0)
	}
	return nil
}

func (builder *graphBuilder) addCommitParents(ctx context.Context, id string, commit CommitRecord, commits map[string]CommitRecord) error {
	start := graphNodeKey(startNode, id)
	for index, parent := range commit.Parents {
		if err := pollContext(ctx, index); err != nil {
			return err
		}
		if _, found := commits[parent]; !found {
			builder.unresolved[start] = true
			continue
		}
		builder.addEdge(graphNodeKey(finishNode, parent), start, 1)
	}
	return nil
}

func (builder *graphBuilder) addEvents(ctx context.Context, commits map[string]CommitRecord, events map[string]EventRecord) error {
	for index, ref := range sortedKeys(events) {
		if err := pollContext(ctx, index); err != nil {
			return err
		}
		event := events[ref]
		if _, found := commits[event.CommitID]; !found {
			appendGraphError(builder.result, builder.limits, fmt.Sprintf("%s: owning commit %s is unavailable", ref, event.CommitID))
		}
		if err := builder.addEventDependencies(ctx, ref, event, events); err != nil {
			return err
		}
	}
	return nil
}

func (builder *graphBuilder) addEventDependencies(ctx context.Context, ref string, event EventRecord, events map[string]EventRecord) error {
	source := graphNodeKey(eventNode, ref)
	for index, dependency := range event.CausedBy {
		if err := pollContext(ctx, index); err != nil {
			return err
		}
		target, found := events[dependency]
		if !found {
			builder.unresolved[source] = true
			continue
		}
		if target.Namespace != event.Namespace {
			appendGraphError(builder.result, builder.limits, fmt.Sprintf("%s: caused_by %s crosses namespace %q to %q", ref, dependency, target.Namespace, event.Namespace))
		}
		builder.addEdge(graphNodeKey(eventNode, dependency), source, 1)
	}
	return nil
}

func propagateUnresolved(ctx context.Context, nodes map[string]*graphNode, unresolved map[string]bool) error {
	queue := make([]string, 0, len(unresolved))
	for key := range unresolved {
		queue = append(queue, key)
	}
	sort.Strings(queue)
	work := 0
	for index := 0; index < len(queue); index++ {
		if err := pollContext(ctx, index); err != nil {
			return err
		}
		key := queue[index]
		for _, edge := range nodes[key].outgoing {
			if err := pollContext(ctx, work); err != nil {
				return err
			}
			work++
			if !unresolved[edge.target] {
				unresolved[edge.target] = true
				queue = append(queue, edge.target)
			}
		}
	}
	return nil
}

func kahnBatches(ctx context.Context, nodes map[string]*graphNode, unresolved map[string]bool, limits Limits, result *graphAnalysis) (uint64, error) {
	frontier := make([]string, 0)
	initialIndex := 0
	for key, node := range nodes {
		if err := pollContext(ctx, initialIndex); err != nil {
			return 0, err
		}
		initialIndex++
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
		for frontierIndex, key := range frontier {
			if err := pollContext(ctx, frontierIndex); err != nil {
				return 0, err
			}
			node := nodes[key]
			processed++
			if err := processGraphNode(ctx, key, node, nodes, unresolved, limits, round, result, &next); err != nil {
				return 0, err
			}
		}
		frontier = next
		round++
	}
	return processed, nil
}

func processGraphNode(ctx context.Context, key string, node *graphNode, nodes map[string]*graphNode, unresolved map[string]bool, limits Limits, round uint64, result *graphAnalysis, next *[]string) error {
	if node.kind == eventNode && !unresolved[key] {
		result.Batches[node.id] = round
	}
	for edgeIndex, edge := range node.outgoing {
		if err := pollContext(ctx, edgeIndex); err != nil {
			return err
		}
		target := nodes[edge.target]
		candidate := node.depth + edge.weight
		if candidate > limits.CausalDepth {
			return limitError("causal_depth", limits.CausalDepth)
		}
		if candidate > target.depth {
			target.depth = candidate
		}
		target.indegree--
		if target.indegree == 0 {
			*next = append(*next, edge.target)
		}
	}
	return nil
}

func commitDependencyGraph(ctx context.Context, commits map[string]CommitRecord) (map[string][]string, error) {
	result := make(map[string][]string, len(commits))
	work := 0
	for id, commit := range commits {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		work++
		for _, parent := range commit.Parents {
			if err := pollContext(ctx, work); err != nil {
				return nil, err
			}
			work++
			if _, found := commits[parent]; found {
				result[id] = append(result[id], parent)
			}
		}
		if result[id] == nil {
			result[id] = []string{}
		}
	}
	return result, nil
}

func eventDependencyGraph(ctx context.Context, events map[string]EventRecord) (map[string][]string, error) {
	result := make(map[string][]string, len(events))
	work := 0
	for ref, event := range events {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		work++
		for _, dependency := range event.CausedBy {
			if err := pollContext(ctx, work); err != nil {
				return nil, err
			}
			work++
			if _, found := events[dependency]; found {
				result[ref] = append(result[ref], dependency)
			}
		}
		if result[ref] == nil {
			result[ref] = []string{}
		}
	}
	return result, nil
}

type cycleFrame struct {
	node      string
	neighbors []string
	next      int
}

func deterministicCycles(ctx context.Context, graph map[string][]string) ([][]string, error) {
	color := make(map[string]uint8, len(graph))
	cycles := [][]string{}
	work := 0
	for _, root := range sortedKeys(graph) {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		work++
		if color[root] != 0 {
			continue
		}
		neighbors := append([]string(nil), graph[root]...)
		sort.Strings(neighbors)
		stack := []cycleFrame{{node: root, neighbors: neighbors}}
		path := []string{root}
		positions := map[string]int{root: 0}
		color[root] = 1
		for len(stack) != 0 {
			if err := pollContext(ctx, work); err != nil {
				return nil, err
			}
			work++
			frame := &stack[len(stack)-1]
			if frame.next == len(frame.neighbors) {
				color[frame.node] = 2
				delete(positions, frame.node)
				stack = stack[:len(stack)-1]
				path = path[:len(path)-1]
				continue
			}
			next := frame.neighbors[frame.next]
			frame.next++
			switch color[next] {
			case 0:
				childNeighbors := append([]string(nil), graph[next]...)
				sort.Strings(childNeighbors)
				positions[next] = len(path)
				path = append(path, next)
				color[next] = 1
				stack = append(stack, cycleFrame{node: next, neighbors: childNeighbors})
			case 1:
				start := positions[next]
				cycle := append([]string(nil), path[start:]...)
				cycles = append(cycles, append(cycle, next))
			}
		}
	}
	return cycles, nil
}

func joinCycle(cycle []string) string {
	return strings.Join(cycle, " -> ")
}

func appendGraphError(result *graphAnalysis, limits Limits, message string) {
	result.ErrorCount++
	if uint64(len(result.Errors)) >= limits.DiagnosticSamples {
		result.DiagnosticsTruncated = true
		return
	}
	if uint64(len(message)) > limits.DiagnosticTextBytes {
		// #nosec G115 -- the effective diagnostic limit is capped at 512 bytes.
		cut := int(limits.DiagnosticTextBytes)
		for cut > 0 && !utf8.ValidString(message[:cut]) {
			cut--
		}
		message = message[:cut]
		result.DiagnosticsTruncated = true
	}
	result.Errors = append(result.Errors, message)
}

func graphNodeKey(kind uint8, id string) string { return fmt.Sprintf("%d:%s", kind, id) }

func graphNodeLess(left, right *graphNode) bool {
	if left.kind != right.kind {
		return left.kind < right.kind
	}
	return left.id < right.id
}
