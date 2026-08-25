// ABOUTME: Verifies PACT object bytes, signatures, DAG references, and root trust separately.
// ABOUTME: Inspects stored commits without resolving evidence or relying on derived state.
package ledger

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
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
	ID                   string   `json:"id"`
	Path                 string   `json:"path"`
	Type                 string   `json:"type"`
	Namespace            string   `json:"namespace"`
	Integrity            string   `json:"integrity"`
	Structure            string   `json:"structure"`
	Authenticity         string   `json:"authenticity"`
	Errors               []string `json:"errors"`
	Warnings             []string `json:"warnings"`
	object               map[string]any
	diagnosticsTruncated bool
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

// LimitsStatus reports the fixed resource profile applied to a successful verification.
type LimitsStatus struct {
	Profile string `json:"profile"`
	Status  string `json:"status"`
}

// VerifyResult contains all verification layers without a collapsed validity signal.
type VerifyResult struct {
	OK                   bool
	Strict               bool
	Repo                 string
	Store                string
	IndexStatus          string
	Counts               VerifyCounts
	Heads                map[string][]string
	Errors               []string
	Warnings             []string
	Authorization        map[string]AuthorizationResult
	Objects              map[string]ObjectVerification
	Integrity            LayerResult
	Structure            LayerResult
	Authenticity         LayerResult
	DAG                  LayerResult
	References           LayerResult
	DiagnosticsTruncated bool
	Completeness         Completeness
	Limits               LimitsStatus
	AuthorityFailed      bool
	diagnosticLimits     Limits
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
	scan, err := scanWithReadLock(context.Background(), st, ScanOptions{Strict: strict, Limits: Phase2Limits})
	if err != nil {
		return VerifyResult{}, err
	}
	return scan.Verification, nil
}

type storedCheckpoint struct {
	frontier []CheckpointFrontier
	previous string
}

