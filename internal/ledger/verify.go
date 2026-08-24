// ABOUTME: Verifies PACT object bytes, signatures, DAG references, and root trust separately.
// ABOUTME: Inspects stored commits without resolving evidence or relying on derived state.
package ledger

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"pact/internal/canonical"
	"pact/internal/identity"
	"pact/internal/store"
)

// ObjectVerification keeps byte integrity and signing authenticity distinct.
type ObjectVerification struct {
	ID           string         `json:"id"`
	Path         string         `json:"path"`
	Type         string         `json:"type"`
	Namespace    string         `json:"namespace"`
	Integrity    string         `json:"integrity"`
	Structure    string         `json:"structure"`
	Authenticity string         `json:"authenticity"`
	Errors       []string       `json:"errors"`
	Warnings     []string       `json:"warnings"`
	Object       map[string]any `json:"-"`
}

// Valid reports whether the object passes integrity, structure, and authenticity.
func (verification ObjectVerification) Valid() bool {
	return verification.Integrity == "valid" && verification.Authenticity == "valid" && len(verification.Errors) == 0
}

// AuthorizationResult is deliberately independent from signature authenticity.
type AuthorizationResult struct {
	Status      string   `json:"status"`
	Reasons     []string `json:"reasons"`
	Chain       []string `json:"chain"`
	LeaseStatus string   `json:"lease_status"`
	Depth       int      `json:"depth"`
}

