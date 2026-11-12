<!-- ABOUTME: Defines the validation implementation kata and its observable completion criteria. -->
<!-- ABOUTME: Gives a fresh implementer file targets, dependencies, scenarios, and proof requirements. -->

# 2. Validate development inputs before immutable signing

Planning state: Open; no implementation has run.
Priority: 1.
Depends on task keys: contract.

## File targets

- internal/development/validate.go
- internal/development/validate_test.go
- internal/development/schema.go
- internal/ledger/event.go (reuse; no domain logic added)

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

Convert a development.RecordInput into a normalized ledger.EventBatch only after
profile and domain checks pass. Validate exact supported schema identity before
any signing key is used or canonical object is written.

## Interfaces

Consumes development.RecordInput and ProfileArtifacts from the contract kata.
Produces development.ValidateInput(context.Context, RecordInput)
(ledger.EventBatch, error). Profile supplies kind and schema_ref; caller input
cannot replace them. Return bounded errors with a field path and error category.

## Acceptance criteria

- All design.md section 3 outcomes, verdicts, severities, required fields, full
  source revisions, and stable finding identities receive strict validation.
- The consumer's partial-status defect fails before signing and creates no object.
- Reject unknown event types, extra fields, arrays at the top level, unsupported
  profile/schema digests, contradictory outcome/exit data, and malformed refs.
- Validation runs core ledger.NormalizeEventBatch after domain conversion, so
  immutable-secret and envelope rules still apply. Do not duplicate that scanner.
- Diagnostics explain the required object shape without echoing event contents.
- The raw commit API retains its generic behavior. Report raw unknown domains as
  unchecked; do not reinterpret historical events under development/v1.
- Validation itself is read-only, including on rejection and cancellation.

## TDD steps

- [ ] Turn every negative corpus input into a failing pre-sign scenario; compare
      protected canonical files and trust bytes before and after.
- [ ] Implement conversion/validation using the shared profile definitions.
- [ ] Add real-store integration tests that validate, commit, and read a valid
      batch, and prove invalid batches never reach admission.
- [ ] Exercise the full library pipeline in a compiled subprocess test, with a
      real external test key and temporary repository; no mocked signing/store.
- [ ] Run targeted tests and canonical gate, review bounded diagnostics and
      secret handling, commit, then record the result and proof in PACT.

## Verification

~~~sh
env -u GOROOT mise exec -- go test ./internal/development ./internal/ledger
~~~
~~~sh
env -u GOROOT mise exec -- scripts/check
~~~
Expected: every invalid fixture refuses before mutation; valid fixtures round-trip.
