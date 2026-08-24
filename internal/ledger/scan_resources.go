// ABOUTME: Accounts for fixed scan resources before secondary record projection.
// ABOUTME: Clamps test overrides to Phase 2 hard caps and counts every edge category once.
package ledger

import "context"

type edgeCategoryCounts struct {
	parents, eventGates, causedBy, supersedes, checkpointHeads, previousCheckpoints uint64
}

func (counts edgeCategoryCounts) total() uint64 {
	return counts.parents + counts.eventGates + counts.causedBy + counts.supersedes + counts.checkpointHeads + counts.previousCheckpoints
}

func (counts edgeCategoryCounts) validate(limits Limits, id string) error {
	if counts.total() > limits.GraphEdges {
		return objectLimitError("graph_edges", limits.GraphEdges, id)
	}
	return nil
}

type scanResourceCounts struct {
	commits, checkpoints, events uint64
	edges                        edgeCategoryCounts
	rawEvents                    uint64
	rawEdges                     edgeCategoryCounts
}

type parsedObjectResources struct {
	kind   string
	events uint64
	edges  edgeCategoryCounts
}

func (counts *scanResourceCounts) preflightParsedObject(ctx context.Context, id string, object map[string]any, limits Limits) (parsedObjectResources, error) {
	if err := ctx.Err(); err != nil {
		return parsedObjectResources{}, err
	}
	body, ok := object["body"].(map[string]any)
	if !ok {
		return parsedObjectResources{}, nil
	}
	resources := parsedObjectResources{}
	var err error
	switch object["format"] {
	case commitFormat:
		resources, err = preflightCommitResources(ctx, id, body, limits, counts.rawEvents)
		if err != nil {
			return parsedObjectResources{}, err
		}
	case checkpointFormat:
		resources, err = preflightCheckpointResources(ctx, body)
		if err != nil {
			return parsedObjectResources{}, err
		}
	}
	nextEdges := counts.rawEdges
	nextEdges.add(resources.edges)
	if err := nextEdges.validate(limits, id); err != nil {
		return parsedObjectResources{}, err
	}
	counts.rawEvents += resources.events
	counts.rawEdges = nextEdges
	return resources, nil
}

func preflightCommitResources(ctx context.Context, id string, body map[string]any, limits Limits, priorEvents uint64) (parsedObjectResources, error) {
	parents, _ := body["parents"].([]any)
	events, _ := body["events"].([]any)
	if uint64(len(parents)) > limits.ParentsPerCommit {
		return parsedObjectResources{}, objectLimitError("parents_per_commit", limits.ParentsPerCommit, id)
	}
	if uint64(len(events)) > limits.EventsPerCommit {
		return parsedObjectResources{}, objectLimitError("events_per_commit", limits.EventsPerCommit, id)
	}
	if uint64(len(events)) > limits.Events-priorEvents {
		return parsedObjectResources{}, objectLimitError("events", limits.Events, id)
	}
	for index := range parents {
		if err := pollContext(ctx, index); err != nil {
			return parsedObjectResources{}, err
		}
	}
	resources := parsedObjectResources{kind: "commit", events: uint64(len(events))}
	resources.edges.parents = uint64(len(parents))
	resources.edges.eventGates = 2 * uint64(len(events))
	for index, raw := range events {
		if err := pollContext(ctx, index); err != nil {
			return parsedObjectResources{}, err
		}
		event, _ := raw.(map[string]any)
		causedBy, _ := event["caused_by"].([]any)
		supersedes, _ := event["supersedes"].([]any)
		for refIndex := range len(causedBy) + len(supersedes) {
			if err := pollContext(ctx, refIndex); err != nil {
				return parsedObjectResources{}, err
			}
		}
		resources.edges.causedBy += uint64(len(causedBy))
		resources.edges.supersedes += uint64(len(supersedes))
	}
	return resources, nil
}

func preflightCheckpointResources(ctx context.Context, body map[string]any) (parsedObjectResources, error) {
	resources := parsedObjectResources{kind: "checkpoint"}
	frontier, _ := body["frontier"].([]any)
	work := 0
	for index, raw := range frontier {
		if err := pollContext(ctx, index); err != nil {
			return parsedObjectResources{}, err
		}
		entry, _ := raw.(map[string]any)
		heads, _ := entry["heads"].([]any)
		for range heads {
			if err := pollContext(ctx, work); err != nil {
				return parsedObjectResources{}, err
			}
			work++
		}
		resources.edges.checkpointHeads += uint64(len(heads))
	}
	if previous, _ := body["previous_checkpoint"].(string); previous != "" {
		resources.edges.previousCheckpoints++
	}
	return resources, nil
}

func (counts *scanResourceCounts) acceptValid(resources parsedObjectResources) {
	switch resources.kind {
	case "commit":
		counts.commits++
	case "checkpoint":
		counts.checkpoints++
	}
	counts.events += resources.events
	counts.edges.add(resources.edges)
}

func (counts *edgeCategoryCounts) add(other edgeCategoryCounts) {
	counts.parents += other.parents
	counts.eventGates += other.eventGates
	counts.causedBy += other.causedBy
	counts.supersedes += other.supersedes
	counts.checkpointHeads += other.checkpointHeads
	counts.previousCheckpoints += other.previousCheckpoints
}

func objectLimitError(resource string, maximum uint64, id string) *LimitError {
	return &LimitError{Resource: resource, Maximum: maximum, ObservedAtLeast: maximum + 1, ObjectID: id}
}

func effectiveLimits(overrides Limits) Limits {
	result := Phase2Limits
	set := func(target *uint64, override uint64) {
		if override != 0 {
			*target = min(*target, override)
		}
	}
	set(&result.ObjectBytes, overrides.ObjectBytes)
	set(&result.Objects, overrides.Objects)
	set(&result.CanonicalBytes, overrides.CanonicalBytes)
	set(&result.EventsPerCommit, overrides.EventsPerCommit)
	set(&result.Events, overrides.Events)
	set(&result.ParentsPerCommit, overrides.ParentsPerCommit)
	set(&result.CausalDepth, overrides.CausalDepth)
	set(&result.FrontierNodes, overrides.FrontierNodes)
	set(&result.GraphEdges, overrides.GraphEdges)
	set(&result.PageResults, overrides.PageResults)
	set(&result.FilterValuesPerFamily, overrides.FilterValuesPerFamily)
	set(&result.FilterValuesTotal, overrides.FilterValuesTotal)
	set(&result.EncodedCursorBytes, overrides.EncodedCursorBytes)
	set(&result.DecodedCursorBytes, overrides.DecodedCursorBytes)
	set(&result.JSONResultBytes, overrides.JSONResultBytes)
	set(&result.SQLiteBytes, overrides.SQLiteBytes)
	set(&result.DiagnosticSamples, overrides.DiagnosticSamples)
	set(&result.DiagnosticTextBytes, overrides.DiagnosticTextBytes)
	return result
}
