<!-- ABOUTME: Plans the command-local orchestration variant of the PACT setup CLI. -->
<!-- ABOUTME: Specifies TDD cycles, interfaces, scenarios, and verification for this variant. -->

# Setup CLI: Command Orchestrator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the full `pact setup` contract with setup state and sequencing kept in `cmd/pact`.

**Architecture:** The store and identity packages gain two read-only safety APIs. Focused command-local types inspect desired state, build the four ordered actions, and apply them through existing production primitives. The process adapter injects input, terminal state, and time.

**Tech Stack:** Go 1.26, standard library, `golang.org/x/term`, existing PACT store/identity/ledger packages.

## Global Constraints

- Follow `docs/plans/setup-cli/design.md` exactly; do not add setup features listed as out of scope.
- Write each production change only after its covering test fails for the expected reason.
- Use real files and real Ed25519 keys; do not add mocks or a production mock mode.
- Handwritten source files start with two `ABOUTME:` comment lines.
- Run Go commands through `env -u GOROOT mise exec --`.
- Never print private or public key bytes from `setup`.

---

### Task 1: Add no-write state readers

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`
- Modify: `internal/identity/keyfile.go`
- Modify: `internal/identity/keyfile_test.go`

**Interfaces:**
- Produces: `func (st *Store) DefaultNamespace() (string, error)`.
- Produces: `func ValidateSigningKeyPath(path, projectRoot string) (string, error)`; it returns the resolved intended path and wraps `ErrSecretSafety` for lexical or resolved project containment.

- [ ] **Step 1: Write failing table tests** for valid namespace reads, malformed/missing namespace, an absent external key path, absent and existing in-repo paths, a symlinked ancestor into the repo, and a repo symlink. Assert no files are created.
- [ ] **Step 2: Verify RED:** run `env -u GOROOT mise exec -- go test ./internal/store ./internal/identity`; expect compile failures for both missing interfaces.
- [ ] **Step 3: Implement the minimum readers.** `DefaultNamespace` must use the store's strict JSON parser and `validateNamespace`. `ValidateSigningKeyPath` must resolve the repo, walk from the intended key to its nearest existing ancestor, resolve that ancestor, append the missing suffix, check both lexical and resolved containment with `filepath.Rel`, and perform no write.
- [ ] **Step 4: Verify GREEN:** rerun the Task 1 command; expect both packages to pass with no warnings.
- [ ] **Step 5: Commit:** stage only the four files and commit `feat: expose setup safety state`.

### Task 2: Build command-local setup orchestration

**Files:**
- Create: `cmd/pact/setup.go`
- Create: `cmd/pact/setup_test.go`

**Interfaces:**
- Produces: `type setupRequest struct { Repo, Namespace, Actor, KeyPath string }`.
- Produces: `type setupAction struct { Step, Status string }` and `type setupResult struct { Status, Repo, Store, Namespace, Actor, KeyPath, KeyID string; Actions []setupAction }`.
- Produces: `func inspectSetup(request setupRequest) (setupPlan, error)` and `func applySetup(plan setupPlan, now func() time.Time) (setupResult, error)`.

- [ ] **Step 1: Write failing tests** that cover fresh setup, exact rerun with byte snapshots, store-only/key-only/untrusted-key states, namespace and actor conflicts with byte snapshots, all unsafe key forms, corrupt store/trust, and two goroutines for identical and conflicting runs. Assert ordered statuses `store,key,trust,verify`.
- [ ] **Step 2: Verify RED:** run `env -u GOROOT mise exec -- go test ./cmd/pact -run 'Test(Inspect|Apply)Setup'`; expect missing setup types/functions.
- [ ] **Step 3: Implement inspection and apply.** Inspect the `.pact` path only to distinguish absence from an invalid existing store; let `store.Open` and `DefaultNamespace` validate contents. Validate key location before any write. Re-observe after `ErrAlreadyInitialized` or `fs.ErrExist` and accept only exact state. Use `identity.LoadSigningKey`, `ledger.Roots`, `ledger.AddRoot`, and `ledger.Verify(st, true)`. Return completed actions with any error.
- [ ] **Step 4: Verify GREEN and race safety:** run the Task 2 test command and `env -u GOROOT mise exec -- go test -race ./cmd/pact -run 'Test.*Setup'`; expect PASS.
- [ ] **Step 5: Commit:** stage `cmd/pact/setup.go` and `cmd/pact/setup_test.go`; commit `feat: orchestrate convergent setup`.

### Task 3: Add the setup command and terminal adapter

**Files:**
- Modify: `cmd/pact/app.go`
- Modify: `cmd/pact/app_test.go`
- Modify: `cmd/pact/main.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Produces: `func runApp(args []string, stdin io.Reader, stdout, stderr io.Writer, interactive bool, now func() time.Time) int`.
- Preserves: `func run(args []string, stdout, stderr io.Writer) int` as a non-interactive test adapter.
- Produces: `func runSetup(args []string, app commandIO, asJSON bool) (map[string]any, error)`.