func storedCheckpointFromObjectContext(ctx context.Context, object map[string]any) (storedCheckpoint, error) {
	if err := ctx.Err(); err != nil {
		return storedCheckpoint{}, err
	}
	work := 0
	body, ok := object["body"].(map[string]any)
	if !ok {
		return storedCheckpoint{}, fmt.Errorf("body is not an object")
	}
	rawFrontier, ok := body["frontier"].([]any)
	if !ok {
		return storedCheckpoint{}, fmt.Errorf("frontier is not an array")
	}
	if err := pollContext(ctx, work); err != nil {
		return storedCheckpoint{}, err
	}
	work++
	frontier := make([]CheckpointFrontier, len(rawFrontier))
	for index, raw := range rawFrontier {
		if err := pollContext(ctx, work); err != nil {
			return storedCheckpoint{}, err
		}
		work++
		entry, ok := raw.(map[string]any)
		if !ok {
			return storedCheckpoint{}, fmt.Errorf("frontier %d is not an object", index)
		}
		namespace, ok := entry["namespace"].(string)
		if !ok {
			return storedCheckpoint{}, fmt.Errorf("frontier %d namespace is not a string", index)
		}
		heads, err := stringSliceContext(ctx, entry["heads"], &work)
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

func verifyCommitParents(ctx context.Context, result *VerifyResult, strict bool, commits map[string]storedCommit, objects map[string]ObjectVerification) error {
	work := 0
	ids, err := sortedKeysContext(ctx, commits)
	if err != nil {
		return err
	}
	for _, id := range ids {
		commit := commits[id]
		if err := pollContext(ctx, work); err != nil {
			return err
		}
		work++
		for _, parentID := range commit.parents {
			if err := pollContext(ctx, work); err != nil {
				return err
			}
			work++
			parentCommit, found := commits[parentID]
			if !found {
				if _, present := objects[parentID]; present {
					message := fmt.Sprintf("%s: parent target is present but is not a valid commit: %s", id, parentID)
					appendVerificationDiagnostic(result, &result.Errors, message)
					appendVerificationDiagnostic(result, &result.DAG.Errors, message)
					result.Counts.DAG++
					continue
				}
				resultDAGAt(result, strict, fmt.Sprintf("%s: missing or invalid parent %s", id, parentID))
				continue
			}
			if parentCommit.namespace != commit.namespace {
				message := fmt.Sprintf("%s: parent %s belongs to different namespace %q", id, parentID, parentCommit.namespace)
				appendVerificationDiagnostic(result, &result.Errors, message)
				appendVerificationDiagnostic(result, &result.DAG.Errors, message)
				result.Counts.DAG++
			}
		}
	}
	return nil
}

func verifyCheckpointReferences(ctx context.Context, result *VerifyResult, strict bool, commits map[string]storedCommit, checkpoints map[string]storedCheckpoint, objects map[string]ObjectVerification) error {
	work := 0
	ids, err := sortedKeysContext(ctx, checkpoints)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := pollContext(ctx, work); err != nil {
			return err
		}
		work++
		if err := verifyCheckpointReference(ctx, result, strict, id, checkpoints[id], commits, checkpoints, objects, &work); err != nil {
			return err
		}
	}
	return nil
}

func verifyCheckpointReference(ctx context.Context, result *VerifyResult, strict bool, id string, checkpoint storedCheckpoint, commits map[string]storedCommit, checkpoints map[string]storedCheckpoint, objects map[string]ObjectVerification, work *int) error {
	for _, entry := range checkpoint.frontier {
		for _, headID := range entry.Heads {
			if err := pollContext(ctx, *work); err != nil {
				return err
			}
			(*work)++
			head, found := commits[headID]
			switch {
			case found && head.namespace != entry.Namespace:
				resultReferenceError(result, fmt.Sprintf("%s: head %s namespace mismatch (%q != %q)", id, headID, head.namespace, entry.Namespace))
			case !found:
				verifyMissingCheckpointHead(result, strict, id, headID, objects)
			}
		}
	}
	if checkpoint.previous != "" {
		if _, found := checkpoints[checkpoint.previous]; !found {
			if _, present := objects[checkpoint.previous]; present {
				resultReferenceError(result, fmt.Sprintf("%s: previous checkpoint is present but invalid: %s", id, checkpoint.previous))
			} else {
				resultReferenceAt(result, strict, fmt.Sprintf("%s: previous checkpoint is unavailable: %s", id, checkpoint.previous))
			}
		}
	}
	return nil
}

func verifyMissingCheckpointHead(result *VerifyResult, strict bool, id, headID string, objects map[string]ObjectVerification) {
	if _, present := objects[headID]; present {
		resultReferenceError(result, fmt.Sprintf("%s: checkpoint head is present but is not a valid commit: %s", id, headID))
		return
	}
	resultReferenceAt(result, strict, fmt.Sprintf("%s: missing checkpoint head %s", id, headID))
}

func verifyEventReferences(ctx context.Context, result *VerifyResult, strict bool, commits map[string]storedCommit, objects map[string]ObjectVerification) error {
	events, work, err := collectKnownEventRefs(ctx, commits)
	if err != nil {
		return err
	}
	ids, err := sortedKeysContext(ctx, commits)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := pollContext(ctx, work); err != nil {
			return err
		}
		work++
		if err := verifyCommitEventReferences(ctx, result, strict, id, commits[id], events, objects, &work); err != nil {
			return err
		}
	}
	return nil
}

func collectKnownEventRefs(ctx context.Context, commits map[string]storedCommit) (map[string]bool, int, error) {
	events := map[string]bool{}
	work := 0
	ids, err := sortedKeysContext(ctx, commits)
	if err != nil {
		return nil, 0, err
	}
	for _, id := range ids {
		commit := commits[id]
		if err := pollContext(ctx, work); err != nil {
			return nil, 0, err
		}
		work++
		for _, event := range commit.events {
			if err := pollContext(ctx, work); err != nil {
				return nil, 0, err
			}
			work++
			events[EventRef(id, event.localID)] = true
		}
	}
	return events, work, nil
}

