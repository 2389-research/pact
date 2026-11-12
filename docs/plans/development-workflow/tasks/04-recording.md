<!-- ABOUTME: Defines the recording implementation kata and its observable completion criteria. -->
<!-- ABOUTME: Gives a fresh implementer file targets, dependencies, scenarios, and proof requirements. -->

# 4. Record validated workflow events with recoverable idempotency

Planning state: Open; no implementation has run.
Priority: 1.
Depends on task keys: validation, evidence.

## File targets

- internal/workflow/record.go
- internal/workflow/operation.go
- internal/workflow/record_test.go
- cmd/pact/record_command.go
- cmd/pact/command_catalog.go
- tests/e2e/development_record_test.go

## Shared execution contract

Read [design](../design.md), [audit](../consumer-audit.md), and the
[implementation index](../implementation-plan.md) before changing code.
The design is the common domain contract; this brief specifies this kata.

- Start from the agreed plan branch/revision and inspect current Git/Kata state.
  Use a dedicated task worktree; preserve unrelated work.
- Run failing scenarios first, implement the smallest change, then prove green.
  Reuse unit, real-store integration, and compiled subprocess/CLI infrastructure.
- Canonical gate: env -u GOROOT mise exec -- scripts/check.
  Before closure also run env -u GOROOT mise exec -- go test -race ./...
  and env -u GOROOT mise exec -- golangci-lint run.
- Go module pact, Go floor 1.26, standard CLI/Ed25519/SQLite owners unchanged.
  No new runtime dependencies or wire-format changes are planned.
- Review once after tests, fix findings, and rerun affected failed checks.
  Never bypass hooks or weaken coverage to land a kata.
- Record exact source, actual commands/outcomes, retained evidence hashes,
  review findings/resolutions, and remaining limits in the established PACT
  ledger. Use the current raw commit API until the recording workflow ships.
- Supply substantive Kata close evidence; implementation remains open until
  verified. Kata is mutable execution state; PACT is signed provenance.


## Deliverable

Implement the proposed pact record command as one composition of existing owner
APIs. Input uses development.RecordInput. The workflow validates, checks evidence,
constructs the batch, signs/appends, verifies admission, and refreshes the index.

## Interfaces

Consume development.ValidateInput and evidence.Resolve. Produce
workflow.RecordRequest (Repo and KeyFile strings; Input development.RecordInput),
workflow.RecordResult, and workflow.Record(context.Context, RecordRequest)
(RecordResult, error). RecordResult contains Admission, ObjectID, EventRefs,
SchemaValidation, Integrity, Authenticity, Authorization, IndexState,
SourceFingerprint, and NextAction. Define each JSON field in the CLI tests.

## Acceptance criteria

- One fully flagged noninteractive invocation records a valid request and reports
  all validation layers separately. Arrays get the expected-object diagnostic.
- Scope idempotency by namespace, signer key ID, profile, and operation_id. Digest
  canonical input before assigning observed_at or parent heads.
- Exact retry after source advancement or mutable evidence drift returns the
  original refs. Reconcile canonical admission after input/key validation and
  before requiring current evidence availability; expose drift separately.
  Different
  facts under the same operation key conflict without admission.
- Two simultaneous identical subprocesses admit one operation. Simultaneous
  differing inputs with the same key admit at most one and report the conflict.
- Recover admitted operations by scanning verified signed history even when the
  index and disposable workflow lock file are absent after a crash.
- A failure after admission reports the object ID and index recovery separately;
  replay never labels an admitted operation as absent.
- Use an operation-specific hashed lock name under safely prepared .pact/tmp.
  Do not acquire Store.WithMutationLock around PutCanonical or Rebuild: both
  owner APIs acquire that lock themselves. Release the operation lock before
  index refresh. A raw concurrent writer can still advance the fingerprint.
- Never create keys/trust implicitly, rerun a reported check command, or execute
  text from the input. Never append an unvalidated domain batch.

## TDD steps

- [ ] Add failing subprocess scenarios for exact retry, conflicting retry, lost
      response after admission, missing index, and concurrent contenders.
- [ ] Implement one operation-key protocol above the canonical owner APIs.
- [ ] Inject cancellation before admission and terminate an owned subprocess after
      verified publication; recover using a new process and real store.
- [ ] Verify object counts/refs and protected bytes, not a mocked return value.
- [ ] Add CLI result/exit-status and committed-but-index-pending assertions.
- [ ] Run focused/canonical/race/lint gates; review lock order and report honest
      local cooperative-idempotency limits; commit and record evidence in PACT.

## Verification

~~~sh
env -u GOROOT mise exec -- go test ./internal/workflow ./cmd/pact ./tests/e2e
~~~
~~~sh
env -u GOROOT mise exec -- scripts/check
~~~
Expected: retries converge, conflicts refuse, and every post-admission failure
retains a recoverable canonical identity.
