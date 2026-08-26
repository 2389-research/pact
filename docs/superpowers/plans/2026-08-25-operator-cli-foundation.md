<!-- ABOUTME: Breaks the operator CLI foundation into shared TDD work, an Omakase trial, and winner hardening. -->
<!-- ABOUTME: Ends with a reviewed help, discovery, status, and terminal-presentation slice ready for setup work. -->

# PACT Operator CLI Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a reviewed operator CLI foundation with real help, ancestor
repository discovery, strict `pact status`, and polished terminal output, while
selecting the command architecture through one bounded Omakase trial.

**Architecture:** Add the framework-neutral store and status read APIs on the
base branch. Then run one compiled-binary contract against standard-library,
Cobra, and Kong command adapters in isolated worktrees. Fast-forward only the
winner, harden its rendering and writer-failure behavior, and leave setup plus
the remaining operator renderers for follow-on plans against the selected
code.

**Tech Stack:** Go 1.26, existing `flag` handlers and domain packages,
`golang.org/x/term v0.45.0`, optional trial dependencies
`github.com/spf13/cobra v1.10.2` and `github.com/alecthomas/kong v1.16.1`, real
Ed25519 keys, real SQLite indexes, Go tests, compiled-binary end-to-end tests,
and the repository's `scripts/check` gate.