func verifyCommitEventReferences(ctx context.Context, result *VerifyResult, strict bool, id string, commit storedCommit, events map[string]bool, objects map[string]ObjectVerification, work *int) error {
	for _, event := range commit.events {
		source := EventRef(id, event.localID)
		if err := verifyEventReferenceList(ctx, result, strict, source, "caused_by", event.causedBy, events, objects, work); err != nil {
			return err
		}
		if err := verifyEventReferenceList(ctx, result, strict, source, "supersedes", event.supersedes, events, objects, work); err != nil {
			return err
		}
	}
	return nil
}

func verifyEventReferenceList(ctx context.Context, result *VerifyResult, strict bool, source, field string, references []string, events map[string]bool, objects map[string]ObjectVerification, work *int) error {
	for _, ref := range references {
		if err := pollContext(ctx, *work); err != nil {
			return err
		}
		(*work)++
		if localRefPattern.MatchString(ref) || events[ref] {
			continue
		}
		if eventTargetObjectPresent(objects, ref) {
			resultReferenceError(result, fmt.Sprintf("%s: %s target is present but invalid: %s", source, field, ref))
			continue
		}
		resultReferenceAt(result, strict, fmt.Sprintf("%s: unresolved %s reference %s", source, field, ref))
	}
	return nil
}

