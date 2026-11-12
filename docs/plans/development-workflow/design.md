<!-- ABOUTME: Defines the proposed PACT development recording and handoff workflow. -->
<!-- ABOUTME: Keeps domain rules above the signed ledger and makes proof limits explicit. -->

# PACT development workflow design

Date: 2026-11-11.
Status: Direction authorized by Doctor Biz; implementation remains open in Kata.
Baseline: fd1f227794d6bf9956bd8eea0798254e0134918f.
Evidence: [consumer audit](consumer-audit.md).
Execution: [implementation plan](implementation-plan.md).

## 1. Goal and scope

An agent should be able to record a development result with one operation, then
another agent should recover the supported state and exact proof before acting.
The initial workflow covers checks, reviews, finding resolution, acceptance,
and explicitly declared next actions.

This is an additive application layer in the existing Go CLI. The ledger remains
domain-neutral. Existing object bytes, signatures, event envelopes, DAG rules,
root trust, low-level commit, and read-only log/query behavior remain unchanged.
Preserving those contracts does not introduce a second historical format.

Kata owns mutable execution status, ownership, and dependencies. PACT owns
signed planning and execution assertions with evidence references. Repository
documents own the design and implementation instructions. The work brief is a
derived view of PACT, not another authoritative task database.

No sync, new trust/delegation model, daemon, hosted service, general policy
interpreter, automatic release, arbitrary shell runner, or historical consumer
migration belongs to this plan. The consumer's existing invalid event remains
history; do not edit it or change its old schema to make it pass.

## 2. Architecture

- internal/ledger and internal/store retain canonical admission and crypto.
- internal/development owns a fixed development/v1 vocabulary, supported schema
  bytes, domain validation, and a versioned local projection policy.
- internal/evidence resolves selected repository files and exact Git blobs.
- internal/workflow composes validation, evidence checks, retry handling,
  ledger.Commit, post-write verification, and index refresh.
- cmd/pact adds adapters to the existing command catalog and renderers.

Do not put development statuses into internal/ledger/event.go. Reuse its
NormalizeEventBatch validation after constructing every domain event. Use
index.New(st).Query, ledger.Show/Scan, and existing limits rather than treating
SQLite rows as canonical records.

The fixed profile uses ordinary Go types and closed field/enum definitions.
Generate its JSON schemas and policy manifest from the same profile definitions
and test their byte digests. No new runtime dependency or general external
JSON Schema interpreter is required for v1. Only known profile digests receive
domain validation; unknown schemas remain explicitly unchecked in raw history
and cannot silently participate in development/v1 acceptance.

## 3. Structured facts

All profile events use a stable work subject, an operation ID, a source identity
with repository label and full Git revision, and exact schema_ref bytes.
Human summaries supplement structured fields. A timestamp never selects a winner.

| Proposed event type | Required meaning |
|---|---|
| dev.check.executed | Check name, command/arguments as reported data, exact source, outcome, exit code or signal when observed, evidence indexes |
| dev.review.completed | Exact reviewed source, verdict, stable finding IDs with severity, and explicit finding references resolved by this review |
| dev.acceptance.recorded | Exact source, selected check/review event refs, declared local policy digest, explicit decision and rationale |
| dev.next_action.recorded | An actor-declared next action and its blocking event/finding refs |

A finding reference consists of review_event_ref and finding_id. A later review
may resolve it only by naming it, naming the correction source, and citing proof.
A caused_by edge alone never closes a finding. A supersedes edge alone does not
supply missing acceptance or resolution semantics.

Check outcomes are passed, failed, interrupted, not_run, or unknown. Passed
requires exit code zero; failed requires an observed nonzero normal exit;
interrupted requires the observed interruption description/code or signal.
Unknown is used when the process handle was lost and the exit outcome is not known.
Coverage is separate completed/total data; partial is not a catch-all outcome.

Review verdicts are approved, changes_required, or inconclusive. Finding severity
is critical, important, or minor. A changes_required review names at least one
finding. Each finding ID is unique within that review and retains stable identity
through explicit resolution references.

