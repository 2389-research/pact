<!-- ABOUTME: Defines the minimal repair for PACT's first failing main-branch CI run. -->
<!-- ABOUTME: Makes declared tooling and PTY output checks independent of runner defaults. -->

# CI Portability Repair Design

**Status:** Approved by Doctor Biz on 2026-08-26.

## Goal

Make the existing GitHub Actions CI workflow pass for the same behavior already
accepted by the canonical local gate. Fix the two proven environment mismatches
without changing PACT's production behavior or weakening test coverage.

## Root Causes

`TestREADMEInstallCommandPlacesPactAtDocumentedDestination` executes the exact
source-install command documented in the root README. That command uses `mise`,
but the Test job installs only Go. The Ubuntu runner therefore exits 127 before
it can test the install.

`TestSetupRealPTYPromptContract` asserts aligned plain-text setup rows while its
child process uses automatic color. GitHub's PTY environment enables ANSI color,
which splits the expected text with escape bytes. Local runs inherit
`NO_COLOR=1`, which hid the dependency on ambient environment. Running the same
focused test locally without `NO_COLOR` reproduces the CI failure; restoring
`NO_COLOR` makes it pass.

## Repair

Add the official `jdx/mise-action@v4` setup step to the Test job before the
documented install command can run. Keep `actions/setup-go` as the Go toolchain
owner; `mise` supplies the declared command wrapper used by the contract test.

Pass `--color never` to the real-PTY setup invocation. That test owns prompt
order, confirmation, action rows, persistence, and secret-safety behavior. ANSI
policy remains covered by the existing focused color tests, so fixing the PTY
test's presentation mode makes its text assertions deterministic without
removing color coverage.

Do not strip ANSI after capture, set a workflow-wide `NO_COLOR`, skip tests, or
change production rendering. Those options either hide malformed terminal
output or couple unrelated tests to a global runner setting.

## Verification

The existing failed checks are the RED evidence. After the repair:

- the PTY accept case passes with `NO_COLOR` absent and `TERM=xterm-256color`;
- the README source-install test runs with `mise` available;
- `env -u GOROOT mise exec -- ./scripts/check` passes locally;
- the branch workflow passes Test, Reference conformance, Lint, and Build;
- after merge, the workflow for `main` passes before any release tag is made.

## Non-Goals

- No application behavior change.
- No README install-command rewrite.
- No workflow-wide color override.
- No release tag until `main` is green.
