<!-- ABOUTME: Defines the lifecycle implementation kata and its observable completion criteria. -->
<!-- ABOUTME: Gives a fresh implementer file targets, dependencies, scenarios, and proof requirements. -->

# 7. Prove the complete recording and handoff lifecycle

Planning state: Open; no implementation has run.
Priority: 1.
Depends on task keys: recording, projection, brief.

## File targets

- tests/e2e/development_lifecycle_test.go
- tests/fixtures/development/
- scripts/check
- scenarios.jsonl

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

Add one real compiled-CLI journey from a failed review through correction,
verification, explicit resolution, and supported local acceptance. Extend the
canonical gate with the journey using existing E2E infrastructure.

## Interfaces

Consume the record command, evidence command, and selected read path from earlier
katas. The brief may be an unpublished prototype if its promotion gate failed;
the public lifecycle then uses existing query/show plus the tested projector.

## Acceptance criteria

- Create a temporary real Git repository, external test key, signed store, and
  real SQLite index. Never use a mock mode, mocked Git/API, or private fixture.
- Record the initial failed review and a passing check; the read path must still
  report the blocking finding and absent acceptance.
- Record the correction on an exact committed source, a resolving review, and
  explicit matching acceptance. Only then can local support become positive.
- Modify mutable evidence while retaining original Git bytes; prove the two
  assessments differ and historical proof remains recoverable.
- Terminate an owned process after admission, drop the derived index, and resume
  with the same operation ID in a new process. Assert one canonical operation.
- Reject an invalid partial outcome without creating an object.
- Add concurrent/unresolved and oversized-required-fact cases. Neither may return
  positive support by silently choosing or truncating history.
- Capture byte hashes of preexisting canonical objects/trust and compare afterward.
- scripts/check retains all prior production/reference gates; add no exclusion,
  new warning, timeout workaround, or weakened assertion.

## TDD steps

- [ ] Write each journey assertion before its corresponding integration change;
      run it to capture the missing behavior.
- [ ] Make the smallest wiring changes needed; fix defects in their owning kata
      modules with regression coverage rather than test-only bypasses.
- [ ] Run the compiled journey and existing complete test suite.
- [ ] Run canonical check, full Go race suite, lint, and real CLI help/usage.
- [ ] Review the actual production delta and failure/recovery logs; resolve
      important findings before recording a passing gate in PACT.

## Verification

~~~sh
env -u GOROOT mise exec -- scripts/check
~~~
~~~sh
env -u GOROOT mise exec -- go test -race ./...
~~~
~~~sh
env -u GOROOT mise exec -- golangci-lint run
~~~
Expected: old and new journeys pass with no new warnings and preserved history.
