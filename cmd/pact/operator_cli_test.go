// ABOUTME: Exercises operator CLI dispatch, repository resolution, terminal modes, and failed writers.
// ABOUTME: Uses real stores and indexes so adapter tests preserve domain behavior.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	statuspkg "pact/internal/status"
)

type closedOutput struct{}

func (closedOutput) Write([]byte) (int, error) { return 0, errors.New("closed output") }

func TestOperatorWriterFailuresReturnUnexpectedExit(t *testing.T) {
	repo := healthyOperatorRepository(t)
	queryFixture := newCLIQueryFixture(t)
	tests := []struct {
		name   string
		args   []string
		stdout io.Writer
		stderr io.Writer
	}{
		{name: "successful status stdout", args: []string{"status", "--repo", repo}, stdout: closedOutput{}, stderr: io.Discard},
		{name: "unknown command stderr", args: []string{"unknown"}, stdout: io.Discard, stderr: closedOutput{}},
		{name: "JSON encoding", args: []string{"init", "--repo", t.TempDir(), "--namespace", "org/example/widget", "--json"}, stdout: closedOutput{}, stderr: io.Discard},
		{name: "human index status", args: []string{"index", "status", "--repo", repo}, stdout: closedOutput{}, stderr: io.Discard},
		{name: "human log", args: []string{"log", "--repo", queryFixture.repo}, stdout: closedOutput{}, stderr: io.Discard},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if code := run(test.args, test.stdout, test.stderr); code != exitUnexpectedError {
				t.Fatalf("run(%q) = %d, want %d", test.args, code, exitUnexpectedError)
			}
		})
	}
}

func TestOperatorSuggestionsAreDeterministic(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"statsu"}, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "status") {
		t.Fatalf("statsu = (%d, %q)", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"wat"}, &stdout, &stderr); code != exitUsage || strings.Contains(stderr.String(), "did you mean") {
		t.Fatalf("wat = (%d, %q)", code, stderr.String())
	}
}

