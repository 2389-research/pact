// ABOUTME: Executes the canonical development guide example with real runtime bindings.
// ABOUTME: Checks public recording, exact retries, Git evidence, and indexed reads without mock services.
package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"pact/internal/canonical"
)

func TestCLIDevelopmentGuideExample(t *testing.T) {
	root, workspace := projectRoot(t), t.TempDir()
	binary, repo := filepath.Join(workspace, "pact"), filepath.Join(workspace, "project")
	key := filepath.Join(workspace, "key.json")
	buildBinary(t, root, binary)
	mustMkdir(t, repo)
	source, err := os.ReadFile(filepath.Join(root, "tests/fixtures/development/source.fast-import"))
	if err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, repo, nil, "init", "--quiet", "--object-format=sha1")
	runFixtureGit(t, repo, source, "fast-import", "--quiet")
	runFixtureGit(t, repo, nil, "symbolic-ref", "HEAD", "refs/heads/main")
	runJSON(t, binary, "setup", "--repo", repo, "--namespace", "org/example/development", "--actor", "Guide operator", "--key-file", key, "--json")
	guide, err := os.ReadFile(filepath.Join(root, "docs/pact-ledger/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, found := strings.Cut(string(guide), "<!-- development-record-example -->\n\n```sh\n")
	if !found {
		t.Fatal("guide lacks executable development example")
	}
	script, _, found := strings.Cut(tail, "\n```")
	if !found {
		t.Fatal("guide example has no closing fence")
	}
	command := exec.Command("sh", "-eu")
	command.Stdin = strings.NewReader(script)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "pact="+binary, "repo="+repo, "work_dir="+workspace, "key_file="+key)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("authored guide example: %v\n%s", err, output)
	}
	read := func(name string) map[string]any {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(workspace, name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	created, retried := read("record-result"), read("retry-result")
	if created["admission"] != "created" || retried["admission"] != "existing" || created["object_id"] != retried["object_id"] || !reflect.DeepEqual(created["event_refs"], retried["event_refs"]) {
		t.Fatalf("guide lost exact retry identity: created=%v retried=%v", created, retried)
	}
	if created["index_state"] != "current" || created["schema_validation"] != "valid" || created["evidence_state"] != "matching" {
		t.Fatalf("guide result lost verified layers: %v", created)
	}
	ref := created["event_refs"].([]any)[0].(string)
	shown := runJSON(t, binary, "show", "--repo", repo, ref, "--json")
	event := shown["event"].(map[string]any)
	if event["type"] != "dev.check.executed" || event["payload"].(map[string]any)["outcome"] != "passed" {
		t.Fatalf("guide created wrong event: %v", event)
	}
	assessment := runJSON(t, binary, "evidence", "check", "--repo", repo, "--event-ref", ref, "--json")["assessments"].([]any)[0].(map[string]any)
	if assessment["status"] != "matching" || assessment["bytes"] != float64(77) {
		t.Fatalf("guide did not recover exact Git proof: %v", assessment)
	}
	proof, err := os.ReadFile(filepath.Join(workspace, "proof.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(proof, runFixtureGit(t, repo, nil, "show", "HEAD:proof.txt")) {
		t.Fatal("guide proof differs from exact Git blob")
	}
	_, tail, found = strings.Cut(string(guide), "<!-- development-brief-example -->\n\n```sh\n")
	if !found {
		t.Fatal("guide lacks executable native brief example")
	}
	script, _, found = strings.Cut(tail, "\n```")
	if !found {
		t.Fatal("brief example has no closing fence")
	}
	command = exec.Command("sh", "-eu")
	command.Stdin = strings.NewReader(script)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "pact="+binary, "repo="+repo, "work_dir="+workspace)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("authored brief example: %v\n%s", err, output)
	}
	state := read("brief-result")
	checks := state["checks"].([]any)
	if state["operation"] != "brief" || state["subject"] != "widget/task-1" || len(checks) != 1 || state["supported_acceptance"].(map[string]any)["status"] != "not_recorded" {
		t.Fatalf("brief lost recorded check or inferred acceptance: %v", state)
	}
	check := checks[0].(map[string]any)
	if check["event_ref"] != ref || check["source"].(map[string]any)["revision"] != strings.TrimSpace(string(runFixtureGit(t, repo, nil, "rev-parse", "HEAD"))) {
		t.Fatalf("brief lost exact source or reference: %v", check)
	}
	for _, link := range regexp.MustCompile(`\]\((references/[^)#]+)(?:#[^)]*)?\)`).FindAllStringSubmatch(string(guide), -1) {
		if _, err := os.Stat(filepath.Join(root, "docs/pact-ledger", link[1])); err != nil {
			t.Errorf("guide link %s: %v", link[1], err)
		}
	}
	help := exec.Command(binary, "brief", "--help")
	output, err := help.CombinedOutput()
	if err != nil {
		t.Fatalf("brief help: %v: %s", err, output)
	}
	for _, flag := range []string{"--profile", "--namespace", "--subject", "--source-revision", "--frontier", "--max-bytes", "--json"} {
		if !bytes.Contains(output, []byte(flag)) {
			t.Errorf("native help omits guide flag %s", flag)
		}
	}
}

// Retain public stores for two read-only readers without running either read path.
// Signers stay outside the stores and disappear with the test's temporary directory.
func TestCLIBriefGuideReaderFixtures(t *testing.T) {
	workspace := os.Getenv("PACT_GUIDE_READER_DIR")
	if workspace == "" {
		t.Skip("set PACT_GUIDE_READER_DIR to retain public reader fixtures")
	}
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatalf("reader fixture directory must not exist: %v", err)
	}
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, projectRoot(t), binary)
	goals := map[string]any{}
	for _, scenario := range []string{"open", "complete", "conflict"} {
		fixture := newBoundedFixture(t, workspace, binary, scenario, boundedInputs(t))
		goals[scenario] = map[string]any{"repo": fixture.trial.Repo, "request": fixture.trial.Request}
		protected := map[string]string{}
		for name, raw := range snapshotSetupFiles(t, fixture.trial.Repo, fixture.key) {
			if relative, err := filepath.Rel(fixture.trial.Repo, name); err == nil && !strings.HasPrefix(relative, "..") {
				protected[relative] = canonical.Digest(raw)
			}
		}
		writeTrialJSON(t, filepath.Join(workspace, scenario+"-protected.json"), protected)
	}
	writeTrialJSON(t, filepath.Join(workspace, "goals.json"), goals)
}