- [ ] **Step 1: Write failing CLI tests** for all flags, missing non-terminal/JSON values, prompts in namespace/actor/key order, one `[y/N]` confirmation, yes/refusal/EOF, fully flagged terminal no prompt, one JSON error, detailed partial actions, resolved paths, human output fields, and no key-byte leakage.
- [ ] **Step 2: Verify RED:** run `env -u GOROOT mise exec -- go test ./cmd/pact -run 'TestRunSetup'`; expect unknown command or missing adapter failures.
- [ ] **Step 3: Implement the adapter.** Add `setup` dispatch before ledger commands. Read lines with a bounded `bufio.Reader`, trim space, accept only `y` or `yes`, and send prompts/plans to stderr. Use `term.IsTerminal(int(os.Stdin.Fd()))` only in `main`; pass terminal state inward. Map setup results to the exact JSON shape and render detailed human setup output without changing other commands.
- [ ] **Step 4: Verify GREEN:** run all `cmd/pact` tests and `env -u GOROOT mise exec -- go mod tidy`; expect PASS and a tidy module.
- [ ] **Step 5: Commit:** stage the five files and module sums; commit `feat: add setup command`.

### Task 4: Add permanent and live scenarios

**Files:**
- Create: `scenarios.jsonl`
- Create: `cmd/pact/setup_e2e_test.go`

**Interfaces:**
- `scenarios.jsonl` records each design scenario with `name`, `description`, `given`, `when`, `then`, and `validates` fields.
- `TestSetupBinaryScenarios` builds and executes the real CLI in isolated temporary directories without mocks.

- [ ] **Step 1: Write the failing compiled-binary E2E test** for every non-terminal shared scenario, including fresh setup, rerun, all partial states, conflicts, JSON errors, corrupt state, secret-safe output, and process-level concurrency. The injected-I/O integration tests from Task 3 cover terminal prompts and confirmation. Add matching records for all ten scenarios to `scenarios.jsonl`.
- [ ] **Step 2: Verify RED against the pre-feature base** by temporarily running the test with the setup commits reverted in a disposable patch or worktree; expect `unknown command: setup`. Restore the implementation, then run the same test and expect PASS.
- [ ] **Step 3: Confirm canonical coverage.** Verify the `go test ./...` in `scripts/check` runs the E2E test; do not change the script when it already does.
- [ ] **Step 4: Run full gates:** `env -u GOROOT mise exec -- ./scripts/check`, `env -u GOROOT mise exec -- go test -race ./...`, `env -u GOROOT mise exec -- go vet ./...`, and `env -u GOROOT mise exec -- golangci-lint run --timeout=10m`. Expect zero failures, warnings, or lint issues.
- [ ] **Step 5: Run one live `.scratch/` scenario** by building `./cmd/pact`, invoking fully flagged `setup --json`, rerunning it, and invoking `verify --strict --json`; assert JSON with `jq` and leave `.scratch/` ignored.
- [ ] **Step 6: Fresh-eyes review and commit:** inspect every changed file for path races, overwrite paths, leaks, and partial-state errors; fix with a failing test first. Commit `test: cover setup scenarios`.
