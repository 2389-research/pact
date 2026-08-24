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

// Verify scans every canonical object and evaluates its layered ledger state.
func Verify(st *store.Store, strict bool) (VerifyResult, error) {
	if st == nil {
		return VerifyResult{}, fmt.Errorf("store is required")
	}
	files, err := st.ObjectFiles()
	if err != nil {
		return VerifyResult{}, err
	}
	result := VerifyResult{Strict: strict, Repo: filepath.Dir(st.Dir()), Store: st.Dir(), IndexStatus: "missing", Heads: map[string][]string{}, Authorization: map[string]AuthorizationResult{}, Objects: map[string]ObjectVerification{}}
	commits := map[string]storedCommit{}
	checkpoints := map[string]ObjectVerification{}
	for _, file := range files {
		verification, err := verifyStoredObject(st, file)
		if err != nil {
			return VerifyResult{}, err
		}
		result.Objects[file.ID] = verification
		result.Counts.Objects++
		for _, message := range verification.Errors {
			result.Errors = append(result.Errors, file.ID+": "+message)
		}
		if verification.Integrity != "valid" {
			result.Counts.Integrity++
			result.Integrity.Errors = append(result.Integrity.Errors, file.ID+": "+strings.Join(verification.Errors, "; "))
		} else if verification.Structure != "valid" {
			result.Counts.Structure++
			result.Structure.Errors = append(result.Structure.Errors, file.ID+": "+strings.Join(verification.Errors, "; "))
		} else if verification.Authenticity != "valid" {
			result.Counts.Authenticity++
			result.Authenticity.Errors = append(result.Authenticity.Errors, file.ID+": "+strings.Join(verification.Errors, "; "))
		}
		for _, message := range verification.Warnings {
			result.Warnings = append(result.Warnings, file.ID+": "+message)
		}
		if verification.Valid() {
			switch verification.Type {
			case "commit":
				body := verification.Object["body"].(map[string]any)
				commits[file.ID] = storedCommit{id: file.ID, body: body, object: verification.Object}
				result.Counts.Commits++
				result.Counts.Events += len(body["events"].([]any))
			case "checkpoint":
				checkpoints[file.ID] = verification
				result.Counts.Checkpoints++
			}
		}
	}
	for id, commit := range commits {
		namespace := commit.body["namespace"].(string)
		for _, parent := range commit.body["parents"].([]any) {
			parentID := parent.(string)
			parentCommit, found := commits[parentID]
			if !found {
				resultDAGAt(&result, strict, fmt.Sprintf("%s: missing or invalid parent %s", id, parentID))
				continue
			}
			if parentCommit.body["namespace"].(string) != namespace {
				message := fmt.Sprintf("%s: parent %s belongs to different namespace %q", id, parentID, parentCommit.body["namespace"].(string))
				result.Errors = append(result.Errors, message)
				result.DAG.Errors = append(result.DAG.Errors, message)
				result.Counts.DAG++
			}
		}
	}
	for id, checkpoint := range checkpoints {
		body := checkpoint.Object["body"].(map[string]any)
		for _, rawEntry := range body["frontier"].([]any) {
			entry := rawEntry.(map[string]any)
			namespace := entry["namespace"].(string)
			for _, rawHead := range entry["heads"].([]any) {
				headID := rawHead.(string)
				head, found := commits[headID]
				if !found {
					resultReferenceError(&result, fmt.Sprintf("%s: missing checkpoint head %s", id, headID))
					continue
				}
				if head.body["namespace"].(string) != namespace {
					resultReferenceError(&result, fmt.Sprintf("%s: head %s namespace mismatch (%q != %q)", id, headID, head.body["namespace"].(string), namespace))
				}
			}
		}
		if previous := body["previous_checkpoint"]; previous != nil {
			if _, found := checkpoints[previous.(string)]; !found {
				resultReferenceAt(&result, strict, fmt.Sprintf("%s: previous checkpoint is unavailable: %s", id, previous.(string)))
			}
		}
	}
	for _, cycle := range commitCycles(commits) {
		message := "commit DAG cycle: " + joinCycle(cycle)
		result.Errors = append(result.Errors, message)
		result.DAG.Errors = append(result.DAG.Errors, message)
		result.Counts.DAG++
	}
	events := map[string]bool{}
	for id, commit := range commits {
		for _, raw := range commit.body["events"].([]any) {
			event := raw.(map[string]any)
			events[EventRef(id, event["local_id"].(string))] = true
		}
	}
	for id, commit := range commits {
		for _, raw := range commit.body["events"].([]any) {
			event := raw.(map[string]any)
			source := EventRef(id, event["local_id"].(string))
			for _, field := range []string{"caused_by", "supersedes"} {
				for _, reference := range event[field].([]any) {
					ref := reference.(string)
					if localRefPattern.MatchString(ref) {
						continue
					}
					if !events[ref] {
						resultReferenceAt(&result, strict, fmt.Sprintf("%s: unresolved %s reference %s", source, field, ref))
					}
				}
			}
		}
	}
	roots, rootErr := Roots(st)
	if rootErr != nil {
		result.Errors = append(result.Errors, "authority evaluation failed: "+rootErr.Error())
	} else {
		for id := range commits {
			verification := result.Objects[id]
			auth := authorizationForRoots(roots, verification)
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
	}
	result.Heads = headsFor(commits, "")
	sort.Strings(result.Errors)
	sort.Strings(result.Warnings)
	for _, layer := range []*LayerResult{&result.Integrity, &result.Structure, &result.Authenticity, &result.DAG, &result.References} {
		sort.Strings(layer.Errors)
		sort.Strings(layer.Warnings)
	}
	result.OK = len(result.Errors) == 0
	return result, nil
}

// Show finds an immutable object or stable event reference without fetching external evidence.
func Show(st *store.Store, identifier string) (ShowResult, error) {
	if st == nil {
		return ShowResult{}, fmt.Errorf("store is required")
	}
	if match := eventRefPattern.FindStringSubmatch(identifier); match != nil {
		verification, err := verificationForID(st, match[1])
		if err != nil {
			return ShowResult{}, err
		}
		if verification.Type != "commit" || verification.Object == nil {
			return ShowResult{}, fmt.Errorf("event reference points to non-commit object: %s", match[1])
		}
		body := verification.Object["body"].(map[string]any)
		for _, raw := range body["events"].([]any) {
			event := raw.(map[string]any)
			if event["local_id"] == match[2] {
				return ShowResult{Identifier: identifier, Kind: "event", CommitID: match[1], Namespace: body["namespace"].(string), Actor: body["actor"].(map[string]any), ObservedAt: body["observed_at"].(string), Event: displayEvent(event, match[1]), Integrity: verification.Integrity, Authenticity: verification.Authenticity, Errors: verification.Errors}, nil
			}
		}
		return ShowResult{}, fmt.Errorf("event not found: %s", identifier)
	}
	if !digestPattern.MatchString(identifier) {
		return ShowResult{}, fmt.Errorf("invalid object ID: %q", identifier)
	}
	verification, err := verificationForID(st, identifier)
	if err != nil {
		return ShowResult{}, err
	}
	return ShowResult{Identifier: identifier, Kind: verification.Type, Object: verification.Object, Integrity: verification.Integrity, Authenticity: verification.Authenticity, Errors: verification.Errors}, nil
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
	return ObjectVerification{}, fmt.Errorf("object not found: %s", id)
}
func verifyStoredObject(_ *store.Store, file store.ObjectFile) (ObjectVerification, error) {
	result := ObjectVerification{ID: file.ID, Path: file.Path, Integrity: "invalid", Structure: "unverified", Authenticity: "unverified"}
	if file.Path == "" {
		return result, fmt.Errorf("object path is required")
	}
	raw, err := os.ReadFile(file.Path)
	if err != nil {
		result.Errors = append(result.Errors, "cannot read object: "+err.Error())
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
	parents, ok := body["parents"].([]any)
	if !ok {
		return "", fmt.Errorf("commit parents must be an array")
	}
	parentList := make([]string, len(parents))
	for index, parent := range parents {
		text, ok := parent.(string)
		if !ok || !digestPattern.MatchString(text) {
			return "", fmt.Errorf("invalid parent ID")
		}
		parentList[index] = text
	}
	sorted := make([]string, len(parentList))
	copy(sorted, parentList)
	sort.Strings(sorted)
	if !reflect.DeepEqual(parentList, sorted) || hasDuplicate(sorted) {
		return "", fmt.Errorf("commit parents are not sorted unique canonical IDs")
	}
	actor, ok := body["actor"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("commit actor must be an object")
	}
	if err := exactKeys(actor, []string{"key_id", "label"}, nil, "$.body.actor"); err != nil {
		return "", err
	}
	actorID, ok := actor["key_id"].(string)
	if !ok || !isKeyID(actorID) {
		return "", fmt.Errorf("invalid key ID")
	}
	label, ok := actor["label"].(string)
	if !ok || label == "" || utf8.RuneCountInString(label) > 255 {
		return "", fmt.Errorf("commit actor label is invalid")
	}
	authority, ok := body["authority"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("commit authority must be an object")
	}
	if err := exactKeys(authority, []string{"delegation_ref", "epoch", "lease_ref"}, nil, "$.body.authority"); err != nil {
		return "", err
	}
	for _, field := range []string{"delegation_ref", "lease_ref"} {
		if value := authority[field]; value != nil {
			text, ok := value.(string)
			if !ok || !eventRefPattern.MatchString(text) {
				return "", fmt.Errorf("invalid event reference")
			}
		}
	}
	if epoch := authority["epoch"]; epoch != nil {
		text, ok := epoch.(string)
		if !ok || text == "" || utf8.RuneCountInString(text) > 255 {
			return "", fmt.Errorf("authority epoch must be string or null")
		}
	}
	observed, ok := body["observed_at"].(string)
	if !ok || observed == "" || utf8.RuneCountInString(observed) > 64 {
		return "", fmt.Errorf("commit observed_at is invalid")
	}
	metadata, ok := body["metadata"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("commit metadata must be an object")
	}
	if correlation, found := body["correlation_id"]; found {
		text, ok := correlation.(string)
		if !ok || utf8.RuneCountInString(text) > 255 {
			return "", fmt.Errorf("commit correlation_id must be a string")
		}
	}
	events, ok := body["events"].([]any)
	if !ok {
		return "", fmt.Errorf("commit events must be an array")
	}
	normalized, err := NormalizeEventBatch(map[string]any{"events": events, "metadata": metadata})
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(events, batchEvents(normalized.Events)) {
		return "", fmt.Errorf("commit events are not in canonical normalized form")
	}
	return namespace, nil
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
	frontier, ok := body["frontier"].([]any)
	if !ok || len(frontier) == 0 {
		return "", fmt.Errorf("checkpoint frontier must contain at least one namespace")
	}
	namespaces := make([]string, len(frontier))
	for index, raw := range frontier {
		entry, ok := raw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("checkpoint frontier[%d] must be an object", index)
		}
		if err := exactKeys(entry, []string{"namespace", "heads"}, nil, fmt.Sprintf("$.body.frontier[%d]", index)); err != nil {
			return "", err
		}
		namespace, ok := entry["namespace"].(string)
		if !ok {
			return "", fmt.Errorf("checkpoint namespace is not canonical")
		}
		if err := validateNamespace(namespace); err != nil {
			return "", err
		}
		if !namespaceInScope(namespace, scope) {
			return "", fmt.Errorf("checkpoint namespace %q is outside scope %q", namespace, scope)
		}
		namespaces[index] = namespace
		heads, ok := entry["heads"].([]any)
		if !ok || len(heads) == 0 {
			return "", fmt.Errorf("checkpoint namespace %s has no heads", namespace)
		}
		headIDs := make([]string, len(heads))
		for headIndex, rawHead := range heads {
			head, ok := rawHead.(string)
			if !ok || !digestPattern.MatchString(head) {
				return "", fmt.Errorf("invalid checkpoint head")
			}
			headIDs[headIndex] = head
		}
		if !sort.StringsAreSorted(headIDs) || hasDuplicate(headIDs) {
			return "", fmt.Errorf("checkpoint heads for %s are not canonical", namespace)
		}
	}
	if !sort.StringsAreSorted(namespaces) || hasDuplicate(namespaces) {
		return "", fmt.Errorf("checkpoint frontier is not sorted by namespace")
	}
	policyRef, ok := body["policy_ref"].(string)
	if !ok || !digestPattern.MatchString(policyRef) {
		return "", fmt.Errorf("invalid policy reference")
	}
	schemaRaw, ok := body["schema_refs"].([]any)
	if !ok {
		return "", fmt.Errorf("checkpoint schema_refs must be an array")
	}
	schemaRefs := make([]string, len(schemaRaw))
	for index, raw := range schemaRaw {
		ref, ok := raw.(string)
		if !ok || !digestPattern.MatchString(ref) {
			return "", fmt.Errorf("invalid schema reference")
		}
		schemaRefs[index] = ref
	}
	if !sort.StringsAreSorted(schemaRefs) || hasDuplicate(schemaRefs) {
		return "", fmt.Errorf("checkpoint schema_refs are not canonical")
	}
	epoch, ok := body["authority_epoch"].(string)
	if !ok || epoch == "" || utf8.RuneCountInString(epoch) > 255 {
		return "", fmt.Errorf("checkpoint authority_epoch is invalid")
	}
	if previous := body["previous_checkpoint"]; previous != nil {
		text, ok := previous.(string)
		if !ok || !digestPattern.MatchString(text) {
			return "", fmt.Errorf("invalid previous checkpoint")
		}
	}
	actor, ok := body["actor"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("checkpoint actor must be an object")
	}
	if err := exactKeys(actor, []string{"key_id", "label"}, nil, "$.body.actor"); err != nil {
		return "", err
	}
	actorID, ok := actor["key_id"].(string)
	if !ok || !isKeyID(actorID) {
		return "", fmt.Errorf("invalid key ID")
	}
	label, ok := actor["label"].(string)
	if !ok || label == "" || utf8.RuneCountInString(label) > 255 {
		return "", fmt.Errorf("checkpoint actor label is invalid")
	}
	observedAt, ok := body["observed_at"].(string)
	if !ok || observedAt == "" || utf8.RuneCountInString(observedAt) > 64 {
		return "", fmt.Errorf("checkpoint observed_at is invalid")
	}
	if _, ok := body["metadata"].(map[string]any); !ok {
		return "", fmt.Errorf("checkpoint metadata must be an object")
	}
	return scope, nil
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
	actor := object["body"].(map[string]any)["actor"].(map[string]any)
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
		if !(character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
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
func authorizationFor(st *store.Store, verification ObjectVerification) string {
	roots, err := Roots(st)
	if err != nil {
		return "indeterminate"
	}
	return authorizationForRoots(roots, verification).Status
}
func authorizationForResult(st *store.Store, verification ObjectVerification) AuthorizationResult {
	roots, err := Roots(st)
	if err != nil {
		return AuthorizationResult{Status: "indeterminate", Reasons: []string{"local trust roots are unavailable"}, Chain: []string{}, LeaseStatus: "not_applicable", Depth: 0}
	}
	return authorizationForRoots(roots, verification)
}
func authorizationReasons(st *store.Store, verification ObjectVerification) []string {
	roots, err := Roots(st)
	if err != nil {
		return []string{"local trust roots are unavailable"}
	}
	return authorizationForRoots(roots, verification).Reasons
}
func authorizationForRoots(roots map[string]Root, verification ObjectVerification) AuthorizationResult {
	actor := verification.Object["body"].(map[string]any)["actor"].(map[string]any)
	keyID := actor["key_id"].(string)
	root, found := roots[keyID]
	if !found {
		return AuthorizationResult{Status: "indeterminate", Reasons: []string{"signer is not a trusted root and no delegation reference was supplied"}, Chain: []string{}, LeaseStatus: "not_applicable", Depth: 0}
	}
	if root.PublicKey != verification.Object["signature"].(map[string]any)["public_key"].(string) {
		return AuthorizationResult{Status: "unauthorized", Reasons: []string{"trusted-root key ID has conflicting public bytes"}, Chain: []string{keyID}, LeaseStatus: "not_applicable", Depth: 0}
	}
	return AuthorizationResult{Status: "authorized", Reasons: []string{"signer is a locally bootstrapped trusted root"}, Chain: []string{keyID}, LeaseStatus: "not_applicable", Depth: 0}
}
func commitCycles(commits map[string]storedCommit) [][]string {
	color := map[string]int{}
	stack := []string{}
	cycles := [][]string{}
	var visit func(string)
	visit = func(id string) {
		color[id] = 1
		stack = append(stack, id)
		for _, raw := range commits[id].body["parents"].([]any) {
			parent := raw.(string)
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
		result := ""
		for _, item := range cycle[1:] {
			result += " -> " + item
		}
		return result
	}()
}
func displayEvent(event map[string]any, commitID string) map[string]any {
	result := cloneObject(event)
	caused := event["caused_by"].([]any)
	expanded := make([]any, len(caused))
	for index, raw := range caused {
		reference := raw.(string)
		if match := localRefPattern.FindStringSubmatch(reference); match != nil {
			expanded[index] = EventRef(commitID, match[1])
		} else {
			expanded[index] = reference
		}
	}
	result["caused_by"] = expanded
	return result
}
