// ABOUTME: Exercises the built pact binary through its complete MVP operator lifecycle.
// ABOUTME: Uses real files, keys, signatures, immutable objects, and store corruption.
package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
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

func TestREADMEInstallCommandPlacesPactAtDocumentedDestination(t *testing.T) {
	root := projectRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	commandText := `env -u GOROOT mise exec -- env GOBIN="$HOME/.local/bin" go install ./cmd/pact`
	if !bytes.Contains(readme, []byte(commandText)) || !bytes.Contains(readme, []byte(`$HOME/.local/bin/pact`)) {
		t.Fatalf("README does not document the exact install command and destination")
	}
	installDir := t.TempDir()
	install := exec.Command("env", "-u", "GOROOT", "mise", "exec", "--", "env", "GOBIN="+installDir, "go", "install", "./cmd/pact")
	install.Dir = root
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("temporary GOBIN install: %v\n%s", err, output)
	}
	installed := filepath.Join(installDir, "pact")
	info, err := os.Stat(installed)
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("installed binary %s = %#v, %v", installed, info, err)
	}
}

func TestCLIReviewSecurityAndResolvedPathContract(t *testing.T) {
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)
	repo := filepath.Join(workspace, "project")
	mustMkdir(t, repo)
	repoLink := filepath.Join(workspace, "project-link")
	if err := os.Symlink(repo, repoLink); err != nil {
		t.Fatal(err)
	}
	runJSON(t, binary, "init", "--repo", repoLink, "--namespace", "org/example/widget", "--json")

	keyDirectory := filepath.Join(workspace, "keys")
	mustMkdir(t, keyDirectory)
	keyDirectoryLink := filepath.Join(workspace, "key-link")
	if err := os.Symlink(keyDirectory, keyDirectoryLink); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(keyDirectory, "operator.key.json")
	key := runJSON(t, binary, "keygen", "--actor", "Alice", "--out", filepath.Join(keyDirectoryLink, "operator.key.json"), "--json")
	keyPath = resolvedPath(t, keyPath)
	if key["path"] != keyPath {
		t.Fatalf("keygen path = %#v, want resolved %q", key["path"], keyPath)
	}

	evidence := filepath.Join(workspace, "evidence.txt")
	if err := os.WriteFile(evidence, []byte("evidence"), 0o644); err != nil {
		t.Fatal(err)
	}
	evidenceLink := filepath.Join(workspace, "evidence-link.txt")
	if err := os.Symlink(evidence, evidenceLink); err != nil {
		t.Fatal(err)
	}
	hashed := runJSON(t, binary, "hash", evidenceLink, "--json")
	resolvedEvidence := resolvedPath(t, evidence)
	if hashed["path"] != resolvedEvidence {
		t.Fatalf("hash path = %#v, want resolved %q", hashed["path"], resolvedEvidence)
	}
	heads := runJSON(t, binary, "heads", "--repo", repoLink, "--json")
	resolvedRepo := resolvedPath(t, repo)
	if heads["repo"] != resolvedRepo {
		t.Fatalf("heads repo = %#v, want resolved %q", heads["repo"], resolvedRepo)
	}

	publicPath := filepath.Join(workspace, "operator.public.json")
	writePublicKeyFile(t, keyPath, publicPath, 0o644)
	runJSON(t, binary, "trust-add", "--repo", repo, "--key-file", publicPath, "--json")
	batchPath := writeBatch(t, workspace, "security.json", "security", "widget.security")
	keyRaw, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(repo, "inside.key.json")
	if err := os.WriteFile(inside, keyRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	insideLinkOutside := filepath.Join(workspace, "inside-link.key.json")
	if err := os.Symlink(inside, insideLinkOutside); err != nil {
		t.Fatal(err)
	}
	outsideLinkInside := filepath.Join(repo, "outside-link.key.json")
	if err := os.Symlink(keyPath, outsideLinkInside); err != nil {
		t.Fatal(err)
	}
	tooOpen := filepath.Join(workspace, "too-open.key.json")
	if err := os.WriteFile(tooOpen, keyRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, signingPath := range map[string]string{
		"direct project path":        inside,
		"resolved project target":    insideLinkOutside,
		"lexical project path":       outsideLinkInside,
		"group-readable private key": tooOpen,
	} {
		t.Run(name, func(t *testing.T) {
			failed := runErrorJSON(t, 7, binary, "commit", "--repo", repo, "--key-file", signingPath, "--events", batchPath, "--json")
			if failed["exit_code"] != float64(7) || strings.Contains(fmt.Sprint(failed), string(keyRaw)) {
				t.Fatalf("signing-key refusal = %#v", failed)
			}
		})
	}

	command := exec.Command(binary, "heads", "--repo", repo, "--invalid-flag", "--json")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err = command.Run()
	exitError := &exec.ExitError{}
	ok := errors.As(err, &exitError)
	if !ok || exitError.ExitCode() != 2 || stdout.Len() != 0 {
		t.Fatalf("invalid flag = %v, stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	decoder := json.NewDecoder(&stderr)
	var diagnostic map[string]any
	if err := decoder.Decode(&diagnostic); err != nil {
		t.Fatalf("decode invalid-flag JSON %q: %v", stderr.String(), err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || strings.Contains(stderr.String(), "Usage of") {
		t.Fatalf("invalid-flag output includes prose or extra JSON: %q", stderr.String())
	}
}

func TestCLIConcurrentTrustAddPreservesDistinctAndIdenticalRoots(t *testing.T) {
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)
	repo := filepath.Join(workspace, "project")
	mustMkdir(t, repo)
	runJSON(t, binary, "init", "--repo", repo, "--namespace", "org/example/widget", "--json")

	distinct := make([]string, 16)
	for index := range distinct {
		distinct[index] = filepath.Join(workspace, fmt.Sprintf("distinct-%02d.key.json", index))
		runJSON(t, binary, "keygen", "--actor", fmt.Sprintf("Actor %02d", index), "--out", distinct[index], "--json")
	}
	results := runConcurrentTrustAdds(t, binary, repo, distinct)
	for index, result := range results {
		if result["created"] != true {
			t.Fatalf("distinct trust-add %d = %#v", index, result)
		}
	}
	trustRaw, err := os.ReadFile(filepath.Join(repo, ".pact", "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	var trust map[string]any
	if err := json.Unmarshal(trustRaw, &trust); err != nil {
		t.Fatal(err)
	}
	if roots := trust["roots"].([]any); len(roots) != len(distinct) {
		t.Fatalf("trusted distinct roots = %d, want %d", len(roots), len(distinct))
	}

	identical := filepath.Join(workspace, "identical.key.json")
	runJSON(t, binary, "keygen", "--actor", "Identical", "--out", identical, "--json")
	identicalPaths := make([]string, 12)
	for index := range identicalPaths {
		identicalPaths[index] = identical
	}
	results = runConcurrentTrustAdds(t, binary, repo, identicalPaths)
	created := 0
	for _, result := range results {
		if result["created"] == true {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("identical concurrent created count = %d, want 1; results=%#v", created, results)
	}
}

func TestCLIVerifyMalformedTrustPreservesFullStrictDetails(t *testing.T) {
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)
	repo := filepath.Join(workspace, "project")
	mustMkdir(t, repo)
	runJSON(t, binary, "init", "--repo", repo, "--namespace", "org/example/widget", "--json")
	keyPath := filepath.Join(workspace, "operator.key.json")
	runJSON(t, binary, "keygen", "--actor", "Operator", "--out", keyPath, "--json")
	batchPath := writeBatch(t, workspace, "verify.json", "verify", "widget.verify")
	commit := runJSON(t, binary, "commit", "--repo", repo, "--key-file", keyPath, "--events", batchPath, "--json")
	commitID := commit["object_id"].(string)
	if err := os.WriteFile(filepath.Join(repo, ".pact", "trust.json"), []byte(`{"format":"wrong","roots":[]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	failure := runErrorJSON(t, 4, binary, "verify", "--repo", repo, "--strict", "--json")
	details, ok := failure["details"].(map[string]any)
	if !ok {
		t.Fatalf("verify failure = %#v, want details", failure)
	}
	counts := details["counts"].(map[string]any)
	if counts["objects"] != float64(1) || counts["commits"] != float64(1) || counts["events"] != float64(1) {
		t.Fatalf("verify counts = %#v", counts)
	}
	objects := details["objects"].(map[string]any)
	if object := objects[commitID].(map[string]any); object["integrity"] != "valid" || object["authenticity"] != "valid" {
		t.Fatalf("verified object = %#v", object)
	}
	heads := details["heads"].(map[string]any)["org/example/widget"].([]any)
	if len(heads) != 1 || heads[0] != commitID {
		t.Fatalf("verify heads = %#v", details["heads"])
	}
	errorsValue := details["errors"].([]any)
	errorsText := make([]string, len(errorsValue))
	for index, value := range errorsValue {
		errorsText[index] = value.(string)
	}
	if !sort.StringsAreSorted(errorsText) || len(errorsText) != 1 || errorsText[0] != "authority evaluation failed: ledger store failure: malformed local trust file" {
		t.Fatalf("verify errors = %#v", errorsText)
	}
	for _, layer := range []string{"integrity", "structure", "authenticity", "dag", "references"} {
		if _, ok := details[layer].(map[string]any); !ok {
			t.Fatalf("verify %s layer missing from %#v", layer, details)
		}
	}
}

func buildBinary(t *testing.T, root, binary string) {
	t.Helper()
	build := exec.Command("go", "build", "-o", binary, "./cmd/pact")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build pact: %v\n%s", err, output)
	}
}

func writePublicKeyFile(t *testing.T, privatePath, publicPath string, mode fs.FileMode) {
	t.Helper()
	raw, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]any
	if err := json.Unmarshal(raw, &key); err != nil {
		t.Fatal(err)
	}
	delete(key, "private_key")
	raw, err = json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, raw, mode); err != nil {
		t.Fatal(err)
	}
}

func runConcurrentTrustAdds(t *testing.T, binary, repo string, paths []string) []map[string]any {
	t.Helper()
	start := make(chan struct{})
	results := make([]map[string]any, len(paths))
	errors := make([]error, len(paths))
	var group sync.WaitGroup
	for index, path := range paths {
		group.Go(func() {
			<-start
			command := exec.Command(binary, "trust-add", "--repo", repo, "--key-file", path, "--json")
			output, err := command.CombinedOutput()
			if err != nil {
				errors[index] = fmt.Errorf("trust-add %s: %w: %s", path, err, output)
				return
			}
			var result map[string]any
			if err := json.Unmarshal(output, &result); err != nil {
				errors[index] = fmt.Errorf("decode trust-add output %q: %w", output, err)
				return
			}
			results[index] = result
		})
	}
	close(start)
	group.Wait()
	for _, err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	return results
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
	exitError := &exec.ExitError{}
	ok := errors.As(err, &exitError)
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

func resolvedPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
