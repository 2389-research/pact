// ABOUTME: Computes local dependency blockers and propagates partial scan records.
// ABOUTME: Uses deterministic iterative closure across commit, event, and checkpoint dependencies.
package ledger

import (
	"context"
	"strings"
)

const commitCompletenessPrefix = "commit:\x00"
const checkpointCompletenessPrefix = "checkpoint:\x00"

func applyRecordCompleteness(ctx context.Context, result *ScanResult) error {
	dependents, err := completenessDependents(ctx, result)
	if err != nil {
		return err
	}
	partial, err := directPartialRecords(ctx, result)
	if err != nil {
		return err
	}
	if err := propagatePartialRecords(ctx, dependents, partial); err != nil {
		return err
	}
	return publishPartialRecords(ctx, result, partial)
}

func completenessDependents(ctx context.Context, result *ScanResult) (map[string][]string, error) {
	dependents := make(map[string][]string)
	work := 0
	if err := addCommitCompletenessDependents(ctx, result, dependents, &work); err != nil {
		return nil, err
	}
	if err := addEventCompletenessDependents(ctx, result, dependents, &work); err != nil {
		return nil, err
	}
	if err := addCheckpointCompletenessDependents(ctx, result, dependents, &work); err != nil {
		return nil, err
	}
	return dependents, nil
}

func addCommitCompletenessDependents(ctx context.Context, result *ScanResult, dependents map[string][]string, work *int) error {
	ids, err := sortedKeysContext(ctx, result.Commits)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := pollCompletenessWork(ctx, work); err != nil {
			return err
		}
		for _, parent := range result.Commits[id].Parents {
			if err := pollContext(ctx, *work); err != nil {
				return err
			}
			(*work)++
			if _, found := result.Commits[parent]; found {
				dependents[commitCompletenessPrefix+parent] = append(dependents[commitCompletenessPrefix+parent], commitCompletenessPrefix+id)
			}
		}
	}
	return nil
}

func addEventCompletenessDependents(ctx context.Context, result *ScanResult, dependents map[string][]string, work *int) error {
	refs, err := sortedKeysContext(ctx, result.Events)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err := pollCompletenessWork(ctx, work); err != nil {
			return err
		}
		event := result.Events[ref]
		for _, dependency := range event.CausedBy {
			if err := pollContext(ctx, *work); err != nil {
				return err
			}
			(*work)++
			if target, found := result.Events[dependency]; found {
				dependents[commitCompletenessPrefix+target.CommitID] = append(dependents[commitCompletenessPrefix+target.CommitID], commitCompletenessPrefix+event.CommitID)
			}
		}
	}
	return nil
}

func addCheckpointCompletenessDependents(ctx context.Context, result *ScanResult, dependents map[string][]string, work *int) error {
	ids, err := sortedKeysContext(ctx, result.Checkpoints)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := pollCompletenessWork(ctx, work); err != nil {
			return err
		}
		checkpoint := result.Checkpoints[id]
		for _, entry := range checkpoint.Frontier {
			for _, head := range entry.Heads {
				if err := pollContext(ctx, *work); err != nil {
					return err
				}
				(*work)++
				if _, found := result.Commits[head]; found {
					dependents[commitCompletenessPrefix+head] = append(dependents[commitCompletenessPrefix+head], checkpointCompletenessPrefix+id)
				}
			}
		}
		if _, found := result.Checkpoints[checkpoint.PreviousCheckpoint]; found {
			dependents[checkpointCompletenessPrefix+checkpoint.PreviousCheckpoint] = append(dependents[checkpointCompletenessPrefix+checkpoint.PreviousCheckpoint], checkpointCompletenessPrefix+id)
		}
	}
	return nil
}

func pollCompletenessWork(ctx context.Context, work *int) error {
	err := pollContext(ctx, *work)
	(*work)++
	return err
}

func directPartialRecords(ctx context.Context, result *ScanResult) (map[string]bool, error) {
	partial := make(map[string]bool)
	for index, blocker := range result.Completeness.Blockers {
		if err := pollContext(ctx, index); err != nil {
			return nil, err
		}
		if _, found := result.Commits[blocker.SourceID]; found {
			partial[commitCompletenessPrefix+blocker.SourceID] = true
		}
		if event, found := result.Events[blocker.SourceID]; found {
			partial[commitCompletenessPrefix+event.CommitID] = true
		}
		if _, found := result.Checkpoints[blocker.SourceID]; found {
			partial[checkpointCompletenessPrefix+blocker.SourceID] = true
		}
	}
	return partial, nil
}

func propagatePartialRecords(ctx context.Context, dependents map[string][]string, partial map[string]bool) error {
	queue, err := sortedKeysContext(ctx, partial)
	if err != nil {
		return err
	}
	work := 0
	for index := 0; index < len(queue); index++ {
		children, err := sortedStringsContext(ctx, dependents[queue[index]])
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := pollContext(ctx, work); err != nil {
				return err
			}
			work++
			if !partial[child] {
				partial[child] = true
				queue = append(queue, child)
			}
		}
	}
	return nil
}

