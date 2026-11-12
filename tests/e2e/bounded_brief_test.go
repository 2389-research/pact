// ABOUTME: Verifies the bounded public brief against real signed public fixture histories.
// ABOUTME: Measures native CLI reads and retains complete output, limits, and read-only proof.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"pact/internal/canonical"
	"pact/internal/development"
	"pact/internal/store"
)

type boundedFixture struct {
	trial developmentReadTrial
	key   string
}

func boundedInputs(t *testing.T) map[string]development.RecordInput {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(projectRoot(t), "tests/fixtures/development/inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inputs map[string]development.RecordInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&inputs); err != nil {
		t.Fatal(err)
	}
	return inputs
}

func newBoundedFixture(t *testing.T, workspace, binary, scenario string, inputs map[string]development.RecordInput) boundedFixture {
	t.Helper()
	repo := filepath.Join(workspace, scenario)
	mustMkdir(t, repo)
	source, err := os.ReadFile(filepath.Join(projectRoot(t), "tests/fixtures/development/source.fast-import"))
	if err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, repo, nil, "init", "--quiet", "--object-format=sha1")
	runFixtureGit(t, repo, source, "fast-import", "--quiet")
	key := filepath.Join(t.TempDir(), "key.json")
	runJSON(t, binary, "setup", "--repo", repo, "--namespace", "org/example/development", "--actor", "Synthetic Fixture", "--key-file", key, "--json")
	initial := commitReadFixture(t, binary, repo, key, inputs["open-finding"], "")
	objects := []string{initial["object_id"].(string)}
	refs := []string{eventRef(initial, "check"), eventRef(initial, "review")}
	revision := inputs["open-finding"].Source.Revision
	if scenario == "complete" || scenario == "conflict" {
		correction := inputs["corrected"]
		correction.Events[1].Payload["resolves"].([]any)[0].(map[string]any)["review_event_ref"] = eventRef(initial, "review")
		corrected := commitReadFixture(t, binary, repo, key, correction, "")
		objects = append(objects, corrected["object_id"].(string))
		refs = append(refs, eventRef(corrected, "check-fixed"), eventRef(corrected, "review-fixed"), eventRef(corrected, "accept"))
		revision = correction.Source.Revision
		if scenario == "conflict" {
			rejected := inputs["rejected"]
			rejected.Events[1].Payload["resolves"].([]any)[0].(map[string]any)["review_event_ref"] = eventRef(initial, "review")
			result := commitReadFixture(t, binary, repo, key, rejected, initial["object_id"].(string))
			objects = append(objects, result["object_id"].(string))
			refs = append(refs, eventRef(result, "check-fixed"), eventRef(result, "review-fixed"), eventRef(result, "accept"))
		}
	}
	runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
	return boundedFixture{trial: developmentReadTrial{Binary: binary, Repo: repo, Request: development.ProjectRequest{Namespace: "org/example/development", Subject: "widget/task-1", SourceRevision: revision}, Objects: objects, EvidenceRefs: refs}, key: key}
}

func boundedArgs(trial developmentReadTrial) []string {
	args := make([]string, 0, 11+2*len(trial.Request.Frontier))
	args = append(args, "brief", "--repo", trial.Repo, "--profile", "development/v1", "--namespace", trial.Request.Namespace, "--subject", trial.Request.Subject, "--source-revision", trial.Request.SourceRevision)
	for _, frontier := range trial.Request.Frontier {
		args = append(args, "--frontier", frontier)
	}
	return args
}