func TestRepositoryDiscoveryStopsAtUnsafeStoreEntry(t *testing.T) {
	parent := t.TempDir()
	runJSON(t, []string{"init", "--repo", parent, "--namespace", "org/example/widget", "--json"})
	nested := filepath.Join(parent, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".pact"), []byte("unsafe"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{"status", "--json"}, runConfig{Stdout: &stdout, Stderr: &stderr, WorkingDir: nested, Width: 80})
	if code != exitStore {
		t.Fatalf("unsafe discovery exit = %d, stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 || strings.Contains(stderr.String(), "pact setup") || !strings.Contains(stderr.String(), filepath.Join(nested, ".pact")) {
		t.Fatalf("unsafe discovery diagnostic = stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRepositoryDiscoveryInjectsRepoBeforeShowIdentifier(t *testing.T) {
	fixture := newCLIQueryFixture(t)
	nested := filepath.Join(fixture.repo, "one", "two")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{"show", fixture.eventRef, "--json"}, runConfig{Stdout: &stdout, Stderr: &stderr, WorkingDir: nested, Width: 80})
	if code != 0 {
		t.Fatalf("discovered show exit = %d, stderr=%q", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["identifier"] != fixture.eventRef {
		t.Fatalf("discovered show = %#v", result)
	}
}

func TestRepositoryDiscoveryResolvesSymlinkedWorkingDirectory(t *testing.T) {
	repo := healthyOperatorRepository(t)
	nested := filepath.Join(repo, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	workingDirectory := filepath.Join(t.TempDir(), "linked-working-directory")
	if err := os.Symlink(nested, workingDirectory); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runWithConfig([]string{"status", "--json"}, runConfig{Stdout: &stdout, Stderr: &stderr, WorkingDir: workingDirectory, Width: 80}); code != 0 {
		t.Fatalf("symlinked discovery exit = %d, stderr=%q", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result["repo"] != resolvedRepo {
		t.Fatalf("symlinked discovery result = %#v, want repo %q", result, resolvedRepo)
	}
}

func TestGlobalOptionsAndRepositoryResolutionStopAtSentinel(t *testing.T) {
	arguments := []string{"hash", "--", "--color", "always", "--json", "--help"}
	parsed, display, err := parsePresentation(arguments)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(parsed, "\x00") != strings.Join(arguments, "\x00") || display.asJSON || display.colorMode != "auto" {
		t.Fatalf("sentinel presentation = (%q, %#v), want (%q, plain)", parsed, display, arguments)
	}

	repo := healthyOperatorRepository(t)
	normalized, err := normalizeRepositoryArgs([]string{"--", "--repo", "outside"}, repositoryOpen, repo)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--repo", resolvedRepo, "--", "--repo", "outside"}
	if strings.Join(normalized, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("sentinel repository arguments = %q, want %q", normalized, want)
	}
	missingValue, err := normalizeRepositoryArgs([]string{"--repo", "--"}, repositoryOpen, repo)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--repo", "--"}; strings.Join(missingValue, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("sentinel repository value = %q, want %q", missingValue, want)
	}

	directory := t.TempDir()
	t.Chdir(directory)
	for _, name := range []string{"--help", "--color", "--repo"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := runWithConfig([]string{"hash", "--", name}, runConfig{Stdout: &stdout, Stderr: &stderr, WorkingDir: directory, Width: 80}); code != 0 {
			t.Fatalf("hash operand %q exit = %d, stderr=%q", name, code, stderr.String())
		}
		if strings.Contains(stdout.String(), "Usage:") {
			t.Fatalf("hash operand %q rendered help: %q", name, stdout.String())
		}
	}
}

func TestStatusColorPrecedenceAndJSONPlainness(t *testing.T) {
	repo := healthyOperatorRepository(t)
	for _, test := range []struct {
		name      string
		args      []string
		config    runConfig
		wantColor bool
	}{
		{name: "NO_COLOR", args: []string{"status", "--repo", repo}, config: runConfig{StdoutTerminal: true, Environment: map[string]string{"NO_COLOR": "1"}}, wantColor: false},
		{name: "empty NO_COLOR", args: []string{"status", "--repo", repo}, config: runConfig{StdoutTerminal: true, Environment: map[string]string{"NO_COLOR": ""}}, wantColor: false},
		{name: "TERM dumb", args: []string{"status", "--repo", repo}, config: runConfig{StdoutTerminal: true, Environment: map[string]string{"TERM": "dumb"}}, wantColor: false},
		{name: "always", args: []string{"status", "--repo", repo, "--color", "always"}, config: runConfig{Environment: map[string]string{"NO_COLOR": "1"}}, wantColor: true},
		{name: "JSON always plain", args: []string{"status", "--repo", repo, "--color", "always", "--json"}, config: runConfig{}, wantColor: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			test.config.Stdout, test.config.Stderr, test.config.WorkingDir, test.config.Width = &stdout, &stderr, repo, 80
			if code := runWithConfig(test.args, test.config); code != 0 {
				t.Fatalf("status exit = %d, stderr=%q", code, stderr.String())
			}
			if got := strings.Contains(stdout.String(), "\x1b["); got != test.wantColor {
				t.Fatalf("color = %v, want %v; stdout=%q", got, test.wantColor, stdout.String())
			}
		})
	}
}

func TestStatusJSONPresentationErrorUsesOneEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{"status", "--json", "--color", "invalid"}, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80})
	if code != exitUsage || stdout.Len() != 0 {
		t.Fatalf("invalid JSON presentation = (%d, %q, %q)", code, stdout.String(), stderr.String())
	}
	decoder := json.NewDecoder(&stderr)
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode JSON diagnostic %q: %v", stderr.String(), err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("extra JSON diagnostic %q: %v", stderr.String(), err)
	}
	if result["ok"] != false || result["exit_code"] != float64(exitUsage) || !strings.Contains(result["error"].(string), "--color requires") {
		t.Fatalf("JSON presentation diagnostic = %#v", result)
	}
}

func TestLeafFlagErrorsUseOnePACTDiagnostic(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runWithConfig([]string{"heads", "--not-a-flag"}, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80}); code != exitUsage {
		t.Fatalf("leaf parser error exit = %d, stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 || strings.Count(stderr.String(), "PACT error:") != 1 ||
		strings.Contains(stderr.String(), "Usage of") || strings.Contains(stderr.String(), "flag provided") ||
		!strings.Contains(stderr.String(), "invalid heads arguments") {
		t.Fatalf("leaf parser diagnostic = stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCatalogHelpRendersMeaningfulFlagDescriptions(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want []string
	}{
		{name: "top level", args: nil, want: []string{"Global options:", "--json", "machine-readable JSON", "--color auto|always|never", "human color output"}},
		{name: "command", args: []string{"status", "--help"}, want: []string{"Options:", "--repo PATH", "project root", "--color auto|always|never"}},
		{name: "nested command", args: []string{"index", "status", "--help"}, want: []string{"Options:", "--repo PATH", "project root", "--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runWithConfig(test.args, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80}); code != 0 || stderr.Len() != 0 {
				t.Fatalf("help exit = %d, stderr=%q", code, stderr.String())
			}
			for _, fragment := range test.want {
				if !strings.Contains(stdout.String(), fragment) {
					t.Fatalf("help lacks %q: %q", fragment, stdout.String())
				}
			}
		})
	}
}

func TestCommandCatalogListsEveryLeafFlag(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{path: "quickstart", want: []string{"--color", "--json"}},
		{path: "setup", want: []string{"--actor", "--color", "--json", "--key-file", "--namespace", "--repo"}},
		{path: "init", want: []string{"--color", "--json", "--namespace", "--repo"}},
		{path: "keygen", want: []string{"--actor", "--color", "--json", "--out"}},
		{path: "trust-add", want: []string{"--color", "--json", "--key-file", "--repo"}},
		{path: "status", want: []string{"--color", "--json", "--repo"}},
		{path: "heads", want: []string{"--color", "--json", "--namespace", "--repo"}},
		{path: "show", want: []string{"--color", "--json", "--repo"}},
		{path: "verify", want: []string{"--color", "--json", "--repo", "--strict"}},
		{path: "log", want: []string{"--actor", "--color", "--cursor", "--json", "--limit", "--namespace", "--repo"}},
		{path: "query", want: []string{"--actor", "--caused-by", "--color", "--cursor", "--event-ref", "--json", "--kind", "--limit", "--namespace", "--repo", "--schema-ref", "--subject", "--supersedes", "--tag", "--type"}},
		{path: "commit", want: []string{"--color", "--correlation-id", "--delegation-ref", "--epoch", "--events", "--json", "--key-file", "--lease-ref", "--namespace", "--observed-at", "--parent", "--repo"}},
		{path: "checkpoint", want: []string{"--authority-epoch", "--color", "--json", "--key-file", "--policy-ref", "--previous", "--purpose", "--repo", "--schema-ref", "--scope"}},
		{path: "index status", want: []string{"--color", "--json", "--repo"}},
		{path: "index rebuild", want: []string{"--color", "--json", "--repo"}},
		{path: "hash", want: []string{"--color", "--json"}},
	}
	catalog := commandCatalog()
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			var got []string
			for _, spec := range catalog {
				if strings.Join(spec.path, " ") != test.path {
					continue
				}
				for _, flag := range spec.flags {
					name := strings.Fields(flag.usage)[0]
					got = append(got, name)
					if strings.TrimSpace(flag.description) == "" {
						t.Fatalf("catalog flag %q lacks a description", name)
					}
				}
				break
			}
			if got == nil {
				t.Fatalf("catalog lacks %q", test.path)
			}
			sort.Strings(got)
			sort.Strings(test.want)
			if strings.Join(got, ",") != strings.Join(test.want, ",") {
				t.Fatalf("catalog flags = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAttentionStatusUsesAttentionColor(t *testing.T) {
	var output bytes.Buffer
	if err := emitStatusHuman(&output, statuspkg.Result{Health: statuspkg.HealthAttention}, true, 80); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b[32mAttention") || !strings.Contains(output.String(), "\x1b[33mAttention") {
		t.Fatalf("attention color = %q", output.String())
	}
}

func TestFailedStatusAutoColorUsesActualOutputTerminal(t *testing.T) {
	for _, fixture := range []struct {
		name string
		repo func(*testing.T) string
	}{
		{name: "attention", repo: staleOperatorRepository},
		{name: "broken", repo: brokenOperatorRepository},
	} {
		t.Run(fixture.name+"/redirected-stderr", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runWithConfig([]string{"status", "--repo", fixture.repo(t)}, runConfig{
				Stdout: &stdout, Stderr: &stderr, StdoutTerminal: true, StderrTerminal: false, Width: 80,
			})
			if code == 0 || strings.Contains(stderr.String(), "\x1b[") {
				t.Fatalf("redirected failed status = (%d, %q)", code, stderr.String())
			}
		})
		t.Run(fixture.name+"/terminal-stderr", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runWithConfig([]string{"status", "--repo", fixture.repo(t)}, runConfig{
				Stdout: &stdout, Stderr: &stderr, StdoutTerminal: false, StderrTerminal: true, Width: 80,
			})
			if code == 0 || !strings.Contains(stderr.String(), "\x1b[") {
				t.Fatalf("terminal failed status = (%d, %q)", code, stderr.String())
			}
		})
	}
}

func TestStatusAcceptsEqualsRepositoryFlag(t *testing.T) {
	repo := healthyOperatorRepository(t)
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{"status", "--repo=" + repo, "--json"}, runConfig{Stdout: &stdout, Stderr: &stderr, WorkingDir: repo, Width: 80})
	if code != 0 {
		t.Fatalf("status --repo= exit = %d, stderr=%q", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result["repo"] != resolvedRepo {
		t.Fatalf("status --repo= result = %#v", result)
	}
}

func TestInvalidHelpPathUsesUsageDiagnosticAndWriterFailuresStayUnexpected(t *testing.T) {
	var stdout, stderr bytes.Buffer
	config := runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80}
	if code := runWithConfig([]string{"help", "index", "missing"}, config); code != exitUsage {
		t.Fatalf("invalid help exit = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown help path: index missing") {
		t.Fatalf("invalid help diagnostic = %q", stderr.String())
	}
	if code := runWithConfig([]string{"help", "index"}, runConfig{Stdout: closedOutput{}, Stderr: io.Discard, Width: 80}); code != exitUnexpectedError {
		t.Fatalf("help writer failure exit = %d", code)
	}
}

func TestStatusMissingExplicitRepositoryDoesNotCreateIt(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-repository")
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{"status", "--repo", missing, "--json"}, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80})
	if code != exitStore {
		t.Fatalf("missing repository exit = %d, stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing repository was created: %v", err)
	}
}

func TestStatusExistingRepositoryWithoutStoreGetsSetupAction(t *testing.T) {
	repo := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{"status", "--repo", repo, "--json"}, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80})
	if code != exitStore || !strings.Contains(stderr.String(), "pact setup") {
		t.Fatalf("existing repository without store = (%d, %q)", code, stderr.String())
	}
	if _, err := os.Lstat(filepath.Join(repo, ".pact")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing store was created: %v", err)
	}
}

