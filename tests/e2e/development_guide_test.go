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
	"strings"
	"testing"
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
}
