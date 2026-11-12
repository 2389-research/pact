<!-- ABOUTME: Defines the development workflow epic and its final acceptance boundary. -->
<!-- ABOUTME: Links the approved direction to independently verifiable implementation katas. -->

# Make PACT support development recording and handoffs

Doctor Biz authorized documenting and planning the workflow on 2026-11-11 after
the Engagement OS consumer audit. This epic tracks implementation; it remains
open until its children and final verification are complete.

The product goal is a small development/v1 layer above the domain-neutral signed
ledger: structured facts, validated single-operation recording, recoverable
Git evidence, explicit state/acceptance, and a measured useful handoff read path.

Canonical plan directory: docs/plans/development-workflow/.
Read design.md, consumer-audit.md, implementation-plan.md, and the applicable
tasks/ brief. Kata holds mutable status/ownership/dependencies; the repository
holds the common design and task instructions; PACT records signed assertions
and exact evidence. No implementation is claimed by this planning pass.

## Deliverables

1. Shared profile definitions and public synthetic scenario corpus.
2. Pre-sign validation of exact supported domain inputs.
3. Bounded local/Git evidence resolver and evidence-check CLI.
4. Reliable record operation with explicit retry and admission/index outcomes.
5. Deterministic state projection with exact source, finding resolution, and
   separate recorded decision versus supported local acceptance.
6. Read-path prototype/evaluation; public brief only if its promotion gate passes.
7. Complete real compiled-CLI lifecycle and interruption/recovery verification.
8. Fresh-context handoff evaluation and updates to the original skill/README.

## Epic acceptance

- A green check with an unresolved Important finding never implies acceptance.
- Exact source, declared finding closure, and matching durable evidence support
  every positive result under the named local policy.
- Repeating an admitted operation cannot silently append it again.
- Invalid payloads refuse before signing; changed evidence is detected and
  historical Git bytes remain recoverable.
- Conflicting/incomplete history and required facts over the byte bound remain
  explicit; no timestamp winner or silent truncation.
- Canonical/reference/Go race/lint/real CLI gates pass with no weakened tests.
- Read-effort results are measured against existing log/query/show; ties and
  regressions are recorded honestly. An unpublished brief is not called shipped.
- Review findings, source, commands actually run, evidence hashes, policy/profile
  identity, and remaining limits are recorded in PACT.
- Every child has substantive closure evidence before the epic closes.

## Exclusions

No synchronization, new authority model, daemon, general schema/policy engine,
private consumer fixture import, historical event rewrite, automatic release,
or automatic main merge. Keep raw commit/query contracts and canonical bytes.
