<!-- ABOUTME: Defines the adoption implementation kata and its observable completion criteria. -->
<!-- ABOUTME: Gives a fresh implementer file targets, dependencies, scenarios, and proof requirements. -->

# 8. Validate fresh-context handoffs and publish the supported workflow

Planning state: Open; no implementation has run.
Priority: 2.
Depends on task keys: lifecycle.

## File targets

- docs/status/development-workflow-evaluation.md
- docs/pact-ledger/SKILL.md
- README.md
- cmd/pact/quickstart_command_test.go
- gotchas.md

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

Run a fresh-context comparison on the completed public fixture, publish the
supported recording/read recipe, and record an honest adoption decision.
Update the existing skill source; do not create a duplicate guide.

## Acceptance criteria

- Use isolated fresh contexts with no prior transcript or broad status reports.
  Both receive identical public fixture, source goal, and answer rubric.
- The baseline uses the current working log/query/show guide. The candidate uses
  only actually shipped capabilities; an unpublished brief stays unpublished.
- Both identify open work, blockers, exact source/proof, evidence availability,
  and local policy limits. Count calls, output bytes, failures, and unsupported
  assertions; record model/version and exact source revision.
- A green check with an open Important finding never becomes accepted. Missing
  acceptance remains not_recorded. A later conflicting claim remains visible.
- Publish all results, including ties/regressions and command omissions. One pair
  supports only a local observation, not a general performance claim.
- Update README and the single skill source with actual commands, pre-sign
  validation, retry/index recovery, exact Git evidence, and layered outcomes.
- Verify the embedded quickstart matches its source and no proposed-only command
  appears as shipped. Explain shared-key independence and local-root limits.
- Link Kata completion evidence to source/test commits and signed PACT events.
  Close the epic only after every child is verified and the adoption result is
  documented, including any justified command-publication rejection.

## TDD steps

- [ ] Write the handoff answer rubric and embedded-guide assertions before edits.
- [ ] Run the paired fresh-context exercise and retain nonprivate transcripts and
      measured results; do not send private consumer data to a model.
- [ ] Update the original guide/README to match the proven selected workflow.
- [ ] Run guide tests, canonical gate, race, lint, and real documented CLI usage.
- [ ] Perform final review, record decisions/proof/known limits in PACT, and close
      each verified child before closing the epic. No release or main merge is
      authorized by this planning task alone.

## Verification

~~~sh
env -u GOROOT mise exec -- go test ./cmd/pact ./tests/e2e
~~~
~~~sh
env -u GOROOT mise exec -- scripts/check
~~~
Expected: documented commands work, quickstart source matches, and the evaluation
report states the actual correctness and read-effort result.
