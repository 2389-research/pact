<!-- ABOUTME: Records runtime validation implementation, exact-source proof, and review limits. -->
<!-- ABOUTME: Gives downstream owners the public interfaces and retained RED/GREEN evidence. -->

# Runtime pre-sign validation — pact#3ezv

Doctor Biz authorized task 2 of the development workflow. This task adds only
validation and its tests; recording, evidence resolution, projection, and CLI
adapters remain separate. Base: `8cea1243b4eab16a1ec19e62916ee690eb97da8e`.
Worktree: `/Users/harper/.codex/worktrees/3f32/pact`; branch:
`feat/development-workflow`. The completed task-1 contract is unchanged.

## Behavior and interfaces

- `DecodeInput(context.Context, []byte) (RecordInput, error)` reads exactly one
  strict object through `canonical.ParseContext`. It rejects duplicate keys,
  unknown fields, arrays/null, and caller envelope overrides; the array diagnostic
  names the required record fields.
- `ValidateInput(context.Context, RecordInput) (ledger.EventBatch, error)` checks
  shared closed field and enum definitions, strict source identities, outcome/
  observation consistency, coverage bounds, unique finding IDs, and all nested
  evidence-index bounds. It rejects payload operation/source overrides, injects
  those facts and fixed kind/subject/schema identity, and applies the core
  normalizer and immutable-secret scanner. It changes neither caller data nor
  store bytes and returns fresh normalized core data.
- `ValidatePayload(context.Context, string, string, map[string]any, int) error`
  accepts event type, exact schema digest, signed payload, and containing evidence
  count. Only the bundled type/digest pairing is supported. Raw unknown domains
  remain unchecked, reported as `unsupported_type`; unsupported exact schema
  identity returns `unsupported_schema`. The raw ledger API remains generic.
- `ValidationError` exposes `Path` and `Category`, with bounded value-free text
  and `Unwrap` for core error identity. Cancellation preserves context identity.
  Callers must render the wrapper, rather than its input-dependent core cause.

Runtime field checks consume `EventDefinitions()` and its nested definitions.
The generated schema/policy bytes and digest manifest remain unchanged. The
fixed conditional outcome and review rules receive local semantic checks; this
is neither an arbitrary schema interpreter nor a new runtime dependency.
Programmatic values use core JSON types, with integers instead of floats and
valid UTF-8. Conversion explicitly builds the core input object and preserves
strict canonical rules rather than repairing input through `encoding/json`.

The bounded core extension is the five-line exported
`ledger.NormalizeEventBatchContext` wrapper around the existing internal owner.
The original public API and all core wire behavior remain unchanged. Its focused
regression covers success, prior cancellation, and mid-loop cancellation.

## Tests and review

All sixteen public cases run through domain validation and real-store signing/
readback for valid batches. Every invalid public case compares all temporary
`.pact` file digests before/after. Cancellation also preserves those bytes.
Caller input is compared before/after validation. Focused tests cover required
fields, nested object closure, enum/ref/source formats, observation variants,
full SHA-256 source IDs, NFC normalization, secret refusal, and bounded errors.

A compiled Go test subprocess runs the complete decode/validate/load-external-
key/commit/readback pipeline against a real temporary store. For invalid corpus
inputs it receives a nonexistent key path and must reject before opening that
key or store; protected byte snapshots remain equal. The compiled production CLI
sets up and verifies the store and reads admitted results. No E2E mocks or mock
application mode exist. Existing compiled raw transport still admits the invalid
`partial` domain payload unchanged, proving that raw strict ledger verification
does not claim domain validity.

The self-review covered all nine changed source/test/contract files and their
actual owners, with special attention to conversion, caller mutation, bounded
errors, source/ref semantics, and normalization. During implementation review,
ordinary JSON encoding was found to repair invalid UTF-8 and accept whole-valued
floats. RED regressions caught both; explicit core-value conversion corrected
them. The final fresh-eyes pass found no remaining defect in this bounded slice.
No independent-review claim is made here.

M1 from the independent khqq review is closed: the inconclusive review now asks
for another review with sufficient evidence, retains its blocking review-event
reference, and carries no reference to an absent finding. The focused
`TestInconclusiveFixtureHasNoDanglingFinding` failed before this correction and
passes after it, alongside the schema oracle and runtime corpus tests.

## Retained development commands and outcomes

All commands ran from the worktree above. Logs are under `evidence/` and carry
unique `3ezv-*` names. A filename containing `green` labels an attempted green
run; the exit column reports the actual outcome, including failed attempts.

