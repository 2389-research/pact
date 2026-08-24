// ABOUTME: Exercises bootstrap PACT CLI commands through their in-process argument boundary.
// ABOUTME: Verifies stable JSON results, validation exit codes, and private-key-safe diagnostics.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
	for _, args := range [][]string{{"init", "--repo", t.TempDir()}, {"keygen", "--actor", "Alice"}, {"trust-add", "--repo", t.TempDir()}, {"hash"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("run(%q) exit = %d, want 2; stderr=%q", args, code, stderr.String())
		}
	}
}

func TestRunRejectsInvalidNamespace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "--repo", t.TempDir(), "--namespace", "not a namespace"}, &stdout, &stderr); code != 2 {
		t.Fatalf("invalid namespace exit = %d, want 2; stderr=%q", code, stderr.String())
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
