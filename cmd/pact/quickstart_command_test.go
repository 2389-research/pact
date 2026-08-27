// ABOUTME: Defines the pipe-safe pact quickstart contract for agents and automation.
// ABOUTME: Verifies canonical skill bytes, JSON parity, help, arguments, and writer failures.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuickstartEmitsCanonicalGoSkill(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "docs", "pact-ledger", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{"quickstart"}, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80})
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("quickstart = (%d, stderr=%q)", code, stderr.String())
	}
	if !bytes.Equal(stdout.Bytes(), want) {
		t.Fatalf("quickstart bytes differ from canonical SKILL.md")
	}
	output := stdout.String()
	for _, required := range []string{"name: pact-ledger", "pact setup", "pact verify", "pact commit", "pact query"} {
		if !strings.Contains(output, required) {
			t.Fatalf("quickstart lacks %q", required)
		}
	}
	for _, legacy := range []string{"python3", "scripts/pact.py", "Requires filesystem access and Python"} {
		if strings.Contains(output, legacy) {
			t.Fatalf("quickstart retained legacy instruction %q", legacy)
		}
	}
}

func TestQuickstartJSONContainsCanonicalSkill(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "docs", "pact-ledger", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{"quickstart", "--json"}, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80})
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("quickstart JSON = (%d, stderr=%q)", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode quickstart JSON: %v", err)
	}
	if result["operation"] != "quickstart" || result["format"] != "skill.md" || result["skill"] != string(want) {
		t.Fatalf("quickstart JSON has the wrong envelope")
	}
}

func TestQuickstartRejectsArgumentsAndReportsWriterFailures(t *testing.T) {
	t.Run("arguments", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := runWithConfig([]string{"quickstart", "extra"}, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80}); code != exitUsage {
			t.Fatalf("quickstart argument exit = %d, want %d", code, exitUsage)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "quickstart accepts no arguments") {
			t.Fatalf("quickstart argument output = stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	})

	t.Run("raw writer", func(t *testing.T) {
		var stderr bytes.Buffer
		if code := runWithConfig([]string{"quickstart"}, runConfig{Stdout: closedOutput{}, Stderr: &stderr, Width: 80}); code != exitUnexpectedError {
			t.Fatalf("quickstart writer exit = %d, want %d", code, exitUnexpectedError)
		}
		if !strings.Contains(stderr.String(), "quickstart output failed") {
			t.Fatalf("quickstart writer stderr = %q", stderr.String())
		}
	})
}

func TestQuickstartCatalogAndHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runWithConfig([]string{"quickstart", "--help"}, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80}); code != 0 || stderr.Len() != 0 {
		t.Fatalf("quickstart help = (%d, stderr=%q)", code, stderr.String())
	}
	for _, fragment := range []string{"Usage: pact quickstart [--json]", "Print the PACT agent skill", "--json"} {
		if !strings.Contains(stdout.String(), fragment) {
			t.Fatalf("quickstart help lacks %q: %q", fragment, stdout.String())
		}
	}
	stdout.Reset()
	if code := runWithConfig([]string{"help"}, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80}); code != 0 || !strings.Contains(stdout.String(), "quickstart") {
		t.Fatalf("top-level help lacks quickstart: %q", stdout.String())
	}
}
