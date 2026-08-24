// ABOUTME: Builds and analyzes the bounded known causal graph without recursion.
// ABOUTME: Assigns deterministic whole-frontier batches and propagates unresolved state.
package ledger

import (
	"context"
	"fmt"
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
	incoming []graphEdge
	indegree uint64
	depth    uint64
}

type graphEdge struct {
	target string
	weight uint64
	kind   uint8
}

const (
	startNode uint8 = iota
	eventNode
	finishNode
)

const (
	gateEdge uint8 = iota
	parentEdge
	causedByEdge
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
	parentRoots, err := cycleRoots(ctx, builder.nodes, startNode)
	if err != nil {
		return graphAnalysis{}, err
	}
	parentCycles, err := reportCycles(ctx, builder.nodes, parentRoots, parentEdge, "commit DAG cycle: ", &result, limits)
	if err != nil {
		return graphAnalysis{}, err
	}
	eventRoots, err := cycleRoots(ctx, builder.nodes, eventNode)
	if err != nil {
		return graphAnalysis{}, err
	}
	eventCycles, err := reportCycles(ctx, builder.nodes, eventRoots, causedByEdge, "caused_by cycle: ", &result, limits)
	if err != nil {
		return graphAnalysis{}, err
	}
	cycleReported := parentCycles+eventCycles != 0
	batch, err := kahnBatches(ctx, builder.nodes, builder.unresolved, limits, &result)
	if err != nil {
		return graphAnalysis{}, err
	}
	if batch != uint64(len(builder.nodes)) && !cycleReported {
		appendGraphError(&result, limits, "known causal graph contains a cycle")
	}
	unresolvedKeys, err := sortedKeysContext(ctx, builder.unresolved)
	if err != nil {
		return graphAnalysis{}, err
	}
	for index, key := range unresolvedKeys {
		if err := pollContext(ctx, index); err != nil {
			return graphAnalysis{}, err
		}
		marked := builder.unresolved[key]
		if !marked {
			continue
		}
		node := builder.nodes[key]
		if node != nil && node.kind == eventNode {
			result.Unresolved = append(result.Unresolved, node.id)
			delete(result.Batches, node.id)
		}
	}
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
	commitIDs, err := sortedKeysContext(ctx, commits)
	if err != nil {
		return nil, err
	}
	for _, id := range commitIDs {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		work++
		builder.nodes[graphNodeKey(startNode, id)] = &graphNode{kind: startNode, id: id}
		builder.nodes[graphNodeKey(finishNode, id)] = &graphNode{kind: finishNode, id: id}
	}
	eventRefs, err := sortedKeysContext(ctx, events)
	if err != nil {
		return nil, err
	}
	for _, ref := range eventRefs {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		work++
		builder.nodes[graphNodeKey(eventNode, ref)] = &graphNode{kind: eventNode, id: ref}
	}
	return builder, nil
}

func (builder *graphBuilder) addEdge(source, target string, weight uint64, kind uint8) {
	from, fromFound := builder.nodes[source]
	to, toFound := builder.nodes[target]
	if !fromFound || !toFound {
		return
	}
	from.outgoing = append(from.outgoing, graphEdge{target: target, weight: weight, kind: kind})
	to.incoming = append(to.incoming, graphEdge{target: source, weight: weight, kind: kind})
	to.indegree++
}

func (builder *graphBuilder) addCommits(ctx context.Context, commits map[string]CommitRecord, events map[string]EventRecord) error {
	ids, err := sortedKeysContext(ctx, commits)
	if err != nil {
		return err
	}
	for index, id := range ids {
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
		builder.addEdge(start, event, 0, gateEdge)
		builder.addEdge(event, finish, 0, gateEdge)
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
		builder.addEdge(graphNodeKey(finishNode, parent), start, 1, parentEdge)
	}
	return nil
}

func (builder *graphBuilder) addEvents(ctx context.Context, commits map[string]CommitRecord, events map[string]EventRecord) error {
	refs, err := sortedKeysContext(ctx, events)
	if err != nil {
		return err
	}
	for index, ref := range refs {
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
			appendBoundedGraphError(builder.result, builder.limits, ref, ": caused_by ", dependency, " crosses namespace \"", target.Namespace, "\" to \"", event.Namespace, "\"")
		}
		builder.addEdge(graphNodeKey(eventNode, dependency), source, 1, causedByEdge)
	}
	return nil
}

