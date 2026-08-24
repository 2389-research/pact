// ABOUTME: Verifies external PACT key files protect and validate Ed25519 identity material.
// ABOUTME: Uses real files to cover permissions, overwrite refusal, and project-store exclusion.
package identity

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pact/internal/store"
)

func TestGenerateKeyFileCreatesOwnerOnlyVerifiedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice.key.json")
	key, err := GenerateKeyFile(path, " Alice ", time.Date(2026, 8, 23, 12, 34, 56, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateKeyFile() error = %v", err)
	}
	if key.Actor != "Alice" || key.KeyID == "" || len(key.Public) != 32 || len(key.Private) != 64 {
		t.Fatalf("generated key = %#v", key)
	}
	if mode := fileMode(t, path); mode != 0o600 {
		t.Fatalf("key mode = %#o, want 0600", mode)
	}
	loaded, err := LoadKeyFile(path, true)
	if err != nil || loaded.KeyID != key.KeyID {
		t.Fatalf("LoadKeyFile() = (%#v, %v)", loaded, err)
	}
}

func TestGenerateKeyFileRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice.key.json")
	if _, err := GenerateKeyFile(path, "Alice", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateKeyFile(path, "Alice", time.Now()); err == nil {
		t.Fatal("GenerateKeyFile() error = nil, want overwrite refusal")
	}
}

func TestGenerateKeyFileRejectsInitializedProjectRoot(t *testing.T) {
	repo := t.TempDir()
	if _, err := store.Init(repo, "org/example/widget", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateKeyFile(filepath.Join(repo, "keys", "alice.key.json"), "Alice", time.Now()); err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("GenerateKeyFile() error = %v, want project-root refusal", err)
	}
}

func TestGenerateKeyFileRejectsSymlinkedOutputWithinProject(t *testing.T) {
	repo := t.TempDir()
	if _, err := store.Init(repo, "org/example/widget", time.Now()); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "keys")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateKeyFile(filepath.Join(link, "alice.key.json"), "Alice", time.Now()); err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("GenerateKeyFile() error = %v, want symlink containment refusal", err)
	}
}

func TestLoadKeyFileRejectsNonStrictJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice.key.json")
	if _, err := GenerateKeyFile(path, "Alice", time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, malformed := range map[string][]byte{
		"duplicate":     bytes.Replace(raw, []byte(`"format": "pact/key/v1"`), []byte(`"format": "bad", "format": "pact/key/v1"`), 1),
		"nfc collision": append([]byte("{\"e\\u0301\":1,\"é\":2,"), raw[1:]...),
		"BOM":           append([]byte{0xef, 0xbb, 0xbf}, raw...),
		"trailing":      append(append([]byte{}, raw...), []byte("{}")...),
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, malformed, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadKeyFile(path, true); err == nil {
				t.Fatal("LoadKeyFile() error = nil, want strict JSON refusal")
			}
		})
	}
}

func TestLoadKeyFileRejectsPrivatePublicMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice.key.json")
	if _, err := GenerateKeyFile(path, "Alice", time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	public := make([]byte, 32)
	value["public_key"] = base64.RawURLEncoding.EncodeToString(public)
	keyID, err := KeyID(public)
	if err != nil {
		t.Fatal(err)
	}
	value["key_id"] = keyID
	updated, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyFile(path, true); err == nil || !strings.Contains(err.Error(), "private/public") {
		t.Fatalf("LoadKeyFile() error = %v, want validation failure", err)
	}
}

func TestLoadKeyFileRejectsKeyIDMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice.key.json")
	if _, err := GenerateKeyFile(path, "Alice", time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	value["key_id"] = "ed25519:sha256:" + strings.Repeat("0", 64)
	updated, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyFile(path, true); err == nil || !strings.Contains(err.Error(), "key ID") {
		t.Fatalf("LoadKeyFile() error = %v, want key ID validation failure", err)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
