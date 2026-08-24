// ABOUTME: Performs one bounded pass over canonical PACT objects and publishes typed records.
// ABOUTME: Computes local completeness and the exact object-set source fingerprint.
package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"pact/internal/store"
)

const sourceFingerprintDomain = "PACT-OBJECT-SET-FINGERPRINT-V1\x00"

// Blocker identifies one absent immutable dependency in the local object set.
type Blocker struct{ Code, SourceID, Field, MissingRef string }

// Completeness describes closure of the scanned local object set.
type Completeness struct {
	Scope, Status, GlobalCompleteness string
	Blockers                          []Blocker
}

// ScanOptions controls strict completeness treatment and resource bounds.
type ScanOptions struct {
	Strict bool
	Limits Limits
}

// ScanCounts records bounded canonical and graph resources consumed by a scan.
type ScanCounts struct{ Objects, Commits, Checkpoints, Events, Edges, CanonicalBytes uint64 }

// CommitRecord is the immutable scalar and slice projection of one valid commit.
type CommitRecord struct {
	ID, Namespace, ActorID, ActorLabel, ObservedAt, BodyDigest string
	Parents, EventRefs                                         []string
	Integrity, Structure, Authenticity, Completeness           string
}

// EventRecord is the immutable index projection of one valid canonical event.
type EventRecord struct {
	Ref, CommitID, LocalID, Namespace, Kind, Type, Subject, SchemaRef string
	CausedBy, Supersedes, Tags                                        []string
}

// CheckpointRecord is the immutable scalar and slice projection of one checkpoint.
type CheckpointRecord struct {
	ID, Scope, PolicyRef, AuthorityEpoch, PreviousCheckpoint string
	ActorID, ActorLabel, ObservedAt, BodyDigest              string
	SchemaRefs                                               []string
	Frontier                                                 []CheckpointFrontier
	Integrity, Structure, Authenticity, Completeness         string
}

// ScanResult is the complete bounded canonical projection used by Phase 2 consumers.
type ScanResult struct {
	Objects           map[string]ObjectVerification
	Commits           map[string]CommitRecord
	Checkpoints       map[string]CheckpointRecord
	Events            map[string]EventRecord
	Heads             map[string][]string
	CausalBatches     map[string]uint64
	UnresolvedEvents  []string
	Completeness      Completeness
	Counts            ScanCounts
	SourceFingerprint string
	Verification      VerifyResult
}

// Scan validates and projects one local canonical object set without acquiring the store lock.
func Scan(ctx context.Context, st *store.Store, options ScanOptions) (ScanResult, error) {
	if st == nil || ctx == nil {
		return ScanResult{}, fmt.Errorf("store and context are required")
	}
	if err := ctx.Err(); err != nil {
		return ScanResult{}, err
	}
	limits := effectiveLimits(options.Limits)
	files, err := st.ObjectFilesBounded(limits.Objects)
	if err != nil {
		if strings.Contains(err.Error(), "canonical object count exceeds limit") {
			return ScanResult{}, limitError("objects", limits.Objects)
		}
		return ScanResult{}, err
	}
	result := emptyScanResult()
	verification := newVerifyResult(st, options.Strict)
	collected, err := collectCanonicalObjects(ctx, st, files, limits, &result, &verification)
	if err != nil {
		return ScanResult{}, err
	}
	if collected.allValid {
		result.SourceFingerprint = sourceFingerprint(collected.ids)
	}
	if err := buildScanRecords(ctx, limits, collected, &result); err != nil {
		return ScanResult{}, err
	}
	if err := setVerificationRecordCounts(&verification, result.Counts); err != nil {
		return ScanResult{}, err
	}
	verifyCommitParents(&verification, options.Strict, collected.commits, collected.objects)
	verifyCheckpointReferences(&verification, options.Strict, collected.commits, collected.checkpoints, collected.objects)
	verifyEventReferences(&verification, options.Strict, collected.commits, collected.objects)
	graph, err := analyzeGraph(ctx, result.Commits, result.Events, limits)
	if err != nil {
		return ScanResult{}, err
	}
	for _, message := range graph.Errors {
		verification.Errors = append(verification.Errors, message)
		verification.DAG.Errors = append(verification.DAG.Errors, message)
		verification.Counts.DAG++
	}
	result.CausalBatches = graph.Batches
	result.UnresolvedEvents = graph.Unresolved
	applyAuthorization(st, &verification, collected.commits)
	finishVerification(&verification, collected.commits)
	result.Completeness.Blockers = completenessBlockers(collected.objects, result.Commits, result.Checkpoints, result.Events)
	if len(result.Completeness.Blockers) != 0 {
		result.Completeness.Status = "incomplete"
		applyRecordCompleteness(&result)
	}
	boundVerificationDiagnostics(&verification, limits)
	result.Objects = cloneVerifications(verification.Objects)
	result.Heads = cloneStringMap(verification.Heads)
	verification.Completeness = cloneCompleteness(result.Completeness)
	verification.Limits = LimitsStatus{Profile: LimitsProfile, Status: "within_limits"}
	result.Verification = verification
	return result, nil
}

