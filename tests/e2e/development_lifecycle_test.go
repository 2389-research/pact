// ABOUTME: Proves the recorded development lifecycle through real CLI processes and retained Git evidence.
// ABOUTME: Keeps findings, explicit decisions, crash recovery, and complete paged history observable together.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"pact/internal/canonical"
	"pact/internal/development"
	"pact/internal/ledger"
)

func TestCLIDevelopmentLifecycle(t *testing.T) {
	workspace, root := t.TempDir(), projectRoot(t)
	binary, projector := filepath.Join(workspace, "pact"), filepath.Join(workspace, "projectdriver")
	buildBinary(t, root, binary)
	build := exec.Command("go", "build", "-o", projector, "./internal/development/testdata/projectdriver")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build projector: %v: %s", err, output)
	}
	repo, key := filepath.Join(workspace, "repo"), filepath.Join(workspace, "key.json")
	mustMkdir(t, repo)
	source, err := os.ReadFile(filepath.Join(root, "tests/fixtures/development/source.fast-import"))
	if err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, repo, nil, "init", "--quiet", "--object-format=sha1")
	runFixtureGit(t, repo, source, "fast-import", "--quiet")
	raw, err := os.ReadFile(filepath.Join(root, "tests/fixtures/development/inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inputs map[string]development.RecordInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&inputs); err != nil {
		t.Fatal(err)
	}
	initial, corrected := inputs["open-finding"], inputs["corrected"]
	for ref, want := range map[string]string{"main^": initial.Source.Revision, "main": corrected.Source.Revision} {
		if got := strings.TrimSpace(string(runFixtureGit(t, repo, nil, "rev-parse", ref))); got != want {
			t.Fatalf("Git source %s = %s, want %s", ref, got, want)
		}
	}
	runFixtureGit(t, repo, nil, "update-ref", "refs/heads/main", initial.Source.Revision)
	runFixtureGit(t, repo, nil, "symbolic-ref", "HEAD", "refs/heads/main")
	runJSON(t, binary, "setup", "--repo", repo, "--namespace", initial.Namespace, "--actor", "Lifecycle Fixture", "--key-file", key, "--json")
	proof := runFixtureGit(t, repo, nil, "show", initial.Source.Revision+":proof.txt")
	if err := os.WriteFile(filepath.Join(repo, "proof.txt"), proof, 0600); err != nil {
		t.Fatal(err)
	}
	mutable := maps.Clone(initial.Events[0].Evidence[0])
	mutable["ref"] = "proof.txt"
	initial.Events[0].Evidence = append(initial.Events[0].Evidence, mutable)
	record := func(input development.RecordInput) map[string]any {
		t.Helper()
		result := runJSON(t, binary, lifecycleRecordArgs(t, repo, key, input)...)
		if result["admission"] != "created" || result["index_state"] != "current" || result["authorization"] != "authorized" {
			t.Fatalf("record result: %v", result)
		}
		return result
	}
	request := development.ProjectRequest{Namespace: initial.Namespace, Subject: initial.Subject, SourceRevision: initial.Source.Revision}
	project := func(want string) development.WorkState {
		t.Helper()
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		result := runJSON(t, projector, repo, string(encoded))
		encoded, err = json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var state development.WorkState
		if err := json.Unmarshal(encoded, &state); err != nil {
			t.Fatal(err)
		}
		if state.SupportedAcceptance.Status != want || state.Completeness.Status != "locally_closed" {
			t.Fatalf("support=%+v completeness=%+v", state.SupportedAcceptance, state.Completeness)
		}
		return state
	}
	opened := record(initial)
	protected := validationStoreSnapshot(t, repo)
	state := project("not_recorded")
	assertLifecycleFinding(t, state, eventRef(opened, "review"), "open", nil)
	if len(state.Checks) != 1 || state.Checks[0].Payload["outcome"] != "passed" || len(state.RecordedDecisions) != 0 {
		t.Fatal("passing check concealed the failed review or invented acceptance")
	}
	showLifecyclePayload(t, binary, repo, eventRef(opened, "review"), "verdict", "changes_required")
	showLifecyclePayload(t, binary, repo, eventRef(opened, "check"), "outcome", "passed")
	refs := lifecycleQueryRefs(t, binary, repo, request)
	if len(refs) != 3 || !slices.Contains(refs, eventRef(opened, "review")) || !slices.Contains(refs, eventRef(opened, "check")) {
		t.Fatal("initial public reads lost the failed review or passing check")
	}

	// A source change and passing check do not resolve a finding or imply acceptance.
	runFixtureGit(t, repo, nil, "update-ref", "refs/heads/main", corrected.Source.Revision)
	request.SourceRevision = corrected.Source.Revision
	checkInput := corrected
	checkInput.OperationID, checkInput.Events = "correction-check", corrected.Events[:1]
	checked := record(checkInput)
	state = project("not_recorded")
	assertLifecycleFinding(t, state, eventRef(opened, "review"), "open", nil)
	showLifecyclePayload(t, binary, repo, eventRef(checked, "check-fixed"), "outcome", "passed")

	corrected.Events[1].Payload["resolves"].([]any)[0].(map[string]any)["review_event_ref"] = eventRef(opened, "review")
	reviewInput := corrected
	reviewInput.OperationID, reviewInput.Events = "resolution", corrected.Events[1:2]
	reviewed := record(reviewInput)
	state = project("not_recorded")
	assertLifecycleFinding(t, state, eventRef(opened, "review"), "resolved", []string{eventRef(reviewed, "review-fixed")})
	if len(state.RecordedDecisions) != 0 {
		t.Fatal("resolution invented acceptance")
	}
	acceptInput := corrected
	acceptInput.OperationID, acceptInput.Events = "acceptance", corrected.Events[2:]
	acceptInput.Events[0].Payload["check_refs"] = []string{eventRef(checked, "check-fixed")}
	acceptInput.Events[0].Payload["review_refs"] = []string{eventRef(reviewed, "review-fixed")}
	acceptInput.Events[0].CausedBy = []string{eventRef(checked, "check-fixed"), eventRef(reviewed, "review-fixed")}
	accepted := record(acceptInput)
	state = project("supported")
	if len(state.RecordedDecisions) != 1 || state.RecordedDecisions[0].EventRef != eventRef(accepted, "accept") || state.RecordedDecisions[0].Source != corrected.Source {
		t.Fatalf("acceptance lost exact identity: %+v", state.RecordedDecisions)
	}
	showLifecyclePayload(t, binary, repo, eventRef(accepted, "accept"), "decision", "accepted")
	refs = lifecycleQueryRefs(t, binary, repo, request)
	if len(refs) != 6 || !slices.Contains(refs, eventRef(accepted, "accept")) || !slices.Contains(refs, eventRef(reviewed, "review-fixed")) || !slices.Contains(refs, eventRef(checked, "check-fixed")) {
		t.Fatal("supported public reads lost selected acceptance proof")
	}
	t.Log("failed review -> passing correction -> explicit resolution -> explicit accepted decision: supported")

	before := validationStoreSnapshot(t, repo)
	failure := runErrorJSON(t, 2, binary, lifecycleRecordArgs(t, repo, key, inputs["partial-outcome"])...)
	if failure["details"].(map[string]any)["schema_validation"] != "invalid" || !reflect.DeepEqual(before, validationStoreSnapshot(t, repo)) {
		t.Fatal("invalid partial outcome changed store state")
	}
	if err := os.WriteFile(filepath.Join(repo, "proof.txt"), []byte("mutable proof changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	assessed := runJSON(t, binary, "evidence", "check", "--repo", repo, "--event-ref", eventRef(opened, "check"), "--json")["assessments"].([]any)
	if len(assessed) != 2 || assessed[0].(map[string]any)["status"] != "matching" || assessed[1].(map[string]any)["status"] != "changed" {
		t.Fatalf("immutable and mutable assessments: %v", assessed)
	}
	if restored := runFixtureGit(t, repo, nil, "show", initial.Source.Revision+":proof.txt"); !bytes.Equal(restored, proof) || canonical.Digest(restored) != mutable["digest"] {
		t.Fatal("original proof bytes were not recoverable")
	}
	project("supported")

	// Reuse the owned-process crash harness; a full event batch keeps stdout blocked.
	recoveryInput := corrected
	recoveryInput.OperationID = "lifecycle-lost-response"
	recoveryInput.Events = make([]development.InputEvent, int(ledger.Phase2Limits.EventsPerCommit))
	for i := range recoveryInput.Events {
		event := corrected.Events[0]
		event.LocalID, event.Payload = fmt.Sprintf("recovery-%04d", i), maps.Clone(event.Payload)
		event.Evidence, event.Payload["evidence_indexes"] = []map[string]any{}, []int{}
		recoveryInput.Events[i] = event
	}
	recovered := recoverRecordLostResponse(t, binary, repo, lifecycleRecordArgs(t, repo, key, recoveryInput), len(recoveryInput.Events))
	project("supported")
	t.Log("owned process killed after verified admission; index removed; exact retry recovered one canonical operation")

	// The unpublished projector has no small-output budget. Large required facts must survive intact.
	large := inputs["open-finding"]
	large.OperationID, large.Source, large.Events = "large-blocker", corrected.Source, large.Events[1:2]
	summary := strings.Repeat("<quoted>\"\\\n", 9000) + "required final blocking fact"
	large.Events[0].Payload["findings"].([]any)[0].(map[string]any)["summary"] = summary
	blocked := record(large)
	state = project("unsupported")
	found := false
	for _, finding := range state.Findings {
		if finding.ReviewEventRef == eventRef(blocked, "review") && finding.Summary == summary && finding.Status == "open" {
			found = true
		}
	}
	if !found {
		t.Fatal("large required finding was truncated or dropped")
	}
	payload := showLifecyclePayload(t, binary, repo, eventRef(blocked, "review"), "verdict", "changes_required")
	if payload["findings"].([]any)[0].(map[string]any)["summary"] != summary {
		t.Fatal("raw read truncated the required finding")
	}
	refs = lifecycleQueryRefs(t, binary, repo, request)
	lastRecovery := fmt.Sprintf("recovery-%04d", len(recoveryInput.Events)-1)
	if len(refs) != 7+len(recoveryInput.Events) || !slices.Contains(refs, eventRef(blocked, "review")) || !slices.Contains(refs, eventRef(recovered, lastRecovery)) {
		t.Fatalf("paged history lost facts: count=%d", len(refs))
	}
	before = validationStoreSnapshot(t, repo)
	large.OperationID = "over-input-limit"
	large.Events[0].Payload["findings"].([]any)[0].(map[string]any)["summary"] = strings.Repeat("x", int(ledger.Phase2Limits.ObjectBytes))
	failure = runErrorJSON(t, 2, binary, lifecycleRecordArgs(t, repo, key, large)...)
	if failure["error"] != "record input could not be read within the object byte limit" || !reflect.DeepEqual(before, validationStoreSnapshot(t, repo)) {
		t.Fatalf("oversized input was not refused without mutation: %v", failure)
	}
	t.Logf("retained %d-byte required finding and all %d paged event refs; over-4-MiB input refused", len(summary), len(refs))

	// Raw explicit parent selection creates a causal competitor, regardless of wall-clock ordering.
	rejected := inputs["rejected"]
	rejected.Events[1].Payload["resolves"].([]any)[0].(map[string]any)["review_event_ref"] = eventRef(opened, "review")
	competing := commitReadFixture(t, binary, repo, key, rejected, opened["object_id"].(string))
	runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
	state = project("unresolved")
	if len(state.Frontier) != 2 || len(state.RecordedDecisions) != 2 || !slices.Contains(state.SupportedAcceptance.DecisionRefs, eventRef(competing, "accept")) || !slices.Contains(state.SupportedAcceptance.DecisionRefs, eventRef(accepted, "accept")) {
		t.Fatalf("concurrent decisions lost: %+v", state.SupportedAcceptance)
	}
	showLifecyclePayload(t, binary, repo, eventRef(competing, "accept"), "decision", "rejected")
	refs = lifecycleQueryRefs(t, binary, repo, request)
	if len(refs) != 10+len(recoveryInput.Events) || !slices.Contains(refs, eventRef(competing, "accept")) {
		t.Fatal("paged history omitted the competing decision")
	}
	verified := runJSON(t, binary, "verify", "--repo", repo, "--strict", "--json")
	if verified["ok"] != true || verified["counts"].(map[string]any)["commits"] != float64(7) {
		t.Fatalf("final canonical verification: %v", verified)
	}
	after := validationStoreSnapshot(t, repo)
	for path, digest := range protected {
		if (strings.Contains(path, "/objects/") || strings.HasSuffix(path, "/trust.json")) && after[path] != digest {
			t.Fatalf("protected canonical or trust hash changed: %s", path)
		}
	}
	t.Log("concurrent accepted/rejected decisions remain unresolved; preexisting canonical and trust hashes unchanged")
}

func lifecycleRecordArgs(t *testing.T, repo, key string, input development.RecordInput) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "record.json")
	writeTrialJSON(t, path, input)
	return []string{"record", "--repo", repo, "--profile", development.Profile, "--input", path, "--key-file", key, "--json"}
}

