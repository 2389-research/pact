// ABOUTME: Exercises reliable development recording through real compiled CLI processes.
// ABOUTME: Checks retry identity, conflicting contenders, and recoverable index failures.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"pact/internal/ledger"
	"pact/internal/store"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"pact/internal/canonical"
)

func TestCLIDevelopmentRecord(t *testing.T) {
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, projectRoot(t), binary)
	repo, key := filepath.Join(workspace, "repo"), filepath.Join(workspace, "key")
	mustMkdir(t, repo)
	runJSON(t, binary, "setup", "--repo", repo, "--namespace", "org/example/record", "--actor", "Recorder", "--key-file", key, "--json")
	input := writeRecordInput(t, repo, "operation", "passed")
	args := []string{"record", "--repo", repo, "--profile", "development/v1", "--input", input, "--key-file", key, "--json"}
	first := runJSON(t, binary, args...)
	for field, want := range map[string]any{"admission": "created", "schema_validation": "valid", "integrity": "valid", "authenticity": "valid", "authorization": "authorized", "index_state": "current"} {
		if first[field] != want {
			t.Fatalf("%s = %v, want %v", field, first[field], want)
		}
	}
	for _, field := range []string{"object_id", "event_refs", "source_fingerprint", "next_action", "evidence", "evidence_state", "conflicting_object_ids"} {
		if _, exists := first[field]; !exists {
			t.Fatalf("missing field %s", field)
		}
	}
	// Advance canonical history, then lose both derived state and mutable evidence.
	commitEvidenceEvent(t, binary, repo, key, []map[string]any{})
	os.Remove(filepath.Join(repo, "proof.txt"))
	os.RemoveAll(filepath.Join(repo, ".pact", "index"))
	os.RemoveAll(filepath.Join(repo, ".pact", "tmp"))
	retry := runJSON(t, binary, args...)
	if retry["admission"] != "existing" || retry["object_id"] != first["object_id"] || !reflect.DeepEqual(retry["event_refs"], first["event_refs"]) {
		t.Fatalf("retry = %#v", retry)
	}
	if retry["evidence"].([]any)[0].(map[string]any)["status"] != "unavailable" {
		t.Fatalf("retry evidence = %#v", retry)
	}
	writeRecordInput(t, repo, "operation", "unknown")
	conflict := runErrorJSON(t, 2, binary, args...)["details"].(map[string]any)
	if conflict["admission"] != "conflict" {
		t.Fatalf("conflict = %#v", conflict)
	}
	// An index obstruction after new canonical admission must preserve identity.
	writeRecordInput(t, repo, "pending", "passed")
	os.RemoveAll(filepath.Join(repo, ".pact", "index"))
	if err := os.WriteFile(filepath.Join(repo, ".pact", "index"), []byte("obstruction"), 0o600); err != nil {
		t.Fatal(err)
	}
	pending := runErrorJSON(t, 10, binary, args...)["details"].(map[string]any)
	if pending["admission"] != "created" || pending["object_id"] == "" || pending["index_state"] != "pending" {
		t.Fatalf("pending = %#v", pending)
	}
	os.Remove(filepath.Join(repo, ".pact", "index"))
	recovered := runJSON(t, binary, args...)
	if recovered["admission"] != "existing" || recovered["object_id"] != pending["object_id"] {
		t.Fatalf("recovered = %#v", recovered)
	}
}

