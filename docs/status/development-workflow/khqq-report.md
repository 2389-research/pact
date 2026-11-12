<!-- ABOUTME: Records implementation and retained verification evidence for Kata pact#khqq. -->
<!-- ABOUTME: Separates the fixed profile contract from later validation and projection work. -->

# Development profile contract — pact#khqq

Date: 2026-11-11. Scope: task 1 only. Base:
`9317e5ec25bfda0b0f998f73433f70e1d8780807` on `feat/development-workflow` in the
existing isolated worktree `/Users/harper/.codex/worktrees/3f32/pact`.

Frozen implementation source: `89e8321f449ba2cc25b80efc34737760e25678e9`.
Tree: `b88ce672e3100b67176e7b281a274f4c69d19fa9`. No implementation edits
occurred after this commit. Final exact-source gate results are recorded below. PACT recording, Kata closure, independent
review, and integration remain with the parent workflow owner.

## Implementation

- Added `development.Source`, `FindingRef`, `InputEvent`, and `RecordInput`.
- Added fresh shared closed field definitions, enum lists, schema conditions,
  and the four fixed event definitions. No ledger/store/core format changed.
- Added `ProfileArtifacts()` for four exact JSON Schema payload artifacts and
  one named declarative local policy. `go generate ./internal/development`
  writes deterministic bytes and their SHA-256 digest manifest.
- Added 16 public input cases (40 payloads): every check outcome, review verdict,
  severity, explicit acceptance decision, next action, partial coverage, and the
  five required invalid inputs. Expected decisions live separately.
- Added two deterministic synthetic Git commits via a public fast-import fixture.
  Its proof text contains no application output. Signing tests generate fresh
  temporary identities; no consumer identity, transcript, source, or signing
  material was imported.
- Added the separate complete-loop, concurrent-incompatible, unresolved-dependency,
  and unknown-schema history instructions and answer rubrics. These are inputs
  for later work, not implemented projection claims.
- Added exact artifact/drift tests, the existing reference environment's Python
  JSON Schema oracle subprocess, real-store signing/readback, and a compiled
  CLI lifecycle. The CLI test also proves that raw commit still transports an
  invalid domain outcome and that strict verification makes no domain claim.

Caller input holds operation/source once. Later validation injects both into
signed payloads and supplies envelope kind, subject, and exact schema digest.
Request digests belong to recorder-generated batch metadata. Human summary is
optional. The precise field and reference contract is documented in
`internal/development/README.md`; fixture binding rules are in
`tests/fixtures/development/README.md`.

## Final verification against frozen source

All three final commands ran after source commit
`89e8321f449ba2cc25b80efc34737760e25678e9`, with no implementation changes
between or during them. Context is retained in `khqq-frozen-source-01.log`.
Toolchain: Go 1.26.6 darwin/arm64; golangci-lint 2.13.2.

| Exact command | Retained final log | Result |
| --- | --- | --- |
| `env -u GOROOT mise exec -- scripts/check` | `khqq-check-final-01.log` | Exit 0: formatting, vet, full Go suite, build, real compiled CLI checks, 18 Python reference tests. |
| `env -u GOROOT mise exec -- go test -race ./...` | `khqq-race-final-01.log` | Exit 0: all packages passed; unchanged package results may be cached. The preceding successful commit hook ran its configured full race suite with `-count=1`. |
| `env -u GOROOT mise exec -- golangci-lint run` | `khqq-lint-final-01.log` | Exit 0: 0 issues. |

The final logs contain no new warnings or errors. Read-only outer state
snapshots before/after final gates (`khqq-outer-state-before-01.log` and
`khqq-outer-state-after-01.log`) compare equal: main config and index SHA-256,
main root path, and main ref stayed unchanged. These snapshots are after the
parent's config-only repair; they do not claim the failed hook caused no incident.

All 25 retained raw logs have exact SHA-256 hashes in
[evidence/khqq-evidence.sha256](evidence/khqq-evidence.sha256). Verify from the
repository root with `shasum -a 256 -c docs/status/development-workflow/evidence/khqq-evidence.sha256`.

## RED and GREEN evidence

Every evidence path below is under `docs/status/development-workflow/evidence/`.
No log was overwritten. Early logs are development observations, not claims
about a frozen committed source.

