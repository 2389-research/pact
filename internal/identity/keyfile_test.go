// ABOUTME: Verifies external PACT key files protect and validate Ed25519 identity material.
// ABOUTME: Uses real files to cover permissions, overwrite refusal, and project-store exclusion.
package identity

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pact/internal/store"
)

func TestSafeKeyDiagnosticOmitsKeyBytes(t *testing.T) {
	publicMarker := "distinctive-public-byte-marker"
	privateMarker := "distinctive-private-byte-marker"
	result := GenerateResult{Status: GenerateCreated, Key: &KeyFile{
		Path:    "/safe/alice.key.json",
		KeyID:   "ed25519:sha256:safe-id",
		Public:  []byte(publicMarker),
		Private: []byte(privateMarker),
	}}

	diagnostic := safeKeyDiagnostic(result.Status, result.Key)
	if diagnosticContainsKeyBytes(diagnostic, publicMarker, privateMarker) {
		t.Fatal("safe diagnostic included key bytes")
	}
	for _, safeField := range []string{"created", "safe-id", "/safe/alice.key.json", "public_len=30", "private_len=31"} {
		if !strings.Contains(diagnostic, safeField) {
			t.Fatalf("safe diagnostic omitted field %q", safeField)
		}
	}
}

func TestNormalizeActorCanonicalizesAndValidates(t *testing.T) {
	got, err := NormalizeActor(" \te\u0301\n")
	if err != nil {
		t.Fatalf("NormalizeActor() error = %v", err)
	}
	if got != "é" {
		t.Fatalf("NormalizeActor() = %q, want NFC-trimmed actor", got)
	}
	for name, actor := range map[string]string{
		"empty":          " \t\n",
		"over 255 runes": strings.Repeat("界", 256),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeActor(actor); err == nil {
				t.Fatal("NormalizeActor() error = nil, want refusal")
			}
		})
	}
}

func TestValidateSigningKeyPathAcceptsExternalTargetsWithoutWriting(t *testing.T) {
	projectRoot := t.TempDir()
	externalRoot := t.TempDir()
	missing := filepath.Join(externalRoot, "missing", "alice.key.json")
	wantMissing, err := filepath.Abs(missing)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ValidateSigningKeyPath(missing, projectRoot)
	if err != nil || got != wantMissing || !filepath.IsAbs(got) {
		t.Fatalf("ValidateSigningKeyPath(missing) = (%q, %v), want (%q, nil)", got, err, wantMissing)
	}
	if _, err := os.Lstat(filepath.Dir(missing)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ValidateSigningKeyPath() created missing parent: %v", err)
	}

	existing := filepath.Join(externalRoot, "existing.key.json")
	keyBytes := []byte("private-key-bytes-must-not-appear")
	if err := os.WriteFile(existing, keyBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	wantExisting, err := filepath.Abs(existing)
	if err != nil {
		t.Fatal(err)
	}
	got, err = ValidateSigningKeyPath(existing, projectRoot)
	if err != nil || got != wantExisting {
		t.Fatalf("ValidateSigningKeyPath(existing) = (%q, %v), want (%q, nil)", got, err, wantExisting)
	}
}

func TestInspectSecretSafetyErrorClassifiesTaintedErrorWithoutEchoingIt(t *testing.T) {
	secret := "private-key-bytes-must-not-appear"
	tainted := fmt.Errorf("%w: %s", ErrSecretSafety, secret)
	leaked, correctIdentity := inspectSecretSafetyError(tainted, secret)
	if !leaked || !correctIdentity {
		t.Fatal("tainted secret-safety error was not classified")
	}
	leaked, correctIdentity = inspectSecretSafetyError(ErrSecretSafety, secret)
	if leaked || !correctIdentity {
		t.Fatal("safe secret-safety error was misclassified")
	}
}

func TestValidateSigningKeyPathRejectsProjectContainmentWithoutLeakingKeyBytes(t *testing.T) {
	projectRoot := t.TempDir()
	externalRoot := t.TempDir()
	link := filepath.Join(externalRoot, "project-link")
	if err := os.Symlink(projectRoot, link); err != nil {
		t.Fatal(err)
	}
	secret := "private-key-bytes-must-not-appear"
	existing := filepath.Join(projectRoot, "existing.key.json")
	if err := os.WriteFile(existing, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"lexical":           filepath.Join(projectRoot, "missing", "alice.key.json"),
		"resolved missing":  filepath.Join(link, "missing", "alice.key.json"),
		"resolved existing": filepath.Join(link, "existing.key.json"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateSigningKeyPath(path, projectRoot)
			leaked, correctIdentity := inspectSecretSafetyError(err, secret)
			if leaked {
				t.Fatal("ValidateSigningKeyPath() error leaked key bytes")
			}
			if !correctIdentity {
				t.Fatal("ValidateSigningKeyPath() did not return a secret-safety refusal")
			}
		})
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ValidateSigningKeyPath() created missing project parent: %v", err)
	}
}

