// ABOUTME: Carries the public development corpus through the compiled raw PACT CLI.
// ABOUTME: Proves signed transport and exact synthetic Git sources without claiming domain validation.
package e2e

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pact/internal/canonical"
	"pact/internal/development"
)

func TestCLIDevelopmentCorpusRawTransport(t *testing.T) {
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)
	repo := filepath.Join(workspace, "repo")
	mustMkdir(t, repo)
	fixtureRoot := filepath.Join(root, "tests/fixtures/development")
	source, err := os.ReadFile(filepath.Join(fixtureRoot, "source.fast-import"))
	if err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, repo, nil, "init", "--quiet", "--object-format=sha1")
	runFixtureGit(t, repo, source, "fast-import", "--quiet")
	var inputs map[string]development.RecordInput
	raw, err := os.ReadFile(filepath.Join(fixtureRoot, "inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&inputs); err != nil {
		t.Fatal(err)
	}
	// Re-imported committed sources must match the identities in public inputs.
	if got := string(bytes.TrimSpace(runFixtureGit(t, repo, nil, "rev-parse", "main"))); got != inputs["corrected"].Source.Revision {
		t.Fatalf("corrected fixture revision = %s", got)
	}
	if got := string(bytes.TrimSpace(runFixtureGit(t, repo, nil, "rev-parse", "main^"))); got != inputs["open-finding"].Source.Revision {
		t.Fatalf("initial fixture revision = %s", got)
	}
	key := filepath.Join(workspace, "fixture.key.json")
	runJSON(t, binary, "setup", "--repo", repo, "--namespace", "org/example/development", "--actor", "Synthetic Fixture", "--key-file", key, "--json")
	priorReview := ""
	for _, name := range []string{"open-finding", "corrected", "partial-outcome"} {
		input := inputs[name]
		if name == "corrected" {
			input.Events[1].Payload["resolves"].([]any)[0].(map[string]any)["review_event_ref"] = priorReview
		}
		batch := developmentRawBatch(t, input)
		path := filepath.Join(workspace, name+".json")
		encoded, err := json.Marshal(batch)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		result := runJSON(t, binary, "commit", "--repo", repo, "--key-file", key, "--events", path, "--json")
		for _, event := range input.Events {
			ref := "pact:event:" + result["object_id"].(string) + "#" + event.LocalID
			if name == "open-finding" && event.LocalID == "review" {
				priorReview = ref
			}
			shown := runJSON(t, binary, "show", "--repo", repo, ref, "--json")
			payload := shown["event"].(map[string]any)["payload"].(map[string]any)
			if payload["operation_id"] != input.OperationID || payload["source"].(map[string]any)["revision"] != input.Source.Revision {
				t.Fatalf("source/operation lost: %#v", payload)
			}
			if name == "partial-outcome" && event.LocalID == "check" && payload["outcome"] != "partial" {
				t.Fatal("raw command reinterpreted domain payload")
			}
			// The signed proof points to exact retained bytes in the selected fixture repository.
			proof := runFixtureGit(t, repo, nil, "show", input.Source.Revision+":proof.txt")
			if canonical.Digest(proof) != event.Evidence[0]["digest"] {
				t.Fatal("fixture proof digest mismatch")
			}
		}
	}
	verified := runJSON(t, binary, "verify", "--repo", repo, "--strict", "--json")
	if verified["ok"] != true || verified["counts"].(map[string]any)["commits"] != float64(3) {
		t.Fatalf("verify: %#v", verified)
	}
}

func developmentRawBatch(t *testing.T, input development.RecordInput) map[string]any {
	t.Helper()
	definitions, artifacts := development.EventDefinitions(), development.ProfileArtifacts()
	events := make([]any, 0, len(input.Events))
	for _, event := range input.Events {
		definition := definitions[event.Type]
		payload := make(map[string]any, len(event.Payload)+2)
		maps.Copy(payload, event.Payload)
		payload["operation_id"] = input.OperationID
		payload["source"] = input.Source
		events = append(events, map[string]any{"local_id": event.LocalID, "kind": definition.Kind, "type": event.Type, "subject": input.Subject, "schema_ref": canonical.Digest(artifacts[definition.Artifact]), "payload": payload, "evidence": event.Evidence, "caused_by": event.CausedBy, "supersedes": event.Supersedes, "tags": []string{}})
	}
	return map[string]any{"namespace": input.Namespace, "events": events}
}

func runFixtureGit(t *testing.T, repo string, stdin []byte, args ...string) []byte {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	// Hooks export repository paths; a fixture must use only its selected temp repo.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Stdin = bytes.NewReader(stdin)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %v\n%s", args, err, output)
	}
	return output
}

func TestFixtureGitIgnoresHookEnvironment(t *testing.T) {
	// Establish the outer fixture safely even if the subprocess filter regresses.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "GIT_") {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	workspace := t.TempDir()
	repo, outside := filepath.Join(workspace, "target"), filepath.Join(workspace, "outside")
	mustMkdir(t, repo)
	mustMkdir(t, outside)
	source, err := os.ReadFile(filepath.Join(projectRoot(t), "tests/fixtures/development/source.fast-import"))
	if err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, outside, nil, "init", "--quiet", "--object-format=sha1")
	runFixtureGit(t, outside, source, "fast-import", "--quiet")
	runFixtureGit(t, outside, nil, "read-tree", "main")
	before := make(map[string][]byte)
	for _, name := range []string{"config", "refs/heads/main", "index"} {
		before[name], err = os.ReadFile(filepath.Join(outside, ".git", name))
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GIT_DIR", filepath.Join(outside, ".git"))
	t.Setenv("GIT_WORK_TREE", outside)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(outside, ".git", "index"))
	t.Setenv("GIT_COMMON_DIR", filepath.Join(outside, ".git"))
	runFixtureGit(t, repo, nil, "init", "--quiet", "--object-format=sha1")
	runFixtureGit(t, repo, source, "fast-import", "--quiet")
	runFixtureGit(t, repo, nil, "read-tree", "main")
	got := string(bytes.TrimSpace(runFixtureGit(t, repo, nil, "rev-parse", "--show-toplevel")))
	resolved, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got != resolved {
		t.Errorf("fixture repository = %s, want %s; hook environment escaped temp repo", got, resolved)
	}
	for name, raw := range before {
		after, err := os.ReadFile(filepath.Join(outside, ".git", name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, after) {
			t.Errorf("outer repository %s changed", name)
		}
	}
}
