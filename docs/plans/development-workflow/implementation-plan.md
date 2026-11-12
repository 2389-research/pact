<!-- ABOUTME: Orders the development workflow katas and their verification gates. -->
<!-- ABOUTME: Gives a fresh session the design boundary, current planning state, and next action. -->

# PACT Development Workflow Implementation Plan

> For agentic workers: use the executing-plans skill to implement this plan
> task by task. Delegation is a later execution choice, not a step performed by
> this planning session. Steps and completion evidence live in each linked brief.

**Goal:** Make development results easy to record and supported work state easy
to recover, while retaining PACT's signed domain-neutral ledger.

**Architecture:** Add a fixed development/v1 profile, a bounded repository/Git
evidence resolver, a reliable recording composition, and a deterministic work
projection. Reuse existing canonical, identity, store, ledger, index, and command
catalog owners. Publish a new brief only after comparison with existing reads.

**Tech stack:** Existing Go module pact (Go floor 1.26), standard-library CLI,
Ed25519, and the current SQLite index. No new runtime dependency is planned.

## Execution outcome

All eight katas and epic pact#ns4y are closed at the verified completion cut.
Tested source: 5782f2499825d6090f8838ed755cffb2edbf713e.
Final acceptance: PACT34d62c13#acceptance, namespace org/2389/pact.
The measured gate rejected public pact brief publication.
See [execution completion](../../status/development-workflow/completion.md) for proof and remaining limits.
The task branch remains unmerged; no push, release, or deployment was performed.

## Historical planning cut

- Direction and planning authorized by Doctor Biz on 2026-11-11.
- Baseline source: fd1f227794d6bf9956bd8eea0798254e0134918f.
- Planning branch: docs/development-workflow-plan.
- Managed planning worktree: /Users/harper/.codex/worktrees/3f32/pact.
- Canonical local ledger: /Users/harper/Public/src/2389/pact/.pact.
- Namespace: org/2389/pact. Signer: the existing locally bootstrapped root.
- Epic: pact#ns4y. All eight implementation katas are open.
- Current task: documentation and planning only. No proposed command exists yet.
- Next execution step: read the shared design, claim pact#khqq,
  inspect current code/Kata state, and start its failing contract scenario.
- Do not merge main, deploy, or cut a release as part of this planning task.
- Compactions during this planning task: 0. Update this state at a landed phase.

Read [design.md](design.md), [consumer-audit.md](consumer-audit.md),
[epic.md](epic.md), then the selected task brief. The existing successful
[resume experiment](../../status/agent-resume-index-recovery.md) is the comparison
baseline. Do not replace it with an assumed failed baseline.

## Global constraints

- Preserve existing canonical bytes, signatures, envelope, causal semantics, raw
  commands, query cursor meaning, root trust, and core limits.
- Keep development-specific meaning outside internal/ledger and internal/store.
- Profile validation is fixed and offline; arbitrary domain-schema and policy
  execution remain outside the engine's claims.
- Versioned source, explicit finding resolution, selected proof, and a named
  local policy govern the derived work view.
- Signature, authority, schema validity, evidence availability, and supported
  acceptance are separate results.
- No hidden index mutation on reads. Recording may compose explicit index refresh
  and must distinguish admission from index readiness.
- No new runtime dependencies, daemon, sync, trust change, private-data import,
  legacy locator fallback, or rewrite of historical consumer records.
- New hand-written comment-capable source files receive the existing two-line
  ABOUTME header. Generated schema/data files are exempt.
- Use named retained evidence; do not overwrite a signed report/log.
- Kata owns live execution status and dependencies. This document and katas.json
  describe the planning cut; they are not a second live task database.

## Kata queue

