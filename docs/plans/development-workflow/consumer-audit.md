<!-- ABOUTME: Records the evidence behind the development workflow proposal. -->
<!-- ABOUTME: Separates observed consumer behavior from proposed product benefits. -->

# Engagement OS consumer audit — 2026-11-11

## Boundary and method

Inspected the main development transcript and child session logs for ../2389-os-pact,
deduplicating inherited tool calls. Verified the complete local ledger and inspected
all 97 events at frontier
sha256:e003d0380ed6e8c48a5623e0be829c39fcb380515e0ef2bbe47584b704220414.
The namespace was org/2389/project/engagement-os/build. This is a local cut;
global completeness remains unknown.

The local transcript is session 01a1ae53-f692-7bc0-897b-9b54ac1e0e4a under
~/.codex/sessions/2026/11/05/. These logs contain private context and are not
copied into this repository. This audit records only nonprivate measurements,
artifact names, public object IDs, and the limits of the observations.

## Observed results

| Measure | Result |
|---|---|
| Canonical objects | 71 commits; zero checkpoints |
| Semantic events | 97, including 16 with status failed |
| Integrity, signatures, DAG, references | No reported errors |
| Authorization basis | One locally bootstrapped root; one signing identity |
| Commit graph | One initial commit and 70 single-parent commits |
| External evidence references | 177 occurrences; 165 unique locator/digest pairs |
| Current evidence bytes | 173 matching occurrences; four changed; none missing |
| Recovery of changed evidence | All four original byte sequences found in Git |
| Data reads in deduplicated command records | Five log reads; 14 show reads including failed attempts |
| Subject queries / cursor reads | Zero / zero |
| Wrong array-shaped event submissions | Four rejected attempts |
| Domain schema check performed during this audit | One nonconforming payload |

The changed evidence was:

| Event | Repository path | Git revision retaining matching bytes |
|---|---|---|
| build-start | docs/2389-engagement-os-saas-spec-v0.2.md | 78778740f124870e2648549b198a8e34dd2ad6d9 |
| s3-redaction-rereview | docs/build/s3-redaction-report.md | 081ade6d0ada953d9938cbc329ae4d5fa22b2355 |
| s3-restore-gate | docs/build/s3-restore-fix-task.md | ce13e6c3a7c56fc2b9eeafb9e24e366e524baa3b |
| browser-design-foundation-merged-2026-11-07 | docs/build/browser-design-foundation.md | 78a849d13744da63808364cc2cfa403d1d2bd762 |

The payload for source-worker-recovery-live-2026-11-10 in commit
sha256:03e9ac26f90f75df8d721683e63add2c590ebef75c876940a1b8d54841bf9563
uses status partial. Its exact schema,
sha256:4c74196fdc4cbc601dd7233d0f255355a69aa13bf6f2b592702a8db57d914106,
permits started, passed, failed, blocked, and reviewed. Ledger verification
does not execute external domain schemas, so its healthy result does not
contradict this payload failure.

## Demonstrated value and limits

- Failed reviews remained distinguishable from passing checks. The ledger also
  retained a correction to an earlier coordinator claim based on a passing log tail.
- At transcript line 15903, the agent noticed that a deployment report was
  already hashed by PACT and wrote an addendum instead of changing that report.
- Historical digests made the four changed references detectable and gave exact
  targets for Git recovery. Recovery still required work outside PACT.
- Handoffs relied largely on reports, trackers, and Git. Sparse query use is
  evidence about adoption, not proof that the existing read API cannot work.
- Tests and reviewers found application defects. This audit does not establish
  a causal improvement in defect detection, throughput, token use, or cost.
- Linear history under one root does not demonstrate delegated authority,
  independent reviewer identity, replica synchronization, or conflict resolution.

The earlier fresh-context experiment in
[agent-resume-index-recovery.md](../../status/agent-resume-index-recovery.md)
already succeeded with existing commands under both skill versions. The updated
guide used more shell calls. Any new read surface must be tested against that
working baseline, rather than claiming a benefit from its existence.

## Consequence

Prioritize a small development profile, validated recording, recoverable evidence,
and an explicit work projection. Keep the existing signed formats, low-level
commands, and separation between authorship, authority, evidence, and acceptance.
