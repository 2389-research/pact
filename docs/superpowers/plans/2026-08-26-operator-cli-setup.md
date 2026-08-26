<!-- ABOUTME: Breaks the approved five-step setup command into small typed TDD slices. -->
<!-- ABOUTME: Closes publication, alias-locking, concurrency, and writer-failure gaps from the rejected plan. -->

# PACT Operator CLI Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development to implement this plan task-by-task.
> Every production slice also requires superpowers:test-driven-development.

**Goal:** Ship `pact setup` as a resumable, conflict-safe, five-step setup
command that is pleasant in a terminal and deterministic in automation.

**Architecture:** A small typed `internal/setup` service owns observation,
planning, and application. The existing standard-library command adapter owns
flags, terminal prompts, confirmation, and rendering. Store, identity, trust,
and index packages report publication truth so setup can return completed
actions even when a later durability, cleanup, or output write fails.

**Tech Stack:** Go 1.26, the existing standard-library CLI catalog,
`golang.org/x/term v0.45.0`, `github.com/creack/pty v1.1.24` for real PTY tests,
real Ed25519 keys, real SQLite indexes, and the repository's `scripts/check`
gate. `go list -m -json github.com/creack/pty@latest` resolved `v1.1.24` on
2026-08-26; pin that version only when the PTY test first needs it.

## Progress

**State:** Tasks 1-7 are implemented and independently approved on
`wip/setup-cli`. Setup is the canonical bootstrap; fresh compiled-command
dogfood and every final gate pass. Requirements and fresh-eyes reviewers
approved the whole branch after all Critical and Important findings were fixed
through TDD.

**Delivered commits:**

- Task 1: `753cc08`, with review repair `2c51ceb`;
- Task 2: `91b0ea6`, with safety repairs `b9eb30c`, `3661ea7`, and `ac3a414`;
- Task 3: `1f35696`;
- Task 4: `43e32a3`, with convergence repairs `9e09dd6` and `0db1567`;
- Task 5: `dbde27b`, with partial-result repair `e3f7172`;
- Task 6: `99f1e52`, with terminal-safety repairs `843eaf3` and `1a541e4`.

**Remaining work:** Land this exact closeout set. No setup implementation work
remains, and the branch must not merge without Doctor Biz's explicit choice.

## Authority and Scope

The approved contract is
[`docs/superpowers/specs/2026-08-25-operator-cli-design.md`](../specs/2026-08-25-operator-cli-design.md).
This plan implements only its setup slice:

```text
pact setup [--repo PATH] [--namespace NAMESPACE] [--actor ACTOR]
           [--key-file PATH] [--json]
```

Setup performs exactly five ordered actions:

1. create or accept the store;
2. create or accept the external signing key;
3. create or accept the local trust root;
4. run strict canonical verification;
5. create, rebuild, or accept the derived index.

It creates no event, commit, or checkpoint. The older
`docs/plans/setup-cli/design.md` and all `setup-cli/omakase/*` branches are
evidence, not implementation authority.

## Non-Negotiable Gates

- Run Go, tests, hooks, and commits through
  `env -u GOROOT mise exec -- ...` on this machine.
- Follow strict red-green-refactor TDD for every behavior. Record the failing
  test command and its expected reason in the task review.
- Change a domain signature and all of its callers in the same compiling task.
- Directory locks must serialize repository aliases by inode for both initial
  creation and later mutation. A path digest in a temporary directory is not
  an acceptable lock.
- A conflict is convergent only when a typed owner result identifies a clean
  no-overwrite collision and reopening proves the requested state. Arbitrary
  errors never become `existing`.
- Cleanup errors retain operation-error identity. Error unwrapping never
  includes a nil member.
- Every successful publication appears in setup's ordered completed actions,
  even when durability, cleanup, or result rendering fails afterward.
- Key bytes and the `private_key` field never appear in plans, results,
  diagnostics, logs, or repository files.
- Unit, integration, compiled-binary end-to-end, and real-PTY tests are all
  required. Production code gets no mock mode.
- `scripts/check`, `go test -race ./...`, writer-failure tests, alias
  concurrency tests, and compiled-command dogfood must all pass before the
  branch is complete.