| Retained log | Exact command | Result |
| --- | --- | --- |
| `khqq-red-01.log` | `env -u GOROOT mise exec -- go test ./internal/development` | Exit 1: all five supported profile artifacts absent; the initial test ran before implementation. |
| `khqq-green-01.log` | `env -u GOROOT mise exec -- go test ./internal/development` | Exit 0 after minimal definitions/generation. |
| `khqq-red-02.log` | `env -u GOROOT mise exec -- go test ./internal/development` | Exit 1: digest manifest absent; integration adapter also used forbidden decoded floats. |
| `khqq-red-03.log` | `env -u GOROOT mise exec -- go test ./internal/development` | Exit 1: manifest absent; adapter compared equivalent integer representations directly. |
| `khqq-red-04.log` | `env -u GOROOT mise exec -- go test ./internal/development` | Exit 1: manifest absent and proposed acceptance envelope kind invalid under the existing core vocabulary. |
| `khqq-green-02.log` | `env -u GOROOT mise exec -- go test -v ./internal/development` | Exit 0: published manifest, core decision/action kinds, reference oracle, real signing/readback. |
| `khqq-green-03.log` | `env -u GOROOT mise exec -- go test -v ./internal/development ./tests/e2e -run 'Profile\|PositiveCorpus\|CLIDevelopment'` | Exit 0: 16 cases/40 payloads and compiled raw transport. |
| `khqq-check-01.log` | `env -u GOROOT mise exec -- scripts/check` | Exit 0: Go vet/tests/build, compiled CLI lifecycle, 18 reference tests. Preliminary source. |
| `khqq-race-01.log` | `env -u GOROOT mise exec -- go test -race ./...` | Exit 0, but source changed during the run; not exact-source proof. |
| `khqq-lint-01.log` | `env -u GOROOT mise exec -- golangci-lint run` | Exit 1: three generator output permission findings, fixed to 0750/0600. |
| `khqq-red-05.log` | `env -u GOROOT mise exec -- go test ./internal/development -run TestProfileCorpusAgainstReferenceJSONSchema` | Exit 1: JSON Schema regex allowed a trailing newline in a full revision. |
| `khqq-red-06.log` | `env -u GOROOT mise exec -- go test ./internal/development -run TestProfileCorpusAgainstReferenceJSONSchema` | Exit 1 after newline fix: interrupted process's observed negative exit was rejected. |
| `khqq-green-04.log` | `env -u GOROOT mise exec -- go test -v ./internal/development ./tests/e2e -run 'Profile\|PositiveCorpus\|CLIDevelopment'` | Exit 0 after both schema boundary fixes; normal failed exits remain positive. |

Generation commands actually run: `env -u GOROOT mise exec -- go generate
./internal/development` (exit 0 after deliberate schema changes). Formatting:
`env -u GOROOT mise exec -- gofmt -w internal/development tests/e2e/development_test.go`
and later `env -u GOROOT mise exec -- gofmt -w internal/development` (exit 0).

The integration adapter's float/representation failures were test defects and
were fixed using canonical JSON parsing and byte comparison. They are retained
for transparency and do not count as feature RED evidence. The missing-artifact,
missing-manifest, core-kind, and two schema-boundary failures do.

## Self-review

Reviewed all 19 implementation/doc/data files against design sections 3 and 7,
the task acceptance criteria, existing core kinds and normalizer, public-data
boundary, artifact generation, subprocess arguments, shared mutable state, and
schema-versus-domain proof limits. No new shell runner, external interpreter,
trust rule, runtime dependency, persistence path, or CLI command was introduced.

Fixed findings:

1. Generator output permissions failed repository security lint: use 0750
   directories and 0600 files, without suppressions.
2. Python's regex end anchor could match before a final newline: patterned
   identities now explicitly exclude whitespace. RED/GREEN oracle proof retained.
3. Interrupted process APIs can report negative exit values: permit integer
   observations for interruption while keeping normal failures positive.
   RED/GREEN oracle proof retained.
4. The full commit hook exposed inherited Git repository selectors in fixture
   subprocesses. Fixed the process boundary, added real-repository isolation
   regression, and recorded the trap in `gotchas.md` (incident below).
5. The repository's `go fix -diff` hook required `maps.Copy` in both test
   adapters. Applied `env -u GOROOT mise exec -- go fix ./internal/development`
   and `env -u GOROOT mise exec -- go fix ./tests/e2e`; the hook rerun passed.

The initial real-store test also caught the non-core kind `attestation`; acceptance
uses existing `decision` and next action uses existing `action`. Core behavior
was preserved rather than extended.

## Hook execution and Git isolation incident

The first source commit attempt used the shared default prek cache and waited
on another project's active cache lock. Only this task's own waiting prek process
was terminated; no unrelated process or lock file was touched. The full unchanged
hook configuration then used a task-specific cache:
`PREK_HOME=/Users/harper/.cache/prek-khqq.QItPsm`. The cache path is retained in
`khqq-hook-cache-01.log`.

Commands and outcomes:

- `env -u GOROOT mise exec -- git commit -m "feat: define development profile and public contract corpus"`:
  exited 1 after terminating only its waiting hook process; no commit created.
  `khqq-source-commit-01.log`.
- `env -u GOROOT PREK_HOME=/Users/harper/.cache/prek-khqq.QItPsm mise exec -- git commit -m "feat: define development profile and public contract corpus"`:
  exited 1 on the real CLI fixture test and required `go fix` changes.
  `khqq-source-commit-02.log` retains the full hook error output.
- The same full commit command after the root-cause fix:
  exited 0; created the frozen implementation commit above.
  `khqq-source-commit-03.log`. Every applicable hook passed; no hooks were bypassed.