func TestCLIRecordConcurrentContenders(t *testing.T) {
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, projectRoot(t), binary)
	for _, differ := range []bool{false, true} {
		t.Run(map[bool]string{false: "identical", true: "different"}[differ], func(t *testing.T) {
			repo, key := t.TempDir(), filepath.Join(t.TempDir(), "key")
			runJSON(t, binary, "setup", "--repo", repo, "--namespace", "org/example/record", "--actor", "Recorder", "--key-file", key, "--json")
			input := writeRecordInput(t, repo, "race", "passed")
			other := filepath.Join(t.TempDir(), "input.json")
			raw, _ := os.ReadFile(input)
			if differ {
				raw = []byte(strings.Replace(string(raw), `"name":"check"`, `"name":"other"`, 1))
			}
			if err := os.WriteFile(other, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			commands := []*exec.Cmd{exec.Command(binary, "record", "--repo", repo, "--profile", "development/v1", "--input", input, "--key-file", key, "--json"), exec.Command(binary, "record", "--repo", repo, "--profile", "development/v1", "--input", other, "--key-file", key, "--json")}
			outputs, diagnostics := make([]bytes.Buffer, 2), make([]bytes.Buffer, 2)
			for i, command := range commands {
				command.Stdout, command.Stderr = &outputs[i], &diagnostics[i]
				if err := command.Start(); err != nil {
					t.Fatal(err)
				}
			}
			failures := 0
			ids := []string{}
			for i, command := range commands {
				if err := command.Wait(); err != nil {
					failures++
					var failure map[string]any
					if err := json.Unmarshal(diagnostics[i].Bytes(), &failure); err != nil {
						t.Fatal(err)
					}
					if failure["exit_code"] != float64(2) || failure["details"].(map[string]any)["admission"] != "conflict" {
						t.Fatal(failure)
					}
				} else {
					var result map[string]any
					if err := json.Unmarshal(outputs[i].Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					ids = append(ids, result["object_id"].(string))
				}
			}
			if !differ && (len(ids) != 2 || ids[0] != ids[1]) {
				t.Fatalf("different retry identities: %v", ids)
			}
			if failures != map[bool]int{false: 0, true: 1}[differ] {
				t.Fatalf("failures = %d", failures)
			}
			verified := runJSON(t, binary, "verify", "--repo", repo, "--strict", "--json")
			if verified["counts"].(map[string]any)["commits"] != float64(1) {
				t.Fatalf("duplicate admission: %#v", verified)
			}
		})
	}
}

func writeRecordInput(t *testing.T, repo, operation, outcome string) string {
	t.Helper()
	proof := []byte("public synthetic check evidence\n")
	if err := os.WriteFile(filepath.Join(repo, "proof.txt"), proof, 0o600); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"name": "check", "command": []string{"reported-check", "--never-executed"}, "outcome": outcome, "evidence_indexes": []int{0}}
	if outcome == "passed" {
		payload["exit_code"] = 0
	}
	input := map[string]any{"operation_id": operation, "namespace": "org/example/record", "subject": "work", "source": map[string]any{"repository": "fixture", "revision": strings.Repeat("a", 40)}, "events": []any{map[string]any{"local_id": "check", "type": "dev.check.executed", "payload": payload, "evidence": []any{map[string]any{"ref": "proof.txt", "digest": canonical.Digest(proof), "media_type": "text/plain", "role": "primary"}}, "caused_by": []any{}, "supersedes": []any{}}}}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, "record.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLIRecordLostResponseAfterPublication(t *testing.T) {
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, projectRoot(t), binary)
	repo, key := filepath.Join(workspace, "repo"), filepath.Join(workspace, "key")
	mustMkdir(t, repo)
	runJSON(t, binary, "setup", "--repo", repo, "--namespace", "org/example/record", "--actor", "Recorder", "--key-file", key, "--json")
	path := writeRecordInput(t, repo, "lost-response", "passed")
	raw, _ := os.ReadFile(path)
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	template := input["events"].([]any)[0].(map[string]any)
	events := make([]any, 1024)
	for i := range events {
		event := map[string]any{}
		maps.Copy(event, template)
		event["local_id"] = fmt.Sprintf("check-%04d", i)
		events[i] = event
	}
	input["events"] = events
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"record", "--repo", repo, "--profile", "development/v1", "--input", path, "--key-file", key, "--json"}
	recoverRecordLostResponse(t, binary, repo, args, len(events))
}

