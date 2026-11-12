// ABOUTME: Exercises bounded evidence checks through the compiled CLI and real Git/store files.
// ABOUTME: Proves historical recovery, refusal states, limits, and non-disclosure without mocks.
package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"pact/internal/canonical"
)

func TestCLIDevelopmentEvidence(t *testing.T) {
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, projectRoot(t), binary)
	repo := filepath.Join(workspace, "repo")
	mustMkdir(t, repo)
	source, err := os.ReadFile(filepath.Join(projectRoot(t), "tests/fixtures/development/source.fast-import"))
	if err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, repo, nil, "init", "--quiet", "--object-format=sha1")
	runFixtureGit(t, repo, source, "fast-import", "--quiet")
	revision := strings.TrimSpace(string(runFixtureGit(t, repo, nil, "rev-parse", "main^")))
	proof := runFixtureGit(t, repo, nil, "show", revision+":proof.txt")
	key := filepath.Join(workspace, "fixture.key")
	runJSON(t, binary, "setup", "--repo", repo, "--namespace", "org/example/evidence", "--actor", "Synthetic Fixture", "--key-file", key, "--json")
	if err := os.WriteFile(filepath.Join(repo, "proof.txt"), []byte("changed synthetic content"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries := make([]map[string]any, 0, 6)
	for _, ref := range []string{"git:" + revision + ":proof.txt", "proof.txt", "missing", "git:" + revision + ":missing", "https://example.test/proof", "/old/absolute/proof"} {
		entries = append(entries, map[string]any{"ref": ref, "digest": canonical.Digest(proof), "media_type": "text/plain", "role": "primary"})
	}
	ref := commitEvidenceEvent(t, binary, repo, key, entries)
	runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")
	before := snapshotSetupFiles(t, repo, key)
	result := runJSON(t, binary, "evidence", "check", "--repo", repo, "--event-ref", ref, "--json")
	assessments := result["assessments"].([]any)
	for i, status := range []string{"matching", "changed", "unavailable", "unavailable", "unsupported", "unsupported"} {
		if assessments[i].(map[string]any)["status"] != status {
			t.Fatalf("assessment %d: %+v", i, assessments[i])
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{string(proof), "changed synthetic content", "synthetic-credential"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("output disclosed %q", secret)
		}
	}
	if after := snapshotSetupFiles(t, repo, key); !reflect.DeepEqual(before, after) {
		t.Fatal("check changed canonical or index state")
	}
	// The same artifact selected repeatedly consumes its bytes on each read.
	large := bytes.Repeat([]byte("x"), 16<<20)
	if err := os.WriteFile(filepath.Join(repo, "large"), large, 0o600); err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{"ref": "large", "digest": canonical.Digest(large), "media_type": "text/plain", "role": "primary"}
	ref = commitEvidenceEvent(t, binary, repo, key, []map[string]any{entry, entry, entry, entry, entry})
	result = runErrorJSON(t, 2, binary, "evidence", "check", "--repo", repo, "--event-ref", ref, "--json")
	if result["error"] != "evidence byte limit exceeded" {
		t.Fatalf("aggregate overflow: %+v", result)
	}
	if err := os.WriteFile(filepath.Join(repo, "large"), append(large, 'x'), 0o600); err != nil {
		t.Fatal(err)
	}
	ref = commitEvidenceEvent(t, binary, repo, key, []map[string]any{entry})
	runErrorJSON(t, 2, binary, "evidence", "check", "--repo", repo, "--event-ref", ref, "--json")
}

func commitEvidenceEvent(t *testing.T, binary, repo, key string, entries []map[string]any) string {
	t.Helper()
	batch := map[string]any{"events": []any{map[string]any{"local_id": "proof", "kind": "observation", "type": "proof.observed", "subject": "fixture", "schema_ref": "pact:core/generic-object/v1", "payload": map[string]any{}, "evidence": entries, "caused_by": []any{}, "supersedes": []any{}, "tags": []any{}}}}
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "events.json")
	if err := os.WriteFile(input, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	committed := runJSON(t, binary, "commit", "--repo", repo, "--key-file", key, "--events", input, "--json")
	return committed["event_refs"].([]any)[0].(string)
}
