// ABOUTME: Builds immutable, signed PACT commits and computes local namespace heads.
// ABOUTME: Refuses writes when existing canonical history fails structural checks.
package ledger

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"maps"
	"sort"
	"time"
	"unicode/utf8"

	"pact/internal/identity"
	"pact/internal/store"
)

const commitFormat = "pact/commit/v1"

// CommitOptions controls the immutable commit envelope.
type CommitOptions struct {
	Namespace     string
	Parents       []string
	ObservedAt    string
	CorrelationID string
}

// CommitResult reports immutable admission without exposing private key bytes.
type CommitResult struct {
	ObjectID             string
	Created              bool
	Namespace            string
	Parents              []string
	EventRefs            []string
	Integrity            string
	Authenticity         string
	Authorization        string
	AuthorizationReasons []string
	LeaseStatus          string
	Path                 string
}

// Commit validates, signs, persists, and checks one normalized event batch.
func Commit(st *store.Store, key *identity.KeyFile, batch EventBatch, options CommitOptions) (CommitResult, error) {
	if st == nil || key == nil {
		return CommitResult{}, fmt.Errorf("store and key are required")
	}
	rechecked, err := NormalizeEventBatch(batchValue(batch))
	if err != nil {
		return CommitResult{}, err
	}
	batch = rechecked
	if err := validateSigningKey(key); err != nil {
		return CommitResult{}, err
	}
	commits, invalid, err := validCommits(st)
	if err != nil {
		return CommitResult{}, err
	}
	if len(invalid) != 0 {
		return CommitResult{}, fmt.Errorf("%w: refusing to mutate a store with invalid canonical objects: %s", ErrIntegrity, invalid[0])
	}
	prepared, err := prepareCommit(st, batch, options, commits)
	if err != nil {
		return CommitResult{}, err
	}
	body := map[string]any{
		"namespace": prepared.namespace, "parents": stringsToAny(prepared.parents),
		"actor":       map[string]any{"key_id": key.KeyID, "label": key.Actor},
		"authority":   map[string]any{"delegation_ref": nil, "epoch": nil, "lease_ref": nil},
		"observed_at": prepared.observedAt, "metadata": prepared.metadata, "events": batchEvents(batch.Events),
	}
	if prepared.correlationID != "" {
		body["correlation_id"] = prepared.correlationID
	}
	if hazards := scanSecretHazards(body, "$"); len(hazards) != 0 {
		return CommitResult{}, fmt.Errorf("%w: refusing to sign immutable secret-like material: %v", ErrSecretSafety, hazards)
	}
	if err := checkSignedObjectBytes(commitFormat, body, key.KeyID, base64.RawURLEncoding.EncodeToString(key.Public)); err != nil {
		return CommitResult{}, err
	}
	bodyDigest, signature, err := identity.SignBody(body, key.Private)
	if err != nil {
		return CommitResult{}, err
	}
	object := map[string]any{"format": commitFormat, "body": body, "body_digest": bodyDigest, "signature": map[string]any{"algorithm": "ed25519", "key_id": key.KeyID, "public_key": base64.RawURLEncoding.EncodeToString(key.Public), "value": base64.RawURLEncoding.EncodeToString(signature)}}
	if _, err := validateCommitObject(object); err != nil {
		return CommitResult{}, fmt.Errorf("commit preflight structure: %w", err)
	}
	if err := verifySignature(object); err != nil {
		return CommitResult{}, fmt.Errorf("commit preflight signature: %w", err)
	}
	objectID, created, err := st.PutCanonical(object)
	if err != nil {
		return CommitResult{}, err
	}
	verified, err := verificationForID(st, objectID)
	if err != nil {
		return CommitResult{}, err
	}
	if !verified.Valid() {
		return CommitResult{}, fmt.Errorf("new commit failed post-write verification: %s: %v", objectID, verified.Errors)
	}
	refs := make([]string, len(batch.Events))
	for index, event := range batch.Events {
		refs[index] = EventRef(objectID, event.LocalID)
	}
	authorization := authorizationForResult(st, verified)
	return CommitResult{ObjectID: objectID, Created: created, Namespace: prepared.namespace, Parents: prepared.parents, EventRefs: refs, Integrity: "valid", Authenticity: "valid", Authorization: authorization.Status, AuthorizationReasons: authorization.Reasons, LeaseStatus: authorization.LeaseStatus, Path: verified.Path}, nil
}

type preparedCommit struct {
	namespace, observedAt, correlationID string
	parents                              []string
	metadata                             map[string]any
}

