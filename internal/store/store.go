// ABOUTME: Manages PACT's initialized local store and immutable canonical objects.
// ABOUTME: Uses durable temporary writes and no-overwrite links for object admission.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"pact/internal/canonical"
)

const (
	formatName = "pact/store/v1"
	trustName  = "pact/trust/v1"
)

var (
	// ErrNotInitialized marks a missing or unsupported project store.
	ErrNotInitialized = errors.New("PACT store is not initialized")
	// ErrAlreadyInitialized marks an existing store that init must not replace.
	ErrAlreadyInitialized = errors.New("PACT store already exists")
	// ErrInvalidNamespace marks namespace input rejected before initialization.
	ErrInvalidNamespace = errors.New("invalid namespace")

	linkFile  = os.Link
	afterLink = func(string) error { return nil }
)

// Store identifies one initialized PACT store.
type Store struct {
	repo string
	dir  string
}

// Init creates an empty PACT store at repo.
func Init(repo, namespace string, now time.Time) (*Store, error) {
	if err := validateNamespace(namespace); err != nil {
		return nil, err
	}
	absRepo, err := resolveRepository(repo)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	st := &Store{repo: absRepo, dir: filepath.Join(absRepo, ".pact")}
	claim, err := os.OpenFile(filepath.Join(absRepo, ".pact.init.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("%w: initialization is already in progress", ErrAlreadyInitialized)
	}
	if err != nil {
		return nil, fmt.Errorf("claim store initialization: %w", err)
	}
	claimPath := claim.Name()
	defer os.Remove(claimPath)
	if err := claim.Close(); err != nil {
		return nil, fmt.Errorf("close store initialization claim: %w", err)
	}
	if info, err := os.Lstat(st.dir); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing symlinked PACT store: %s", st.dir)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("inspect store directory: %w", err)
	}
	if entries, err := os.ReadDir(st.dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%w: refusing to overwrite existing PACT store: %s", ErrAlreadyInitialized, st.dir)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("inspect store directory: %w", err)
	}
	for _, path := range []string{
		st.dir,
		filepath.Join(st.dir, "objects"),
		filepath.Join(st.dir, "objects", "sha256"),
		filepath.Join(st.dir, "index"),
		filepath.Join(st.dir, "refs"),
		filepath.Join(st.dir, "tmp"),
	} {
		if err := ensureRealDirectory(path); err != nil {
			return nil, fmt.Errorf("create store layout: %w", err)
		}
	}
	createdAt := now.UTC().Format(time.RFC3339)
	if err := st.WriteLocalJSON("format.json", map[string]any{
		"format":              formatName,
		"default_namespace":   namespace,
		"created_at":          createdAt,
		"canonicalization":    "pact-json-v1",
		"hash_algorithm":      "sha256",
		"signature_algorithm": "ed25519",
	}, 0o644); err != nil {
		return nil, err
	}
	if err := st.WriteLocalJSON("trust.json", map[string]any{"format": trustName, "roots": []any{}}, 0o644); err != nil {
		return nil, err
	}
	if err := atomicReplace(filepath.Join(st.dir, ".gitignore"), []byte("index/\ntmp/\nrefs/\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write store gitignore: %w", err)
	}
	return st, nil
}

// Open verifies and opens an initialized PACT store at repo.
func Open(repo string) (*Store, error) {
	absRepo, err := resolveRepository(repo)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	st := &Store{repo: absRepo, dir: filepath.Join(absRepo, ".pact")}
	raw, err := st.ReadLocal("format.json")
	if err != nil {
		return nil, fmt.Errorf("%w at %s; run 'pact init' first", ErrNotInitialized, st.dir)
	}
	var format map[string]any
	if err := decodeStrictJSON(raw, &format); err != nil || format["format"] != formatName {
		return nil, fmt.Errorf("%w or unsupported format at %s", ErrNotInitialized, filepath.Join(st.dir, "format.json"))
	}
	return st, nil
}

// Dir returns the absolute .pact directory path.
func (st *Store) Dir() string { return st.dir }

// ReadLocal reads one named mutable local configuration file.
func (st *Store) ReadLocal(name string) ([]byte, error) {
	path, err := st.localPath(name)
	if err != nil {
		return nil, err
	}
	if err := ensureExistingRealDirectory(st.dir); err != nil {
		return nil, err
	}
	if err := rejectSymlink(path); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read local file %s: %w", path, err)
	}
	return raw, nil
}

// WriteLocalJSON atomically replaces one named mutable local JSON file.
func (st *Store) WriteLocalJSON(name string, value any, mode fs.FileMode) error {
	path, err := st.localPath(name)
	if err != nil {
		return err
	}
	if err := ensureExistingRealDirectory(st.dir); err != nil {
		return err
	}
	if err := rejectSymlink(path); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode local JSON: %w", err)
	}
	raw = append(raw, '\n')
	if err := atomicReplace(path, raw, mode); err != nil {
		return fmt.Errorf("write local file %s: %w", path, err)
	}
	return nil
}