// This check compares every owner field, then applies an independent public-corpus rubric.
func assertBoundedState(t *testing.T, trial developmentReadTrial, raw []byte, scenario string) {
	t.Helper()
	var got development.WorkState
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(trial.Repo)
	if err != nil {
		t.Fatal(err)
	}
	want, err := development.Project(context.Background(), st, trial.Request)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope["operation"] != "brief" || envelope["work_state"] != nil {
		t.Fatalf("brief envelope: %s: %v", raw, err)
	}
	delete(envelope, "operation")
	wantRaw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]any
	if err := json.Unmarshal(wantRaw, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(envelope, expected) {
		t.Fatalf("adapter lost or changed owner JSON fields: got=%s want=%s", raw, wantRaw)
	}
	if got.SupportedAcceptance.Status != readEvaluationExpected(t)[scenario] {
		t.Fatalf("unsupported acceptance: %+v", got.SupportedAcceptance)
	}
	if len(got.Findings) != 1 || got.Findings[0].FindingID != "finding-1" || got.Findings[0].Severity != "important" || got.Findings[0].Summary != "Synthetic input loses a declared fact." || got.Findings[0].ReviewEventRef != "pact:event:"+trial.Objects[0]+"#review" {
		t.Fatalf("lost Important finding identity: %+v", got.Findings)
	}
	if (got.Findings[0].Status == "open") != (scenario == "open") || len(got.Findings[0].ResolvedBy) != len(trial.Objects)-1 {
		t.Fatalf("resolution changed: %+v", got.Findings)
	}
	wantDecisions := len(trial.Objects) - 1
	wantChecks := max(1, wantDecisions)
	if len(got.RecordedDecisions) != wantDecisions || len(got.Checks) != wantChecks || len(got.DeclaredNextActions) != 1 || len(got.EvidenceAssessments) != len(trial.EvidenceRefs) {
		t.Fatalf("required facts missing: %+v", got)
	}
	for _, check := range got.Checks {
		if check.Payload["outcome"] != "passed" || check.Source.Revision != trial.Request.SourceRevision {
			t.Fatalf("check changed: %+v", check)
		}
	}
	decisions := make([]string, 0, len(got.RecordedDecisions))
	for _, decision := range got.RecordedDecisions {
		decisions = append(decisions, decision.Payload["decision"].(string))
	}
	slices.Sort(decisions)
	wantedDecisions := map[string][]string{"open": {}, "complete": {"accepted"}, "conflict": {"accepted", "rejected"}}[scenario]
	if !reflect.DeepEqual(decisions, wantedDecisions) || (scenario == "conflict" && !slices.Contains(got.SupportedAcceptance.Reasons, "concurrent_incompatible_decisions")) {
		t.Fatalf("decision conflict hidden: %+v", got)
	}
	for _, assessment := range got.EvidenceAssessments {
		if !slices.Contains(trial.EvidenceRefs, assessment.EventRef) || assessment.Assessment.Status != "matching" || !assessment.Durable || assessment.Assessment.Bytes != 77 || assessment.Assessment.ActualDigest != "sha256:c3f58fa85abd4403a63dc44eb205a4be351054f86f0f748865d4094160d3f446" {
			t.Fatalf("actual evidence changed: %+v", assessment)
		}
	}
	if got.Completeness.Status != "locally_closed" || got.Completeness.GlobalCompleteness != "unknown" || got.Completeness.Authority != "locally_configured_root" || got.Completeness.ReviewerIndependence != "not_established" || got.ProjectorIdentity.Digest == "" || got.PolicyDigest == "" || got.ProfileDigest == "" {
		t.Fatalf("identity or limits lost: %+v", got)
	}
}

