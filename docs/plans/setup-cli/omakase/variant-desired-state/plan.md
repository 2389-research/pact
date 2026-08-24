<!-- ABOUTME: Plans the desired-state reconciler variant of the PACT setup CLI. -->
<!-- ABOUTME: Specifies TDD cycles, interfaces, scenarios, and verification for this variant. -->

# Setup CLI: Desired-State Reconciler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the full `pact setup` contract through an explicit observed-versus-desired state reconciler.

**Architecture:** Store and identity expose two read-only safety APIs. `internal/setup` models desired and observed resources, derives a four-action diff, then applies the diff with re-observation after every state change. The reconciler is bounded to store, key, trust, and verification; it is not a generic framework.

**Tech Stack:** Go 1.26, standard library, `golang.org/x/term`, existing PACT packages.

## Global Constraints

- Follow `docs/plans/setup-cli/design.md` exactly and add no generic plugin, registry, rollback, or manifest system.
- Test first and watch the expected failure before each production change.
- Use real filesystem state and Ed25519 keys; no mocks or production fake mode.
- Handwritten source files start with two `ABOUTME:` comment lines.
- Run Go commands through `env -u GOROOT mise exec --`.
- Never emit private or public key bytes from setup.

---

### Task 1: Add no-write state readers

**Files:** `internal/store/store.go`, `internal/store/store_test.go`, `internal/identity/keyfile.go`, `internal/identity/keyfile_test.go`.

**Interfaces:** `func (st *Store) DefaultNamespace() (string, error)`; `func ValidateSigningKeyPath(path, projectRoot string) (string, error)` returning the resolved intended target and `ErrSecretSafety` on lexical/resolved containment.

- [ ] Add failing tests for strict namespace reads and absent/existing/symlinked key targets, including no-write assertions.
- [ ] Run `env -u GOROOT mise exec -- go test ./internal/store ./internal/identity`; expect missing symbols.
- [ ] Implement the minimum strict reader and nearest-existing-ancestor path validation in their owning packages.
- [ ] Rerun tests; expect PASS with no warnings.
- [ ] Fresh-eyes review and commit only these files as `feat: expose setup safety state`.

### Task 2: Model and reconcile desired setup state

**Files:**
- Create: `internal/setup/state.go`
- Create: `internal/setup/reconcile.go`
- Create: `internal/setup/reconcile_test.go`

**Interfaces:**
```go
type Desired struct { Repo, Namespace, Actor, KeyPath string }
type Presence string
const (Missing Presence = "missing"; Matching Presence = "matching")
type Observed struct { Repo, Store, KeyPath string; StoreState, KeyState, TrustState Presence; KeyID string }
type Action struct { Step, Status string }
type Plan struct { Desired Desired; Observed Observed; Actions []Action }
type Result struct { Status, Repo, Store, Namespace, Actor, KeyPath, KeyID string; Actions []Action }
func Observe(desired Desired) (Observed, error)
func Diff(desired Desired, observed Observed) Plan
func Reconcile(plan Plan, now func() time.Time) (Result, error)
```
Conflicts return errors from `Observe`; the model does not represent a conflict as a creatable action. Errors after writes expose completed actions.

- [ ] Write failing pure `Diff` tests for all eight missing/matching store-key-trust combinations and fixed `store,key,trust,verify` ordering.
- [ ] Write failing integration tests for fresh/rerun/partial state, conflicts with byte snapshots, unsafe keys, corrupt state, concurrent identical/conflicting calls, and completed-action errors.
- [ ] Run `env -u GOROOT mise exec -- go test ./internal/setup`; expect missing types/functions.
- [ ] Implement `Observe` with no writes, owner-package readers, and pre-write key containment. Implement `Diff` as a small explicit switch per resource, never reflection or a registry.
- [ ] Implement `Reconcile` by re-observing before each action. On init/key create races, load and compare the winner; call `ledger.AddRoot` and strict verify for the last steps.
- [ ] Run `env -u GOROOT mise exec -- go test -race ./internal/setup`; expect PASS.
- [ ] Fresh-eyes review all three files and commit `feat: reconcile setup state`.

### Task 3: Add the setup CLI adapter

**Files:** `cmd/pact/app.go`, `cmd/pact/app_test.go`, `cmd/pact/main.go`, `go.mod`, `go.sum`.

**Interfaces:**
```go
type commandIO struct { stdin io.Reader; stdout, stderr io.Writer; interactive bool; now func() time.Time }
func runApp(args []string, stdin io.Reader, stdout, stderr io.Writer, interactive bool, now func() time.Time) int
func run(args []string, stdout, stderr io.Writer) int
func runSetup(args []string, app commandIO, asJSON bool) (map[string]any, error)
```
`runSetup` calls `setup.Observe`, `setup.Diff`, then `setup.Reconcile`.

- [ ] Write failing CLI tests for complete and missing flags, prompt order, one confirmation, refusal/EOF, no prompt for full flags/JSON, one JSON diagnostic, partial actions, human result, resolved paths, and no key-byte output.
- [ ] Run `env -u GOROOT mise exec -- go test ./cmd/pact -run 'TestRunSetup'`; expect missing setup command behavior.
- [ ] Implement command parsing and injected I/O. Render observed plan to stderr before confirmation. Use `term.IsTerminal` only at `main`, accept only `y`/`yes`, and keep all other command output stable.
- [ ] Map reconciler results/errors to the exact shared JSON and exit contract.
- [ ] Run all command tests and `go mod tidy` through the project toolchain; expect PASS.
- [ ] Fresh-eyes review and commit `feat: add setup command`.

### Task 4: Prove reconciliation with real scenarios

**Files:** Create `scenarios.jsonl` and create `cmd/pact/setup_e2e_test.go`; leave `scripts/check` unchanged because its `go test ./...` includes the E2E test.

- [ ] Add independent JSONL records for the ten shared scenarios with `name`, `description`, `given`, `when`, `then`, and `validates`.
- [ ] Write a compiled-binary `TestSetupBinaryScenarios` that executes every non-terminal scenario with real temporary repos and keys, including process-level concurrency and byte snapshots. Task 3's injected-I/O integration tests cover terminal prompts and confirmation.
- [ ] Prove the E2E test fails as `unknown command: setup` against the pre-feature base in a disposable worktree/patch, then restore and prove it passes.
- [ ] Run the canonical check, full race suite, vet, and golangci-lint through `env -u GOROOT mise exec --`; expect zero failures or new warnings.
- [ ] Run a live `.scratch/` fresh setup, rerun, and strict verify against the compiled binary; assert JSON with `jq` and keep scratch files ignored.
- [ ] Fresh-eyes review for needless reconciler machinery, races, unsafe paths, leaks, and error-detail loss. Fix test-first, then commit `test: cover setup scenarios`.