func prepareCommit(st *store.Store, batch EventBatch, options CommitOptions, commits map[string]storedCommit) (preparedCommit, error) {
	namespace := options.Namespace
	if namespace == "" {
		namespace = batch.Namespace
	}
	if namespace == "" {
		var err error
		namespace, err = st.DefaultNamespace()
		if err != nil {
			return preparedCommit{}, err
		}
	}
	if err := validateNamespace(namespace); err != nil {
		return preparedCommit{}, err
	}
	parents, err := prepareParents(options.Parents, namespace, commits)
	if err != nil {
		return preparedCommit{}, err
	}
	observedAt := options.ObservedAt
	if observedAt == "" {
		observedAt = batch.ObservedAt
	}
	if observedAt == "" {
		observedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if utf8.RuneCountInString(observedAt) > 64 {
		return preparedCommit{}, fmt.Errorf("observed_at must be a short timestamp string")
	}
	correlationID := options.CorrelationID
	if correlationID == "" {
		correlationID = batch.CorrelationID
	}
	if utf8.RuneCountInString(correlationID) > 255 {
		return preparedCommit{}, fmt.Errorf("correlation ID is too long")
	}
	metadata := batch.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	if _, exists := metadata["producer"]; !exists {
		metadata = cloneObject(metadata)
		metadata["producer"] = "pact-reference-cli/0.1.0"
	}
	return preparedCommit{namespace: namespace, parents: parents, observedAt: observedAt, correlationID: correlationID, metadata: metadata}, nil
}

func prepareParents(requested []string, namespace string, commits map[string]storedCommit) ([]string, error) {
	parents := requested
	if parents == nil {
		parents = headsFor(commits, namespace)[namespace]
	}
	parents, err := normalizeAndLimitParents(parents)
	if err != nil {
		return nil, err
	}
	for _, parent := range parents {
		parentObject, found := commits[parent]
		if !found {
			return nil, fmt.Errorf("%w: parent commit is unavailable or invalid: %s", ErrMissingDependency, parent)
		}
		if parentObject.namespace != namespace {
			return nil, fmt.Errorf("parent %s belongs to namespace %q, not %q", parent, parentObject.namespace, namespace)
		}
	}
	return parents, nil
}

// Heads reports valid local commit heads, optionally limited to a namespace prefix.
func Heads(st *store.Store, namespacePrefix string) (map[string][]string, error) {
	if st == nil {
		return nil, fmt.Errorf("store is required")
	}
	if namespacePrefix != "" {
		if err := validateNamespace(namespacePrefix); err != nil {
			return nil, err
		}
	}
	commits, invalid, err := validCommits(st)
	if err != nil {
		return nil, err
	}
	if len(invalid) != 0 {
		return nil, fmt.Errorf("%w: refusing to compute heads with invalid canonical objects: %s", ErrIntegrity, invalid[0])
	}
	all := headsFor(commits, "")
	if namespacePrefix == "" {
		return all, nil
	}
	result := map[string][]string{}
	for namespace, ids := range all {
		if namespace == namespacePrefix || len(namespace) > len(namespacePrefix) && namespace[:len(namespacePrefix)+1] == namespacePrefix+"/" {
			result[namespace] = ids
		}
	}
	return result, nil
}

// EventRef returns a stable event reference from a commit ID and local event ID.
func EventRef(commitID, localID string) string { return "pact:event:" + commitID + "#" + localID }

type storedCommit struct {
	namespace string
	parents   []string
	events    []storedEvent
	signerID  string
	publicKey string
	actor     map[string]any
	observed  string
}

type storedEvent struct {
	localID    string
	causedBy   []string
	supersedes []string
	object     map[string]any
}

func validCommits(st *store.Store) (map[string]storedCommit, []string, error) {
	scan, err := scanWithReadLock(context.Background(), st, ScanOptions{Limits: Phase2Limits})
	if err != nil {
		return nil, nil, err
	}
	commits := make(map[string]storedCommit, len(scan.Commits))
	invalid := []string{}
	for id, verified := range scan.Objects {
		if !verified.Valid() {
			invalid = append(invalid, id)
		}
	}
	invalid = append(invalid, scan.Verification.DAG.Errors...)
	for id, record := range scan.Commits {
		commits[id] = storedCommit{namespace: record.Namespace, parents: append([]string(nil), record.Parents...)}
	}
	sort.Strings(invalid)
	return commits, invalid, nil
}
func headsFor(commits map[string]storedCommit, namespace string) map[string][]string {
	available := map[string]map[string]bool{}
	referenced := map[string]map[string]bool{}
	for id, commit := range commits {
		ns := commit.namespace
		if namespace != "" && ns != namespace {
			continue
		}
		if available[ns] == nil {
			available[ns], referenced[ns] = map[string]bool{}, map[string]bool{}
		}
		available[ns][id] = true
		for _, parent := range commit.parents {
			referenced[ns][parent] = true
		}
	}
	result := map[string][]string{}
	for ns, ids := range available {
		for id := range ids {
			if !referenced[ns][id] {
				result[ns] = append(result[ns], id)
			}
		}
		sort.Strings(result[ns])
	}
	return result
}

func storedCommitFromObject(object map[string]any) (storedCommit, error) {
	return storedCommitFromObjectContext(context.Background(), object)
}

func storedCommitFromObjectContext(ctx context.Context, object map[string]any) (storedCommit, error) {
	if err := ctx.Err(); err != nil {
		return storedCommit{}, err
	}
	work := 0
	body, ok := object["body"].(map[string]any)
	if !ok {
		return storedCommit{}, fmt.Errorf("body is not an object")
	}
	namespace, ok := body["namespace"].(string)
	if !ok {
		return storedCommit{}, fmt.Errorf("namespace is not a string")
	}
	parents, err := stringSliceContext(ctx, body["parents"], &work)
	if err != nil {
		return storedCommit{}, fmt.Errorf("parents: %w", err)
	}
	rawEvents, ok := body["events"].([]any)
	if !ok {
		return storedCommit{}, fmt.Errorf("events are not an array")
	}
	events, err := storedEventsFromObjectsContext(ctx, rawEvents, &work)
	if err != nil {
		return storedCommit{}, err
	}
	actor, ok := body["actor"].(map[string]any)
	if !ok {
		return storedCommit{}, fmt.Errorf("actor is not an object")
	}
	signerID, ok := actor["key_id"].(string)
	if !ok {
		return storedCommit{}, fmt.Errorf("actor key_id is not a string")
	}
	signature, ok := object["signature"].(map[string]any)
	if !ok {
		return storedCommit{}, fmt.Errorf("signature is not an object")
	}
	publicKey, ok := signature["public_key"].(string)
	if !ok {
		return storedCommit{}, fmt.Errorf("signature public_key is not a string")
	}
	observed, ok := body["observed_at"].(string)
	if !ok {
		return storedCommit{}, fmt.Errorf("observed_at is not a string")
	}
	return storedCommit{namespace: namespace, parents: parents, events: events, signerID: signerID, publicKey: publicKey, actor: actor, observed: observed}, nil
}

func storedEventsFromObjectsContext(ctx context.Context, rawEvents []any, work *int) ([]storedEvent, error) {
	if err := pollContext(ctx, *work); err != nil {
		return nil, err
	}
	*work++
	events := make([]storedEvent, len(rawEvents))
	for index, raw := range rawEvents {
		if err := pollContext(ctx, *work); err != nil {
			return nil, err
		}
		*work++
		event, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("event %d is not an object", index)
		}
		localID, ok := event["local_id"].(string)
		if !ok {
			return nil, fmt.Errorf("event %d local_id is not a string", index)
		}
		causedBy, err := stringSliceContext(ctx, event["caused_by"], work)
		if err != nil {
			return nil, fmt.Errorf("event %d caused_by: %w", index, err)
		}
		supersedes, err := stringSliceContext(ctx, event["supersedes"], work)
		if err != nil {
			return nil, fmt.Errorf("event %d supersedes: %w", index, err)
		}
		events[index] = storedEvent{localID: localID, causedBy: causedBy, supersedes: supersedes, object: event}
	}
	return events, nil
}