The first profile accepts full committed source revisions. Do not claim that a
dirty working tree equals its HEAD. Freeze a source commit before a release
check; ordinary TDD logs may remain external evidence until the tested source
is identified. The workflow records reported execution facts and hashes their
proof; it does not independently prove that a caller actually ran a command.
It does not execute strings supplied in an event.

Local configured root authority is the existing proof basis. Shared signing keys
do not establish reviewer independence. Policy evaluation must expose that limit.

## 4. Proposed CLI and input contracts

These are planned commands; they do not exist at the baseline:

~~~text
pact record --repo PATH --profile development/v1 --input FILE --key-file FILE --json
pact brief --repo PATH --profile development/v1 --namespace NAME --subject SUBJECT --source-revision FULL --json
pact evidence check --repo PATH --event-ref REF --json
~~~

Record input is one JSON object containing operation_id, namespace, subject,
source, and events. Each event has local_id, type, payload, evidence, caused_by,
and supersedes; the profile supplies kind and exact schema_ref. Reject arrays
with an error that shows the expected object shape. No interactive prompts,
implicit key creation, namespace guessing, or automatic trust installation.

The record result separates admission (created, existing, conflict, or unknown),
object/event refs, schema validation, integrity, authenticity, authorization,
and index readiness. A failure after admission carries the object ID and the
next recovery action. Machine output uses arrays consistently for collections.

The brief requires an exact source revision and subject; it must not infer a
project's current code revision from wall clocks or Git's mutable HEAD. Its
default encoded budget is 16 KiB, configurable up to 64 KiB, including newline.
These are new workflow limits; existing 16 MiB query limits remain unchanged.
If required proof cannot fit, fail with a bounded limit diagnostic and narrowing
instructions. Do not drop blocking findings and then report acceptance.

## 5. Evidence contract

The v1 resolver supports only explicitly selected evidence in the caller's
repository: repository-relative regular files and immutable Git blobs.
A Git locator is git:<full-object-id>:<repository-relative-path>.
The repository is selected by --repo; the signed payload's repository label
remains a descriptive identity, not proof that a remote owns it.

Git reads invoke git show with separated process arguments, no shell evaluation,
no network, no external pager, and bounded output. Reject traversal, absolute
paths, symlink escapes, unsupported schemes, and credential-bearing locators.
Report old absolute file references as unsupported in this profile; users can
still inspect them through existing raw commands. This plan adds no legacy
locator fallback or migration path.

For new acceptance evidence, prefer Git revision/path/digest. Mutable file
evidence may support an observation but cannot become durable acceptance proof
until its exact bytes are retained in Git. A changed working file does not
invalidate a matching immutable Git blob. Classify matching, changed, unavailable,
unsupported, and unchecked separately. Hash exact bytes, not decoded/reformatted
text. Ordinary brief reads do not fetch every historical artifact.

Limit one selected evidence item to 16 MiB and one operation's selected evidence
to 64 MiB. Refuse overflow explicitly. Evidence contents never enter ledger
payloads, error messages, or ordinary brief output.

## 6. Reliable admission and retries

Idempotency is scoped by namespace, signing key ID, profile, and operation_id.
Compute a canonical request digest before choosing observed_at or parent heads.
An exact retry returns the original object/event refs even after other commits
advance the ledger or mutable evidence changes. Reconcile an admitted operation
after input/key identity validation and before requiring fresh evidence bytes;
report new evidence availability separately. Reusing the operation ID with different request facts is a
conflict and appends nothing.

Persist operation_id and request digest in signed profile events/metadata.
Canonical objects, not SQLite or a new mutable journal, establish whether an
operation was admitted. Recover from a missing index by scanning verified
canonical history. Two simultaneous identical record operations converge; two
different requests with the same key cannot both be admitted.

