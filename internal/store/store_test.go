// ABOUTME: Exercises PACT's immutable, content-addressed object store on real filesystems.
// ABOUTME: Covers initialization, collision safety, durability checks, and failure cleanup.
package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pact/internal/canonical"
)

func TestInitCreatesReferenceLayout(t *testing.T) {
	repo := t.TempDir()
	now := time.Date(2026, 8, 23, 12, 34, 56, 0, time.UTC)
	st, err := Init(repo, "org/example/widget", now)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if got, want := st.Dir(), filepath.Join(repo, ".pact"); got != want {
		t.Fatalf("store directory = %q, want %q", got, want)
	}
	for _, name := range []string{"objects/sha256", "index", "refs", "tmp"} {
		info, err := os.Stat(filepath.Join(repo, ".pact", name))
		if err != nil || !info.IsDir() {
			t.Fatalf("layout directory %q: info=%v err=%v", name, info, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(repo, ".pact", ".gitignore")); err != nil || string(got) != "index/\ntmp/\nrefs/\n" {
		t.Fatalf(".gitignore = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(repo, ".pact", "format.json")); err != nil || !strings.Contains(string(got), "\"format\": \"pact/store/v1\"") || !strings.Contains(string(got), "\"created_at\": \"2026-08-23T12:34:56Z\"") {
		t.Fatalf("format.json = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(repo, ".pact", "trust.json")); err != nil || string(got) != "{\n  \"format\": \"pact/trust/v1\",\n  \"roots\": []\n}\n" {
		t.Fatalf("trust.json = %q, err=%v", got, err)
	}
}

func TestInitRefusesExistingStore(t *testing.T) {
	repo := t.TempDir()
	if _, err := Init(repo, "org/example/widget", time.Now()); err != nil {
		t.Fatalf("first Init() error = %v", err)
	}
	if _, err := Init(repo, "org/example/widget", time.Now()); err == nil {
		t.Fatal("second Init() error = nil, want refusal")
	}
}

func TestPutCanonicalIsIdempotent(t *testing.T) {
	st := testStore(t)
	value := map[string]any{"kind": "test", "count": 1}
	objectID, created, err := st.PutCanonical(value)
	if err != nil || !created {
		t.Fatalf("first PutCanonical() = (%q, %v, %v), want created object", objectID, created, err)
	}
	againID, created, err := st.PutCanonical(value)
	if err != nil || created || againID != objectID {
		t.Fatalf("second PutCanonical() = (%q, %v, %v), want (%q, false, nil)", againID, created, err, objectID)
	}
}

func TestPutCanonicalRefusesDifferentBytesAtSameDigestPath(t *testing.T) {
	st := testStore(t)
	value := map[string]any{"kind": "test"}
	raw, err := canonical.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	objectID := canonical.Digest(raw)
	path := objectFile(st, objectID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"kind":"tampered"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutCanonical(value); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("PutCanonical() error = %v, want collision refusal", err)
	}
}

func TestPutCanonicalVerifiesPersistedHash(t *testing.T) {
	st := testStore(t)
	original := afterLink
	afterLink = func(path string) error {
		return os.WriteFile(path, []byte(`{"kind":"tampered"}`), 0o644)
	}
	t.Cleanup(func() { afterLink = original })
	if _, _, err := st.PutCanonical(map[string]any{"kind": "test"}); err == nil || !strings.Contains(err.Error(), "post-write") {
		t.Fatalf("PutCanonical() error = %v, want post-write verification failure", err)
	}
}

func TestPutCanonicalLeavesNoObjectAfterPreLinkFailure(t *testing.T) {
	st := testStore(t)
	original := linkFile
	linkFile = func(_, _ string) error { return errors.New("injected pre-link failure") }
	t.Cleanup(func() { linkFile = original })
	value := map[string]any{"kind": "test"}
	raw, err := canonical.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutCanonical(value); err == nil || !strings.Contains(err.Error(), "injected pre-link") {
		t.Fatalf("PutCanonical() error = %v, want injected failure", err)
	}
	if _, err := os.Stat(objectFile(st, canonical.Digest(raw))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canonical object exists after failed admission: err=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(st.Dir(), "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("tmp contents after failed admission = %v, err=%v", entries, err)
	}
}

func TestGetRejectsTamperedObject(t *testing.T) {
	st := testStore(t)
	objectID, _, err := st.PutCanonical(map[string]any{"kind": "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectFile(st, objectID), []byte(`{"kind":"tampered"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(objectID); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("Get() error = %v, want digest mismatch", err)
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Init(t.TempDir(), "org/example/widget", time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func objectFile(st *Store, objectID string) string {
	hexDigest := strings.TrimPrefix(objectID, "sha256:")
	return filepath.Join(st.Dir(), "objects", "sha256", hexDigest[:2], hexDigest[2:]+".json")
}