## Stable Types Between Tasks

Task 1 changes store initialization to a typed result and uses the repository
or store directory descriptor as the lock object:

```go
package store

type InitStatus string

const (
	InitCreated  InitStatus = "created"
	InitConflict InitStatus = "conflict"
)

type InitResult struct {
	Store  *Store
	Status InitStatus
}

type LockError struct {
	Operation error
	Release   error
}

func (err *LockError) Error() string
func (err *LockError) Unwrap() []error

func Init(repo, namespace string, now time.Time) (InitResult, error)
```

`InitResult{Status: InitConflict}` accompanies only
`store.ErrAlreadyInitialized`. Once the `.pact` rename succeeds, `Store` and
`InitCreated` remain populated even if repository sync or lock release fails.
`LockError.Unwrap` returns a slice containing only non-nil errors.

Task 2 changes identity and trust owners in the same way:

```go
package identity

type GenerateStatus string

const (
	GenerateCreated  GenerateStatus = "created"
	GenerateConflict GenerateStatus = "conflict"
)

type GenerateResult struct {
	Key    *KeyFile
	Status GenerateStatus
}

func NormalizeActor(actor string) (string, error)
func ValidateSigningKeyPath(path, projectRoot string) (string, error)
func GenerateKeyFile(path, actor string, now time.Time) (GenerateResult, error)
```

`ValidateSigningKeyPath` validates a not-yet-created path as well as an
existing file. It returns the absolute lexical target and rejects lexical or
resolved containment in the project. `GenerateConflict` accompanies only the
existing-target no-overwrite error. Once the link publishes the key,
`GenerateResult.Key` and `GenerateCreated` remain populated through parent
sync or temporary cleanup errors.

```go
package ledger

type RootStatus string

const (
	RootCreated  RootStatus = "created"
	RootExisting RootStatus = "existing"
)

type RootResult struct {
	Root   Root
	Status RootStatus
}

func AddRoot(st *store.Store, key *identity.KeyFile, now time.Time) (RootResult, error)
```

`RootExisting` is returned only after matching key ID and public bytes.
`RootCreated` remains populated when `trust.json` was renamed but a later sync
or lock release failed.

Task 2 also makes the existing index result truthful after rename:

```go
func (m *Manager) Rebuild(ctx context.Context) (RebuildResult, error)
```

The signature stays unchanged. `Created` or `Replaced` is set immediately
after the live-index rename and remains set on later sync, validation,
unchanged-source, cleanup, or lock-release errors.

Task 3 creates the setup boundary:

```go
package setup

type Request struct {
	Repo      string
	Namespace string
	Actor     string
	KeyFile   string
	Now       time.Time
}

type ActionName string
type ActionStatus string

const (
	ActionStore  ActionName = "store"
	ActionKey    ActionName = "key"
	ActionTrust  ActionName = "trust"
	ActionVerify ActionName = "verify"
	ActionIndex  ActionName = "index"
)

type Action struct {
	Name   ActionName   `json:"name"`
	Status ActionStatus `json:"status"`
}

type Plan struct {
	Repo      string
	Namespace string
	Actor     string
	KeyFile   string
	Actions   []Action
}

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

type ApplyError struct {
	Result Result
	Err    error
}

func (err *ApplyError) Error() string
func (err *ApplyError) Unwrap() error
func Inspect(ctx context.Context, request Request) (Plan, error)
func Apply(ctx context.Context, request Request) (Result, error)
```

`Inspect` is read-only. `Apply` re-observes under each owner lock instead of
trusting an old plan. Results always order store, key, trust, verify, index.
Allowed statuses are:

| Action | Statuses |
| --- | --- |
| store | `created`, `existing` |
| key | `created`, `existing` |
| trust | `created`, `existing` |
| verify | `valid` |
| index | `created`, `rebuilt`, `current` |

`ApplyError.Result.Actions` holds every action proven complete before the
error. It never claims the failing action unless that action's owner reports
that its publication completed.

The command runtime gains only injectable process facts:

```go
type runConfig struct {
	// existing fields...
	StdinTerminal bool
	Now           func() time.Time
}
```

