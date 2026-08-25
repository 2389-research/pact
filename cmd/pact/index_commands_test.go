// ABOUTME: Exercises index status and rebuild through the real store and SQLite boundary.
// ABOUTME: Verifies exact command shapes, nullable JSON fields, and concise human output.
package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"pact/internal/store"
)

func TestRunIndexRequiresExactSubcommandAndNoPositionals(t *testing.T) {
	tests := [][]string{
		{"index", "--json"},
		{"index", "unknown", "--json"},
		{"index", "status", "extra", "--json"},
		{"index", "rebuild", "extra", "--json"},
	}
	for _, args := range tests {
		runErrorJSON(t, args, exitUsage)
	}
}

func TestRunIndexInvalidSourceUsesStableTypedExit(t *testing.T) {
	repo := t.TempDir()
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	st, err := store.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutCanonical(map[string]any{"invalid": true}); err != nil {
		t.Fatal(err)
	}
	for _, subcommand := range []string{"status", "rebuild"} {
		result := runErrorJSON(t, []string{"index", subcommand, "--repo", repo, "--json"}, exitIntegrity)
		details, ok := result["details"].(map[string]any)
		if !ok || details["code"] != "source_invalid" {
			t.Fatalf("index %s invalid-source error = %#v", subcommand, result)
		}
	}
}

func TestRunIndexStatusAndRebuildEmitExactJSONShapes(t *testing.T) {
	repo := t.TempDir()
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})

	status := runJSON(t, []string{"index", "status", "--repo", repo, "--json"})
	assertExactKeys(t, status, "operation", "index", "replica", "counts", "limits")
	if status["operation"] != "index-status" {
		t.Fatalf("status operation = %#v", status["operation"])
	}
	indexInfo := status["index"].(map[string]any)
	if indexInfo["state"] != "missing" || indexInfo["path"] != nil || indexInfo["schema_version"] != nil || indexInfo["source_fingerprint"] != nil || indexInfo["logical_digest"] != nil {
		t.Fatalf("missing index JSON = %#v", indexInfo)
	}
	assertExactKeys(t, status["counts"].(map[string]any), "objects", "commits", "checkpoints", "events", "edges", "canonical_bytes")

	rebuilt := runJSON(t, []string{"index", "rebuild", "--repo", repo, "--json"})
	assertExactKeys(t, rebuilt, "operation", "index", "replica", "counts", "limits", "created", "replaced")
	if rebuilt["operation"] != "index-rebuild" || rebuilt["created"] != true || rebuilt["replaced"] != false {
		t.Fatalf("rebuild JSON = %#v", rebuilt)
	}
	current := rebuilt["index"].(map[string]any)
	if current["state"] != "current" || current["coverage"] != "complete" || current["path"] == nil || current["schema_version"] != float64(1) {
		t.Fatalf("current index JSON = %#v", current)
	}
}

func TestRunIndexHumanOutputNamesStateAndReplicaFacts(t *testing.T) {
	repo := t.TempDir()
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	var stdout, stderr bytes.Buffer
	if code := run([]string{"index", "status", "--repo", repo}, &stdout, &stderr); code != 0 {
		t.Fatalf("index status exit = %d, stderr=%q", code, stderr.String())
	}
	for _, required := range []string{"missing", "unavailable", "locally_closed", "global", "unknown", "rebuild"} {
		if !strings.Contains(strings.ToLower(stdout.String()), required) {
			t.Fatalf("human status %q lacks %q", stdout.String(), required)
		}
	}
}

func assertExactKeys(t *testing.T, value map[string]any, keys ...string) {
	t.Helper()
	got := make([]string, 0, len(value))
	for key := range value {
		got = append(got, key)
	}
	slicesSort(got)
	slicesSort(keys)
	if !reflect.DeepEqual(got, keys) {
		t.Fatalf("keys = %#v, want %#v; value=%#v", got, keys, value)
	}
}

func slicesSort(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
