<!-- ABOUTME: Records the planning checks, review corrections, and provenance publication. -->
<!-- ABOUTME: Distinguishes this completed planning work from open implementation katas. -->

# Development workflow planning publication

Date: 2026-11-11.
Scope: repository documentation and Kata planning; no production implementation.
Baseline source: fd1f227794d6bf9956bd8eea0798254e0134918f.
Branch: docs/development-workflow-plan.

## Planning artifacts

The design, consumer audit, eight self-contained task briefs, epic brief, and
dependency manifest live beside this report. Epic pact#ns4y and all eight
implementation children remain open. The next implementation task is pact#khqq.

Repository task briefs are the planning source. Each Kata body is a read-back
checked publication of its corresponding brief; katas.json records the body
digests and the issue IDs at this planning cut. Live execution status remains in
Kata. The manifest does not track future status changes.

## Checks and review

- Baseline scripts/check passed, including Go vet, the Go suite, compiled CLI
  lifecycle/index tests, and all 18 Python reference tests.
- Baseline golangci-lint run passed with zero issues.
- Documentation changes add no production behavior and claim no new-feature
  test pass. Existing source checks cover only the unchanged baseline.
- Inline review checked the design against the eight task boundaries and fixed
  retry reconciliation after evidence drift, a missing projector identity,
  and incorrect draft references to the E2E package paths.
- Final plan/Kata consistency readback is retained in evidence/. It checks
  bodies/digests, open status, parent/blocking edges, task coverage and local links.

The planning source intentionally does not contain speculative production code.
The future implementation tasks require TDD, real-store integration, compiled
usage, canonical/race/lint gates, review, and signed evidence before closure.

## PACT publication boundary

The canonical ledger remains the main checkout's .pact under org/2389/pact.
The managed worktree excludes its restored ledger copy. The signed planning
commit is sha256:8ffe712f46822413a784d0b2fbc509e5525054eabac5685030a21c2bd2bde54b.
Its artifacts, direction, and planning-checks events name Git revision
63e876bef56c84f137f280b7788cdc295cdef65b and 17 distinct retained evidence
files with exact byte digests. Strict post-write integrity, signature, DAG,
and reference verification passed. Authorization is authorized by the existing
locally bootstrapped root; the rebuilt index is current and the replica is
locally closed, with global completeness unknown.

The exact result is retained in [planning-ledger.json](evidence/planning-ledger.json).
The new canonical object is included in the task branch through Git object/index
staging; no second PACT store was initialized and no object was manually admitted
through file copying. All implementation katas remain open.
The report itself stays outside that signed artifact set to avoid a circular
reference between its event ID and its own hash.

No merge, release, delegation, independent reviewer identity, or implementation
acceptance is asserted by the planning record.