func TestStatusUnsafeStoreUsesValidationDiagnostic(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".pact"), []byte("unsafe"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{"status", "--repo", repo, "--json"}, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80})
	if code != exitStore || strings.Contains(stderr.String(), "pact setup") {
		t.Fatalf("unsafe store diagnostic = (%d, %q)", code, stderr.String())
	}
}

func staleOperatorRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	keyPath := filepath.Join(t.TempDir(), "stale.key.json")
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	runJSON(t, []string{"keygen", "--actor", "Alice", "--out", keyPath, "--json"})
	runJSON(t, []string{"trust-add", "--repo", repo, "--key-file", keyPath, "--json"})
	runJSON(t, []string{"index", "rebuild", "--repo", repo, "--json"})
	writeCLIEvent(t, repo, keyPath, "stale", "widget-stale", "widget.stale", nil, nil)
	return repo
}

func brokenOperatorRepository(t *testing.T) string {
	t.Helper()
	fixture := newCLIQueryFixture(t)
	objects, err := filepath.Glob(filepath.Join(fixture.repo, ".pact", "objects", "sha256", "*", "*.json"))
	if err != nil || len(objects) == 0 {
		t.Fatalf("fixture objects = %v, %v", objects, err)
	}
	if err := os.WriteFile(objects[0], []byte(`{"corrupt":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return fixture.repo
}

func healthyOperatorRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	keyPath := filepath.Join(t.TempDir(), "operator.key.json")
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	runJSON(t, []string{"keygen", "--actor", "Alice", "--out", keyPath, "--json"})
	runJSON(t, []string{"trust-add", "--repo", repo, "--key-file", keyPath, "--json"})
	runJSON(t, []string{"index", "rebuild", "--repo", repo, "--json"})
	return repo
}