func propagateUnresolved(ctx context.Context, nodes map[string]*graphNode, unresolved map[string]bool) error {
	queue, err := sortedKeysContext(ctx, unresolved)
	if err != nil {
		return err
	}
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
		var err error
		frontier, err = sortOwnedStringsByContext(ctx, frontier, func(left, right string) bool { return graphNodeLess(nodes[left], nodes[right]) })
		if err != nil {
			return 0, err
		}
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

type cycleFrame struct {
	node      string
	neighbors []string
	next      int
}

func cycleRoots(ctx context.Context, nodes map[string]*graphNode, kind uint8) ([]string, error) {
	keys, err := sortedKeysContext(ctx, nodes)
	if err != nil {
		return nil, err
	}
	roots := make([]string, 0)
	for index, key := range keys {
		if err := pollContext(ctx, index); err != nil {
			return nil, err
		}
		if nodes[key].kind == kind {
			roots = append(roots, key)
		}
	}
	return roots, nil
}

func reportCycles(ctx context.Context, nodes map[string]*graphNode, roots []string, edgeKind uint8, prefix string, result *graphAnalysis, limits Limits) (uint64, error) {
	color := make(map[string]uint8, len(roots))
	var count uint64
	work := 0
	for _, root := range roots {
		if err := pollContext(ctx, work); err != nil {
			return 0, err
		}
		work++
		if color[root] != 0 {
			continue
		}
		neighbors, err := cycleNeighbors(ctx, nodes, root, edgeKind)
		if err != nil {
			return 0, err
		}
		stack := []cycleFrame{{node: root, neighbors: neighbors}}
		path := []string{root}
		positions := map[string]int{root: 0}
		color[root] = 1
		for len(stack) != 0 {
			if err := pollContext(ctx, work); err != nil {
				return 0, err
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
				childNeighbors, err := cycleNeighbors(ctx, nodes, next, edgeKind)
				if err != nil {
					return 0, err
				}
				positions[next] = len(path)
				path = append(path, next)
				color[next] = 1
				stack = append(stack, cycleFrame{node: next, neighbors: childNeighbors})
			case 1:
				start := positions[next]
				appendGraphCycle(result, limits, prefix, path[start:], next, nodes)
				count++
			}
		}
	}
	return count, nil
}

func cycleNeighbors(ctx context.Context, nodes map[string]*graphNode, key string, edgeKind uint8) ([]string, error) {
	neighbors := make([]string, 0)
	for index, edge := range nodes[key].incoming {
		if err := pollContext(ctx, index); err != nil {
			return nil, err
		}
		if edge.kind != edgeKind {
			continue
		}
		source := nodes[edge.target]
		if edgeKind == parentEdge {
			neighbors = append(neighbors, graphNodeKey(startNode, source.id))
		} else {
			neighbors = append(neighbors, edge.target)
		}
	}
	return sortOwnedStringsByContext(ctx, neighbors, func(left, right string) bool { return nodes[left].id < nodes[right].id })
}

func appendGraphCycle(result *graphAnalysis, limits Limits, prefix string, path []string, closing string, nodes map[string]*graphNode) {
	result.ErrorCount++
	if uint64(len(result.Errors)) >= limits.DiagnosticSamples {
		result.DiagnosticsTruncated = true
		return
	}
	builder := newBoundedDiagnosticBuilder(limits.DiagnosticTextBytes)
	builder.write(prefix)
	for index, key := range path {
		if index != 0 {
			builder.write(" -> ")
		}
		builder.write(nodes[key].id)
	}
	if len(path) != 0 {
		builder.write(" -> ")
	}
	builder.write(nodes[closing].id)
	if builder.truncated {
		result.DiagnosticsTruncated = true
	}
	result.Errors = append(result.Errors, builder.String())
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

func appendBoundedGraphError(result *graphAnalysis, limits Limits, parts ...string) {
	result.ErrorCount++
	if uint64(len(result.Errors)) >= limits.DiagnosticSamples {
		result.DiagnosticsTruncated = true
		return
	}
	builder := newBoundedDiagnosticBuilder(limits.DiagnosticTextBytes)
	for _, part := range parts {
		builder.write(part)
	}
	if builder.truncated {
		result.DiagnosticsTruncated = true
	}
	result.Errors = append(result.Errors, builder.String())
}

type boundedDiagnosticBuilder struct {
	builder   strings.Builder
	maximum   int
	truncated bool
}

func newBoundedDiagnosticBuilder(maximum uint64) *boundedDiagnosticBuilder {
	// #nosec G115 -- effective diagnostic limits never exceed 512 bytes.
	limit := int(maximum)
	builder := &boundedDiagnosticBuilder{maximum: limit}
	builder.builder.Grow(limit)
	return builder
}

func (builder *boundedDiagnosticBuilder) write(value string) {
	if builder.truncated {
		return
	}
	remaining := builder.maximum - builder.builder.Len()
	if len(value) <= remaining {
		builder.builder.WriteString(value)
		return
	}
	builder.truncated = true
	cut := remaining
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	builder.builder.WriteString(value[:cut])
}

func (builder *boundedDiagnosticBuilder) String() string { return builder.builder.String() }

func graphNodeKey(kind uint8, id string) string { return fmt.Sprintf("%d:%s", kind, id) }

func graphNodeLess(left, right *graphNode) bool {
	if left.kind != right.kind {
		return left.kind < right.kind
	}
	return left.id < right.id
}