// VerifyCounts provides separate outcome counts for each verification layer.
type VerifyCounts struct {
	Objects, Commits, Checkpoints, Events, Authorized, Unauthorized, Indeterminate int
	Integrity, Structure, Authenticity, DAG, References                            int
}
type LayerResult struct {
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// VerifyResult contains all verification layers without a collapsed validity signal.
type VerifyResult struct {
	OK            bool
	Strict        bool
	Repo          string
	Store         string
	IndexStatus   string
	Counts        VerifyCounts
	Heads         map[string][]string
	Errors        []string
	Warnings      []string
	Authorization map[string]AuthorizationResult
	Objects       map[string]ObjectVerification
	Integrity     LayerResult
	Structure     LayerResult
	Authenticity  LayerResult
	DAG           LayerResult
	References    LayerResult
}

// ShowResult provides an object or event inspection result without evidence retrieval.
type ShowResult struct {
	Identifier   string
	Kind         string
	CommitID     string
	Namespace    string
	Actor        map[string]any
	ObservedAt   string
	Event        map[string]any
	Object       map[string]any
	Integrity    string
	Authenticity string
	Errors       []string
}

// ShowError preserves inspection details when stored bytes fail integrity or structure checks.
type ShowError struct {
	Result ShowResult
}

func (err *ShowError) Error() string { return "object inspection failed integrity verification" }
func (err *ShowError) Unwrap() error { return ErrIntegrity }

// Verify scans every canonical object and evaluates its layered ledger state.
func Verify(st *store.Store, strict bool) (VerifyResult, error) {
	if st == nil {
		return VerifyResult{}, fmt.Errorf("store is required")
	}
	files, err := st.ObjectFiles()
	if err != nil {
		return VerifyResult{}, err
	}
	result := newVerifyResult(st, strict)
	commits := make(map[string]storedCommit)
	checkpoints := make(map[string]storedCheckpoint)
	for _, file := range files {
		if err := collectVerifiedObject(st, file, &result, commits, checkpoints); err != nil {
			return VerifyResult{}, err
		}
	}
	verifyCommitParents(&result, strict, commits)
	verifyCheckpointReferences(&result, strict, commits, checkpoints)
	verifyCommitCycles(&result, commits)
	verifyEventReferences(&result, strict, commits)
	if err := applyAuthorization(st, &result, commits); err != nil {
		return VerifyResult{}, err
	}
	finishVerification(&result, commits)
	return result, nil
}

type storedCheckpoint struct {
	frontier []CheckpointFrontier
	previous string
}

func storedCheckpointFromObject(object map[string]any) (storedCheckpoint, error) {
	body, ok := object["body"].(map[string]any)
	if !ok {
		return storedCheckpoint{}, fmt.Errorf("body is not an object")
	}
	rawFrontier, ok := body["frontier"].([]any)
	if !ok {
		return storedCheckpoint{}, fmt.Errorf("frontier is not an array")
	}
	frontier := make([]CheckpointFrontier, len(rawFrontier))
	for index, raw := range rawFrontier {
		entry, ok := raw.(map[string]any)
		if !ok {
			return storedCheckpoint{}, fmt.Errorf("frontier %d is not an object", index)
		}
		namespace, ok := entry["namespace"].(string)
		if !ok {
			return storedCheckpoint{}, fmt.Errorf("frontier %d namespace is not a string", index)
		}
		heads, err := stringSlice(entry["heads"])
		if err != nil {
			return storedCheckpoint{}, fmt.Errorf("frontier %d heads: %w", index, err)
		}
		frontier[index] = CheckpointFrontier{Namespace: namespace, Heads: heads}
	}
	previous := ""
	if raw := body["previous_checkpoint"]; raw != nil {
		var ok bool
		previous, ok = raw.(string)
		if !ok {
			return storedCheckpoint{}, fmt.Errorf("previous_checkpoint is not a string")
		}
	}
	return storedCheckpoint{frontier: frontier, previous: previous}, nil
}

func newVerifyResult(st *store.Store, strict bool) VerifyResult {
	return VerifyResult{Strict: strict, Repo: filepath.Dir(st.Dir()), Store: st.Dir(), IndexStatus: "missing", Heads: map[string][]string{}, Authorization: map[string]AuthorizationResult{}, Objects: map[string]ObjectVerification{}}
}

func collectVerifiedObject(st *store.Store, file store.ObjectFile, result *VerifyResult, commits map[string]storedCommit, checkpoints map[string]storedCheckpoint) error {
	verification, err := verifyStoredObject(st, file)
	if err != nil {
		return err
	}
	result.Objects[file.ID] = verification
	result.Counts.Objects++
	for _, message := range verification.Errors {
		result.Errors = append(result.Errors, file.ID+": "+message)
	}
	switch {
	case verification.Integrity != "valid":
		result.Counts.Integrity++
		result.Integrity.Errors = append(result.Integrity.Errors, file.ID+": "+strings.Join(verification.Errors, "; "))
	case verification.Structure != "valid":
		result.Counts.Structure++
		result.Structure.Errors = append(result.Structure.Errors, file.ID+": "+strings.Join(verification.Errors, "; "))
	case verification.Authenticity != "valid":
		result.Counts.Authenticity++
		result.Authenticity.Errors = append(result.Authenticity.Errors, file.ID+": "+strings.Join(verification.Errors, "; "))
	}
	for _, message := range verification.Warnings {
		result.Warnings = append(result.Warnings, file.ID+": "+message)
	}
	if !verification.Valid() {
		return nil
	}
	switch verification.Type {
	case "commit":
		commit, err := storedCommitFromObject(verification.Object)
		if err != nil {
			return fmt.Errorf("validated commit %s has inconsistent shape: %w", file.ID, err)
		}
		commits[file.ID] = commit
		result.Counts.Commits++
		result.Counts.Events += len(commit.events)
	case "checkpoint":
		checkpoint, err := storedCheckpointFromObject(verification.Object)
		if err != nil {
			return fmt.Errorf("validated checkpoint %s has inconsistent shape: %w", file.ID, err)
		}
		checkpoints[file.ID] = checkpoint
		result.Counts.Checkpoints++
	}
	return nil
}

func verifyCommitParents(result *VerifyResult, strict bool, commits map[string]storedCommit) {
	for id, commit := range commits {
		for _, parentID := range commit.parents {
			parentCommit, found := commits[parentID]
			if !found {
				resultDAGAt(result, strict, fmt.Sprintf("%s: missing or invalid parent %s", id, parentID))
				continue
			}
			if parentCommit.namespace != commit.namespace {
				message := fmt.Sprintf("%s: parent %s belongs to different namespace %q", id, parentID, parentCommit.namespace)
				result.Errors = append(result.Errors, message)
				result.DAG.Errors = append(result.DAG.Errors, message)
				result.Counts.DAG++
			}
		}
	}
}

func verifyCheckpointReferences(result *VerifyResult, strict bool, commits map[string]storedCommit, checkpoints map[string]storedCheckpoint) {
	for id, checkpoint := range checkpoints {
		for _, entry := range checkpoint.frontier {
			for _, headID := range entry.Heads {
				head, found := commits[headID]
				if !found {
					resultReferenceError(result, fmt.Sprintf("%s: missing checkpoint head %s", id, headID))
					continue
				}
				if head.namespace != entry.Namespace {
					resultReferenceError(result, fmt.Sprintf("%s: head %s namespace mismatch (%q != %q)", id, headID, head.namespace, entry.Namespace))
				}
			}
		}
		if checkpoint.previous != "" {
			if _, found := checkpoints[checkpoint.previous]; !found {
				resultReferenceAt(result, strict, fmt.Sprintf("%s: previous checkpoint is unavailable: %s", id, checkpoint.previous))
			}
		}
	}
}

func verifyCommitCycles(result *VerifyResult, commits map[string]storedCommit) {
	for _, cycle := range commitCycles(commits) {
		message := "commit DAG cycle: " + joinCycle(cycle)
		result.Errors = append(result.Errors, message)
		result.DAG.Errors = append(result.DAG.Errors, message)
		result.Counts.DAG++
	}
}

func verifyEventReferences(result *VerifyResult, strict bool, commits map[string]storedCommit) {
	events := map[string]bool{}
	for id, commit := range commits {
		for _, event := range commit.events {
			events[EventRef(id, event.localID)] = true
		}
	}
	for id, commit := range commits {
		for _, event := range commit.events {
			source := EventRef(id, event.localID)
			for field, references := range map[string][]string{"caused_by": event.causedBy, "supersedes": event.supersedes} {
				for _, ref := range references {
					if localRefPattern.MatchString(ref) {
						continue
					}
					if !events[ref] {
						resultReferenceAt(result, strict, fmt.Sprintf("%s: unresolved %s reference %s", source, field, ref))
					}
				}
			}
		}
	}
}

func applyAuthorization(st *store.Store, result *VerifyResult, commits map[string]storedCommit) error {
	roots, rootErr := Roots(st)
	if rootErr != nil {
		return rootErr
	}
	for id, commit := range commits {
		auth := authorizationForCommit(roots, commit)
		result.Authorization[id] = auth
		switch auth.Status {
		case "authorized":
			result.Counts.Authorized++
		case "unauthorized":
			result.Counts.Unauthorized++
		default:
			result.Counts.Indeterminate++
		}
	}
	return nil
}

func finishVerification(result *VerifyResult, commits map[string]storedCommit) {
	result.Heads = headsFor(commits, "")
	sort.Strings(result.Errors)
	sort.Strings(result.Warnings)
	for _, layer := range []*LayerResult{&result.Integrity, &result.Structure, &result.Authenticity, &result.DAG, &result.References} {
		sort.Strings(layer.Errors)
		sort.Strings(layer.Warnings)
	}
	result.OK = len(result.Errors) == 0
}

// Show finds an immutable object or stable event reference without fetching external evidence.
func Show(st *store.Store, identifier string) (ShowResult, error) {
	if st == nil {
		return ShowResult{}, fmt.Errorf("store is required")
	}
	match := eventRefPattern.FindStringSubmatch(identifier)
	if match != nil {
		return showEvent(st, identifier, match[1], match[2])
	}
	if !digestPattern.MatchString(identifier) {
		return ShowResult{}, fmt.Errorf("invalid object ID: %q", identifier)
	}
	verification, err := verificationForID(st, identifier)
	if err != nil {
		return ShowResult{}, err
	}
	shown := showResult(identifier, verification)
	if verification.Integrity != "valid" || verification.Structure != "valid" {
		return ShowResult{}, &ShowError{Result: shown}
	}
	return shown, nil
}

func showEvent(st *store.Store, identifier, commitID, localID string) (ShowResult, error) {
	verification, err := verificationForID(st, commitID)
	if err != nil {
		return ShowResult{}, err
	}
	base := showResult(identifier, verification)
	base.CommitID = commitID
	if verification.Integrity != "valid" || verification.Structure != "valid" {
		return ShowResult{}, &ShowError{Result: base}
	}
	if verification.Type != "commit" || verification.Object == nil {
		return ShowResult{}, fmt.Errorf("%w: event reference points to non-commit object: %s", ErrIntegrity, commitID)
	}
	commit, err := storedCommitFromObject(verification.Object)
	if err != nil {
		return ShowResult{}, fmt.Errorf("%w: validated commit has inconsistent shape: %w", ErrIntegrity, err)
	}
	for _, event := range commit.events {
		if event.localID == localID {
			return ShowResult{Identifier: identifier, Kind: "event", CommitID: commitID, Namespace: commit.namespace, Actor: commit.actor, ObservedAt: commit.observed, Event: displayEvent(event, commitID), Integrity: verification.Integrity, Authenticity: verification.Authenticity, Errors: verification.Errors}, nil
		}
	}
	return ShowResult{}, fmt.Errorf("%w: event not found: %s", ErrMissingDependency, identifier)
}

func showResult(identifier string, verification ObjectVerification) ShowResult {
	return ShowResult{Identifier: identifier, Kind: verification.Type, Object: verification.Object, Integrity: verification.Integrity, Authenticity: verification.Authenticity, Errors: verification.Errors}
}

func verificationForID(st *store.Store, id string) (ObjectVerification, error) {
	files, err := st.ObjectFiles()
	if err != nil {
		return ObjectVerification{}, err
	}
	for _, file := range files {
		if file.ID == id {
			return verifyStoredObject(st, file)
		}
	}
	return ObjectVerification{}, fmt.Errorf("%w: object not found: %s", ErrMissingDependency, id)
}
func verifyStoredObject(_ *store.Store, file store.ObjectFile) (ObjectVerification, error) {
	result := ObjectVerification{ID: file.ID, Path: file.Path, Integrity: "invalid", Structure: "unverified", Authenticity: "unverified"}
	if file.Path == "" {
		return result, fmt.Errorf("object path is required")
	}
	raw, err := os.ReadFile(file.Path)
	if err != nil {
		result.Errors = append(result.Errors, "cannot read object: "+err.Error())
		//nolint:nilerr // Verification failures are returned as structured result data for show/verify inspection.
		return result, nil
	}
	if canonical.Digest(raw) != file.ID {
		result.Errors = append(result.Errors, fmt.Sprintf("object digest mismatch: path says %s, bytes say %s", file.ID, canonical.Digest(raw)))
		return result, nil
	}
	parsed, err := canonical.Parse(raw)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.Structure = "invalid"
		return result, nil
	}
	encoded, err := canonical.Marshal(parsed)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	if !bytes.Equal(encoded, raw) {
		result.Errors = append(result.Errors, "object bytes are not canonical pact-json-v1")
		return result, nil
	}
	result.Integrity = "valid"
	object, ok := parsed.(map[string]any)
	if !ok {
		result.Errors = append(result.Errors, "canonical object must be a JSON object")
		result.Structure = "invalid"
		return result, nil
	}
	result.Object = object
	objectType, namespace, err := validateSignedObject(object)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.Structure = "invalid"
		return result, nil
	}
	result.Type = objectType
	result.Namespace = namespace
	result.Structure = "valid"
	if err := verifySignature(object); err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.Authenticity = "invalid"
		return result, nil
	}
	result.Authenticity = "valid"
	return result, nil
}
func validateSignedObject(object map[string]any) (string, string, error) {
	format, ok := object["format"].(string)
	if !ok {
		return "", "", fmt.Errorf("unsupported signed object format: %q", object["format"])
	}
	switch format {
	case commitFormat:
		namespace, err := validateCommitObject(object)
		return "commit", namespace, err
	case checkpointFormat:
		scope, err := validateCheckpointObject(object)
		return "checkpoint", scope, err
	default:
		return "", "", fmt.Errorf("unsupported signed object format: %q", format)
	}
}
func validateCommitObject(object map[string]any) (string, error) {
	if err := exactKeys(object, []string{"format", "body", "body_digest", "signature"}, nil, "$"); err != nil {
		return "", err
	}
	if object["format"] != commitFormat {
		return "", fmt.Errorf("unsupported signed object format: %q", object["format"])
	}
	body, ok := object["body"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("commit body must be an object")
	}
	if err := exactKeys(body, []string{"namespace", "parents", "actor", "authority", "observed_at", "metadata", "events"}, []string{"correlation_id"}, "$.body"); err != nil {
		return "", err
	}
	namespace, ok := body["namespace"].(string)
	if !ok {
		return "", fmt.Errorf("commit namespace is not canonical")
	}
	if err := validateNamespace(namespace); err != nil {
		return "", err
	}
	if err := validateCommitParents(body["parents"]); err != nil {
		return "", err
	}
	if err := validateSignedActor(body["actor"], "commit"); err != nil {
		return "", err
	}
	if err := validateCommitAuthority(body["authority"]); err != nil {
		return "", err
	}
	if err := validateCommitContent(body); err != nil {
		return "", err
	}
	return namespace, nil
}

