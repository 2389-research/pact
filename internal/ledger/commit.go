// ABOUTME: Builds immutable, signed PACT commits and computes local namespace heads.
// ABOUTME: Refuses writes when existing canonical history fails structural checks.
package ledger

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	"pact/internal/canonical"
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
	namespace := options.Namespace
	if namespace == "" {
		namespace = batch.Namespace
	}
	if namespace == "" {
		namespace, err = defaultNamespace(st)
		if err != nil {
			return CommitResult{}, err
		}
	}
	if err := validateNamespace(namespace); err != nil {
		return CommitResult{}, err
	}
	parents := append([]string(nil), options.Parents...)
	if parents == nil {
		parents = headsFor(commits, namespace)[namespace]
	}
	if err := normalizeParents(parents); err != nil {
		return CommitResult{}, err
	}
	parents = uniqueSorted(parents)
	for _, parent := range parents {
		parentObject, found := commits[parent]
		if !found {
			return CommitResult{}, fmt.Errorf("parent commit is unavailable or invalid: %s", parent)
		}
		if parentObject.body["namespace"] != namespace {
			return CommitResult{}, fmt.Errorf("parent %s belongs to namespace %q, not %q", parent, parentObject.body["namespace"], namespace)
		}
	}
	observedAt := options.ObservedAt
	if observedAt == "" {
		observedAt = batch.ObservedAt
	}
	if observedAt == "" {
		observedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if utf8.RuneCountInString(observedAt) > 64 {
		return CommitResult{}, fmt.Errorf("observed_at must be a short timestamp string")
	}
	correlationID := options.CorrelationID
	if correlationID == "" {
		correlationID = batch.CorrelationID
	}
	if utf8.RuneCountInString(correlationID) > 255 {
		return CommitResult{}, fmt.Errorf("correlation ID is too long")
	}
	metadata := batch.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	if _, exists := metadata["producer"]; !exists {
		metadata = cloneObject(metadata)
		metadata["producer"] = "pact-reference-cli/0.1.0"
	}
	body := map[string]any{
		"namespace": namespace, "parents": stringsToAny(parents),
		"actor":       map[string]any{"key_id": key.KeyID, "label": key.Actor},
		"authority":   map[string]any{"delegation_ref": nil, "epoch": nil, "lease_ref": nil},
		"observed_at": observedAt, "metadata": metadata, "events": batchEvents(batch.Events),
	}
	if correlationID != "" {
		body["correlation_id"] = correlationID
	}
	if hazards := scanSecretHazards(body, "$"); len(hazards) != 0 {
		return CommitResult{}, fmt.Errorf("refusing to sign immutable secret-like material: %v", hazards)
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
	return CommitResult{ObjectID: objectID, Created: created, Namespace: namespace, Parents: parents, EventRefs: refs, Integrity: "valid", Authenticity: "valid", Authorization: authorization.Status, AuthorizationReasons: authorization.Reasons, LeaseStatus: authorization.LeaseStatus, Path: verified.Path}, nil
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
	id     string
	body   map[string]any
	object map[string]any
}

func validCommits(st *store.Store) (map[string]storedCommit, []string, error) {
	files, err := st.ObjectFiles()
	if err != nil {
		return nil, nil, err
	}
	commits := map[string]storedCommit{}
	invalid := []string{}
	for _, file := range files {
		verified, err := verifyStoredObject(st, file)
		if err != nil {
			return nil, nil, err
		}
		if !verified.Valid() {
			invalid = append(invalid, file.ID)
			continue
		}
		if verified.Type == "commit" {
			body := verified.Object["body"].(map[string]any)
			commits[file.ID] = storedCommit{id: file.ID, body: body, object: verified.Object}
		}
	}
	return commits, invalid, nil
}
func headsFor(commits map[string]storedCommit, namespace string) map[string][]string {
	available := map[string]map[string]bool{}
	referenced := map[string]map[string]bool{}
	for id, commit := range commits {
		ns := commit.body["namespace"].(string)
		if namespace != "" && ns != namespace {
			continue
		}
		if available[ns] == nil {
			available[ns], referenced[ns] = map[string]bool{}, map[string]bool{}
		}
		available[ns][id] = true
		for _, parent := range commit.body["parents"].([]any) {
			referenced[ns][parent.(string)] = true
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
func normalizeParents(parents []string) error {
	for _, parent := range parents {
		if !digestPattern.MatchString(parent) {
			return fmt.Errorf("invalid parent ID: %q", parent)
		}
	}
	return nil
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
	if !key.Private.Public().(ed25519.PublicKey).Equal(key.Public) {
		return fmt.Errorf("commit private/public key mismatch")
	}
	return nil
}
func defaultNamespace(st *store.Store) (string, error) {
	raw, err := st.ReadLocal("format.json")
	if err != nil {
		return "", err
	}
	value, err := canonical.Parse(raw)
	if err != nil {
		return "", err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("malformed store format")
	}
	namespace, ok := object["default_namespace"].(string)
	if !ok {
		return "", fmt.Errorf("malformed store format")
	}
	return namespace, validateNamespace(namespace)
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
	for key, item := range value {
		result[key] = item
	}
	return result
}
