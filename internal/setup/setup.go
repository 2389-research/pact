// ABOUTME: Inspects local PACT setup state and applies its five ordered owner operations.
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

var errInvalidOwnerOutcome = errors.New("invalid owner outcome")

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
	ActionCreated  ActionStatus = "created"
	ActionExisting ActionStatus = "existing"
	ActionValid    ActionStatus = "valid"
	ActionCurrent  ActionStatus = "current"
	ActionRebuilt  ActionStatus = "rebuilt"
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

// Result reports the resolved setup identity and every action proven complete.
type Result struct {
	Status    string
	Repo      string
	Store     string
	Namespace string
	Actor     string
	KeyFile   string
	KeyID     string
	Actions   []Action
}

// ApplyError preserves completed setup actions and the owner operation failure.
type ApplyError struct {
	Result Result
	Err    error
}

func (err *ApplyError) Error() string {
	if err == nil || err.Err == nil {
		return "setup apply failed"
	}
	return err.Err.Error()
}

// Unwrap exposes the original owner failure for errors.Is and errors.As.
func (err *ApplyError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

type ownerOperations struct {
	initStore    func(string, string, time.Time) (store.InitResult, error)
	generateKey  func(string, string, time.Time) (identity.GenerateResult, error)
	addRoot      func(*store.Store, *identity.KeyFile, time.Time) (ledger.RootResult, error)
	verify       func(context.Context, *store.Store, bool) (ledger.VerifyResult, error)
	indexStatus  func(context.Context, *store.Store) (index.Status, error)
	rebuildIndex func(context.Context, *store.Store) (index.RebuildResult, error)
}

var defaultOwnerOperations = ownerOperations{
	initStore:   store.Init,
	generateKey: identity.GenerateKeyFile,
	addRoot:     ledger.AddRoot,
	verify:      ledger.VerifyContext,
	indexStatus: func(ctx context.Context, st *store.Store) (index.Status, error) {
		return index.New(st).Status(ctx)
	},
	rebuildIndex: func(ctx context.Context, st *store.Store) (index.RebuildResult, error) {
		return index.New(st).Rebuild(ctx)
	},
}

// Apply runs the fixed setup actions in order and reports only proven completion.
func Apply(ctx context.Context, request Request) (Result, error) {
	return applyWithOwners(ctx, request, defaultOwnerOperations)
}

func applyWithOwners(ctx context.Context, request Request, owners ownerOperations) (Result, error) { //nolint:funlen,gocyclo,gocognit // The linear branches mirror the five ordered owner actions.
	observed, err := normalizeRequest(ctx, request)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Repo:      observed.repo,
		Namespace: observed.namespace,
		Actor:     observed.actor,
		KeyFile:   observed.keyFile,
		Actions:   make([]Action, 0, 5),
	}

	initResult, initErr := owners.initStore(result.Repo, result.Namespace, request.Now)
	switch initResult.Status {
	case store.InitCreated:
		if initResult.Store == nil {
			return applyFailure(result, fmt.Errorf("initialize store reported creation without a store"))
		}
		result.Repo = initResult.Store.Root()
		result.Store = initResult.Store.Dir()
		result.Actions = append(result.Actions, Action{Name: ActionStore, Status: ActionCreated})
		if initErr != nil {
			return applyFailure(result, initErr)
		}
	case store.InitConflict:
		if initErr == nil {
			return applyFailure(result, invalidOwnerOutcome("store"))
		}
		//nolint:contextcheck // store owns the only validated opener and namespace reader.
		opened, openErr := openMatchingStore(result.Repo, result.Namespace)
		if openErr != nil {
			return applyFailure(result, errors.Join(initErr, openErr))
		}
		initResult.Store = opened
		result.Repo = opened.Root()
		result.Store = opened.Dir()
		result.Actions = append(result.Actions, Action{Name: ActionStore, Status: ActionExisting})
		if !store.IsCleanInitCollision(initErr) {
			return applyFailure(result, initErr)
		}
	default:
		if initErr == nil {
			initErr = fmt.Errorf("initialize store reported no publication outcome")
		}
		return applyFailure(result, initErr)
	}

	validatedKeyFile, err := identity.ValidateSigningKeyPath(request.KeyFile, result.Repo)
	if err != nil {
		return applyFailure(result, err)
	}
	result.KeyFile = validatedKeyFile
	keyResult, keyErr := owners.generateKey(result.KeyFile, result.Actor, request.Now)
	var key *identity.KeyFile
	switch keyResult.Status {
	case identity.GenerateCreated:
		if keyResult.Key == nil {
			return applyFailure(result, fmt.Errorf("generate key reported creation without a key"))
		}
		key = keyResult.Key
		result.Actions = append(result.Actions, Action{Name: ActionKey, Status: ActionCreated})
		result.KeyID = key.KeyID
		if keyErr != nil {
			return applyFailure(result, keyErr)
		}
	case identity.GenerateConflict:
		if keyErr == nil {
			return applyFailure(result, invalidOwnerOutcome("key"))
		}
		//nolint:contextcheck // identity owns the only validated private-key loader.
		key, err = openMatchingKey(result.KeyFile, result.Repo, result.Actor)
		if err != nil {
			return applyFailure(result, errors.Join(keyErr, err))
		}
		result.Actions = append(result.Actions, Action{Name: ActionKey, Status: ActionExisting})
		result.KeyID = key.KeyID
		if !cleanKeyCollision(keyErr) {
			return applyFailure(result, keyErr)
		}
	default:
		if keyErr == nil {
			keyErr = fmt.Errorf("generate key reported no publication outcome")
		}
		return applyFailure(result, keyErr)
	}

	rootResult, rootErr := owners.addRoot(initResult.Store, key, request.Now)
	switch rootResult.Status {
	case ledger.RootCreated:
		result.Actions = append(result.Actions, Action{Name: ActionTrust, Status: ActionCreated})
	case ledger.RootExisting:
		result.Actions = append(result.Actions, Action{Name: ActionTrust, Status: ActionExisting})
	default:
		if rootErr == nil {
			rootErr = fmt.Errorf("add trusted root reported no publication outcome")
		}
		return applyFailure(result, rootErr)
	}
	if rootErr != nil {
		return applyFailure(result, rootErr)
	}
	verification, err := owners.verify(ctx, initResult.Store, true)
	if err != nil {
		return applyFailure(result, fmt.Errorf("strict verification: %w", err))
	}
	if !verification.OK {
		return applyFailure(result, fmt.Errorf("%w: strict verification failed", ledger.ErrIntegrity))
	}
	result.Actions = append(result.Actions, Action{Name: ActionVerify, Status: ActionValid})

	status, err := owners.indexStatus(ctx, initResult.Store)
	if err != nil {
		return applyFailure(result, fmt.Errorf("inspect index: %w", err))
	}
	if status.Index.State == "current" {
		result.Actions = append(result.Actions, Action{Name: ActionIndex, Status: ActionCurrent})
		result.Status = "ready"
		return result, nil
	}
	rebuild, rebuildErr := owners.rebuildIndex(ctx, initResult.Store)
	switch {
	case rebuild.Created:
		result.Actions = append(result.Actions, Action{Name: ActionIndex, Status: ActionCreated})
	case rebuild.Replaced:
		result.Actions = append(result.Actions, Action{Name: ActionIndex, Status: ActionRebuilt})
	default:
		if rebuildErr == nil {
			rebuildErr = fmt.Errorf("rebuild index reported no publication outcome")
		}
		return applyFailure(result, rebuildErr)
	}
	if rebuildErr != nil {
		return applyFailure(result, rebuildErr)
	}
	result.Status = "ready"
	return result, nil
}

