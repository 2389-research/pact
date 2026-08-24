// ABOUTME: Exercises the built pact binary through its complete MVP operator lifecycle.
// ABOUTME: Uses real files, keys, signatures, immutable objects, and store corruption.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLIProductLifecycle(t *testing.T) {
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	build := exec.Command("go", "build", "-o", binary, "./cmd/pact")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build pact: %v\n%s", err, output)
	}

	repo := filepath.Join(workspace, "project")
	keyPath := filepath.Join(workspace, "operator.key.json")
	mustMkdir(t, repo)
	runJSON(t, binary, "init", "--repo", repo, "--namespace", "org/example/widget", "--json")
	key := runJSON(t, binary, "keygen", "--actor", "Alice", "--out", keyPath, "--json")
	trusted := runJSON(t, binary, "trust-add", "--repo", repo, "--key-file", keyPath, "--json")
	if trusted["key_id"] != key["key_id"] {
		t.Fatalf("trusted root = %#v, key = %#v", trusted, key)
	}

	firstBatch := writeBatch(t, workspace, "first.json", "e1", "widget.first")
	first := runJSON(t, binary, "commit", "--repo", repo, "--key-file", keyPath, "--events", firstBatch, "--json")
	secondBatch := writeBatch(t, workspace, "second.json", "e2", "widget.second")
	second := runJSON(t, binary, "commit", "--repo", repo, "--key-file", keyPath, "--events", secondBatch, "--json")

	heads := runJSON(t, binary, "heads", "--repo", repo, "--namespace", "org/example", "--json")
	namespaceHeads := heads["heads"].(map[string]any)["org/example/widget"].([]any)
	if len(namespaceHeads) != 1 || namespaceHeads[0] != second["object_id"] {
		t.Fatalf("heads = %#v", heads)
	}
	eventRef := first["event_refs"].([]any)[0].(string)
	shownEvent := runJSON(t, binary, "show", "--repo", repo, eventRef, "--json")
	if shownEvent["kind"] != "event" || shownEvent["event"].(map[string]any)["local_id"] != "e1" {
		t.Fatalf("shown event = %#v", shownEvent)
	}
	verified := runJSON(t, binary, "verify", "--repo", repo, "--strict", "--json")
	if verified["ok"] != true || verified["counts"].(map[string]any)["commits"] != float64(2) {
		t.Fatalf("strict verify = %#v", verified)
	}

	checkpoint := runJSON(t, binary, "checkpoint", "--repo", repo, "--key-file", keyPath, "--scope", "org/example", "--policy-ref", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--authority-epoch", "epoch-1", "--schema-ref", "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "--schema-ref", "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "--purpose", "release cut", "--json")
	if checkpoint["authorization"] != "authorized" || checkpoint["integrity"] != "valid" {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}
	shownCheckpoint := runJSON(t, binary, "show", "--repo", repo, checkpoint["object_id"].(string), "--json")
	if shownCheckpoint["kind"] != "checkpoint" || shownCheckpoint["object"].(map[string]any)["format"] != "pact/checkpoint/v1" {
		t.Fatalf("shown checkpoint = %#v", shownCheckpoint)
	}
	verified = runJSON(t, binary, "verify", "--repo", repo, "--strict", "--json")
	if verified["counts"].(map[string]any)["checkpoints"] != float64(1) {
		t.Fatalf("checkpoint verify = %#v", verified)
	}
	assertNoPrivateKey(t, repo)

	corruptRepo := filepath.Join(workspace, "corrupt-copy")
	copyTree(t, repo, corruptRepo)
	objectPaths, err := filepath.Glob(filepath.Join(corruptRepo, ".pact", "objects", "sha256", "*", "*.json"))
	if err != nil || len(objectPaths) == 0 {
		t.Fatalf("copied object paths = %#v, %v", objectPaths, err)
	}
	if err := os.WriteFile(objectPaths[0], []byte(`{"corrupt":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	failed := runErrorJSON(t, 4, binary, "verify", "--repo", corruptRepo, "--strict", "--json")
	if failed["details"] == nil || !strings.Contains(failed["error"].(string), "verification failed") {
		t.Fatalf("corrupt verify = %#v", failed)
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate e2e source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func writeBatch(t *testing.T, directory, name, localID, eventType string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	raw := fmt.Sprintf(`{"events":[{"local_id":%q,"kind":"observation","type":%q,"subject":"widget-1","schema_ref":"pact:core/widget/v1","payload":{},"evidence":[],"caused_by":[],"supersedes":[],"tags":[]}]}`, localID, eventType)
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runJSON(t *testing.T, binary string, args ...string) map[string]any {
	t.Helper()
	command := exec.Command(binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("%s %s: %v\nstdout: %s\nstderr: %s", binary, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return decodeJSON(t, stdout.Bytes())
}

func runErrorJSON(t *testing.T, exitCode int, binary string, args ...string) map[string]any {
	t.Helper()
	command := exec.Command(binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exitError, ok := err.(*exec.ExitError)
	if !ok || exitError.ExitCode() != exitCode {
		t.Fatalf("%s %s exit = %v, want %d\nstdout: %s\nstderr: %s", binary, strings.Join(args, " "), err, exitCode, stdout.String(), stderr.String())
	}
	return decodeJSON(t, stderr.Bytes())
}

func decodeJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode JSON %q: %v", raw, err)
	}
	return result
}

func assertNoPrivateKey(t *testing.T, repo string) {
	t.Helper()
	if err := filepath.WalkDir(repo, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(raw, []byte(`"private_key"`)) {
			return fmt.Errorf("private key field found under project: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
