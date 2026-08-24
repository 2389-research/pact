<!-- ABOUTME: Plans the focused internal service variant of the PACT setup CLI. -->
<!-- ABOUTME: Specifies TDD cycles, interfaces, scenarios, and verification for this variant. -->

# Setup CLI: Focused Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the full `pact setup` contract through a small typed `internal/setup` service.

**Architecture:** Store and identity expose two read-only safety APIs. `internal/setup` owns request inspection, a typed plan, and convergent apply; the CLI owns only flags, prompts, rendering, and exit mapping. The service has no terminal or process-global dependencies.

**Tech Stack:** Go 1.26, standard library, `golang.org/x/term`, existing PACT packages.

## Global Constraints

- Follow `docs/plans/setup-cli/design.md` exactly; exclude every out-of-scope feature.
- Use strict test-first red-green-refactor and real filesystem/crypto dependencies.
- Handwritten source files start with two `ABOUTME:` comment lines.
- Run Go commands through `env -u GOROOT mise exec --`.
- Setup output never includes private or public key bytes.

---

### Task 1: Add no-write state readers

**Files:** `internal/store/store.go`, `internal/store/store_test.go`, `internal/identity/keyfile.go`, `internal/identity/keyfile_test.go`.

**Interfaces:** `func (st *Store) DefaultNamespace() (string, error)` and `func ValidateSigningKeyPath(path, projectRoot string) (string, error)` with the exact behavior in the shared design.

- [ ] Write failing table tests for valid/malformed namespace and external/in-repo/symlinked/absent key targets; assert no writes.
- [ ] Run `env -u GOROOT mise exec -- go test ./internal/store ./internal/identity`; expect missing-interface compile failures.
- [ ] Implement strict namespace parsing and nearest-existing-ancestor path resolution; wrap containment refusals with `identity.ErrSecretSafety`.
- [ ] Rerun the package tests; expect PASS without warnings.
- [ ] Fresh-eyes review the four files, stage only them, and commit `feat: expose setup safety state`.

### Task 2: Implement the setup service

**Files:**
- Create: `internal/setup/setup.go`
- Create: `internal/setup/setup_test.go`

**Interfaces:**
```go
type Request struct { Repo, Namespace, Actor, KeyPath string }
type Action struct { Step, Status string }
type Plan struct { Request Request; Repo, Store, KeyPath string; Actions []Action }
type Result struct { Status, Repo, Store, Namespace, Actor, KeyPath, KeyID string; Actions []Action }
func Inspect(request Request) (Plan, error)
func Apply(plan Plan, now func() time.Time) (Result, error)
```
Errors after writes implement `interface { CompletedActions() []Action }` without exposing key bytes.

- [ ] Write failing service tests for fresh/rerun/three partial states, namespace/actor conflicts, unsafe keys, corrupt state, completed actions, and concurrent identical/conflicting calls; snapshot files before refusal.
- [ ] Run `env -u GOROOT mise exec -- go test ./internal/setup`; expect missing package symbols.
- [ ] Implement `Inspect`: distinguish absent `.pact` from invalid existing state, call the shared readers, validate key containment before writes, and derive exactly four ordered actions.
- [ ] Implement `Apply`: recheck every resource, handle create-race errors only by loading and comparing winner state, then call `ledger.AddRoot` and strict `ledger.Verify`. Preserve valid partial work and completed actions.
- [ ] Run `env -u GOROOT mise exec -- go test -race ./internal/setup`; expect PASS.
- [ ] Fresh-eyes review, stage the two files, and commit `feat: add convergent setup service`.

### Task 3: Adapt the service to the CLI

**Files:** `cmd/pact/app.go`, `cmd/pact/app_test.go`, `cmd/pact/main.go`, `go.mod`, `go.sum`.

**Interfaces:**
```go
type commandIO struct { stdin io.Reader; stdout, stderr io.Writer; interactive bool; now func() time.Time }
func runApp(args []string, stdin io.Reader, stdout, stderr io.Writer, interactive bool, now func() time.Time) int
func run(args []string, stdout, stderr io.Writer) int
func runSetup(args []string, app commandIO, asJSON bool) (map[string]any, error)
```

- [ ] Write failing CLI tests for complete flags, missing input modes, exact prompt order and one confirmation, refusal/EOF, fully flagged terminal, JSON error count, completed-action details, human output, resolved paths, and key-byte leaks.
- [ ] Run `env -u GOROOT mise exec -- go test ./cmd/pact -run 'TestRunSetup'`; expect unknown-command/missing-adapter failures.
- [ ] Implement `setup` dispatch. Prompt with a bounded reader, accept only case-insensitive `y`/`yes`, render the inspected plan to stderr, and call `setup.Apply` only after consent. Detect terminal state with `term.IsTerminal` only in `main`.
- [ ] Map `setup.Result` to the shared JSON shape and typed exit codes; preserve all existing command behavior.
- [ ] Run all command tests and `env -u GOROOT mise exec -- go mod tidy`; expect PASS.
- [ ] Fresh-eyes review, stage only named files, and commit `feat: add setup command`.

### Task 4: Prove the service through real scenarios

**Files:** Create `scenarios.jsonl` and create `cmd/pact/setup_e2e_test.go`; leave `scripts/check` unchanged because its `go test ./...` includes the E2E test.

- [ ] Write `TestSetupBinaryScenarios` to build and run the real binary for every non-terminal shared scenario. Each subtest gets independent real temporary storage and keys; use no mock or fake mode. Task 3's injected-I/O integration tests cover terminal prompts and confirmation.
- [ ] Record matching standalone scenario specifications in `scenarios.jsonl` with `name`, `description`, `given`, `when`, `then`, and `validates`.
- [ ] Prove RED against the pre-feature base in a disposable patch/worktree; expect `unknown command: setup`. Restore and run `env -u GOROOT mise exec -- go test ./cmd/pact -run TestSetupBinaryScenarios`; expect PASS.
- [ ] Run `env -u GOROOT mise exec -- ./scripts/check`, race tests, vet, and golangci-lint; expect clean output.
- [ ] Build into `.scratch/`, run fresh fully flagged setup, rerun, and strict verify with real files; validate each JSON result with `jq`.
- [ ] Fresh-eyes review for leaks, path races, partial progress, and duplicate format parsing; fix findings test-first. Commit `test: cover setup scenarios`.