func openMatchingStore(repo, namespace string) (*store.Store, error) {
	//nolint:contextcheck // store owns the validated opener and namespace reader.
	st, err := store.Open(repo)
	if err != nil {
		return nil, err
	}
	existing, err := st.DefaultNamespace()
	if err != nil {
		return nil, err
	}
	if existing != namespace {
		return nil, fmt.Errorf("%w: existing store namespace conflicts with requested namespace", store.ErrAlreadyInitialized)
	}
	return st, nil
}

func openMatchingKey(path, projectRoot, actor string) (*identity.KeyFile, error) {
	//nolint:contextcheck // identity owns the validated private-key loader.
	key, err := identity.LoadSigningKey(path, projectRoot)
	if err != nil {
		return nil, err
	}
	if key.Actor != actor {
		return nil, fmt.Errorf("%w: existing signing key actor conflicts with requested actor", identity.ErrIntegrity)
	}
	return key, nil
}

func applyFailure(result Result, err error) (Result, error) {
	return result, &ApplyError{Result: result, Err: err}
}

func invalidOwnerOutcome(owner string) error {
	return fmt.Errorf("%w: %s conflict has no error", errInvalidOwnerOutcome, owner)
}

func cleanKeyCollision(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(interface{ Unwrap() []error }); ok {
		return false
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		cause := single.Unwrap()
		if cause == nil {
			return errors.Is(err, fs.ErrExist)
		}
		return cleanKeyCollision(cause)
	}
	return errors.Is(err, fs.ErrExist)
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
