// ABOUTME: Accounts for fixed scan resources before secondary record projection.
// ABOUTME: Clamps test overrides to Phase 2 hard caps and counts every edge category once.
package ledger

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
}

func (counts *scanResourceCounts) addCanonicalObject(id string, object ObjectVerification, limits Limits) error {
	if !object.Valid() {
		return nil
	}
	body, ok := object.object["body"].(map[string]any)
	if !ok {
		return nil
	}
	next := *counts
	switch object.Type {
	case "commit":
		parents, _ := body["parents"].([]any)
		events, _ := body["events"].([]any)
		if uint64(len(parents)) > limits.ParentsPerCommit {
			return objectLimitError("parents_per_commit", limits.ParentsPerCommit, id)
		}
		if uint64(len(events)) > limits.EventsPerCommit {
			return objectLimitError("events_per_commit", limits.EventsPerCommit, id)
		}
		if uint64(len(events)) > limits.Events-next.events {
			return objectLimitError("events", limits.Events, id)
		}
		next.commits++
		next.events += uint64(len(events))
		next.edges.parents += uint64(len(parents))
		next.edges.eventGates += 2 * uint64(len(events))
		for _, raw := range events {
			event, _ := raw.(map[string]any)
			causedBy, _ := event["caused_by"].([]any)
			supersedes, _ := event["supersedes"].([]any)
			next.edges.causedBy += uint64(len(causedBy))
			next.edges.supersedes += uint64(len(supersedes))
		}
	case "checkpoint":
		next.checkpoints++
		frontier, _ := body["frontier"].([]any)
		for _, raw := range frontier {
			entry, _ := raw.(map[string]any)
			heads, _ := entry["heads"].([]any)
			next.edges.checkpointHeads += uint64(len(heads))
		}
		if previous, _ := body["previous_checkpoint"].(string); previous != "" {
			next.edges.previousCheckpoints++
		}
	}
	if err := next.edges.validate(limits, id); err != nil {
		return err
	}
	*counts = next
	return nil
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
