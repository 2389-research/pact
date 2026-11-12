// ABOUTME: Projects a frozen verified ledger cut into local development state.
// ABOUTME: Reads canonical facts without mutating indexes or choosing semantic winners by time.
package development

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"pact/internal/canonical"
	"pact/internal/evidence"
	"pact/internal/ledger"
	"pact/internal/store"
)

type projectedEvent struct {
	record     ledger.EventRecord
	claim      Claim
	evidence   []any
	valid      bool
	authorized bool
}
type projection struct {
	issues   map[ProjectionIssue]bool
	ctx      context.Context
	state    WorkState
	scan     ledger.ScanResult
	selected map[string]bool
	events   map[string]*projectedEvent
	refs     []string
	proof    map[string]map[int]bool
	work     uint64
	err      error
}

// Project reads one bounded frozen local snapshot; evidence observations happen after capture.
// SourceRevision is reported identity, never a claim of execution or repository ownership.
func Project(ctx context.Context, st *store.Store, request ProjectRequest) (WorkState, error) {
	if ctx == nil || st == nil {
		return WorkState{}, fmt.Errorf("context and store are required")
	}
	if request.Namespace == "" || request.Subject == "" || !regexp.MustCompile(RevisionPattern).MatchString(request.SourceRevision) {
		return WorkState{}, fmt.Errorf("namespace, subject, and full source revision are required")
	}
	p := &projection{ctx: ctx, state: emptyWorkState(request), selected: map[string]bool{}, events: map[string]*projectedEvent{}, proof: map[string]map[int]bool{}}
	err := st.WithReadLock(func() error { return p.capture(st, request) })
	if err != nil {
		return WorkState{}, err
	}
	if err = p.assessEvidence(st.Root()); err != nil {
		return WorkState{}, err
	}
	p.projectClaims()
	p.resolveFindings()
	p.evaluateAcceptance()
	if p.err != nil {
		return WorkState{}, p.err
	}
	if err = ctx.Err(); err != nil {
		return WorkState{}, err
	}
	sort.Slice(p.state.Completeness.Issues, func(i, j int) bool {
		a, b := p.state.Completeness.Issues[i], p.state.Completeness.Issues[j]
		if a.EventRef != b.EventRef {
			return a.EventRef < b.EventRef
		}
		return a.Code < b.Code
	})
	if len(p.state.Completeness.Issues) > 0 {
		p.state.Completeness.Status = "inconclusive"
	}
	return p.state, nil
}

func (p *projection) capture(st *store.Store, request ProjectRequest) error {
	scan, err := ledger.Scan(p.ctx, st, ledger.ScanOptions{Strict: true, Limits: ledger.Phase2Limits})
	if err != nil {
		return err
	}
	p.scan = scan
	frontier := request.Frontier
	if frontier == nil {
		for _, object := range scan.Objects {
			if !object.Valid() {
				return fmt.Errorf("%w: cannot derive current heads while canonical objects are invalid; select a verified explicit frontier", ledger.ErrIntegrity)
			}
		}
		frontier = scan.Heads[request.Namespace]
	}
	p.state.Frontier = append([]string{}, frontier...)
	sort.Strings(p.state.Frontier)
	for i, id := range p.state.Frontier {
		if i > 0 && id == p.state.Frontier[i-1] {
			return fmt.Errorf("duplicate frontier commit")
		}
		commit, ok := scan.Commits[id]
		if !ok {
			return fmt.Errorf("%w: frontier commit unavailable", ledger.ErrMissingDependency)
		}
		if commit.Namespace != request.Namespace {
			return fmt.Errorf("frontier namespace does not match request")
		}
	}
	if err = p.selectCommits(p.state.Frontier); err != nil {
		return err
	}
	ids := make([]string, 0, len(p.selected))
	for id := range p.selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err = p.readCommit(st, id); err != nil {
			return err
		}
	}
	sort.Strings(p.refs)
	return nil
}

func (p *projection) selectCommits(frontier []string) error {
	queue := append([]string{}, frontier...)
	for len(queue) > 0 {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if p.selected[id] {
			continue
		}
		commit, ok := p.scan.Commits[id]
		if !ok {
			return fmt.Errorf("%w: selected commit dependency unavailable", ledger.ErrMissingDependency)
		}
		if !p.scan.Objects[id].Valid() {
			return fmt.Errorf("%w: invalid selected commit", ledger.ErrIntegrity)
		}
		p.selected[id] = true
		queue = append(queue, commit.Parents...)
		for _, ref := range commit.EventRefs {
			event := p.scan.Events[ref]
			if _, ok := p.scan.CausalBatches[ref]; !ok {
				return fmt.Errorf("%w: selected causal history incomplete or invalid", ledger.ErrMissingDependency)
			}
			for _, cause := range event.CausedBy {
				dependency, ok := p.scan.Events[cause]
				if !ok {
					return fmt.Errorf("%w: selected causal reference unavailable", ledger.ErrMissingDependency)
				}
				queue = append(queue, dependency.CommitID)
			}
		}
	}
	return nil
}