func publishPartialRecords(ctx context.Context, result *ScanResult, partial map[string]bool) error {
	keys, err := sortedKeysContext(ctx, partial)
	if err != nil {
		return err
	}
	for index, key := range keys {
		if err := pollContext(ctx, index); err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(key, commitCompletenessPrefix):
			id := strings.TrimPrefix(key, commitCompletenessPrefix)
			commit := result.Commits[id]
			commit.Completeness = "partial"
			result.Commits[id] = commit
		case strings.HasPrefix(key, checkpointCompletenessPrefix):
			id := strings.TrimPrefix(key, checkpointCompletenessPrefix)
			checkpoint := result.Checkpoints[id]
			checkpoint.Completeness = "partial"
			result.Checkpoints[id] = checkpoint
		}
	}
	return nil
}

func completenessBlockers(ctx context.Context, objects map[string]ObjectVerification, commits map[string]CommitRecord, checkpoints map[string]CheckpointRecord, events map[string]EventRecord) ([]Blocker, error) {
	unique := make(map[string]Blocker)
	add := func(blocker Blocker) { unique[blockerSortKey(blocker)] = blocker }
	if err := collectCommitBlockers(ctx, objects, commits, add); err != nil {
		return nil, err
	}
	if err := collectEventBlockers(ctx, objects, events, add); err != nil {
		return nil, err
	}
	if err := collectCheckpointBlockers(ctx, objects, checkpoints, add); err != nil {
		return nil, err
	}
	keys, err := sortedKeysContext(ctx, unique)
	if err != nil {
		return nil, err
	}
	result := make([]Blocker, 0, len(unique))
	for index, key := range keys {
		if err := pollContext(ctx, index); err != nil {
			return nil, err
		}
		result = append(result, unique[key])
	}
	return result, nil
}

func collectCommitBlockers(ctx context.Context, objects map[string]ObjectVerification, commits map[string]CommitRecord, add func(Blocker)) error {
	work := 0
	for id, commit := range commits {
		if err := pollContext(ctx, work); err != nil {
			return err
		}
		work++
		for _, parent := range commit.Parents {
			if err := pollContext(ctx, work); err != nil {
				return err
			}
			work++
			if _, present := objects[parent]; !present {
				add(Blocker{Code: "missing_parent", SourceID: id, Field: "parents", MissingRef: parent})
			}
		}
	}
	return nil
}

func collectEventBlockers(ctx context.Context, objects map[string]ObjectVerification, events map[string]EventRecord, add func(Blocker)) error {
	work := 0
	for ref, event := range events {
		if err := pollContext(ctx, work); err != nil {
			return err
		}
		work++
		for _, dependency := range event.CausedBy {
			if err := pollContext(ctx, work); err != nil {
				return err
			}
			work++
			if _, present := events[dependency]; !present && !eventTargetObjectPresent(objects, dependency) {
				add(Blocker{Code: "missing_event_reference", SourceID: ref, Field: "caused_by", MissingRef: dependency})
			}
		}
		for _, dependency := range event.Supersedes {
			if err := pollContext(ctx, work); err != nil {
				return err
			}
			work++
			if _, present := events[dependency]; !present && !eventTargetObjectPresent(objects, dependency) {
				add(Blocker{Code: "missing_event_reference", SourceID: ref, Field: "supersedes", MissingRef: dependency})
			}
		}
	}
	return nil
}

func collectCheckpointBlockers(ctx context.Context, objects map[string]ObjectVerification, checkpoints map[string]CheckpointRecord, add func(Blocker)) error {
	work := 0
	for id, checkpoint := range checkpoints {
		if err := pollContext(ctx, work); err != nil {
			return err
		}
		work++
		for _, entry := range checkpoint.Frontier {
			for _, head := range entry.Heads {
				if err := pollContext(ctx, work); err != nil {
					return err
				}
				work++
				if _, present := objects[head]; !present {
					add(Blocker{Code: "missing_checkpoint_head", SourceID: id, Field: "frontier.heads", MissingRef: head})
				}
			}
		}
		if checkpoint.PreviousCheckpoint != "" {
			if _, present := objects[checkpoint.PreviousCheckpoint]; !present {
				add(Blocker{Code: "missing_previous_checkpoint", SourceID: id, Field: "previous_checkpoint", MissingRef: checkpoint.PreviousCheckpoint})
			}
		}
	}
	return nil
}

func eventTargetObjectPresent(objects map[string]ObjectVerification, ref string) bool {
	match := eventRefPattern.FindStringSubmatch(ref)
	if match == nil {
		return false
	}
	_, present := objects[match[1]]
	return present
}

func blockerSortKey(blocker Blocker) string {
	return blocker.Code + "\x00" + blocker.SourceID + "\x00" + blocker.Field + "\x00" + blocker.MissingRef
}
