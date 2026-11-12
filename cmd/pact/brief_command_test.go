// ABOUTME: Exercises bounded development reads through the public command adapter.
// ABOUTME: Checks exact encoded budgets, safe presentation, and read-only recovery errors.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"pact/internal/development"
	"pact/internal/evidence"
	"pact/internal/ledger"
)

func briefTestArgs(repo string) []string {
	return []string{"brief", "--repo", repo, "--profile", "development/v1", "--namespace", "org/example/brief", "--subject", "task/brief", "--source-revision", strings.Repeat("a", 40)}
}

func TestBriefProjectionErrorOwnership(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code int
	}{
		{"request", &development.ProjectRequestError{Message: "duplicate frontier commit"}, exitUsage},
		{"canceled", context.Canceled, exitUnexpectedError},
		{"unknown read failure", errors.New("read failed"), exitUnexpectedError},
		{"missing dependency", ledger.ErrMissingDependency, exitMissingDependency},
		{"integrity", ledger.ErrIntegrity, exitIntegrity},
		{"evidence limit", evidence.ErrLimit, exitUsage},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := briefProjectionError(fmt.Errorf("project: %w", test.err))
			if failure.code != test.code || !strings.Contains(failure.message, test.err.Error()) {
				t.Fatalf("error ownership: %+v, want exit %d", failure, test.code)
			}
		})
	}
}

func TestBriefEncodingPreservesStateAndSanitizesControls(t *testing.T) {
	text := "quotes \" slash \\ tab\t line\n return\r snowman ☃ café \x1b\x7f\u0085"
	state := development.WorkState{Namespace: "org/example/brief", Subject: text, SourceRevision: strings.Repeat("a", 64), Frontier: []string{}, Checks: []development.Claim{{EventRef: "check", Payload: map[string]any{"outcome": "passed", "command": []string{text}}}}, Findings: []development.Finding{{FindingID: "blocker", Summary: text, Severity: "important", Status: "open"}}, SupportedAcceptance: development.AcceptanceSupport{Status: "not_recorded", Reasons: []string{text}}}
	for _, asJSON := range []bool{false, true} {
		raw, err := encodeBrief(state, asJSON)
		if err != nil {
			t.Fatal(err)
		}
		if raw[len(raw)-1] != '\n' {
			t.Fatal("missing final newline")
		}
		if !asJSON {
			for _, character := range string(raw) {
				if unicode.IsControl(character) && character != '\n' {
					t.Fatalf("unescaped terminal control %U", character)
				}
			}
			raw = raw[bytes.IndexByte(raw, '\n')+1:]
		}
		var decoded development.WorkState
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		// Compare JSON values to avoid []string versus []any in claim payloads.
		want, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(decoded)
		if err != nil {
			t.Fatal(err)
		}
		var wantValue, gotValue any
		json.Unmarshal(want, &wantValue)
		json.Unmarshal(got, &gotValue)
		if !reflect.DeepEqual(wantValue, gotValue) {
			t.Fatalf("state changed: %s want %s", got, want)
		}
	}
}

func TestBriefCatalogAndRequiredArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{"brief", "--help"}, runConfig{Stdout: &stdout, Stderr: &stderr})
	if code != 0 || !strings.Contains(stdout.String(), "--max-bytes") || !strings.Contains(stdout.String(), "--frontier") {
		t.Fatalf("brief help: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	for _, args := range [][]string{{"brief", "--json"}, append(briefTestArgs(t.TempDir()), "--max-bytes", "1023", "--json"), append(briefTestArgs(t.TempDir()), "--max-bytes", "65537", "--json")} {
		result := runErrorJSON(t, args, exitUsage)
		if strings.Contains(result["error"].(string), "unknown command") {
			t.Fatalf("brief is absent: %+v", result)
		}
	}
}

func TestBriefReadsSignedStoreAndReportsWriterFailure(t *testing.T) {
	repo := t.TempDir()
	key := filepath.Join(t.TempDir(), "key.json")
	runJSON(t, []string{"setup", "--repo", repo, "--namespace", "org/example/brief", "--actor", "Brief fixture", "--key-file", key, "--json"})
	args := briefTestArgs(repo)
	result := runJSON(t, append(args, "--json"))
	if result["operation"] != "brief" || result["supported_acceptance"].(map[string]any)["status"] != "not_recorded" {
		t.Fatalf("brief lost absent decision: %+v", result)
	}
	var stdout, stderr bytes.Buffer
	if code := runWithConfig(args, runConfig{Stdout: &stdout, Stderr: &stderr}); code != 0 || !strings.Contains(stdout.String(), "not_recorded") || !strings.Contains(stdout.String(), "reviewer_independence") {
		t.Fatalf("human brief: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	stderr.Reset()
	if code := runWithConfig(args, runConfig{Stdout: closedOutput{}, Stderr: &stderr}); code != exitUnexpectedError || !strings.Contains(stderr.String(), "brief output failed") {
		t.Fatalf("writer: code=%d stderr=%s", code, &stderr)
	}
}
