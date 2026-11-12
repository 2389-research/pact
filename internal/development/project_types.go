// ABOUTME: Defines the exact local development projection request and result.
// ABOUTME: Separates signed claims from policy support and observed proof availability.
package development

import "pact/internal/evidence"

type ProjectRequest struct {
	Namespace      string   `json:"namespace"`
	Subject        string   `json:"subject"`
	SourceRevision string   `json:"source_revision"`
	Frontier       []string `json:"frontier"`
}

type ProjectorIdentity struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
}
type Claim struct {
	EventRef string         `json:"event_ref"`
	Source   Source         `json:"source"`
	Payload  map[string]any `json:"payload"`
}
type Finding struct {
	ReviewEventRef string   `json:"review_event_ref"`
	FindingID      string   `json:"finding_id"`
	Source         Source   `json:"source"`
	Severity       string   `json:"severity"`
	Summary        string   `json:"summary"`
	Status         string   `json:"status"`
	ResolvedBy     []string `json:"resolved_by"`
}
type AcceptanceSupport struct {
	Status       string   `json:"status"`
	DecisionRefs []string `json:"decision_refs"`
	Reasons      []string `json:"reasons"`
}
type EvidenceAssessment struct {
	EventRef   string              `json:"event_ref"`
	Index      int                 `json:"index"`
	Assessment evidence.Assessment `json:"assessment"`
	Durable    bool                `json:"durable"`
}
type ProjectionIssue struct {
	EventRef string `json:"event_ref"`
	Code     string `json:"code"`
}
type ProjectionCompleteness struct {
	Scope                string            `json:"scope"`
	Status               string            `json:"status"`
	GlobalCompleteness   string            `json:"global_completeness"`
	Authority            string            `json:"authority"`
	ReviewerIndependence string            `json:"reviewer_independence"`
	Issues               []ProjectionIssue `json:"issues"`
}
type WorkState struct {
	Namespace           string                 `json:"namespace"`
	Subject             string                 `json:"subject"`
	SourceRevision      string                 `json:"source_revision"`
	Frontier            []string               `json:"frontier"`
	ProfileDigest       string                 `json:"profile_digest"`
	PolicyDigest        string                 `json:"policy_digest"`
	ProjectorIdentity   ProjectorIdentity      `json:"projector_identity"`
	Checks              []Claim                `json:"checks"`
	Findings            []Finding              `json:"findings"`
	RecordedDecisions   []Claim                `json:"recorded_decisions"`
	SupportedAcceptance AcceptanceSupport      `json:"supported_acceptance"`
	DeclaredNextActions []Claim                `json:"declared_next_actions"`
	EvidenceAssessments []EvidenceAssessment   `json:"evidence_assessments"`
	Completeness        ProjectionCompleteness `json:"completeness"`
}