func applyAuthorization(ctx context.Context, st *store.Store, result *VerifyResult, commits map[string]storedCommit) error {
	roots, rootErr := RootsContext(ctx, st)
	if rootErr != nil {
		if errors.Is(rootErr, context.Canceled) || errors.Is(rootErr, context.DeadlineExceeded) {
			return rootErr
		}
		appendVerificationDiagnostic(result, &result.Errors, "authority evaluation failed: "+rootErr.Error())
		result.AuthorityFailed = true
		return authorityContextStatus(ctx)
	}
	work := 0
	ids, err := sortedKeysContext(ctx, commits)
	if err != nil {
		return err
	}
	for _, id := range ids {
		commit := commits[id]
		if err := pollContext(ctx, work); err != nil {
			return err
		}
		work++
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

func authorityContextStatus(ctx context.Context) error { return ctx.Err() }

func finishVerification(ctx context.Context, result *VerifyResult, commits map[string]storedCommit) error {
	heads, err := headsForContext(ctx, commits)
	if err != nil {
		return err
	}
	result.Heads = heads
	sort.Strings(result.Errors)
	sort.Strings(result.Warnings)
	for _, layer := range []*LayerResult{&result.Integrity, &result.Structure, &result.Authenticity, &result.DAG, &result.References} {
		sort.Strings(layer.Errors)
		sort.Strings(layer.Warnings)
	}
	result.OK = len(result.Errors) == 0
	return nil
}

func headsForContext(ctx context.Context, commits map[string]storedCommit) (map[string][]string, error) {
	available := map[string]map[string]bool{}
	referenced := map[string]map[string]bool{}
	ids, err := sortedKeysContext(ctx, commits)
	if err != nil {
		return nil, err
	}
	work := 0
	for _, id := range ids {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		work++
		commit := commits[id]
		if available[commit.namespace] == nil {
			available[commit.namespace] = map[string]bool{}
			referenced[commit.namespace] = map[string]bool{}
		}
		available[commit.namespace][id] = true
		for _, parent := range commit.parents {
			if err := pollContext(ctx, work); err != nil {
				return nil, err
			}
			work++
			referenced[commit.namespace][parent] = true
		}
	}
	result := map[string][]string{}
	namespaces, err := sortedKeysContext(ctx, available)
	if err != nil {
		return nil, err
	}
	for _, namespace := range namespaces {
		candidateIDs, err := sortedKeysContext(ctx, available[namespace])
		if err != nil {
			return nil, err
		}
		for _, id := range candidateIDs {
			if !referenced[namespace][id] {
				result[namespace] = append(result[namespace], id)
			}
		}
	}
	return result, nil
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
	if verification.Type != "commit" || verification.object == nil {
		return ShowResult{}, fmt.Errorf("%w: event reference points to non-commit object: %s", ErrIntegrity, commitID)
	}
	commit, err := storedCommitFromObject(verification.object)
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
	return ShowResult{Identifier: identifier, Kind: verification.Type, Object: verification.object, Integrity: verification.Integrity, Authenticity: verification.Authenticity, Errors: verification.Errors}
}

func verificationForID(st *store.Store, id string) (ObjectVerification, error) {
	hexID := strings.TrimPrefix(id, "sha256:")
	if !digestPattern.MatchString(id) {
		return ObjectVerification{}, fmt.Errorf("invalid object ID: %q", id)
	}
	file := store.ObjectFile{ID: id, Path: filepath.Join(st.Dir(), "objects", "sha256", hexID[:2], hexID[2:]+".json")}
	raw, err := st.GetBounded(id, Phase2Limits.ObjectBytes)
	if err == nil {
		return verifyCanonicalBytes(file, raw), nil
	}
	if os.IsNotExist(err) {
		return ObjectVerification{}, fmt.Errorf("%w: object not found: %s", ErrMissingDependency, id)
	}
	if _, ok := errors.AsType[*store.ObjectByteLimitError](err); ok {
		return ObjectVerification{}, objectLimitError("object_bytes", Phase2Limits.ObjectBytes, id)
	}
	if isPermissionError(err) {
		result := ObjectVerification{ID: id, Path: file.Path, Integrity: "invalid", Structure: "unverified", Authenticity: "unverified"}
		result.Errors = append(result.Errors, "cannot read object: "+err.Error())
		return result, nil
	}
	if !errors.Is(err, store.ErrObjectDigestMismatch) {
		return ObjectVerification{}, err
	}
	// GetBounded intentionally refuses corrupt bytes; show still returns bounded structured integrity details.
	raw, readErr := readBoundedCanonicalPathContext(context.Background(), file.Path, Phase2Limits.ObjectBytes)
	if readErr != nil {
		return ObjectVerification{}, fmt.Errorf("read object: %w", readErr)
	}
	if uint64(len(raw)) > Phase2Limits.ObjectBytes {
		return ObjectVerification{}, objectLimitError("object_bytes", Phase2Limits.ObjectBytes, id)
	}
	return verifyCanonicalBytes(file, raw), nil
}
func verifyCanonicalBytes(file store.ObjectFile, raw []byte) ObjectVerification {
	return finishCanonicalVerification(confirmCanonicalBytes(parseCanonicalBytes(file, raw), raw))
}

func verifyCanonicalBytesWithPreflight(ctx context.Context, file store.ObjectFile, raw []byte, resources *scanResourceCounts, limits Limits) (ObjectVerification, parsedObjectResources, error) {
	result, err := parseCanonicalBytesContext(ctx, file, raw)
	if err != nil {
		return ObjectVerification{}, parsedObjectResources{}, err
	}
	if result.object == nil {
		return result, parsedObjectResources{}, nil
	}
	parsedResources, err := resources.preflightParsedObject(ctx, file.ID, result.object, limits)
	if err != nil {
		return ObjectVerification{}, parsedObjectResources{}, err
	}
	result, err = confirmCanonicalBytesContext(ctx, result, raw)
	if err != nil {
		return ObjectVerification{}, parsedObjectResources{}, err
	}
	result, err = finishCanonicalVerificationContext(ctx, result)
	return result, parsedResources, err
}

func parseCanonicalBytes(file store.ObjectFile, raw []byte) ObjectVerification {
	result, _ := parseCanonicalBytesContext(context.Background(), file, raw)
	return result
}

func parseCanonicalBytesContext(ctx context.Context, file store.ObjectFile, raw []byte) (ObjectVerification, error) {
	result := ObjectVerification{ID: file.ID, Path: file.Path, Integrity: "invalid", Structure: "unverified", Authenticity: "unverified"}
	digest, err := canonical.DigestContext(ctx, raw)
	if err != nil {
		return ObjectVerification{}, err
	}
	if digest != file.ID {
		result.Errors = append(result.Errors, fmt.Sprintf("object digest mismatch: path says %s, bytes say %s", file.ID, digest))
		return result, nil
	}
	parsed, err := canonical.ParseContext(ctx, raw)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return ObjectVerification{}, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return ObjectVerification{}, context.DeadlineExceeded
		}
		var diagnostic interface{ DiagnosticTruncated() bool }
		if errors.As(err, &diagnostic) && diagnostic.DiagnosticTruncated() {
			result.diagnosticsTruncated = true
		}
		result.Errors = append(result.Errors, err.Error())
		result.Structure = "invalid"
		return result, nil
	}
	object, ok := parsed.(map[string]any)
	if !ok {
		return verifyCanonicalNonObjectContext(ctx, result, parsed, raw)
	}
	result.object = object
	return result, nil
}

