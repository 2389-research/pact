<!-- ABOUTME: Plans the two small changes that make PACT's CI independent of runner defaults. -->
<!-- ABOUTME: Preserves the install and terminal contracts while restoring a green main branch. -->

# CI Portability Repair Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make all four GitHub Actions CI jobs pass without changing production behavior or weakening coverage.

**Architecture:** Declare `mise` in the one job that executes the documented `mise` command. Make the PTY text-contract test choose plain output explicitly while leaving ANSI policy to its existing focused tests.

**Tech Stack:** GitHub Actions, Go 1.26, `creack/pty`, `jdx/mise-action@v4`.

## Global Constraints

- Do not change application behavior.
- Do not rewrite the README install command.
- Do not set a workflow-wide color override.
- Do not create a release tag until the post-merge `main` workflow is green.

---

### Task 1: Deterministic PTY text contract

**Files:**
- Modify: `tests/e2e/setup_test.go:434`
- Test: `tests/e2e/setup_test.go:171`

**Interfaces:**
- Consumes: the existing command-local `--color never` presentation flag.
- Produces: a real-PTY transcript whose status rows contain stable plain text under any ambient `NO_COLOR` or `TERM` value.

- [ ] **Step 1: Verify RED under the CI-like color environment**

Run:

```sh
env -u GOROOT mise exec -- env -u NO_COLOR TERM=xterm-256color \
  go test ./tests/e2e -run 'TestSetupRealPTYPromptContract/accept$' -count=1 -v
```

Expected: FAIL because ANSI bytes split the expected `store   created` text.

- [ ] **Step 2: Select plain output in the PTY contract test**

Change the command construction to:

```go
command := exec.CommandContext(ctx, binary, "setup", "--repo", repo, "--color", "never")
```

- [ ] **Step 3: Verify GREEN under the same environment**

Run the command from Step 1.

Expected: PASS.

- [ ] **Step 4: Commit the focused test repair**

```sh
git add tests/e2e/setup_test.go
git commit -m "test: make PTY setup assertions color-independent"
```

### Task 2: Declare the CI test job's mise dependency

**Files:**
- Modify: `.github/workflows/ci.yml:20`

**Interfaces:**
- Consumes: the documented `mise exec` source-install command exercised by `TestREADMEInstallCommandPlacesPactAtDocumentedDestination`.
- Produces: a `mise` executable on the Test job's `PATH`; Go remains supplied by `actions/setup-go`.

- [ ] **Step 1: Preserve the failing integration evidence**

Use GitHub Actions run `33036004424` as RED evidence. Its Test job exits 127 with:

```text
env: ‘mise’: No such file or directory
```

- [ ] **Step 2: Install only the mise command wrapper**

After `Set up Go`, add:

```yaml
      - name: Set up mise
        uses: jdx/mise-action@v4
        with:
          install: false
```

- [ ] **Step 3: Validate workflow syntax and repository hooks**

Run:

```sh
git diff --check
git add .github/workflows/ci.yml
pre-commit run --files .github/workflows/ci.yml
```

Expected: all applicable hooks pass with no YAML error.

- [ ] **Step 4: Commit the dependency repair**

```sh
git commit -m "ci: install declared mise dependency"
```

### Task 3: Prove the branch and main workflows

**Files:**
- Modify: `gotchas.md`
- Verify: all files changed since `main`

**Interfaces:**
- Consumes: Tasks 1 and 2.
- Produces: a clean, fully verified branch ready for broad review and integration.

- [ ] **Step 1: Record the environment trap**

Append this project memory:

```markdown
- Real-PTY text assertions must select `--color never`; local `NO_COLOR`
  can hide ANSI-dependent failures that appear on GitHub runners.
```

- [ ] **Step 2: Run the canonical gate without the local color override**

```sh
env -u GOROOT -u NO_COLOR mise exec -- ./scripts/check
```

Expected: Go unit and E2E suites plus all 18 Python oracle tests pass.

- [ ] **Step 3: Review and commit the implementation**

Run a fresh-eyes review over the branch diff, then:

```sh
git add gotchas.md docs/superpowers/plans/2026-08-26-ci-portability.md
git commit -m "docs: record CI portability repair"
```

## Completion after broad review

After Task 3 passes its task review, run the required whole-branch review and
branch-finishing workflow. Push `wip/fix-ci-portability`, open a draft pull
request against `main`, and wait for Test, Reference conformance, Lint, and
Build.

Expected: all four jobs pass.

After the pull-request workflow passes, mark the pull request ready, merge it,
update the local `main`, and wait for the push workflow.

Expected: local `main`, `origin/main`, and GitHub's `refs/heads/main` match; all four post-merge jobs pass; no `v*` tag exists.