func TestCLIBoundedBriefNativeEvaluation(t *testing.T) {
	workspace := t.TempDir()
	if retained := os.Getenv("PACT_BOUNDED_BRIEF_DIR"); retained != "" {
		workspace = retained
		if err := os.Mkdir(workspace, 0700); err != nil {
			t.Fatalf("retained directory must not exist: %v", err)
		}
	}
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, projectRoot(t), binary)
	for _, scenario := range []string{"open", "complete", "conflict"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newBoundedFixture(t, workspace, binary, scenario, boundedInputs(t))
			trial := fixture.trial
			writeTrialJSON(t, filepath.Join(workspace, scenario+"-trial.json"), trial)
			before := snapshotSetupFiles(t, trial.Repo, fixture.key)
			baselineBytes, baselineCalls, baselineInput := 0, 0, 0
			capture := func(name string, args ...string) []byte {
				baselineCalls++
				for _, arg := range args {
					baselineInput += len(arg) + 1
				}
				raw := runReadCapture(t, workspace, scenario+"-"+name, binary, args...)
				baselineBytes += len(raw)
				return raw
			}
			query := capture("query", "query", "--repo", trial.Repo, "--namespace", trial.Request.Namespace, "--subject", trial.Request.Subject, "--json")
			var verification map[string]any
			if err := json.Unmarshal(capture("verify", "verify", "--repo", trial.Repo, "--strict", "--json"), &verification); err != nil || verification["ok"] != true {
				t.Fatalf("baseline verification: %+v: %v", verification, err)
			}
			shownRefs := make([]string, 0, 3*len(trial.Objects))
			for i, object := range trial.Objects {
				var shown map[string]any
				if err := json.Unmarshal(capture(fmt.Sprintf("show-%d", i), "show", "--repo", trial.Repo, object, "--json"), &shown); err != nil || shown["integrity"] != "valid" || shown["authenticity"] != "valid" {
					t.Fatalf("baseline object: %+v: %v", shown, err)
				}
				for _, item := range shown["object"].(map[string]any)["body"].(map[string]any)["events"].([]any) {
					event := item.(map[string]any)
					if event["payload"] == nil || event["evidence"] == nil {
						t.Fatalf("baseline omitted payload or evidence: %+v", event)
					}
					shownRefs = append(shownRefs, "pact:event:"+object+"#"+event["local_id"].(string))
				}
			}
			for i, ref := range trial.EvidenceRefs {
				var checked map[string]any
				if err := json.Unmarshal(capture(fmt.Sprintf("evidence-%d", i), "evidence", "check", "--repo", trial.Repo, "--event-ref", ref, "--json"), &checked); err != nil || checked["event_ref"] != ref {
					t.Fatalf("baseline evidence: %+v: %v", checked, err)
				}
				for _, assessment := range checked["assessments"].([]any) {
					if assessment.(map[string]any)["status"] != "matching" {
						t.Fatalf("baseline evidence did not match: %+v", checked)
					}
				}
			}
			args := append(boundedArgs(trial), "--json")
			brief := runReadCapture(t, workspace, scenario+"-brief", binary, args...)
			assertBoundedState(t, trial, brief, scenario)
			var queryPage map[string]any
			if err := json.Unmarshal(query, &queryPage); err != nil {
				t.Fatal(err)
			}
			refs := queryReadRefs(t, queryPage)
			slices.Sort(refs)
			slices.Sort(shownRefs)
			if len(refs) != 3*len(trial.Objects) || !reflect.DeepEqual(refs, shownRefs) || queryPage["page"].(map[string]any)["next_cursor"] != nil {
				t.Fatalf("baseline lost event refs: %v", refs)
			}
			if len(brief) > baselineBytes || baselineCalls <= 1 {
				t.Fatalf("promotion gate: brief bytes=%d baseline=%d, calls=1:%d", len(brief), baselineBytes, baselineCalls)
			}
			if !reflect.DeepEqual(before, snapshotSetupFiles(t, trial.Repo, fixture.key)) {
				t.Fatal("reads changed canonical, trust, index, Git, evidence, or external key bytes")
			}
			protected := map[string]string{}
			for path, raw := range before {
				if relative, err := filepath.Rel(trial.Repo, path); err == nil && !strings.HasPrefix(relative, "..") {
					protected[relative] = canonical.Digest(raw)
				}
			}
			writeTrialJSON(t, filepath.Join(workspace, scenario+"-protected-hashes.json"), protected)
			briefInput := 0
			for _, arg := range args {
				briefInput += len(arg) + 1
			}
			writeTrialJSON(t, filepath.Join(workspace, scenario+"-metrics.json"), map[string]any{"baseline_cli_calls": baselineCalls, "brief_cli_calls": 1, "baseline_stdout_bytes": baselineBytes, "brief_stdout_bytes": len(brief), "baseline_argv_bytes": baselineInput, "brief_argv_bytes": briefInput, "baseline_failures": 0, "brief_failures": 0, "truncation": false, "missed_or_changed_required_facts": 0, "unsupported_acceptance": 0, "outer_shell_calls_per_path": 1, "outer_shell_scope": "both paths execute inside this one go test invocation", "excluded_work": "fixture creation, build, owner parity assertion and guard tests; internal subprocesses are uninstrumented"})
			t.Logf("%s baseline CLI=%d stdout=%d input argv=%d; brief CLI=1 stdout=%d input argv=%d; failures=0; truncation=false; facts lost=0", scenario, baselineCalls, baselineBytes, baselineInput, len(brief), briefInput)
		})
	}
}

