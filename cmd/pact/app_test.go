// ABOUTME: Exercises bootstrap PACT CLI commands through their in-process argument boundary.
// ABOUTME: Verifies stable JSON results, validation exit codes, and private-key-safe diagnostics.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"pact/internal/index"
	"pact/internal/ledger"
	statuspkg "pact/internal/status"
	"pact/internal/store"
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
	resolvedFile, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatal(err)
	}
	result = runJSON(t, []string{"hash", file, "--json"})
	if result["operation"] != "hash" || result["digest"] != "sha256:9d11f9a71c12d6194481f5fa5086b0eff7df05a4a228f022f55bd890009a9d16" || result["size"] != float64(14) || result["path"] != resolvedFile {
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
	verify := runErrorJSON(t, []string{"verify", "--repo", repo, "--strict", "--json"}, 9)
	if verify["details"].(map[string]any)["completeness"].(map[string]any)["status"] != "incomplete" {
		t.Fatalf("strict incomplete verification = %#v", verify)
	}
	result := runErrorJSON(t, []string{"checkpoint", "--repo", repo, "--key-file", keyPath, "--scope", "org/example", "--policy-ref", testCLIPolicyRef, "--authority-epoch", "epoch-1", "--json"}, 9)
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

func TestVerificationFailureExitCodeFailsClosedForHardAndUnknownFailures(t *testing.T) {
	tests := []struct {
		name   string
		result ledger.VerifyResult
		want   int
	}{
		{
			name:   "missing parent only",
			result: ledger.VerifyResult{Completeness: ledger.Completeness{Status: "incomplete", Blockers: []ledger.Blocker{{Code: "missing_parent"}}}, Counts: ledger.VerifyCounts{DAG: 1}},
			want:   exitMissingDependency,
		},
		{
			name:   "missing event reference only",
			result: ledger.VerifyResult{Completeness: ledger.Completeness{Status: "incomplete", Blockers: []ledger.Blocker{{Code: "missing_event_reference"}}}, Counts: ledger.VerifyCounts{References: 1}},
			want:   exitMissingDependency,
		},
		{
			name:   "cycle plus missing reference",
			result: ledger.VerifyResult{Completeness: ledger.Completeness{Status: "incomplete", Blockers: []ledger.Blocker{{Code: "missing_event_reference"}}}, Counts: ledger.VerifyCounts{DAG: 1, References: 1}},
			want:   exitIntegrity,
		},
		{
			name:   "hard reference plus missing reference",
			result: ledger.VerifyResult{Completeness: ledger.Completeness{Status: "incomplete", Blockers: []ledger.Blocker{{Code: "missing_event_reference"}}}, Counts: ledger.VerifyCounts{References: 2}},
			want:   exitIntegrity,
		},
		{
			name:   "canonical invalid plus missing reference",
			result: ledger.VerifyResult{Completeness: ledger.Completeness{Status: "incomplete", Blockers: []ledger.Blocker{{Code: "missing_event_reference"}}}, Counts: ledger.VerifyCounts{Integrity: 1, References: 1}},
			want:   exitIntegrity,
		},
		{
			name:   "unknown blocker",
			result: ledger.VerifyResult{Completeness: ledger.Completeness{Status: "incomplete", Blockers: []ledger.Blocker{{Code: "future_blocker"}}}, Counts: ledger.VerifyCounts{References: 1}},
			want:   exitIntegrity,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := verificationFailureExitCode(test.result); got != test.want {
				t.Fatalf("verificationFailureExitCode() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestRunVerifyMixedIncompleteAndMalformedTrustUsesIntegrityExit(t *testing.T) {
	repo := t.TempDir()
	keyPath := filepath.Join(t.TempDir(), "alice.key.json")
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	runJSON(t, []string{"keygen", "--actor", "Alice", "--out", keyPath, "--json"})
	batchPath := filepath.Join(t.TempDir(), "incomplete.json")
	missing := "pact:event:sha256:" + strings.Repeat("d", 64) + "#gone"
	batch := []byte(`{"events":[{"local_id":"e1","kind":"observation","type":"widget.seen","subject":"widget-1","schema_ref":"pact:core/widget/v1","payload":{},"evidence":[],"caused_by":["` + missing + `"],"supersedes":[],"tags":[]}]}`)
	if err := os.WriteFile(batchPath, batch, 0o644); err != nil {
		t.Fatal(err)
	}
	runJSON(t, []string{"commit", "--repo", repo, "--key-file", keyPath, "--events", batchPath, "--json"})
	if err := os.WriteFile(filepath.Join(repo, ".pact", "trust.json"), []byte(`{"format":"wrong","roots":[]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runErrorJSON(t, []string{"verify", "--repo", repo, "--strict", "--json"}, exitIntegrity)
	if result["details"].(map[string]any)["completeness"].(map[string]any)["status"] != "incomplete" {
		t.Fatalf("mixed incomplete and malformed-trust verification = %#v", result)
	}
}

func TestRunRejectsInvalidNamespace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "--repo", t.TempDir(), "--namespace", "not a namespace"}, &stdout, &stderr); code != 2 {
		t.Fatalf("invalid namespace exit = %d, want 2; stderr=%q", code, stderr.String())
	}
}

func TestRunJSONFlagParseErrorEmitsOneStructuredDiagnostic(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"heads", "--repo", t.TempDir(), "--not-a-flag", "--json"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("invalid flag exit = %d, want %d", code, exitUsage)
	}
	decoder := json.NewDecoder(&stderr)
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode structured error %q: %v", stderr.String(), err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil || err != io.EOF {
		t.Fatalf("stderr has extra flag prose or JSON: %q, decode err=%v", stderr.String(), err)
	}
	if result["exit_code"] != float64(exitUsage) || strings.Contains(stderr.String(), "Usage of") || stdout.Len() != 0 {
		t.Fatalf("invalid flag output: stdout=%q stderr=%q result=%#v", stdout.String(), stderr.String(), result)
	}
}

func TestRunTrustAddAllowsPublicOnlyReadableKey(t *testing.T) {
	repo := t.TempDir()
	privatePath := filepath.Join(t.TempDir(), "alice.key.json")
	publicPath := filepath.Join(t.TempDir(), "alice.public.json")
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	runJSON(t, []string{"keygen", "--actor", "Alice", "--out", privatePath, "--json"})
	raw, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]any
	if err := json.Unmarshal(raw, &key); err != nil {
		t.Fatal(err)
	}
	delete(key, "private_key")
	raw, err = json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	result := runJSON(t, []string{"trust-add", "--repo", repo, "--key-file", publicPath, "--json"})
	if result["created"] != true || result["key_id"] != key["key_id"] {
		t.Fatalf("trust-add public key = %#v", result)
	}
}

func TestRunUsesTypedExitClassificationAndPreservesShowDetails(t *testing.T) {
	repo := t.TempDir()
	keyPath := filepath.Join(t.TempDir(), "alice.key.json")
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	runJSON(t, []string{"keygen", "--actor", "Alice", "--out", keyPath, "--json"})

	raw, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]any
	if err := json.Unmarshal(raw, &key); err != nil {
		t.Fatal(err)
	}
	key["key_id"] = "ed25519:sha256:" + strings.Repeat("0", 64)
	badID, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	badIDPath := filepath.Join(t.TempDir(), "bad-id.key.json")
	if err := os.WriteFile(badIDPath, badID, 0o600); err != nil {
		t.Fatal(err)
	}
	runErrorJSON(t, []string{"trust-add", "--repo", repo, "--key-file", badIDPath, "--json"}, exitIntegrity)

	otherPath := filepath.Join(t.TempDir(), "other.key.json")
	runJSON(t, []string{"keygen", "--actor", "Other", "--out", otherPath, "--json"})
	otherRaw, err := os.ReadFile(otherPath)
	if err != nil {
		t.Fatal(err)
	}
	var other map[string]any
	if err := json.Unmarshal(otherRaw, &other); err != nil {
		t.Fatal(err)
	}
	key["public_key"] = other["public_key"]
	key["key_id"] = other["key_id"]
	mismatchRaw, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	mismatchPath := filepath.Join(t.TempDir(), "mismatch.key.json")
	if err := os.WriteFile(mismatchPath, mismatchRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	batchForMismatch := filepath.Join(t.TempDir(), "mismatch-events.json")
	if err := os.WriteFile(batchForMismatch, []byte(`{"events":[{"local_id":"mismatch","kind":"observation","type":"widget.seen","subject":"widget-1","schema_ref":"pact:core/widget/v1","payload":{},"evidence":[],"caused_by":[],"supersedes":[],"tags":[]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runErrorJSON(t, []string{"commit", "--repo", repo, "--key-file", mismatchPath, "--events", batchForMismatch, "--json"}, exitIntegrity)

	if err := os.WriteFile(filepath.Join(repo, ".pact", "trust.json"), []byte(`{"format":"wrong","roots":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runErrorJSON(t, []string{"trust-add", "--repo", repo, "--key-file", keyPath, "--json"}, exitStore)
	if err := os.WriteFile(filepath.Join(repo, ".pact", "format.json"), []byte(`{"format":"wrong"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runErrorJSON(t, []string{"heads", "--repo", repo, "--json"}, exitStore)
	if err := os.WriteFile(filepath.Join(repo, ".pact", "format.json"), []byte("{\n  \"format\": \"pact/store/v1\",\n  \"default_namespace\": \"org/example/widget\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	missing := "sha256:" + strings.Repeat("a", 64)
	runErrorJSON(t, []string{"show", "--repo", repo, missing, "--json"}, exitMissingDependency)

	if err := os.WriteFile(filepath.Join(repo, ".pact", "trust.json"), []byte("{\n  \"format\": \"pact/trust/v1\",\n  \"roots\": []\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	batchPath := filepath.Join(t.TempDir(), "events.json")
	if err := os.WriteFile(batchPath, []byte(`{"events":[{"local_id":"e1","kind":"observation","type":"widget.seen","subject":"widget-1","schema_ref":"pact:core/widget/v1","payload":{},"evidence":[],"caused_by":[],"supersedes":[],"tags":[]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := runJSON(t, []string{"commit", "--repo", repo, "--key-file", keyPath, "--events", batchPath, "--json"})
	hexID := strings.TrimPrefix(commit["object_id"].(string), "sha256:")
	objectPath := filepath.Join(repo, ".pact", "objects", "sha256", hexID[:2], hexID[2:]+".json")
	if err := os.WriteFile(objectPath, []byte(`{"bad":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runErrorJSON(t, []string{"show", "--repo", repo, commit["object_id"].(string), "--json"}, exitIntegrity)
	details, ok := result["details"].(map[string]any)
	if !ok || details["identifier"] != commit["object_id"] || details["integrity"] != "invalid" || details["errors"] == nil {
		t.Fatalf("show integrity error = %#v", result)
	}
	objectsRoot := filepath.Join(repo, ".pact", "objects")
	if err := os.RemoveAll(objectsRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), objectsRoot); err != nil {
		t.Fatal(err)
	}
	runErrorJSON(t, []string{"heads", "--repo", repo, "--json"}, exitIntegrity)
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
	beforeSecond := runJSON(t, []string{"heads", "--repo", repo, "--namespace", "org/example", "--json"})
	if !reflect.DeepEqual(beforeSecond["heads"].(map[string]any)["org/example/widget"], []any{commit["object_id"]}) {
		t.Fatalf("heads before second commit = %#v", beforeSecond)
	}
	secondBatchPath := filepath.Join(t.TempDir(), "events.json")
	secondBatch := []byte(`{"observed_at":"2026-08-23T12:00:01Z","events":[{"local_id":"e2","kind":"observation","type":"widget.seen","subject":"widget-2","schema_ref":"pact:core/widget/v1","payload":{},"evidence":[],"caused_by":[],"supersedes":[],"tags":[]}]}`)
	if err := os.WriteFile(secondBatchPath, secondBatch, 0o644); err != nil {
		t.Fatal(err)
	}
	second := runJSON(t, []string{"commit", "--repo", repo, "--key-file", keyPath, "--events", secondBatchPath, "--json"})
	if !reflect.DeepEqual(second["parents"], []any{commit["object_id"]}) {
		t.Fatalf("second commit parents = %#v, want first commit", second["parents"])
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
	completeness := verify["completeness"].(map[string]any)
	limits := verify["limits"].(map[string]any)
	if completeness["scope"] != "local_object_set" || completeness["status"] != "locally_closed" || limits["profile"] != "pact/resource-limits/phase2-v1" || limits["status"] != "within_limits" {
		t.Fatalf("verify Phase 2 JSON = %#v", verify)
	}
	if limits["diagnostics_truncated"] != false {
		t.Fatalf("verify diagnostics_truncated = %#v", limits["diagnostics_truncated"])
	}
	authorization := verify["authorization"].(map[string]any)[commit["object_id"].(string)].(map[string]any)
	if authorization["status"] != "indeterminate" || authorization["Status"] != nil {
		t.Fatalf("verify authorization JSON = %#v", authorization)
	}
}

func TestRunShowMapsResourceLimitToStableUnavailableError(t *testing.T) {
	repo := t.TempDir()
	runJSON(t, []string{"init", "--repo", repo, "--namespace", "org/example/widget", "--json"})
	st, err := store.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := st.PutCanonical(map[string]any{"oversize": strings.Repeat("x", int(ledger.Phase2Limits.ObjectBytes))})
	if err != nil {
		t.Fatal(err)
	}
	result := runErrorJSON(t, []string{"show", "--repo", repo, id, "--json"}, exitMissingDependency)
	details := result["details"].(map[string]any)
	if details["code"] != "resource_limit" || details["resource"] != "object_bytes" || details["maximum"] != float64(ledger.Phase2Limits.ObjectBytes) {
		t.Fatalf("resource limit JSON = %#v", result)
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
	incompleteBatchPath := filepath.Join(t.TempDir(), "incomplete.json")
	missing := "pact:event:sha256:" + strings.Repeat("d", 64) + "#gone"
	incompleteBatch := []byte(`{"events":[{"local_id":"missing","kind":"observation","type":"widget.missing","subject":"widget-missing","schema_ref":"pact:core/widget/v1","payload":{},"evidence":[],"caused_by":["` + missing + `"],"supersedes":[],"tags":[]}]}`)
	if err := os.WriteFile(incompleteBatchPath, incompleteBatch, 0o644); err != nil {
		t.Fatal(err)
	}
	runJSON(t, []string{"commit", "--repo", repo, "--key-file", keyPath, "--events", incompleteBatchPath, "--namespace", "org/example/incomplete", "--json"})
	hexID := commit["object_id"].(string)[len("sha256:"):]
	path := filepath.Join(repo, ".pact", "objects", "sha256", hexID[:2], hexID[2:]+".json")
	if err := os.WriteFile(path, []byte(`{"bad":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runErrorJSON(t, []string{"verify", "--repo", repo, "--strict", "--json"}, 4)
	if result["details"] == nil || result["details"].(map[string]any)["objects"] == nil || result["details"].(map[string]any)["counts"] == nil {
		t.Fatalf("verify error JSON = %#v", result)
	}
	if result["details"].(map[string]any)["completeness"].(map[string]any)["status"] != "incomplete" {
		t.Fatalf("mixed invalid and incomplete verification = %#v", result)
	}
	result = runErrorJSON(t, []string{"commit", "--repo", repo, "--key-file", keyPath, "--events", batchPath, "--json"}, 4)
	if result["exit_code"] != float64(4) {
		t.Fatalf("commit error JSON = %#v", result)
	}
}

func TestStatusGoldenOutput(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		result statuspkg.Result
		golden string
	}{
		{name: "healthy/60", width: 60, result: statusGoldenResult(statuspkg.HealthHealthy, "current"), golden: "healthy-60.golden"},
		{name: "healthy/80", width: 80, result: statusGoldenResult(statuspkg.HealthHealthy, "current"), golden: "healthy-80.golden"},
		{name: "healthy/120", width: 120, result: statusGoldenResult(statuspkg.HealthHealthy, "current"), golden: "healthy-120.golden"},
		{name: "attention/80", width: 80, result: statusGoldenResult(statuspkg.HealthAttention, "stale"), golden: "attention-80.golden"},
		{name: "broken/80", width: 80, result: statusGoldenResult(statuspkg.HealthBroken, ""), golden: "broken-80.golden"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := emitStatusHuman(&output, test.result, false, test.width); err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "status", test.golden))
			if err != nil {
				t.Fatal(err)
			}
			if got := output.Bytes(); !bytes.Equal(got, want) {
				t.Fatalf("status output at width %d:\n%s", test.width, got)
			}
		})
	}
}

func TestColorStatusOutputPreservesPlainWordsAndOrder(t *testing.T) {
	result := statusGoldenResult(statuspkg.HealthHealthy, "current")
	var plain, colored bytes.Buffer
	if err := emitStatusHuman(&plain, result, false, 80); err != nil {
		t.Fatal(err)
	}
	if err := emitStatusHuman(&colored, result, true, 80); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colored.String(), "\x1b[") {
		t.Fatalf("forced color output has no ANSI sequence: %q", colored.String())
	}
	if got := stripANSI(colored.String()); got != plain.String() {
		t.Fatalf("ANSI-stripped output differs from plain output:\nplain:\n%s\nstripped:\n%s", plain.String(), got)
	}
}

func TestStatusHumanCountsEachLocalHead(t *testing.T) {
	result := statusGoldenResult(statuspkg.HealthHealthy, "current")
	result.Verification.Heads = map[string][]string{"org/example/widget": {"sha256:one", "sha256:two"}}
	var output bytes.Buffer
	if err := emitStatusHuman(&output, result, false, 80); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Heads 2") {
		t.Fatalf("status heads = %q", output.String())
	}
}

func TestStatusJSONRetainsStableSummary(t *testing.T) {
	repo := healthyOperatorRepository(t)
	var stdout, stderr bytes.Buffer
	code := runWithConfig([]string{"status", "--repo", repo, "--json"}, runConfig{Stdout: &stdout, Stderr: &stderr, Width: 80})
	if code != 0 {
		t.Fatalf("status JSON exit = %d, stderr=%q", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	verification, ok := result["verification"].(map[string]any)
	if !ok || result["operation"] != "status" || result["health"] != "healthy" ||
		result["default_namespace"] != "org/example/widget" || verification["strict"] != true || verification["ok"] != true ||
		result["next_action"] != nil {
		t.Fatalf("status JSON = %#v", result)
	}
}

func TestWriterStatusOutputFailuresMapToUnexpectedExit(t *testing.T) {
	tests := []struct {
		name string
		repo func(*testing.T) string
		args []string
	}{
		{name: "healthy human", repo: healthyOperatorRepository, args: []string{"status"}},
		{name: "healthy JSON", repo: healthyOperatorRepository, args: []string{"status", "--json"}},
		{name: "attention human", repo: staleOperatorRepository, args: []string{"status"}},
		{name: "attention JSON", repo: staleOperatorRepository, args: []string{"status", "--json"}},
		{name: "broken human", repo: brokenOperatorRepository, args: []string{"status"}},
		{name: "broken JSON", repo: brokenOperatorRepository, args: []string{"status", "--json"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := test.repo(t)
			args := append(append([]string{}, test.args...), "--repo", repo)
			if code := runWithConfig(args, runConfig{Stdout: closedOutput{}, Stderr: closedOutput{}, Width: 80}); code != exitUnexpectedError {
				t.Fatalf("status writer failure exit = %d, want %d", code, exitUnexpectedError)
			}
		})
	}
}

func stripANSI(value string) string {
	result := make([]byte, 0, len(value))
	for position := 0; position < len(value); {
		if value[position] == '\x1b' && position+1 < len(value) && value[position+1] == '[' {
			position += 2
			for position < len(value) && (value[position] < '@' || value[position] > '~') {
				position++
			}
			if position < len(value) {
				position++
			}
			continue
		}
		result = append(result, value[position])
		position++
	}
	return string(result)
}

func statusGoldenResult(health statuspkg.Health, indexState string) statuspkg.Result {
	result := statuspkg.Result{
		Health:           health,
		Repo:             "/work/pact",
		Store:            "/work/pact/.pact",
		DefaultNamespace: "org/example/widget",
		Verification: ledger.VerifyResult{
			OK:     health != statuspkg.HealthBroken,
			Strict: true,
			Counts: ledger.VerifyCounts{},
			Heads:  map[string][]string{},
			Completeness: ledger.Completeness{
				Scope:  "local_object_set",
				Status: "locally_closed",
			},
		},
	}
	if health == statuspkg.HealthAttention {
		result.NextAction = &statuspkg.NextAction{Reason: "indexed reads are not ready", Command: "pact index rebuild"}
	}
	if health != statuspkg.HealthBroken {
		coverage := "complete"
		if health == statuspkg.HealthAttention {
			coverage = "unavailable"
		}
		result.Index = &index.Status{Index: index.IndexInfo{State: indexState, Coverage: coverage}}
	}
	return result
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
