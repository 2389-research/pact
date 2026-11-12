// ABOUTME: Records validated development facts and recovers exact retries from signed history.
// ABOUTME: Reports admission, evidence observations, and index readiness as distinct outcomes.
package workflow

import (
	"context"
	"errors"
	"fmt"

	"pact/internal/development"
	"pact/internal/evidence"
	"pact/internal/identity"
	"pact/internal/index"
	"pact/internal/ledger"
	"pact/internal/store"
)

// RecordRequest selects an initialized repository, existing signer, and reported facts.
type RecordRequest struct {
	Repo, KeyFile string
	Input         development.RecordInput
}

// RecordResult separates durable admission from current evidence and derived readiness.
type RecordResult struct {
	Admission            string                `json:"admission"`
	ObjectID             string                `json:"object_id"`
	EventRefs            []string              `json:"event_refs"`
	SchemaValidation     string                `json:"schema_validation"`
	Integrity            string                `json:"integrity"`
	Authenticity         string                `json:"authenticity"`
	Authorization        string                `json:"authorization"`
	IndexState           string                `json:"index_state"`
	SourceFingerprint    string                `json:"source_fingerprint"`
	NextAction           string                `json:"next_action"`
	Evidence             []evidence.Assessment `json:"evidence"`
	EvidenceState        string                `json:"evidence_state"`
	ConflictingObjectIDs []string              `json:"conflicting_object_ids"`
}

// ErrEvidence means new admission requires matching selected evidence bytes.
var ErrEvidence = errors.New("record requires matching evidence")

// Record validates before key access, reconciles under an operation lock, and refreshes the index.
func Record(ctx context.Context, request RecordRequest) (RecordResult, error) {
	result := RecordResult{Admission: "unknown", EventRefs: []string{}, SchemaValidation: "unchecked", Integrity: "unchecked", Authenticity: "unchecked", Authorization: "indeterminate", IndexState: "unchecked", Evidence: []evidence.Assessment{}, EvidenceState: "unchecked", ConflictingObjectIDs: []string{}, NextAction: "Retry the same input and operation_id to reconcile canonical history."}
	if ctx == nil {
		return result, fmt.Errorf("context is required")
	}
	batch, err := development.ValidateInput(ctx, request.Input)
	if err != nil {
		result.SchemaValidation = "invalid"
		return result, err
	}
	result.SchemaValidation = "valid"
	//nolint:contextcheck // store owns the validated opener and has no context-aware variant.
	st, err := store.Open(request.Repo)
	if err != nil {
		return result, err
	}
	//nolint:contextcheck // identity owns the validated signing-key loader and has no context-aware variant.
	key, err := identity.LoadSigningKey(request.KeyFile, st.Root())
	if err != nil {
		return result, err
	}
	digest, err := digestValue(ctx, batchFacts(batch))
	if err != nil {
		return result, err
	}
	// Validation normalized operation identity in every payload; use those exact signed facts.
	operation, _ := batch.Events[0].Payload["operation_id"].(string)
	lock, err := operationLock(ctx, st, batch.Namespace, key.KeyID, operation)
	if err != nil {
		return result, err
	}
	admissionErr := admit(ctx, st, key, batch, operation, digest, &result)
	closeErr := lock.Close() // Closing releases flock; never unlink an active lock inode.
	if closeErr != nil {
		admissionErr = errors.Join(admissionErr, fmt.Errorf("release operation lock: %w", closeErr))
	}
	if admissionErr != nil {
		return result, admissionErr
	}
	return refreshIndex(ctx, st, result)
}

func admit(ctx context.Context, st *store.Store, key *identity.KeyFile, batch ledger.EventBatch, operation, digest string, result *RecordResult) error {
	if err := reconcile(ctx, st, key.KeyID, operation, digest, result, batch.Namespace); err != nil {
		return err
	}
	existing := result.Admission == "existing"
	evidenceErr := assessEvidence(ctx, st.Root(), batch, result)
	if !existing && evidenceErr != nil {
		return evidenceErr
	}
	if existing {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	batch.Metadata = map[string]any{"workflow": map[string]any{"profile": development.Profile, "operation_id": operation, "request_digest": digest}}
	//nolint:contextcheck // ledger owns signing/admission and has no context-aware variant; reconcile errors independently.
	committed, commitErr := ledger.Commit(st, key, batch, ledger.CommitOptions{Namespace: batch.Namespace})
	if commitErr == nil {
		result.Admission, result.ObjectID, result.EventRefs = "created", committed.ObjectID, committed.EventRefs
		result.Integrity, result.Authenticity, result.Authorization = committed.Integrity, committed.Authenticity, committed.Authorization
		result.IndexState = "pending"
		result.SourceFingerprint = ""
		return nil
	}
	// Commit may lose its response after publication. Reconcile even if the caller canceled.
	// This bounded owner scan is intentionally independent of caller cancellation.
	recoveryErr := reconcile(context.WithoutCancel(ctx), st, key.KeyID, operation, digest, result, batch.Namespace)
	if recoveryErr != nil {
		return fmt.Errorf("admission outcome unknown; retry exact input: %w", errors.Join(commitErr, recoveryErr))
	}
	if result.Admission == "existing" {
		result.IndexState = "pending"
		return nil
	}
	return fmt.Errorf("admission outcome unknown; retry exact input: %w", commitErr)
}

func assessEvidence(ctx context.Context, repo string, batch ledger.EventBatch, result *RecordResult) error {
	requests := []evidence.Request{}
	for _, event := range batch.Events {
		for _, item := range event.Evidence {
			ref, _ := item["ref"].(string)
			digest, _ := item["digest"].(string)
			requests = append(requests, evidence.Request{Repo: repo, Ref: ref, Digest: digest})
		}
	}
	assessments, err := evidence.ResolveAll(ctx, requests)
	if err != nil {
		result.EvidenceState = "unavailable"
		switch {
		case errors.Is(err, evidence.ErrLimit):
			result.EvidenceState = "limit_exceeded"
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			result.EvidenceState = "interrupted"
		default:
			var processError *evidence.ProcessError
			if errors.As(err, &processError) {
				result.EvidenceState = "process_failed"
			}
		}
		return err
	}
	result.Evidence, result.EvidenceState = assessments, "matching"
	for _, assessment := range assessments {
		if assessment.Status != "matching" {
			result.EvidenceState = "not_matching"
			return ErrEvidence
		}
	}
	return nil
}

func refreshIndex(ctx context.Context, st *store.Store, result RecordResult) (RecordResult, error) {
	result.IndexState = "pending"
	result.NextAction = "Run pact index rebuild, or retry this exact record input."
	rebuilt, err := index.New(st).Rebuild(ctx)
	if err != nil {
		return result, fmt.Errorf("operation admitted; index refresh pending: %w", err)
	}
	result.IndexState = rebuilt.Index.State
	if rebuilt.Index.SourceFingerprint != nil {
		result.SourceFingerprint = *rebuilt.Index.SourceFingerprint
	}
	result.NextAction = ""
	return result, nil
}