| Order | Kata | Deliverable | Must finish first |
|---|---|---|---|
| 1 | pact#khqq | [Define the development profile and synthetic scenario corpus](tasks/01-contract.md) | none |
| 2 | pact#3ezv | [Validate development inputs before immutable signing](tasks/02-validation.md) | pact#khqq |
| 3 | pact#spns | [Resolve and check local files and immutable Git evidence](tasks/03-evidence.md) | pact#khqq |
| 4 | pact#x9r1 | [Record validated workflow events with recoverable idempotency](tasks/04-recording.md) | pact#3ezv, pact#spns |
| 5 | pact#pzm4 | [Derive development state without losing findings or uncertainty](tasks/05-projection.md) | pact#3ezv, pact#spns |
| 6 | pact#w5cf | [Evaluate and expose a bounded development work brief](tasks/06-brief.md) | pact#x9r1, pact#pzm4 |
| 7 | pact#z895 | [Prove the complete recording and handoff lifecycle](tasks/07-lifecycle.md) | pact#x9r1, pact#pzm4, pact#w5cf |
| 8 | pact#k7cc | [Validate fresh-context handoffs and publish the supported workflow](tasks/08-adoption.md) | pact#z895 |

~~~mermaid
flowchart TD
  A["khqq: profile and corpus"] --> B["3ezv: validation"]
  A --> C["spns: evidence"]
  B --> D["x9r1: recording"]
  C --> D
  B --> E["pzm4: projection"]
  C --> E
  D --> F["w5cf: brief evaluation"]
  E --> F
  F --> G["z895: lifecycle"]
  D --> G
  E --> G
  G --> H["k7cc: adoption"]
~~~

Readiness uses native Kata blocking edges. Parent containment alone does not gate
readiness. Inspect the execution queue without the epic:

~~~sh
kata ready --project pact --no-label epic --agent
kata show --project pact khqq --agent
~~~

## Execution checklist

- [x] pact#khqq: Define the development profile and synthetic scenario corpus.
- [x] pact#3ezv: Validate development inputs before immutable signing.
- [x] pact#spns: Resolve and check local files and immutable Git evidence.
- [x] pact#x9r1: Record validated workflow events with recoverable idempotency.
- [x] pact#pzm4: Derive development state without losing findings or uncertainty.
- [x] pact#w5cf: Evaluate the bounded brief and reject public command publication.
- [x] pact#z895: Prove the complete recording and handoff lifecycle.
- [x] pact#k7cc: Validate fresh-context handoffs and publish the supported workflow.
- [x] Close epic pact#ns4y only after every verified child is closed
      and the final adoption decision/evidence is recorded.

Validation and evidence can proceed after the contract; recording and projection
share their completed prerequisites. Use separate task worktrees if executing
independent tasks concurrently. Coordinate commits rather than sharing staging.

## Contract and file map

All new symbols and commands described in design/tasks are implementation targets,
not claims about existing code. Each task defines the interfaces it produces.
Use the same names and fields in downstream tasks; change this contract explicitly
if real implementation evidence requires a correction.

Existing owners to inspect:

| Existing source | Responsibility to retain |
|---|---|
| internal/ledger/event.go | Core envelope, refs, secret-hazard checks |
| internal/ledger/commit.go | Signing, admission, event refs |
| internal/ledger/scan.go and graph.go | Verified canonical snapshot and causality |
| internal/store/store.go | Store safety, mutation/read locks, canonical writes |
| internal/index/rebuild.go and hydrate.go | Disposable index and bounded canonical reads |
| cmd/pact/command_catalog.go | Command registration and shared help |
| cmd/pact/ledger_commands.go | Existing raw commit/show/verify adapters |
| tests/e2e/cli_test.go and tests/e2e/index_query_test.go | Real compiled-process lifecycle infrastructure |
| scripts/check | Canonical production/reference gate |
| docs/pact-ledger/SKILL.md | Single source for the embedded operator guide |

Proposed package ownership and exact per-task file targets appear in the briefs.
Do not refactor unrelated modules to match the proposed layout.

## Requirement coverage

