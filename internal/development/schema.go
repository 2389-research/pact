// ABOUTME: Generates exact bundled development payload schemas and the local policy.
// ABOUTME: Shares closed field and enum definitions with the fixed profile validator.
package development

import (
	"encoding/json"
	"sort"
)

//go:generate go run ./cmd/profilegen -out schema

const (
	// EventReferencePattern includes same-commit references, resolved in the containing commit.
	EventReferencePattern = `^(local:[A-Za-z0-9][A-Za-z0-9._-]{0,127}|pact:event:sha256:[0-9a-f]{64}#[A-Za-z0-9][A-Za-z0-9._-]{0,127})$`
	// RevisionPattern accepts full Git SHA-1 and SHA-256 object names.
	RevisionPattern = `^([0-9a-f]{40}|[0-9a-f]{64})$`
	// DigestPattern names exact retained bytes.
	DigestPattern = `^sha256:[0-9a-f]{64}$`
)

// ProfileArtifacts generates supported exact payload schemas and the named local policy.
// A schema describes the signed payload after operation_id/source injection.
func ProfileArtifacts() map[string][]byte {
	artifacts := map[string][]byte{}
	for eventType, definition := range EventDefinitions() {
		schema := objectSchema(definition.Fields)
		schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
		schema["title"] = Profile + "/" + eventType
		if len(definition.Rules) > 0 {
			schema["allOf"] = definition.Rules
		}
		artifacts[definition.Artifact] = artifactJSON(schema)
	}
	artifacts["local-policy.json"] = artifactJSON(map[string]any{
		"name": "development/v1/local-policy/v1", "profile": Profile,
		"authority": "locally_configured_root", "reviewer_independence": "not_established",
		"requirements": []string{
			"explicit_accepted_decision_for_exact_source", "selected_checks_passed_for_exact_source",
			"selected_reviews_approved_for_exact_source", "selected_evidence_durable_and_matching",
			"no_unresolved_critical_or_important_findings_in_selected_history",
			"no_later_causal_blocker", "no_concurrent_incompatible_claims",
			"known_schemas_and_valid_payloads", "relevant_references_resolved", "known_authorization",
		},
		"minor_findings": "visible", "missing_decision": "not_recorded",
		"unsupported_decision": "preserve_recorded_decision", "completeness": "local_snapshot_only",
	})
	return artifacts
}

func artifactJSON(value map[string]any) []byte {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	} // Fixed definitions contain only JSON values.
	return append(raw, '\n')
}