func stringSliceContext(ctx context.Context, value any, work *int) ([]string, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("not an array")
	}
	if err := pollContext(ctx, *work); err != nil {
		return nil, err
	}
	*work++
	result := make([]string, len(raw))
	for index, item := range raw {
		if err := pollContext(ctx, *work); err != nil {
			return nil, err
		}
		*work++
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("item %d is not a string", index)
		}
		result[index] = text
	}
	return result, nil
}
func normalizeAndLimitParents(parents []string) ([]string, error) {
	unique := map[string]struct{}{}
	for _, parent := range parents {
		if !digestPattern.MatchString(parent) {
			return nil, fmt.Errorf("invalid parent ID: %q", parent)
		}
		if _, found := unique[parent]; found {
			continue
		}
		if uint64(len(unique)) == Phase2Limits.ParentsPerCommit {
			return nil, limitError("parents_per_commit", Phase2Limits.ParentsPerCommit)
		}
		unique[parent] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for parent := range unique {
		result = append(result, parent)
	}
	sort.Strings(result)
	return result, nil
}
func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
func validateSigningKey(key *identity.KeyFile) error {
	if utf8.RuneCountInString(key.Actor) == 0 || utf8.RuneCountInString(key.Actor) > 255 {
		return fmt.Errorf("commit actor label is invalid")
	}
	if len(key.Public) != ed25519.PublicKeySize || len(key.Private) != ed25519.PrivateKeySize {
		return fmt.Errorf("commit signing key is invalid")
	}
	expected, err := identity.KeyID(key.Public)
	if err != nil || expected != key.KeyID {
		return fmt.Errorf("commit key ID does not match public key")
	}
	derived, ok := key.Private.Public().(ed25519.PublicKey)
	if !ok || !derived.Equal(key.Public) {
		return fmt.Errorf("commit private/public key mismatch")
	}
	return nil
}
func batchEvents(events []Event) []any {
	result := make([]any, len(events))
	for index, event := range events {
		result[index] = eventValue(event)
	}
	return result
}
func cloneObject(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	maps.Copy(result, value)
	return result
}