type collectedObjects struct {
	ids         []string
	allValid    bool
	objects     map[string]ObjectVerification
	commits     map[string]storedCommit
	checkpoints map[string]storedCheckpoint
}

func collectCanonicalObjects(ctx context.Context, st *store.Store, files []store.ObjectFile, limits Limits, result *ScanResult, verification *VerifyResult) (collectedObjects, error) {
	collected := collectedObjects{
		ids: make([]string, 0, len(files)), allValid: true, objects: make(map[string]ObjectVerification, len(files)),
		commits: make(map[string]storedCommit), checkpoints: make(map[string]storedCheckpoint),
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return collectedObjects{}, err
		}
		remaining := limits.CanonicalBytes - result.Counts.CanonicalBytes
		object, size, err := readScannedObject(st, file, limits, remaining)
		if err != nil {
			return collectedObjects{}, err
		}
		result.Counts.CanonicalBytes += size
		result.Counts.Objects++
		if object.Valid() {
			collected.ids = append(collected.ids, file.ID)
		} else {
			collected.allValid = false
		}
		collected.objects[file.ID] = object
		if err := collectScannedObject(file.ID, object, verification, collected.commits, collected.checkpoints); err != nil {
			return collectedObjects{}, err
		}
	}
	return collected, nil
}

func readScannedObject(st *store.Store, file store.ObjectFile, limits Limits, remaining uint64) (ObjectVerification, uint64, error) {
	readLimit := min(limits.ObjectBytes, remaining)
	raw, err := st.GetBounded(file.ID, readLimit)
	if err == nil {
		return verifyCanonicalBytes(file, raw), uint64(len(raw)), nil
	}
	if strings.Contains(err.Error(), "exceeds byte limit") {
		return ObjectVerification{}, 0, scanByteLimitError(limits, remaining)
	}
	if strings.Contains(err.Error(), "object digest mismatch") {
		raw, err = readBoundedCanonicalPath(file.Path, readLimit)
		if err == nil {
			return verifyCanonicalBytes(file, raw), uint64(len(raw)), nil
		}
	}
	if !isPermissionError(err) {
		return ObjectVerification{}, 0, err
	}
	size, statErr := canonicalFileSize(file.Path)
	if statErr != nil {
		return ObjectVerification{}, 0, err
	}
	if size > readLimit {
		return ObjectVerification{}, 0, scanByteLimitError(limits, remaining)
	}
	object := ObjectVerification{ID: file.ID, Path: file.Path, Integrity: "invalid", Structure: "unverified", Authenticity: "unverified", Errors: []string{"cannot read object: " + err.Error()}}
	return object, size, nil
}

func scanByteLimitError(limits Limits, remaining uint64) error {
	if remaining < limits.ObjectBytes {
		return limitError("canonical_bytes", limits.CanonicalBytes)
	}
	return limitError("object_bytes", limits.ObjectBytes)
}

func canonicalFileSize(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.Size() < 0 {
		return 0, fmt.Errorf("canonical object has a negative file size")
	}
	// #nosec G115 -- the negative case is rejected immediately above.
	return uint64(info.Size()), nil
}