func TestGenerateKeyFileCreatesOwnerOnlyVerifiedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice.key.json")
	result, err := GenerateKeyFile(path, " Alice ", time.Date(2026, 8, 23, 12, 34, 56, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateKeyFile() error = %v", err)
	}
	key := result.Key
	if result.Status != GenerateCreated {
		t.Fatalf("GenerateKeyFile() status = %q, want created", result.Status)
	}
	if key.Actor != "Alice" || key.KeyID == "" || len(key.Public) != 32 || len(key.Private) != 64 {
		t.Fatalf("generated key = %s", safeKeyDiagnostic(result.Status, key))
	}
	if mode := fileMode(t, path); mode != 0o600 {
		t.Fatalf("key mode = %#o, want 0600", mode)
	}
	loaded, err := LoadKeyFile(path, true)
	if err != nil || loaded.KeyID != key.KeyID {
		t.Fatalf("LoadKeyFile() = (%s, %v)", safeKeyDiagnostic("", loaded), err)
	}
}

func TestGenerateKeyFileReportsCreatedAfterDirectorySyncFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice.key.json")
	fault := errors.New("injected key directory sync failure")
	oldSync := syncKeyDirectory
	syncKeyDirectory = func(string) error { return fault }
	t.Cleanup(func() { syncKeyDirectory = oldSync })

	result, err := GenerateKeyFile(path, "Alice", time.Now())
	if !errors.Is(err, fault) {
		t.Fatalf("GenerateKeyFile() error = %v, want injected fault", err)
	}
	assertPublishedGeneratedKey(t, result, path)
}

func TestGenerateKeyFileReportsCreatedAfterDirectoryCloseFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice.key.json")
	fault := errors.New("injected key directory close failure")
	originalClose := closeKeyDirectory
	closeKeyDirectory = func(directory *os.File) error {
		if closeErr := directory.Close(); closeErr != nil {
			return errors.Join(closeErr, fault)
		}
		return fault
	}
	t.Cleanup(func() { closeKeyDirectory = originalClose })

	result, err := GenerateKeyFile(path, "Alice", time.Now())
	if !errors.Is(err, fault) {
		t.Fatalf("GenerateKeyFile() error = %v, want injected close fault", err)
	}
	assertPublishedGeneratedKey(t, result, path)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	closeKeyDirectory = originalClose
	rerun, err := GenerateKeyFile(path, "Alice", time.Now())
	if rerun.Status != GenerateConflict || !errors.Is(err, os.ErrExist) {
		t.Fatalf("GenerateKeyFile() rerun = (%s, %v), want existing-key convergence", safeKeyDiagnostic(rerun.Status, rerun.Key), err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatal("clean key rerun changed published bytes")
	}
}

func TestGenerateKeyFileReportsCreatedAfterTemporaryCleanupFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice.key.json")
	fault := errors.New("injected key temporary cleanup failure")
	oldRemove := removeKeyTemporary
	removeKeyTemporary = func(string) error { return fault }
	t.Cleanup(func() { removeKeyTemporary = oldRemove })

	result, err := GenerateKeyFile(path, "Alice", time.Now())
	if !errors.Is(err, fault) {
		t.Fatalf("GenerateKeyFile() error = %v, want injected fault", err)
	}
	assertPublishedGeneratedKey(t, result, path)
}

func TestGenerateKeyFilePreservesMixedNotExistTemporaryCleanupFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice.key.json")
	cleanupFault := errors.New("injected key temporary cleanup failure")
	originalRemove := removeKeyTemporary
	temporaryPath := ""
	removeKeyTemporary = func(path string) error {
		temporaryPath = path
		return errors.Join(fs.ErrNotExist, cleanupFault)
	}
	t.Cleanup(func() {
		removeKeyTemporary = originalRemove
		if temporaryPath != "" {
			_ = os.Remove(temporaryPath)
		}
	})

	result, err := GenerateKeyFile(path, "Alice", time.Now())
	if !errors.Is(err, cleanupFault) {
		t.Fatalf("GenerateKeyFile() error = %v, want mixed cleanup fault", err)
	}
	assertPublishedGeneratedKey(t, result, path)
}

func TestGenerateKeyFilePreservesDirectorySyncAndTemporaryCleanupFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice.key.json")
	syncFault := errors.New("injected key directory sync failure")
	cleanupFault := errors.New("injected key temporary cleanup failure")
	oldSync := syncKeyDirectory
	oldRemove := removeKeyTemporary
	syncKeyDirectory = func(string) error { return syncFault }
	removeKeyTemporary = func(string) error { return cleanupFault }
	t.Cleanup(func() {
		syncKeyDirectory = oldSync
		removeKeyTemporary = oldRemove
	})

	result, err := GenerateKeyFile(path, "Alice", time.Now())
	if !errors.Is(err, syncFault) || !errors.Is(err, cleanupFault) {
		t.Fatalf("GenerateKeyFile() error = %v, want sync and cleanup faults", err)
	}
	assertPublishedGeneratedKey(t, result, path)
}