func validateCommitParents(value any) error {
	parents, ok := value.([]any)
	if !ok {
		return fmt.Errorf("commit parents must be an array")
	}
	parentList := make([]string, len(parents))
	for index, parent := range parents {
		text, ok := parent.(string)
		if !ok || !digestPattern.MatchString(text) {
			return fmt.Errorf("invalid parent ID")
		}
		parentList[index] = text
	}
	sorted := make([]string, len(parentList))
	copy(sorted, parentList)
	sort.Strings(sorted)
	if !reflect.DeepEqual(parentList, sorted) || hasDuplicate(sorted) {
		return fmt.Errorf("commit parents are not sorted unique canonical IDs")
	}
	return nil
}

func validateSignedActor(value any, objectType string) error {
	actor, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s actor must be an object", objectType)
	}
	if err := exactKeys(actor, []string{"key_id", "label"}, nil, "$.body.actor"); err != nil {
		return err
	}
	actorID, ok := actor["key_id"].(string)
	if !ok || !isKeyID(actorID) {
		return fmt.Errorf("invalid key ID")
	}
	label, ok := actor["label"].(string)
	if !ok || label == "" || utf8.RuneCountInString(label) > 255 {
		return fmt.Errorf("%s actor label is invalid", objectType)
	}
	return nil
}

