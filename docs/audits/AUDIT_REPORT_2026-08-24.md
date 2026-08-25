<!-- ABOUTME: Audits the current operator README and historical Phase 0/1 dogfood status. -->
<!-- ABOUTME: Records verified claims, corrected drift, pattern expansion, and inventory checks. -->

# Documentation Audit Report

Generated: 2026-08-24 | Base revision: `e66f333` plus the Task 7 working tree

## Executive summary

| Metric | Count |
|---|---:|
| Documents scanned | 2 |
| Claim groups checked | 33 |
| Verified without correction | 25 |
| Corrected during audit | 8 |
| Remaining Critical findings | 0 |
| Remaining Important findings | 0 |

The audit covered `README.md` and
`docs/status/phase-0-1-dogfood.md`. Two independent read-only reviewers checked
operator commands, flags, paths, state transitions, limits, historic IDs,
counts, permissions, negative capability claims, and recovery statements
against current source, compiled command behavior, tests, Git history, and a
clean replay of the historical revision where needed.

## Corrections made during audit

The README no longer says every unavailable index state can recover through a
bare rebuild. Rebuild repairs derived database contents only after the index
directory, live path, and SQLite sidecar shape are safe. The guide now also
separates replica incompleteness from unavailable index coverage, describes the
`index_stale` to rebuild to `cursor_stale` transition, and names the distinct
machine-readable outcomes for resource, usage, cursor, and oversized-index
failures. It records NFC normalization for subject and tag filters.

The Phase 0/1 status page now labels itself as a historical snapshot. Its Phase
2 exclusions are tied to revision `9428ad1`, its recovery procedure is clearly
recorded evidence from the original run, and its former “next milestone” text
now acknowledges that Phase 2 began after that snapshot.

## Verified inventory

- The README lists all 13 shipped command shapes: `init`, `keygen`, `trust-add`,
  `hash`, `commit`, `heads`, `show`, `verify`, `checkpoint`, `index status`,
  `index rebuild`, `log`, and `query`.
- The documented log and query flags match the command adapters. Query exposes
  ten fixed filter families; it has no raw SQL surface.
- The five unusable index states, current complete and partial coverage, and
  explicit recovery boundary match the validator and rebuild code.
- Causal batches never use `observed_at`; cursor binding and failure transitions
  match the cursor and hydration code plus compiled restart tests.
- Every numeric value in `pact/resource-limits/phase2-v1` matches the production
  profile. Boundary behavior maps to compiled, integration, or focused
  algorithm tests without claiming giant duplicate E2E fixtures.
- The historical store still contains the documented Phase 0/1 commit and
  checkpoint IDs. Current read-only verification reproduces the documented
  counts and head, and a clean `9428ad1` export passes its historical gate with
  17 Python tests.
- Setup remains absent from the production dispatcher and preserved only on its
  named review branch. Phase 3, network sync, policy execution, hardware-key
  service, trusted timestamps, and global completeness remain unshipped.

## Reviewed acceptance and historical claims

Historical action claims need a narrower standard than live operator claims.
The Phase 0/1 object IDs, counts, modes, branch artifacts, and historical gate
are reproducible. Repository history preserves the recovery procedure as
recorded evidence, not as a reconstructable transcript of every shell action.
The status page now says so. No false, unresolved, Critical, or Important
documentation finding remains.

## Pattern expansion and gap detection

Pass 2A expanded every enumerated and negative claim: command lists, filter
families, index-state lists, limit rows, scope exclusions, setup artifacts, and
historical counts. It found no remaining false variant after the corrections.

Pass 2B compared the dispatcher and index subcommands with the README inventory,
then compared every accepted flag with the operator examples and prose. No
shipped command is missing from the inventory, no documented flag is absent,
and no raw SQL, setup, or Phase 3 command is present.