func TestGenerateKeyFileClassifiesOnlyExistingTargetAsConflict(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "alice.key.json")
	original := []byte("existing target stays byte-for-byte unchanged")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := GenerateKeyFile(path, "Alice", time.Now())
	if err == nil || result.Status != GenerateConflict || result.Key != nil {
		t.Fatalf("GenerateKeyFile(existing) = (%s, %v), want conflict without key", safeKeyDiagnostic(result.Status, result.Key), err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("existing target = (%q, %v), want original bytes", got, readErr)
	}

	blockedParent := filepath.Join(directory, "blocked")
	if err := os.WriteFile(blockedParent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = GenerateKeyFile(filepath.Join(blockedParent, "alice.key.json"), "Alice", time.Now())
	if err == nil || result.Status == GenerateConflict {
		t.Fatalf("GenerateKeyFile(random I/O failure) = (%s, %v), want non-conflict error", safeKeyDiagnostic(result.Status, result.Key), err)
	}
}

func assertPublishedGeneratedKey(t *testing.T, result GenerateResult, path string) {
	t.Helper()
	if result.Status != GenerateCreated || result.Key == nil {
		t.Fatalf("GenerateKeyFile() result = %s, want created key", safeKeyDiagnostic(result.Status, result.Key))
	}
	if mode := fileMode(t, path); mode != 0o600 {
		t.Fatalf("published key mode = %#o, want 0600", mode)
	}
	loaded, err := LoadKeyFile(path, true)
	if err != nil || loaded.KeyID != result.Key.KeyID {
		t.Fatalf("LoadKeyFile() = (%s, %v), want published key %s", safeKeyDiagnostic("", loaded), err, result.Key.KeyID)
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
	if _, err := LoadKeyFile(path, true); err == nil || !strings.Contains(err.Error(), "private/public") || !errors.Is(err, ErrIntegrity) {
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
	if _, err := LoadKeyFile(path, true); err == nil || !strings.Contains(err.Error(), "key ID") || !errors.Is(err, ErrIntegrity) {
		t.Fatalf("LoadKeyFile() error = %v, want key ID validation failure", err)
	}
}

func TestLoadSigningKeyEnforcesExternalRegularOwnerOnlyFile(t *testing.T) {
	repo := t.TempDir()
	result, err := store.Init(repo, "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st := result.Store
	external := filepath.Join(t.TempDir(), "alice.key.json")
	if _, err := GenerateKeyFile(external, "Alice", time.Now()); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(repo, "alice.key.json")
	raw, err := os.ReadFile(external)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	outsideLinkToInside := filepath.Join(t.TempDir(), "inside-link.key.json")
	if err := os.Symlink(inside, outsideLinkToInside); err != nil {
		t.Fatal(err)
	}
	insideLinkToOutside := filepath.Join(repo, "outside-link.key.json")
	if err := os.Symlink(external, insideLinkToOutside); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "key-directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	tooOpen := filepath.Join(t.TempDir(), "open.key.json")
	if err := os.WriteFile(tooOpen, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"direct inside":            inside,
		"resolved target inside":   outsideLinkToInside,
		"lexical path inside":      insideLinkToOutside,
		"non-regular final target": directory,
		"group-readable":           tooOpen,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSigningKey(path, st.Root()); !errors.Is(err, ErrSecretSafety) {
				t.Fatalf("LoadSigningKey(%q) error = %v, want secret-safety refusal", path, err)
			}
		})
	}

	outsideLink := filepath.Join(t.TempDir(), "outside-link.key.json")
	if err := os.Symlink(external, outsideLink); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSigningKey(outsideLink, st.Root())
	resolvedExternal, resolveErr := filepath.EvalSymlinks(external)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err != nil || loaded.Path != resolvedExternal {
		t.Fatalf("LoadSigningKey(outside symlink) = (%s, %v), want resolved external key", safeKeyDiagnostic("", loaded), err)
	}
}

func TestLoadPublicKeyAllowsReadablePublicOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alice.public.json")
	generatedPath := filepath.Join(t.TempDir(), "alice.key.json")
	if _, err := GenerateKeyFile(generatedPath, "Alice", time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	delete(value, "private_key")
	publicOnly, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, publicOnly, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPublicKey(path)
	resolvedPath, resolveErr := filepath.EvalSymlinks(path)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err != nil || len(loaded.Private) != 0 || loaded.Path != resolvedPath {
		t.Fatalf("LoadPublicKey() = (%s, %v)", safeKeyDiagnostic("", loaded), err)
	}
}

func safeKeyDiagnostic(status GenerateStatus, key *KeyFile) string {
	if key == nil {
		return fmt.Sprintf("status=%q key_nil=true", status)
	}
	return fmt.Sprintf(
		"status=%q key_nil=false key_id=%q path=%q public_len=%d private_len=%d",
		status,
		key.KeyID,
		key.Path,
		len(key.Public),
		len(key.Private),
	)
}

func diagnosticContainsKeyBytes(diagnostic, publicMarker, privateMarker string) bool {
	return strings.Contains(diagnostic, publicMarker) || strings.Contains(diagnostic, privateMarker)
}

func inspectSecretSafetyError(err error, secret string) (leaked, correctIdentity bool) {
	if err == nil {
		return false, false
	}
	return strings.Contains(err.Error(), secret), errors.Is(err, ErrSecretSafety)
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