func boundedProcessFailure(t *testing.T, binary string, args []string, code int) []byte {
	t.Helper()
	command := exec.Command(binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	exit, isExit := errors.AsType[*exec.ExitError](err)
	if !isExit || exit.ExitCode() != code || stdout.Len() != 0 || stderr.Len() >= 1024 {
		t.Fatalf("expected bounded error %d: err=%v stdout=%q stderr=%q", code, err, &stdout, &stderr)
	}
	return stderr.Bytes()
}

func boundedFailure(t *testing.T, binary string, args []string, code int) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(boundedProcessFailure(t, binary, args, code), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertBoundedLimit(t *testing.T, binary string, args []string, asJSON bool, required, budget int) {
	t.Helper()
	if !asJSON {
		raw := boundedProcessFailure(t, binary, args, 2)
		if !strings.Contains(string(raw), strconv.Itoa(required)) || !strings.Contains(string(raw), strconv.Itoa(budget)) {
			t.Fatalf("human limit counts missing: %s", raw)
		}
		return
	}
	failure := boundedFailure(t, binary, args, 2)
	details := failure["details"].(map[string]any)
	if details["code"] != "brief_output_limit" || details["required_bytes"] != float64(required) || details["requested_bytes"] != float64(budget) {
		t.Fatalf("limit counts: %+v", details)
	}
}

func TestCLIBoundedBriefEncodedLimitsAndReadOnlyGuards(t *testing.T) {
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, projectRoot(t), binary)
	inputs := boundedInputs(t)
	summary := "quote \" slash \\ tab\t line\n café ☃ escape\x1b delete\x7f next\u0085"
	inputs["open-finding"].Events[1].Payload["findings"].([]any)[0].(map[string]any)["summary"] = summary
	fixture := newBoundedFixture(t, workspace, binary, "limits", inputs)
	trial := fixture.trial
	before := snapshotSetupFiles(t, trial.Repo, fixture.key)
	for _, extra := range [][]string{{"--frontier", trial.Objects[0], "--frontier", trial.Objects[0]}, {"--namespace", "org/example/other", "--frontier", trial.Objects[0]}} {
		boundedFailure(t, binary, append(append(boundedArgs(trial), extra...), "--json"), 2)
	}
	for _, asJSON := range []bool{false, true} {
		args := boundedArgs(trial)
		if asJSON {
			args = append(args, "--json")
		}
		command := exec.Command(binary, args...)
		raw, err := command.Output()
		if err != nil {
			t.Fatal(err)
		}
		if raw[len(raw)-1] != '\n' || len(raw) < 1025 {
			t.Fatalf("invalid encoded fixture length=%d", len(raw))
		}
		t.Logf("JSON=%t encoded bytes=%d; tested budgets=%d,%d,%d,1024 including escaping and final newline", asJSON, len(raw), len(raw)-1, len(raw), len(raw)+1)
		stateRaw := raw
		if !asJSON {
			for _, character := range string(raw) {
				if unicode.IsControl(character) && character != '\n' {
					t.Fatalf("human output contains terminal control %U", character)
				}
			}
			stateRaw = raw[bytes.IndexByte(raw, '\n')+1:]
		}
		var state development.WorkState
		if err := json.Unmarshal(stateRaw, &state); err != nil || len(state.Findings) != 1 || state.Findings[0].Summary != summary || state.Findings[0].Status != "open" || state.SupportedAcceptance.Status != "not_recorded" {
			t.Fatalf("encoded facts changed: %+v: %v", state, err)
		}
		for _, budget := range []int{len(raw) - 1, len(raw), len(raw) + 1, 1024} {
			bounded := append(slices.Clone(args), "--max-bytes", strconv.Itoa(budget))
			if budget < len(raw) {
				assertBoundedLimit(t, binary, bounded, asJSON, len(raw), budget)
				continue
			}
			got, err := exec.Command(binary, bounded...).Output()
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatalf("encoded boundary %d: err=%v output changed", budget, err)
			}
		}
	}
	explicit := trial
	explicit.Request.Frontier = []string{trial.Objects[0]}
	prior, err := exec.Command(binary, append(boundedArgs(explicit), "--json")...).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshotSetupFiles(t, trial.Repo, fixture.key)) {
		t.Fatal("bounded reads changed protected files")
	}
	commitReadFixture(t, binary, trial.Repo, fixture.key, boundedInputs(t)["open-finding"], "")
	staleBefore := snapshotSetupFiles(t, trial.Repo, fixture.key)
	failure := boundedFailure(t, binary, append(boundedArgs(trial), "--json"), 9)
	if failure["details"].(map[string]any)["code"] != "index_stale" || !reflect.DeepEqual(staleBefore, snapshotSetupFiles(t, trial.Repo, fixture.key)) {
		t.Fatalf("stale index: %+v", failure)
	}
	runJSON(t, binary, "index", "rebuild", "--repo", trial.Repo, "--json")
	after, err := exec.Command(binary, append(boundedArgs(explicit), "--json")...).Output()
	if err != nil || !bytes.Equal(prior, after) {
		t.Fatalf("explicit frontier changed: %v", err)
	}
	indexPath := filepath.Join(trial.Repo, ".pact", "index", "pact-v1.sqlite3")
	for _, corruption := range []string{"missing", "corrupt"} {
		if corruption == "missing" {
			if err := os.Remove(indexPath); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(indexPath, []byte("not sqlite"), 0600); err != nil {
			t.Fatal(err)
		}
		before := snapshotSetupFiles(t, trial.Repo, fixture.key)
		failure := boundedFailure(t, binary, append(boundedArgs(trial), "--json"), 9)
		if failure["details"].(map[string]any)["code"] != "index_"+corruption || !strings.Contains(failure["error"].(string), "index rebuild") || !reflect.DeepEqual(before, snapshotSetupFiles(t, trial.Repo, fixture.key)) {
			t.Fatalf("%s index error or read mutated files: %+v", corruption, failure)
		}
	}
}

