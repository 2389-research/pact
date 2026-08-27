// ABOUTME: Generates and validates external PACT Ed25519 key files.
// ABOUTME: Keeps private key material owner-only and outside initialized project roots.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
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

	"golang.org/x/text/unicode/norm"
)

const keyFormat = "pact/key/v1"

var (
	syncKeyDirectory   = syncDirectory
	removeKeyTemporary = os.Remove
	closeKeyDirectory  = func(directory *os.File) error { return directory.Close() }
)

// ErrProjectKeyOutput marks a key path inside an initialized PACT project root.
var ErrProjectKeyOutput = errors.New("key output is within initialized project root")

// ErrIntegrity marks inconsistent identity bytes in an otherwise readable key file.
var ErrIntegrity = errors.New("key integrity failure")

// ErrSecretSafety marks a private signing key that violates filesystem safety rules.
var ErrSecretSafety = errors.New("signing key safety refusal")

// KeyFile is a verified external PACT identity file.
type KeyFile struct {
	Path      string
	Actor     string
	KeyID     string
	Public    ed25519.PublicKey
	Private   ed25519.PrivateKey
	CreatedAt time.Time
}

// GenerateStatus reports the publication outcome of key generation.
type GenerateStatus string

const (
	// GenerateCreated means the requested key file became visible.
	GenerateCreated GenerateStatus = "created"
	// GenerateConflict means generation refused an existing target.
	GenerateConflict GenerateStatus = "conflict"
)

// GenerateResult preserves a published key through later durability or cleanup errors.
type GenerateResult struct {
	Key    *KeyFile
	Status GenerateStatus
}