func verifyCanonicalNonObjectContext(ctx context.Context, result ObjectVerification, parsed any, raw []byte) (ObjectVerification, error) {
	encoded, err := canonical.MarshalContext(ctx, parsed)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ObjectVerification{}, err
		}
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	equal, err := equalBytesContext(ctx, encoded, raw)
	if err != nil {
		return ObjectVerification{}, err
	}
	if !equal {
		result.Errors = append(result.Errors, "object bytes are not canonical pact-json-v1")
		return result, nil
	}
	result.Integrity = "valid"
	result.Errors = append(result.Errors, "canonical object must be a JSON object")
	result.Structure = "invalid"
	return result, nil
}

func confirmCanonicalBytes(result ObjectVerification, raw []byte) ObjectVerification {
	result, _ = confirmCanonicalBytesContext(context.Background(), result, raw)
	return result
}

func confirmCanonicalBytesContext(ctx context.Context, result ObjectVerification, raw []byte) (ObjectVerification, error) {
	if result.object == nil {
		return result, nil
	}
	encoded, err := canonical.MarshalContext(ctx, result.object)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ObjectVerification{}, err
		}
		result.Errors = append(result.Errors, err.Error())
		result.object = nil
		return result, nil
	}
	equal, err := equalBytesContext(ctx, encoded, raw)
	if err != nil {
		return ObjectVerification{}, err
	}
	if !equal {
		result.Errors = append(result.Errors, "object bytes are not canonical pact-json-v1")
		result.object = nil
		return result, nil
	}
	result.Integrity = "valid"
	return result, nil
}

func equalBytesContext(ctx context.Context, left, right []byte) (bool, error) {
	if len(left) != len(right) {
		return false, nil
	}
	for start := 0; start < len(left); start += 256 {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		end := min(start+256, len(left))
		if !bytes.Equal(left[start:end], right[start:end]) {
			return false, nil
		}
	}
	return true, nil
}

func pollRawStructureContext(ctx context.Context, value any) error {
	stack := []any{value}
	work := 0
	for len(stack) != 0 {
		if err := pollContext(ctx, work); err != nil {
			return err
		}
		work++
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch item := current.(type) {
		case []any:
			for _, child := range item {
				if err := pollContext(ctx, work); err != nil {
					return err
				}
				work++
				stack = append(stack, child)
			}
		case map[string]any:
			for _, child := range item {
				if err := pollContext(ctx, work); err != nil {
					return err
				}
				work++
				stack = append(stack, child)
			}
		}
	}
	return nil
}

func finishCanonicalVerification(result ObjectVerification) ObjectVerification {
	result, _ = finishCanonicalVerificationContext(context.Background(), result)
	return result
}

func finishCanonicalVerificationContext(ctx context.Context, result ObjectVerification) (ObjectVerification, error) {
	if result.object == nil || result.Integrity != "valid" {
		return result, nil
	}
	object := result.object
	objectType, namespace, err := validateSignedObjectContext(ctx, object)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ObjectVerification{}, err
	}
	if err != nil {
		var diagnostic *validationDiagnosticError
		if errors.As(err, &diagnostic) && diagnostic.truncated {
			result.diagnosticsTruncated = true
		}
		result.Errors = append(result.Errors, err.Error())
		result.Structure = "invalid"
		return result, nil
	}
	result.Type = objectType
	result.Namespace = namespace
	result.Structure = "valid"
	beforeSignatureVerification()
	if err := verifySignatureContext(ctx, object); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ObjectVerification{}, err
		}
		result.Errors = append(result.Errors, err.Error())
		result.Authenticity = "invalid"
		return result, nil
	}
	result.Authenticity = "valid"
	return result, ctx.Err()
}

var beforeSignatureVerification = func() {}

