// ABOUTME: Exercises bootstrap PACT CLI commands through their in-process argument boundary.
// ABOUTME: Verifies stable JSON results, validation exit codes, and private-key-safe diagnostics.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testCLIPolicyRef = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testCLISchemaA   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testCLISchemaB   = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func TestRunInitAndHashEmitContractJSON(t *testing.T) {
	repo := t.TempDir()
	result := runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	storePath, err := filepath.EvalSymlinks(filepath.Join(repo, ".pact"))
	if err != nil {
		t.Fatal(err)
	}
	if result["operation"] != "init" || result["format"] != "pact/store/v1" || result["default_namespace"] != "org/example/widget" || result["store"] != storePath {
		t.Fatalf("init JSON = %#v", result)
	}
	file := filepath.Join(t.TempDir(), "evidence.txt")
	if err := os.WriteFile(file, []byte("evidence bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	result = runJSON(t, []string{"hash", file, "--json"})
	if result["operation"] != "hash" || result["digest"] != "sha256:9d11f9a71c12d6194481f5fa5086b0eff7df05a4a228f022f55bd890009a9d16" || result["size"] != float64(14) || result["path"] != file {
		t.Fatalf("hash JSON = %#v", result)
	}
}

func TestRunKeygenAndTrustAddNeverLeakPrivateBytes(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "alice.key.json")
	result := runJSON(t, []string{"keygen", "--actor", "Alice", "--out", keyPath, "--json"})
	if result["operation"] != "keygen" || result["actor"] != "Alice" || result["private_key"] != nil {
		t.Fatalf("keygen JSON = %#v", result)
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]any
	if err := json.Unmarshal(keyBytes, &key); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	result = runJSON(t, []string{"trust-add", "--repo", repo, "--key-file", keyPath, "--json"})
	if result["operation"] != "trust-add" || result["created"] != true || result["key_id"] != key["key_id"] {
		t.Fatalf("trust-add JSON = %#v", result)
	}
	secret := "private_material_that_must_not_leak"
	key["private_key"] = secret
	badKey, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	badKeyPath := filepath.Join(t.TempDir(), "bad.key.json")
	if err := os.WriteFile(badKeyPath, badKey, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"trust-add", "--repo", repo, "--key-file", badKeyPath}, &stdout, &stderr); code != 2 {
		t.Fatalf("invalid key exit = %d, want 2; stderr=%q", code, stderr.String())
	}
	if bytes.Contains(stderr.Bytes(), []byte(secret)) || bytes.Contains(stdout.Bytes(), []byte(secret)) {
		t.Fatalf("diagnostic leaked private key bytes: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunRejectsMissingRequiredArguments(t *testing.T) {
	for _, args := range [][]string{{"init", "--repo", t.TempDir()}, {"keygen", "--actor", "Alice"}, {"trust-add", "--repo", t.TempDir()}, {"hash"}, {"checkpoint", "--repo", t.TempDir()}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("run(%q) exit = %d, want 2; stderr=%q", args, code, stderr.String())
		}
	}
}