func validateCommitAuthority(value any) error {
	authority, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("commit authority must be an object")
	}
	if err := exactKeys(authority, []string{"delegation_ref", "epoch", "lease_ref"}, nil, "$.body.authority"); err != nil {
		return err
	}
	for _, field := range []string{"delegation_ref", "lease_ref"} {
		if value := authority[field]; value != nil {
			text, ok := value.(string)
			if !ok || !eventRefPattern.MatchString(text) {
				return fmt.Errorf("invalid event reference")
			}
		}
	}
	if epoch := authority["epoch"]; epoch != nil {
		text, ok := epoch.(string)
		if !ok || text == "" || utf8.RuneCountInString(text) > 255 {
			return fmt.Errorf("authority epoch must be string or null")
		}
	}
	return nil
}

func validateCommitContent(body map[string]any) error {
	observed, ok := body["observed_at"].(string)
	if !ok || observed == "" || utf8.RuneCountInString(observed) > 64 {
		return fmt.Errorf("commit observed_at is invalid")
	}
	metadata, ok := body["metadata"].(map[string]any)
	if !ok {
		return fmt.Errorf("commit metadata must be an object")
	}
	if correlation, found := body["correlation_id"]; found {
		text, ok := correlation.(string)
		if !ok || utf8.RuneCountInString(text) > 255 {
			return fmt.Errorf("commit correlation_id must be a string")
		}
	}
	events, ok := body["events"].([]any)
	if !ok {
		return fmt.Errorf("commit events must be an array")
	}
	normalized, err := NormalizeEventBatch(map[string]any{"events": events, "metadata": metadata})
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(events, batchEvents(normalized.Events)) {
		return fmt.Errorf("commit events are not in canonical normalized form")
	}
	return nil
}
func validateCheckpointObject(object map[string]any) (string, error) {
	if err := exactKeys(object, []string{"format", "body", "body_digest", "signature"}, nil, "$"); err != nil {
		return "", err
	}
	if object["format"] != checkpointFormat {
		return "", fmt.Errorf("unsupported signed object format: %q", object["format"])
	}
	body, ok := object["body"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("checkpoint body must be an object")
	}
	if err := exactKeys(body, []string{"scope", "frontier", "policy_ref", "schema_refs", "authority_epoch", "previous_checkpoint", "actor", "observed_at", "metadata"}, nil, "$.body"); err != nil {
		return "", err
	}
	scope, ok := body["scope"].(string)
	if !ok {
		return "", fmt.Errorf("checkpoint scope is not canonical")
	}
	if err := validateNamespace(scope); err != nil {
		return "", err
	}
	if err := validateCheckpointFrontier(body["frontier"], scope); err != nil {
		return "", err
	}
	if err := validateCheckpointReferences(body); err != nil {
		return "", err
	}
	if err := validateSignedActor(body["actor"], "checkpoint"); err != nil {
		return "", err
	}
	if err := validateCheckpointMetadata(body); err != nil {
		return "", err
	}
	return scope, nil
}

