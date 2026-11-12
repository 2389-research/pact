// ABOUTME: Evaluates the fixed local development acceptance policy over exact signed facts.
// ABOUTME: Keeps explicit decisions, finding resolution, and local support distinct.
package development

import (
	"embed"
	"sort"

	"pact/internal/canonical"
)

//go:embed project.go project_types.go policy.go
var projectorSources embed.FS

var projectionIdentity = bundledProjectionIdentity()

func bundledProjectionIdentity() [3]string {
	artifacts := ProfileArtifacts()
	manifest := map[string]any{}
	for name, raw := range artifacts {
		manifest[name] = canonical.Digest(raw)
	}
	raw, _ := canonical.Marshal(manifest)
	sources := map[string]any{}
	for _, name := range []string{"project.go", "project_types.go", "policy.go"} {
		content, err := projectorSources.ReadFile(name)
		if err != nil {
			panic(err)
		}
		sources[name] = canonical.Digest(content)
	}
	sourceBytes, _ := canonical.Marshal(sources)
	return [3]string{canonical.Digest(raw), canonical.Digest(artifacts["local-policy.json"]), canonical.Digest(sourceBytes)}
}

func emptyWorkState(request ProjectRequest) WorkState {
	return WorkState{Namespace: request.Namespace, Subject: request.Subject, SourceRevision: request.SourceRevision, Frontier: []string{}, ProfileDigest: projectionIdentity[0], PolicyDigest: projectionIdentity[1], ProjectorIdentity: ProjectorIdentity{"development/projector/v1", projectionIdentity[2]}, Checks: []Claim{}, Findings: []Finding{}, RecordedDecisions: []Claim{}, DeclaredNextActions: []Claim{}, EvidenceAssessments: []EvidenceAssessment{}, SupportedAcceptance: AcceptanceSupport{Status: "not_recorded", DecisionRefs: []string{}, Reasons: []string{}}, Completeness: ProjectionCompleteness{Scope: "selected_local_frontier", Status: "locally_closed", GlobalCompleteness: "unknown", Authority: "locally_configured_root", ReviewerIndependence: "not_established", Issues: []ProjectionIssue{}}}
}
func (p *projection) projectClaims() {
	for _, ref := range p.refs {
		if err := p.ctx.Err(); err != nil {
			p.err = err
			return
		}
		e := p.events[ref]
		if !e.valid {
			continue
		}
		p.checkReferences(e)
		switch e.record.Type {
		case CheckExecuted:
			if e.claim.Source.Revision == p.state.SourceRevision {
				p.state.Checks = append(p.state.Checks, e.claim)
			}
		case AcceptanceRecorded:
			if e.claim.Source.Revision == p.state.SourceRevision {
				p.state.RecordedDecisions = append(p.state.RecordedDecisions, e.claim)
			}
		case NextActionRecorded:
			p.state.DeclaredNextActions = append(p.state.DeclaredNextActions, e.claim)
		case ReviewCompleted:
			findings, _ := e.claim.Payload["findings"].([]any)
			for _, raw := range findings {
				finding, _ := raw.(map[string]any)
				p.state.Findings = append(p.state.Findings, Finding{ReviewEventRef: ref, FindingID: textValue(finding["id"]), Source: e.claim.Source, Severity: textValue(finding["severity"]), Summary: textValue(finding["summary"]), Status: "open", ResolvedBy: []string{}})
			}
		}
	}
}
func (p *projection) checkReferences(e *projectedEvent) {
	refs := append([]string{}, e.record.Supersedes...)
	for _, key := range []string{"check_refs", "review_refs", "blocking_event_refs"} {
		refs = append(refs, stringValues(e.claim.Payload[key])...)
	}
	for _, ref := range refs {
		target := p.events[exactRef(e, ref)]
		if target == nil || !p.relevant(target) || !target.valid {
			p.issue(e.record.Ref, "unresolved_reference")
		}
	}
	findings, _ := e.claim.Payload["blocking_findings"].([]any)
	for _, raw := range findings {
		finding, _ := raw.(map[string]any)
		if !p.findingExists(exactRef(e, textValue(finding["review_event_ref"])), textValue(finding["finding_id"])) {
			p.issue(e.record.Ref, "unresolved_finding")
		}
	}
}
func (p *projection) findingExists(ref, id string) bool {
	e := p.events[ref]
	if e == nil || !e.valid || !p.relevant(e) || e.record.Type != ReviewCompleted {
		return false
	}
	findings, _ := e.claim.Payload["findings"].([]any)
	for _, raw := range findings {
		finding, _ := raw.(map[string]any)
		if textValue(finding["id"]) == id {
			return true
		}
	}
	return false
}
func (p *projection) resolveFindings() {
	for _, ref := range p.refs {
		if err := p.ctx.Err(); err != nil {
			p.err = err
			return
		}
		e := p.events[ref]
		if !e.valid || e.record.Type != ReviewCompleted {
			continue
		}
		resolutions, _ := e.claim.Payload["resolves"].([]any)
		for _, raw := range resolutions {
			resolution, _ := raw.(map[string]any)
			p.resolveFinding(e, resolution)
		}
	}
}
func (p *projection) resolveFinding(e *projectedEvent, resolution map[string]any) {
	ref := exactRef(e, textValue(resolution["review_event_ref"]))
	id := textValue(resolution["finding_id"])
	source, _ := resolution["correction_source"].(map[string]any)
	valid := p.findingExists(ref, id) && e.authorized && textValue(e.claim.Payload["verdict"]) == "approved" &&
		textValue(source["revision"]) == e.claim.Source.Revision && textValue(source["repository"]) == e.claim.Source.Repository &&
		p.before(ref, e.record.Ref) && p.durable(e, resolution["evidence_indexes"]) && p.durable(e, e.claim.Payload["evidence_indexes"])
	if !valid {
		p.issue(e.record.Ref, "invalid_resolution")
		return
	}
	for i := range p.state.Findings {
		finding := &p.state.Findings[i]
		if finding.ReviewEventRef == ref && finding.FindingID == id {
			finding.Status = "resolved"
			finding.ResolvedBy = append(finding.ResolvedBy, e.record.Ref)
		}
	}
}
func (p *projection) evaluateAcceptance() {
	decisions := p.state.RecordedDecisions
	if len(decisions) == 0 {
		return
	}
	current := []Claim{}
	for _, claim := range decisions {
		superseded := false
		for _, other := range decisions {
			if p.err != nil {
				return
			}
			if p.before(claim.EventRef, other.EventRef) {
				superseded = true
				break
			}
		}
		if !superseded {
			current = append(current, claim)
		}
	}
	support := &p.state.SupportedAcceptance
	support.Status = "unsupported"
	for _, claim := range current {
		support.DecisionRefs = append(support.DecisionRefs, claim.EventRef)
	}
	for _, claim := range current {
		if textValue(claim.Payload["decision"]) != textValue(current[0].Payload["decision"]) {
			support.Status = "unresolved"
			support.Reasons = append(support.Reasons, "concurrent_incompatible_decisions")
			return
		}
	}
	reasons := map[string]bool{}
	for _, issue := range p.state.Completeness.Issues {
		reasons[issue.Code] = true
	}
	for _, finding := range p.state.Findings {
		if finding.Status == "open" && finding.Severity != "minor" {
			reasons["open_blocking_finding"] = true
		}
	}
	for _, claim := range current {
		if p.err != nil {
			return
		}
		p.acceptanceReasons(p.events[claim.EventRef], reasons)
	}
	for reason := range reasons {
		support.Reasons = append(support.Reasons, reason)
	}
	sort.Strings(support.Reasons)
	if len(support.Reasons) == 0 {
		support.Status = "supported"
	}
}
func (p *projection) acceptanceReasons(e *projectedEvent, reasons map[string]bool) {
	if textValue(e.claim.Payload["decision"]) != "accepted" {
		reasons["decision_rejected"] = true
	}
	if textValue(e.claim.Payload["policy_ref"]) != p.state.PolicyDigest {
		reasons["unknown_policy"] = true
	}
	if !p.durable(e, e.claim.Payload["evidence_indexes"]) {
		reasons["decision_proof_not_durable"] = true
	}
	for _, field := range []string{"check_refs", "review_refs"} {
		for _, raw := range stringValues(e.claim.Payload[field]) {
			if p.err != nil {
				return
			}
			ref := exactRef(e, raw)
			proof := p.events[ref]
			if !p.supportingProof(e, proof, field) {
				reasons["invalid_"+field] = true
			}
		}
	}
	// A resolution supports this decision only if the decision observed that resolution.
	for _, finding := range p.state.Findings {
		if finding.Severity == "minor" || finding.Status != "resolved" {
			continue
		}
		observed := false
		for _, ref := range finding.ResolvedBy {
			if p.err != nil {
				return
			}
			if p.before(ref, e.record.Ref) {
				observed = true
				break
			}
		}
		if !observed {
			reasons["unobserved_resolution"] = true
		}
	}
}
func (p *projection) supportingProof(decision, proof *projectedEvent, field string) bool {
	if proof == nil || !proof.valid || !proof.authorized || !p.relevant(proof) || proof.claim.Source != decision.claim.Source || !p.before(proof.record.Ref, decision.record.Ref) || !p.durable(proof, proof.claim.Payload["evidence_indexes"]) {
		return false
	}
	if field == "check_refs" {
		return proof.record.Type == CheckExecuted && textValue(proof.claim.Payload["outcome"]) == "passed"
	}
	return proof.record.Type == ReviewCompleted && textValue(proof.claim.Payload["verdict"]) == "approved"
}