func buildScanRecords(ctx context.Context, limits Limits, collected collectedObjects, result *ScanResult) error {
	result.Counts.Commits = uint64(len(collected.commits))
	result.Counts.Checkpoints = uint64(len(collected.checkpoints))
	for _, id := range sortedKeys(collected.commits) {
		commit := collected.commits[id]
		if err := ctx.Err(); err != nil {
			return err
		}
		if uint64(len(commit.parents)) > limits.ParentsPerCommit {
			return objectLimitError("parents_per_commit", limits.ParentsPerCommit, id)
		}
		if uint64(len(commit.events)) > limits.EventsPerCommit {
			return objectLimitError("events_per_commit", limits.EventsPerCommit, id)
		}
		if uint64(len(commit.events)) > limits.Events-result.Counts.Events {
			return objectLimitError("events", limits.Events, id)
		}
		result.Counts.Events += uint64(len(commit.events))
		if err := addGraphEdges(&result.Counts, limits, uint64(len(commit.parents))+2*uint64(len(commit.events))); err != nil {
			return err
		}
		record, events, recordErr := recordsForCommit(id, collected.objects[id], commit)
		if recordErr != nil {
			return recordErr
		}
		result.Commits[id] = record
		for ref, event := range events {
			if err := addGraphEdges(&result.Counts, limits, uint64(len(event.CausedBy))+uint64(len(event.Supersedes))); err != nil {
				return err
			}
			result.Events[ref] = event
		}
	}
	for _, id := range sortedKeys(collected.checkpoints) {
		checkpoint := collected.checkpoints[id]
		record, recordErr := recordForCheckpoint(id, collected.objects[id], checkpoint)
		if recordErr != nil {
			return recordErr
		}
		result.Checkpoints[id] = record
		edges := uint64(0)
		for _, entry := range record.Frontier {
			edges += uint64(len(entry.Heads))
		}
		if record.PreviousCheckpoint != "" {
			edges++
		}
		if err := addGraphEdges(&result.Counts, limits, edges); err != nil {
			return err
		}
	}
	return nil
}

func setVerificationRecordCounts(verification *VerifyResult, counts ScanCounts) error {
	objects, err := boundedCountInt(counts.Objects, Phase2Limits.Objects)
	if err != nil {
		return err
	}
	commits, err := boundedCountInt(counts.Commits, Phase2Limits.Objects)
	if err != nil {
		return err
	}
	checkpoints, err := boundedCountInt(counts.Checkpoints, Phase2Limits.Objects)
	if err != nil {
		return err
	}
	events, err := boundedCountInt(counts.Events, Phase2Limits.Events)
	if err != nil {
		return err
	}
	verification.Counts.Objects = objects
	verification.Counts.Commits = commits
	verification.Counts.Checkpoints = checkpoints
	verification.Counts.Events = events
	return nil
}

func boundedCountInt(value, maximum uint64) (int, error) {
	if value > maximum || maximum > uint64(math.MaxInt) {
		return 0, fmt.Errorf("bounded count does not fit in an int")
	}
	// #nosec G115 -- value is bounded by a checked maximum that fits in int.
	return int(value), nil
}

func applyRecordCompleteness(result *ScanResult) {
	for _, blocker := range result.Completeness.Blockers {
		if commit, found := result.Commits[blocker.SourceID]; found {
			commit.Completeness = "partial"
			result.Commits[blocker.SourceID] = commit
			continue
		}
		if event, found := result.Events[blocker.SourceID]; found {
			commit := result.Commits[event.CommitID]
			commit.Completeness = "partial"
			result.Commits[event.CommitID] = commit
			continue
		}
		if checkpoint, found := result.Checkpoints[blocker.SourceID]; found {
			checkpoint.Completeness = "partial"
			result.Checkpoints[blocker.SourceID] = checkpoint
		}
	}
}

func scanWithReadLock(ctx context.Context, st *store.Store, options ScanOptions) (ScanResult, error) {
	var result ScanResult
	err := st.WithReadLock(func() error {
		var scanErr error
		result, scanErr = Scan(ctx, st, options)
		return scanErr
	})
	return result, err
}

// ResolveCommit performs one bounded canonical lookup and returns a typed commit projection.
func ResolveCommit(ctx context.Context, st *store.Store, id string, requested Limits) (CommitRecord, error) {
	if st == nil || ctx == nil {
		return CommitRecord{}, fmt.Errorf("store and context are required")
	}
	if err := ctx.Err(); err != nil {
		return CommitRecord{}, err
	}
	limits := effectiveLimits(requested)
	readLimit := min(limits.ObjectBytes, limits.CanonicalBytes)
	raw, err := st.GetBounded(id, readLimit)
	if err != nil {
		if os.IsNotExist(err) {
			return CommitRecord{}, fmt.Errorf("%w: object not found: %s", ErrMissingDependency, id)
		}
		if strings.Contains(err.Error(), "exceeds byte limit") {
			if limits.CanonicalBytes < limits.ObjectBytes {
				return CommitRecord{}, limitError("canonical_bytes", limits.CanonicalBytes)
			}
			return CommitRecord{}, objectLimitError("object_bytes", limits.ObjectBytes, id)
		}
		return CommitRecord{}, err
	}
	if err := ctx.Err(); err != nil {
		return CommitRecord{}, err
	}
	verification := verifyCanonicalBytes(store.ObjectFile{ID: id}, raw)
	if !verification.Valid() || verification.Type != "commit" {
		return CommitRecord{}, fmt.Errorf("%w: object is not a valid commit: %s", ErrIntegrity, id)
	}
	commit, err := storedCommitFromObject(verification.object)
	if err != nil {
		return CommitRecord{}, fmt.Errorf("%w: validated commit shape: %w", ErrIntegrity, err)
	}
	if uint64(len(commit.parents)) > limits.ParentsPerCommit {
		return CommitRecord{}, objectLimitError("parents_per_commit", limits.ParentsPerCommit, id)
	}
	if uint64(len(commit.events)) > limits.EventsPerCommit {
		return CommitRecord{}, objectLimitError("events_per_commit", limits.EventsPerCommit, id)
	}
	if uint64(len(commit.events)) > limits.Events {
		return CommitRecord{}, objectLimitError("events", limits.Events, id)
	}
	record, _, err := recordsForCommit(id, verification, commit)
	if err != nil {
		return CommitRecord{}, err
	}
	return record, nil
}

