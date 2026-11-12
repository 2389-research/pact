<!-- ABOUTME: Records the reliable recording implementation and retained verification evidence. -->
<!-- ABOUTME: Separates signed admission from reported execution, evidence checks, and index recovery. -->

# pact#x9r1 recording report

Date: 2026-11-11.
Base: `b310c2c6f207806308c36f319658ff104c1c2a7c`.
Branch: `feat/development-workflow`.
Worktree: `/Users/harper/.codex/worktrees/3f32/pact`.
Frozen implementation source and final gate results appear below.

## Scope and contract

Implemented `workflow.Record(context.Context, RecordRequest) (RecordResult, error)`.
`RecordRequest` has `Repo`, `KeyFile`, and `Input development.RecordInput`.
`RecordResult` exposes `Admission`, `ObjectID`, `EventRefs`, `SchemaValidation`,
`Integrity`, `Authenticity`, `Authorization`, `IndexState`, `SourceFingerprint`,
`NextAction`, `Evidence`, `EvidenceState`, and `ConflictingObjectIDs`.
`ErrConflict` and `ErrEvidence` support `errors.Is`.

Added the catalog command:

```text
pact record --repo PATH --profile development/v1 --input FILE --key-file FILE --json
```

The [API and result contract](../../../internal/workflow/README.md) defines every
JSON field, outcome, exit status, and recovery instruction.
The CLI rejects arrays with the expected-object diagnostic.
JSON errors preserve recording results in `details` on stderr.
Human post-admission failures preserve the object ID and recovery instruction.

Used these existing owner APIs:

- `development.DecodeInput` and `development.ValidateInput`.
- `identity.LoadSigningKey` and `store.Open`.
- `evidence.ResolveAll` with its aggregate byte limit.
- `Store.WithReadLock`, `ledger.Scan`, and `Store.GetBoundedContext` for reconciliation.
- `ledger.Commit` for signing, canonical admission, and verification.
- `index.New(st).Rebuild` after releasing the operation lock.

The operation key includes signed namespace, signer key ID, profile, and operation ID.
The request digest covers normalized profile, namespace, and complete event facts.
It excludes time, parents, actor label, and machine paths.
Signed `metadata.workflow` stores profile, operation ID, and request digest.
Reconciliation recomputes the digest from stored event facts.
It does not trust a copied metadata claim.
Multiple matching-key raw claims return every candidate ID as a conflict.

The operation-specific hashed flock lives under checked `.pact/tmp`.
The recorder never nests a store mutation lock around an owner write.
It never unlinks a held lock inode.
A killed process releases its flock.
Signed history remains authoritative after index and lock-file loss.

The parent confirmed the source contract before implementation.
Source is a reported full revision.
The recorder validates its shape.
It does not add a Git-existence gate or claim execution proof.
Evidence reads check selected local bytes.
No command text executes.
No key or trust root is created implicitly.
Generic raw APIs, core wire formats, and runtime dependencies remain unchanged.

## Scenarios and review

Compiled CLI scenarios cover exact retries, source advancement, mutable evidence
loss, index loss, lock-file loss, conflicting retries, and simultaneous contenders.
Identical subprocesses return the same object ID.
Different facts under one key produce one successful admission and one conflict.
Canonical object counts verify the result.

The lost-response scenario starts the real CLI with 1,024 events.
It withholds stdout reads.
The resulting response exceeds pipe capacity.
The test independently verifies the published object through `ledger.Show`.
It kills that owned process and waits for termination.
A new CLI process recovers the same object and all 1,024 event refs.
The canonical object count stays one.

A real Git fixture checks immutable Git evidence after the source branch advances.
The fixture uses the shared Git helper that strips inherited `GIT_*` variables.
Protected canonical, trust, and key bytes remain unchanged.
No application mock mode or shell runner was introduced.

Real-store unit scenarios cover evidence refusal, pre-admission cancellation,
post-admission canceled index refresh, forged raw metadata, duplicate raw claims,
namespace/signer scope, untrusted signing without trust changes, unsafe lock paths,
validation before key access, and retry evidence overflow.

Fresh-eyes review covered seven code/test files and the API document.
It checked lock order, publication boundaries, digest scope, raw claims, and error output.
It found a stale-fingerprint report after new admission with an obstructed index.
A red regression showed the pre-admission fingerprint leaking into that result.
The fix leaves the fingerprint empty until the rebuilt snapshot is known.
A second regression made retry evidence overflow explicit as `limit_exceeded`.
The admitted object remains recoverable.

The first source commit attempt failed its `go fix -diff` hook.
The hook required `maps.Copy` in the lost-response fixture.
`go fix ./tests/e2e` applied that change.
All hooks remained enabled for the next attempt.
`PREK_HOME=/Users/harper/.cache/prek-khqq.QItPsm` was used for every commit.

## Retained development runs

All paths below are relative to `docs/status/development-workflow/evidence/`.
Go commands ran through `env -u GOROOT mise exec --` in this worktree.