// PutCanonical encodes and admits one immutable canonical object.
func (st *Store) PutCanonical(value any) (objectID string, created bool, err error) {
	raw, err := canonical.Marshal(value)
	if err != nil {
		return "", false, fmt.Errorf("canonicalize object: %w", err)
	}
	objectID = canonical.Digest(raw)
	path, err := st.objectPath(objectID)
	if err != nil {
		return "", false, err
	}
	if err := st.ensureObjectDirectories(objectID, true); err != nil {
		return "", false, err
	}
	if err := rejectSymlink(path); err != nil {
		return "", false, err
	}
	if existing, readErr := os.ReadFile(path); readErr == nil {
		if !bytes.Equal(existing, raw) {
			return "", false, fmt.Errorf("content-address collision or corruption at %s", path)
		}
		return objectID, false, nil
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return "", false, fmt.Errorf("read canonical object: %w", readErr)
	}
	temporary, err := os.CreateTemp(filepath.Join(st.dir, "tmp"), "object.*.tmp")
	if err != nil {
		return "", false, fmt.Errorf("create object temporary file: %w", err)
	}
	tempPath := temporary.Name()
	defer func() {
		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) && err == nil {
			err = fmt.Errorf("remove object temporary file: %w", removeErr)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return "", false, fmt.Errorf("set object temporary mode: %w", err)
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return "", false, fmt.Errorf("write object temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", false, fmt.Errorf("sync object temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", false, fmt.Errorf("close object temporary file: %w", err)
	}
	if err := linkFile(tempPath, path); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return "", false, err
		}
		existing, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(existing, raw) {
			return "", false, fmt.Errorf("content-address collision or concurrent corruption at %s", path)
		}
	} else {
		created = true
		if err := syncDirectory(filepath.Dir(path)); err != nil {
			return "", false, fmt.Errorf("sync object directory: %w", err)
		}
	}
	if err := afterLink(path); err != nil {
		return "", false, err
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read persisted object: %w", err)
	}
	if !bytes.Equal(persisted, raw) || canonical.Digest(persisted) != objectID {
		return "", false, fmt.Errorf("post-write verification failed for %s", objectID)
	}
	return objectID, created, nil
}

// Get returns exact canonical bytes after checking the requested digest.
func (st *Store) Get(objectID string) ([]byte, error) {
	path, err := st.objectPath(objectID)
	if err != nil {
		return nil, err
	}
	if err := st.ensureObjectDirectories(objectID, false); err != nil {
		return nil, err
	}
	if err := rejectSymlink(path); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("object not found: %s", objectID)
	}
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}
	if canonical.Digest(raw) != objectID {
		return nil, fmt.Errorf("object digest mismatch at %s", path)
	}
	return raw, nil
}

func (st *Store) ensureObjectDirectories(objectID string, createTemp bool) error {
	hexDigest := strings.TrimPrefix(objectID, "sha256:")
	for _, path := range []string{st.dir, filepath.Join(st.dir, "objects"), filepath.Join(st.dir, "objects", "sha256")} {
		if err := ensureExistingRealDirectory(path); err != nil {
			return err
		}
	}
	prefix := filepath.Join(st.dir, "objects", "sha256", hexDigest[:2])
	if createTemp {
		if err := ensureRealDirectory(prefix); err != nil {
			return err
		}
	} else if err := ensureExistingRealDirectory(prefix); err != nil {
		return err
	}
	if createTemp {
		return ensureRealDirectory(filepath.Join(st.dir, "tmp"))
	}
	return nil
}

func (st *Store) localPath(name string) (string, error) {
	if name == "" || filepath.Base(name) != name {
		return "", fmt.Errorf("invalid local store filename")
	}
	return filepath.Join(st.dir, name), nil
}

func (st *Store) objectPath(objectID string) (string, error) {
	if !strings.HasPrefix(objectID, "sha256:") || len(objectID) != len("sha256:")+64 {
		return "", fmt.Errorf("invalid object ID: %q", objectID)
	}
	hexDigest := strings.TrimPrefix(objectID, "sha256:")
	for _, character := range hexDigest {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return "", fmt.Errorf("invalid object ID: %q", objectID)
		}
	}
	return filepath.Join(st.dir, "objects", "sha256", hexDigest[:2], hexDigest[2:]+".json"), nil
}

func validateNamespace(namespace string) error {
	if namespace == "" || len(namespace) > 512 || strings.HasPrefix(namespace, "/") || strings.HasSuffix(namespace, "/") || strings.Contains(namespace, "//") {
		return fmt.Errorf("%w: %q", ErrInvalidNamespace, namespace)
	}
	for index, character := range namespace {
		if index == 0 && !isASCIIAlphaNumeric(character) {
			return fmt.Errorf("%w: %q", ErrInvalidNamespace, namespace)
		}
		if !isASCIIAlphaNumeric(character) && character != '.' && character != '_' && character != '/' && character != '-' {
			return fmt.Errorf("%w: %q", ErrInvalidNamespace, namespace)
		}
	}
	for _, segment := range strings.Split(namespace, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("%w: %q", ErrInvalidNamespace, namespace)
		}
	}
	return nil
}

func isASCIIAlphaNumeric(character rune) bool {
	return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9')
}

func atomicReplace(path string, data []byte, mode fs.FileMode) (err error) {
	if err := ensureRealDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".")
	if err != nil {
		return err
	}
	tempPath := temporary.Name()
	defer func() {
		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) && err == nil {
			err = removeErr
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return ignoreUnsupportedDirectorySync(err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return ignoreUnsupportedDirectorySync(err)
	}
	return nil
}

func resolveRepository(repo string) (string, error) {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absRepo, 0o755); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absRepo)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("repository is not a directory")
	}
	return resolved, nil
}

func ensureRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(path, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlinked store path: %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("store path is not a directory: %s", path)
	}
	return nil
}

func ensureExistingRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlinked store path: %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("store path is not a directory: %s", path)
	}
	return nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlinked store path: %s", path)
	}
	return nil
}

func decodeStrictJSON(raw []byte, destination any) error {
	value, err := canonical.Parse(raw)
	if err != nil {
		return err
	}
	encoded, err := canonical.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, destination)
}

func ignoreUnsupportedDirectorySync(err error) error {
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) {
		return nil
	}
	return err
}
