// ABOUTME: Evaluates the unpublished development projection against the existing read recipe.
// ABOUTME: Preserves real compiled command output and verifies complete, read-only retrieval.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"pact/internal/development"
)

type developmentReadTrial struct {
	Binary       string                     `json:"binary"`
	Prototype    string                     `json:"prototype"`
	Repo         string                     `json:"repo"`
	Request      development.ProjectRequest `json:"request"`
	Objects      []string                   `json:"objects"`
	EvidenceRefs []string                   `json:"evidence_refs"`
}

// An optional directory retains only public command outputs and a runnable fixture.
// The generated signing key stays in t.TempDir and is never exported.
func TestCLIDevelopmentReadEvaluation(t *testing.T) {
	workspace := t.TempDir()
	if retained := os.Getenv("PACT_READ_EVALUATION_DIR"); retained != "" {
		workspace = retained
		if err := os.Mkdir(workspace, 0700); err != nil {
			t.Fatalf("retained fixture directory must not exist: %v", err)
		}
	}
	binary, prototype := filepath.Join(workspace, "pact"), filepath.Join(workspace, "projectdriver")
	buildBinary(t, projectRoot(t), binary)
	build := exec.Command("go", "build", "-o", prototype, "./internal/development/testdata/projectdriver")
	build.Dir = projectRoot(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build prototype: %v: %s", err, output)
	}
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
	expected := readEvaluationExpected(t)
	source, err := os.ReadFile(filepath.Join(projectRoot(t), "tests/fixtures/development/source.fast-import"))
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"open", "complete", "conflict"} {
		t.Run(scenario, func(t *testing.T) {
			repo := filepath.Join(workspace, scenario)
			mustMkdir(t, repo)
			runFixtureGit(t, repo, nil, "init", "--quiet", "--object-format=sha1")
			runFixtureGit(t, repo, source, "fast-import", "--quiet")
			key := filepath.Join(t.TempDir(), "key.json")
			runJSON(t, binary, "setup", "--repo", repo, "--namespace", "org/example/development", "--actor", "Synthetic Fixture", "--key-file", key, "--json")
			initial := commitReadFixture(t, binary, repo, key, inputs["open-finding"], "")
			objects := []string{initial["object_id"].(string)}
			evidenceRefs := []string{eventRef(initial, "check"), eventRef(initial, "review")}
			revision := inputs["open-finding"].Source.Revision
			if scenario != "open" {
				correction := inputs["corrected"]
				correction.Events[1].Payload["resolves"].([]any)[0].(map[string]any)["review_event_ref"] = eventRef(initial, "review")
				corrected := commitReadFixture(t, binary, repo, key, correction, "")
				objects = append(objects, corrected["object_id"].(string))
				evidenceRefs = append(evidenceRefs, eventRef(corrected, "check-fixed"), eventRef(corrected, "review-fixed"), eventRef(corrected, "accept"))
				revision = correction.Source.Revision
				if scenario == "conflict" {
					rejected := inputs["rejected"]
					rejected.Events[1].Payload["resolves"].([]any)[0].(map[string]any)["review_event_ref"] = eventRef(initial, "review")
					result := commitReadFixture(t, binary, repo, key, rejected, initial["object_id"].(string))
					objects = append(objects, result["object_id"].(string))
					evidenceRefs = append(evidenceRefs, eventRef(result, "check-fixed"), eventRef(result, "review-fixed"), eventRef(result, "accept"))
				}
			}
			runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
			trial := developmentReadTrial{Binary: binary, Prototype: prototype, Repo: repo, Request: development.ProjectRequest{Namespace: "org/example/development", Subject: "widget/task-1", SourceRevision: revision}, Objects: objects, EvidenceRefs: evidenceRefs}
			writeTrialJSON(t, filepath.Join(workspace, scenario+"-trial.json"), trial)
			before := snapshotSetupFiles(t, repo, key)
			query := runReadCapture(t, workspace, scenario+"-query", binary, "query", "--repo", repo, "--namespace", trial.Request.Namespace, "--subject", trial.Request.Subject, "--json")
			verify := runReadCapture(t, workspace, scenario+"-verify", binary, "verify", "--repo", repo, "--strict", "--json")
			baselineBytes := len(query) + len(verify)
			for i, object := range objects {
				baselineBytes += len(runReadCapture(t, workspace, fmt.Sprintf("%s-show-%d", scenario, i), binary, "show", "--repo", repo, object, "--json"))
			}
			for i, ref := range evidenceRefs {
				baselineBytes += len(runReadCapture(t, workspace, fmt.Sprintf("%s-evidence-%d", scenario, i), binary, "evidence", "check", "--repo", repo, "--event-ref", ref, "--json"))
			}
			request, err := json.Marshal(trial.Request)
			if err != nil {
				t.Fatal(err)
			}
			projected := runReadCapture(t, workspace, scenario+"-prototype", prototype, repo, string(request))
			var state development.WorkState
			if err := json.Unmarshal(projected, &state); err != nil {
				t.Fatal(err)
			}
			want := expected[scenario]
			if state.SupportedAcceptance.Status != want {
				t.Fatalf("support=%s, want %s: %+v", state.SupportedAcceptance.Status, want, state.SupportedAcceptance)
			}
			if len(state.Findings) != 1 || (state.Findings[0].Status == "open") != (scenario == "open") {
				t.Fatalf("lost finding: %+v", state.Findings)
			}
			if len(state.Checks) == 0 || state.Checks[0].Payload["outcome"] != "passed" || len(state.DeclaredNextActions) != 1 {
				t.Fatalf("lost scoped check or next action: %+v", state)
			}
			if len(state.EvidenceAssessments) != len(evidenceRefs) {
				t.Fatalf("evidence=%d want=%d", len(state.EvidenceAssessments), len(evidenceRefs))
			}
			for _, assessment := range state.EvidenceAssessments {
				if assessment.Assessment.Status != "matching" || !assessment.Durable {
					t.Fatalf("evidence=%+v", assessment)
				}
			}
			if !reflect.DeepEqual(before, snapshotSetupFiles(t, repo, key)) {
				t.Fatal("reads changed fixture bytes")
			}
			verifyReadPagination(t, binary, trial, query)
			verifyReadIndexRecovery(t, binary, trial, objects[0])
			t.Logf("%s: baseline CLI calls=%d stdout bytes=%d; prototype CLI calls=1 stdout bytes=%d", scenario, 2+len(objects)+len(evidenceRefs), baselineBytes, len(projected))
		})
	}
}