func assertLifecycleFinding(t *testing.T, state development.WorkState, review, status string, resolutions []string) {
	t.Helper()
	if len(state.Findings) != 1 {
		t.Fatalf("findings = %+v", state.Findings)
	}
	finding := state.Findings[0]
	if finding.ReviewEventRef != review || finding.FindingID != "finding-1" || finding.Severity != "important" || finding.Status != status || !slices.Equal(finding.ResolvedBy, resolutions) {
		t.Fatalf("finding = %+v", finding)
	}
}

func showLifecyclePayload(t *testing.T, binary, repo, ref, field string, want any) map[string]any {
	t.Helper()
	shown := runJSON(t, binary, "show", "--repo", repo, ref, "--json")
	payload := shown["event"].(map[string]any)["payload"].(map[string]any)
	if payload[field] != want {
		t.Fatalf("%s %s=%v, want %v", ref, field, payload[field], want)
	}
	return payload
}

func lifecycleQueryRefs(t *testing.T, binary, repo string, request development.ProjectRequest) []string {
	t.Helper()
	refs, seen, cursor := []string{}, map[string]bool{}, ""
	for {
		args := []string{"query", "--repo", repo, "--namespace", request.Namespace, "--subject", request.Subject, "--limit", "100", "--json"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		result := runJSON(t, binary, args...)
		for _, ref := range queryReadRefs(t, result) {
			if seen[ref] {
				t.Fatalf("duplicate paged event %s", ref)
			}
			seen[ref] = true
			refs = append(refs, ref)
		}
		page := result["page"].(map[string]any)
		next, _ := page["next_cursor"].(string)
		if page["has_more"] == false {
			if next != "" {
				t.Fatal("terminal page supplied a cursor")
			}
			return refs
		}
		if next == "" || next == cursor {
			t.Fatal("incomplete page did not advance cursor")
		}
		cursor = next
	}
}