func validateSignedObjectContext(ctx context.Context, object map[string]any) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	format, ok := object["format"].(string)
	if !ok {
		return "", "", errors.New("unsupported signed object format")
	}
	switch format {
	case commitFormat:
		namespace, err := validateCommitObjectContext(ctx, object)
		return "commit", namespace, err
	case checkpointFormat:
		scope, err := validateCheckpointObjectContext(ctx, object)
		return "checkpoint", scope, err
	default:
		return "", "", quotedValidationError("unsupported signed object format: ", format)
	}
}
func validateCommitObject(object map[string]any) (string, error) {
	return validateCommitObjectContext(context.Background(), object)
}

func validateCommitObjectContext(ctx context.Context, object map[string]any) (string, error) {
	if err := exactKeysContext(ctx, object, []string{"format", "body", "body_digest", "signature"}, nil, "$"); err != nil {
		return "", err
	}
	if object["format"] != commitFormat {
		return "", errors.New("unsupported signed object format")
	}
	body, ok := object["body"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("commit body must be an object")
	}
	if err := exactKeysContext(ctx, body, []string{"namespace", "parents", "actor", "authority", "observed_at", "metadata", "events"}, []string{"correlation_id"}, "$.body"); err != nil {
		return "", err
	}
	namespace, ok := body["namespace"].(string)
	if !ok {
		return "", fmt.Errorf("commit namespace is not canonical")
	}
	if err := validateNamespace(namespace); err != nil {
		return "", err
	}
	if err := validateCommitParentsContext(ctx, body["parents"]); err != nil {
		return "", err
	}
	if err := validateSignedActorContext(ctx, body["actor"], "commit"); err != nil {
		return "", err
	}
	if err := validateCommitAuthorityContext(ctx, body["authority"]); err != nil {
		return "", err
	}
	if err := validateCommitContentContext(ctx, body); err != nil {
		return "", err
	}
	return namespace, nil
}

func validateCommitParentsContext(ctx context.Context, value any) error {
	parents, ok := value.([]any)
	if !ok {
		return fmt.Errorf("commit parents must be an array")
	}
	previous := ""
	for index, parent := range parents {
		if err := pollContext(ctx, index); err != nil {
			return err
		}
		text, ok := parent.(string)
		if !ok || !digestPattern.MatchString(text) {
			return fmt.Errorf("invalid parent ID")
		}
		if index != 0 {
			if previous >= text {
				return fmt.Errorf("commit parents are not sorted unique canonical IDs")
			}
		}
		previous = text
	}
	return nil
}

