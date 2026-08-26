// ABOUTME: Observes local PACT setup state and returns a deterministic, read-only action plan.
// ABOUTME: Delegates store, identity, trust, verification, and index rules to their owning packages.
package setup

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pact/internal/identity"
	"pact/internal/index"
	"pact/internal/ledger"
	"pact/internal/store"
)

// Request describes the setup state an operator wants to reach.
type Request struct {
	Repo      string
	Namespace string
	Actor     string
	KeyFile   string
	Now       time.Time
}

// ActionName identifies one fixed setup action.
type ActionName string

const (
	ActionStore  ActionName = "store"
	ActionKey    ActionName = "key"
	ActionTrust  ActionName = "trust"
	ActionVerify ActionName = "verify"
	ActionIndex  ActionName = "index"
)

// ActionStatus describes either observed completion or an action still to run.
type ActionStatus string

const (
	ActionPlanned  ActionStatus = "planned"
	ActionExisting ActionStatus = "existing"
	ActionValid    ActionStatus = "valid"
	ActionCurrent  ActionStatus = "current"
)

// Action is one ordered setup action and its observed plan status.
type Action struct {
	Name   ActionName   `json:"name"`
	Status ActionStatus `json:"status"`
}

// Plan is a resolved setup request and exactly five actions, without key material.
type Plan struct {
	Repo      string
	Namespace string
	Actor     string
	KeyFile   string
	Actions   []Action
}

type observedSetup struct {
	repo, namespace, actor, keyFile string
	store                           *store.Store
	key                             *identity.KeyFile
	storeStatus, keyStatus          ActionStatus
	trustStatus, verifyStatus       ActionStatus
	indexStatus                     ActionStatus
}

// Inspect validates request intent, observes the filesystem through owner APIs, and never writes.
func Inspect(ctx context.Context, request Request) (Plan, error) {
	observed, err := observe(ctx, request)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Repo:      observed.repo,
		Namespace: observed.namespace,
		Actor:     observed.actor,
		KeyFile:   observed.keyFile,
		Actions: []Action{
			{Name: ActionStore, Status: observed.storeStatus},
			{Name: ActionKey, Status: observed.keyStatus},
			{Name: ActionTrust, Status: observed.trustStatus},
			{Name: ActionVerify, Status: observed.verifyStatus},
			{Name: ActionIndex, Status: observed.indexStatus},
		},
	}, nil
}

func observe(ctx context.Context, request Request) (observedSetup, error) {
	observed, err := normalizeRequest(ctx, request)
	if err != nil {
		return observedSetup{}, err
	}

	st, storeStatus, err := observeStore(ctx, observed.repo, observed.namespace)
	if err != nil {
		return observedSetup{}, err
	}
	observed.store, observed.storeStatus = st, storeStatus
	if st != nil {
		observed.repo = st.Root()
		keyFile, err := identity.ValidateSigningKeyPath(request.KeyFile, observed.repo)
		if err != nil {
			return observedSetup{}, err
		}
		observed.keyFile = keyFile
	}

	key, keyStatus, err := observeKey(ctx, observed.keyFile, observed.repo, observed.actor)
	if err != nil {
		return observedSetup{}, err
	}
	observed.key, observed.keyStatus = key, keyStatus
	if st == nil {
		return observed, nil
	}

	if key != nil {
		roots, err := ledger.RootsContext(ctx, st)
		if err != nil {
			return observedSetup{}, fmt.Errorf("inspect trusted roots: %w", err)
		}
		if root, found := roots[key.KeyID]; found {
			if root.PublicKey != base64.RawURLEncoding.EncodeToString(key.Public) {
				return observedSetup{}, fmt.Errorf("%w: trusted root conflicts with requested signing key", ledger.ErrIntegrity)
			}
			observed.trustStatus = ActionExisting
		}
	}

	verification, err := ledger.VerifyContext(ctx, st, true)
	if err != nil {
		return observedSetup{}, fmt.Errorf("strict verification: %w", err)
	}
	if !verification.OK {
		return observedSetup{}, fmt.Errorf("%w: strict verification failed", ledger.ErrIntegrity)
	}
	observed.verifyStatus = ActionValid

	status, err := index.New(st).Status(ctx)
	if err != nil {
		return observedSetup{}, fmt.Errorf("inspect index: %w", err)
	}
	if status.Index.State == "current" {
		observed.indexStatus = ActionCurrent
	}
	return observed, nil
}