func TestRunCheckpointEmitsCanonicalAuthorizedResult(t *testing.T) {
	repo := t.TempDir()
	keyPath := filepath.Join(t.TempDir(), "alice.key.json")
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	runJSON(t, []string{"keygen", "--actor", "Alice", "--out", keyPath, "--json"})
	runJSON(t, []string{"trust-add", "--repo", repo, "--key-file", keyPath, "--json"})
	batchPath := filepath.Join(t.TempDir(), "events.json")
	if err := os.WriteFile(batchPath, []byte(`{"events":[{"local_id":"e1","kind":"observation","type":"widget.seen","subject":"widget-1","schema_ref":"pact:core/widget/v1","payload":{},"evidence":[],"caused_by":[],"supersedes":[],"tags":[]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runJSON(t, []string{"commit", "--repo", repo, "--key-file", keyPath, "--events", batchPath, "--json"})
	checkpoint := runJSON(t, []string{"checkpoint", "--repo", repo, "--key-file", keyPath, "--scope", "org/example", "--policy-ref", testCLIPolicyRef, "--authority-epoch", "epoch-1", "--schema-ref", testCLISchemaB, "--schema-ref", testCLISchemaA, "--schema-ref", testCLISchemaB, "--purpose", "release cut", "--json"})
	if checkpoint["authorization"] != "authorized" || checkpoint["integrity"] != "valid" || checkpoint["authenticity"] != "valid" {
		t.Fatalf("checkpoint JSON = %#v", checkpoint)
	}
	refs := checkpoint["schema_refs"].([]any)
	if len(refs) != 2 || refs[0] != testCLISchemaA || refs[1] != testCLISchemaB {
		t.Fatalf("schema refs = %#v", refs)
	}
	shown := runJSON(t, []string{"show", "--repo", repo, checkpoint["object_id"].(string), "--json"})
	if shown["kind"] != "checkpoint" {
		t.Fatalf("show checkpoint JSON = %#v", shown)
	}
	verified := runJSON(t, []string{"verify", "--repo", repo, "--strict", "--json"})
	if verified["counts"].(map[string]any)["checkpoints"] != float64(1) {
		t.Fatalf("verify checkpoint JSON = %#v", verified)
	}
}

func TestRunCheckpointStrictFailureIncludesVerificationDetails(t *testing.T) {
	repo := t.TempDir()
	keyPath := filepath.Join(t.TempDir(), "alice.key.json")
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	runJSON(t, []string{"keygen", "--actor", "Alice", "--out", keyPath, "--json"})
	runJSON(t, []string{"trust-add", "--repo", repo, "--key-file", keyPath, "--json"})
	batchPath := filepath.Join(t.TempDir(), "incomplete.json")
	missing := "pact:event:sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd#gone"
	batch := []byte(`{"events":[{"local_id":"e1","kind":"observation","type":"widget.seen","subject":"widget-1","schema_ref":"pact:core/widget/v1","payload":{},"evidence":[],"caused_by":[],"supersedes":["` + missing + `"],"tags":[]}]}`)
	if err := os.WriteFile(batchPath, batch, 0o644); err != nil {
		t.Fatal(err)
	}
	runJSON(t, []string{"commit", "--repo", repo, "--key-file", keyPath, "--events", batchPath, "--json"})
	result := runErrorJSON(t, []string{"checkpoint", "--repo", repo, "--key-file", keyPath, "--scope", "org/example", "--policy-ref", testCLIPolicyRef, "--authority-epoch", "epoch-1", "--json"}, 4)
	details, ok := result["details"].(map[string]any)
	if !ok || details["objects"] == nil || details["references"].(map[string]any)["errors"] == nil {
		t.Fatalf("checkpoint error JSON = %#v", result)
	}
	if details["counts"].(map[string]any)["objects"] != float64(1) {
		t.Fatalf("checkpoint verification counts = %#v", details["counts"])
	}
}

func TestRunCheckpointUntrustedSignerUsesAuthorizationExitWithoutPersistence(t *testing.T) {
	repo := t.TempDir()
	keyPath := filepath.Join(t.TempDir(), "alice.key.json")
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	runJSON(t, []string{"keygen", "--actor", "Alice", "--out", keyPath, "--json"})
	batchPath := filepath.Join(t.TempDir(), "events.json")
	if err := os.WriteFile(batchPath, []byte(`{"events":[{"local_id":"e1","kind":"observation","type":"widget.seen","subject":"widget-1","schema_ref":"pact:core/widget/v1","payload":{},"evidence":[],"caused_by":[],"supersedes":[],"tags":[]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runJSON(t, []string{"commit", "--repo", repo, "--key-file", keyPath, "--events", batchPath, "--json"})
	before := cliObjectCount(t, repo)
	result := runErrorJSON(t, []string{"checkpoint", "--repo", repo, "--key-file", keyPath, "--scope", "org/example", "--policy-ref", testCLIPolicyRef, "--authority-epoch", "epoch-1", "--json"}, 5)
	if !strings.Contains(result["error"].(string), "trusted root") {
		t.Fatalf("checkpoint authorization error = %#v", result)
	}
	if after := cliObjectCount(t, repo); after != before {
		t.Fatalf("object count after authorization refusal = %d, want %d", after, before)
	}
}

func TestRunRejectsInvalidNamespace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "--repo", t.TempDir(), "--namespace", "not a namespace"}, &stdout, &stderr); code != 2 {
		t.Fatalf("invalid namespace exit = %d, want 2; stderr=%q", code, stderr.String())
	}
}

func TestRunCommitHeadsShowAndVerifyEmitJSON(t *testing.T) {
	repo := t.TempDir()
	keyPath := filepath.Join(t.TempDir(), "alice.key.json")
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	runJSON(t, []string{"keygen", "--actor", "Alice", "--out", keyPath, "--json"})
	batchPath := filepath.Join(t.TempDir(), "events.json")
	batch := []byte(`{"observed_at":"2026-08-23T12:00:00Z","events":[{"local_id":"e1","kind":"observation","type":"widget.seen","subject":"widget-1","schema_ref":"pact:core/widget/v1","payload":{},"evidence":[],"caused_by":[],"supersedes":[],"tags":[]}]}`)
	if err := os.WriteFile(batchPath, batch, 0o644); err != nil {
		t.Fatal(err)
	}
	commit := runJSON(t, []string{"commit", "--repo", repo, "--key-file", keyPath, "--events", batchPath, "--json"})
	if commit["operation"] != "commit" || commit["integrity"] != "valid" || commit["authenticity"] != "valid" {
		t.Fatalf("commit JSON = %#v", commit)
	}
	heads := runJSON(t, []string{"heads", "--repo", repo, "--namespace", "org/example", "--json"})
	if heads["operation"] != "heads" || heads["heads"] == nil {
		t.Fatalf("heads JSON = %#v", heads)
	}
	eventRef := commit["event_refs"].([]any)[0].(string)
	show := runJSON(t, []string{"show", "--repo", repo, eventRef, "--json"})
	if show["kind"] != "event" || show["event"].(map[string]any)["local_id"] != "e1" {
		t.Fatalf("show JSON = %#v", show)
	}
	verify := runJSON(t, []string{"verify", "--repo", repo, "--json"})
	if verify["operation"] != "verify" || verify["ok"] != true || verify["counts"] == nil {
		t.Fatalf("verify JSON = %#v", verify)
	}
	authorization := verify["authorization"].(map[string]any)[commit["object_id"].(string)].(map[string]any)
	if authorization["status"] != "indeterminate" || authorization["Status"] != nil {
		t.Fatalf("verify authorization JSON = %#v", authorization)
	}
}

func TestRunVerifyFailureIncludesLayeredDetailsAndCommitUsesIntegrityExit(t *testing.T) {
	repo := t.TempDir()
	keyPath := filepath.Join(t.TempDir(), "alice.key.json")
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	runJSON(t, []string{"keygen", "--actor", "Alice", "--out", keyPath, "--json"})
	batchPath := filepath.Join(t.TempDir(), "events.json")
	if err := os.WriteFile(batchPath, []byte(`{"events":[{"local_id":"e1","kind":"observation","type":"widget.seen","subject":"widget-1","schema_ref":"pact:core/widget/v1","payload":{},"evidence":[],"caused_by":[],"supersedes":[],"tags":[]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := runJSON(t, []string{"commit", "--repo", repo, "--key-file", keyPath, "--events", batchPath, "--json"})
	hexID := commit["object_id"].(string)[len("sha256:"):]
	path := filepath.Join(repo, ".pact", "objects", "sha256", hexID[:2], hexID[2:]+".json")
	if err := os.WriteFile(path, []byte(`{"bad":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runErrorJSON(t, []string{"verify", "--repo", repo, "--json"}, 4)
	if result["details"] == nil || result["details"].(map[string]any)["objects"] == nil || result["details"].(map[string]any)["counts"] == nil {
		t.Fatalf("verify error JSON = %#v", result)
	}
	result = runErrorJSON(t, []string{"commit", "--repo", repo, "--key-file", keyPath, "--events", batchPath, "--json"}, 4)
	if result["exit_code"] != float64(4) {
		t.Fatalf("commit error JSON = %#v", result)
	}
}

func runJSON(t *testing.T, args []string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("run(%q) exit = %d, stderr=%q", args, code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode JSON output %q: %v", stdout.String(), err)
	}
	return result
}

func runErrorJSON(t *testing.T, args []string, wantCode int) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != wantCode {
		t.Fatalf("run(%q) exit = %d, want %d; stderr=%q", args, code, wantCode, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &result); err != nil {
		t.Fatalf("decode error JSON %q: %v", stderr.String(), err)
	}
	return result
}

func cliObjectCount(t *testing.T, repo string) int {
	t.Helper()
	objects, err := filepath.Glob(filepath.Join(repo, ".pact", "objects", "sha256", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return len(objects)
}