func recoverRecordLostResponse(t *testing.T, binary, repo string, args []string, eventCount int) map[string]any {
	t.Helper()
	st, err := store.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := st.ObjectFiles()
	if err != nil {
		t.Fatal(err)
	}
	existing := make(map[string]bool, len(before))
	for _, file := range before {
		existing[file.ID] = true
	}
	command := exec.Command(binary, args...)
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	// The real CLI writes more than pipe capacity; withholding reads loses its response.
	var admitted string
	var admittedRefs []any
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		files, err := st.ObjectFiles()
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if existing[file.ID] {
				continue
			}
			shown, err := ledger.Show(st, file.ID)
			if err == nil && shown.Integrity == "valid" && shown.Authenticity == "valid" {
				admitted = file.ID
				for _, raw := range shown.Object["body"].(map[string]any)["events"].([]any) {
					event := raw.(map[string]any)
					admittedRefs = append(admittedRefs, ledger.EventRef(admitted, event["local_id"].(string)))
				}
				break
			}
		}
		if admitted != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if admitted == "" {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("no verified canonical publication: %s", diagnostic.Bytes())
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("owned process was not terminated")
	}
	for _, name := range []string{"index", "tmp"} {
		if err := os.RemoveAll(filepath.Join(repo, ".pact", name)); err != nil {
			t.Fatal(err)
		}
	}
	recovered := runJSON(t, binary, args...)
	if recovered["admission"] != "existing" || recovered["object_id"] != admitted || recovered["index_state"] != "current" || len(admittedRefs) != eventCount || !reflect.DeepEqual(recovered["event_refs"], admittedRefs) {
		t.Fatalf("recovered = %#v", recovered)
	}
	files, err := st.ObjectFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(before)+1 {
		t.Fatalf("canonical count %d", len(files))
	}
	return recovered
}

func TestCLIRecordGitEvidenceSurvivesSourceAdvance(t *testing.T) {
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, projectRoot(t), binary)
	repo, key := filepath.Join(workspace, "repo"), filepath.Join(workspace, "key")
	mustMkdir(t, repo)
	source, err := os.ReadFile(filepath.Join(projectRoot(t), "tests/fixtures/development/source.fast-import"))
	if err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, repo, nil, "init", "--quiet", "--object-format=sha1")
	runFixtureGit(t, repo, source, "fast-import", "--quiet")
	initial := strings.TrimSpace(string(runFixtureGit(t, repo, nil, "rev-parse", "main^")))
	latest := strings.TrimSpace(string(runFixtureGit(t, repo, nil, "rev-parse", "main")))
	runFixtureGit(t, repo, nil, "update-ref", "refs/heads/main", initial)
	runFixtureGit(t, repo, nil, "symbolic-ref", "HEAD", "refs/heads/main")
	runJSON(t, binary, "setup", "--repo", repo, "--namespace", "org/example/record", "--actor", "Recorder", "--key-file", key, "--json")
	path := writeRecordInput(t, repo, "git-proof", "passed")
	raw, _ := os.ReadFile(path)
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	input["source"].(map[string]any)["revision"] = initial
	proof := runFixtureGit(t, repo, nil, "show", initial+":proof.txt")
	item := input["events"].([]any)[0].(map[string]any)["evidence"].([]any)[0].(map[string]any)
	item["ref"], item["digest"] = "git:"+initial+":proof.txt", canonical.Digest(proof)
	raw, err = json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"record", "--repo", repo, "--profile", "development/v1", "--input", path, "--key-file", key, "--json"}
	first := runJSON(t, binary, args...)
	before := snapshotSetupFiles(t, repo, key)
	runFixtureGit(t, repo, nil, "update-ref", "refs/heads/main", latest)
	retry := runJSON(t, binary, args...)
	if retry["object_id"] != first["object_id"] || retry["admission"] != "existing" || retry["evidence_state"] != "matching" {
		t.Fatal(retry)
	}
	after := snapshotSetupFiles(t, repo, key)
	for name, value := range before {
		if strings.Contains(name, "/objects/") || strings.Contains(name, "trust") || name == key {
			if !bytes.Equal(after[name], value) {
				t.Fatalf("protected bytes changed: %s", name)
			}
		}
	}
}
