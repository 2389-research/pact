// ABOUTME: Maintains local, out-of-band PACT trust roots outside immutable ledger history.
// ABOUTME: Validates root identity bytes and atomically persists sorted trust configuration.
package ledger

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"pact/internal/canonical"
	"pact/internal/identity"
	"pact/internal/store"
)

const trustFormat = "pact/trust/v1"

// ErrIntegrity marks an invalid or conflicting trusted identity.
var ErrIntegrity = errors.New("trust integrity failure")

// Root is one locally trusted public identity.
type Root struct {
	KeyID     string `json:"key_id"`
	Actor     string `json:"actor"`
	PublicKey string `json:"public_key"`
	AddedAt   string `json:"added_at"`
}

type trustFile struct {
	Format string `json:"format"`
	Roots  []Root `json:"roots"`
}

// AddRoot adds key's public identity to the local root set without ledger admission.
func AddRoot(st *store.Store, key *identity.KeyFile, now time.Time) (bool, error) {
	if st == nil || key == nil {
		return false, fmt.Errorf("store and key are required")
	}
	roots, err := loadRoots(st)
	if err != nil {
		return false, err
	}
	public := base64.RawURLEncoding.EncodeToString(key.Public)
	for _, root := range roots {
		if root.KeyID != key.KeyID {
			continue
		}
		if root.PublicKey != public {
			return false, fmt.Errorf("%w: conflicting trusted-root bytes for %s", ErrIntegrity, key.KeyID)
		}
		return false, nil
	}
	expectedID, err := identity.KeyID(key.Public)
	if err != nil || expectedID != key.KeyID {
		return false, fmt.Errorf("%w: trusted root key ID does not match public key", ErrIntegrity)
	}
	roots = append(roots, Root{
		KeyID:     key.KeyID,
		Actor:     key.Actor,
		PublicKey: public,
		AddedAt:   now.UTC().Format(time.RFC3339),
	})
	sort.Slice(roots, func(i, j int) bool { return roots[i].KeyID < roots[j].KeyID })
	if err := st.WriteLocalJSON("trust.json", trustFile{Format: trustFormat, Roots: roots}, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// Roots returns trusted identities keyed by their stable PACT key IDs.
func Roots(st *store.Store) (map[string]Root, error) {
	if st == nil {
		return nil, fmt.Errorf("store is required")
	}
	loaded, err := loadRoots(st)
	if err != nil {
		return nil, err
	}
	result := make(map[string]Root, len(loaded))
	for _, root := range loaded {
		if existing, found := result[root.KeyID]; found && existing.PublicKey != root.PublicKey {
			return nil, fmt.Errorf("%w: conflicting trusted-root bytes for %s", ErrIntegrity, root.KeyID)
		}
		public, err := base64.RawURLEncoding.DecodeString(root.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid trusted root public key", ErrIntegrity)
		}
		expectedID, err := identity.KeyID(public)
		if err != nil || expectedID != root.KeyID {
			return nil, fmt.Errorf("%w: trusted root public key mismatch for %s", ErrIntegrity, root.KeyID)
		}
		result[root.KeyID] = root
	}
	return result, nil
}

func loadRoots(st *store.Store) ([]Root, error) {
	raw, err := st.ReadLocal("trust.json")
	if err != nil {
		return nil, err
	}
	value, err := canonical.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("malformed local trust file")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("malformed local trust file")
	}
	rootsValue, exists := object["roots"]
	if !exists {
		return nil, fmt.Errorf("malformed local trust file")
	}
	if _, ok := rootsValue.([]any); !ok {
		return nil, fmt.Errorf("malformed local trust file")
	}
	encoded, err := canonical.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("malformed local trust file")
	}
	var config trustFile
	if err := json.Unmarshal(encoded, &config); err != nil || config.Format != trustFormat {
		return nil, fmt.Errorf("malformed local trust file")
	}
	for _, root := range config.Roots {
		if err := validateRoot(root); err != nil {
			return nil, err
		}
	}
	return config.Roots, nil
}

func validateRoot(root Root) error {
	if root.Actor == "" || root.AddedAt == "" {
		return fmt.Errorf("%w: malformed trusted-root entry", ErrIntegrity)
	}
	if _, err := time.Parse(time.RFC3339, root.AddedAt); err != nil {
		return fmt.Errorf("%w: malformed trusted-root entry", ErrIntegrity)
	}
	public, err := base64.RawURLEncoding.DecodeString(root.PublicKey)
	if err != nil {
		return fmt.Errorf("%w: invalid trusted root public key", ErrIntegrity)
	}
	expectedID, err := identity.KeyID(public)
	if err != nil || expectedID != root.KeyID {
		return fmt.Errorf("%w: trusted root public key mismatch for %s", ErrIntegrity, root.KeyID)
	}
	return nil
}
