// ABOUTME: Exercises the development policy against real signed stores and retained Git proof.
// ABOUTME: Keeps expected acceptance outcomes independent of the projector implementation.
package development

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"pact/internal/canonical"
	"pact/internal/identity"
	"pact/internal/ledger"
	"pact/internal/store"
)

type projectionFixture struct {
	t      *testing.T
	st     *store.Store
	key    *identity.KeyFile
	source Source
	proof  map[string]any
}

func newProjectionFixture(t *testing.T) *projectionFixture {
	t.Helper()
	repo := t.TempDir()
	fixtureGit(t, repo, "init", "--quiet", "--object-format=sha1")
	if err := os.WriteFile(filepath.Join(repo, "proof.txt"), []byte("verified proof\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, repo, "add", "proof.txt")
	fixtureGit(t, repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--quiet", "-m", "proof")
	rev := strings.TrimSpace(fixtureGit(t, repo, "rev-parse", "HEAD"))
	initialized, err := store.Init(repo, "org/test/development", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	generated, err := identity.GenerateKeyFile(filepath.Join(t.TempDir(), "key.json"), "Fixture", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.AddRoot(initialized.Store, generated.Key, time.Now()); err != nil {
		t.Fatal(err)
	}
	digest := canonical.Digest([]byte("verified proof\n"))
	return &projectionFixture{t, initialized.Store, generated.Key, Source{"fixture", rev}, map[string]any{"ref": "git:" + rev + ":proof.txt", "digest": digest, "media_type": "text/plain", "role": "primary"}}
}
func fixtureGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}
func (f *projectionFixture) event(id, typ string, payload map[string]any) InputEvent {
	return InputEvent{LocalID: id, Type: typ, Payload: payload, Evidence: []map[string]any{f.proof}, CausedBy: []string{}, Supersedes: []string{}}
}
func (f *projectionFixture) commit(parents []string, events ...InputEvent) ledger.CommitResult {
	f.t.Helper()
	batch, err := ValidateInput(context.Background(), RecordInput{OperationID: events[0].LocalID, Namespace: "org/test/development", Subject: "work", Source: f.source, Events: events})
	if err != nil {
		f.t.Fatal(err)
	}
	result, err := ledger.Commit(f.st, f.key, batch, ledger.CommitOptions{Parents: parents, ObservedAt: "2026-11-11T00:00:00Z"})
	if err != nil {
		f.t.Fatal(err)
	}
	return result
}
func checkPayload() map[string]any {
	return map[string]any{"name": "check", "command": []any{"scripts/check"}, "outcome": "passed", "exit_code": 0, "evidence_indexes": []any{0}}
}
func reviewPayload(severity string) map[string]any {
	findings := []any{}
	verdict := "approved"
	if severity != "" {
		verdict = "changes_required"
		findings = append(findings, map[string]any{"id": "bug", "severity": severity, "summary": "A real concern", "evidence_indexes": []any{0}})
	}
	return map[string]any{"verdict": verdict, "findings": findings, "resolves": []any{}, "evidence_indexes": []any{0}}
}
func (f *projectionFixture) acceptance(check, review, decision string) InputEvent {
	digest := canonical.Digest(ProfileArtifacts()["local-policy.json"])
	return f.event("accept", AcceptanceRecorded, map[string]any{"decision": decision, "rationale": "Local assessment", "policy_ref": digest, "check_refs": []any{check}, "review_refs": []any{review}, "evidence_indexes": []any{0}})
}
func (f *projectionFixture) project(frontier []string) WorkState {
	f.t.Helper()
	state, err := Project(context.Background(), f.st, ProjectRequest{Namespace: "org/test/development", Subject: "work", SourceRevision: f.source.Revision, Frontier: frontier})
	if err != nil {
		f.t.Fatal(err)
	}
	return state
}
func TestProjectionAcceptanceRubric(t *testing.T) {
	cases := []struct {
		name     string
		severity string
		resolve  bool
		decision bool
		want     string
		open     bool
	}{
		{"missing decision", "", false, false, "not_recorded", false},
		{"supported", "", false, true, "supported", false},
		{"green does not close important", "important", false, true, "unsupported", true},
		{"critical blocks", "critical", false, true, "unsupported", true},
		{"minor remains visible", "minor", false, true, "supported", true},
		{"explicit correction resolves", "important", true, true, "supported", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newProjectionFixture(t)
			old := f.commit(nil, f.event("old", ReviewCompleted, reviewPayload(tc.severity)))
			review := reviewPayload("")
			if tc.resolve {
				review["resolves"] = []any{map[string]any{"review_event_ref": old.EventRefs[0], "finding_id": "bug", "correction_source": map[string]any{"repository": f.source.Repository, "revision": f.source.Revision}, "evidence_indexes": []any{0}}}
			}
			proof := f.commit(nil, f.event("check", CheckExecuted, checkPayload()), f.event("review", ReviewCompleted, review))
			if tc.decision {
				f.commit(nil, f.acceptance(proof.EventRefs[0], proof.EventRefs[1], "accepted"))
			}
			state := f.project(nil)
			if state.SupportedAcceptance.Status != tc.want {
				t.Fatalf("status=%s want=%s reasons=%v", state.SupportedAcceptance.Status, tc.want, state.SupportedAcceptance.Reasons)
			}
			if tc.severity != "" {
				if len(state.Findings) != 1 || (state.Findings[0].Status == "open") != tc.open {
					t.Fatalf("findings=%+v", state.Findings)
				}
			}
		})
	}
}
func TestProjectionFrozenCutAndLaterBlocker(t *testing.T) {
	f := newProjectionFixture(t)
	proof := f.commit(nil, f.event("check", CheckExecuted, checkPayload()), f.event("review", ReviewCompleted, reviewPayload("")))
	accepted := f.commit(nil, f.acceptance(proof.EventRefs[0], proof.EventRefs[1], "accepted"))
	before := f.project([]string{accepted.ObjectID})
	f.commit(nil, f.event("later", ReviewCompleted, reviewPayload("important")))
	after := f.project([]string{accepted.ObjectID})
	if !reflect.DeepEqual(before, after) {
		t.Fatal("frozen frontier changed")
	}
	if state := f.project(nil); state.SupportedAcceptance.Status != "unsupported" {
		t.Fatalf("later blocker accepted: %+v", state)
	}
}
func TestProjectionConcurrentDecisions(t *testing.T) {
	f := newProjectionFixture(t)
	proof := f.commit(nil, f.event("check", CheckExecuted, checkPayload()), f.event("review", ReviewCompleted, reviewPayload("")))
	a := f.commit([]string{proof.ObjectID}, f.acceptance(proof.EventRefs[0], proof.EventRefs[1], "accepted"))
	b := f.commit([]string{proof.ObjectID}, f.acceptance(proof.EventRefs[0], proof.EventRefs[1], "rejected"))
	state := f.project([]string{a.ObjectID, b.ObjectID})
	if state.SupportedAcceptance.Status != "unresolved" || len(state.RecordedDecisions) != 2 {
		t.Fatalf("concurrent decisions=%+v", state)
	}
}
func TestProjectionProofMustBeObservedAndSameSource(t *testing.T) {
	for _, mode := range []string{"unobserved", "wrong-source", "mutable", "unknown-schema", "invalid-payload", "missing-ref", "unknown-authority"} {
		t.Run(mode, func(t *testing.T) {
			f := newProjectionFixture(t)
			root := f.commit(nil, f.event("root", ReviewCompleted, reviewPayload("")))
			if mode == "mutable" {
				f.proof["ref"] = "proof.txt"
			}
			proof := f.commit(nil, f.event("check", CheckExecuted, checkPayload()), f.event("review", ReviewCompleted, reviewPayload("")))
			if mode == "wrong-source" {
				f.source.Revision = strings.Repeat("a", 40)
			}
			acceptance := f.acceptance(proof.EventRefs[0], proof.EventRefs[1], "accepted")
			if mode == "missing-ref" {
				acceptance.Payload["check_refs"] = []any{ledger.EventRef(root.ObjectID, "absent")}
			}
			parents := []string{proof.ObjectID}
			if mode == "unobserved" {
				parents = []string{root.ObjectID}
			}
			if mode == "unknown-authority" {
				generated, err := identity.GenerateKeyFile(filepath.Join(t.TempDir(), "other.json"), "Other", time.Now())
				if err != nil {
					t.Fatal(err)
				}
				f.key = generated.Key
			}
			f.commit(parents, acceptance)
			if mode == "unknown-schema" || mode == "invalid-payload" {
				batch, err := ValidateInput(context.Background(), RecordInput{OperationID: "raw", Namespace: "org/test/development", Subject: "work", Source: f.source, Events: []InputEvent{f.event("raw", CheckExecuted, checkPayload())}})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "unknown-schema" {
					batch.Events[0].SchemaRef = "pact:core/generic-object/v1"
				} else {
					batch.Events[0].Payload["outcome"] = "banana"
				}
				if _, err = ledger.Commit(f.st, f.key, batch, ledger.CommitOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			state := f.project(nil)
			if state.SupportedAcceptance.Status != "unsupported" {
				raw, _ := json.Marshal(state)
				t.Fatalf("unsupported proof accepted: %s", raw)
			}
		})
	}
}

func TestProjectionResolutionAndCausalityBoundaries(t *testing.T) {
	for _, mode := range []string{"source-change", "cause-alone", "wrong-correction", "missing-finding", "unobserved-resolution", "unobserved-local-proof", "observed-local-proof"} {
		t.Run(mode, func(t *testing.T) {
			f := newProjectionFixture(t)
			old := f.commit(nil, f.event("old", ReviewCompleted, reviewPayload("important")))
			if mode == "source-change" {
				f.source.Revision = strings.Repeat("b", 40)
			}
			review := f.event("review", ReviewCompleted, reviewPayload(""))
			review.CausedBy = []string{old.EventRefs[0]}
			if mode == "wrong-correction" || mode == "missing-finding" || mode == "unobserved-resolution" {
				id := "bug"
				source := f.source.Revision
				if mode == "wrong-correction" {
					source = strings.Repeat("c", 40)
				}
				if mode == "missing-finding" {
					id = "absent"
				}
				review.Payload["resolves"] = []any{map[string]any{"review_event_ref": old.EventRefs[0], "finding_id": id, "correction_source": map[string]any{"repository": f.source.Repository, "revision": source}, "evidence_indexes": []any{0}}}
			}
			if strings.Contains(mode, "local-proof") {
				testLocalProjectionProof(t, f, mode)
				return
			}
			proof := f.commit(nil, f.event("check", CheckExecuted, checkPayload()), review)
			parents := []string{proof.ObjectID}
			if mode == "unobserved-resolution" {
				parents = []string{old.ObjectID}
			}
			f.commit(parents, f.acceptance(proof.EventRefs[0], proof.EventRefs[1], "accepted"))
			state := f.project(nil)
			if state.SupportedAcceptance.Status != "unsupported" {
				t.Fatalf("invalid resolution accepted=%+v", state)
			}
		})
	}
}

func TestProjectionEvidenceLossAndDeclaredActions(t *testing.T) {
	f := newProjectionFixture(t)
	proof := f.commit(nil, f.event("check", CheckExecuted, checkPayload()), f.event("review", ReviewCompleted, reviewPayload("")))
	f.commit(nil, f.acceptance(proof.EventRefs[0], proof.EventRefs[1], "accepted"))
	next := f.event("next", NextActionRecorded, map[string]any{"actor": "Fixture", "action": "inspect the release", "blocking_event_refs": []any{}, "blocking_findings": []any{}, "evidence_indexes": []any{}})
	f.commit(nil, next)
	before := f.project(nil)
	if len(before.DeclaredNextActions) != 1 || before.SupportedAcceptance.Status != "supported" {
		t.Fatalf("state=%+v", before)
	}
	if err := os.WriteFile(filepath.Join(f.st.Root(), "proof.txt"), []byte("changed working bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	after := f.project(nil)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("working file change affected immutable Git proof")
	}
	gitdir := filepath.Join(f.st.Root(), ".git")
	if err := os.Rename(gitdir, gitdir+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	state := f.project(nil)
	if state.SupportedAcceptance.Status != "unsupported" || len(state.RecordedDecisions) != 1 {
		t.Fatalf("lost evidence=%+v", state)
	}
}

func TestProjectionRequestAndDependencyRefusals(t *testing.T) {
	f := newProjectionFixture(t)
	root := f.commit(nil, f.event("root", ReviewCompleted, reviewPayload("")))
	next := f.commit(nil, f.event("next", CheckExecuted, checkPayload()))
	request := ProjectRequest{Namespace: "org/test/development", Subject: "work", SourceRevision: f.source.Revision, Frontier: []string{next.ObjectID}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Project(ctx, f.st, request); err == nil {
		t.Fatal("cancellation accepted")
	}
	bad := request
	bad.SourceRevision = "HEAD"
	if _, err := Project(context.Background(), f.st, bad); err == nil {
		t.Fatal("mutable source accepted")
	}
	bad = request
	bad.Frontier = []string{next.ObjectID, next.ObjectID}
	if _, err := Project(context.Background(), f.st, bad); err == nil {
		t.Fatal("duplicate frontier accepted")
	}
	bad = request
	bad.Frontier = []string{}
	empty, err := Project(context.Background(), f.st, bad)
	if err != nil || len(empty.Checks) != 0 {
		t.Fatalf("explicit empty cut=%+v err=%v", empty, err)
	}
	files, err := f.st.ObjectFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.ID == root.ObjectID {
			if err = os.Remove(file.Path); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err = Project(context.Background(), f.st, request); err == nil {
		t.Fatal("missing parent accepted")
	}
}

func TestProjectionSelectedProofScopeAndEmptyEvidence(t *testing.T) {
	for _, mode := range []string{"no-check-proof", "no-review-proof", "no-decision-proof", "other-subject", "other-repository", "other-namespace", "unknown-policy"} {
		t.Run(mode, func(t *testing.T) {
			f := newProjectionFixture(t)
			check := f.event("check", CheckExecuted, checkPayload())
			review := f.event("review", ReviewCompleted, reviewPayload(""))
			if mode == "no-check-proof" {
				check.Payload["evidence_indexes"] = []any{}
			}
			if mode == "no-review-proof" {
				review.Payload["evidence_indexes"] = []any{}
			}
			input := RecordInput{OperationID: "proof", Namespace: "org/test/development", Subject: "work", Source: f.source, Events: []InputEvent{check, review}}
			if mode == "other-subject" {
				input.Subject = "unrelated"
			}
			if mode == "other-repository" {
				input.Source.Repository = "elsewhere"
			}
			if mode == "other-namespace" {
				input.Namespace = "org/test/other"
			}
			batch, err := ValidateInput(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			proof, err := ledger.Commit(f.st, f.key, batch, ledger.CommitOptions{})
			if err != nil {
				t.Fatal(err)
			}
			accept := f.acceptance(proof.EventRefs[0], proof.EventRefs[1], "accepted")
			if mode == "no-decision-proof" {
				accept.Payload["evidence_indexes"] = []any{}
			}
			if mode == "unknown-policy" {
				accept.Payload["policy_ref"] = "sha256:" + strings.Repeat("f", 64)
			}
			if mode == "other-namespace" {
				accept.CausedBy = proof.EventRefs
			}
			f.commit(nil, accept)
			state := f.project(nil)
			if state.SupportedAcceptance.Status != "unsupported" || len(state.RecordedDecisions) != 1 {
				t.Fatalf("invalid selected proof=%+v", state)
			}
		})
	}
}

func TestProjectionCompiledDriverExpectedBytes(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "projectdriver")
	cmd := exec.Command("go", "build", "-o", binary, "./testdata/projectdriver")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build driver: %v: %s", err, output)
	}
	f := newProjectionFixture(t)
	check := checkPayload()
	review := reviewPayload("")
	proof := f.commit(nil, f.event("check", CheckExecuted, check), f.event("review", ReviewCompleted, review))
	acceptance := f.acceptance(proof.EventRefs[0], proof.EventRefs[1], "accepted")
	accepted := f.commit(nil, acceptance)
	request := ProjectRequest{Namespace: "org/test/development", Subject: "work", SourceRevision: f.source.Revision, Frontier: []string{accepted.ObjectID}}
	requestRaw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	run := func() []byte {
		t.Helper()
		out, err := exec.Command(binary, f.st.Root(), string(requestRaw)).CombinedOutput()
		if err != nil {
			t.Fatalf("driver: %v: %s", err, out)
		}
		return out
	}
	got := run()
	// The expected state is authored from fixture facts, without calling Project or its helpers.
	check["operation_id"] = "check"
	check["source"] = f.source
	acceptance.Payload["operation_id"] = "accept"
	acceptance.Payload["source"] = f.source
	expected := map[string]any{
		"namespace": "org/test/development", "subject": "work", "source_revision": f.source.Revision, "frontier": []string{accepted.ObjectID},
		"profile_digest": expectedProfileDigest(t), "policy_digest": canonical.Digest(ProfileArtifacts()["local-policy.json"]), "projector_identity": map[string]any{"version": "development/projector/v1", "digest": expectedProjectorDigest(t)},
		"checks": []any{map[string]any{"event_ref": proof.EventRefs[0], "source": f.source, "payload": check}}, "findings": []any{},
		"recorded_decisions":   []any{map[string]any{"event_ref": accepted.EventRefs[0], "source": f.source, "payload": acceptance.Payload}},
		"supported_acceptance": map[string]any{"status": "supported", "decision_refs": []string{accepted.EventRefs[0]}, "reasons": []any{}}, "declared_next_actions": []any{},
		"completeness": map[string]any{"scope": "selected_local_frontier", "status": "locally_closed", "global_completeness": "unknown", "authority": "locally_configured_root", "reviewer_independence": "not_established", "issues": []any{}},
	}
	refs := make([]string, 0, 4)
	refs = append(refs, proof.EventRefs[0], proof.EventRefs[1], accepted.EventRefs[0])
	sort.Strings(refs)
	assessments := make([]any, 0, len(refs))
	for _, ref := range refs {
		assessments = append(assessments, map[string]any{"event_ref": ref, "index": 0, "durable": true, "assessment": map[string]any{"ref": f.proof["ref"], "digest": f.proof["digest"], "actual_digest": f.proof["digest"], "status": "matching", "bytes": len("verified proof\n")}})
	}
	expected["evidence_assessments"] = assessments
	want := canonicalTestJSON(t, expected)
	if string(got) != string(want) {
		t.Fatalf("canonical driver result\ngot %s\nwant %s", got, want)
	}

	// An incompatible concurrent decision remains visible in the compiled result.
	rejectedEvent := f.acceptance(proof.EventRefs[0], proof.EventRefs[1], "rejected")
	rejected := f.commit([]string{proof.ObjectID}, rejectedEvent)
	rejectedEvent.Payload["operation_id"] = "accept"
	rejectedEvent.Payload["source"] = f.source
	conflictFrontier := []string{accepted.ObjectID, rejected.ObjectID}
	sort.Strings(conflictFrontier)
	conflictRefs := []string{accepted.EventRefs[0], rejected.EventRefs[0]}
	sort.Strings(conflictRefs)
	conflictClaims := make([]any, 0, len(conflictRefs))
	for _, ref := range conflictRefs {
		payload := acceptance.Payload
		if ref == rejected.EventRefs[0] {
			payload = rejectedEvent.Payload
		}
		conflictClaims = append(conflictClaims, map[string]any{"event_ref": ref, "source": f.source, "payload": payload})
	}
	expected["frontier"] = conflictFrontier
	expected["recorded_decisions"] = conflictClaims
	expected["supported_acceptance"] = map[string]any{"status": "unresolved", "decision_refs": conflictRefs, "reasons": []string{"concurrent_incompatible_decisions"}}
	refs = append(refs, rejected.EventRefs[0])
	sort.Strings(refs)
	conflictAssessments := make([]any, 0, len(refs))
	for _, ref := range refs {
		conflictAssessments = append(conflictAssessments, map[string]any{"event_ref": ref, "index": 0, "durable": true, "assessment": map[string]any{"ref": f.proof["ref"], "digest": f.proof["digest"], "actual_digest": f.proof["digest"], "status": "matching", "bytes": len("verified proof\n")}})
	}
	expected["evidence_assessments"] = conflictAssessments
	conflictRequest := request
	conflictRequest.Frontier = conflictFrontier
	conflictRaw, err := json.Marshal(conflictRequest)
	if err != nil {
		t.Fatal(err)
	}
	conflictOutput, err := exec.Command(binary, f.st.Root(), string(conflictRaw)).CombinedOutput()
	if err != nil {
		t.Fatalf("conflict driver: %v: %s", err, conflictOutput)
	}
	if expectedBytes := canonicalTestJSON(t, expected); string(conflictOutput) != string(expectedBytes) {
		t.Fatalf("conflict output\ngot %s\nwant %s", conflictOutput, expectedBytes)
	}
	f.commit(nil, f.event("later", ReviewCompleted, reviewPayload("critical")))
	if string(run()) != string(want) {
		t.Fatal("compiled frozen cut changed after newer history")
	}
}
func canonicalTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := canonical.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = canonical.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}
func expectedProfileDigest(t *testing.T) string {
	t.Helper()
	manifest := map[string]any{}
	for name, raw := range ProfileArtifacts() {
		manifest[name] = canonical.Digest(raw)
	}
	return canonical.Digest(bytes.TrimSuffix(canonicalTestJSON(t, manifest), []byte{'\n'}))
}
func expectedProjectorDigest(t *testing.T) string {
	t.Helper()
	manifest := map[string]any{}
	for _, name := range []string{"project.go", "project_types.go", "policy.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		manifest[name] = canonical.Digest(raw)
	}
	return canonical.Digest(bytes.TrimSuffix(canonicalTestJSON(t, manifest), []byte{'\n'}))
}

func testLocalProjectionProof(t *testing.T, f *projectionFixture, mode string) {
	t.Helper()
	// A clean work subject isolates the local event-order test from the old finding.
	batch, err := ValidateInput(context.Background(), RecordInput{OperationID: "batch", Namespace: "org/test/development", Subject: "clean", Source: f.source, Events: []InputEvent{f.event("check", CheckExecuted, checkPayload()), f.event("review", ReviewCompleted, reviewPayload("")), f.acceptance("local:check", "local:review", "accepted")}})
	if err != nil {
		t.Fatal(err)
	}
	if mode == "observed-local-proof" {
		for i := range batch.Events {
			if batch.Events[i].LocalID == "accept" {
				batch.Events[i].CausedBy = []string{"local:check", "local:review"}
			}
		}
	}
	result, err := ledger.Commit(f.st, f.key, batch, ledger.CommitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	state, err := Project(context.Background(), f.st, ProjectRequest{Namespace: "org/test/development", Subject: "clean", SourceRevision: f.source.Revision, Frontier: []string{result.ObjectID}})
	if err != nil {
		t.Fatal(err)
	}
	want := "unsupported"
	if mode == "observed-local-proof" {
		want = "supported"
	}
	if state.SupportedAcceptance.Status != want {
		t.Fatalf("local proof=%+v", state.SupportedAcceptance)
	}
}

func TestProjectionCommitAtomicityAndSupersedes(t *testing.T) {
	f := newProjectionFixture(t)
	root := f.commit(nil, f.event("root", ReviewCompleted, reviewPayload("")))
	branch := f.commit(nil, f.event("check", CheckExecuted, checkPayload()), f.event("review", ReviewCompleted, reviewPayload("")), f.event("blocking", ReviewCompleted, reviewPayload("critical")))
	accept := f.acceptance(ledger.EventRef(branch.ObjectID, "check"), ledger.EventRef(branch.ObjectID, "review"), "accepted")
	accept.CausedBy = []string{ledger.EventRef(branch.ObjectID, "check"), ledger.EventRef(branch.ObjectID, "review")}
	admitted := f.commit([]string{root.ObjectID}, accept)
	state := f.project([]string{admitted.ObjectID})
	if state.SupportedAcceptance.Status != "unsupported" || len(state.Findings) != 1 {
		t.Fatalf("partial commit projected: %+v", state)
	}
	// Supersedes references identify facts but do not make them causally observed.
	accept.CausedBy = []string{}
	accept.Supersedes = []string{ledger.EventRef(branch.ObjectID, "check"), ledger.EventRef(branch.ObjectID, "review")}
	admitted = f.commit([]string{root.ObjectID}, accept)
	state = f.project([]string{admitted.ObjectID, branch.ObjectID})
	if state.SupportedAcceptance.Status != "unsupported" {
		t.Fatalf("supersedes supplied causal observation: %+v", state)
	}
}

func TestProjectionPolicyUnit(t *testing.T) {
	// Small policy predicates use plain typed facts; integration tests own store verification.
	p := projection{proof: map[string]map[int]bool{"review": {0: true, 1: false}}}
	e := &projectedEvent{record: ledger.EventRecord{Ref: "review", CommitID: "sha256:" + strings.Repeat("a", 64)}}
	if p.durable(e, []any{}) || p.durable(e, []any{int64(1)}) || !p.durable(e, []any{int64(0)}) {
		t.Fatal("durable evidence rubric failed")
	}
	if got := exactRef(e, "local:check"); got != ledger.EventRef(e.record.CommitID, "check") {
		t.Fatalf("local ref=%s", got)
	}
	if got := exactRef(e, "external"); got != "external" {
		t.Fatalf("external ref=%s", got)
	}
	indexes := map[int]bool{}
	collectEvidenceIndexes(map[string]any{"evidence_indexes": []any{int64(0)}, "resolves": []any{map[string]any{"evidence_indexes": []any{int64(1), int64(0)}}}}, indexes)
	if !reflect.DeepEqual(indexes, map[int]bool{0: true, 1: true}) {
		t.Fatalf("selected indexes=%v", indexes)
	}
}

func TestProjectionCausalBudgetStopsFurtherWork(t *testing.T) {
	p := projection{ctx: context.Background(), work: ledger.Phase2Limits.GraphEdges, events: map[string]*projectedEvent{}}
	if p.before("earlier", "later") || p.err == nil {
		t.Fatal("causal work limit did not refuse")
	}
	work := p.work
	p.before("another", "later")
	if p.work != work {
		t.Fatal("failed traversal kept doing work")
	}
}

func TestProjectionDefaultFrontierDoesNotHideInvalidObjects(t *testing.T) {
	f := newProjectionFixture(t)
	root := f.commit(nil, f.event("root", ReviewCompleted, reviewPayload("")))
	before := f.project([]string{root.ObjectID})
	if _, _, err := f.st.PutCanonical(map[string]any{"unknown": "object"}); err != nil {
		t.Fatal(err)
	}
	request := ProjectRequest{Namespace: "org/test/development", Subject: "work", SourceRevision: f.source.Revision}
	if _, err := Project(context.Background(), f.st, request); err == nil {
		t.Fatal("default frontier silently omitted invalid canonical object")
	}
	after := f.project([]string{root.ObjectID})
	if !reflect.DeepEqual(before, after) {
		t.Fatal("explicit valid cut changed because of unrelated malformed object")
	}
}
