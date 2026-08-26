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
	var envelope map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	details := envelope["details"].(map[string]any)
	if details["repo"] != nested {
		t.Fatalf("unsafe discovery chose parent: %#v", details)
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

func TestStatusColorPrecedenceAndJSONPlainness(t *testing.T) {
	repo := healthyOperatorRepository(t)
	for _, test := range []struct {
		name      string
		args      []string
		config    runConfig
		wantColor bool
	}{
		{name: "NO_COLOR", args: []string{"status", "--repo", repo}, config: runConfig{StdoutTerminal: true, Environment: map[string]string{"NO_COLOR": "1"}}, wantColor: false},
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

func TestAttentionStatusUsesAttentionColor(t *testing.T) {
	var output bytes.Buffer
	if err := emitStatusHuman(&output, statuspkg.Result{Health: statuspkg.HealthAttention}, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b[32mattention") || !strings.Contains(output.String(), "\x1b[33mattention") {
		t.Fatalf("attention color = %q", output.String())
	}
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
