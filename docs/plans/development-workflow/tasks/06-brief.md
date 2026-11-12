<!-- ABOUTME: Defines the brief implementation kata and its observable completion criteria. -->
<!-- ABOUTME: Gives a fresh implementer file targets, dependencies, scenarios, and proof requirements. -->

# 6. Evaluate and expose a bounded development work brief

Planning state: Open; no implementation has run.
Priority: 2.
Depends on task keys: recording, projection.

## File targets

- cmd/pact/brief_command.go (conditional on evaluation)
- cmd/pact/command_catalog.go
- cmd/pact/brief_render.go
- cmd/pact/brief_command_test.go
- tests/e2e/development_brief_test.go
- docs/status/development-read-evaluation.md

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

Compare a local work-brief prototype with the working log/query/show baseline.
Expose the proposed pact brief command only after the measured read-path gate
passes. A tie/regression leaves the existing guide as default and records the
decision; it does not justify shipping a redundant command.

## Interfaces

Consume development.Project and WorkState. Proposed flags and budgets are in
design.md section 4. Human and JSON output share one state representation and
one exact encoded-byte accounting rule. No new source of task state is created.

## Acceptance criteria

- Both read paths use the same public fixture and task rubric, including
  unresolved Important findings despite a green check and an absent acceptance.
- Measure actual commands, returned bytes, failures, missed blockers, and
  unsupported claims. Correctness is mandatory; report mixed measurements
  instead of hiding a regression behind a single score.
- The promotion criterion is fewer tool calls with no increase in returned bytes,
  no missed/changed facts, and no unsupported acceptance on the complete rubric.
- The command, if promoted, reports exact frontier/profile/policy/projector/source, scoped
  checks, open/resolved findings, explicit decisions, actually checked evidence,
  uncertainty, and declared next actions. It links supporting exact event refs.
- Default encoded output is at most 16 KiB; maximum requested budget is 64 KiB,
  both including JSON newline. Exact-bound tests include escaped text.
- Required facts that cannot fit cause an explicit bounded limit error with
  narrowing instructions; do not return a deceptively accepted truncated view.
- Missing/stale indexes produce recovery guidance without writes. Existing
  log/query/show semantics and cursor meanings remain unchanged.
- A negative evaluation is a valid result: retain the tested projection/library,
  document the existing read recipe, and omit unpublished CLI files/catalog rows.

## TDD steps

- [ ] Capture the baseline using existing documented commands and exact event
      shows; implement the prototype and compare the same answer rubric.
- [ ] If the gate passes, add failing human/JSON/byte-bound/read-only CLI cases
      before registering or implementing the public command.
- [ ] If the gate fails, retain the report and test the documented existing recipe
      against the same real store; do not call the brief command implemented.
- [ ] Exercise the selected read path with a compiled executable, real SQLite,
      missing index, conflicts, and a tight output budget.
- [ ] Run canonical/race/lint gates, review required-fact retention, and record
      the measured decision and evidence in PACT.

## Verification

~~~sh
env -u GOROOT mise exec -- go test ./cmd/pact ./internal/development ./tests/e2e
~~~
~~~sh
env -u GOROOT mise exec -- scripts/check
~~~
Expected: correctness rubric passes on the selected read path; publication follows
the explicit measured gate rather than an assumed benefit.