func readEvaluationExpected(t *testing.T) map[string]string {
	t.Helper()
	fixtureRoot := filepath.Join(projectRoot(t), "tests/fixtures/development")
	var cases map[string]struct {
		Support string `json:"supported_acceptance"`
	}
	var histories map[string]struct {
		Expected struct {
			Support string `json:"supported_acceptance"`
		} `json:"expected"`
	}
	for filename, target := range map[string]any{"expected.json": &cases, "histories.json": &histories} {
		raw, err := os.ReadFile(filepath.Join(fixtureRoot, filename))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	return map[string]string{"open": cases["open-finding"].Support, "complete": cases["corrected"].Support, "conflict": histories["concurrent-incompatible"].Expected.Support}
}

func eventRef(result map[string]any, local string) string {
	return "pact:event:" + result["object_id"].(string) + "#" + local
}

func commitReadFixture(t *testing.T, binary, repo, key string, input development.RecordInput, parent string) map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "batch.json")
	writeTrialJSON(t, path, developmentRawBatch(t, input))
	args := []string{"commit", "--repo", repo, "--key-file", key, "--events", path, "--json"}
	if parent != "" {
		args = append(args, "--parent", parent)
	}
	return runJSON(t, binary, args...)
}
func writeTrialJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
func runReadCapture(t *testing.T, workspace, name, binary string, args ...string) []byte {
	t.Helper()
	command := exec.Command(binary, args...)
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	output, err := command.Output()
	writeTrialJSON(t, filepath.Join(workspace, name+"-command.json"), append([]string{binary}, args...))
	for suffix, raw := range map[string][]byte{".stdout": output, ".stderr": diagnostic.Bytes()} {
		if err := os.WriteFile(filepath.Join(workspace, name+suffix), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err != nil {
		t.Fatalf("%s: %v: %s", name, err, diagnostic.Bytes())
	}
	return output
}
func verifyReadPagination(t *testing.T, binary string, trial developmentReadTrial, complete []byte) {
	t.Helper()
	var all map[string]any
	if err := json.Unmarshal(complete, &all); err != nil {
		t.Fatal(err)
	}
	references := queryReadRefs(t, all)
	var paged []string
	cursor := ""
	for {
		args := []string{"query", "--repo", trial.Repo, "--namespace", trial.Request.Namespace, "--subject", trial.Request.Subject, "--limit", "1", "--json"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		page := runJSON(t, binary, args...)
		paged = append(paged, queryReadRefs(t, page)...)
		next, _ := page["page"].(map[string]any)["next_cursor"].(string)
		if next == "" {
			break
		}
		if next == cursor {
			t.Fatal("cursor did not advance")
		}
		cursor = next
	}
	slices.Sort(references)
	slices.Sort(paged)
	if !reflect.DeepEqual(references, paged) {
		t.Fatalf("tight page lost facts: %v want %v", paged, references)
	}
}
func queryReadRefs(t *testing.T, page map[string]any) []string {
	t.Helper()
	var refs []string
	for _, batch := range page["batches"].([]any) {
		for _, item := range batch.(map[string]any)["items"].([]any) {
			refs = append(refs, item.(map[string]any)["event_ref"].(string))
		}
	}
	return refs
}
func verifyReadIndexRecovery(t *testing.T, binary string, trial developmentReadTrial, object string) {
	t.Helper()
	indexDir := filepath.Join(trial.Repo, ".pact", "index")
	if err := os.Rename(indexDir, indexDir+"-saved"); err != nil {
		t.Fatal(err)
	}
	before := snapshotSetupFiles(t, trial.Repo, trial.Repo)
	failure := runErrorJSON(t, 9, binary, "query", "--repo", trial.Repo, "--subject", trial.Request.Subject, "--json")
	raw, err := json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "index_missing") {
		t.Fatalf("missing index classification lost: %s", raw)
	}
	runJSON(t, binary, "show", "--repo", trial.Repo, object, "--json")
	if !reflect.DeepEqual(before, snapshotSetupFiles(t, trial.Repo, trial.Repo)) {
		t.Fatal("missing-index read wrote files")
	}
	if err := os.Rename(indexDir+"-saved", indexDir); err != nil {
		t.Fatal(err)
	}
}
