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
	"time"

	"golang.org/x/text/unicode/norm"
)

const keyFormat = "pact/key/v1"

// ErrProjectKeyOutput marks a key path inside an initialized PACT project root.
var ErrProjectKeyOutput = errors.New("key output is within initialized project root")

// KeyFile is a verified external PACT identity file.
type KeyFile struct {
	Actor     string
	KeyID     string
	Public    ed25519.PublicKey
	Private   ed25519.PrivateKey
	CreatedAt time.Time
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
func GenerateKeyFile(path, actor string, now time.Time) (*KeyFile, error) {
	actor = norm.NFC.String(strings.TrimSpace(actor))
	if actor == "" || len([]rune(actor)) > 255 {
		return nil, fmt.Errorf("actor label must be 1-255 characters")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve key file path: %w", err)
	}
	if initializedProjectAncestor(filepath.Dir(absPath)) {
		return nil, fmt.Errorf("%w", ErrProjectKeyOutput)
	}
	if _, err := os.Lstat(absPath); err == nil {
		return nil, fmt.Errorf("refusing to overwrite existing key file: %s", absPath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("inspect key output: %w", err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate Ed25519 key: %w", err)
	}
	keyID, err := KeyID(public)
	if err != nil {
		return nil, err
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
	raw, err := json.MarshalIndent(encoded, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode key file: %w", err)
	}
	if err := writeNewFile(absPath, append(raw, '\n'), 0o600); err != nil {
		return nil, err
	}
	return &KeyFile{Actor: actor, KeyID: keyID, Public: public, Private: private, CreatedAt: now.UTC()}, nil
}

// LoadKeyFile reads and cross-checks public, private, and key-ID values.
func LoadKeyFile(path string, requirePrivate bool) (*KeyFile, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve key file path: %w", err)
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	var encoded encodedKeyFile
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("unsupported or malformed PACT key file: %s", absPath)
	}
	if encoded.Format != keyFormat || encoded.Algorithm != "ed25519" {
		return nil, fmt.Errorf("unsupported or malformed PACT key file: %s", absPath)
	}
	actor := norm.NFC.String(strings.TrimSpace(encoded.Actor))
	if actor == "" || len([]rune(actor)) > 255 {
		return nil, fmt.Errorf("key file has invalid actor label: %s", absPath)
	}
	public, err := decodeBase64URL(encoded.PublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("key file has invalid Ed25519 public key: %s", absPath)
	}
	keyID, err := KeyID(ed25519.PublicKey(public))
	if err != nil {
		return nil, err
	}
	if encoded.KeyID != keyID {
		return nil, fmt.Errorf("key ID does not match public key in %s", absPath)
	}
	var private ed25519.PrivateKey
	if encoded.PrivateKey == "" {
		if requirePrivate {
			return nil, fmt.Errorf("private key is required in %s", absPath)
		}
	} else {
		seed, err := decodeBase64URL(encoded.PrivateKey)
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("key file has invalid Ed25519 private key: %s", absPath)
		}
		private = ed25519.NewKeyFromSeed(seed)
		if !private.Public().(ed25519.PublicKey).Equal(ed25519.PublicKey(public)) {
			return nil, fmt.Errorf("private/public key mismatch in %s", absPath)
		}
	}
	createdAt, err := time.Parse(time.RFC3339, encoded.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("key file has invalid creation time: %s", absPath)
	}
	return &KeyFile{Actor: actor, KeyID: keyID, Public: ed25519.PublicKey(public), Private: private, CreatedAt: createdAt}, nil
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
		if !((character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' || character == '_') {
			return nil, errors.New("invalid base64url")
		}
	}
	return base64.RawURLEncoding.DecodeString(value)
}

func writeNewFile(path string, data []byte, mode fs.FileMode) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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
	if err := os.Link(tempPath, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("refusing to overwrite existing key file: %s", path)
		}
		return err
	}
	syncDirectory(filepath.Dir(path))
	return nil
}

func syncDirectory(path string) {
	directory, err := os.Open(path)
	if err != nil {
		return
	}
	defer directory.Close()
	_ = directory.Sync()
}