| Log | Command or stage | Exit |
| --- | --- | --- |
| `x9r1-red-cli.log.gz` | `go test ./tests/e2e -run 'TestCLIDevelopmentRecord\|TestCLIRecordConcurrent' -count=1`; missing record command | 1 |
| `x9r1-green-cli.log` | Same selector with `./internal/workflow`; intermediate state-name expectation | 1 |
| `x9r1-green-core.log.gz` | Focused core/CLI selector; intermediate nil evidence fixture | 1 |
| `x9r1-red-human.log` | `go test ./cmd/pact -run TestRecord -count=1`; missing human renderer | 1 |
| `x9r1-green-record.log` | `go test ./internal/workflow ./cmd/pact ./tests/e2e -run 'TestRecord\|TestCLIDevelopmentRecord\|TestCLIRecord' -count=1` | 0 |
| `x9r1-red-fingerprint.log` | `go test ./internal/workflow -run TestRecordIndexPending -count=1` | 1 |
| `x9r1-focused.log` | Focused package suite; intermediate byte-slice test comparison | 1 |
| `x9r1-lint-first.log` | `golangci-lint run`; same test comparison | 1 |
| `x9r1-lint-second.log` | `golangci-lint run`; owner context signatures and integer conversion | 1 |
| `x9r1-focused-fixed.log` | `go test ./internal/workflow ./cmd/pact ./tests/e2e` | 0 |
| `x9r1-red-evidence-limit.log` | `go test ./internal/workflow -run TestRecordRetryReportsEvidenceLimit -count=1` | 1 |
| `x9r1-lint-fixed.log` | `golangci-lint run` | 0 |
| `x9r1-focused-final.log` | `go test ./internal/workflow ./cmd/pact ./tests/e2e` | 0 |
| `x9r1-unit-final.log` | `go test ./internal/workflow -count=1` | 0 |
| `x9r1-lint-precommit.log` | `golangci-lint run` | 0 |
| `x9r1-source-commit.log` | Source commit hooks; `go fix -diff` refused fixture style | 1 |

Two raw failure logs use gzip to preserve trailing diagnostic whitespace through repository hooks.
Read those exact bytes with `gzip -dc FILE`.
Early log names describe the intended stage.
The exit column records the actual outcome.
No failed attempt is presented as a passing gate.
Pre-freeze lint used this worktree's `.cache/x9r1-lint` directory.

## Known limits and recommendation

- Moderate, accepted scope: cooperative locking covers this local recorder.
  Raw writers can add conflicting claims or stale the returned index snapshot.
  Raw conflicts remain visible on reconciliation.
- Moderate, inherited API limit: store opening, key loading, and ledger commit lack context parameters.
  Cancellation cannot interrupt those owner calls.
  Post-error reconciliation uses `context.WithoutCancel` to recover possible admission.
- Minor: the recorder scans bounded canonical history for every operation.
  It favors durable recovery over a separate mutable operation journal.
- Proof limit: source labels and execution outcomes are signed caller assertions.
  Evidence matching does not prove execution, reviewer independence, or acceptance support.

No remaining implementation defect was found in the self-review.
Proceed to the parent's independent review before closing the task.
The parent owns PACT provenance and Kata closure.
This agent did not edit `.pact`, the parent progress tracker, or Kata state.

## Frozen-source final gates

Source: `7472ddc70f1a17e901863caa7aaaf7346c2b6581`.
Only documentation and retained proof files were written after this source commit.
`git diff --exit-code` passed after the gates.
The source-commit retry passed every hook.
Its output is `evidence/x9r1-source-commit-fixed.log`.

| Actual command | Exit | Retained log |
| --- | --- | --- |
| `env -u GOROOT mise exec -- scripts/check` | 0 | `evidence/x9r1-final-check.log` |
| `env -u GOROOT mise exec -- go test -race ./...` | 0 | `evidence/x9r1-final-race.log` |
| `env -u GOROOT GOLANGCI_LINT_CACHE=/Users/harper/.codex/worktrees/3f32/pact/.cache/x9r1-final-lint mise exec -- golangci-lint run` | 0 | `evidence/x9r1-final-lint.log` |

The final lint cache did not exist before that invocation.
Lint reported `0 issues.`
The canonical gate ran vet, Go tests, a compiled CLI lifecycle, and the Python reference suite.
The reference suite passed all 18 tests.
The race gate passed every package.
The new workflow package and E2E package ran uncached in that race invocation.
No warnings or failing checks remain.

Observed tool versions were Go 1.26.6 on darwin/arm64, golangci-lint 2.13.2,
and Git 2.54.0 (Apple Git-157).
The lint binary reports that it was built with Go 1.27.0.
The project's pinned Go toolchain ran the build and test commands.

Exact log SHA-256 values are retained in
[evidence/x9r1-sha256.txt](evidence/x9r1-sha256.txt).
The frozen source ID is retained in
[evidence/x9r1-source.txt](evidence/x9r1-source.txt).
The proof commit contains the report, all listed logs, and the hash manifest.