func validateSignedActorContext(ctx context.Context, value any, objectType string) error {
	actor, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s actor must be an object", objectType)
	}
	if err := exactKeysContext(ctx, actor, []string{"key_id", "label"}, nil, "$.body.actor"); err != nil {
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

func validateCommitAuthorityContext(ctx context.Context, value any) error {
	authority, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("commit authority must be an object")
	}
	if err := exactKeysContext(ctx, authority, []string{"delegation_ref", "epoch", "lease_ref"}, nil, "$.body.authority"); err != nil {
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

func validateCommitContentContext(ctx context.Context, body map[string]any) error {
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
	if err := pollRawStructureContext(ctx, events); err != nil {
		return err
	}
	normalized, err := normalizeEventBatchContext(ctx, map[string]any{"events": events, "metadata": metadata})
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(events, batchEvents(normalized.Events)) {
		return fmt.Errorf("commit events are not in canonical normalized form")
	}
	return nil
}
func validateCheckpointObject(object map[string]any) (string, error) {
	return validateCheckpointObjectContext(context.Background(), object)
}

func validateCheckpointObjectContext(ctx context.Context, object map[string]any) (string, error) {
	if err := exactKeysContext(ctx, object, []string{"format", "body", "body_digest", "signature"}, nil, "$"); err != nil {
		return "", err
	}
	if object["format"] != checkpointFormat {
		return "", errors.New("unsupported signed object format")
	}
	body, ok := object["body"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("checkpoint body must be an object")
	}
	if err := exactKeysContext(ctx, body, []string{"scope", "frontier", "policy_ref", "schema_refs", "authority_epoch", "previous_checkpoint", "actor", "observed_at", "metadata"}, nil, "$.body"); err != nil {
		return "", err
	}
	scope, ok := body["scope"].(string)
	if !ok {
		return "", fmt.Errorf("checkpoint scope is not canonical")
	}
	if err := validateNamespace(scope); err != nil {
		return "", err
	}
	if err := validateCheckpointFrontierContext(ctx, body["frontier"], scope); err != nil {
		return "", err
	}
	if err := validateCheckpointReferencesContext(ctx, body); err != nil {
		return "", err
	}
	if err := validateSignedActorContext(ctx, body["actor"], "checkpoint"); err != nil {
		return "", err
	}
	if err := validateCheckpointMetadata(body); err != nil {
		return "", err
	}
	return scope, nil
}

func validateCheckpointFrontierContext(ctx context.Context, value any, scope string) error {
	frontier, ok := value.([]any)
	if !ok || len(frontier) == 0 {
		return fmt.Errorf("checkpoint frontier must contain at least one namespace")
	}
	previousNamespace := ""
	work := 0
	for index, raw := range frontier {
		if err := pollContext(ctx, work); err != nil {
			return err
		}
		work++
		namespace, err := validateCheckpointFrontierEntry(ctx, raw, index, scope, &work)
		if err != nil {
			return err
		}
		if index != 0 && previousNamespace >= namespace {
			return fmt.Errorf("checkpoint frontier is not sorted by namespace")
		}
		previousNamespace = namespace
	}
	return nil
}

func validateCheckpointFrontierEntry(ctx context.Context, raw any, index int, scope string, work *int) (string, error) {
	entry, ok := raw.(map[string]any)
	if !ok {
		return "", fmt.Errorf("checkpoint frontier[%d] must be an object", index)
	}
	if err := exactKeysContext(ctx, entry, []string{"namespace", "heads"}, nil, fmt.Sprintf("$.body.frontier[%d]", index)); err != nil {
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
	heads, ok := entry["heads"].([]any)
	if !ok || len(heads) == 0 {
		return "", fmt.Errorf("checkpoint namespace %s has no heads", namespace)
	}
	previousHead := ""
	for headIndex, rawHead := range heads {
		if err := pollContext(ctx, *work); err != nil {
			return "", err
		}
		(*work)++
		head, ok := rawHead.(string)
		if !ok || !digestPattern.MatchString(head) {
			return "", fmt.Errorf("invalid checkpoint head")
		}
		if headIndex != 0 && previousHead >= head {
			return "", fmt.Errorf("checkpoint heads for %s are not canonical", namespace)
		}
		previousHead = head
	}
	return namespace, nil
}

func validateCheckpointReferencesContext(ctx context.Context, body map[string]any) error {
	policyRef, ok := body["policy_ref"].(string)
	if !ok || !digestPattern.MatchString(policyRef) {
		return fmt.Errorf("invalid policy reference")
	}
	schemaRaw, ok := body["schema_refs"].([]any)
	if !ok {
		return fmt.Errorf("checkpoint schema_refs must be an array")
	}
	previousSchema := ""
	for index, raw := range schemaRaw {
		if err := pollContext(ctx, index); err != nil {
			return err
		}
		ref, ok := raw.(string)
		if !ok || !digestPattern.MatchString(ref) {
			return fmt.Errorf("invalid schema reference")
		}
		if index != 0 && previousSchema >= ref {
			return fmt.Errorf("checkpoint schema_refs are not canonical")
		}
		previousSchema = ref
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
	return verifySignatureContext(context.Background(), object)
}

func verifySignatureContext(ctx context.Context, object map[string]any) error {
	signature, ok := object["signature"].(map[string]any)
	if !ok {
		return fmt.Errorf("signed object signature is missing or not an object")
	}
	if err := exactKeysContext(ctx, signature, []string{"algorithm", "key_id", "public_key", "value"}, nil, "$.signature"); err != nil {
		return err
	}
	if signature["algorithm"] != "ed25519" {
		return fmt.Errorf("unsupported or missing signature algorithm")
	}
	public, signatureBytes, err := decodeSignatureMaterialContext(ctx, signature)
	if err != nil {
		return err
	}
	// KeyID hashes exactly 32 admitted bytes; there is no unbounded work to cancel.
	keyID, err := identity.KeyID(ed25519.PublicKey(public)) //nolint:contextcheck
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
	return identity.VerifyBodyContext(ctx, object["body"], bodyDigest, ed25519.PublicKey(public), signatureBytes)
}

func decodeSignatureMaterialContext(ctx context.Context, signature map[string]any) ([]byte, []byte, error) {
	publicText, ok := signature["public_key"].(string)
	if !ok {
		return nil, nil, fmt.Errorf("invalid base64url value")
	}
	valueText, ok := signature["value"].(string)
	if !ok {
		return nil, nil, fmt.Errorf("invalid base64url value")
	}
	if len(publicText) != base64.RawURLEncoding.EncodedLen(ed25519.PublicKeySize) {
		return nil, nil, fmt.Errorf("Ed25519 public key is not 32 bytes")
	}
	public, err := decodeBase64URLContext(ctx, publicText, ed25519.PublicKeySize)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, nil, err
	}
	if err != nil || len(public) != ed25519.PublicKeySize {
		return nil, nil, fmt.Errorf("Ed25519 public key is not 32 bytes")
	}
	if len(valueText) != base64.RawURLEncoding.EncodedLen(ed25519.SignatureSize) {
		return nil, nil, fmt.Errorf("Ed25519 signature is not 64 bytes")
	}
	signatureBytes, err := decodeBase64URLContext(ctx, valueText, ed25519.SignatureSize)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, nil, err
	}
	if err != nil || len(signatureBytes) != ed25519.SignatureSize {
		return nil, nil, fmt.Errorf("Ed25519 signature is not 64 bytes")
	}
	return public, signatureBytes, nil
}

var beforeSignatureBase64Decode = func() {}

func decodeBase64URLContext(ctx context.Context, value string, decodedLength int) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf("invalid base64url value")
	}
	if len(value) != base64.RawURLEncoding.EncodedLen(decodedLength) {
		return nil, fmt.Errorf("invalid base64url value")
	}
	for index, character := range value {
		if err := pollContext(ctx, index); err != nil {
			return nil, err
		}
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' {
			return nil, fmt.Errorf("invalid base64url value")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	beforeSignatureBase64Decode()
	return base64.RawURLEncoding.DecodeString(value)
}
func isKeyID(value string) bool {
	return len(value) == len("ed25519:")+len("sha256:")+64 && len(value) > 8 && value[:8] == "ed25519:" && digestPattern.MatchString(value[8:])
}
func resultDAGAt(result *VerifyResult, strict bool, message string) {
	if strict {
		appendVerificationDiagnostic(result, &result.Errors, message)
		appendVerificationDiagnostic(result, &result.DAG.Errors, message)
	} else {
		appendVerificationDiagnostic(result, &result.Warnings, message)
		appendVerificationDiagnostic(result, &result.DAG.Warnings, message)
	}
	result.Counts.DAG++
}
func resultReferenceAt(result *VerifyResult, strict bool, message string) {
	if strict {
		appendVerificationDiagnostic(result, &result.Errors, message)
		appendVerificationDiagnostic(result, &result.References.Errors, message)
	} else {
		appendVerificationDiagnostic(result, &result.Warnings, message)
		appendVerificationDiagnostic(result, &result.References.Warnings, message)
	}
	result.Counts.References++
}
func resultReferenceError(result *VerifyResult, message string) {
	appendVerificationDiagnostic(result, &result.Errors, message)
	appendVerificationDiagnostic(result, &result.References.Errors, message)
	result.Counts.References++
}
func authorizationForResult(st *store.Store, verification ObjectVerification) AuthorizationResult {
	roots, err := Roots(st)
	if err != nil {
		return AuthorizationResult{Status: "indeterminate", Reasons: []string{"local trust roots are unavailable"}, Chain: []string{}, LeaseStatus: "not_applicable", Depth: 0}
	}
	commit, err := storedCommitFromObject(verification.object)
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
