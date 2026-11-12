<!-- ABOUTME: Defines the projection implementation kata and its observable completion criteria. -->
<!-- ABOUTME: Gives a fresh implementer file targets, dependencies, scenarios, and proof requirements. -->

# 5. Derive development state without losing findings or uncertainty

Planning state: Open; no implementation has run.
Priority: 1.
Depends on task keys: validation, evidence.

## File targets

- internal/development/project.go
- internal/development/policy.go
- internal/development/project_test.go
- internal/development/schema/ (generated policy artifact)
- internal/ledger/scan.go (reuse existing snapshot APIs)

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

Build the deterministic development/v1 projection for an exact subject, source,
and local frontier. The projector interprets structured facts and exact links;
it does not summarize prose into authoritative state.

## Interfaces

Produce development.ProjectRequest (Namespace, Subject, SourceRevision strings;
Frontier []string) and development.WorkState. WorkState identifies Frontier,
ProfileDigest, PolicyDigest, ProjectorIdentity, SourceRevision, Checks, Findings, RecordedDecisions,
SupportedAcceptance, DeclaredNextActions, EvidenceAssessments, and Completeness.
development.Project(context.Context, *store.Store, ProjectRequest)
(WorkState, error) consumes one verified frozen snapshot and the profile/resolver.

## Acceptance criteria

- The requested frontier determines the reachable snapshot; do not scan newer
  history and quietly include it. Omitted frontier means capture current heads
  once and report them. SourceRevision is always explicit.
- Passing checks cannot close findings. A source change cannot close findings.
  Only a valid explicit resolving review on its named correction source does so.
- A recorded acceptance is distinguished from support under the named local
  policy, missing acceptance, and an unsupported/inconclusive assessment.
- Review/check refs must exist, be causally observed by the acceptance, and match
  its source. A caused_by link alone is insufficient resolution evidence.
- Relevant unknown schemas, invalid payloads, missing refs, unknown authority,
  unmatched durable evidence, or open critical/important findings block a
  positive supported-acceptance result.
- Later causal blocking findings make an old decision insufficient at the new
  inspected frontier. Concurrent incompatible claims stay unresolved.
- Minor findings remain visible. Next actions come only from declared events.
- Same input cut, profile/policy bytes, and evidence assessment inputs yield the
  same state. Evidence observation time is separate metadata, not sort authority.
- Record the projector version/digest separately from the target work's source;
  an unknown projector identity cannot support a provenance-complete result.
- Never claim reviewer independence, organizational approval, globally complete
  history, or official checkpoint acceptance from the local root/profile.

## TDD steps

- [ ] Add failing unit cases for green tests/open findings, explicit resolution,
      wrong-source proof, later blocking findings, and concurrent decisions.
- [ ] Implement the smallest policy over typed events and exact references.
- [ ] Add real-store branch/frontier integration scenarios and missing-dependency
      refusals. Compare cut A before and after appending unrelated cut B.
- [ ] Run a compiled projection driver against signed fixture history and compare
      canonical expected-state bytes; preserve all unresolved claims.
- [ ] Run focused/canonical/race/lint gates, review positive-result conditions,
      commit and record source, proof, findings, and policy identity in PACT.

## Verification

~~~sh
env -u GOROOT mise exec -- go test ./internal/development ./internal/ledger
~~~
~~~sh
env -u GOROOT mise exec -- scripts/check
~~~
Expected: all fixture states match the rubric; no unsupported acceptance succeeds.