Existing Store.PutCanonical and index.Manager.Rebuild each acquire the store
mutation lock. Do not wrap them in another acquisition of that same lock.
Use an operation-specific workflow lock under .pact/tmp to serialize profile
retries, then scan/append via the existing owner APIs. Hold it through admission;
release it before index refresh. Low-level raw commit is outside this cooperative
idempotency guarantee; duplicate/conflicting raw claims must remain visible.
Test lock ordering and process termination with real subprocesses.

Admission and index refresh are separate durable outcomes. After verified
admission, an index failure never means nothing happened. An exact retry must
recover the object first and then retry rebuilding. Cancellation or a lost
response after publication requires canonical reconciliation, not blind append.
An advancing raw writer may leave the index stale again; report observed
readiness against the returned fingerprint rather than promising a global state.

## 7. Projection and acceptance

Project a strict verified local snapshot identified by exact frontier, profile
digest, policy digest, and projector implementation identity. The projector's
version/digest is distinct from the inspected work's source revision. Consume
only causally reachable selected history and
preserve commit atomicity. Report replica completeness and unresolved refs.
No arbitrary hash ordering or timestamp can resolve competing semantic claims.

For the requested source, return checks and their scope, open and resolved
findings, explicit acceptance decisions, evidence assessments actually performed,
and declared next actions. Unknown schemas, invalid payloads, unresolved relevant
refs, or unknown authorization prevent a positive supported-acceptance result.

The fixed local policy requires a matching explicit acceptance decision, matching
source checks and approved reviews named by that decision, durable matching
selected evidence, and no unresolved critical/important findings in the selected
work's known history. Findings on an earlier source remain open until explicitly
resolved; a source change alone does not erase them. A later causal blocking
finding makes a prior decision insufficient for the current inspected frontier.
Concurrent incompatible decisions remain unresolved. Minor findings stay visible.

Report recorded_decision separately from supported_acceptance. A signed acceptance
may exist while its evidence is now unavailable or a finding remains open.
No decision means not_recorded, not rejected. Unsupported means that the selected
local policy cannot substantiate the decision; it does not rewrite history.

This is a named local workflow policy, not proof of organizational approval or an
official signed checkpoint. Do not describe unregistered external policies as
governed. Schema/policy registration and arbitrary policy execution remain outside
the current engine's claims; the bundled profile is explicit local configuration.

Read operations remain read-only. A missing or stale index yields an actionable
index rebuild requirement. Do not silently mutate it during brief, log, query,
or evidence checking.

## 8. Adoption and success gate

Use one synthetic complete loop: failed review, corrected source, passing check,
explicit finding resolution, and explicit acceptance. Include an interruption
after admission, a changed file with recoverable Git evidence, and a payload with
an invalid outcome. Existing signed history remains byte-identical.

Compare a fresh context using the existing log/query/show guide with a fresh
context using the proposed brief. Give both the same public fixture, work goal,
and correct-answer rubric; exclude prior chat and broad status reports. Count
tool invocations, returned bytes, failures, missed blockers, and unsupported
claims. Keep correctness mandatory. Record the actual model and version.

First compare the read paths using a local prototype. Publish a new brief command
only if it preserves every required fact and reduces measured read effort.
A tie or regression keeps the existing guide as default and records the result.
Do not claim a general efficiency improvement from one paired trial.

## 9. Verification and release boundary

Every implementation kata requires a failing scenario before code, the passing
scenario after the smallest fix, unit and real-store integration tests, and
compiled CLI end-to-end checks. No mocked E2E or disabled functionality.
Use scripts/check, Go race tests, golangci-lint, and actual CLI usage. Reuse
tests/e2e/cli_test.go, tests/e2e/index_query_test.go, and cmd/pact tests.

Before shipping, review the new resolver's process/path boundary, cooperative
retry locking, and any positive acceptance result under truncated/conflicting
history. Record source revision, commands actually run, raw retained logs,
independent findings/resolutions, and final limits through PACT.

This document authorizes planning within the agreed direction. It does not
assert implementation completion, benchmark success, or release acceptance.
