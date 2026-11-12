// ABOUTME: Defines the fixed development/v1 record input and shared event vocabulary.
// ABOUTME: Keeps domain meaning above PACT's unchanged signed event envelope.
package development

const (
	Profile            = "development/v1"
	CheckExecuted      = "dev.check.executed"
	ReviewCompleted    = "dev.review.completed"
	AcceptanceRecorded = "dev.acceptance.recorded"
	NextActionRecorded = "dev.next_action.recorded"
)

// Source names a repository and a full committed Git object identity.
type Source struct {
	Repository string `json:"repository"`
	Revision   string `json:"revision"`
}

// FindingRef identifies a finding within its original review event.
type FindingRef struct {
	ReviewEventRef string `json:"review_event_ref"`
	FindingID      string `json:"finding_id"`
}

// InputEvent holds caller facts; the profile supplies kind and schema identity.
// Payload excludes operation_id and source, which are injected from RecordInput.
type InputEvent struct {
	LocalID    string           `json:"local_id"`
	Type       string           `json:"type"`
	Payload    map[string]any   `json:"payload"`
	Evidence   []map[string]any `json:"evidence"`
	CausedBy   []string         `json:"caused_by"`
	Supersedes []string         `json:"supersedes"`
}

// RecordInput describes one operation against one exact work subject and source.
type RecordInput struct {
	OperationID string       `json:"operation_id"`
	Namespace   string       `json:"namespace"`
	Subject     string       `json:"subject"`
	Source      Source       `json:"source"`
	Events      []InputEvent `json:"events"`
}

// FieldDefinition is a closed payload field shared by generators and validators.
type FieldDefinition struct {
	Required bool
	Schema   map[string]any
}

// EventDefinition supplies the envelope and fixed payload shape for an event type.
// Rules describe cross-field JSON Schema constraints; domain checks remain separate.
type EventDefinition struct {
	Kind     string
	Artifact string
	Fields   map[string]FieldDefinition
	Rules    []map[string]any
}

// CheckOutcomes returns the sole list of supported process outcomes.
func CheckOutcomes() []string {
	return []string{"passed", "failed", "interrupted", "not_run", "unknown"}
}

// ReviewVerdicts returns the sole list of supported review verdicts.
func ReviewVerdicts() []string { return []string{"approved", "changes_required", "inconclusive"} }

// FindingSeverities returns the sole list of supported finding severities.
func FindingSeverities() []string { return []string{"critical", "important", "minor"} }

// AcceptanceDecisions returns explicit recorded decisions, separate from support.
func AcceptanceDecisions() []string { return []string{"accepted", "rejected"} }

// EventDefinitions returns fresh fixed definitions so callers cannot mutate global state.
func EventDefinitions() map[string]EventDefinition {
	return map[string]EventDefinition{
		CheckExecuted:      checkDefinition(),
		ReviewCompleted:    reviewDefinition(),
		AcceptanceRecorded: acceptanceDefinition(),
		NextActionRecorded: nextActionDefinition(),
	}
}