`normalizedRunConfig` supplies `time.Now` when `Now` is nil. `main` detects
stdin with `term.IsTerminal`. There is no second runtime path.

## Result Contract

Successful JSON is one newline-terminated object:

```json
{
  "operation": "setup",
  "ok": true,
  "status": "ready",
  "repo": "/absolute/project",
  "store": "/absolute/project/.pact",
  "namespace": "org/example/widget",
  "actor": "Alice",
  "key_file": "/absolute/operator.key.json",
  "key_id": "ed25519:sha256:...",
  "actions": [
    {"name":"store","status":"created"},
    {"name":"key","status":"created"},
    {"name":"trust","status":"created"},
    {"name":"verify","status":"valid"},
    {"name":"index","status":"created"}
  ]
}
```

Cancellation after a prompt uses `status: "cancelled"`, `ok: true`, no key ID,
and no actions. It exits 0 and writes nothing. Setup request errors exit 2;
domain error mapping remains unchanged; unexpected and writer failures exit
10. A domain failure uses the existing error envelope and includes the partial
setup result in `details`.

## Task 1: Make Store Publication and Locks Truthful

**Files:**

- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`
- Modify every direct `store.Init` caller found by
  `rg -n 'store\.Init\(|\bInit\(' internal cmd tests --glob '*.go'`

- [x] **Step 1: Add failing typed-publication and error-identity tests**

Add tests that prove:

- a clean first init returns `InitCreated` and a non-nil store;
- an existing destination returns `InitConflict` with
  `ErrAlreadyInitialized`;
- failure after rename returns `InitCreated`, the published store, and the
  injected error;
- operation plus unlock plus close errors all satisfy `errors.Is`;
- release-only failure does not expose a nil operation through `Unwrap`;
- a callback failure remains the operation cause and is never classified as a
  conflict.

Run:

```sh
env -u GOROOT mise exec -- go test ./internal/store -run 'TestInitResult|TestLockError' -count=1
```

Expected red: `Init` still returns `*Store`, and no typed result/error exists.

- [x] **Step 2: Replace temporary path locks with directory-descriptor locks**

Open and flock the resolved repository directory for init. Open and flock the
real `.pact` directory for read and mutation operations. Keep lock release in
one helper. Remove the hashed temporary lock path and its private lock
directory only after no caller uses them.

Do not silently ignore `Close` after a failed `Flock`; join it only when it is
non-nil.

- [x] **Step 3: Return typed init and lock outcomes**

Set `InitCreated` and the final `Store` immediately after the `.pact` rename.
Set `InitConflict` only where `checkStoreDestination` or the rename proves a
no-overwrite collision. Compose operation and release failures with
`LockError`, filtering nil members from `Unwrap`.

- [x] **Step 4: Update all callers in the same compiling step**

Every caller that needs the store reads `result.Store`; `pact init` converts
`InitConflict` back to the existing exit-3 behavior. Tests that assert a
second init refusal assert both status and error identity. Search direct,
test, command, and fixture callers separately before running the package set.

- [x] **Step 5: Add separate real-filesystem alias concurrency tests**

Use a real directory plus symlink alias and start barriers, not sleeps:

- two `Init` calls through canonical and alias paths yield exactly one
  `created` and one typed `conflict`; reopen proves byte-identical valid
  `format.json` and `trust.json`;
- two `WithMutationLock` callbacks through stores opened by canonical and
  alias paths never overlap; both mutations persist in their serialized order.

These are separate tests because init locks the repository inode while later
mutation locks the `.pact` inode.

Run:

```sh
env -u GOROOT mise exec -- go test ./internal/store ./internal/identity ./internal/ledger ./internal/index ./internal/status ./cmd/pact -count=1
env -u GOROOT mise exec -- go test -race ./internal/store -run 'TestInitSerializesAliases|TestMutationLockSerializesAliases' -count=20
```

- [x] **Step 6: Review and commit**

Run a requirements review, then a code-quality review. Fix all findings and
commit only the inspected paths:

```sh
git status --short
git add internal/store internal/identity internal/ledger internal/index internal/status cmd/pact
env -u GOROOT mise exec -- git commit -m "refactor: report store publication outcomes"
```

## Task 2: Make Key, Trust, and Index Outcomes Truthful

**Files:**

- Modify: `internal/identity/keyfile.go`
- Modify: `internal/identity/keyfile_test.go`
- Modify: `internal/ledger/trust.go`
- Modify: `internal/ledger/trust_test.go`
- Modify: `internal/index/rebuild.go`
- Modify: `internal/index/rebuild_test.go`
- Modify every `identity.GenerateKeyFile` and `ledger.AddRoot` caller found in
  separate production and test searches

- [x] **Step 1: Add failing key normalization and path-preflight tests**

Prove NFC/trim normalization, empty and over-255-rune refusal, external missing
target acceptance, existing external target acceptance, and lexical or
symlink-resolved project containment refusal. The returned path must be
absolute and must never print key bytes.

Run:

```sh
env -u GOROOT mise exec -- go test ./internal/identity -run 'TestNormalizeActor|TestValidateSigningKeyPath' -count=1
```

Expected red: the exported validators do not exist.

- [x] **Step 2: Add failing key publication tests**

Inject failures after link publication and during temporary cleanup. Prove the
result stays `GenerateCreated` with a loadable 0600 key and the original error
identity. Prove an existing target is `GenerateConflict`, remains byte-for-byte
unchanged, and a random I/O failure has no conflict status.

- [x] **Step 3: Implement typed key outcomes and update all callers**

Have the internal writer return whether its link succeeded. Normalize the
actor once and reuse it. Preflight the key path before random generation. On a
clean link collision, leave reopening and requested-state comparison to setup.
Update command and test callers in this same compiling step.

- [x] **Step 4: Add failing trust publication tests**

Prove matching public bytes return `RootExisting`, conflicting public bytes
remain an integrity error, and a post-rename sync failure returns
`RootCreated` plus the original error. Exercise operation plus lock-release
failure so the created root remains visible in the result.

- [x] **Step 5: Implement typed root outcomes and update all callers**

Set `RootCreated` when `WriteLocalJSON` reports that replacement was published;
set `RootExisting` only after key ID and public bytes match. To support this,
make the store's atomic local writer expose a typed published error without
changing its public call shape. Do not infer publication from error text.

- [x] **Step 6: Preserve index publication flags after rename**

Add tests for post-rename directory-sync and published-validation failures.
After either error, `Created` or `Replaced` must match the pre-rename live-file
state, and reopening `Status` must classify the actual final bytes. Set these
flags immediately after rename; fill the full `Status` only after validation.

- [x] **Step 7: Compile every affected package, review, and commit**

```sh
env -u GOROOT mise exec -- go test ./internal/identity ./internal/ledger ./internal/index ./internal/status ./cmd/pact ./tests/e2e -count=1
git status --short
git add internal/identity internal/ledger internal/index internal/store cmd/pact tests
env -u GOROOT mise exec -- git commit -m "refactor: preserve setup publication facts"
```

The review must rerun four separate searches: direct calls, interface/type
uses, string references, and tests. This closes the first rejected-plan
finding instead of trusting one grep.

## Task 3: Build the Read-Only Setup Inspector and Plan

**Files:**

- Create: `internal/setup/setup.go`
- Create: `internal/setup/setup_test.go`

- [x] **Step 1: Add failing request-normalization tests**

Table-test missing repository, namespace, actor, key path, nil context, bad
namespace, unsafe key path, and actor normalization. `Inspect` must reject the
request without creating the repo, `.pact`, key parent, key file, trust change,
or index.

Run:

```sh
env -u GOROOT mise exec -- go test ./internal/setup -run 'TestInspectRejectsInvalidRequestWithoutWrites' -count=1
```

Expected red: package `internal/setup` does not exist.

- [x] **Step 2: Add failing observation matrix tests**

With real files and owner APIs, cover:

- fresh: all five actions need work;
- complete: store/key/trust exist, strict verify is valid, index is current;
- partial after each of the first four completed steps;
- store namespace conflict;
- key actor conflict, malformed key, unsafe mode, and project-contained key;
- trust public-byte conflict;
- corrupt canonical state;
- missing, stale, corrupt, incompatible, and partial-build indexes.

Assert plan order and exact planned status. A current index plans no rebuild;
every other index state plans a rebuild. Snapshot every observed file before
and after `Inspect` to prove no writes.

- [x] **Step 3: Implement normalization and observation**

Keep unexported typed observed state in `internal/setup`. Use only owner APIs:
`store.Open`, `DefaultNamespace`, identity validators/loaders, `ledger.Roots`,
`ledger.VerifyContext(..., true)`, and `index.Manager.Status`. Missing state is
distinct from corrupt or conflicting state.

The repository may not exist yet; normalize its absolute lexical path without
creating it. A store opened through an alias reports the owner's resolved root
in the plan.

- [x] **Step 4: Implement deterministic planning**

Build exactly five ordered entries. The plan contains paths, actor, namespace,
and action intent, never key material. Equal filesystem state and request must
produce `reflect.DeepEqual` plans.

- [x] **Step 5: Verify, review, and commit**

```sh
env -u GOROOT mise exec -- go test ./internal/setup -count=1
env -u GOROOT mise exec -- go test -race ./internal/setup -count=1
git status --short
git add internal/setup
env -u GOROOT mise exec -- git commit -m "feat: add setup inspection plan"
```

## Task 4: Apply the Five Steps and Converge Safely

**Files:**

- Modify: `internal/setup/setup.go`
- Modify: `internal/setup/setup_test.go`

- [x] **Step 1: Add a failing fresh-and-rerun integration test**

Use a fixed time. First apply must return:

```text
store created
key created
trust created
verify valid
index created
```

Save bytes for the key, `format.json`, `trust.json`, all canonical files, and
the live index. The exact rerun must return:

```text
store existing
key existing
trust existing
verify valid
index current
```

Every saved file must remain byte-for-byte identical.

- [x] **Step 2: Implement ordered application with truthful partial results**

For each action:

- call the owner operation;
- append a completed action only from its typed result;
- if a clean collision occurs, reopen and validate namespace or actor before
  appending `existing`;
- on any other error, return `*ApplyError` with completed actions and the
  original cause;
- run strict verification before touching the index;
- call `Status`; return `current` without rebuilding only for current state;
  otherwise call `Rebuild` and map `Created` to `created`, `Replaced` to
  `rebuilt`.

Do not hold one outer mutation lock around owner operations; each owner locks
the smallest valid publication unit and rechecks state under that lock.

- [x] **Step 3: Add partial-resume and conflict tests**

Start from each partial boundary and prove only missing actions change. Test
namespace, actor, key safety, trust bytes, corrupt store, failed strict verify,
and failed index rebuild. Snapshot winner bytes in every conflict test.

- [x] **Step 4: Add publication-failure tests at every mutable owner**

Inject store post-rename, key post-link, trust post-rename, and index
post-rename failures. For each, assert:

- the returned error satisfies the injected cause;
- `ApplyError.Result.Actions` includes the published action and no later one;
- reopening validates the published bytes;
- a clean rerun converges without changing those bytes.

These service-boundary tests are the feasible proof missing from the rejected
plan. Command-boundary writer failures are a separate Task 5 concern.

- [x] **Step 5: Add identical and conflicting concurrency tests**

Run two setup applications through canonical and symlinked repository paths
with barriers at owner publication points:

- identical requests both succeed; combined statuses show one creator and one
  accepter for store/key/trust, both verify valid, and the final index is
  current;
- conflicting namespace or actor requests yield one success and one typed
  conflict; the winner's format, key, trust, and index bytes remain valid and
  are never overwritten.

Assert final bytes and both result status sequences. Do not accept “no race
detector report” as proof of semantic convergence.

- [x] **Step 6: Verify, review, and commit**

```sh
env -u GOROOT mise exec -- go test ./internal/setup -count=1
env -u GOROOT mise exec -- go test -race ./internal/setup -run 'TestApplyConcurrent' -count=20
git status --short
git add internal/setup
env -u GOROOT mise exec -- git commit -m "feat: apply resumable ledger setup"
```

## Task 5: Add the Catalog Command, Automation Output, and Writer Safety

**Files:**

- Modify: `cmd/pact/command_catalog.go`
- Modify: `cmd/pact/stdlib_adapter.go`
- Modify: `cmd/pact/main.go`
- Create: `cmd/pact/setup_command.go`
- Create: `cmd/pact/setup_render.go`
- Create: `cmd/pact/setup_command_test.go`
- Modify: `cmd/pact/operator_cli_test.go`

- [x] **Step 1: Add failing catalog and nonterminal tests**

Prove help lists setup under **Get started** from the catalog and documents all
five flags. Fully flagged `--json` and redirected invocations do not read
stdin, do not prompt, and return the exact five-action result. Missing
namespace, actor, or key path exits 2 before any filesystem write.

`--repo` defaults to the working directory for setup only; explicit `--repo`
remains authoritative and uses `repositoryCreate`.

- [x] **Step 2: Wire the typed command handler**

Parse setup-local flags with a silent `flag.FlagSet`. Add `StdinTerminal` and
`Now` to `runConfig`; `main` supplies the real terminal fact and clock. The
handler calls `setup.Inspect` only for an interactive prompted path and
`setup.Apply` for execution.

- [x] **Step 3: Add exact human and JSON renderers**

Human output uses the selected terminal style: one clear heading, aligned
five-step progress, restrained color from the existing presentation policy,
absolute repo/key paths, and a final “ready” line. Meaning must survive no
color and width 60. JSON is produced from a typed map conversion and contains
no ANSI bytes.

An `ApplyError` renders the existing safe diagnostic plus partial setup details
without private bytes. Do not print an action as complete unless it exists in
the service result.

- [x] **Step 4: Add failed-writer tests at the application boundary**

Test both human and JSON with a writer that fails after accepting enough bytes
to publish part of the result. Assert exit 10 after durable setup state exists.
Call the setup handler directly with an in-memory result sink or extract its
typed conversion helper to assert all five completed actions after output
publication fails.

Also test stderr failure while reporting the stdout failure. The command still
returns 10 and does not invent a diagnostic on the failed stream. Cover prompt
and plan write failure separately in Task 6 because those must happen before
mutation.

- [x] **Step 5: Verify, review, and commit**

```sh
env -u GOROOT mise exec -- go test ./cmd/pact -run 'TestSetup|TestHelp.*Setup' -count=1
env -u GOROOT mise exec -- go test ./cmd/pact ./internal/setup -count=1
git status --short
git add cmd/pact
env -u GOROOT mise exec -- git commit -m "feat: add automated setup command"
```

## Task 6: Add Real Terminal Prompts and Compiled Scenarios

**Files:**

- Modify: `cmd/pact/setup_command.go`
- Modify: `cmd/pact/setup_render.go`
- Modify: `cmd/pact/setup_command_test.go`
- Create: `tests/e2e/setup_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [x] **Step 1: Add failing prompt state-machine tests**