func emptyScanResult() ScanResult {
	return ScanResult{
		Objects: map[string]ObjectVerification{}, Commits: map[string]CommitRecord{}, Checkpoints: map[string]CheckpointRecord{},
		Events: map[string]EventRecord{}, Heads: map[string][]string{}, CausalBatches: map[string]uint64{}, UnresolvedEvents: []string{},
		Completeness: Completeness{Scope: "local_object_set", Status: "locally_closed", GlobalCompleteness: "unknown", Blockers: []Blocker{}},
	}
}

func collectScannedObject(id string, object ObjectVerification, result *VerifyResult, commits map[string]storedCommit, checkpoints map[string]storedCheckpoint) error {
	result.Objects[id] = object
	for _, message := range object.Errors {
		result.Errors = append(result.Errors, id+": "+message)
	}
	switch {
	case object.Integrity != "valid":
		result.Counts.Integrity++
		result.Integrity.Errors = append(result.Integrity.Errors, id+": "+strings.Join(object.Errors, "; "))
	case object.Structure != "valid":
		result.Counts.Structure++
		result.Structure.Errors = append(result.Structure.Errors, id+": "+strings.Join(object.Errors, "; "))
	case object.Authenticity != "valid":
		result.Counts.Authenticity++
		result.Authenticity.Errors = append(result.Authenticity.Errors, id+": "+strings.Join(object.Errors, "; "))
	}
	if !object.Valid() {
		return nil
	}
	switch object.Type {
	case "commit":
		commit, err := storedCommitFromObject(object.object)
		if err != nil {
			return fmt.Errorf("validated commit %s has inconsistent shape: %w", id, err)
		}
		commits[id] = commit
	case "checkpoint":
		checkpoint, err := storedCheckpointFromObject(object.object)
		if err != nil {
			return fmt.Errorf("validated checkpoint %s has inconsistent shape: %w", id, err)
		}
		checkpoints[id] = checkpoint
	}
	return nil
}

func recordsForCommit(id string, verification ObjectVerification, commit storedCommit) (CommitRecord, map[string]EventRecord, error) {
	body, err := requiredObjectField(verification.object, "body")
	if err != nil {
		return CommitRecord{}, nil, err
	}
	actor, err := requiredObjectField(body, "actor")
	if err != nil {
		return CommitRecord{}, nil, err
	}
	actorID, err := requiredStringField(actor, "key_id")
	if err != nil {
		return CommitRecord{}, nil, err
	}
	actorLabel, err := requiredStringField(actor, "label")
	if err != nil {
		return CommitRecord{}, nil, err
	}
	bodyDigest, err := requiredStringField(verification.object, "body_digest")
	if err != nil {
		return CommitRecord{}, nil, err
	}
	record := CommitRecord{
		ID: id, Namespace: commit.namespace, ActorID: actorID, ActorLabel: actorLabel,
		ObservedAt: commit.observed, BodyDigest: bodyDigest, Parents: append([]string(nil), commit.parents...),
		Integrity: "valid", Structure: "valid", Authenticity: "valid", Completeness: "complete",
	}
	record.EventRefs = make([]string, len(commit.events))
	events := make(map[string]EventRecord, len(commit.events))
	for index, stored := range commit.events {
		ref := EventRef(id, stored.localID)
		record.EventRefs[index] = ref
		event := stored.object
		kind, kindErr := requiredStringField(event, "kind")
		eventType, typeErr := requiredStringField(event, "type")
		subject, subjectErr := requiredStringField(event, "subject")
		schemaRef, schemaErr := requiredStringField(event, "schema_ref")
		tags, tagsErr := stringSlice(event["tags"])
		if err := errors.Join(kindErr, typeErr, subjectErr, schemaErr, tagsErr); err != nil {
			return CommitRecord{}, nil, fmt.Errorf("validated event %s projection: %w", ref, err)
		}
		events[ref] = EventRecord{
			Ref: ref, CommitID: id, LocalID: stored.localID, Namespace: commit.namespace,
			Kind: kind, Type: eventType, Subject: subject, SchemaRef: schemaRef,
			CausedBy: resolvedEventRefs(id, stored.causedBy), Supersedes: append([]string(nil), stored.supersedes...), Tags: tags,
		}
	}
	return record, events, nil
}

