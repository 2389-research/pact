// ABOUTME: Validates the fixed development input contract before signing.
// ABOUTME: Converts caller facts into normalized ledger events without storage access.
package development

import (
	"context"
	"fmt"

	"pact/internal/canonical"
	"pact/internal/ledger"
)

// ValidationError reports a fixed field path and category without caller contents.
// Unwrap preserves cancellation and core error identity for programmatic handling.
type ValidationError struct {
	Path     string
	Category string
	message  string
	cause    error
}

func (err *ValidationError) Error() string {
	return err.Path + ": " + err.Category + ": " + err.message
}
func (err *ValidationError) Unwrap() error { return err.cause }
func invalid(path, category, message string) error {
	return &ValidationError{Path: path, Category: category, message: message}
}
func validationCause(ctx context.Context, category, message string, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return &ValidationError{Path: "$", Category: category, message: message, cause: cause}
}

// DecodeInput reads one strict caller object, rejecting extra fields and duplicate keys.
// Domain validation and normalization remain ValidateInput's responsibility.
func DecodeInput(ctx context.Context, raw []byte) (RecordInput, error) {
	value, err := canonical.ParseContext(ctx, raw)
	if err != nil {
		return RecordInput{}, validationCause(ctx, "json", "expected one record object with operation_id, namespace, subject, source, and events", err)
	}
	if _, object := value.(map[string]any); !object {
		return RecordInput{}, invalid("$", "shape", "expected one record object with operation_id, namespace, subject, source, and events")
	}
	if err := validateShape(ctx, value, recordShape(), "$"); err != nil {
		return RecordInput{}, err
	}
	object, _ := value.(map[string]any)
	source, _ := object["source"].(map[string]any)
	result := RecordInput{OperationID: textValue(object["operation_id"]), Namespace: textValue(object["namespace"]), Subject: textValue(object["subject"]), Source: Source{Repository: textValue(source["repository"]), Revision: textValue(source["revision"])}}
	events, _ := object["events"].([]any)
	result.Events = make([]InputEvent, 0, len(events))
	for _, rawEvent := range events {
		if err := ctx.Err(); err != nil {
			return RecordInput{}, err
		}
		event, _ := rawEvent.(map[string]any)
		payload, _ := event["payload"].(map[string]any)
		evidence, _ := event["evidence"].([]any)
		item := InputEvent{LocalID: textValue(event["local_id"]), Type: textValue(event["type"]), Payload: payload, Evidence: make([]map[string]any, 0, len(evidence)), CausedBy: stringValues(event["caused_by"]), Supersedes: stringValues(event["supersedes"])}
		for _, rawProof := range evidence {
			proof, _ := rawProof.(map[string]any)
			item.Evidence = append(item.Evidence, proof)
		}
		result.Events = append(result.Events, item)
	}
	return result, nil
}

func recordShape() map[string]any {
	event := objectSchema(map[string]FieldDefinition{
		"local_id": required(textSchema()), "type": required(textSchema()),
		"payload":    required(map[string]any{"type": "object"}),
		"evidence":   required(arraySchema(map[string]any{"type": "object"}, 0)),
		"caused_by":  required(arraySchema(map[string]any{"type": "string"}, 0)),
		"supersedes": required(arraySchema(map[string]any{"type": "string"}, 0)),
	})
	return objectSchema(map[string]FieldDefinition{
		"operation_id": required(textSchema()), "namespace": required(textSchema()), "subject": required(textSchema()),
		"source": required(sourceSchema()), "events": required(arraySchema(event, 1)),
	})
}

// ValidateInput validates caller facts, injects profile-owned facts, and applies core rules.
// It neither reads source repositories nor accesses keys, stores, evidence, or indexes.
func ValidateInput(ctx context.Context, input RecordInput) (ledger.EventBatch, error) {
	// Reuse the strict canonical parser for numbers, normalized keys, and fresh nested data.
	value, err := canonicalValue(ctx, inputValue(input))
	if err != nil {
		return ledger.EventBatch{}, err
	}
	if err := validateShape(ctx, value, recordShape(), "$"); err != nil {
		return ledger.EventBatch{}, err
	}
	object, _ := value.(map[string]any)
	events, _ := object["events"].([]any)
	definitions, artifacts := EventDefinitions(), ProfileArtifacts()
	for index, rawEvent := range events {
		if err := ctx.Err(); err != nil {
			return ledger.EventBatch{}, err
		}
		event, _ := rawEvent.(map[string]any)
		path := fmt.Sprintf("$.events[%d]", index)
		definition, found := definitions[textValue(event["type"])]
		if !found {
			return ledger.EventBatch{}, invalid(path+".type", "unsupported_type", "expected a supported development/v1 event type")
		}
		payload, _ := event["payload"].(map[string]any)
		for _, name := range []string{"operation_id", "source"} {
			if _, exists := payload[name]; exists {
				return ledger.EventBatch{}, invalid(path+".payload."+name, "profile_owned", "supply this fact only on the record object")
			}
		}
		payload["operation_id"], payload["source"] = object["operation_id"], object["source"]
		evidence, _ := event["evidence"].([]any)
		if err := validatePayload(ctx, definition, payload, len(evidence), path+".payload"); err != nil {
			return ledger.EventBatch{}, err
		}
		schemaDigest, err := canonical.DigestContext(ctx, artifacts[definition.Artifact])
		if err != nil {
			return ledger.EventBatch{}, err
		}
		event["kind"], event["schema_ref"], event["subject"], event["tags"] = definition.Kind, schemaDigest, object["subject"], []any{}
	}
	batch, err := ledger.NormalizeEventBatchContext(ctx, map[string]any{"namespace": object["namespace"], "events": events})
	if err != nil {
		return ledger.EventBatch{}, validationCause(ctx, "core", "core envelope or immutable secret rules rejected the record", err)
	}
	if err := ctx.Err(); err != nil {
		return ledger.EventBatch{}, err
	}
	return batch, nil
}

