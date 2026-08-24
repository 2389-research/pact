// ABOUTME: Maintains local, out-of-band PACT trust roots outside immutable ledger history.
// ABOUTME: Validates root identity bytes and atomically persists sorted trust configuration.
package ledger

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"pact/internal/canonical"
	"pact/internal/identity"
	"pact/internal/store"
)

const trustFormat = "pact/trust/v1"

var (
	// ErrIntegrity marks invalid canonical bytes, signatures, DAG state, or trusted identity bytes.
	ErrIntegrity = errors.New("ledger integrity failure")
	// ErrStore marks malformed or unavailable mutable ledger store configuration.
	ErrStore = errors.New("ledger store failure")
	// ErrSecretSafety marks immutable material refused because it may contain a secret.
	ErrSecretSafety = errors.New("ledger secret safety refusal")
	// ErrMissingDependency marks a requested or referenced object that is unavailable.
	ErrMissingDependency = errors.New("ledger dependency missing")
)

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
	var created bool
	err := st.WithMutationLock(func() error {
		var err error
		created, err = addRootLocked(st, key, now)
		return err
	})
	return created, err
}

func addRootLocked(st *store.Store, key *identity.KeyFile, now time.Time) (bool, error) {
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
		return false, fmt.Errorf("%w: %w", ErrStore, err)
	}
	return true, nil
}

// Roots returns trusted identities keyed by their stable PACT key IDs.
func Roots(st *store.Store) (map[string]Root, error) {
	return RootsContext(context.Background(), st)
}

// RootsContext returns trusted identities while honoring cancellation during local canonical work.
func RootsContext(ctx context.Context, st *store.Store) (map[string]Root, error) {
	if st == nil {
		return nil, fmt.Errorf("store is required")
	}
	loaded, err := loadRootsContext(ctx, st)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make(map[string]Root, len(loaded))
	for index, root := range loaded {
		if err := pollContext(ctx, index); err != nil {
			return nil, err
		}
		if existing, found := result[root.KeyID]; found && existing.PublicKey != root.PublicKey {
			return nil, fmt.Errorf("%w: conflicting trusted-root bytes for %s", ErrIntegrity, root.KeyID)
		}
		public, err := base64.RawURLEncoding.DecodeString(root.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid trusted root public key", ErrIntegrity)
		}
		// KeyID hashes exactly 32 admitted bytes; there is no unbounded work to cancel.
		expectedID, err := identity.KeyID(public) //nolint:contextcheck
		if err != nil || expectedID != root.KeyID {
			return nil, fmt.Errorf("%w: trusted root public key mismatch for %s", ErrIntegrity, root.KeyID)
		}
		result[root.KeyID] = root
	}
	return result, nil
}

func loadRoots(st *store.Store) ([]Root, error) {
	return loadRootsContext(context.Background(), st)
}

func loadRootsContext(ctx context.Context, st *store.Store) ([]Root, error) {
	raw, err := st.ReadLocalContext(ctx, "trust.json")
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", ErrStore, err)
	}
	value, err := canonical.ParseContext(ctx, raw)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: malformed local trust file", ErrStore)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: malformed local trust file", ErrStore)
	}
	rootsValue, exists := object["roots"]
	if !exists {
		return nil, fmt.Errorf("%w: malformed local trust file", ErrStore)
	}
	if _, ok := rootsValue.([]any); !ok {
		return nil, fmt.Errorf("%w: malformed local trust file", ErrStore)
	}
	encoded, err := canonical.MarshalContext(ctx, value)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: malformed local trust file", ErrStore)
	}
	var config trustFile
	decoder := json.NewDecoder(&ledgerContextReader{ctx: ctx, reader: bytes.NewReader(encoded)})
	if err := decoder.Decode(&config); err != nil || config.Format != trustFormat {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: malformed local trust file", ErrStore)
	}
	for index, root := range config.Roots {
		if err := pollContext(ctx, index); err != nil {
			return nil, err
		}
		if err := validateRootContext(ctx, root); err != nil {
			return nil, err
		}
	}
	return config.Roots, nil
}

type ledgerContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *ledgerContextReader) Read(destination []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	if len(destination) > 256 {
		destination = destination[:256]
	}
	return reader.reader.Read(destination)
}

func validateRootContext(ctx context.Context, root Root) error {
	if root.Actor == "" || root.AddedAt == "" {
		return fmt.Errorf("%w: malformed trusted-root entry", ErrIntegrity)
	}
	if _, err := time.Parse(time.RFC3339, root.AddedAt); err != nil {
		return fmt.Errorf("%w: malformed trusted-root entry", ErrIntegrity)
	}
	public, err := decodeBase64URLContext(ctx, root.PublicKey, 32)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if err != nil {
		return fmt.Errorf("%w: invalid trusted root public key", ErrIntegrity)
	}
	// KeyID hashes exactly 32 admitted bytes; there is no unbounded work to cancel.
	expectedID, err := identity.KeyID(public) //nolint:contextcheck
	if err != nil || expectedID != root.KeyID {
		return fmt.Errorf("%w: trusted root public key mismatch for %s", ErrIntegrity, root.KeyID)
	}
	return nil
}