func validateCheckpointFrontier(value any, scope string) error {
	frontier, ok := value.([]any)
	if !ok || len(frontier) == 0 {
		return fmt.Errorf("checkpoint frontier must contain at least one namespace")
	}
	namespaces := make([]string, len(frontier))
	for index, raw := range frontier {
		entry, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("checkpoint frontier[%d] must be an object", index)
		}
		if err := exactKeys(entry, []string{"namespace", "heads"}, nil, fmt.Sprintf("$.body.frontier[%d]", index)); err != nil {
			return err
		}
		namespace, ok := entry["namespace"].(string)
		if !ok {
			return fmt.Errorf("checkpoint namespace is not canonical")
		}
		if err := validateNamespace(namespace); err != nil {
			return err
		}
		if !namespaceInScope(namespace, scope) {
			return fmt.Errorf("checkpoint namespace %q is outside scope %q", namespace, scope)
		}
		namespaces[index] = namespace
		heads, ok := entry["heads"].([]any)
		if !ok || len(heads) == 0 {
			return fmt.Errorf("checkpoint namespace %s has no heads", namespace)
		}
		headIDs := make([]string, len(heads))
		for headIndex, rawHead := range heads {
			head, ok := rawHead.(string)
			if !ok || !digestPattern.MatchString(head) {
				return fmt.Errorf("invalid checkpoint head")
			}
			headIDs[headIndex] = head
		}
		if !sort.StringsAreSorted(headIDs) || hasDuplicate(headIDs) {
			return fmt.Errorf("checkpoint heads for %s are not canonical", namespace)
		}
	}
	if !sort.StringsAreSorted(namespaces) || hasDuplicate(namespaces) {
		return fmt.Errorf("checkpoint frontier is not sorted by namespace")
	}
	return nil
}