// ValidatePayload checks a signed payload only under its exact supported type/schema pair.
// Unknown domains or schemas are unsupported here; raw ledger history remains unchecked.
// It does not establish reference reachability, source existence, evidence, or acceptance.
func ValidatePayload(ctx context.Context, eventType, schemaRef string, payload map[string]any, evidenceCount int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	definition, found := EventDefinitions()[eventType]
	if !found {
		return invalid("$.type", "unsupported_type", "raw unknown domains are unchecked; expected a supported development/v1 type")
	}
	schemaDigest, err := canonical.DigestContext(ctx, ProfileArtifacts()[definition.Artifact])
	if err != nil {
		return err
	}
	if schemaRef != schemaDigest {
		return invalid("$.schema_ref", "unsupported_schema", "expected the exact bundled schema digest for this event type")
	}
	if evidenceCount < 0 {
		return invalid("$.evidence", "bounds", "evidence count must be nonnegative")
	}
	value, err := canonicalValue(ctx, payload)
	if err != nil {
		return err
	}
	object, _ := value.(map[string]any)
	return validatePayload(ctx, definition, object, evidenceCount, "$.payload")
}

func canonicalValue(ctx context.Context, value any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := canonical.MarshalContext(ctx, value)
	if err != nil {
		return nil, validationCause(ctx, "json", "expected canonical JSON values and whole safe integers", err)
	}
	parsed, err := canonical.ParseContext(ctx, raw)
	if err != nil {
		return nil, validationCause(ctx, "json", "expected canonical JSON values and whole safe integers", err)
	}
	return parsed, nil
}

func validatePayload(ctx context.Context, definition EventDefinition, payload map[string]any, evidenceCount int, path string) error {
	if err := validateShape(ctx, payload, objectSchema(definition.Fields), path); err != nil {
		return err
	}
	if _, check := definition.Fields["outcome"]; check {
		if err := validateCheck(payload, path); err != nil {
			return err
		}
	}
	if verdict, review := payload["verdict"]; review {
		findings, _ := payload["findings"].([]any)
		if verdict == "changes_required" && len(findings) == 0 {
			return invalid(path+".findings", "required", "changes_required needs at least one finding")
		}
		ids := map[string]bool{}
		for index, raw := range findings {
			if err := ctx.Err(); err != nil {
				return err
			}
			finding, _ := raw.(map[string]any)
			id := textValue(finding["id"])
			if ids[id] {
				return invalid(fmt.Sprintf("%s.findings[%d].id", path, index), "duplicate", "finding IDs must be unique within a review")
			}
			ids[id] = true
		}
	}
	return validateEvidenceIndexes(ctx, payload, evidenceCount, path)
}

func validateCheck(payload map[string]any, path string) error {
	exit, hasExit := payload["exit_code"]
	code, _ := exit.(int64)
	_, signal := payload["signal"]
	_, interruption := payload["interruption"]
	valid := false
	switch payload["outcome"] {
	case "passed":
		valid = hasExit && code == 0 && !signal && !interruption
	case "failed":
		valid = hasExit && code > 0 && !signal && !interruption
	case "interrupted":
		valid = signal || interruption || hasExit && code != 0
	case "unknown", "not_run":
		valid = !hasExit && !signal && !interruption
	}
	if !valid {
		return invalid(path+".outcome", "contradiction", "outcome requires consistent observed exit and interruption fields")
	}

	if raw, exists := payload["coverage"]; exists {
		coverage, _ := raw.(map[string]any)
		completed, _ := coverage["completed"].(int64)
		total, _ := coverage["total"].(int64)
		if completed > total {
			return invalid(path+".coverage.completed", "bounds", "completed cannot exceed total")
		}
	}
	return nil
}

func validateEvidenceIndexes(ctx context.Context, value any, count int, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch current := value.(type) {
	case map[string]any:
		for name, child := range current {
			if name == "evidence_indexes" {
				indexes, _ := child.([]any)
				for index, item := range indexes {
					number, _ := item.(int64)
					if number >= int64(count) {
						return invalid(fmt.Sprintf("%s.evidence_indexes[%d]", path, index), "bounds", "index must name evidence on this event")
					}
				}
			} else if err := validateEvidenceIndexes(ctx, child, count, path+"."+name); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range current {
			if err := validateEvidenceIndexes(ctx, child, count, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func textValue(value any) string { text, _ := value.(string); return text }
func stringValues(value any) []string {
	raw, _ := value.([]any)
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		result = append(result, textValue(item))
	}
	return result
}

func inputValue(input RecordInput) map[string]any {
	events := make([]any, 0, len(input.Events))
	for _, event := range input.Events {
		var evidence any
		if event.Evidence != nil {
			items := make([]any, len(event.Evidence))
			for index, item := range event.Evidence {
				items[index] = item
			}
			evidence = items
		}
		events = append(events, map[string]any{"local_id": event.LocalID, "type": event.Type, "payload": event.Payload,
			"evidence": evidence, "caused_by": inputRefs(event.CausedBy), "supersedes": inputRefs(event.Supersedes)})
	}
	return map[string]any{"operation_id": input.OperationID, "namespace": input.Namespace, "subject": input.Subject,
		"source": map[string]any{"repository": input.Source.Repository, "revision": input.Source.Revision}, "events": events}
}

func inputRefs(refs []string) any {
	if refs == nil {
		return nil
	}
	result := make([]any, len(refs))
	for index, ref := range refs {
		result[index] = ref
	}
	return result
}