func recordForCheckpoint(id string, verification ObjectVerification, checkpoint storedCheckpoint) (CheckpointRecord, error) {
	body, err := requiredObjectField(verification.object, "body")
	if err != nil {
		return CheckpointRecord{}, err
	}
	actor, err := requiredObjectField(body, "actor")
	if err != nil {
		return CheckpointRecord{}, err
	}
	scope, scopeErr := requiredStringField(body, "scope")
	policyRef, policyErr := requiredStringField(body, "policy_ref")
	authorityEpoch, epochErr := requiredStringField(body, "authority_epoch")
	actorID, actorIDErr := requiredStringField(actor, "key_id")
	actorLabel, labelErr := requiredStringField(actor, "label")
	observedAt, observedErr := requiredStringField(body, "observed_at")
	bodyDigest, digestErr := requiredStringField(verification.object, "body_digest")
	schemaRefs, schemaErr := stringSlice(body["schema_refs"])
	if err := errors.Join(scopeErr, policyErr, epochErr, actorIDErr, labelErr, observedErr, digestErr, schemaErr); err != nil {
		return CheckpointRecord{}, fmt.Errorf("validated checkpoint %s projection: %w", id, err)
	}
	frontier := make([]CheckpointFrontier, len(checkpoint.frontier))
	for index, entry := range checkpoint.frontier {
		frontier[index] = CheckpointFrontier{Namespace: entry.Namespace, Heads: append([]string(nil), entry.Heads...)}
	}
	return CheckpointRecord{
		ID: id, Scope: scope, PolicyRef: policyRef, AuthorityEpoch: authorityEpoch, PreviousCheckpoint: checkpoint.previous,
		ActorID: actorID, ActorLabel: actorLabel, ObservedAt: observedAt, BodyDigest: bodyDigest,
		SchemaRefs: schemaRefs, Frontier: frontier,
		Integrity: "valid", Structure: "valid", Authenticity: "valid", Completeness: "complete",
	}, nil
}

func completenessBlockers(objects map[string]ObjectVerification, commits map[string]CommitRecord, checkpoints map[string]CheckpointRecord, events map[string]EventRecord) []Blocker {
	unique := make(map[string]Blocker)
	add := func(blocker Blocker) { unique[blockerSortKey(blocker)] = blocker }
	collectCommitBlockers(objects, commits, add)
	collectEventBlockers(objects, events, add)
	collectCheckpointBlockers(objects, checkpoints, add)
	result := make([]Blocker, 0, len(unique))
	for _, blocker := range unique {
		result = append(result, blocker)
	}
	sort.Slice(result, func(i, j int) bool { return blockerSortKey(result[i]) < blockerSortKey(result[j]) })
	return result
}

func collectCommitBlockers(objects map[string]ObjectVerification, commits map[string]CommitRecord, add func(Blocker)) {
	for id, commit := range commits {
		for _, parent := range commit.Parents {
			if _, present := objects[parent]; !present {
				add(Blocker{Code: "missing_parent", SourceID: id, Field: "parents", MissingRef: parent})
			}
		}
	}
}

func collectEventBlockers(objects map[string]ObjectVerification, events map[string]EventRecord, add func(Blocker)) {
	for ref, event := range events {
		for _, dependency := range event.CausedBy {
			if _, present := events[dependency]; !present && !eventTargetObjectPresent(objects, dependency) {
				add(Blocker{Code: "missing_event_reference", SourceID: ref, Field: "caused_by", MissingRef: dependency})
			}
		}
		for _, dependency := range event.Supersedes {
			if _, present := events[dependency]; !present && !eventTargetObjectPresent(objects, dependency) {
				add(Blocker{Code: "missing_event_reference", SourceID: ref, Field: "supersedes", MissingRef: dependency})
			}
		}
	}
}

