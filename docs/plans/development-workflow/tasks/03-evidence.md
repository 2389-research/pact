<!-- ABOUTME: Defines the evidence implementation kata and its observable completion criteria. -->
<!-- ABOUTME: Gives a fresh implementer file targets, dependencies, scenarios, and proof requirements. -->

# 3. Resolve and check local files and immutable Git evidence

Planning state: Open; no implementation has run.
Priority: 1.
Depends on task keys: contract.

## File targets

- internal/evidence/resolve.go
- internal/evidence/git.go
- internal/evidence/resolve_test.go
- cmd/pact/evidence_commands.go
- cmd/pact/command_catalog.go
- tests/e2e/development_evidence_test.go

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

Implement the bounded local resolver and proposed pact evidence check adapter.
Use repository-relative regular files and git:<full-object-id>:<relative-path>.
This is the first v1 resolver; it adds no fallback for old absolute locators.

## Interfaces

Produce evidence.Request with Repo, Ref, Digest strings and MaxBytes int64.
Produce evidence.Assessment with Ref, Digest, ActualDigest, Status strings and
Bytes int64. evidence.Resolve(context.Context, Request) returns
(Assessment, error). Status is matching, changed, unavailable, unsupported, or
unchecked; transport/process/cancellation errors stay distinguishable.

The CLI consumes an explicit --event-ref, reads evidence with ledger.Show, and
reports each selected assessment without appending any event.

## Acceptance criteria

- Changing a working file yields changed for its mutable ref; selecting the
  retained Git revision/path yields matching for the original exact bytes.
- A missing Git blob is unavailable; an unsupported scheme is unsupported.
- Only explicit local repository scope is read. Reject absolute paths, traversal,
  symlink escapes, credential-bearing locators, and Git option injection.
- Invoke Git with process arguments and bounded streaming, no shell/network/pager.
  Include adversarial revision/path arguments in real Git subprocess tests.
- Enforce 16 MiB per item and 64 MiB aggregate selected evidence. Refuse overflow;
  do not hash a prefix and report a full digest.
- No raw evidence contents enter errors, normal CLI output, or canonical objects.
- Reading evidence never changes ledger objects, trust, refs, or index state.
- The resolver's exact bytes and its verification time are distinct from an
  event author's original claim and from ledger signature verification.

## TDD steps

- [ ] Build a temporary real Git repository containing original evidence and a
      committed later edit; add failing current-vs-historical-byte scenarios.
- [ ] Implement the resolver and limits, then adversarial path/process cases.
- [ ] Add real signed-event/SQLite integration tests for evidence selection.
- [ ] Add compiled CLI E2E for matching, changed, unavailable, unsupported,
      overflow, and non-disclosure. No mocked Git or mock application modes.
- [ ] Run unit, integration, CLI, canonical, race, and lint gates.
- [ ] Review the new read/process boundary before release; record findings,
      corrections, exact proof, and limits in PACT.

## Verification

~~~sh
env -u GOROOT mise exec -- go test ./internal/evidence ./cmd/pact ./tests/e2e
~~~
~~~sh
env -u GOROOT mise exec -- scripts/check
~~~
Expected: all evidence states and refusal paths match the contract with no writes.