type encodedKeyFile struct {
	Format     string `json:"format"`
	Algorithm  string `json:"algorithm"`
	Actor      string `json:"actor"`
	KeyID      string `json:"key_id"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// GenerateKeyFile creates a new owner-only key file without replacing an existing file.
func GenerateKeyFile(path, actor string, now time.Time) (result GenerateResult, err error) {
	actor, err = NormalizeActor(actor)
	if err != nil {
		return result, err
	}
	absPath, err := resolveOutputPath(path)
	if err != nil {
		return result, fmt.Errorf("resolve key file path: %w", err)
	}
	if initializedProjectAncestor(filepath.Dir(absPath)) {
		return result, fmt.Errorf("%w", ErrProjectKeyOutput)
	}
	if _, err := os.Lstat(absPath); err == nil {
		result.Status = GenerateConflict
		return result, fmt.Errorf("refusing to overwrite existing key file %s: %w", absPath, fs.ErrExist)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return result, fmt.Errorf("inspect key output: %w", err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return result, fmt.Errorf("generate Ed25519 key: %w", err)
	}
	keyID, err := KeyID(public)
	if err != nil {
		return result, err
	}
	createdAt := now.UTC().Format(time.RFC3339)
	encoded := encodedKeyFile{
		Format:     keyFormat,
		Algorithm:  "ed25519",
		Actor:      actor,
		KeyID:      keyID,
		PublicKey:  base64.RawURLEncoding.EncodeToString(public),
		PrivateKey: base64.RawURLEncoding.EncodeToString(private.Seed()),
		CreatedAt:  createdAt,
	}
	// #nosec G117 -- private_key is the required wire field; the file is created mode 0600 and rechecked before signing.
	raw, err := json.MarshalIndent(encoded, "", "  ")
	if err != nil {
		return result, fmt.Errorf("encode key file: %w", err)
	}
	key := &KeyFile{Path: absPath, Actor: actor, KeyID: keyID, Public: public, Private: private, CreatedAt: now.UTC()}
	published, err := writeNewFile(absPath, append(raw, '\n'), 0o600)
	if published {
		result = GenerateResult{Key: key, Status: GenerateCreated}
	}
	if err != nil {
		if !published && errors.Is(err, fs.ErrExist) {
			result.Status = GenerateConflict
		}
		return result, err
	}
	return result, nil
}

// LoadKeyFile reads and cross-checks public, private, and key-ID values.
func LoadKeyFile(path string, requirePrivate bool) (*KeyFile, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve key file path: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return nil, fmt.Errorf("resolve key file path %s: %w", absPath, err)
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("inspect key file %s: %w", resolvedPath, err)
	}
	if err := validateKeyFileMode(info, resolvedPath, requirePrivate); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	var encoded encodedKeyFile
	if err := decodeStrictJSON(raw, &encoded); err != nil {
		return nil, fmt.Errorf("unsupported or malformed PACT key file: %s", resolvedPath)
	}
	if encoded.Format != keyFormat || encoded.Algorithm != "ed25519" {
		return nil, fmt.Errorf("unsupported or malformed PACT key file: %s", resolvedPath)
	}
	actor, err := NormalizeActor(encoded.Actor)
	if err != nil {
		return nil, fmt.Errorf("key file has invalid actor label: %s", resolvedPath)
	}
	public, err := decodeBase64URL(encoded.PublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("key file has invalid Ed25519 public key: %s", resolvedPath)
	}
	keyID, err := KeyID(ed25519.PublicKey(public))
	if err != nil {
		return nil, err
	}
	if encoded.KeyID != keyID {
		return nil, fmt.Errorf("%w: key ID does not match public key in %s", ErrIntegrity, resolvedPath)
	}
	private, err := loadPrivateKey(encoded.PrivateKey, ed25519.PublicKey(public), resolvedPath, requirePrivate)
	if err != nil {
		return nil, err
	}
	createdAt, err := time.Parse(time.RFC3339, encoded.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("key file has invalid creation time: %s", resolvedPath)
	}
	return &KeyFile{Path: resolvedPath, Actor: actor, KeyID: keyID, Public: ed25519.PublicKey(public), Private: private, CreatedAt: createdAt}, nil
}

func validateKeyFileMode(info fs.FileInfo, path string, requirePrivate bool) error {
	if !info.Mode().IsRegular() {
		if requirePrivate {
			return fmt.Errorf("%w: signing key must be a regular file: %s", ErrSecretSafety, path)
		}
		return fmt.Errorf("key file must be a regular file: %s", path)
	}
	if requirePrivate && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: signing key has group or other permission bits: %s", ErrSecretSafety, path)
	}
	return nil
}

func loadPrivateKey(encoded string, public ed25519.PublicKey, path string, required bool) (ed25519.PrivateKey, error) {
	if encoded == "" {
		if required {
			return nil, fmt.Errorf("private key is required in %s", path)
		}
		return nil, nil
	}
	seed, err := decodeBase64URL(encoded)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("key file has invalid Ed25519 private key: %s", path)
	}
	private := ed25519.NewKeyFromSeed(seed)
	derived, ok := private.Public().(ed25519.PublicKey)
	if !ok || !derived.Equal(public) {
		return nil, fmt.Errorf("%w: private/public key mismatch in %s", ErrIntegrity, path)
	}
	return private, nil
}

// LoadPublicKey reads a public identity without applying private-key containment or mode rules.
func LoadPublicKey(path string) (*KeyFile, error) {
	return LoadKeyFile(path, false)
}

// ValidateSigningKeyPath validates a planned key path without reading or writing key bytes.
func ValidateSigningKeyPath(path, projectRoot string) (string, error) {
	lexicalPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve key file path: %w", err)
	}
	lexicalRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	resolvedRoot, err := resolveOutputPath(projectRoot)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	if _, statErr := os.Lstat(lexicalRoot); statErr == nil {
		resolvedRoot, err = filepath.EvalSymlinks(lexicalRoot)
		if err != nil {
			return "", fmt.Errorf("resolve project root: %w", err)
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return "", fmt.Errorf("inspect project root: %w", statErr)
	}
	plannedPath, err := resolveOutputPath(lexicalPath)
	if err != nil {
		return "", fmt.Errorf("resolve key file path: %w", err)
	}
	resolvedPath := plannedPath
	if _, statErr := os.Lstat(lexicalPath); statErr == nil {
		resolvedPath, err = filepath.EvalSymlinks(lexicalPath)
		if err != nil {
			return "", fmt.Errorf("resolve key file path: %w", err)
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return "", fmt.Errorf("inspect key file: %w", statErr)
	}
	if pathWithin(lexicalPath, lexicalRoot) || pathWithin(plannedPath, resolvedRoot) || pathWithin(resolvedPath, resolvedRoot) {
		return "", fmt.Errorf("%w: signing key is within project root: %s", ErrSecretSafety, lexicalPath)
	}
	return lexicalPath, nil
}

// NormalizeActor returns one trimmed NFC actor label with the protocol length bound.
func NormalizeActor(actor string) (string, error) {
	normalized := norm.NFC.String(strings.TrimSpace(actor))
	if normalized == "" || len([]rune(normalized)) > 255 {
		return "", fmt.Errorf("actor label must be 1-255 characters")
	}
	return normalized, nil
}

// LoadSigningKey loads a private key only when its lexical and resolved paths stay outside projectRoot.
func LoadSigningKey(path, projectRoot string) (*KeyFile, error) {
	validatedPath, err := ValidateSigningKeyPath(path, projectRoot)
	if err != nil {
		return nil, err
	}
	return LoadKeyFile(validatedPath, true)
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func initializedProjectAncestor(directory string) bool {
	for {
		if info, err := os.Stat(filepath.Join(directory, ".pact", "format.json")); err == nil && !info.IsDir() {
			return true
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return false
		}
		directory = parent
	}
}

func decodeBase64URL(value string) ([]byte, error) {
	if value == "" {
		return nil, errors.New("empty base64url")
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' {
			return nil, errors.New("invalid base64url")
		}
	}
	return base64.RawURLEncoding.DecodeString(value)
}

func writeNewFile(path string, data []byte, mode fs.FileMode) (published bool, err error) {
	// #nosec G301 -- key parents are ordinary user-selected directories; the key file itself is mode 0600.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".")
	if err != nil {
		return false, err
	}
	tempPath := temporary.Name()
	defer func() {
		if removeErr := removeKeyTemporary(tempPath); removeErr != nil && !onlyNotExistLeaves(removeErr) {
			if err == nil {
				err = removeErr
			} else {
				err = errors.Join(err, removeErr)
			}
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return false, err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return false, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return false, err
	}
	if err := temporary.Close(); err != nil {
		return false, err
	}
	if err := os.Link(tempPath, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, fmt.Errorf("refusing to overwrite existing key file %s: %w", path, fs.ErrExist)
		}
		return false, err
	}
	published = true
	if err := syncKeyDirectory(filepath.Dir(path)); err != nil {
		return published, err
	}
	return published, nil
}

func onlyNotExistLeaves(err error) bool {
	if err == nil {
		return false
	}
	if multiple, ok := err.(interface{ Unwrap() []error }); ok {
		causes := multiple.Unwrap()
		if len(causes) == 0 {
			return errors.Is(err, fs.ErrNotExist)
		}
		for _, cause := range causes {
			if !onlyNotExistLeaves(cause) {
				return false
			}
		}
		return true
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		cause := single.Unwrap()
		if cause != nil {
			return onlyNotExistLeaves(cause)
		}
	}
	return errors.Is(err, fs.ErrNotExist)
}

func syncDirectory(path string) (err error) {
	// #nosec G304 -- path is the already resolved parent directory of the caller-validated key path.
	directory, err := os.Open(path)
	if err != nil {
		return ignoreUnsupportedDirectorySync(err)
	}
	defer func() {
		closeErr := closeKeyDirectory(directory)
		if closeErr == nil {
			return
		}
		if err == nil {
			err = closeErr
			return
		}
		err = errors.Join(err, closeErr)
	}()
	return ignoreUnsupportedDirectorySync(directory.Sync())
}

func resolveOutputPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(absPath)
	var missing []string
	for {
		info, err := os.Lstat(directory)
		if err == nil {
			if !info.IsDir() && info.Mode()&fs.ModeSymlink == 0 {
				return "", fmt.Errorf("key output parent is not a directory")
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", err
		}
		missing = append([]string{filepath.Base(directory)}, missing...)
		directory = parent
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{resolved}, append(missing, filepath.Base(absPath))...)...), nil
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
	if err == nil || errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) {
		return nil
	}
	return err
}