func collectCheckpointBlockers(objects map[string]ObjectVerification, checkpoints map[string]CheckpointRecord, add func(Blocker)) {
	for id, checkpoint := range checkpoints {
		for _, entry := range checkpoint.Frontier {
			for _, head := range entry.Heads {
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

func sourceFingerprint(ids []string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(sourceFingerprintDomain))
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(ids)))
	_, _ = hash.Write(count[:])
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	for _, id := range sorted {
		var size [2]byte
		// #nosec G115 -- ObjectFilesBounded validates fixed 71-byte SHA-256 IDs before this helper is called.
		binary.BigEndian.PutUint16(size[:], uint16(len(id)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(id))
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil))
}

func objectLimitError(resource string, maximum uint64, id string) *LimitError {
	return &LimitError{Resource: resource, Maximum: maximum, ObservedAtLeast: maximum + 1, ObjectID: id}
}

func addGraphEdges(counts *ScanCounts, limits Limits, amount uint64) error {
	if counts.Edges > limits.GraphEdges || amount > limits.GraphEdges-counts.Edges {
		return limitError("graph_edges", limits.GraphEdges)
	}
	counts.Edges += amount
	return nil
}

func resolvedEventRefs(commitID string, refs []string) []string {
	result := make([]string, len(refs))
	for index, ref := range refs {
		if match := localRefPattern.FindStringSubmatch(ref); match != nil {
			result[index] = EventRef(commitID, match[1])
		} else {
			result[index] = ref
		}
	}
	return result
}

func requiredObjectField(object map[string]any, field string) (map[string]any, error) {
	value, ok := object[field].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("validated field %s is not an object", field)
	}
	return value, nil
}

func requiredStringField(object map[string]any, field string) (string, error) {
	value, ok := object[field].(string)
	if !ok {
		return "", fmt.Errorf("validated field %s is not a string", field)
	}
	return value, nil
}

func effectiveLimits(overrides Limits) Limits {
	result := Phase2Limits
	set := func(target *uint64, override uint64) {
		if override != 0 {
			*target = override
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

func boundVerificationDiagnostics(result *VerifyResult, limits Limits) {
	bound := func(values *[]string) {
		for index, value := range *values {
			if uint64(len(value)) > limits.DiagnosticTextBytes {
				// #nosec G115 -- this branch proves the limit is smaller than len(value), which is an int.
				cut := int(limits.DiagnosticTextBytes)
				for cut > 0 && !utf8.ValidString(value[:cut]) {
					cut--
				}
				(*values)[index] = value[:cut]
				result.DiagnosticsTruncated = true
			}
		}
		if uint64(len(*values)) > limits.DiagnosticSamples {
			*values = append([]string(nil), (*values)[:limits.DiagnosticSamples]...)
			result.DiagnosticsTruncated = true
		}
	}
	bound(&result.Errors)
	bound(&result.Warnings)
	for _, layer := range []*LayerResult{&result.Integrity, &result.Structure, &result.Authenticity, &result.DAG, &result.References} {
		bound(&layer.Errors)
		bound(&layer.Warnings)
	}
	for id, object := range result.Objects {
		bound(&object.Errors)
		bound(&object.Warnings)
		result.Objects[id] = object
	}
}

func cloneVerifications(source map[string]ObjectVerification) map[string]ObjectVerification {
	result := make(map[string]ObjectVerification, len(source))
	for id, object := range source {
		object.Errors = append([]string(nil), object.Errors...)
		object.Warnings = append([]string(nil), object.Warnings...)
		result[id] = object
	}
	return result
}

func cloneStringMap(source map[string][]string) map[string][]string {
	result := make(map[string][]string, len(source))
	for key, values := range source {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func cloneCompleteness(source Completeness) Completeness {
	source.Blockers = append([]Blocker(nil), source.Blockers...)
	return source
}

func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func readBoundedCanonicalPath(path string, maximum uint64) ([]byte, error) {
	// #nosec G304 -- callers pass only canonical paths returned by the store's checked object enumeration or fixed digest layout.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	limit := int64(math.MaxInt64)
	if maximum < uint64(math.MaxInt64) {
		limit = int64(maximum) + 1
	}
	return io.ReadAll(io.LimitReader(file, limit))
}

func isPermissionError(err error) bool { return errors.Is(err, fs.ErrPermission) }