func normalizeRequest(ctx context.Context, request Request) (observedSetup, error) {
	if ctx == nil {
		return observedSetup{}, fmt.Errorf("context is required")
	}
	if err := ctx.Err(); err != nil {
		return observedSetup{}, err
	}
	if strings.TrimSpace(request.Repo) == "" || request.Namespace == "" || strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.KeyFile) == "" {
		return observedSetup{}, fmt.Errorf("repo, namespace, actor, and key file are required")
	}
	namespace, err := ledger.NormalizeNamespace(request.Namespace)
	if err != nil {
		return observedSetup{}, err
	}
	actor, err := identity.NormalizeActor(request.Actor)
	if err != nil {
		return observedSetup{}, err
	}
	repo, err := filepath.Abs(request.Repo)
	if err != nil {
		return observedSetup{}, fmt.Errorf("resolve repository path: %w", err)
	}
	if info, err := os.Stat(repo); err == nil && !info.IsDir() {
		return observedSetup{}, fmt.Errorf("repository is not a directory")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return observedSetup{}, fmt.Errorf("inspect repository: %w", err)
	}
	keyFile, err := identity.ValidateSigningKeyPath(request.KeyFile, repo)
	if err != nil {
		return observedSetup{}, err
	}
	return observedSetup{
		repo:         repo,
		namespace:    namespace,
		actor:        actor,
		keyFile:      keyFile,
		storeStatus:  ActionPlanned,
		keyStatus:    ActionPlanned,
		trustStatus:  ActionPlanned,
		verifyStatus: ActionPlanned,
		indexStatus:  ActionPlanned,
	}, nil
}

func observeStore(ctx context.Context, repo, namespace string) (*store.Store, ActionStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if _, err := os.Lstat(repo); errors.Is(err, fs.ErrNotExist) {
		return nil, ActionPlanned, nil
	} else if err != nil {
		return nil, "", fmt.Errorf("inspect repository: %w", err)
	}
	//nolint:contextcheck // store owns the only validated opener and has no context-aware variant.
	st, err := store.Open(repo)
	if err == nil {
		//nolint:contextcheck // store owns the only validated namespace reader and has no context-aware variant.
		existing, namespaceErr := st.DefaultNamespace()
		if namespaceErr != nil {
			return nil, "", fmt.Errorf("inspect store namespace: %w", namespaceErr)
		}
		if existing != namespace {
			return nil, "", fmt.Errorf("%w: existing store namespace conflicts with requested namespace", store.ErrAlreadyInitialized)
		}
		return st, ActionExisting, nil
	}
	if !errors.Is(err, store.ErrNotInitialized) {
		return nil, "", fmt.Errorf("open store: %w", err)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".pact")); errors.Is(statErr, fs.ErrNotExist) {
		return nil, ActionPlanned, nil
	} else if statErr != nil {
		return nil, "", fmt.Errorf("inspect store: %w", statErr)
	}
	return nil, "", fmt.Errorf("open existing store: %w", err)
}

func observeKey(ctx context.Context, path, projectRoot, actor string) (*identity.KeyFile, ActionStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, ActionPlanned, nil
	} else if err != nil {
		return nil, "", fmt.Errorf("inspect signing key: %w", err)
	}
	//nolint:contextcheck // identity owns the private-key loader and has no context-aware variant.
	key, err := identity.LoadSigningKey(path, projectRoot)
	if err != nil {
		return nil, "", err
	}
	if key.Actor != actor {
		return nil, "", fmt.Errorf("%w: existing signing key actor conflicts with requested actor", identity.ErrIntegrity)
	}
	return key, ActionExisting, nil
}