func (p *projection) readCommit(st *store.Store, id string) error {
	raw, err := st.GetBoundedContext(p.ctx, id, ledger.Phase2Limits.ObjectBytes)
	if err != nil {
		return err
	}
	digest, err := canonical.DigestContext(p.ctx, raw)
	if err != nil {
		return err
	}
	if digest != id {
		return fmt.Errorf("%w: selected snapshot changed", ledger.ErrIntegrity)
	}
	value, err := canonical.ParseContext(p.ctx, raw)
	if err != nil {
		return err
	}
	object, _ := value.(map[string]any)
	body, _ := object["body"].(map[string]any)
	events, _ := body["events"].([]any)
	for _, item := range events {
		if err = p.ctx.Err(); err != nil {
			return err
		}
		event, _ := item.(map[string]any)
		ref := ledger.EventRef(id, textValue(event["local_id"]))
		record := p.scan.Events[ref]
		payload, _ := event["payload"].(map[string]any)
		proof, _ := event["evidence"].([]any)
		source, _ := payload["source"].(map[string]any)
		e := &projectedEvent{record: record, claim: Claim{EventRef: ref, Source: Source{textValue(source["repository"]), textValue(source["revision"])}, Payload: payload}, evidence: proof, authorized: p.scan.Verification.Authorization[id].Status == "authorized"}
		p.events[ref] = e
		if !p.relevant(e) {
			continue
		}
		p.refs = append(p.refs, ref)
		if !e.authorized {
			p.issue(ref, "unknown_authority")
		}
		validationErr := ValidatePayload(p.ctx, record.Type, record.SchemaRef, payload, len(proof))
		if validationErr != nil {
			if err = p.ctx.Err(); err != nil {
				return err
			}
			p.issue(ref, "unchecked_or_invalid_payload")
			continue
		}
		if record.Kind != EventDefinitions()[record.Type].Kind {
			p.issue(ref, "invalid_envelope_kind")
			continue
		}
		e.valid = true
	}
	return nil
}
func (p *projection) relevant(e *projectedEvent) bool {
	return e.record.Namespace == p.state.Namespace && e.record.Subject == p.state.Subject
}
func (p *projection) issue(ref, code string) {
	if p.issues == nil {
		p.issues = map[ProjectionIssue]bool{}
	}
	issue := ProjectionIssue{ref, code}
	if !p.issues[issue] {
		p.issues[issue] = true
		p.state.Completeness.Issues = append(p.state.Completeness.Issues, issue)
	}
}

func exactRef(event *projectedEvent, ref string) string {
	if after, ok := strings.CutPrefix(ref, "local:"); ok {
		return ledger.EventRef(event.record.CommitID, after)
	}
	return ref
}

func collectEvidenceIndexes(value any, result map[int]bool) {
	switch node := value.(type) {
	case map[string]any:
		for key, item := range node {
			if key == "evidence_indexes" {
				indexes, _ := item.([]any)
				for _, index := range indexes {
					number, _ := index.(int64)
					result[int(number)] = true
				}
			} else {
				collectEvidenceIndexes(item, result)
			}
		}
	case []any:
		for _, item := range node {
			collectEvidenceIndexes(item, result)
		}
	}
}
func (p *projection) assessEvidence(repo string) error {
	requests := []evidence.Request{}
	selections := []EvidenceAssessment{}
	for _, ref := range p.refs {
		e := p.events[ref]
		if !e.valid {
			continue
		}
		selected := map[int]bool{}
		collectEvidenceIndexes(e.claim.Payload, selected)
		indexes := make([]int, 0, len(selected))
		for index := range selected {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		for _, index := range indexes {
			proof, _ := e.evidence[index].(map[string]any)
			locator := textValue(proof["ref"])
			requests = append(requests, evidence.Request{Repo: repo, Ref: locator, Digest: textValue(proof["digest"])})
			selections = append(selections, EvidenceAssessment{EventRef: ref, Index: index, Durable: strings.HasPrefix(locator, "git:")})
		}
	}
	assessments, err := evidence.ResolveAll(p.ctx, requests)
	if err != nil {
		return err
	}
	for i, assessment := range assessments {
		selection := selections[i]
		selection.Assessment = assessment
		p.state.EvidenceAssessments = append(p.state.EvidenceAssessments, selection)
		if p.proof[selection.EventRef] == nil {
			p.proof[selection.EventRef] = map[int]bool{}
		}
		p.proof[selection.EventRef][selection.Index] = assessment.Status == "matching" && selection.Durable
		if assessment.Status != "matching" {
			p.issue(selection.EventRef, "evidence_unmatched")
		}
	}
	return nil
}
func (p *projection) durable(e *projectedEvent, value any) bool {
	indexes, _ := value.([]any)
	if len(indexes) == 0 {
		return false
	}
	for _, index := range indexes {
		number, _ := index.(int64)
		if !p.proof[e.record.Ref][int(number)] {
			return false
		}
	}
	return true
}

// before follows event causes and whole parent commits, never sibling array order.
func (p *projection) before(earlier, later string) bool {
	if p.err != nil || earlier == later {
		return false
	}
	queue := []string{later}
	seen := map[string]bool{}
	for len(queue) > 0 {
		p.work++
		if p.work > ledger.Phase2Limits.GraphEdges {
			p.err = &ledger.LimitError{Resource: "projection_causal_work", Maximum: ledger.Phase2Limits.GraphEdges, ObservedAtLeast: p.work}
			return false
		}
		if err := p.ctx.Err(); err != nil {
			p.err = err
			return false
		}
		ref := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[ref] {
			continue
		}
		seen[ref] = true
		e := p.events[ref]
		if e == nil {
			continue
		}
		previous := append([]string{}, e.record.CausedBy...)
		for _, id := range p.scan.Commits[e.record.CommitID].Parents {
			previous = append(previous, p.scan.Commits[id].EventRefs...)
		}
		for _, dependency := range previous {
			if dependency == earlier {
				return true
			}
			queue = append(queue, dependency)
		}
	}
	return false
}
