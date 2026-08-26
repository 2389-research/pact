// ABOUTME: Defines the compiled-binary contract shared by every operator CLI foundation candidate.
// ABOUTME: Uses real stores, keys, indexes, process exits, discovery, JSON, and terminal modes.
//
//nolint:unused // Candidate wrappers invoke this dormant shared contract in their own worktrees.
package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runOperatorCLIContract(t *testing.T) {
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)

	bareHelp := runOperatorProcess(t, binary, workspace, nil)
	assertCleanHelp(t, bareHelp)
	if !strings.Contains(bareHelp.Stdout, "signed & sealed") || strings.Contains(bareHelp.Stdout, "\x1b[") {
		t.Fatalf("bare help branding = %#v", bareHelp)
	}

	for _, args := range [][]string{{"--help"}, {"help"}, {"status", "--help"}} {
		explicitHelp := runOperatorProcess(t, binary, workspace, nil, args...)
		assertCleanHelp(t, explicitHelp)
		if strings.Contains(explicitHelp.Stdout, "signed & sealed") || strings.Contains(explicitHelp.Stdout, "\x1b[") {
			t.Fatalf("explicit help branding for %q = %#v", args, explicitHelp)
		}
	}

	typo := runOperatorProcess(t, binary, workspace, nil, "statsu")
	if typo.Code != 2 || typo.Stdout != "" || !strings.Contains(typo.Stderr, "status") {
		t.Fatalf("typo result = %#v", typo)
	}

	repo := filepath.Join(workspace, "project")
	keyPath := filepath.Join(workspace, "operator.key.json")
	mustMkdir(t, repo)
	runJSON(t, binary, "init", "--repo", repo, "--namespace", "org/example/widget", "--json")
	runJSON(t, binary, "keygen", "--actor", "Alice", "--out", keyPath, "--json")
	runJSON(t, binary, "trust-add", "--repo", repo, "--key-file", keyPath, "--json")
	runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")

	nested := filepath.Join(repo, "one", "two")
	mustMkdir(t, nested)
	healthy := runOperatorProcess(t, binary, nested, nil, "status", "--json")
	if healthy.Code != 0 || healthy.Stderr != "" {
		t.Fatalf("healthy status = %#v", healthy)
	}
	assertHealthyStatusJSON(t, healthy.Stdout, repo)

	plain := runOperatorProcess(t, binary, nested, nil, "status", "--color", "never")
	assertPlainHealthyStatus(t, plain, "org/example/widget")
	colored := runOperatorProcess(t, binary, nested, nil, "status", "--color", "always")
	if colored.Code != 0 || !strings.Contains(colored.Stdout, "\x1b[") || !strings.Contains(colored.Stdout, "Healthy") {
		t.Fatalf("forced-color status = %#v", colored)
	}
	dumb := runOperatorProcess(t, binary, nested, []string{"TERM=dumb"}, "status")
	if dumb.Code != 0 || strings.Contains(dumb.Stdout, "\x1b[") {
		t.Fatalf("TERM=dumb status = %#v", dumb)
	}

	notRepo := filepath.Join(nested, "explicit-empty")
	mustMkdir(t, notRepo)
	explicit := runOperatorProcess(t, binary, nested, nil, "status", "--repo", notRepo, "--json")
	if explicit.Code != 3 || explicit.Stdout != "" || !strings.Contains(explicit.Stderr, "pact setup") {
		t.Fatalf("authoritative --repo = %#v", explicit)
	}

	batch := writeBatch(t, workspace, "stale.json", "stale", "widget.stale")
	runJSON(t, binary, "commit", "--repo", repo, "--key-file", keyPath, "--events", batch, "--json")
	stale := runOperatorProcess(t, binary, nested, nil, "status", "--json")
	if stale.Code != 9 || stale.Stdout != "" {
		t.Fatalf("stale status = %#v", stale)
	}
	assertAttentionStatusJSON(t, stale.Stderr, repo, "stale", "pact index rebuild")

	corruptRepo := filepath.Join(workspace, "corrupt")
	copyTree(t, repo, corruptRepo)
	objects, err := filepath.Glob(filepath.Join(corruptRepo, ".pact", "objects", "sha256", "*", "*.json"))
	if err != nil || len(objects) == 0 {
		t.Fatalf("corrupt fixture objects = %v, %v", objects, err)
	}
	if err := os.WriteFile(objects[0], []byte(`{"corrupt":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := runOperatorProcess(t, binary, workspace, nil, "status", "--repo", corruptRepo, "--json")
	if broken.Code != 4 || broken.Stdout != "" {
		t.Fatalf("broken status = %#v", broken)
	}
	assertBrokenStatusJSON(t, broken.Stderr, corruptRepo)

	heads := runOperatorProcess(t, binary, nested, nil, "heads", "--json")
	if heads.Code != 0 || heads.Stderr != "" {
		t.Fatalf("discovered heads = %#v", heads)
	}
	var headsJSON map[string]any
	if err := json.Unmarshal([]byte(heads.Stdout), &headsJSON); err != nil {
		t.Fatal(err)
	}
	if headsJSON["operation"] != "heads" || headsJSON["repo"] != resolvedPath(t, repo) || headsJSON["note"] == nil {
		t.Fatalf("heads JSON compatibility = %#v", headsJSON)
	}
}

type operatorProcessResult struct {
	Code           int
	Stdout, Stderr string
}

func runOperatorProcess(t *testing.T, binary, directory string, overrides []string, args ...string) operatorProcessResult {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = directory
	command.Env = mergedOperatorEnvironment(overrides)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	code := 0
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			t.Fatalf("run pact %q: %v", args, err)
		}
		code = exitError.ExitCode()
	}
	return operatorProcessResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
}

func mergedOperatorEnvironment(overrides []string) []string {
	replaced := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		key, _, _ := strings.Cut(entry, "=")
		replaced[key] = struct{}{}
	}
	result := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, found := replaced[key]; !found {
			result = append(result, entry)
		}
	}
	return append(result, overrides...)
}

func assertCleanHelp(t *testing.T, result operatorProcessResult) {
	t.Helper()
	if result.Code != 0 || result.Stderr != "" {
		t.Fatalf("help result = %#v", result)
	}
	for _, text := range []string{"Usage:", "Commands:", "status"} {
		if !strings.Contains(result.Stdout, text) {
			t.Fatalf("help lacks %q: %q", text, result.Stdout)
		}
	}
	if strings.Contains(result.Stdout, "Usage of") {
		t.Fatalf("help leaked Go flag usage: %q", result.Stdout)
	}
}

func decodeSingleOperatorJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode JSON %q: %v", raw, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("JSON has trailing value: %q", raw)
	}
	return result
}

func assertHealthyStatusJSON(t *testing.T, raw, repo string) {
	t.Helper()
	result := decodeSingleOperatorJSON(t, raw)
	verification, ok := result["verification"].(map[string]any)
	if !ok {
		t.Fatalf("verification = %#v", result["verification"])
	}
	indexValue, ok := result["index"].(map[string]any)
	if !ok {
		t.Fatalf("index = %#v", result["index"])
	}
	if result["operation"] != "status" || result["ok"] != true || result["health"] != "healthy" ||
		result["repo"] != resolvedPath(t, repo) || result["next_action"] != nil ||
		verification["strict"] != true || verification["ok"] != true || indexValue["state"] != "current" {
		t.Fatalf("healthy status JSON = %#v", result)
	}
}

func assertPlainHealthyStatus(t *testing.T, result operatorProcessResult, namespace string) {
	t.Helper()
	if result.Code != 0 || result.Stderr != "" || strings.Contains(result.Stdout, "\x1b[") {
		t.Fatalf("plain status = %#v", result)
	}
	for _, text := range []string{"Healthy", "Strict verification", "locally closed", "Global completeness", "current", namespace} {
		if !strings.Contains(result.Stdout, text) {
			t.Fatalf("plain status lacks %q: %q", text, result.Stdout)
		}
	}
}

func assertAttentionStatusJSON(t *testing.T, raw, repo, indexState, command string) {
	t.Helper()
	envelope := decodeSingleOperatorJSON(t, raw)
	details, ok := envelope["details"].(map[string]any)
	if !ok {
		t.Fatalf("attention details = %#v", envelope)
	}
	indexValue, ok := details["index"].(map[string]any)
	if !ok {
		t.Fatalf("attention index = %#v", details["index"])
	}
	action, ok := details["next_action"].(map[string]any)
	if !ok {
		t.Fatalf("attention action = %#v", details["next_action"])
	}
	if envelope["ok"] != false || envelope["exit_code"] != float64(9) ||
		details["health"] != "attention" || details["repo"] != resolvedPath(t, repo) ||
		indexValue["state"] != indexState || action["command"] != command {
		t.Fatalf("attention status JSON = %#v", envelope)
	}
}

func assertBrokenStatusJSON(t *testing.T, raw, repo string) {
	t.Helper()
	envelope := decodeSingleOperatorJSON(t, raw)
	details, ok := envelope["details"].(map[string]any)
	if !ok {
		t.Fatalf("broken details = %#v", envelope)
	}
	if envelope["ok"] != false || envelope["exit_code"] != float64(4) ||
		details["health"] != "broken" || details["repo"] != resolvedPath(t, repo) ||
		details["index"] != nil || details["next_action"] != nil {
		t.Fatalf("broken status JSON = %#v", envelope)
	}
}