| Evidence log | Actual command after `env -u GOROOT mise exec --` | Exit / result |
| --- | --- | --- |
| `3ezv-red-01.log` | `go test ./internal/development` | 1; initial compile gap, including corrected test method call; not substantive RED proof |
| `3ezv-red-02.log` | `go test ./internal/development -run 'TestValidate\|TestDecodeInput\|TestValidationCancellation\|TestInconclusive'` | 1; substantive failures against no-validation stubs and dangling M1 fixture |
| `3ezv-green-01.log` | same as RED-02 | 1; explicit struct-to-core conversion still needed |
| `3ezv-green-02.log` | same as RED-02 | 0; first corpus/local checks green |
| `3ezv-green-03.log` | `go test ./internal/development ./internal/ledger ./tests/e2e -run 'TestValidate\|TestDecodeInput\|TestValidationCancellation\|TestInconclusive\|TestDevelopmentValidation'` | 0; compiled pipeline green |
| `3ezv-red-03.log` | `go test ./internal/development -run TestValidateRejectsDomainContradictions` | 1; invalid UTF-8 and whole-valued float accepted |
| `3ezv-green-04.log` | `go test ./internal/development ./tests/e2e -run 'TestValidate\|TestDecodeInput\|TestValidationCancellation\|TestInconclusive\|TestDevelopmentValidation'` | 0; strict core conversion restored |
| `3ezv-green-05.log` | `go test ./internal/development ./internal/ledger ./tests/e2e -run 'TestValidate\|TestDecodeInput\|TestValidationCancellation\|TestInconclusive\|TestDevelopmentValidation\|TestNormalizeEventBatchContextPublic'` | 0; broader shapes and core context wrapper |
| `3ezv-red-04.log` | `go test ./internal/development -run TestDecodeArrayExplainsRecordObject` | 1; missing required object-field guidance |
| `3ezv-green-06.log` | `go test ./internal/development ./internal/ledger` | 0; full task and ledger packages |
| `3ezv-green-07.log`, `3ezv-green-08.log` | `go test ./internal/development ./internal/ledger ./tests/e2e -run 'TestValidate\|TestDecode\|TestValidationCancellation\|TestInconclusive\|TestDevelopmentValidation\|TestNormalizeEventBatchContextPublic'` | 0; final focused suite, including canceled store-byte check |
| `3ezv-check-preliminary-01.log` | `scripts/check` | 0; production and reference gate |
| `3ezv-lint-preliminary-01.log` | `golangci-lint run` | 1; seven context/style findings, all fixed without suppressions |
| `3ezv-lint-preliminary-02.log` | `golangci-lint run` | 0; fresh cache `.scratch/3ezv-lint-preliminary`, zero issues |
| `3ezv-fix-preliminary-01.log` | `go fix -diff ./...` | 1; two test loops needed canonical `maps.Copy`; `go fix ./internal/development` applied them successfully |

Formatting used `go fmt`'s canonical formatter directly:
`env -u GOROOT mise exec -- gofmt -w` with each changed Go path.
`git diff --check` exited 0. Source commits run all configured hooks with
`PREK_HOME=/Users/harper/.cache/prek-khqq.QItPsm`, used only by this worktree.
Git fixture subprocesses retain the existing inherited-`GIT_*` filter; no
repository selectors or hooks were bypassed. Parent progress, Kata, and `.pact`
files remain untouched.

## Scope and remaining limits

No known release-blocking defect remains in validation itself. This library
alone does not provide a validated recording workflow. Before shipping supported
acceptance, complete evidence/source existence checks, payload-ref reachability,
resolution semantics, policy identity, authorization, and acceptance projection
in their assigned tasks. These are deliberate domain boundaries, not assertions
proved by local shape validation. Command arguments are data and never execute.

Changed implementation/contract paths:
`internal/development/{validate.go,validate_shape.go,validate_test.go,README.md}`;
`internal/ledger/{event.go,event_test.go}`;
`tests/e2e/development_validation_test.go`;
`tests/fixtures/development/inputs.json`;
`docs/status/development-workflow/khqq-review.md`.
This report and `evidence/3ezv-*` are provenance artifacts committed separately.

## Frozen source and final proof

Source commit: `736a1be846e79e689a2ca53845cab01c366849a6`.
Source tree: `19ff12d852b79d9e37b79748fd4800c80da99a8b`.
The source commit contains the nine implementation/contract paths listed above.
Its complete successful hooks are retained in `evidence/3ezv-source-commit.log`.
The hook invocation was:

```sh
env -u GOROOT PREK_HOME=/Users/harper/.cache/prek-khqq.QItPsm GOLANGCI_LINT_CACHE=$PWD/.scratch/3ezv-hook-lint mise exec -- git commit -m 'feat: validate development inputs before signing'
```

Final checks execute the frozen source, with no tracked source diff. Each final
log starts with that source commit/tree and the actual full command, then retains
raw output and explicit exit status. The only untracked files while these checks
run are this report and the unique task evidence logs.

```sh
env -u GOROOT mise exec -- scripts/check
env -u GOROOT mise exec -- go test -race ./...
env -u GOROOT GOLANGCI_LINT_CACHE=$PWD/.scratch/3ezv-final-lint mise exec -- golangci-lint run
```

`3ezv-final-check.log` exits 0: formatting, vet, all Go packages, real production
CLI usage, derived-index recovery, and eighteen reference tests pass.
`3ezv-final-lint.log` exits 0 with zero issues using the fresh isolated cache.
`3ezv-final-race.log` exits 0 for all packages, including the real compiled
validation pipeline. Some unchanged package results use Go's content-addressed
cache; the source-commit hook also ran `go test -race -count=1 ./...` successfully.
No new warning, error, or unresolved gate failure remains.

`git diff --name-only HEAD` returned no tracked changes after final checks.
SHA-256 hashes for every retained raw log are pinned in
`evidence/3ezv-evidence-sha256.txt`; the report/evidence commit follows source
freeze, so it does not alter the tested source. Parent orchestration owns signed
PACT publication and Kata closure after review; this implementer did not mutate
either store.
