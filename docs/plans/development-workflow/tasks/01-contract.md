<!-- ABOUTME: Defines the contract implementation kata and its observable completion criteria. -->
<!-- ABOUTME: Gives a fresh implementer file targets, dependencies, scenarios, and proof requirements. -->

# 1. Define the development profile and synthetic scenario corpus

Planning state: Open; no implementation has run.
Priority: 1.
Depends on task keys: none.

## File targets

- internal/development/profile.go
- internal/development/schema.go
- internal/development/profile_test.go
- internal/development/schema/ (generated profile artifacts)
- tests/fixtures/development/

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

Introduce the fixed development/v1 domain vocabulary and public synthetic inputs
without changing canonical PACT formats. This is the common contract used by all
later tasks. Source identities use a repository label and full committed Git
revision. All paths below are implementation targets, not existing APIs.

## Interfaces to produce

- development.Source: Repository and Revision strings.
- development.FindingRef: ReviewEventRef and FindingID strings.
- development.InputEvent: LocalID and Type strings; Payload map[string]any;
  Evidence []map[string]any; CausedBy and Supersedes []string.
- development.RecordInput: OperationID, Namespace, Subject strings; Source Source;
  Events []InputEvent.
- development.ProfileArtifacts() map[string][]byte: supported exact schema bytes
  and the named local policy artifact, generated from one profile definition.
- Fixtures cover the four event types and domain values specified in design.md
  sections 3 and 7. Store expected decisions separately from event summaries.

## Acceptance criteria

- Generate the supported schema files deterministically; a generation/drift test
  compares exact bytes and SHA-256 digests. Validators consume the same field and
  enum definitions; no second hand-maintained status list.
- Positive fixtures include an unresolved Important finding despite a passing
  check, then an explicit correction/resolution/acceptance on a second source.
- Negative fixtures include partial as a check outcome, missing source revision,
  a duplicate finding ID, mismatched check outcome/exit code, and a bad event ref.
- Concurrent incompatible claims, a relevant unresolved dependency, and an
  unknown schema each have an expected unsupported/incomplete result.
- Historical formats and raw commit semantics remain unchanged. There is no
  arbitrary external schema interpreter, registration claim, or new dependency.
- Audit measurements remain in consumer-audit.md; synthetic cases copy no private
  consumer transcript, source text, signing identity, or raw application output.

## TDD steps

- [ ] Add a deterministic fixture-generation scenario that fails before profile
      definitions exist; run it and retain the failure.
- [ ] Add the smallest shared definitions/generator and the positive corpus.
- [ ] Run the existing Python jsonschema oracle against generated schemas and
      positive/negative examples in a subprocess; keep its dependency within the
      existing reference test environment.
- [ ] Add real-store integration coverage that signs/reads positive envelope
      fixtures through the existing ledger API; do not claim domain validation
      from the raw commit command.
- [ ] Run profile unit tests, canonical compiled-CLI scenarios, and scripts/check.
- [ ] Review artifact determinism and vocabulary; commit the contract and record
      the exact source, checks, and remaining work in PACT.

## Verification

~~~sh
env -u GOROOT mise exec -- go test ./internal/development ./tests/e2e
~~~
~~~sh
env -u GOROOT mise exec -- scripts/check
~~~
Expected: new profile/corpus tests pass; existing conformance and CLI tests pass.
Full Go race and lint gates apply before the kata closes.