| Design section / invariant | Owning katas |
|---|---|
| 1–2: additive scope, domain-neutral core, owners | khqq; all integration reviews |
| 3: structured source/outcome/finding/decision facts | khqq, 3ezv |
| 4: record input/results and explicit errors | 3ezv, x9r1 |
| 4: bounded brief, exact source, no silent truncation | pzm4, w5cf |
| 5: Git recovery, selected evidence, read/process boundaries | spns |
| 6: idempotency, conflict, crash and index recovery | x9r1, z895 |
| 7: exact-cut projection, explicit closure, conflicts, uncertainty | pzm4, z895 |
| 8: baseline comparison and conditional brief publication | w5cf, k7cc |
| 9: unit/integration/compiled CLI, canonical/race/lint, guide | all applicable task boundaries; z895, k7cc aggregate |

The aggregate lifecycle supplements task coverage; it never excuses a kata from
testing its own new behavior before closure. A library task uses a compiled test
driver for its complete pipeline before the public CLI adapter exists.

## Verification and PACT protocol

For each implementation kata:

1. Run its failing scenario and retain the raw failure.
2. Implement the smallest change and run its unit, real-store integration, and
   compiled subprocess/CLI scenarios. Keep input/output and source identity exact.
3. Run the canonical command and missing whole-project gates:

~~~sh
env -u GOROOT mise exec -- scripts/check
env -u GOROOT mise exec -- go test -race ./...
env -u GOROOT mise exec -- golangci-lint run
~~~

4. Perform one review pass on actual changed production behavior. Fix findings
   and rerun failed/affected checks. Never infer approval from a green suite.
5. Commit exact paths with hooks. Record signed results, retained proof digests,
   unresolved findings, source revision, and explicit acceptance scope in PACT.
6. Read back the Kata/Pact records. Close only verified work with substantive
   prose and typed evidence; leave unresolved work open.

Until the new recorder ships, use the current supported raw workflow:

~~~sh
pact status --repo /Users/harper/Public/src/2389/pact --json
pact verify --repo /Users/harper/Public/src/2389/pact --strict --json
pact commit --repo /Users/harper/Public/src/2389/pact --key-file /Users/harper/.config/pact/keys/org-2389-pact-root.json --events /absolute/retained-event-batch.json --json
pact index rebuild --repo /Users/harper/Public/src/2389/pact --json
~~~

The example event file is an input to be authored, validated, and retained for
that real operation, not a supplied file or a runnable new API.
Private key bytes never enter docs, logs, Git, or payloads. Existing local-root
trust is not proof of independent reviewers or organizational delegation.

The planning worktree excludes its Git-restored .pact copy using sparse checkout;
all PACT commands explicitly select the main ledger. Do not initialize another.
A newly admitted object's exact bytes may be staged into the task branch through
Git's object/index APIs for review without creating another PACT store.

## Known risks and release recommendation

- High: a positive result can be wrong if unresolved findings, exact source, or
  causal proof are omitted. Prove the negative cases before accepting the feature.
- High: retry scans and store/index locks can deadlock or duplicate an operation
  if nested or scoped incorrectly. Require real concurrent and crash scenarios.
- High: Git/path resolution introduces a read/process boundary. Review local scope,
  option handling, symlink rejection, limits, and non-disclosure before release.
- Moderate: a new brief may add maintenance without reducing read effort. The
  promotion gate may reject it while the useful writer/evidence work still lands.
- Moderate: one shared root cannot prove independent review identity. Keep the
  limitation visible; do not expand authority work into this slice.

Fix the first three risks before shipping. A measured brief rejection and explicit
local-identity limits are acceptable documented outcomes.

## Planning verification evidence

The unchanged baseline passed scripts/check in the managed worktree before
planning edits. Retained output: [planning-check.log](evidence/planning-check.log).
Additional plan consistency, Kata readback, review, and PACT publication results
belong in [publication.md](publication.md); that file is written only after the
corresponding operations actually finish.
