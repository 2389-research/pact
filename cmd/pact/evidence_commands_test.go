// ABOUTME: Checks evidence command selection against real signed events and SQLite state.
// ABOUTME: Ensures reads preserve store bytes and keep observation time separate from authorship.
package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"pact/internal/canonical"
)

func TestEvidenceCheckSelectsEventWithoutMutation(t *testing.T) {
	repo := t.TempDir()
	key := filepath.Join(t.TempDir(), "fixture.key")
	runJSON(t, []string{"setup", "--repo", repo, "--namespace", "org/example/evidence", "--actor", "Synthetic Fixture", "--key-file", key, "--json"})
	proof := []byte("synthetic proof body never printed\n")
	if err := os.WriteFile(filepath.Join(repo, "proof.txt"), proof, 0o600); err != nil {
		t.Fatal(err)
	}
	batch := map[string]any{"events": []any{map[string]any{"local_id": "proof", "kind": "observation", "type": "proof.observed", "subject": "fixture", "schema_ref": "pact:core/generic-object/v1", "payload": map[string]any{}, "evidence": []any{map[string]any{"ref": "proof.txt", "digest": canonical.Digest(proof), "media_type": "text/plain", "role": "primary"}}, "caused_by": []any{}, "supersedes": []any{}, "tags": []any{}}}}
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "events.json")
	if err := os.WriteFile(input, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	committed := runJSON(t, []string{"commit", "--repo", repo, "--key-file", key, "--events", input, "--json"})
	ref := committed["event_refs"].([]any)[0].(string)
	runJSON(t, []string{"index", "rebuild", "--repo", repo, "--json"})
	for _, state := range []string{"current", "missing"} {
		if state == "missing" {
			if err := os.Remove(filepath.Join(repo, ".pact", "index", "pact-v1.sqlite3")); err != nil {
				t.Fatal(err)
			}
		}
		before := evidenceSnapshot(t, repo)
		var out, diagnostic bytes.Buffer
		checked := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
		code := runWithConfig([]string{"evidence", "check", "--repo", repo, "--event-ref", ref, "--json"}, runConfig{Stdout: &out, Stderr: &diagnostic, Now: func() time.Time { return checked }})
		if code != 0 {
			t.Fatalf("%s index: exit %d: %s", state, code, diagnostic.String())
		}
		var result map[string]any
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result["checked_at"] != checked.Format(time.RFC3339Nano) || result["event_ref"] != ref {
			t.Fatalf("observation identity: %+v", result)
		}
		assessments := result["assessments"].([]any)
		if len(assessments) != 1 || assessments[0].(map[string]any)["status"] != "matching" {
			t.Fatalf("assessments: %+v", assessments)
		}
		if strings.Contains(out.String(), string(proof)) || strings.Contains(out.String(), "payload") {
			t.Fatal("evidence or payload content leaked")
		}
		if after := evidenceSnapshot(t, repo); !reflect.DeepEqual(before, after) {
			t.Fatal("evidence read changed store")
		}
	}
	var out, diagnostic bytes.Buffer
	if code := run([]string{"evidence", "check", "--repo", repo, "--event-ref", ref}, &out, &diagnostic); code != 0 || !strings.Contains(out.String(), "matching") || !strings.Contains(out.String(), "proof.txt") {
		t.Fatalf("human assessments: %d %q %q", code, out.String(), diagnostic.String())
	}
	for _, args := range [][]string{{"evidence", "check", "--repo", repo, "--json"}, {"evidence", "check", "--repo", repo, "--event-ref", committed["object_id"].(string), "--json"}, {"evidence", "check", "--repo", repo, "--event-ref", ref, "extra", "--json"}} {
		runErrorJSON(t, args, exitUsage)
	}
}

func evidenceSnapshot(t *testing.T, repo string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(filepath.Join(repo, ".pact"), func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		result[name] = canonical.Digest(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEvidenceCheckEmptySelectionAndOutputFailure(t *testing.T) {
	fixture := newCLIQueryFixture(t)
	result := runJSON(t, []string{"evidence", "check", "--repo", fixture.repo, "--event-ref", fixture.eventRef, "--json"})
	assessments, ok := result["assessments"].([]any)
	if !ok || len(assessments) != 0 {
		t.Fatalf("empty selection is not an array: %+v", result)
	}
	for _, jsonOutput := range []bool{false, true} {
		args := []string{"evidence", "check", "--repo", fixture.repo, "--event-ref", fixture.eventRef}
		if jsonOutput {
			args = append(args, "--json")
		}
		var diagnostic bytes.Buffer
		if code := run(args, failingQueryWriter{}, &diagnostic); code != exitUnexpectedError {
			t.Fatalf("output failure exit: %d", code)
		}
		if strings.Contains(diagnostic.String(), "secret-pipe-detail") {
			t.Fatal("writer detail disclosed")
		}
	}
}