Only when stdin is a terminal, prompt in order for missing namespace, actor,
and key path. Existing observed values may be offered as defaults for explicit
confirmation; fresh state shows a short valid example but empty input remains
missing. Bound each line read to 64 KiB and reject overlong input as usage.

If any prompt occurred, render one complete plan to stderr and ask one final
case-insensitive `[y/N]` confirmation. `y` and `yes` continue. Empty, `n`, or
`no` returns the typed cancelled result, exits 0, and writes nothing.

- [x] **Step 2: Prove all pre-mutation writer failures**

Inject a stderr writer failure during each prompt, during the plan, and during
confirmation. Every case exits 10 and leaves repo, key, trust, and index
absent or byte-identical. No fallback prompt is written to stdout.

- [x] **Step 3: Implement the smallest prompt layer**

Keep prompt parsing in `cmd/pact`; do not put terminal facts or readers in
`internal/setup`. Use one bounded reader for the interaction. Render the plan
from the same `setup.Plan` that the confirmation approves, then let `Apply`
re-observe state for concurrency safety.

- [x] **Step 4: Add compiled-binary and real-PTY coverage**

Pin `github.com/creack/pty v1.1.24` as a test-only dependency. Start the built
binary under a real PTY and assert prompt order, one plan, one confirmation,
cancel-without-writes, and accepted five-step success. Close the PTY on all
paths and use context deadlines, never sleeps, to prevent hangs.