**Dependency evidence:** On 2026-08-25, `go list -m -json ...@latest` resolved
the versions above. Cobra's official releases list `v1.10.2`; Kong's official
repository documents command help, dynamic commands, and passthrough; the
official `x/term` package documents `IsTerminal` and `GetSize`. Sources:
[Cobra releases](https://github.com/spf13/cobra/releases),
[Kong](https://github.com/alecthomas/kong), and
[`x/term`](https://pkg.go.dev/golang.org/x/term).

## Progress

**State:** Planned. No implementation task has started.

**Next step:** Task 1, followed by Task 2 and the shared trial contract before
any command-adapter worktree is created.

## Global Constraints

- Run Go and git-hook commands through `env -u GOROOT mise exec -- ...`; the
  inherited `GOROOT` is stale on this machine.
- Follow strict TDD: add one failing behavior test, observe the expected
  failure, write the smallest implementation, observe the pass, then commit.
- Every hand-written Go, shell, and Markdown file starts with its required
  two-line `ABOUTME` comment after any shebang.
- Existing explicit commands keep JSON fields, ordering, query default 100,
  resource bounds, and exit-code meanings.
- `--json` and non-terminal operation never prompt and never emit ANSI bytes.
- Only presentation may vary with terminal state; command data and semantics
  may not.
- `status`, help, and repository discovery perform no canonical or derived
  writes.
- `--repo PATH` opens that exact project path; discovery applies only when the
  flag is absent.
- Human meaning never depends on color or Unicode symbols.
- All output writes and flushes propagate failure; a renderer failure returns
  exit code 10.
- Use real files, keys, indexes, and processes in integration and end-to-end
  tests. Do not add mocks or mock modes to production code.
- Use `scripts/check` as the canonical gate. Add no new warnings, ignored test
  output, skipped contract tests, or hook bypasses.
- Keep the existing setup Omakase branches and worktrees untouched.

## Required Execution Skills

- `superpowers:subagent-driven-development` for task execution and two-stage
  review, unless Doctor Biz selects inline execution.
- `superpowers:test-driven-development` before each production behavior slice;
  `superpowers:systematic-debugging` for any unexpected failure.
- `superpowers:using-git-worktrees` before Task 4 worktree creation.
- `omakase-off`, `scenario-testing`, `fresh-eyes-review`, and `judge` for the
  bounded three-candidate trial.
- `requesting-code-review` and `verification-before-completion` for Task 7.

## Phase Boundary

This plan implements the first independently useful slice from the approved
[operator CLI design](../specs/2026-08-25-operator-cli-design.md): command
metadata, help, discovery, status, presentation policy, and diagnostics needed
by that slice. It deliberately stops before guided setup and before the full
log, query, and show visual pass. Those plans depend on the winning command
adapter and must not guess its file structure in advance.

The shared APIs, failing tests, external results, commands, and pass gates are
exact below. Task 4 does not prescribe identical parser-wiring code because
that would erase the architectural difference the approved Omakase trial is
meant to measure. Each candidate must still use TDD and satisfy every named
interface and assertion.

## File and Responsibility Map

Shared base work, present in every variant:

- Modify `internal/store/store.go`: expose the validated default namespace from
  the package that owns `format.json`.
- Modify `internal/store/store_test.go`: prove valid and corrupt namespace
  reads.
- Modify `internal/ledger/commit.go`: consume the store accessor instead of
  parsing `format.json` again.
- Modify `internal/ledger/verify.go`: add a context-aware verification entry
  point while preserving `Verify`.
- Modify `internal/ledger/verify_test.go`: prove cancellation identity.
- Create `internal/status/status.go`: compose strict verification and index
  inspection into one typed operator result.
- Create `internal/status/status_test.go`: cover healthy, attention, broken,
  and cancellation states with real repositories.
- Create `tests/e2e/operator_contract_test.go`: hold the shared black-box
  contract helper; each variant adds the one top-level test that enables it.
- Create `docs/plans/operator-cli/omakase/contract.md`: freeze variant inputs,
  pass gates, sample commands, and forbidden shortcuts.

The winning variant will own these focused command files. A variant may choose
one different adapter filename, but it must preserve these responsibilities:

- Create `cmd/pact/command_catalog.go`: the one command tree and its purpose,
  usage, groups, examples, repository mode, and handler.
- Create one of `cmd/pact/stdlib_adapter.go`, `cmd/pact/cobra_adapter.go`, or
  `cmd/pact/kong_adapter.go`: route the catalog through the candidate parser.
- Create `cmd/pact/runtime.go`: process streams, working directory, environment,
  terminal detection, width, and color precedence.
- Create `cmd/pact/repository.go`: ancestor discovery and authoritative
  `--repo` resolution.
- Create `cmd/pact/status_command.go`: adapt `internal/status` to compact JSON,
  existing exit classes, and typed human rendering.
- Create `cmd/pact/status_render.go`: render healthy, attention, and broken
  status without web-style cards.
- Create `cmd/pact/help_render.go`: render top-level, command, and nested help
  from the catalog.
- Create `cmd/pact/diagnostic_render.go`: render the fixed human diagnostic
  shape and safe next action.
- Modify `cmd/pact/app.go`: replace the command switch with the selected
  adapter and propagate every writer error.
- Modify `cmd/pact/main.go`: pass real process streams and terminal capability
  into the testable application boundary.
- Modify `cmd/pact/query_commands.go` and `cmd/pact/index_commands.go`: return
  human-rendering write failures rather than discarding them.
- Modify `cmd/pact/app_test.go`: retain old JSON assertions and add help,
  discovery, color, status, and failed-writer unit coverage.
- Create `cmd/pact/testdata/status/*.golden`: pin plain and colored winner
  output at widths 60, 80, and 120.
- Create `tests/e2e/operator_variant_test.go`: enable the shared contract on a
  variant and, later, on the winner.
- Modify `go.mod` and `go.sum`: pin `x/term` plus only the winning parser
  dependency, if any.
- Create `docs/plans/operator-cli/omakase/result.md`: record evidence, scores,
  winner, weaknesses, and cleanup.
- Modify `README.md`: document the delivered help, discovery, and status slice.

## Stable Interfaces Between Tasks

Task 1 produces:

```go
func (st *Store) DefaultNamespace() (string, error)

func VerifyContext(
	ctx context.Context,
	st *store.Store,
	strict bool,
) (VerifyResult, error)
```

Task 2 produces:

```go
package status

type Health string

const (
	HealthHealthy   Health = "healthy"
	HealthAttention Health = "attention"
	HealthBroken    Health = "broken"
)

type NextAction struct {
	Reason  string `json:"reason"`
	Command string `json:"command"`
}

type Result struct {
	Health           Health
	Repo             string
	Store            string
	DefaultNamespace string
	Verification     ledger.VerifyResult
	Index            *index.Status
	NextAction       *NextAction
}

func Inspect(ctx context.Context, st *store.Store) (Result, error)
```

Every Task 4 adapter consumes those APIs and produces this application
behavior:

```go
func run(args []string, stdout, stderr io.Writer) int

type runConfig struct {
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	WorkingDir     string
	StdoutTerminal bool
	StderrTerminal bool
	Width          int
	Environment    map[string]string
}

func runWithConfig(args []string, config runConfig) int
```

`run` remains the compact non-terminal test adapter used by existing tests.
`main` constructs the real `runConfig`. This is an internal contract, not a
second public CLI path.

---

### Task 1: Put Store Metadata and Contextual Verification in Their Owning Packages

**Files:**

- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`
- Modify: `internal/ledger/commit.go`
- Modify: `internal/ledger/verify.go`
- Modify: `internal/ledger/verify_test.go`

**Interfaces:**

- Consumes: current `Store.ReadLocal`, `decodeStrictJSON`, `validateNamespace`,
  and `scanWithReadLock`.
- Produces: `(*Store).DefaultNamespace` and `ledger.VerifyContext` with the
  signatures in “Stable Interfaces Between Tasks.”

- [ ] **Step 1: Write the failing store tests**

Add these focused tests to `internal/store/store_test.go`:

```go
func TestDefaultNamespaceReadsValidatedStoreMetadata(t *testing.T) {
	st, err := Init(t.TempDir(), "org/example/widget", time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := st.DefaultNamespace()
	if err != nil {
		t.Fatal(err)
	}
	if namespace != "org/example/widget" {
		t.Fatalf("default namespace = %q", namespace)
	}
}

func TestDefaultNamespaceRejectsCorruptConfiguredNamespace(t *testing.T) {
	st, err := Init(t.TempDir(), "org/example/widget", time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WriteLocalJSON("format.json", map[string]any{
		"format":              "pact/store/v1",
		"default_namespace":   "../escape",
		"created_at":          "2026-08-25T12:00:00Z",
		"canonicalization":    "pact-json-v1",
		"hash_algorithm":      "sha256",
		"signature_algorithm": "ed25519",
	}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DefaultNamespace(); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("DefaultNamespace() error = %v, want ErrIntegrity", err)
	}
}
```

- [ ] **Step 2: Run the store tests and observe the missing method**

Run:

```bash
env -u GOROOT mise exec -- go test ./internal/store -run 'TestDefaultNamespace' -count=1
```

Expected: compilation fails because `(*Store).DefaultNamespace` does not exist.

- [ ] **Step 3: Add the store-owned accessor**

Add this method after `Root` in `internal/store/store.go`:

```go
// DefaultNamespace returns the validated namespace stored in format.json.
func (st *Store) DefaultNamespace() (string, error) {
	if st == nil {
		return "", fmt.Errorf("store is required")
	}
	raw, err := st.ReadLocal("format.json")
	if err != nil {
		return "", err
	}
	var format map[string]any
	if err := decodeStrictJSON(raw, &format); err != nil || format["format"] != formatName {
		return "", fmt.Errorf("%w: malformed store format at %s", ErrIntegrity, filepath.Join(st.dir, "format.json"))
	}
	namespace, ok := format["default_namespace"].(string)
	if !ok || validateNamespace(namespace) != nil {
		return "", fmt.Errorf("%w: invalid default namespace at %s", ErrIntegrity, filepath.Join(st.dir, "format.json"))
	}
	return namespace, nil
}
```

Replace `defaultNamespace(st)` in `internal/ledger/commit.go` with
`st.DefaultNamespace()`, delete the now-duplicate helper, and remove the
`pact/internal/canonical` import that becomes unused.

- [ ] **Step 4: Run the store and commit tests**

Run:

```bash
env -u GOROOT mise exec -- go test ./internal/store ./internal/ledger -run 'TestDefaultNamespace|TestCommitDefaults' -count=1
```

Expected: PASS. Existing default-namespace commit behavior remains green.

- [ ] **Step 5: Write the failing contextual verification test**

Add to `internal/ledger/verify_test.go`:

```go
func TestVerifyContextPreservesCancellationIdentity(t *testing.T) {
	st, _ := ledgerStoreAndKey(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := VerifyContext(ctx, st, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifyContext() error = %v, want context.Canceled", err)
	}
}
```

- [ ] **Step 6: Run the verification test and observe the missing function**

Run:

```bash
env -u GOROOT mise exec -- go test ./internal/ledger -run TestVerifyContextPreservesCancellationIdentity -count=1
```

Expected: compilation fails because `VerifyContext` does not exist.

- [ ] **Step 7: Add `VerifyContext` and preserve `Verify`**

Replace the current `Verify` body in `internal/ledger/verify.go` with:

```go
// Verify scans every canonical object and evaluates its layered ledger state.
func Verify(st *store.Store, strict bool) (VerifyResult, error) {
	return VerifyContext(context.Background(), st, strict)
}

// VerifyContext evaluates the ledger while preserving caller cancellation.
func VerifyContext(ctx context.Context, st *store.Store, strict bool) (VerifyResult, error) {
	if st == nil || ctx == nil {
		return VerifyResult{}, fmt.Errorf("store and context are required")
	}
	scan, err := scanWithReadLock(ctx, st, ScanOptions{Strict: strict, Limits: Phase2Limits})
	if err != nil {
		return VerifyResult{}, err
	}
	return scan.Verification, nil
}
```

- [ ] **Step 8: Run the focused and canonical tests**

Run:

```bash
env -u GOROOT mise exec -- go test ./internal/store ./internal/ledger -count=1
env -u GOROOT mise exec -- ./scripts/check
```

Expected: both commands pass with no warnings.

- [ ] **Step 9: Commit Task 1**

```bash
git status --short
git add internal/store/store.go internal/store/store_test.go internal/ledger/commit.go internal/ledger/verify.go internal/ledger/verify_test.go
env -u GOROOT mise exec -- git commit -m "refactor: expose store status metadata"
```

### Task 2: Build the Framework-Neutral Status Read Model

**Files:**

- Create: `internal/status/status.go`
- Create: `internal/status/status_test.go`

**Interfaces:**

- Consumes: `Store.DefaultNamespace`, `ledger.VerifyContext`,
  `index.New(st).Status`, and existing `ledger.VerifyResult` and `index.Status`.
- Produces: `status.Health`, `status.NextAction`, `status.Result`, and
  `status.Inspect` exactly as declared above.

- [ ] **Step 1: Write failing real-store status tests**

Create `internal/status/status_test.go` with the required `ABOUTME` lines and
these tests. Use package `status` so the test can inspect only the exported
contract:

```go
// ABOUTME: Exercises the operator status model over real canonical stores and disposable indexes.
// ABOUTME: Proves healthy, attention, broken, and cancelled inspection without repair side effects.
package status

import (
	"context"
	"errors"
	"testing"
	"time"

	"pact/internal/index"
	"pact/internal/store"
)

func TestInspectClassifiesMissingAndCurrentIndex(t *testing.T) {
	st, err := store.Init(t.TempDir(), "org/example/widget", time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	attention, err := Inspect(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if attention.Health != HealthAttention || attention.Index == nil || attention.Index.Index.State != "missing" {
		t.Fatalf("missing-index status = %#v", attention)
	}
	if attention.NextAction == nil || attention.NextAction.Command != "pact index rebuild" {
		t.Fatalf("missing-index action = %#v", attention.NextAction)
	}

	if _, err := index.New(st).Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	healthy, err := Inspect(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if healthy.Health != HealthHealthy || healthy.Index == nil || healthy.Index.Index.State != "current" || healthy.NextAction != nil {
		t.Fatalf("current-index status = %#v", healthy)
	}
	if healthy.DefaultNamespace != "org/example/widget" || healthy.Verification.Strict != true || healthy.Verification.OK != true {
		t.Fatalf("healthy verification = %#v", healthy)
	}
}

func TestInspectPreservesCancellation(t *testing.T) {
	st, err := store.Init(t.TempDir(), "org/example/widget", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Inspect(ctx, st); !errors.Is(err, context.Canceled) {
		t.Fatalf("Inspect() error = %v, want context.Canceled", err)
	}
}
```

Add a third test in the same file that writes one digest-correct but invalid
canonical object through `st.PutCanonical`, calls `Inspect`, and asserts
`HealthBroken`, `Index == nil`, and `NextAction == nil`:

```go
func TestInspectStopsBeforeIndexWhenCanonicalVerificationFails(t *testing.T) {
	st, err := store.Init(t.TempDir(), "org/example/widget", time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutCanonical(map[string]any{"format": "pact/unknown/v1"}); err != nil {
		t.Fatal(err)
	}
	result, err := Inspect(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if result.Health != HealthBroken || result.Index != nil || result.NextAction != nil {
		t.Fatalf("broken status = %#v", result)
	}
}
```

- [ ] **Step 2: Run the tests and observe the missing package API**

Run:

```bash
env -u GOROOT mise exec -- go test ./internal/status -count=1
```

Expected: compilation fails because `Inspect`, the health constants, and result
types do not exist.

- [ ] **Step 3: Add the minimal typed status service**

Create `internal/status/status.go`:

```go
// ABOUTME: Composes strict ledger verification and disposable-index inspection for operators.
// ABOUTME: Returns typed health without rendering, prompting, repairing, or parsing canonical files.
package status

import (
	"context"
	"fmt"

	"pact/internal/index"
	"pact/internal/ledger"
	"pact/internal/store"
)

type Health string

const (
	HealthHealthy   Health = "healthy"
	HealthAttention Health = "attention"
	HealthBroken    Health = "broken"
)

type NextAction struct {
	Reason  string `json:"reason"`
	Command string `json:"command"`
}

type Result struct {
	Health           Health
	Repo             string
	Store            string
	DefaultNamespace string
	Verification     ledger.VerifyResult
	Index            *index.Status
	NextAction       *NextAction
}

func Inspect(ctx context.Context, st *store.Store) (Result, error) {
	if ctx == nil || st == nil {
		return Result{}, fmt.Errorf("status requires a context and store")
	}
	namespace, err := st.DefaultNamespace()
	if err != nil {
		return Result{}, err
	}
	verification, err := ledger.VerifyContext(ctx, st, true)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Health: HealthBroken, Repo: st.Root(), Store: st.Dir(),
		DefaultNamespace: namespace, Verification: verification,
	}
	if !verification.OK {
		return result, nil
	}
	indexStatus, err := index.New(st).Status(ctx)
	if err != nil {
		return Result{}, err
	}
	result.Index = &indexStatus
	if indexStatus.Index.State != "current" {
		result.Health = HealthAttention
		result.NextAction = &NextAction{
			Reason:  "indexed reads are not ready",
			Command: "pact index rebuild",
		}
		return result, nil
	}
	result.Health = HealthHealthy
	return result, nil
}
```

- [ ] **Step 4: Run focused tests and fix only contract mismatches**

Run:

```bash
env -u GOROOT mise exec -- go test ./internal/status -count=1
env -u GOROOT mise exec -- go test ./internal/store ./internal/ledger ./internal/index -count=1
```

Expected: PASS. If `PutCanonical` has a different existing signature, adapt the
test to that real signature; do not bypass the store API or add a test-only
write path.

- [ ] **Step 5: Run the race and canonical gates**

Run:

```bash
env -u GOROOT mise exec -- go test -race ./internal/status ./internal/store ./internal/ledger ./internal/index
env -u GOROOT mise exec -- ./scripts/check
```

Expected: PASS with no warnings.

- [ ] **Step 6: Commit Task 2**

```bash
git status --short
git add internal/status/status.go internal/status/status_test.go
env -u GOROOT mise exec -- git commit -m "feat: add operator status model"
```

### Task 3: Freeze the Shared Omakase Scenario Contract

**Files:**

- Create: `tests/e2e/operator_contract_test.go`
- Create: `docs/plans/operator-cli/omakase/contract.md`

**Interfaces:**

- Consumes: the compiled `pact` binary, existing E2E helpers in
  `tests/e2e/cli_test.go`, and the Task 2 status API indirectly through the
  command.
- Produces: `runOperatorCLIContract(t *testing.T)`. This helper is compiled on
  the base branch but has no top-level `Test` until each variant adds the
  identical enabling wrapper.

- [ ] **Step 1: Write the reusable black-box contract helper**

Create `tests/e2e/operator_contract_test.go`. It must use `exec.Command`, real
directories, existing `runJSON`, existing key commands, and a real index. The
helper must perform these exact assertions:

```go
// ABOUTME: Defines the compiled-binary contract shared by every operator CLI foundation candidate.
// ABOUTME: Uses real stores, keys, indexes, process exits, discovery, JSON, and terminal modes.
package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runOperatorCLIContract(t *testing.T) {
	root := projectRoot(t)
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "pact")
	buildBinary(t, root, binary)

	assertCleanHelp(t, runOperatorProcess(t, binary, workspace, nil))
	assertCleanHelp(t, runOperatorProcess(t, binary, workspace, nil, "--help"))
	assertCleanHelp(t, runOperatorProcess(t, binary, workspace, nil, "help"))
	assertCleanHelp(t, runOperatorProcess(t, binary, workspace, nil, "status", "--help"))

	typo := runOperatorProcess(t, binary, workspace, nil, "statsu")
	if typo.Code != 2 || typo.Stdout != "" || !strings.Contains(typo.Stderr, "status") {
		t.Fatalf("typo result = %#v", typo)
	}

	repo := filepath.Join(workspace, "project")
	keyPath := filepath.Join(workspace, "operator.key.json")
	mustMkdir(t, repo)
	runJSON(t, binary, "init", "--repo", repo, "--namespace", "org/example/widget", "--json")
	runJSON(t, binary, "keygen", "--actor", "Alice", "--out", keyPath, "--json")
	runJSON(t, binary, "trust-add", "--repo", repo, "--key-file", keyPath, "--json")
	runJSON(t, binary, "index", "rebuild", "--repo", repo, "--json")

	nested := filepath.Join(repo, "one", "two")
	mustMkdir(t, nested)
	healthy := runOperatorProcess(t, binary, nested, nil, "status", "--json")
	if healthy.Code != 0 || healthy.Stderr != "" {
		t.Fatalf("healthy status = %#v", healthy)
	}
	assertHealthyStatusJSON(t, healthy.Stdout, repo)

	plain := runOperatorProcess(t, binary, nested, nil, "status", "--color", "never")
	assertPlainHealthyStatus(t, plain, "org/example/widget")
	colored := runOperatorProcess(t, binary, nested, nil, "status", "--color", "always")
	if colored.Code != 0 || !strings.Contains(colored.Stdout, "\x1b[") || !strings.Contains(colored.Stdout, "Healthy") {
		t.Fatalf("forced-color status = %#v", colored)
	}
	dumb := runOperatorProcess(t, binary, nested, []string{"TERM=dumb"}, "status")
	if dumb.Code != 0 || strings.Contains(dumb.Stdout, "\x1b[") {
		t.Fatalf("TERM=dumb status = %#v", dumb)
	}

	notRepo := filepath.Join(nested, "explicit-empty")
	mustMkdir(t, notRepo)
	explicit := runOperatorProcess(t, binary, nested, nil, "status", "--repo", notRepo, "--json")
	if explicit.Code != 3 || explicit.Stdout != "" || !strings.Contains(explicit.Stderr, "pact setup") {
		t.Fatalf("authoritative --repo = %#v", explicit)
	}

	batch := writeBatch(t, workspace, "stale.json", "stale", "widget.stale")
	runJSON(t, binary, "commit", "--repo", repo, "--key-file", keyPath, "--events", batch, "--json")
	stale := runOperatorProcess(t, binary, nested, nil, "status", "--json")
	if stale.Code != 9 || stale.Stdout != "" {
		t.Fatalf("stale status = %#v", stale)
	}
	assertAttentionStatusJSON(t, stale.Stderr, repo, "stale", "pact index rebuild")

	corruptRepo := filepath.Join(workspace, "corrupt")
	copyTree(t, repo, corruptRepo)
	objects, err := filepath.Glob(filepath.Join(corruptRepo, ".pact", "objects", "sha256", "*", "*.json"))
	if err != nil || len(objects) == 0 {
		t.Fatalf("corrupt fixture objects = %v, %v", objects, err)
	}
	if err := os.WriteFile(objects[0], []byte(`{"corrupt":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := runOperatorProcess(t, binary, workspace, nil, "status", "--repo", corruptRepo, "--json")
	if broken.Code != 4 || broken.Stdout != "" {
		t.Fatalf("broken status = %#v", broken)
	}
	assertBrokenStatusJSON(t, broken.Stderr, corruptRepo)

	heads := runOperatorProcess(t, binary, nested, nil, "heads", "--json")
	if heads.Code != 0 || heads.Stderr != "" {
		t.Fatalf("discovered heads = %#v", heads)
	}
	var headsJSON map[string]any
	if err := json.Unmarshal([]byte(heads.Stdout), &headsJSON); err != nil {
		t.Fatal(err)
	}
	if headsJSON["operation"] != "heads" || headsJSON["repo"] != resolvedPath(t, repo) || headsJSON["note"] == nil {
		t.Fatalf("heads JSON compatibility = %#v", headsJSON)
	}
}
```

In the same file define `operatorProcessResult`, `runOperatorProcess`,
`assertCleanHelp`, `assertHealthyStatusJSON`, `assertPlainHealthyStatus`, and
`assertAttentionStatusJSON` with this exact behavior:

```go
type operatorProcessResult struct {
	Code           int
	Stdout, Stderr string
}

func runOperatorProcess(t *testing.T, binary, directory string, overrides []string, args ...string) operatorProcessResult {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = directory
	command.Env = mergedOperatorEnvironment(overrides)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	code := 0
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			t.Fatalf("run pact %q: %v", args, err)
		}
		code = exitError.ExitCode()
	}
	return operatorProcessResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
}

func mergedOperatorEnvironment(overrides []string) []string {
	replaced := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		key, _, _ := strings.Cut(entry, "=")
		replaced[key] = struct{}{}
	}
	result := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, found := replaced[key]; !found {
			result = append(result, entry)
		}
	}
	return append(result, overrides...)
}

func assertCleanHelp(t *testing.T, result operatorProcessResult) {
	t.Helper()
	if result.Code != 0 || result.Stderr != "" {
		t.Fatalf("help result = %#v", result)
	}
	for _, text := range []string{"Usage:", "Commands:", "status"} {
		if !strings.Contains(result.Stdout, text) {
			t.Fatalf("help lacks %q: %q", text, result.Stdout)
		}
	}
	if strings.Contains(result.Stdout, "Usage of") {
		t.Fatalf("help leaked Go flag usage: %q", result.Stdout)
	}
}

func decodeSingleOperatorJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode JSON %q: %v", raw, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("JSON has trailing value: %q", raw)
	}
	return result
}

func assertHealthyStatusJSON(t *testing.T, raw, repo string) {
	t.Helper()
	result := decodeSingleOperatorJSON(t, raw)
	verification, ok := result["verification"].(map[string]any)
	if !ok {
		t.Fatalf("verification = %#v", result["verification"])
	}
	indexValue, ok := result["index"].(map[string]any)
	if !ok {
		t.Fatalf("index = %#v", result["index"])
	}
	if result["operation"] != "status" || result["ok"] != true || result["health"] != "healthy" ||
		result["repo"] != resolvedPath(t, repo) || result["next_action"] != nil ||
		verification["strict"] != true || verification["ok"] != true || indexValue["state"] != "current" {
		t.Fatalf("healthy status JSON = %#v", result)
	}
}

func assertPlainHealthyStatus(t *testing.T, result operatorProcessResult, namespace string) {
	t.Helper()
	if result.Code != 0 || result.Stderr != "" || strings.Contains(result.Stdout, "\x1b[") {
		t.Fatalf("plain status = %#v", result)
	}
	for _, text := range []string{"Healthy", "Strict verification", "locally closed", "Global completeness", "current", namespace} {
		if !strings.Contains(result.Stdout, text) {
			t.Fatalf("plain status lacks %q: %q", text, result.Stdout)
		}
	}
}

func assertAttentionStatusJSON(t *testing.T, raw, repo, indexState, command string) {
	t.Helper()
	envelope := decodeSingleOperatorJSON(t, raw)
	details, ok := envelope["details"].(map[string]any)
	if !ok {
		t.Fatalf("attention details = %#v", envelope)
	}
	indexValue, ok := details["index"].(map[string]any)
	if !ok {
		t.Fatalf("attention index = %#v", details["index"])
	}
	action, ok := details["next_action"].(map[string]any)
	if !ok {
		t.Fatalf("attention action = %#v", details["next_action"])
	}
	if envelope["ok"] != false || envelope["exit_code"] != float64(9) ||
		details["health"] != "attention" || details["repo"] != resolvedPath(t, repo) ||
		indexValue["state"] != indexState || action["command"] != command {
		t.Fatalf("attention status JSON = %#v", envelope)
	}
}

func assertBrokenStatusJSON(t *testing.T, raw, repo string) {
	t.Helper()
	envelope := decodeSingleOperatorJSON(t, raw)
	details, ok := envelope["details"].(map[string]any)
	if !ok {
		t.Fatalf("broken details = %#v", envelope)
	}
	if envelope["ok"] != false || envelope["exit_code"] != float64(4) ||
		details["health"] != "broken" || details["repo"] != resolvedPath(t, repo) ||
		details["index"] != nil || details["next_action"] != nil {
		t.Fatalf("broken status JSON = %#v", envelope)
	}
}
```

- [ ] **Step 2: Compile the dormant contract helper**

Run:

```bash
env -u GOROOT mise exec -- go test ./tests/e2e -run '^$' -count=1
```

Expected: PASS. No top-level test calls the helper on the base branch, so the
repository stays green without a skip.

- [ ] **Step 3: Write the human-readable contract**

Create `docs/plans/operator-cli/omakase/contract.md` with these fixed sections:

````markdown
<!-- ABOUTME: Freezes the external scenario shared by all operator CLI foundation variants. -->
<!-- ABOUTME: Prevents parser-specific shortcuts from changing PACT's machine or terminal contracts. -->

# Operator CLI Omakase Contract

## Candidate Inputs

- Design: `docs/superpowers/specs/2026-08-25-operator-cli-design.md`.
- Plan: `docs/superpowers/plans/2026-08-25-operator-cli-foundation.md`.
- Shared test helper: `tests/e2e/operator_contract_test.go`.

## Required Wrapper

Each candidate adds `tests/e2e/operator_variant_test.go` with one test:

```go
func TestOperatorCLIContract(t *testing.T) {
	runOperatorCLIContract(t)
}
```

## Pass Gate

Run `go test ./tests/e2e -run TestOperatorCLIContract -count=1`,
`go test -race ./cmd/pact ./internal/status ./tests/e2e`, and
`./scripts/check`. Every command must pass.

## Candidate Evidence

Record dependency changes, production lines changed, full gate output, and
plain plus forced-color samples for healthy and stale status.

## Forbidden Shortcuts

- no changed JSON fields or exit meanings for existing explicit commands;
- no hidden index rebuild or canonical write;
- no mock mode, skipped contract, parser-error leak, or ignored writer error;
- no setup implementation in this slice;
- no edits to another candidate worktree.
````

The first contract commit omits a commit-ID bullet. Step 5 adds the immutable
scenario-contract commit ID in a follow-up documentation commit before
creating worktrees.

- [ ] **Step 4: Run documentation and canonical checks**

Run:

```bash
git diff --check
env -u GOROOT mise exec -- go test ./tests/e2e -run '^$' -count=1
env -u GOROOT mise exec -- ./scripts/check
```

Expected: PASS.

- [ ] **Step 5: Commit the shared contract and pin its base ID**

```bash
git status --short
git add tests/e2e/operator_contract_test.go docs/plans/operator-cli/omakase/contract.md
env -u GOROOT mise exec -- git commit -m "test: define operator CLI contract"
git rev-parse HEAD
```

Use `apply_patch` to add a Scenario contract commit bullet under Candidate
Inputs with the exact hexadecimal ID printed by `git rev-parse HEAD`, then
commit only the contract document:

```bash
git add docs/plans/operator-cli/omakase/contract.md
env -u GOROOT mise exec -- git commit -m "docs: pin operator CLI trial contract"
```

The second commit is the exact base for all three variants. Task 4 captures
that base with `git rev-parse HEAD`, and the final result records it.

### Task 4: Run the Three Command-Architecture Variants in Parallel

**Files:**

- Create in every variant: `tests/e2e/operator_variant_test.go`
- Create/modify in every variant: the winning-file candidates listed in the
  file map above
- Modify in Cobra only: `go.mod`, `go.sum` for Cobra `v1.10.2`
- Modify in Kong only: `go.mod`, `go.sum` for Kong `v1.16.1`
- Modify in every variant: `go.mod`, `go.sum` for `x/term v0.45.0`

**Interfaces:**

- Consumes: the Task 3 base commit, contract helper, command handlers, and
  `internal/status.Inspect`.
- Produces: three independently green branches implementing the same public
  contract, each with one final candidate commit ID and recorded evidence.

- [ ] **Step 1: Enter the required worktree workflow**

Before creating worktrees, read and follow
`superpowers:using-git-worktrees`. Confirm `git status --short` is empty and
that `/.worktrees/` remains ignored.

- [ ] **Step 2: Create exact candidate branches and worktrees**

Run from the repository root:

```bash
operator_cli_base_commit="$(git rev-parse HEAD)"
git worktree add .worktrees/operator-cli-stdlib -b operator-cli/omakase/stdlib "$operator_cli_base_commit"
git worktree add .worktrees/operator-cli-cobra -b operator-cli/omakase/cobra "$operator_cli_base_commit"
git worktree add .worktrees/operator-cli-kong -b operator-cli/omakase/kong "$operator_cli_base_commit"
```

Do not touch the pre-existing setup worktrees.

- [ ] **Step 3: Dispatch all three variant agents in one message**

Use the Omakase skill's single parallel dispatch. Each agent receives the
design, this plan, the contract, its exact worktree, and these candidate rules:

**Standard-library candidate**

```text
Use one commandSpec catalog and a longest-path dispatcher. Keep flag parsing in
the existing handlers. Intercept no-argument help, help paths, --help, --json,
--color, and repository injection before handler execution. Add only
golang.org/x/term v0.45.0.
```

**Cobra candidate**

```text
Pin github.com/spf13/cobra v1.10.2. Build the Cobra tree from the same
commandSpec catalog. Use DisableFlagParsing for leaf commands so existing flag
semantics and JSON remain authoritative. Disable Cobra's automatic error and
usage printing; PACT owns both renderers. Add golang.org/x/term v0.45.0.
```

**Kong candidate**

```text
Pin github.com/alecthomas/kong v1.16.1. Build command and nested-command nodes
from the same commandSpec catalog and use command passthrough to preserve the
existing leaf flag handlers. Disable default help/error output where it would
bypass PACT rendering. Add golang.org/x/term v0.45.0.
```

Every agent must first add the exact enabling wrapper:

```go
// ABOUTME: Enables the shared compiled-binary operator CLI contract for this candidate.
// ABOUTME: Keeps parser variants accountable to identical external behavior.
package e2e

import "testing"

func TestOperatorCLIContract(t *testing.T) {
	runOperatorCLIContract(t)
}
```

They run it and capture the expected initial failure before production edits.

- [ ] **Step 4: Require the same command catalog content from every candidate**

Each catalog contains exactly these paths and groups in this slice:

```text
Get started: init, keygen, trust-add
Inspect:     status, heads, show, verify, log, query
Write:       commit, checkpoint
Maintain:    index status, index rebuild, hash
```

Use these exact purposes and usage lines:

| Path | Purpose | Usage |
|---|---|---|
| `init` | Initialize a PACT store. | `pact init --namespace NAMESPACE [--repo PATH]` |
| `keygen` | Create an external signing key. | `pact keygen --actor ACTOR --out PATH` |
| `trust-add` | Trust a public signing identity. | `pact trust-add --key-file PATH [--repo PATH]` |
| `status` | Check ledger and indexed-read health. | `pact status [--repo PATH] [--json]` |
| `heads` | Show local namespace heads. | `pact heads [--repo PATH] [--namespace PREFIX] [--json]` |
| `show` | Inspect one object or event. | `pact show [--repo PATH] IDENTIFIER [--json]` |
| `verify` | Verify canonical ledger state. | `pact verify [--repo PATH] [--strict] [--json]` |
| `log` | Read compact causal history. | `pact log [--repo PATH] [--namespace PREFIX]... [--actor KEY_ID]... [--limit N] [--cursor TOKEN] [--json]` |
| `query` | Filter causal event history. | `pact query [--repo PATH] FILTER... [--limit N] [--cursor TOKEN] [--json]` |
| `commit` | Sign and append an event batch. | `pact commit --key-file PATH --events PATH [--repo PATH] [OPTIONS] [--json]` |
| `checkpoint` | Sign an authorized checkpoint. | `pact checkpoint --key-file PATH --scope PREFIX --policy-ref ID --authority-epoch EPOCH [OPTIONS] [--json]` |
| `index status` | Inspect the disposable index. | `pact index status [--repo PATH] [--json]` |
| `index rebuild` | Rebuild the disposable index. | `pact index rebuild [--repo PATH] [--json]` |
| `hash` | Hash one evidence file. | `pact hash FILE [--json]` |

Each entry carries path, one-line purpose, exact usage, at most two examples,
repository mode (`none`, `create`, or `open`), and its existing handler. The
catalog is the only dispatch and help inventory. `status` calls
`status.Inspect`; all other entries call existing handlers.

Each adapter must preserve the existing `run` function and add
`runWithConfig`. Actual process mode uses `x/term.IsTerminal` and
`x/term.GetSize`; a failed size lookup uses width 80. JSON forces plain mode.
For human output, explicit `--color` wins, then `NO_COLOR`, `TERM=dumb`, and
non-terminal streams disable color, then `auto` enables color.

- [ ] **Step 5: Require exact status and error mapping**

Each candidate's `status` JSON renderer emits:

```go
map[string]any{
	"operation":         "status",
	"ok":                result.Health == status.HealthHealthy,
	"health":            string(result.Health),
	"repo":              result.Repo,
	"store":             result.Store,
	"default_namespace": result.DefaultNamespace,
	"verification":      compactVerificationMap(result.Verification),
	"index":             compactIndexValue(result.Index),
	"replica":           compactReplicaValue(result.Index, result.Verification),
	"counts":            verifyCountsMap(result.Verification.Counts),
	"heads":             result.Verification.Heads,
	"next_action":       result.NextAction,
}
```

Healthy status writes this object to stdout and exits 0. Attention status
writes the existing error envelope to stderr with `exit_code:9` and this object
as `details`. Broken status uses `verificationFailureExitCode`. Missing store
uses exit 3 and the action `pact setup`. A broken result with no safe index
inspection encodes `index:null`.

Human status uses section headings `Ledger`, `Replica`, and `Index`, includes
the default namespace, labels global completeness `unknown`, and ends with the
one next command only in attention state. It must not draw boxes or truncate a
copyable value.

- [ ] **Step 6: Require writer-failure and compatibility tests from every candidate**

Each candidate adds in-process tests with a writer that returns
`errors.New("closed output")` from every `Write`. The tests require exit 10 for
a successful status whose stdout fails, an unknown command whose stderr fails,
JSON encoding failure, human `index status` output failure, and human `log`
output failure. Add deterministic suggestion tests that accept `statsu` →
`status` and refuse a suggestion for `wat`. Add repository tests proving that
an unsafe `.pact` entry stops discovery and reaches store validation instead of
being skipped in favor of a parent store. Add presentation tests for
`NO_COLOR`, `TERM=dumb`, explicit `always`, and JSON-always-plain precedence.
They also retain all existing `cmd/pact` and E2E tests without edits that
weaken assertions.

Each candidate must run:

```bash
env -u GOROOT mise exec -- go test ./tests/e2e -run TestOperatorCLIContract -count=1
env -u GOROOT mise exec -- go test -race ./cmd/pact ./internal/status ./tests/e2e
env -u GOROOT mise exec -- ./scripts/check
```

Expected: PASS. Each agent uses its exact commit message:

```text
feat: add standard library operator CLI foundation
feat: add Cobra operator CLI foundation
feat: add Kong operator CLI foundation
```

Each agent reports the commit ID, dependency diff, production line count,
commands, and healthy/stale samples.

### Task 5: Review, Judge, and Adopt One Winner

**Files:**

- Create on the winning history:
  `docs/plans/operator-cli/omakase/result.md`
- Remove through git dependency cleanup if the winner does not use them:
  candidate-only modules from `go.mod` and `go.sum`

**Interfaces:**

- Consumes: three clean candidate commits and their evidence.
- Produces: one fast-forwarded `wip/operator-cli` containing exactly one
  adapter, a decision record, and no losing dependency.

- [ ] **Step 1: Verify every candidate from the root session**

Run the three gates from Task 4 inside each worktree. Save concise pass/fail,
test count, dependency diff, and line-count evidence. A candidate with a
contract, race, or canonical-gate failure is ineligible; do not soften the
contract to keep it in competition.

- [ ] **Step 2: Run fresh-eyes review before judging**

Use `fresh-eyes-review` on each exact candidate diff from the pinned base.
Review for JSON drift, hidden writes, writer-error loss, parser output leakage,
unsafe discovery, color-only meaning, and framework code leaking into domain
packages. Record findings. Do not spend time polishing an ineligible loser.

- [ ] **Step 3: Run the Omakase judge**

Use the `judge` skill with its five criteria. The evidence must include real
plain and colored terminal samples, not only test results. Favor the smallest
candidate that is pleasant, preserves contracts, and makes later setup easy.
Record the exact scores and the judge's decisive evidence.

- [ ] **Step 4: Write and commit the result on the winning branch**

Create `docs/plans/operator-cli/omakase/result.md` with:

```markdown
<!-- ABOUTME: Records the tested operator CLI adapter decision and cleanup evidence. -->
<!-- ABOUTME: Makes the winning architecture and rejected tradeoffs durable for later CLI work. -->

# Operator CLI Omakase Result

## Contract

State the pinned base and exact scenario command.

## Candidates

For each candidate, record commit, dependencies, changed production lines,
gate result, review findings, and one output sample.

## Scores

Copy the judge's five-criterion scores and totals without rewriting them.

## Decision

Name the winner and cite the code and output evidence that decided it.

## Known Weaknesses

List real remaining weaknesses with severity and the next plan that owns them.

## Cleanup

List every generated worktree and candidate branch with state `pending`.
```

Create the file inside the selected winner worktree. Run exactly one matching
pair:

```bash
git -C .worktrees/operator-cli-stdlib add docs/plans/operator-cli/omakase/result.md
env -u GOROOT mise exec -- git -C .worktrees/operator-cli-stdlib commit -m "docs: select operator CLI foundation"

git -C .worktrees/operator-cli-cobra add docs/plans/operator-cli/omakase/result.md
env -u GOROOT mise exec -- git -C .worktrees/operator-cli-cobra commit -m "docs: select operator CLI foundation"

git -C .worktrees/operator-cli-kong add docs/plans/operator-cli/omakase/result.md
env -u GOROOT mise exec -- git -C .worktrees/operator-cli-kong commit -m "docs: select operator CLI foundation"
```

Run one pair only. The result commit becomes the winning branch tip.

- [ ] **Step 5: Fast-forward the winner**

From the main worktree on `wip/operator-cli`:

Run exactly one of these commands according to the recorded judge decision:

```bash
git merge --ff-only operator-cli/omakase/stdlib
git merge --ff-only operator-cli/omakase/cobra
git merge --ff-only operator-cli/omakase/kong
```

Do not run either losing merge command and do not cherry-pick a losing branch.
The fast-forward includes the result commit from Step 4.

- [ ] **Step 6: Remove generated candidate worktrees and branches safely**

First require `git status --short` to be empty in all three generated
worktrees. Then remove only these explicit worktrees and branches:

```bash
git worktree remove .worktrees/operator-cli-stdlib
git worktree remove .worktrees/operator-cli-cobra
git worktree remove .worktrees/operator-cli-kong
git branch -D operator-cli/omakase/stdlib
git branch -D operator-cli/omakase/cobra
git branch -D operator-cli/omakase/kong
```

The winning branch deletion is safe only after its commit is reachable from
`wip/operator-cli`. Losing commit IDs are already recorded in the result, so
their generated branches can be discarded deliberately. Leave every setup
worktree and branch alone.

- [ ] **Step 7: Record completed cleanup**

Use `apply_patch` on `docs/plans/operator-cli/omakase/result.md` to change each
generated operator CLI worktree and candidate branch from `pending` to
`removed`. Confirm `git worktree list` still contains all three setup
worktrees, then commit:

```bash
git add docs/plans/operator-cli/omakase/result.md
env -u GOROOT mise exec -- git commit -m "docs: record operator CLI trial cleanup"
```

### Task 6: Harden the Winning Terminal Contract

**Files:**

- Create: `cmd/pact/testdata/status/healthy-60.golden`
- Create: `cmd/pact/testdata/status/healthy-80.golden`
- Create: `cmd/pact/testdata/status/healthy-120.golden`
- Create: `cmd/pact/testdata/status/attention-80.golden`
- Create: `cmd/pact/testdata/status/broken-80.golden`
- Modify: winner's `cmd/pact/status_render.go`
- Modify: winner's `cmd/pact/status_command.go`
- Modify: `cmd/pact/app_test.go`
- Modify: `tests/e2e/operator_variant_test.go`

**Interfaces:**

- Consumes: the selected application adapter and status result.
- Produces: pinned plain output at required widths, stable failed-status JSON,
  and complete writer-failure propagation.

- [ ] **Step 1: Add failing golden tests around the winner's actual renderer**

Add a table test in `cmd/pact/app_test.go` that passes deterministic healthy,
attention, and broken `status.Result` fixtures directly to the selected human
renderer. Test widths 60, 80, and 120. Compare bytes to the five exact golden
paths above. The fixture uses repo `/work/pact`, store `/work/pact/.pact`,
namespace `org/example/widget`, zero counts, locally closed completeness, and a
current or stale index.

Run:

```bash
env -u GOROOT mise exec -- go test ./cmd/pact -run TestStatusGoldenOutput -count=1
```

Expected: FAIL because the goldens do not exist.

- [ ] **Step 2: Add the exact plain goldens and make the renderer match**

Use terminal-native sections without border glyphs. The 80-column healthy
golden must have this information and order; spacing may differ only where the
60- and 120-column files reflow it:

```text
PACT  Healthy
/work/pact
Namespace  org/example/widget

Ledger
  Strict verification  passed
  Objects 0  Commits 0  Checkpoints 0  Events 0
  Heads 0

Replica
  Local completeness   locally closed
  Global completeness  unknown

Index
  State     current
  Coverage  complete
```

The attention golden changes the title and index state, then ends with
`Run  pact index rebuild`. The broken golden says `Index  not inspected` and
does not recommend rebuild. All files end with one newline.

- [ ] **Step 3: Pass golden, color, and JSON tests**

Run:

```bash
env -u GOROOT mise exec -- go test ./cmd/pact -run 'TestStatusGoldenOutput|TestColor|TestStatusJSON|TestWriter' -count=1
```

Expected: PASS. Forced-color output contains ANSI, and stripping ANSI yields
the same words and order as the plain fixture.

- [ ] **Step 4: Run the complete winner contract and race gate**

Run:

```bash
env -u GOROOT mise exec -- go test ./tests/e2e -run TestOperatorCLIContract -count=1
env -u GOROOT mise exec -- go test -race ./cmd/pact ./internal/status ./tests/e2e
env -u GOROOT mise exec -- ./scripts/check
```

Expected: PASS with no warnings or skipped tests.

- [ ] **Step 5: Commit hardening**

```bash
git status --short
git add cmd/pact/app_test.go cmd/pact/status_command.go cmd/pact/status_render.go cmd/pact/testdata/status tests/e2e/operator_variant_test.go
env -u GOROOT mise exec -- git commit -m "test: harden operator CLI output"
```

### Task 7: Document and Close the Foundation Slice

**Files:**

- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-08-25-operator-cli-design.md`
- Modify: `docs/superpowers/plans/2026-08-25-operator-cli-foundation.md`
- Modify: `docs/plans/operator-cli/omakase/result.md`
- Modify: `gotchas.md` only for a new durable architectural fact discovered
  during implementation

**Interfaces:**

- Consumes: the reviewed winner and all gate evidence.
- Produces: a clean, documented Phase A checkpoint whose next step is the
  guided setup implementation plan.

- [ ] **Step 1: Write failing documentation assertions**

Extend the existing README contract test in `tests/e2e/cli_test.go` to require
the exact strings:

```go
for _, fragment := range []string{
	"pact status",
	"pact help",
	"--color auto|always|never",
	"walks up through parent directories",
	"pact index rebuild",
} {
	if !bytes.Contains(readme, []byte(fragment)) {
		t.Fatalf("README lacks %q", fragment)
	}
}
```

Run the focused test and expect it to fail on the first missing fragment.

- [ ] **Step 2: Document actual, shipped behavior**

Update `README.md` with at most two examples per command level. Explain no-arg
help, nested discovery, authoritative `--repo`, status exit behavior, color
control, and explicit rebuild. Do not document setup as shipped yet.

Update this plan's progress block with completed task commits and the exact
next plan: `2026-08-25-operator-cli-setup.md`. Update the design status to
“Foundation implemented; setup and renderer plans pending.” Add only actual
judge evidence to the Omakase result.

- [ ] **Step 3: Run the final review and all gates**

Use `requesting-code-review`, resolve each valid finding with a failing test,
and then use `verification-before-completion`. Run:

```bash
env -u GOROOT mise exec -- go test ./...
env -u GOROOT mise exec -- go test -race ./cmd/pact ./internal/status ./tests/e2e
env -u GOROOT mise exec -- go vet ./...
env -u GOROOT mise exec -- ./scripts/check
```

Build the real binary and run, from a nested directory in this repository:

```bash
env -u GOROOT mise exec -- go build -o /tmp/pact-operator-cli ./cmd/pact
/tmp/pact-operator-cli --help
/tmp/pact-operator-cli status --color never
```

Expected: every automated gate passes; help exits 0; status discovers this
repository and truthfully reports its current ledger and index state. Remove
only the explicit `/tmp/pact-operator-cli` scratch binary after inspection.

- [ ] **Step 4: Commit the foundation closeout**

```bash
git status --short
git add README.md tests/e2e/cli_test.go docs/superpowers/specs/2026-08-25-operator-cli-design.md docs/superpowers/plans/2026-08-25-operator-cli-foundation.md docs/plans/operator-cli/omakase/result.md
env -u GOROOT mise exec -- git commit -m "docs: record operator CLI foundation"
git status --short --branch
```

Expected: the commit passes every hook and the worktree is clean.

## Author Self-Review

- [x] Spec coverage: Tasks 1–7 cover the foundation slice's help, discovery,
  status, visual, JSON, exit, writer, Omakase, test, and documentation rules.
  The phase boundary assigns setup and the complete log/query/show rendering
  pass to plans written against the selected adapter.
- [x] Unresolved-marker scan: the plan contains no deferred code or unnamed
  error-handling step. Values that only exist during execution come from exact
  git commands or the recorded judge decision.
- [x] Type consistency: `DefaultNamespace`, `VerifyContext`, `status.Inspect`,
  `run`, and `runWithConfig` use the same signatures in the file map,
  interface section, tests, and tasks.
- [x] TDD and commits: every shared or winner production task begins with an
  observed failing test and ends with a focused commit.
- [x] Variant parity: every candidate enables the same compiled-binary helper
  and runs the same focused, race, and canonical gates.
- [x] Cleanup: Task 5 removes only the generated operator CLI worktrees and
  branches after the winner is reachable; setup worktrees remain untouched.