The failed hook exported `GIT_DIR`/`GIT_WORK_TREE` from the enclosing repository.
The new fixture helper set only the subprocess working directory, which does
not override Git's environment selection. Its `git init` therefore wrote
`core.worktree=/Users/harper/.codex/worktrees/3f32/pact` into the common
`/Users/harper/Public/src/2389/pact/.git/config`; subsequent fast-import refused
to replace main because the synthetic tip did not contain its current commit.
This was a high-impact test isolation defect, caught before any source commit.

Execution stopped immediately for read-only reconciliation. Main remained
`fd1f227794d6bf9956bd8eea0798254e0134918f`, and the task branch remained the
original `9317e5ec25bfda0b0f998f73433f70e1d8780807`. The parent saved the incident
common config mode 0600 at
`.scratch/development-workflow/git-config-incident-before-repair`, removed only
the shared `core.worktree` key, and verified main's correct root and original
`.gitignore`/`.kata` changes plus its intentional PACT objects. The parent also
verified PACT remained healthy. Per-worktree configuration and the worktree
extension stayed intact. No reset, restore, force update, or object cleanup ran.
Unreferenced synthetic Git objects may remain; deleting objects was not needed
for the config-only repair and was not attempted.

The fixture helper now strips inherited `GIT_*` variables and explicitly uses
SHA-1 when creating these fixed fixture commits. `TestFixtureGitIgnoresHookEnvironment`
creates two real temporary repositories, injects the outer repository's
`GIT_DIR`, `GIT_WORK_TREE`, `GIT_COMMON_DIR`, and `GIT_INDEX_FILE`, executes
init/import/read-tree against the selected target, and verifies the outer
config, main ref, and index are byte-identical. Its setup clears inherited Git
selectors first so even a regression cannot target the enclosing real project.

`khqq-red-07.log` retains the sandbox reproduction from
`env -u GOROOT mise exec -- go test ./tests/e2e -run TestFixtureGitIgnoresHookEnvironment`
(exit 1). `khqq-green-05.log` retains
`env -u GOROOT mise exec -- go test -v ./internal/development ./tests/e2e -run 'Profile|PositiveCorpus|CLIDevelopment|FixtureGitIgnores'`
(exit 0) after repair, including unchanged outer state assertions. The successful
full hook rerun exercises the fix in the original triggering environment.

## Remaining boundaries and recommendation

No known release-blocking defect in this bounded contract remains after review.
This task alone is not a usable validated development workflow. Do not ship a
positive supported-acceptance command until the later validation, evidence,
recording, and projection tasks pass their own gates.

- JSON Schema checks shape, enums, and expressible conditional fields. Duplicate
  finding IDs by field, evidence-index bounds, coverage arithmetic, actual source
  existence, ref reachability, authorization, and acceptance support belong to
  later runtime tasks. Semantic/projection fixture expectations are declarative.
- The correction fixture has a valid-shaped placeholder external review ref;
  history instructions and real-store/CLI adapters bind it to actual original
  review identity. It supports acceptance only in that complete bound history.
- `local:id` refs inside payloads need profile-level resolution relative to the
  containing commit. Core envelope resolution does not interpret payload data.
- The named policy expresses local root-based checks and explicitly disclaims
  established reviewer independence. It is not registered organizational policy.
- Oracle tests use the existing uv reference test dependency environment, not a
  new Go runtime dependency. These tests need uv, Git, and that environment.

## Source references and changed files

Requirements: `docs/plans/development-workflow/tasks/01-contract.md`;
`docs/plans/development-workflow/design.md` sections 2–9;
`docs/plans/development-workflow/implementation-plan.md` global constraints;
`docs/plans/development-workflow/consumer-audit.md` (measurements remain there).
Task 2 boundary checked against `tasks/02-validation.md`.

Existing owners reused: `internal/ledger/event.go` (`NormalizeEventBatch` and
allowed kinds), `internal/ledger/commit.go` (`Commit`, `EventRef`),
`internal/ledger/verify.go` (`Show`, `Verify`), `internal/store/store.go`
(`Init`), `internal/identity/keyfile.go` (fresh external temporary key),
`tests/e2e/cli_test.go` (compiled binary and CLI helpers), and
`docs/pact-ledger/requirements-dev.txt` (existing JSON Schema test oracle).

Implementation files: `internal/development/{profile.go,schema.go,profile_test.go,README.md}`;
`internal/development/cmd/profilegen/main.go`;
`internal/development/schema/{check.schema.json,review.schema.json,acceptance.schema.json,next-action.schema.json,local-policy.json,digests.json}`;
`tests/e2e/development_test.go`;
`tests/fixtures/development/{README.md,inputs.json,expected.json,histories.json,source.fast-import,oracle.py}`.
`gotchas.md` adds the dated Git-hook isolation trap.
Evidence and this report are separate provenance artifacts. No `.pact` files,
mutable Kata state, parent tracker, go.mod/go.sum, or core source were changed.