func objectSchema(fields map[string]FieldDefinition) map[string]any {
	properties := make(map[string]any, len(fields))
	required := []string{}
	for name, field := range fields {
		properties[name] = field.Schema
		if field.Required {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func textSchema() map[string]any { return map[string]any{"type": "string", "minLength": 1} }
func patternSchema(pattern string) map[string]any {
	return map[string]any{"type": "string", "pattern": pattern, "not": map[string]any{"pattern": `\s`}}
}
func enumSchema(values []string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}
func arraySchema(items map[string]any, minimum int) map[string]any {
	return map[string]any{"type": "array", "items": items, "minItems": minimum}
}
func integerSchema() map[string]any { return map[string]any{"type": "integer", "minimum": 0} }
func required(schema map[string]any) FieldDefinition {
	return FieldDefinition{Required: true, Schema: schema}
}
func optional(schema map[string]any) FieldDefinition { return FieldDefinition{Schema: schema} }
func evidenceIndexes(minimum int) map[string]any {
	schema := arraySchema(integerSchema(), minimum)
	schema["uniqueItems"] = true
	return schema
}
func sourceSchema() map[string]any {
	return objectSchema(map[string]FieldDefinition{"repository": required(textSchema()), "revision": required(patternSchema(RevisionPattern))})
}
func findingRefFields() map[string]FieldDefinition {
	return map[string]FieldDefinition{"review_event_ref": required(patternSchema(EventReferencePattern)), "finding_id": required(textSchema())}
}
func commonFields() map[string]FieldDefinition {
	return map[string]FieldDefinition{
		"operation_id": required(textSchema()), "source": required(sourceSchema()), "summary": optional(textSchema()),
	}
}

func checkDefinition() EventDefinition {
	fields := commonFields()
	fields["name"] = required(textSchema())
	fields["command"] = required(arraySchema(map[string]any{"type": "string"}, 1))
	fields["outcome"] = required(enumSchema(CheckOutcomes()))
	fields["exit_code"] = optional(map[string]any{"type": "integer"})
	fields["signal"] = optional(textSchema())
	fields["interruption"] = optional(textSchema())
	fields["evidence_indexes"] = required(evidenceIndexes(0))
	fields["coverage"] = optional(objectSchema(map[string]FieldDefinition{"completed": required(integerSchema()), "total": required(integerSchema())}))
	return EventDefinition{Kind: "observation", Artifact: "check.schema.json", Fields: fields, Rules: checkRules()}
}

func outcomeRule(outcome string, then map[string]any) map[string]any {
	return map[string]any{"if": map[string]any{"properties": map[string]any{"outcome": map[string]any{"const": outcome}}, "required": []string{"outcome"}}, "then": then}
}
func absent(names ...string) map[string]any {
	options := make([]any, 0, len(names))
	for _, name := range names {
		options = append(options, map[string]any{"required": []string{name}})
	}
	return map[string]any{"not": map[string]any{"anyOf": options}}
}
func checkRules() []map[string]any {
	passed := absent("signal", "interruption")
	passed["required"] = []string{"exit_code"}
	passed["properties"] = map[string]any{"exit_code": map[string]any{"const": 0}}
	failed := absent("signal", "interruption")
	failed["required"] = []string{"exit_code"}
	failed["properties"] = map[string]any{"exit_code": map[string]any{"minimum": 1}}
	interrupted := map[string]any{"anyOf": []any{
		map[string]any{"required": []string{"signal"}}, map[string]any{"required": []string{"interruption"}},
		map[string]any{"required": []string{"exit_code"}, "properties": map[string]any{"exit_code": map[string]any{"not": map[string]any{"const": 0}}}},
	}}
	return []map[string]any{
		outcomeRule("passed", passed), outcomeRule("failed", failed), outcomeRule("interrupted", interrupted),
		outcomeRule("not_run", absent("exit_code", "signal", "interruption")), outcomeRule("unknown", absent("exit_code", "signal", "interruption")),
	}
}

func reviewDefinition() EventDefinition {
	fields := commonFields()
	fields["verdict"] = required(enumSchema(ReviewVerdicts()))
	fields["findings"] = required(arraySchema(objectSchema(map[string]FieldDefinition{
		"id": required(textSchema()), "severity": required(enumSchema(FindingSeverities())),
		"summary": required(textSchema()), "evidence_indexes": required(evidenceIndexes(0)),
	}), 0))
	resolution := findingRefFields()
	resolution["correction_source"] = required(sourceSchema())
	resolution["evidence_indexes"] = required(evidenceIndexes(1))
	fields["resolves"] = required(arraySchema(objectSchema(resolution), 0))
	fields["evidence_indexes"] = required(evidenceIndexes(0))
	rules := []map[string]any{{"if": map[string]any{"properties": map[string]any{"verdict": map[string]any{"const": "changes_required"}}, "required": []string{"verdict"}},
		"then": map[string]any{"properties": map[string]any{"findings": map[string]any{"minItems": 1}}}}}
	return EventDefinition{Kind: "observation", Artifact: "review.schema.json", Fields: fields, Rules: rules}
}

func acceptanceDefinition() EventDefinition {
	fields := commonFields()
	fields["decision"] = required(enumSchema(AcceptanceDecisions()))
	fields["rationale"] = required(textSchema())
	fields["policy_ref"] = required(patternSchema(DigestPattern))
	fields["check_refs"] = required(arraySchema(patternSchema(EventReferencePattern), 1))
	fields["review_refs"] = required(arraySchema(patternSchema(EventReferencePattern), 1))
	fields["evidence_indexes"] = required(evidenceIndexes(0))
	return EventDefinition{Kind: "decision", Artifact: "acceptance.schema.json", Fields: fields}
}

func nextActionDefinition() EventDefinition {
	fields := commonFields()
	fields["actor"] = required(textSchema())
	fields["action"] = required(textSchema())
	fields["blocking_event_refs"] = required(arraySchema(patternSchema(EventReferencePattern), 0))
	fields["blocking_findings"] = required(arraySchema(objectSchema(findingRefFields()), 0))
	fields["evidence_indexes"] = required(evidenceIndexes(0))
	return EventDefinition{Kind: "action", Artifact: "next-action.schema.json", Fields: fields}
}
