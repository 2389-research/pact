// ABOUTME: Builds official signed PACT checkpoints from verified local commit heads.
// ABOUTME: Admits checkpoints only for locally trusted roots after strict store checks.
package ledger

import (
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	"pact/internal/identity"
	"pact/internal/store"

	"golang.org/x/text/unicode/norm"
)

const checkpointFormat = "pact/checkpoint/v1"

// ErrCheckpointAuthorization marks refusal to create an official checkpoint.
var ErrCheckpointAuthorization = errors.New("checkpoint authorization failure")

// CheckpointFrontier binds one namespace to its sorted historical heads.
type CheckpointFrontier struct {
	Namespace string
	Heads     []string
}

// CheckpointOptions controls the official checkpoint body.
type CheckpointOptions struct {
	Scope              string
	PolicyRef          string
	AuthorityEpoch     string
	SchemaRefs         []string
	PreviousCheckpoint string
	Purpose            string
	ObservedAt         string
}

// CheckpointResult reports the immutable official cut without private key data.
type CheckpointResult struct {
	ObjectID             string
	Created              bool
	Scope                string
	Frontier             []CheckpointFrontier
	PolicyRef            string
	SchemaRefs           []string
	AuthorityEpoch       string
	PreviousCheckpoint   string
	Integrity            string
	Authenticity         string
	Authorization        string
	AuthorizationReasons []string
	Path                 string
}

// CheckpointVerificationError preserves the strict verification state that blocked admission.
type CheckpointVerificationError struct {
	Result VerifyResult
}

func (err *CheckpointVerificationError) Error() string {
	return "cannot checkpoint an invalid or incomplete strict frontier"
}

// Unwrap classifies strict checkpoint refusal as an integrity failure.
func (err *CheckpointVerificationError) Unwrap() error { return ErrIntegrity }

// Checkpoint verifies, signs, and persists one official historical frontier.
func Checkpoint(st *store.Store, key *identity.KeyFile, options CheckpointOptions) (CheckpointResult, error) {
	if st == nil || key == nil {
		return CheckpointResult{}, fmt.Errorf("store and key are required")
	}
	if err := validateSigningKey(key); err != nil {
		return CheckpointResult{}, err
	}
	prepared, err := prepareCheckpointOptions(options)
	if err != nil {
		return CheckpointResult{}, err
	}

	publicKey, err := validateCheckpointSigner(st, key)
	if err != nil {
		return CheckpointResult{}, err
	}
	frontier, err := checkpointFrontier(st, options.Scope, prepared.previous)
	if err != nil {
		return CheckpointResult{}, err
	}

	metadata := map[string]any{"producer": "pact-reference-cli/0.1.0"}
	if options.Purpose != "" {
		metadata["purpose"] = norm.NFC.String(options.Purpose)
	}
	body := map[string]any{
		"scope":               options.Scope,
		"frontier":            frontierValue(frontier),
		"policy_ref":          options.PolicyRef,
		"schema_refs":         stringsToAny(prepared.schemaRefs),
		"authority_epoch":     prepared.authorityEpoch,
		"previous_checkpoint": nil,
		"actor":               map[string]any{"key_id": key.KeyID, "label": key.Actor},
		"observed_at":         prepared.observedAt,
		"metadata":            metadata,
	}
	if prepared.previous != "" {
		body["previous_checkpoint"] = prepared.previous
	}
	if hazards := scanSecretHazards(body, "$"); len(hazards) != 0 {
		return CheckpointResult{}, fmt.Errorf("%w: refusing to sign immutable secret-like material: %v", ErrSecretSafety, hazards)
	}
	if err := checkSignedObjectBytes(checkpointFormat, body, key.KeyID, publicKey); err != nil {
		return CheckpointResult{}, err
	}
	bodyDigest, signature, err := identity.SignBody(body, key.Private)
	if err != nil {
		return CheckpointResult{}, err
	}
	object := map[string]any{
		"format":      checkpointFormat,
		"body":        body,
		"body_digest": bodyDigest,
		"signature": map[string]any{
			"algorithm": "ed25519", "key_id": key.KeyID, "public_key": publicKey,
			"value": base64.RawURLEncoding.EncodeToString(signature),
		},
	}
	if _, err := validateCheckpointObject(object); err != nil {
		return CheckpointResult{}, fmt.Errorf("checkpoint preflight structure: %w", err)
	}
	if err := verifySignature(object); err != nil {
		return CheckpointResult{}, fmt.Errorf("checkpoint preflight signature: %w", err)
	}
	objectID, created, err := st.PutCanonical(object)
	if err != nil {
		return CheckpointResult{}, err
	}
	persisted, err := verificationForID(st, objectID)
	if err != nil {
		return CheckpointResult{}, err
	}
	if !persisted.Valid() || persisted.Type != "checkpoint" {
		return CheckpointResult{}, fmt.Errorf("new checkpoint failed post-write verification: %s: %v", objectID, persisted.Errors)
	}
	return CheckpointResult{
		ObjectID: objectID, Created: created, Scope: options.Scope, Frontier: frontier,
		PolicyRef: options.PolicyRef, SchemaRefs: prepared.schemaRefs, AuthorityEpoch: prepared.authorityEpoch,
		PreviousCheckpoint: prepared.previous, Integrity: "valid", Authenticity: "valid",
		Authorization: "authorized", AuthorizationReasons: []string{"checkpoint signer is a locally trusted root"},
		Path: persisted.Path,
	}, nil
}