func TestCLIBoundedBriefOversizedFindingAndUncertainty(t *testing.T) {
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, projectRoot(t), binary)
	t.Run("oversized Important finding", func(t *testing.T) {
		inputs := boundedInputs(t)
		inputs["open-finding"].Events[1].Payload["findings"].([]any)[0].(map[string]any)["summary"] = strings.Repeat("Important blocker remains open. ", 2400)
		fixture := newBoundedFixture(t, workspace, binary, "oversized", inputs)
		before := snapshotSetupFiles(t, fixture.trial.Repo, fixture.key)
		failure := boundedFailure(t, binary, append(boundedArgs(fixture.trial), "--max-bytes", "65536", "--json"), 2)
		details := failure["details"].(map[string]any)
		if details["code"] != "brief_output_limit" || details["required_bytes"].(float64) <= 65536 || details["requested_bytes"] != float64(65536) || !reflect.DeepEqual(before, snapshotSetupFiles(t, fixture.trial.Repo, fixture.key)) {
			t.Fatalf("oversized blocker silently dropped or read wrote: %+v", failure)
		}
	})
	t.Run("unknown schema", func(t *testing.T) {
		inputs := boundedInputs(t)
		fixture := newBoundedFixture(t, workspace, binary, "unknown", inputs)
		correction := inputs["corrected"]
		correction.Events[1].Payload["resolves"].([]any)[0].(map[string]any)["review_event_ref"] = "pact:event:" + fixture.trial.Objects[0] + "#review"
		batch := developmentRawBatch(t, correction)
		batch["events"].([]any)[1].(map[string]any)["schema_ref"] = "sha256:" + strings.Repeat("3", 64)
		path := filepath.Join(t.TempDir(), "batch.json")
		writeTrialJSON(t, path, batch)
		runJSON(t, binary, "commit", "--repo", fixture.trial.Repo, "--key-file", fixture.key, "--events", path, "--json")
		runJSON(t, binary, "index", "rebuild", "--repo", fixture.trial.Repo, "--json")
		fixture.trial.Request.SourceRevision = correction.Source.Revision
		before := snapshotSetupFiles(t, fixture.trial.Repo, fixture.key)
		result := runJSON(t, binary, append(boundedArgs(fixture.trial), "--json")...)
		if result["supported_acceptance"].(map[string]any)["status"] != "unsupported" || result["completeness"].(map[string]any)["status"] != "inconclusive" || len(result["recorded_decisions"].([]any)) != 1 || result["findings"].([]any)[0].(map[string]any)["status"] != "open" || !reflect.DeepEqual(before, snapshotSetupFiles(t, fixture.trial.Repo, fixture.key)) {
			t.Fatalf("unknown schema disappeared: %+v", result)
		}
	})
	t.Run("unavailable evidence", func(t *testing.T) {
		fixture := newBoundedFixture(t, workspace, binary, "complete", boundedInputs(t))
		gitdir := filepath.Join(fixture.trial.Repo, ".git")
		if err := os.Rename(gitdir, gitdir+"-unavailable"); err != nil {
			t.Fatal(err)
		}
		before := snapshotSetupFiles(t, fixture.trial.Repo, fixture.key)
		result := runJSON(t, binary, append(boundedArgs(fixture.trial), "--json")...)
		if result["supported_acceptance"].(map[string]any)["status"] != "unsupported" || len(result["recorded_decisions"].([]any)) != 1 || !reflect.DeepEqual(before, snapshotSetupFiles(t, fixture.trial.Repo, fixture.key)) {
			t.Fatalf("evidence uncertainty hidden: %+v", result)
		}
		for _, item := range result["evidence_assessments"].([]any) {
			if item.(map[string]any)["assessment"].(map[string]any)["status"] != "unavailable" {
				t.Fatalf("unavailable proof fabricated: %+v", item)
			}
		}
	})
}