In ordinary compiled-process tests cover:

- fresh fully flagged JSON setup;
- exact byte-stable rerun;
- partial resume;
- conflict without overwrite;
- missing nonterminal input and unsafe key path;
- corrupt local state;
- identical and conflicting processes using canonical and symlink paths;
- no `private_key` under the repo or in captured stdout/stderr.

- [x] **Step 5: Verify, review, and commit**

```sh
env -u GOROOT mise exec -- go test ./cmd/pact ./tests/e2e -run 'TestSetup' -count=1
env -u GOROOT mise exec -- go test -race ./cmd/pact ./internal/setup ./tests/e2e -run 'TestSetup|TestApplyConcurrent' -count=10
git status --short
git add cmd/pact tests/e2e go.mod go.sum
env -u GOROOT mise exec -- git commit -m "test: prove interactive setup lifecycle"
```

## Task 7: Dogfood, Document, and Close the Branch

**Files:**

- Modify: `scripts/check`
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-08-25-operator-cli-design.md`
- Modify: `docs/superpowers/plans/2026-08-26-operator-cli-setup.md`
- Modify: `gotchas.md`
- Create: `docs/status/operator-cli-setup.md`

- [x] **Step 1: Make setup part of the canonical lifecycle**

Replace the separate init/keygen/trust-add bootstrap in `scripts/check` with
one fully flagged `pact setup --json` call. Assert five ordered statuses and a
current index. Keep existing commit/query/verify behavior and the repository
secret scan.

- [x] **Step 2: Update operator documentation**

README shows automated and guided setup, states that keys live outside the
project, and explains safe reruns. Mark only the setup slice complete in the
design. Update this plan's progress with commit IDs and exact remaining work.
Replace the obsolete setup-deferred gotcha with the shipped behavior; edit
that entry in place rather than appending a contradiction.

- [x] **Step 3: Dogfood the compiled command**

Build one binary in a fresh temporary directory. Run fresh and rerun setup,
compare store/key/trust/canonical/index bytes, verify 0600 key mode, run strict
verify and index status, repeat through a symlink alias, and scan all captured
output plus repository files for `private_key` and base64 private seed bytes.
Record commands and durable IDs/digests in
`docs/status/operator-cli-setup.md`; never record the private key bytes.

- [x] **Step 4: Run every final gate from a clean status**

```sh
env -u GOROOT mise exec -- gofmt -w <only changed Go files>
env -u GOROOT mise exec -- go test ./...
env -u GOROOT mise exec -- go test -race ./...
env -u GOROOT mise exec -- ./scripts/check
env -u GOROOT mise exec -- pre-commit run --all-files
env -u GOROOT mise exec -- go build ./cmd/pact
git status --short
```

Use a fresh `GOCACHE` and `GOLANGCI_LINT_CACHE` if the lint cache reports paths
from another clone. Do not dismiss any warning as noise without proving it
predates this branch.

- [x] **Step 5: Run independent whole-branch review**

Request a requirements review against the approved design and this plan, then
a fresh-eyes code review of `wip/operator-cli...HEAD`. The reviewers must
explicitly answer:

1. Were all callers updated in each signature-changing commit?
2. Can any ordinary error be mistaken for convergence?
3. Do init and mutation alias tests prove final bytes and statuses?
4. Do human and JSON writer failures return 10 while preserving completed
   actions after durable publication?
5. Is the fifth derived-index action implemented and current on success?

Fix every Critical and Important finding with TDD, rerun all gates, and ask the
reviewers to verify the fixes.

- [x] **Step 6: Commit the closeout**

```sh
git status --short
git add scripts/check README.md docs/superpowers/specs/2026-08-25-operator-cli-design.md docs/superpowers/plans/2026-08-26-operator-cli-setup.md docs/status/operator-cli-setup.md gotchas.md
env -u GOROOT mise exec -- git commit -m "docs: record operator setup delivery"
git status --short --branch
```

The branch is complete only when status is clean and every gate above passes.
Do not merge without Doctor Biz's explicit choice.