func validateCheckpointSigner(st *store.Store, key *identity.KeyFile) (string, error) {
	roots, err := Roots(st)
	if err != nil {
		return "", err
	}
	root, trusted := roots[key.KeyID]
	publicKey := base64.RawURLEncoding.EncodeToString(key.Public)
	if !trusted || root.PublicKey != publicKey {
		return "", fmt.Errorf("%w: checkpoint signer must be a locally trusted root", ErrCheckpointAuthorization)
	}
	return publicKey, nil
}

func checkpointFrontier(st *store.Store, scope, previous string) ([]CheckpointFrontier, error) {
	verified, err := Verify(st, true)
	if err != nil {
		return nil, err
	}
	if !verified.OK {
		return nil, &CheckpointVerificationError{Result: verified}
	}
	frontier := make([]CheckpointFrontier, 0)
	for namespace, heads := range verified.Heads {
		if namespaceInScope(namespace, scope) {
			frontier = append(frontier, CheckpointFrontier{Namespace: namespace, Heads: append([]string(nil), heads...)})
		}
	}
	sort.Slice(frontier, func(i, j int) bool { return frontier[i].Namespace < frontier[j].Namespace })
	if len(frontier) == 0 {
		return nil, fmt.Errorf("%w: no commit heads found under checkpoint scope %q", ErrMissingDependency, scope)
	}
	if previous != "" {
		prior, found := verified.Objects[previous]
		if !found || !prior.Valid() || prior.Type != "checkpoint" {
			return nil, fmt.Errorf("%w: previous checkpoint is unavailable or invalid: %s", ErrMissingDependency, previous)
		}
	}
	return frontier, nil
}

type preparedCheckpoint struct {
	schemaRefs     []string
	authorityEpoch string
	previous       string
	observedAt     string
}

func prepareCheckpointOptions(options CheckpointOptions) (preparedCheckpoint, error) {
	if err := validateNamespace(options.Scope); err != nil {
		return preparedCheckpoint{}, err
	}
	if !digestPattern.MatchString(options.PolicyRef) {
		return preparedCheckpoint{}, fmt.Errorf("invalid policy reference: %q", options.PolicyRef)
	}
	schemaRefs := uniqueSorted(options.SchemaRefs)
	for _, schemaRef := range schemaRefs {
		if !digestPattern.MatchString(schemaRef) {
			return preparedCheckpoint{}, fmt.Errorf("invalid schema reference: %q", schemaRef)
		}
	}
	authorityEpoch := norm.NFC.String(options.AuthorityEpoch)
	if authorityEpoch == "" || utf8.RuneCountInString(authorityEpoch) > 255 {
		return preparedCheckpoint{}, fmt.Errorf("checkpoint authority_epoch is invalid")
	}
	previous := options.PreviousCheckpoint
	if previous != "" && !digestPattern.MatchString(previous) {
		return preparedCheckpoint{}, fmt.Errorf("invalid previous checkpoint: %q", previous)
	}
	observedAt := norm.NFC.String(options.ObservedAt)
	if observedAt == "" {
		observedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if utf8.RuneCountInString(observedAt) > 64 {
		return preparedCheckpoint{}, fmt.Errorf("checkpoint observed_at is invalid")
	}
	return preparedCheckpoint{schemaRefs: schemaRefs, authorityEpoch: authorityEpoch, previous: previous, observedAt: observedAt}, nil
}

func namespaceInScope(namespace, scope string) bool {
	return namespace == scope || len(namespace) > len(scope) && namespace[:len(scope)+1] == scope+"/"
}

func frontierValue(frontier []CheckpointFrontier) []any {
	result := make([]any, len(frontier))
	for index, entry := range frontier {
		result[index] = map[string]any{"namespace": entry.Namespace, "heads": stringsToAny(entry.Heads)}
	}
	return result
}