func validateCheckpointReferences(body map[string]any) error {
	policyRef, ok := body["policy_ref"].(string)
	if !ok || !digestPattern.MatchString(policyRef) {
		return fmt.Errorf("invalid policy reference")
	}
	schemaRaw, ok := body["schema_refs"].([]any)
	if !ok {
		return fmt.Errorf("checkpoint schema_refs must be an array")
	}
	schemaRefs := make([]string, len(schemaRaw))
	for index, raw := range schemaRaw {
		ref, ok := raw.(string)
		if !ok || !digestPattern.MatchString(ref) {
			return fmt.Errorf("invalid schema reference")
		}
		schemaRefs[index] = ref
	}
	if !sort.StringsAreSorted(schemaRefs) || hasDuplicate(schemaRefs) {
		return fmt.Errorf("checkpoint schema_refs are not canonical")
	}
	epoch, ok := body["authority_epoch"].(string)
	if !ok || epoch == "" || utf8.RuneCountInString(epoch) > 255 {
		return fmt.Errorf("checkpoint authority_epoch is invalid")
	}
	if previous := body["previous_checkpoint"]; previous != nil {
		text, ok := previous.(string)
		if !ok || !digestPattern.MatchString(text) {
			return fmt.Errorf("invalid previous checkpoint")
		}
	}
	return nil
}

func validateCheckpointMetadata(body map[string]any) error {
	observedAt, ok := body["observed_at"].(string)
	if !ok || observedAt == "" || utf8.RuneCountInString(observedAt) > 64 {
		return fmt.Errorf("checkpoint observed_at is invalid")
	}
	if _, ok := body["metadata"].(map[string]any); !ok {
		return fmt.Errorf("checkpoint metadata must be an object")
	}
	return nil
}
func verifySignature(object map[string]any) error {
	signature, ok := object["signature"].(map[string]any)
	if !ok {
		return fmt.Errorf("signed object signature is missing or not an object")
	}
	if err := exactKeys(signature, []string{"algorithm", "key_id", "public_key", "value"}, nil, "$.signature"); err != nil {
		return err
	}
	if signature["algorithm"] != "ed25519" {
		return fmt.Errorf("unsupported or missing signature algorithm")
	}
	publicText, ok := signature["public_key"].(string)
	if !ok {
		return fmt.Errorf("invalid base64url value")
	}
	valueText, ok := signature["value"].(string)
	if !ok {
		return fmt.Errorf("invalid base64url value")
	}
	public, err := decodeBase64URL(publicText)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return fmt.Errorf("Ed25519 public key is not 32 bytes")
	}
	signatureBytes, err := decodeBase64URL(valueText)
	if err != nil || len(signatureBytes) != ed25519.SignatureSize {
		return fmt.Errorf("Ed25519 signature is not 64 bytes")
	}
	keyID, err := identity.KeyID(ed25519.PublicKey(public))
	if err != nil {
		return err
	}
	if signature["key_id"] != keyID {
		return fmt.Errorf("signature key ID does not match embedded public key")
	}
	body, ok := object["body"].(map[string]any)
	if !ok {
		return fmt.Errorf("signed object body is missing or not an object")
	}
	actor, ok := body["actor"].(map[string]any)
	if !ok {
		return fmt.Errorf("signed object actor is missing or not an object")
	}
	if actor["key_id"] != keyID {
		return fmt.Errorf("body actor key ID does not match signature key ID")
	}
	bodyDigest, ok := object["body_digest"].(string)
	if !ok || !digestPattern.MatchString(bodyDigest) {
		return fmt.Errorf("body digest is malformed; signature cannot be checked")
	}
	if err := identity.VerifyBody(object["body"], bodyDigest, ed25519.PublicKey(public), signatureBytes); err != nil {
		return err
	}
	return nil
}
func decodeBase64URL(value string) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf("invalid base64url value")
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' {
			return nil, fmt.Errorf("invalid base64url value")
		}
	}
	return base64.RawURLEncoding.DecodeString(value)
}
func isKeyID(value string) bool {
	return len(value) == len("ed25519:")+len("sha256:")+64 && len(value) > 8 && value[:8] == "ed25519:" && digestPattern.MatchString(value[8:])
}
func hasDuplicate(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return true
		}
	}
	return false
}
func resultDAGAt(result *VerifyResult, strict bool, message string) {
	if strict {
		result.Errors = append(result.Errors, message)
		result.DAG.Errors = append(result.DAG.Errors, message)
	} else {
		result.Warnings = append(result.Warnings, message)
		result.DAG.Warnings = append(result.DAG.Warnings, message)
	}
	result.Counts.DAG++
}
func resultReferenceAt(result *VerifyResult, strict bool, message string) {
	if strict {
		result.Errors = append(result.Errors, message)
		result.References.Errors = append(result.References.Errors, message)
	} else {
		result.Warnings = append(result.Warnings, message)
		result.References.Warnings = append(result.References.Warnings, message)
	}
	result.Counts.References++
}
func resultReferenceError(result *VerifyResult, message string) {
	result.Errors = append(result.Errors, message)
	result.References.Errors = append(result.References.Errors, message)
	result.Counts.References++
}
func authorizationForResult(st *store.Store, verification ObjectVerification) AuthorizationResult {
	roots, err := Roots(st)
	if err != nil {
		return AuthorizationResult{Status: "indeterminate", Reasons: []string{"local trust roots are unavailable"}, Chain: []string{}, LeaseStatus: "not_applicable", Depth: 0}
	}
	commit, err := storedCommitFromObject(verification.Object)
	if err != nil {
		return AuthorizationResult{Status: "indeterminate", Reasons: []string{"signed object shape is unavailable"}, Chain: []string{}, LeaseStatus: "not_applicable", Depth: 0}
	}
	return authorizationForCommit(roots, commit)
}
func authorizationForCommit(roots map[string]Root, commit storedCommit) AuthorizationResult {
	root, found := roots[commit.signerID]
	if !found {
		return AuthorizationResult{Status: "indeterminate", Reasons: []string{"signer is not a trusted root and no delegation reference was supplied"}, Chain: []string{}, LeaseStatus: "not_applicable", Depth: 0}
	}
	if root.PublicKey != commit.publicKey {
		return AuthorizationResult{Status: "unauthorized", Reasons: []string{"trusted-root key ID has conflicting public bytes"}, Chain: []string{commit.signerID}, LeaseStatus: "not_applicable", Depth: 0}
	}
	return AuthorizationResult{Status: "authorized", Reasons: []string{"signer is a locally bootstrapped trusted root"}, Chain: []string{commit.signerID}, LeaseStatus: "not_applicable", Depth: 0}
}
func commitCycles(commits map[string]storedCommit) [][]string {
	color := map[string]int{}
	stack := []string{}
	cycles := [][]string{}
	var visit func(string)
	visit = func(id string) {
		color[id] = 1
		stack = append(stack, id)
		for _, parent := range commits[id].parents {
			if _, known := commits[parent]; !known {
				continue
			}
			if color[parent] == 0 {
				visit(parent)
			} else if color[parent] == 1 {
				for index, node := range stack {
					if node == parent {
						cycle := append([]string{}, stack[index:]...)
						cycle = append(cycle, parent)
						cycles = append(cycles, cycle)
						break
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = 2
	}
	ids := make([]string, 0, len(commits))
	for id := range commits {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if color[id] == 0 {
			visit(id)
		}
	}
	return cycles
}
func joinCycle(cycle []string) string {
	return fmt.Sprint(cycle[0]) + func() string {
		var result strings.Builder
		for _, item := range cycle[1:] {
			result.WriteString(" -> " + item)
		}
		return result.String()
	}()
}
func displayEvent(event storedEvent, commitID string) map[string]any {
	result := cloneObject(event.object)
	expanded := make([]any, len(event.causedBy))
	for index, reference := range event.causedBy {
		if match := localRefPattern.FindStringSubmatch(reference); match != nil {
			expanded[index] = EventRef(commitID, match[1])
		} else {
			expanded[index] = reference
		}
	}
	result["caused_by"] = expanded
	return result
}
